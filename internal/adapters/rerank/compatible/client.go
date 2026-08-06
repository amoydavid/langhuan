package compatible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	rerankadapter "github.com/dajee/langhuan/internal/adapters/rerank"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// maxResponseBody 限制远端响应体大小，避免压缩炸弹式无限读取。
const maxResponseBody = 2 << 20 // 2 MiB

// retryBackoffs 是固定指数退避序列（毫秒），服从 context 取消。
var retryBackoffs = []time.Duration{
	100 * time.Millisecond,
	200 * time.Millisecond,
	400 * time.Millisecond,
}

type client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	provider   string
	modelName  string
	retryTimes int
	parameters ModelParameters
}

type requestBody struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n"`
	ReturnDocuments bool     `json:"return_documents"`
}

type responseBody struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

func (c *client) Rerank(ctx context.Context, input rerankport.RerankInput) (*rerankport.RerankResult, error) {
	if err := validateInput(input, c.parameters); err != nil {
		return nil, err
	}
	body := requestBody{
		Model:           c.modelName,
		Query:           truncateQuery(input.Query, c.parameters.MaxQueryChars),
		Documents:       buildDocuments(input.Documents, c.parameters.MaxDocumentChars),
		TopN:            input.TopN,
		ReturnDocuments: false,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("编码重排请求失败: %w", err)
	}

	maxAttempts := c.retryTimes + 1
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if !sleepWithContext(ctx, retryBackoffs[attempt-1]) {
				return nil, ctx.Err()
			}
		}
		result, retryable, err := c.doOnce(ctx, payload, input.Documents)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable {
			break
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("重排请求失败")
	}
	return nil, sanitizeLastError(c.provider, lastErr)
}

// sanitizeLastError 仅清洗上游协议/网络错误。已经是领域哨兵错误或 context
// 取消的错误不再二次包装，保持 errors.Is 判定的精确性。
func sanitizeLastError(provider string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, sentinel := range rerankDomainSentinels {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	if strings.TrimSpace(provider) == "" {
		provider = providerKey
	}
	return rerankadapter.SanitizeProviderError(provider, err)
}

// rerankDomainSentinels 列出已经在 client 内部确定化的领域哨兵错误，
// 它们不需要再次经过上游错误清洗。
var rerankDomainSentinels = []error{
	domainerrors.ErrInvalidRerankResponse,
	domainerrors.ErrRerankInputTooLarge,
}

// doOnce 执行一次远端调用。返回的 retryable 标识瞬时错误是否值得重试。
func (c *client) doOnce(ctx context.Context, payload []byte, documents []rerankport.Document) (*rerankport.RerankResult, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, false, fmt.Errorf("构造重排请求失败: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.apiKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		// 网络错误 / context 超时通常可重试。
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, true, httpStatusError{status: response.StatusCode}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, httpStatusError{status: response.StatusCode}
	}

	limitReader := io.LimitReader(response.Body, maxResponseBody+1)
	raw, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, true, fmt.Errorf("读取重排响应失败: %w", err)
	}
	if len(raw) > maxResponseBody {
		return nil, false, fmt.Errorf("%w: 响应体超过 %d 字节", domainerrors.ErrInvalidRerankResponse, maxResponseBody)
	}

	var decoded responseBody
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false, fmt.Errorf("%w: 响应 JSON 无效", domainerrors.ErrInvalidRerankResponse)
	}
	items, err := mapResults(decoded.Results, documents)
	if err != nil {
		return nil, false, err
	}
	return &rerankport.RerankResult{Items: items}, false, nil
}

type httpStatusError struct {
	status int
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("rerank upstream status %d", e.status)
}

func (e httpStatusError) StatusCode() int { return e.status }

// mapResults 把上游按位置返回的 index 还原成 application 生成的 opaque DocumentID。
func mapResults(results []struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}, documents []rerankport.Document) ([]rerankport.RerankItem, error) {
	if len(results) != len(documents) {
		return nil, fmt.Errorf("%w: 返回结果数量 %d 与输入 %d 不一致", domainerrors.ErrInvalidRerankResponse, len(results), len(documents))
	}
	seen := make(map[int]struct{}, len(results))
	items := make([]rerankport.RerankItem, 0, len(results))
	for _, result := range results {
		if _, duplicate := seen[result.Index]; duplicate {
			return nil, fmt.Errorf("%w: 重复 index %d", domainerrors.ErrInvalidRerankResponse, result.Index)
		}
		if result.Index < 0 || result.Index >= len(documents) {
			return nil, fmt.Errorf("%w: index %d 越界", domainerrors.ErrInvalidRerankResponse, result.Index)
		}
		if math.IsNaN(result.RelevanceScore) || math.IsInf(result.RelevanceScore, 0) {
			return nil, fmt.Errorf("%w: relevance_score 非有限值", domainerrors.ErrInvalidRerankResponse)
		}
		seen[result.Index] = struct{}{}
		items = append(items, rerankport.RerankItem{
			DocumentID: documents[result.Index].ID,
			Score:      result.RelevanceScore,
		})
	}
	return items, nil
}

func validateInput(input rerankport.RerankInput, params ModelParameters) error {
	query := strings.TrimSpace(input.Query)
	queryRunes := len([]rune(query))
	if queryRunes < 1 || queryRunes > params.MaxQueryChars {
		return fmt.Errorf("%w: query 长度非法", domainerrors.ErrRerankInputTooLarge)
	}
	if len(input.Documents) < 1 || len(input.Documents) > params.MaxDocuments {
		return fmt.Errorf("%w: documents 数量 %d 非法", domainerrors.ErrRerankInputTooLarge, len(input.Documents))
	}
	if input.TopN < 1 || input.TopN > len(input.Documents) {
		return fmt.Errorf("%w: top_n 非法", domainerrors.ErrRerankInputTooLarge)
	}
	return nil
}

func truncateQuery(query string, maxChars int) string {
	runes := []rune(query)
	if len(runes) <= maxChars {
		return query
	}
	return string(runes[:maxChars])
}

func buildDocuments(documents []rerankport.Document, maxChars int) []string {
	out := make([]string, 0, len(documents))
	for _, document := range documents {
		runes := []rune(document.Text)
		if len(runes) > maxChars {
			out = append(out, string(runes[:maxChars]))
		} else {
			out = append(out, document.Text)
		}
	}
	return out
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
