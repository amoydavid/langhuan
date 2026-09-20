package web

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// auto 引擎单测（spec §6.2）：升级触发两条件、无处可去 warning、last_engine 短路
// 与失败回落。会话级用例直接构造 runSession（白盒），连接器级用例经
// advancedEngineOverrides 注入 fake 高级引擎走完整 Fetch 路径。

// richHTML 是抽取后字符数充足的静态页（不满足任何升级条件）。
func richHTML(marker string) string {
	return `<html><head><title>Doc</title></head><body><p>` + marker + ` ` +
		strings.Repeat("content ", 40) + `</p></body></html>`
}

// shellHTML 是 JS 空壳页：空挂载点 root + 无可见正文。
const shellHTML = `<html><head><title>App</title></head>` +
	`<body><div id="root"></div><script src="/app.js"></script></body></html>`

// engineSpy 记录请求并可编程返回的引擎 fake。
type engineSpy struct {
	mu       sync.Mutex
	requests []FetchRequest
	page     fetchedPage
	err      error
}

func (s *engineSpy) fetch(_ context.Context, request FetchRequest) (fetchedPage, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	page, err := s.page, s.err
	s.mu.Unlock()
	return page, err
}

func (s *engineSpy) calls() []FetchRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]FetchRequest(nil), s.requests...)
}

// autoCfg 构造 auto 引擎的 typed 源配置（CharThreshold=1 隔离 readability 回落噪声）。
func autoCfg(enabled bool, minTextChars int) value.WebSourceConfig {
	return value.WebSourceConfig{
		Mode: value.WebSourceModeURLs,
		Fetch: value.WebSourceFetchConfig{
			Engine: value.WebSourceFetchEngineAuto,
			AutoEscalate: value.WebSourceAutoEscalateConfig{
				Enabled:      enabled,
				MinTextChars: minTextChars,
			},
		},
		Extract: value.WebSourceExtractConfig{
			Mode:           value.WebSourceExtractModeReadability,
			FallbackToFull: true,
			Readability:    value.WebSourceReadabilityConfig{CharThreshold: 1},
		},
	}
}

func autoTestSession(cfg value.WebSourceConfig, httpEngine, escalate engineFunc) *runSession {
	return &runSession{
		cfg:       cfg,
		userAgent: "test-agent",
		cache:     NewPageCache(10),
		httpFetch: httpEngine,
		escalate:  escalate,
	}
}

func TestAutoStaysOnHTTPForRichPages(t *testing.T) {
	t.Parallel()

	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/a", Status: 200,
		HTML: []byte(richHTML("static-marker")), Engine: EngineHTTP,
	}}
	escalate := &engineSpy{}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{URL: "https://example.com/a"})
	require.NoError(t, err)
	assert.Equal(t, EngineHTTP, page.Engine)
	assert.Empty(t, escalate.calls(), "静态页不升级")
	assert.NotNil(t, page.extracted, "升级判定的抽取产物应缓存在页上，组装阶段不二次抽取")
	assert.Empty(t, page.Warnings)
}

func TestAutoEscalatesOnMinTextChars(t *testing.T) {
	t.Parallel()

	// 正文 2 字符 < min_text_chars=200 → 升级。
	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/thin", Status: 200,
		HTML: []byte("<html><body><p>hi</p></body></html>"), Engine: EngineHTTP,
	}}
	escalate := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/thin", Status: 200,
		Markdown: []byte("provider markdown"), Engine: EngineJina,
	}}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{URL: "https://example.com/thin"})
	require.NoError(t, err)
	require.Len(t, escalate.calls(), 1, "min_text_chars 命中触发升级重抓")
	assert.Equal(t, "https://example.com/thin", escalate.calls()[0].URL)
	assert.Equal(t, EngineJina, page.Engine, "返回升级后的页面")
	assert.Equal(t, "provider markdown", string(page.Markdown))
	assert.Empty(t, page.Warnings)
}

func TestAutoEscalatesOnJSShellEvenWhenMinTextCharsPasses(t *testing.T) {
	t.Parallel()

	// MinTextChars=1：抽取 0 字符其实也命中字符数条件，这里用空 body 页同时覆盖
	// jshell 启发式的挂载点判定路径（isLikelyJSShell 的独立行为已在 jshell_test）。
	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/app", Status: 200,
		HTML: []byte(shellHTML), Engine: EngineHTTP,
	}}
	escalate := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/app", Status: 200,
		Markdown: []byte("rendered"), Engine: EngineRender,
	}}
	session := autoTestSession(autoCfg(true, 1), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{URL: "https://example.com/app"})
	require.NoError(t, err)
	require.Len(t, escalate.calls(), 1)
	assert.Equal(t, EngineRender, page.Engine)
}

func TestAutoAcceptsWithWarningWhenNowhereToGo(t *testing.T) {
	t.Parallel()

	// escalate=nil（运行时连接缺失）：接受 http 结果 + warning possible_js_shell。
	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/app", Status: 200,
		HTML: []byte(shellHTML), Engine: EngineHTTP,
	}}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, nil)

	page, err := session.fetchEngine(t.Context(), FetchRequest{URL: "https://example.com/app"})
	require.NoError(t, err)
	assert.Equal(t, EngineHTTP, page.Engine)
	assert.Equal(t, []string{WarningPossibleJSShell}, page.Warnings)
	assert.NotNil(t, page.extracted, "接受原结果时同样缓存抽取产物")
}

