package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// jina 引擎单测：httptest 假 reader（响应形状按 r.jina.ai 文本响应约定模拟），
// 覆盖请求形状/头组/响应解析（markdown 直通 + links summary 解析）/错误映射。

// fakeJina 是 reader 假端点：记录请求并按用例返回。
type fakeJina struct {
	mu     sync.Mutex
	status int // HTTP 状态；0 表示 200
	body   string

	hits []jinaHit
}

type jinaHit struct {
	path             string
	auth             string
	engine           string
	respondWith      string
	withLinksSummary string
}

func (f *fakeJina) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, jinaHit{
		path: r.URL.Path, auth: r.Header.Get("Authorization"),
		engine: r.Header.Get("X-Engine"), respondWith: r.Header.Get("X-Respond-With"),
		withLinksSummary: r.Header.Get("X-With-Links-Summary"),
	})
	f.mu.Unlock()
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(f.body))
}

func (f *fakeJina) requestHits() []jinaHit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jinaHit(nil), f.hits...)
}

func newTestJina(t *testing.T, fake *fakeJina) *JinaFetcher {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fetcher, err := NewJinaFetcher(JinaFetcherConfig{
		APIKey: "jina-key", Endpoint: server.URL, validateTarget: passthroughValidate,
	})
	require.NoError(t, err)
	return fetcher
}

// jinaReaderResponse 按约定拼装 reader 文本响应（头部元数据 + 正文 + 链接摘要）。
func jinaReaderResponse(title, sourceURL, markdown string, links []string) string {
	builder := &strings.Builder{}
	builder.WriteString("Title: " + title + "\n")
	builder.WriteString("URL Source: " + sourceURL + "\n")
	builder.WriteString("Markdown Content:\n")
	builder.WriteString(markdown)
	if len(links) > 0 {
		builder.WriteString("\n\nLinks/Buttons:\n")
		for _, link := range links {
			builder.WriteString("- [label](" + link + ")\n")
		}
	}
	return builder.String()
}

func TestJinaFetchRequestShapeAndResponseParse(t *testing.T) {
	t.Parallel()

	body := jinaReaderResponse("Example",
		"https://target.example.com/page",
		"# Example\n\nparagraph with **bold**.",
		[]string{"https://target.example.com/a", "https://target.example.com/b"})
	fake := &fakeJina{body: body}
	fetcher := newTestJina(t, fake)

	page, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
	require.NoError(t, err)

	hits := fake.requestHits()
	require.Len(t, hits, 1)
	hit := hits[0]
	assert.Equal(t, "/https://target.example.com/page", hit.path, "目标 URL 作为 reader path 透传")
	assert.Equal(t, "Bearer jina-key", hit.auth)
	assert.Equal(t, "auto", hit.engine)
	assert.Equal(t, "markdown", hit.respondWith)
	assert.Equal(t, "all", hit.withLinksSummary)

	assert.Equal(t, EngineJina, page.Engine)
	assert.Equal(t, 200, page.Status)
	assert.Equal(t, "https://target.example.com/page", page.FinalURL, "URL Source 即最终地址")
	assert.Equal(t, "# Example\n\nparagraph with **bold**.", string(page.Markdown),
		"markdown 直通，剥离头部元数据与链接摘要节")
	assert.Equal(t, []string{"https://target.example.com/a", "https://target.example.com/b"}, page.Links,
		"links summary 解析为出链集合")
	assert.Empty(t, page.HTML)
	assert.NotContains(t, string(page.Markdown), "Links/Buttons:", "链接摘要不混入正文")
}

func TestJinaPlainBodyWithoutMarkers(t *testing.T) {
	t.Parallel()

	// 无约定标记时按整体 markdown 兜底（provider 形状演进不致命）。
	fake := &fakeJina{body: "# Just markdown\n\ntext"}
	fetcher := newTestJina(t, fake)

	page, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/plain"})
	require.NoError(t, err)
	assert.Equal(t, "# Just markdown\n\ntext", string(page.Markdown))
	assert.Empty(t, page.Links)
	assert.Equal(t, "https://target.example.com/plain", page.FinalURL, "无 URL Source 时回退请求 URL")
}

func TestJinaProviderStatusMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		status      int
		wantOutcome PageOutcome
	}{
		{name: "401 → blocked（配额/鉴权，非 retryable）", status: 401, wantOutcome: OutcomeBlocked},
		{name: "403 → blocked", status: 403, wantOutcome: OutcomeBlocked},
		{name: "429 → retryable", status: 429, wantOutcome: OutcomeRetryable},
		{name: "5xx → retryable", status: 500, wantOutcome: OutcomeRetryable},
		{name: "451 → skipped", status: 451, wantOutcome: OutcomeSkipped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeJina{status: tc.status, body: "denied"}
			fetcher := newTestJina(t, fake)

			_, err := fetcher.Fetch(t.Context(), FetchRequest{URL: "https://target.example.com/page"})
			require.Error(t, err)
			var pageErr *PageError
			require.ErrorAs(t, err, &pageErr)
			assert.Equal(t, tc.status, pageErr.Status)
			assert.Equal(t, tc.wantOutcome, pageErr.Outcome, "非 2xx → ClassifyStatus 等价语义")
		})
	}
}

func TestJinaMarkdownOversize(t *testing.T) {
	t.Parallel()

	fake := &fakeJina{body: strings.Repeat("0123456789", 4)}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fetcher, err := NewJinaFetcher(JinaFetcherConfig{
		APIKey: "jina-key", Endpoint: server.URL, MaxContentBytes: 16,
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

func TestJinaRejectsPrivateTargetBeforeSending(t *testing.T) {
	t.Parallel()

	fake := &fakeJina{body: "x"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	// 默认校验器（真实 ValidateWebTarget）：私网目标在发送前拒绝。
	fetcher, err := NewJinaFetcher(JinaFetcherConfig{APIKey: "jina-key", Endpoint: server.URL})
	require.NoError(t, err)

	_, err = fetcher.Fetch(t.Context(), FetchRequest{URL: "http://127.0.0.1/secret"})
	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeSkipped, pageErr.Outcome)
	assert.Empty(t, fake.requestHits(), "私网目标不得发往 provider")
}

func TestNewJinaFetcherRequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewJinaFetcher(JinaFetcherConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}
