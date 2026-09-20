package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/adapters/httpclient"
)

// firecrawl 引擎（spec §6.1）：POST {endpoint}/v2/scrape，formats=["markdown","links"]，
// onlyMainContent 映射 extract.mode==readability；markdown 直通（不跑本地抽取）。
// endpoint 默认 https://api.firecrawl.dev，connection config 的 endpoint_url 覆盖
//（自托管栈一等集成，spec §1）。
//
// robots 与 SSRF：抓取在 provider 基础设施执行，robots 由 provider 侧决定
//（firecrawl 遵守目标站 robots，spec §6.3）；琅嬛在发送前对目标 URL 做解析 +
// 私网拒绝（ValidateWebTarget），不声称端到端 SSRF 保证（spec §6.4）。
// 层内不做 429/5xx 退避重试：失败按 ClassifyStatus 等价语义交 Job 级重试。

// DefaultFirecrawlEndpoint 是 firecrawl 官方云 endpoint。
const DefaultFirecrawlEndpoint = "https://api.firecrawl.dev"

// FirecrawlFetcherConfig 配置 firecrawl 引擎抓取器。
type FirecrawlFetcherConfig struct {
	// APIKey 是 firecrawl connection 的 api_key（解密后明文，仅内存透传）。
	APIKey string
	// Endpoint 是 API base（connection config 的 endpoint_url 覆盖）；空值用官方云。
	Endpoint string
	// OnlyMainContent 对应 extract.mode==readability（正文过滤交由 provider 侧）。
	OnlyMainContent bool
	// Timeout 是单次请求超时；<=0 用 defaultProviderTimeout。
	Timeout time.Duration
	// MaxContentBytes 是 markdown 内容上限；<=0 表示不限（请求级 MaxContentBytes 优先）。
	MaxContentBytes int64
	// validateTarget 可注入（测试绕过真实 DNS 预校验）；nil 用
	// httpclient.ValidateWebTarget（发送前公网校验，spec §6.4）。
	validateTarget func(ctx context.Context, rawURL string) error
}

// FirecrawlFetcher 是 firecrawl 引擎的单页抓取器。
type FirecrawlFetcher struct {
	client          *http.Client
	apiKey          string
	endpoint        string
	onlyMainContent bool
	maxContentBytes int64
	validateTarget  func(ctx context.Context, rawURL string) error
}

// NewFirecrawlFetcher 构造 firecrawl 引擎抓取器；api_key 缺失视为装配错误
// （显式引擎与 auto 升级通道的凭证判空在 connector 装配处先行，这里是最终防线）。
func NewFirecrawlFetcher(cfg FirecrawlFetcherConfig) (*FirecrawlFetcher, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("firecrawl 引擎必须提供 api_key（firecrawl connection 凭证）")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultFirecrawlEndpoint
	}
	validateTarget := cfg.validateTarget
	if validateTarget == nil {
		validateTarget = httpclient.ValidateWebTarget
	}
	return &FirecrawlFetcher{
		client:          providerHTTPClient(cfg.Timeout),
		apiKey:          cfg.APIKey,
		endpoint:        endpoint,
		onlyMainContent: cfg.OnlyMainContent,
		maxContentBytes: cfg.MaxContentBytes,
		validateTarget:  validateTarget,
	}, nil
}

// firecrawlScrapeRequest 是 /v2/scrape 请求体（v2 API 合同的最小子集）。
type firecrawlScrapeRequest struct {
	URL             string   `json:"url"`
	Formats         []string `json:"formats"`
	OnlyMainContent bool     `json:"onlyMainContent"`
}

// firecrawlScrapeResponse 是 /v2/scrape 响应体的最小解析形状：
// metadata.statusCode/url 提供目标页的等价状态与最终地址。
type firecrawlScrapeResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Markdown string   `json:"markdown"`
		Links    []string `json:"links"`
		Metadata struct {
			URL        string `json:"url"`
			StatusCode int    `json:"statusCode"`
		} `json:"metadata"`
	} `json:"data"`
}