func TestAutoShortCircuitUsesLastEngine(t *testing.T) {
	t.Parallel()

	httpEngine := &engineSpy{}
	escalate := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/app", Status: 200,
		Markdown: []byte("short-circuit markdown"), Engine: EngineFirecrawl,
	}}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{
		URL: "https://example.com/app", LastEngine: EngineFirecrawl,
	})
	require.NoError(t, err)
	assert.Empty(t, httpEngine.calls(), "last_engine 命中高级引擎时 http 起步被短路")
	require.Len(t, escalate.calls(), 1)
	assert.Equal(t, EngineFirecrawl, page.Engine)
}

func TestAutoShortCircuitFallsBackAndStripsConditionalHeaders(t *testing.T) {
	t.Parallel()

	// 短路引擎失败 → 回落 auto 判定：http 起步剥离条件头（validator 属 http 通道
	// 语义，304 会把高级引擎内容误判为未变更）；内容充足时接受 http 结果。
	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/a", Status: 200,
		HTML: []byte(richHTML("fallback-marker")), Engine: EngineHTTP,
	}}
	escalate := &engineSpy{err: &PageError{Outcome: OutcomeRetryable, Err: errors.New("render down")}}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{
		URL: "https://example.com/a", LastEngine: EngineRender,
		IfNoneMatch: `"v1"`, IfModifiedSince: "Tue, 01 Sep 2026 00:00:00 GMT",
	})
	require.NoError(t, err)
	require.Len(t, escalate.calls(), 1, "短路失败先于 http 起步发生")
	assert.Equal(t, EngineHTTP, page.Engine, "http 结果充足时被接受")

	httpCalls := httpEngine.calls()
	require.Len(t, httpCalls, 1)
	assert.Empty(t, httpCalls[0].IfNoneMatch, "回落抓取必须剥离 If-None-Match")
	assert.Empty(t, httpCalls[0].IfModifiedSince, "回落抓取必须剥离 If-Modified-Since")
}

func TestAutoDisabledStopsShortCircuitAndEscalation(t *testing.T) {
	t.Parallel()

	// enabled=false：auto 退化为纯 http——短路（沿用高级引擎）一并停用，条件头保留。
	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/a", Status: 200,
		HTML: []byte(shellHTML), Engine: EngineHTTP,
	}}
	escalate := &engineSpy{}
	session := autoTestSession(autoCfg(false, 200), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{
		URL: "https://example.com/a", LastEngine: EngineRender, IfNoneMatch: `"v1"`,
	})
	require.NoError(t, err)
	assert.Equal(t, EngineHTTP, page.Engine)
	assert.Empty(t, escalate.calls(), "enabled=false 不升级")
	assert.Empty(t, page.Warnings, "enabled=false 不产生 possible_js_shell（升级语义整体关闭）")
	httpCalls := httpEngine.calls()
	require.Len(t, httpCalls, 1)
	assert.Equal(t, `"v1"`, httpCalls[0].IfNoneMatch, "非短路路径条件头原样透传")
}

func TestAutoEscalationFailurePropagatesClassification(t *testing.T) {
	t.Parallel()

	httpEngine := &engineSpy{page: fetchedPage{
		FinalURL: "https://example.com/app", Status: 200,
		HTML: []byte(shellHTML), Engine: EngineHTTP,
	}}
	escalate := &engineSpy{err: &PageError{Status: 503, Outcome: OutcomeRetryable, Err: errors.New("provider 5xx")}}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, escalate.fetch)

	_, err := session.fetchEngine(t.Context(), FetchRequest{URL: "https://example.com/app"})
	require.Error(t, err)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err), "升级失败按该引擎分类透传，Job 级重试兜底")
}

func TestAutoNotModifiedIsReturnedUntouched(t *testing.T) {
	t.Parallel()

	httpEngine := &engineSpy{page: fetchedPage{Status: 304, NotModified: true, Engine: EngineHTTP}}
	escalate := &engineSpy{}
	session := autoTestSession(autoCfg(true, 200), httpEngine.fetch, escalate.fetch)

	page, err := session.fetchEngine(t.Context(), FetchRequest{URL: "https://example.com/a", LastEngine: ""})
	require.NoError(t, err)
	assert.True(t, page.NotModified)
	assert.Nil(t, page.extracted)
	assert.Empty(t, escalate.calls(), "304 无内容可判定，不升级")
}

// —— 连接器级（真实装配路径：__credentials → provider → 引擎） ——

