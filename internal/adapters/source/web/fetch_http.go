package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 引擎合同（fetchedPage / PageOutcome / PageError）见 fetch_contract.go；
// 本文件只实现 http 引擎自身的抓取行为（限速、重试、条件请求、字符集、内容上限）。

// maxResponseDrainBytes 是丢弃响应体前最多读取的字节数：只为复用连接，不做语义处理
// （robots.go 读取后同样复用该值）。
const maxResponseDrainBytes = 64 * 1024

// 抓取重试与限速的默认值（生产由 T7 从 config.yaml web_source 小节注入）。
const (
	defaultHTTPMaxRetries = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
	maxRetryDelay         = 30 * time.Second
	maxRetryAfterDelay    = 60 * time.Second
	acceptHeaderValue     = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	httpDateLayout        = http.TimeFormat
)

// HTTPFetcherConfig 配置 http 引擎单页抓取器。
type HTTPFetcherConfig struct {
	// Client 必须注入 SSRF-safe client（生产为 httpclient.NewPublicWebClient 的产物：
	// dial 期 IP 校验 + redirect 逐跳重校验 + 私网/环回/链路本地阻断，spec §6.4）。
	Client *http.Client
	// UserAgent 是抓取 UA（config.yaml web_source.user_agent_template）。
	UserAgent string
	// RequestDelay 是同一 host 上两次请求之间的最小间隔
	// （config.yaml web_source.default_request_delay_ms）。
	RequestDelay time.Duration
	// MaxRetries 是 429/5xx 的重试次数上限（不含首次请求）；<=0 用 defaultHTTPMaxRetries。
	MaxRetries int
	// RetryBaseDelay 是指数退避基数；<=0 用 defaultRetryBaseDelay。
	RetryBaseDelay time.Duration
	// MaxContentBytes 是单页响应体上限；<=0 表示不限（请求级 MaxContentBytes 优先）。
	MaxContentBytes int64
}

// HTTPFetcher 是 http 引擎的单页抓取器（spec §6.1）。
//
// 职责边界：只做「取回一页 HTML 并给出分类」，不做 robots 校验、抽取、
// 去重与缓存策略（分别由 RobotsChecker、Extract、PageCache 与编排层负责）。
//
// 礼貌性：同一 host 上请求串行 + 请求间隔（spec §5.1）。跨 host 的串行由编排层保证
// （首版发现/抓取在单协程内顺序推进）；本层只做 per-host 串行，以便未来按真实性能
// 证据放开多 host 并发时无需改本层。
type HTTPFetcher struct {
	client          *http.Client
	userAgent       string
	requestDelay    time.Duration
	maxRetries      int
	retryBaseDelay  time.Duration
	maxContentBytes int64

	// now/sleep 可注入（测试用）；生产为 time.Now 与可取消睡眠。
	now   func() time.Time
	sleep func(context.Context, time.Duration) error

	mu    sync.Mutex
	hosts map[string]*hostLimiter
}

// hostLimiter 是单个 host 的串行锁与最近一次请求时间。
type hostLimiter struct {
	mu            sync.Mutex
	lastRequestAt time.Time
}

// FetchRequest 是单页抓取输入。
type FetchRequest struct {
	// URL 是待抓取的绝对 URL（必须已规范化，页面身份口径）。
	URL string
	// IfNoneMatch / IfModifiedSince 为空表示不发条件请求（provider/render 通道忽略）。
	IfNoneMatch     string
	IfModifiedSince string
	// MaxContentBytes 覆盖 HTTPFetcherConfig.MaxContentBytes（>0 时生效）。
	MaxContentBytes int64
	// LastEngine 是该文档上次成功抓取的引擎（documents.source_meta.last_engine
	// 经 FetchOptions 透传）。仅 auto 引擎消费（last_engine 短路，spec §6.2）；
	// 其它引擎忽略。
	LastEngine string
}

// NewHTTPFetcher 构造 http 引擎抓取器；未注入 client 视为装配错误
// （nil client 意味着无 SSRF 防护且无超时，必须显式提供）。
func NewHTTPFetcher(cfg HTTPFetcherConfig) (*HTTPFetcher, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("http 引擎必须注入 SSRF-safe HTTP client")
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultHTTPMaxRetries
	}
	retryBaseDelay := cfg.RetryBaseDelay
	if retryBaseDelay <= 0 {
		retryBaseDelay = defaultRetryBaseDelay
	}
	return &HTTPFetcher{
		client:          cfg.Client,
		userAgent:       cfg.UserAgent,
		requestDelay:    cfg.RequestDelay,
		maxRetries:      maxRetries,
		retryBaseDelay:  retryBaseDelay,
		maxContentBytes: cfg.MaxContentBytes,
		now:             time.Now,
		sleep:           sleepWithContext,
		hosts:           make(map[string]*hostLimiter),
	}, nil
}