// Fetch 抓取单页。返回 nil error 表示取得可处理结果（markdown 直通）；否则返回
// *PageError，Outcome/Status 表达失败分类（ClassifyError 可复现同一结论）：
//   - provider 通道自身非 2xx（鉴权失败/配额/服务故障）→ ClassifyStatus(该状态)；
//   - success=false（2xx 但抓取任务失败）→ retryable（provider 瞬时故障）；
//   - 目标页失败经 metadata.statusCode 透出 → ClassifyStatus 等价映射
//     （目标 404 → gone、目标 401/403 → blocked、目标 5xx → retryable）。
func (f *FirecrawlFetcher) Fetch(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	// 发送前目标 URL 预校验（spec §6.4）。
	if err := f.validateTarget(ctx, request.URL); err != nil {
		return fetchedPage{}, &PageError{
			Outcome: OutcomeSkipped,
			Err:     fmt.Errorf("抓取目标未通过公网预校验: %w", err),
		}
	}
	maxBytes := request.MaxContentBytes
	if maxBytes <= 0 {
		maxBytes = f.maxContentBytes
	}

	payload, err := json.Marshal(firecrawlScrapeRequest{
		URL:             request.URL,
		Formats:         []string{"markdown", "links"},
		OnlyMainContent: f.onlyMainContent,
	})
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeSkipped, Err: fmt.Errorf("编码 firecrawl 请求失败: %w", err)}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(f.endpoint, "/")+"/v2/scrape", bytes.NewReader(payload))
	if err != nil {
		// endpoint 不可解析属连接配置问题，重试无益：按 skipped 处理。
		return fetchedPage{}, &PageError{Outcome: OutcomeSkipped, Err: fmt.Errorf("构造 firecrawl 请求失败: %w", err)}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.client.Do(httpRequest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: fmt.Errorf("firecrawl 请求被中断: %w", ctxErr)}
		}
		return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: fmt.Errorf("firecrawl 请求失败: %w", err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		status := resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseDrainBytes))
		_ = resp.Body.Close()
		return fetchedPage{}, providerStatusError(status, fmt.Errorf("firecrawl 返回 HTTP %d", status))
	}

	body, err := readProviderBody(resp, maxBytes)
	if err != nil {
		return fetchedPage{}, err
	}
	var parsed firecrawlScrapeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fetchedPage{}, &PageError{
			Status:  resp.StatusCode,
			Outcome: OutcomeRetryable,
			Err:     fmt.Errorf("解析 firecrawl 响应失败: %w", err),
		}
	}
	if !parsed.Success {
		return fetchedPage{}, &PageError{
			Status:  resp.StatusCode,
			Outcome: OutcomeRetryable,
			Err:     fmt.Errorf("firecrawl 抓取任务失败"),
		}
	}
	// 目标页失败的等价语义：metadata.statusCode 是 provider 观测到的目标页状态。
	if pageStatus := parsed.Data.Metadata.StatusCode; pageStatus >= 400 {
		return fetchedPage{}, providerStatusError(pageStatus,
			fmt.Errorf("目标页返回 HTTP %d（firecrawl metadata）", pageStatus))
	}
	status := parsed.Data.Metadata.StatusCode
	if status < 200 || status > 299 {
		status = http.StatusOK
	}
	if err := checkMarkdownSize([]byte(parsed.Data.Markdown), status, maxBytes); err != nil {
		return fetchedPage{}, err
	}
	finalURL := strings.TrimSpace(parsed.Data.Metadata.URL)
	if finalURL == "" {
		finalURL = request.URL
	}
	return fetchedPage{
		FinalURL: finalURL,
		Status:   status,
		Markdown: []byte(parsed.Data.Markdown),
		Links:    parsed.Data.Links,
		Engine:   EngineFirecrawl,
	}, nil
}
