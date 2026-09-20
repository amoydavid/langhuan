package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// firecrawl 引擎单测：httptest 假 provider（响应形状按 v2 API 文档结构模拟），
// 覆盖请求形状/头/响应解析/错误映射/目标预校验；不发真实网络请求。

// fakeFirecrawl 是 /v2/scrape 假 provider：记录请求并按用例返回。
type fakeFirecrawl struct {
	status      int  // HTTP 状态；0 表示 200
	success     bool // 响应体 success 字段
	markdown    string
	links       []string
	metadataURL string
	pageStatus  int // metadata.statusCode

	mu   sync.Mutex
	hits []firecrawlHit
}

type firecrawlHit struct {
	path     string
	method   string
	auth     string
	url      string
	formats  []string
	onlyMain bool
}

func (f *fakeFirecrawl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var payload firecrawlScrapeRequest
	_ = json.Unmarshal(body, &payload)

	f.mu.Lock()
	f.hits = append(f.hits, firecrawlHit{
		path: r.URL.Path, method: r.Method, auth: r.Header.Get("Authorization"),
		url: payload.URL, formats: payload.Formats, onlyMain: payload.OnlyMainContent,
	})
	f.mu.Unlock()

	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if f.status >= 400 {
		_, _ = w.Write([]byte(`{"error":"boom"}`))
		return
	}
	resp := map[string]any{"success": f.success, "data": map[string]any{
		"markdown": f.markdown,
		"links":    f.links,
		"metadata": map[string]any{"url": f.metadataURL, "statusCode": f.pageStatus},
	}}
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeFirecrawl) requestHits() []firecrawlHit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]firecrawlHit(nil), f.hits...)
}

// passthroughValidate 是引擎测试共用的目标校验注入（跳过真实 DNS/私网判定），
// render/jina/auto 测试同样复用。
func passthroughValidate(context.Context, string) error { return nil }

func newTestFirecrawl(t *testing.T, fake *fakeFirecrawl) (*FirecrawlFetcher, string) {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fetcher, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{
		APIKey:          "fc-key",
		Endpoint:        server.URL,
		OnlyMainContent: true,
		validateTarget:  passthroughValidate,
	})
	require.NoError(t, err)
	return fetcher, server.URL
}

func TestFirecrawlFetchRequestShapeAndResponse(t *testing.T) {
	t.Parallel()

	fake := &fakeFirecrawl{
		success:     true,
		markdown:    "# Page\n\ncontent",
		links:       []string{"https://example.com/a", "https://example.com/b"},
		metadataURL: "https://example.com/final",
		pageStatus:  200,
	}
	fetcher, _ := newTestFirecrawl(t, fake)

	page, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
	require.NoError(t, err)

	hits := fake.requestHits()
	require.Len(t, hits, 1)
	hit := hits[0]
	assert.Equal(t, "/v2/scrape", hit.path)
	assert.Equal(t, http.MethodPost, hit.method)
	assert.Equal(t, "Bearer fc-key", hit.auth, "Authorization: Bearer <api_key>")
	assert.Equal(t, "https://target.example.com/page", hit.url)
	assert.Equal(t, []string{"markdown", "links"}, hit.formats)
	assert.True(t, hit.onlyMain, "extract.mode=readability → onlyMainContent=true")

	assert.Equal(t, EngineFirecrawl, page.Engine)
	assert.Equal(t, 200, page.Status)
	assert.Equal(t, "https://example.com/final", page.FinalURL, "metadata.url 优先")
	assert.Equal(t, "# Page\n\ncontent", string(page.Markdown), "markdown 直通")
	assert.Equal(t, fake.links, page.Links)
	assert.Empty(t, page.HTML, "provider 通道不填 HTML")
}

func TestFirecrawlOnlyMainContentMapsExtractModeFull(t *testing.T) {
	t.Parallel()

	// extract.mode=full → onlyMainContent=false（构造侧映射）。
	fetcher, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{
		APIKey: "fc-key", Endpoint: "http://unused.test",
		OnlyMainContent: false, validateTarget: passthroughValidate,
	})
	require.NoError(t, err)
	assert.False(t, fetcher.onlyMainContent)
}