// Fetch 抓取单页。
//
// 返回 nil error 表示取得了可处理的结果（含 NotModified）；否则返回 *PageError，
// 其 Outcome/Status 表达失败分类（ClassifyError(err) 可复现同一结论）。
// 429/5xx 在层内指数退避重试（上限 MaxRetries 次，优先采用 Retry-After）；
// 网络错误不在层内重试，直接按 OutcomeRetryable 交给 Job 级 RetryRequired。
func (f *HTTPFetcher) Fetch(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	target, err := parseFetchTarget(request.URL)
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeSkipped, Err: err}
	}
	maxBytes := request.MaxContentBytes
	if maxBytes <= 0 {
		maxBytes = f.maxContentBytes
	}

	release, err := f.lockHost(ctx, target.Hostname())
	if err != nil {
		return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: err}
	}
	defer release()

	for attempt := 0; ; attempt++ {
		response, err := f.do(ctx, target, request)
		if err != nil {
			return fetchedPage{}, &PageError{Outcome: OutcomeRetryable, Err: err}
		}

		if response.StatusCode == http.StatusNotModified {
			page := fetchedPage{
				FinalURL:     finalURL(response, target),
				Status:       response.StatusCode,
				ETag:         response.Header.Get("ETag"),
				LastModified: response.Header.Get("Last-Modified"),
				Engine:       EngineHTTP,
				NotModified:  true,
			}
			drainAndClose(response)
			return page, nil
		}

		if isRetryableStatus(response.StatusCode) && attempt < f.maxRetries {
			delay := f.retryDelay(attempt, response)
			drainAndClose(response)
			if err := f.sleep(ctx, delay); err != nil {
				return fetchedPage{}, &PageError{Status: response.StatusCode, Outcome: OutcomeRetryable, Err: err}
			}
			continue
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			status := response.StatusCode
			drainAndClose(response)
			return fetchedPage{}, &PageError{
				Status:  status,
				Outcome: ClassifyStatus(status),
				Err:     fmt.Errorf("HTTP 状态 %d", status),
			}
		}

		if contentType := strings.TrimSpace(response.Header.Get("Content-Type")); !isHTMLResponse(contentType) {
			status := response.StatusCode
			drainAndClose(response)
			return fetchedPage{}, &PageError{
				Status:  status,
				Outcome: OutcomeSkippedNonHTML,
				Err:     fmt.Errorf("响应内容类型不可抽取 (Content-Type: %s)", contentType),
			}
		}

		body, err := readBody(response, maxBytes)
		if err != nil {
			return fetchedPage{}, err
		}
		return fetchedPage{
			FinalURL:     finalURL(response, target),
			Status:       response.StatusCode,
			HTML:         body,
			Links:        extractHrefs(body),
			ETag:         response.Header.Get("ETag"),
			LastModified: response.Header.Get("Last-Modified"),
			Engine:       EngineHTTP,
		}, nil
	}
}

// lockHost 取得 host 串行锁并保证与上一次请求之间的最小间隔；
// 返回的释放函数必须在本次抓取（含读体）结束后调用。
func (f *HTTPFetcher) lockHost(ctx context.Context, host string) (func(), error) {
	limiter := f.hostLimiter(host)
	limiter.mu.Lock()
	if !limiter.lastRequestAt.IsZero() && f.requestDelay > 0 {
		wait := f.requestDelay - f.now().Sub(limiter.lastRequestAt)
		if wait > 0 {
			if err := f.sleep(ctx, wait); err != nil {
				limiter.mu.Unlock()
				return nil, err
			}
		}
	}
	limiter.lastRequestAt = f.now()
	return limiter.mu.Unlock, nil
}

func (f *HTTPFetcher) hostLimiter(host string) *hostLimiter {
	f.mu.Lock()
	defer f.mu.Unlock()
	limiter, ok := f.hosts[host]
	if !ok {
		limiter = &hostLimiter{}
		f.hosts[host] = limiter
	}
	return limiter
}

// do 发起单次 GET（含条件请求头与 UA）。
func (f *HTTPFetcher) do(ctx context.Context, target *url.URL, request FetchRequest) (*http.Response, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构造抓取请求失败: %w", err)
	}
	if f.userAgent != "" {
		httpRequest.Header.Set("User-Agent", f.userAgent)
	}
	httpRequest.Header.Set("Accept", acceptHeaderValue)
	if request.IfNoneMatch != "" {
		httpRequest.Header.Set("If-None-Match", request.IfNoneMatch)
	}
	if request.IfModifiedSince != "" {
		httpRequest.Header.Set("If-Modified-Since", request.IfModifiedSince)
	}
	response, err := f.client.Do(httpRequest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("抓取被中断: %w", ctxErr)
		}
		return nil, fmt.Errorf("抓取请求失败: %w", err)
	}
	return response, nil
}