// renderOverrideConnector 构造注入 fake render 引擎的 connector。
func renderOverrideConnector(t *testing.T, site *fakeSite, renderCalls *int, marker string) (*Connector, string) {
	t.Helper()
	override := func(_ context.Context, request FetchRequest) (fetchedPage, error) {
		*renderCalls++
		return fetchedPage{
			FinalURL: request.URL, Status: 200,
			HTML:   []byte(`<html><head><title>Rendered</title></head><body><p>` + marker + `</p></body></html>`),
			Engine: EngineRender,
		}, nil
	}
	connector, baseURL := newTestConnector(t, site)
	connector.advancedEngineOverrides = map[value.SourceProvider]engineFunc{
		value.SourceProviderBrowser: override,
	}
	return connector, baseURL
}

func autoConnectorConfig(baseURL, connectionID string) map[string]any {
	return map[string]any{
		"mode":  "urls",
		"urls":  []string{baseURL + "/app"},
		"crawl": map[string]any{"respect_robots": false},
		"fetch": map[string]any{"engine": "auto", "connection_id": connectionID},
	}
}

func TestFetchAutoEscalatesShellPageViaConnectionEngine(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/app": {body: shellHTML},
	})
	renderCalls := 0
	connector, baseURL := renderOverrideConnector(t, site, &renderCalls, "rendered-content-marker")

	// __credentials 形状与编排层 webSourceConfigWithCredentials 注入一致。
	config := autoConnectorConfig(baseURL, "0b5a4d38-2c5f-4a5e-9a2b-8f1c3d4e5f60")
	config["__credentials"] = map[string]any{
		"provider":     "browser",
		"token":        "cdp-token",
		"endpoint_url": "ws://cdp.test",
	}

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/app",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.Equal(t, 1, site.hits("/app"), "http 起步抓取一次")
	assert.Equal(t, 1, renderCalls, "空壳页触发一次升级重抓")
	assert.Contains(t, string(document.Markdown), "rendered-content-marker", "升级结果进入文档")
	assert.Equal(t, "render", document.Meta[sourceport.SourceMetaLastEngine],
		"升级重抓留痕实际引擎")
	assert.NotContains(t, document.Warnings, WarningPossibleJSShell)
}

func TestFetchAutoAcceptsShellPageWithWarningWithoutCredentials(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/app": {body: shellHTML},
	})
	connector, baseURL := newTestConnector(t, site)

	// 有 connection_id 但凭证未注入（连接被删/停用的运行时防御路径）。
	config := autoConnectorConfig(baseURL, "0b5a4d38-2c5f-4a5e-9a2b-8f1c3d4e5f60")

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/app",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.Equal(t, "http", document.Meta[sourceport.SourceMetaLastEngine])
	assert.Contains(t, document.Warnings, WarningPossibleJSShell,
		"升级无处可去时接受原结果并透出 warning")
	assert.Empty(t, document.Markdown, "接受 http 结果：空壳页无可抽取正文（这正是需要升级的形态）")
}

func TestFetchAutoShortCircuitSkipsHTTPViaLastEngine(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/app": {body: richHTML("http-marker")},
	})
	renderCalls := 0
	connector, baseURL := renderOverrideConnector(t, site, &renderCalls, "short-circuit-marker")

	config := autoConnectorConfig(baseURL, "0b5a4d38-2c5f-4a5e-9a2b-8f1c3d4e5f60")
	config["__credentials"] = map[string]any{"provider": "browser", "endpoint_url": "ws://cdp.test"}

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/app",
		sourceport.FetchOptions{SourceConfig: config, LastEngine: EngineRender})
	require.NoError(t, err)
	assert.Zero(t, site.hits("/app"), "last_engine 短路不发起 http 起步抓取")
	assert.Equal(t, 1, renderCalls)
	assert.Contains(t, string(document.Markdown), "short-circuit-marker")
}

func TestFetchExplicitFirecrawlEngineViaCredentials(t *testing.T) {
	t.Parallel()

	// 显式 firecrawl 引擎 + 真实 FirecrawlFetcher 构造路径：目标用公网字面量
	//（真实 ValidateWebTarget 无需 DNS），provider 为 httptest 假端点。
	fake := &fakeFirecrawl{success: true, markdown: "# firewalled\n\nprovider markdown"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	site := newFakeSite(map[string]fakeWebPage{})

	connector, _ := newTestConnector(t, site)
	config := map[string]any{
		"mode":  "urls",
		"urls":  []string{"https://93.184.216.34/docs"},
		"crawl": map[string]any{"respect_robots": false},
		"fetch": map[string]any{"engine": "firecrawl", "connection_id": "0b5a4d38-2c5f-4a5e-9a2b-8f1c3d4e5f60"},
		"__credentials": map[string]any{
			"provider":     "firecrawl",
			"api_key":      "fc-secret",
			"endpoint_url": server.URL,
		},
	}

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, "https://93.184.216.34/docs",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.Equal(t, 1, len(fake.requestHits()), "provider 收到一次 scrape 请求")
	assert.Equal(t, "Bearer fc-secret", fake.requestHits()[0].auth)
	assert.Equal(t, "# firewalled\n\nprovider markdown", string(document.Markdown))
	assert.Equal(t, "firecrawl", document.Meta[sourceport.SourceMetaLastEngine])
	assert.Equal(t, "https://93.184.216.34/docs", document.Meta[sourceport.SourceMetaFinalURL])
}