func TestFirecrawlTargetStatusEquivalentSemantics(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		pageStatus  int
		wantOutcome PageOutcome
	}{
		{name: "目标 404 → gone", pageStatus: 404, wantOutcome: OutcomeGone},
		{name: "目标 410 → gone", pageStatus: 410, wantOutcome: OutcomeGone},
		{name: "目标 401 → blocked", pageStatus: 401, wantOutcome: OutcomeBlocked},
		{name: "目标 403 → blocked", pageStatus: 403, wantOutcome: OutcomeBlocked},
		{name: "目标 429 → retryable", pageStatus: 429, wantOutcome: OutcomeRetryable},
		{name: "目标 500 → retryable", pageStatus: 500, wantOutcome: OutcomeRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeFirecrawl{success: true, markdown: "x", pageStatus: tc.pageStatus}
			fetcher, _ := newTestFirecrawl(t, fake)

			_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
			require.Error(t, err)
			var pageErr *PageError
			require.ErrorAs(t, err, &pageErr)
			assert.Equal(t, tc.wantOutcome, pageErr.Outcome, "metadata.statusCode 经 ClassifyStatus 等价映射")
		})
	}
}

func TestFirecrawlProviderStatusMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		providerStatus int
		wantOutcome    PageOutcome
	}{
		{name: "api key 无效 401 → blocked（非 retryable）", providerStatus: 401, wantOutcome: OutcomeBlocked},
		{name: "403 → blocked", providerStatus: 403, wantOutcome: OutcomeBlocked},
		{name: "配额 429 → retryable", providerStatus: 429, wantOutcome: OutcomeRetryable},
		{name: "5xx → retryable", providerStatus: 503, wantOutcome: OutcomeRetryable},
		{name: "404（provider 路由错误）→ gone 语义", providerStatus: 404, wantOutcome: OutcomeGone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeFirecrawl{status: tc.providerStatus, success: true}
			fetcher, _ := newTestFirecrawl(t, fake)

			_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
			require.Error(t, err)
			var pageErr *PageError
			require.ErrorAs(t, err, &pageErr)
			assert.Equal(t, tc.providerStatus, pageErr.Status)
			assert.Equal(t, tc.wantOutcome, pageErr.Outcome, "provider 非 2xx → ClassifyStatus 等价语义")
		})
	}
}

func TestFirecrawlScrapeTaskFailureIsRetryable(t *testing.T) {
	t.Parallel()

	// 2xx 但 success=false：provider 瞬时故障，走 Job 级重试。
	fake := &fakeFirecrawl{success: false}
	fetcher, _ := newTestFirecrawl(t, fake)

	_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
	require.Error(t, err)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err))
}

func TestFirecrawlMarkdownOversize(t *testing.T) {
	t.Parallel()

	fake := &fakeFirecrawl{success: true, markdown: "0123456789012345678901234567890123456789"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fetcher, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{
		APIKey: "fc-key", Endpoint: server.URL, MaxContentBytes: 16,
		validateTarget: passthroughValidate,
	})
	require.NoError(t, err)

	_, err = fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeOversize, pageErr.Outcome)
	assert.ErrorIs(t, err, sourceport.ErrSourceContentTooLarge)
}

func TestFirecrawlRejectsPrivateTargetBeforeSending(t *testing.T) {
	t.Parallel()

	fake := &fakeFirecrawl{success: true, markdown: "x"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	// 默认校验器（真实 ValidateWebTarget）：私网目标在发送前拒绝。
	fetcher, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{APIKey: "k", Endpoint: server.URL})
	require.NoError(t, err)

	_, err = fetcher.Fetch(t.Context(), FetchRequest{URL: "http://127.0.0.1/secret"})
	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeSkipped, pageErr.Outcome)
	assert.Empty(t, fake.requestHits(), "私网目标不得发往 provider")
}

func TestNewFirecrawlFetcherRequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}