// retryDelay 计算下一次重试的等待时长：优先采用 Retry-After（上限 maxRetryAfterDelay），
// 否则按指数退避（上限 maxRetryDelay）。
func (f *HTTPFetcher) retryDelay(attempt int, response *http.Response) time.Duration {
	if delay, ok := retryAfterDelay(response, f.now()); ok {
		return delay
	}
	delay := f.retryBaseDelay << attempt
	if delay > maxRetryDelay || delay <= 0 {
		return maxRetryDelay
	}
	return delay
}

// retryAfterDelay 解析 Retry-After（delta-seconds 或 HTTP-date）。0 是合法值（立即重试）。
func retryAfterDelay(response *http.Response, now time.Time) (time.Duration, bool) {
	raw := strings.TrimSpace(response.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if seconds, err := time.ParseDuration(raw + "s"); err == nil {
		return clampRetryAfter(seconds), true
	}
	if at, err := time.Parse(httpDateLayout, raw); err == nil {
		return clampRetryAfter(at.Sub(now)), true
	}
	return 0, false
}

func clampRetryAfter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	if delay > maxRetryAfterDelay {
		return maxRetryAfterDelay
	}
	return delay
}

// isRetryableStatus 报告状态码是否属于层内重试范围（spec §6.1：429/5xx）。
func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// readBody 读取响应体：按声明的字符集转为 UTF-8，并施加上限检查。
func readBody(response *http.Response, maxBytes int64) ([]byte, error) {
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseDrainBytes))
		_ = response.Body.Close()
	}()

	reader := utf8Body(response.Body, response.Header.Get("Content-Type"))
	if maxBytes <= 0 {
		body, err := io.ReadAll(reader)
		if err != nil {
			return nil, &PageError{Status: response.StatusCode, Outcome: OutcomeRetryable, Err: fmt.Errorf("读取响应体失败: %w", err)}
		}
		return body, nil
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, &PageError{Status: response.StatusCode, Outcome: OutcomeRetryable, Err: fmt.Errorf("读取响应体失败: %w", err)}
	}
	if int64(len(body)) > maxBytes {
		return nil, &PageError{
			Status:  response.StatusCode,
			Outcome: OutcomeOversize,
			Err:     fmt.Errorf("%w: 页面内容超过上限 %d 字节", sourceport.ErrSourceContentTooLarge, maxBytes),
		}
	}
	return body, nil
}

// utf8Body 把响应体按 Content-Type/meta 声明的字符集转为 UTF-8，
// 使下游（链接提取、jshell 判定、抽取）只需处理 UTF-8。
// 字符集未知时按原样读取（退化为 UTF-8 假设），不因此失败。
func utf8Body(body io.Reader, contentType string) io.Reader {
	converted, err := charset.NewReader(body, contentType)
	if err != nil {
		return body
	}
	return converted
}

// htmlMediaTypes 是可抽取的 HTML media type（spec §7）。
var htmlMediaTypes = map[string]struct{}{
	"text/html":             {},
	"application/xhtml+xml": {},
}

// isHTMLResponse 报告响应是否可抽取（spec §7）：Content-Type 缺失时按 HTML 尝试；
// 声明了但非 text/html / application/xhtml+xml（含无法解析的声明，如 PDF/DOC/图片）→ 不可抽取。
func isHTMLResponse(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	_, ok := htmlMediaTypes[mediaType]
	return ok
}

// extractHrefs 提取页面所有 <a href> 的原始值（去首尾空白、丢弃空值与无 href 的锚点）。
// 解析/过滤交由 ResolveLinks 完成——发现层需要先拿到原始集合（spec §6.1 Links 语义）。
func extractHrefs(rawHTML []byte) []string {
	document, err := html.Parse(bytes.NewReader(rawHTML))
	if err != nil {
		return nil
	}

	var hrefs []string
	for _, anchor := range findElementsByTag(document, "a") {
		for _, attr := range anchor.Attr {
			if attr.Key != "href" {
				continue
			}
			if value := strings.TrimSpace(attr.Val); value != "" {
				hrefs = append(hrefs, value)
			}
			break
		}
	}
	return hrefs
}

// parseFetchTarget 校验并解析抓取目标：必须是绝对的无凭证 HTTP(S) URL。
func parseFetchTarget(raw string) (*url.URL, error) {
	parsed, ok := resolveToURL(nil, raw)
	if !ok {
		return nil, fmt.Errorf("抓取目标不是合法的绝对 HTTP(S) URL: %q", raw)
	}
	return parsed, nil
}

func finalURL(response *http.Response, fallback *url.URL) string {
	if response.Request != nil && response.Request.URL != nil {
		return response.Request.URL.String()
	}
	return fallback.String()
}

// drainAndClose 丢弃并关闭响应体，使连接可被复用。
func drainAndClose(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseDrainBytes))
	_ = response.Body.Close()
}

// sleepWithContext 是可被取消的睡眠。
func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
