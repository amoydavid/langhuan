package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/adapters/httpclient"
)

// jina 引擎（spec §6.1）：GET https://r.jina.ai/<url> + 头组
//（X-Engine/X-Respond-With/X-With-Links-Summary/Authorization），markdown 直通
//（不跑本地抽取，readability 过滤为 provider 侧默认行为）。
//
// robots 与 SSRF：抓取在 provider 基础设施执行，robots 行为由 provider 侧决定
//（jina 不保证遵守，spec §6.3 文档明示）；琅嬛在发送前对目标 URL 做解析 + 私网
// 拒绝（ValidateWebTarget），不声称端到端 SSRF 保证（spec §6.4）。
// 层内不做退避重试：失败按 ClassifyStatus 等价语义交 Job 级重试。

// DefaultJinaReaderEndpoint 是 jina reader 的公开端点。
const DefaultJinaReaderEndpoint = "https://r.jina.ai"

// jina 响应的结构标记（reader 文本响应约定）：头部元数据行 + 「Markdown Content:」
// 分隔 + 可选的「Links/Buttons:」链接摘要节（X-With-Links-Summary: all 时附加）。
const (
	jinaMarkdownContentMarker = "Markdown Content:"
	jinaLinksSummaryMarker    = "Links/Buttons:"
	jinaURLSourceMarker       = "URL Source:"
)

// jinaLinkPattern 匹配链接摘要节里的 [label](url) 项，捕获 url。
var jinaLinkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)

// JinaFetcherConfig 配置 jina 引擎抓取器。
type JinaFetcherConfig struct {
	// APIKey 是 jina connection 的 api_key（解密后明文，仅内存透传）。
	APIKey string
	// Endpoint 是 reader 端点；空值用 https://r.jina.ai（首版无覆盖入口，保留参数
	// 与 firecrawl 对称，测试注入假端点用）。
	Endpoint string
	// Timeout 是单次请求超时；<=0 用 defaultProviderTimeout。
	Timeout time.Duration
	// MaxContentBytes 是 markdown 内容上限；<=0 表示不限（请求级 MaxContentBytes 优先）。
	MaxContentBytes int64
	// validateTarget 可注入（测试绕过真实 DNS 预校验）；nil 用
	// httpclient.ValidateWebTarget（发送前公网校验，spec §6.4）。
	validateTarget func(ctx context.Context, rawURL string) error
}

// JinaFetcher 是 jina 引擎的单页抓取器。
type JinaFetcher struct {
	client          *http.Client
	apiKey          string
	endpoint        string
	maxContentBytes int64
	validateTarget  func(ctx context.Context, rawURL string) error
}

// NewJinaFetcher 构造 jina 引擎抓取器；api_key 缺失视为装配错误。
func NewJinaFetcher(cfg JinaFetcherConfig) (*JinaFetcher, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("jina 引擎必须提供 api_key（jina connection 凭证）")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultJinaReaderEndpoint
	}
	validateTarget := cfg.validateTarget
	if validateTarget == nil {
		validateTarget = httpclient.ValidateWebTarget
	}
	return &JinaFetcher{
		client:          providerHTTPClient(cfg.Timeout),
		apiKey:          cfg.APIKey,
		endpoint:        endpoint,
		maxContentBytes: cfg.MaxContentBytes,
		validateTarget:  validateTarget,
	}, nil
}

// Fetch 抓取单页。返回 nil error 表示取得可处理结果（markdown 直通）；否则返回
// *PageError：provider 通道非 2xx（鉴权/配额/服务故障）→ ClassifyStatus 等价语义。
func (f *JinaFetcher) Fetch(ctx context.Context, request FetchRequest) (fetchedPage, error) {
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

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(f.endpoint, "/")+"/"+request.URL, nil)
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeSkipped, Err: fmt.Errorf("构造 jina 请求失败: %w", err)}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+f.apiKey)
	httpRequest.Header.Set("X-Engine", "auto")
	httpRequest.Header.Set("X-Respond-With", "markdown")
	httpRequest.Header.Set("X-With-Links-Summary", "all")

	resp, err := f.client.Do(httpRequest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: fmt.Errorf("jina 请求被中断: %w", ctxErr)}
		}
		return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: fmt.Errorf("jina 请求失败: %w", err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		status := resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseDrainBytes))
		_ = resp.Body.Close()
		return fetchedPage{}, providerStatusError(status, fmt.Errorf("jina 返回 HTTP %d", status))
	}

	body, err := readProviderBody(resp, maxBytes)
	if err != nil {
		return fetchedPage{}, err
	}
	markdown, links, finalURL := parseJinaReaderResponse(string(body))
	if err := checkMarkdownSize(markdown, http.StatusOK, maxBytes); err != nil {
		return fetchedPage{}, err
	}
	if finalURL == "" {
		finalURL = request.URL
	}
	return fetchedPage{
		FinalURL: finalURL,
		Status:   http.StatusOK,
		Markdown: markdown,
		Links:    links,
		Engine:   EngineJina,
	}, nil
}

// parseJinaReaderResponse 解析 reader 文本响应：剥离头部元数据（Title/URL Source）
// 与尾部链接摘要节，返回（markdown 正文, 出链 href 集合, 最终地址）。
// 响应缺少约定标记时按整体 markdown 兜底（provider 形状演进不致命）。
func parseJinaReaderResponse(body string) ([]byte, []string, string) {
	content := body
	finalURL := ""

	// 头部：Markdown Content: 之前是 Title/URL Source 等元数据行。
	if before, after, found := strings.Cut(content, jinaMarkdownContentMarker+"\n"); found {
		finalURL = jinaURLSourceValue(before)
		content = after
	} else {
		finalURL = jinaURLSourceValue(content)
	}

	// 尾部：Links/Buttons: 起为链接摘要节，不计入正文。
	links := []string(nil)
	if before, after, found := strings.Cut(content, "\n"+jinaLinksSummaryMarker); found {
		content = before
		for _, match := range jinaLinkPattern.FindAllStringSubmatch(after, -1) {
			if href := strings.TrimSpace(match[1]); href != "" {
				links = append(links, href)
			}
		}
	}
	return []byte(strings.TrimSpace(content)), links, finalURL
}

// jinaURLSourceValue 从响应头部提取 URL Source: 行的值（最终地址）。
func jinaURLSourceValue(header string) string {
	for _, line := range strings.Split(header, "\n") {
		if value, found := strings.CutPrefix(line, jinaURLSourceMarker); found {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
