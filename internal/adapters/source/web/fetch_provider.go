package web

import (
	"fmt"
	"io"
	"net/http"
	"time"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// provider 通道（firecrawl/jina）共享行为：连接端点是管理员配置的连接地址
//（firecrawl 支持自托管 endpoint 覆盖，可位于私网），故请求走普通 client 而
// 非 SSRF web client；目标 URL 的公网预校验在各引擎发送前显式执行（spec §6.4，
// 降级语义：provider 基础设施内的后续请求不受本检查约束）。失败分类与 http
// 引擎同词汇：非 2xx → ClassifyStatus 等价语义（401/403 blocked、429/5xx
// retryable），由编排层统一消费。

const (
	// defaultProviderTimeout 是 provider 请求超时兜底（生产由
	// web_source.request_timeout_seconds 经 ConnectorConfig.HTTPClient.Timeout 推导）。
	defaultProviderTimeout = 30 * time.Second
	// providerResponseScale 是 provider 响应体读取上限相对 max_content_bytes 的倍数：
	// 响应 JSON 除 markdown 外还含 links/metadata，超限判定仍以解析后的 markdown
	// 精确执行（OutcomeOversize），此 cap 只防异常超大响应挤占内存。
	providerResponseScale = 2
	// providerResponseFloorBytes 是 max_content_bytes 极小（或未限）时的响应读取下限。
	providerResponseFloorBytes = int64(1) << 20
)

// providerHTTPClient 构造 provider 通道专用 client；timeout<=0 用兜底默认。
func providerHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultProviderTimeout
	}
	return &http.Client{Timeout: timeout}
}

// providerStatusError 把 provider 的非 2xx 响应映射为页面失败（ClassifyStatus
// 等价语义），供引擎层直接返回。
func providerStatusError(status int, err error) *PageError {
	return &PageError{Status: status, Outcome: ClassifyStatus(status), Err: err}
}

// readProviderBody 读取 provider 响应体（原始字节），施加上限：rawCap 为
// max_content_bytes 的 providerResponseScale 倍（下限 providerResponseFloorBytes）。
// 读取失败按 retryable 归类（网络/中断层错误，Job 级重试兜底）。
func readProviderBody(resp *http.Response, maxBytes int64) ([]byte, error) {
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseDrainBytes))
		_ = resp.Body.Close()
	}()

	rawCap := int64(providerResponseFloorBytes)
	if maxBytes > 0 {
		rawCap = maxBytes * providerResponseScale
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, rawCap+1))
	if err != nil {
		return nil, &PageError{
			Status:  resp.StatusCode,
			Outcome: OutcomeRetryable,
			Err:     fmt.Errorf("读取 provider 响应失败: %w", err),
		}
	}
	if int64(len(body)) > rawCap {
		return nil, &PageError{
			Status:  resp.StatusCode,
			Outcome: OutcomeOversize,
			Err: fmt.Errorf("%w: provider 响应超过读取上限 %d 字节",
				sourceport.ErrSourceContentTooLarge, rawCap),
		}
	}
	return body, nil
}

// checkMarkdownSize 对 provider 直出的 markdown 施加单页内容上限（与 http 引擎
// readBody 同一 OutcomeOversize 语义）。
func checkMarkdownSize(markdown []byte, status int, maxBytes int64) error {
	if maxBytes > 0 && int64(len(markdown)) > maxBytes {
		return &PageError{
			Status:  status,
			Outcome: OutcomeOversize,
			Err: fmt.Errorf("%w: 页面 markdown %d 字节超过上限 %d",
				sourceport.ErrSourceContentTooLarge, len(markdown), maxBytes),
		}
	}
	return nil
}
