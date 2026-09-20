package web

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/model"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件覆盖 connector 的 Fetch 侧：运行页缓存命中（发现-抓取复用）、抽取产物与
// source_meta 组装、条件请求透传与 304、robots 门、skipped_non_html、内容上限。

func TestFetchReusesDiscoveryCache(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/": {etag: `"pg-root"`, body: `<html><head><title>Root Page</title></head>` +
			`<body><p>root-page-marker</p><a href="/help/a">a</a></body></html>`},
		"/help/a": {body: "<html><body>page a</body></html>"},
	})
	connector, baseURL := newTestConnector(t, site)
	config := crawlSourceConfig(baseURL+"/", nil)

	_, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb, Token: baseURL + "/"},
		sourceport.ListOptions{SourceConfig: config})
	require.NoError(t, err)
	require.Equal(t, 1, site.hits("/"))

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)

	assert.Equal(t, 1, site.hits("/"), "Fetch 应命中发现阶段的页缓存，不二次抓取")
	assert.Contains(t, string(document.Markdown), "root-page-marker")
	assert.Equal(t, "Root Page", document.Title)
	assert.Equal(t, ObjTypePage, document.ObjType)
	assert.False(t, document.NotModified)

	// Meta = SourceMeta.ToMap()（T5 编解码）：本次观测到的键全部在列。
	assert.Equal(t, EngineHTTP, document.Meta[sourceport.SourceMetaLastEngine])
	assert.Equal(t, `"pg-root"`, document.Meta[sourceport.SourceMetaETag])
	assert.Equal(t, 200, document.Meta[sourceport.SourceMetaLastHTTPStatus])
	assert.Equal(t, baseURL+"/", document.Meta[sourceport.SourceMetaFinalURL])
	fetchAt, ok := document.Meta[sourceport.SourceMetaLastFetchAt].(string)
	require.True(t, ok, "last_fetch_at 应为 RFC3339 字符串")
	_, err = time.Parse(time.RFC3339, fetchAt)
	require.NoError(t, err)

	// 深层页面同样命中缓存。
	_, err = connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/help/a",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.Equal(t, 1, site.hits("/help/a"))
}

func TestFetchConditionalRequestAndNotModified(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/a": {etag: `"v1"`, ifNoneMatchValue: `"v1"`,
			body: "<html><body>conditional-marker</body></html>"},
		"/b": {body: "<html><body>page b</body></html>"},
	})
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL+"/a", baseURL+"/b")

	// 304：条件头透传，返回空正文 + NotModified，Meta 只含本次观测到的键。
	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/a",
		sourceport.FetchOptions{SourceConfig: config, IfNoneMatch: `"v1"`})
	require.NoError(t, err)
	assert.True(t, document.NotModified)
	assert.Empty(t, document.Markdown)
	assert.Equal(t, 304, document.Meta[sourceport.SourceMetaLastHTTPStatus])
	assert.NotContains(t, document.Meta, sourceport.SourceMetaETag,
		"304 响应无 ETag，不得用空值覆盖既有凭据（SourceMeta.ToMap 零值省略）")
	require.Equal(t, 1, site.hits("/a"))
	site.mu.Lock()
	assert.Equal(t, `"v1"`, site.lastIfNoneMatch)
	site.mu.Unlock()

	// 304 结果不入缓存：不带条件头的再次 Fetch 走真实抓取并取得完整内容。
	document, err = connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/a",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.False(t, document.NotModified)
	assert.Contains(t, string(document.Markdown), "conditional-marker")
	assert.Equal(t, `"v1"`, document.Meta[sourceport.SourceMetaETag])
	assert.Equal(t, 2, site.hits("/a"))

	// If-Modified-Since 同样透传。
	_, err = connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/b",
		sourceport.FetchOptions{SourceConfig: config, IfModifiedSince: "Wed, 01 Sep 2026 00:00:00 GMT"})
	require.NoError(t, err)
	site.mu.Lock()
	assert.Equal(t, "Wed, 01 Sep 2026 00:00:00 GMT", site.lastIfModifiedSince)
	site.mu.Unlock()
}

func TestFetchSkippedNonHTMLIsClassifiedError(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/manual.pdf": {body: "%PDF-1.4 fake", contentType: "application/pdf"},
	})
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL + "/manual.pdf")

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/manual.pdf",
		sourceport.FetchOptions{SourceConfig: config})
	require.Error(t, err)
	assert.Empty(t, document.Markdown, "非 HTML 页不产出文档（与「跳过不进 Nodes」对齐）")

	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeSkippedNonHTML, pageErr.Outcome)
	assert.Equal(t, OutcomeSkippedNonHTML, ClassifyError(err), "编排层经 ClassifyError 归类：跳过 + warning，不算 retryable")
	assert.Equal(t, 1, site.hits("/manual.pdf"))
}

func TestFetchRobotsGateAppliesToFetchPath(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/private/x": {body: "<html><body>secret</body></html>"},
		"/open":      {body: "<html><body>open page</body></html>"},
	})
	site.robots = "User-agent: *\nDisallow: /private/\n"
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL+"/private/x", baseURL+"/open")
	config["crawl"] = map[string]any{"respect_robots": true}

	_, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/private/x",
		sourceport.FetchOptions{SourceConfig: config})
	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, OutcomeRobotsDisallowed, pageErr.Outcome)
	assert.Zero(t, site.hits("/private/x"), "被 robots 拒绝的页不发起真实抓取")

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/open",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.Contains(t, string(document.Markdown), "open page")
	assert.NotContains(t, document.Warnings, WarningRobotsUnavailable,
		"robots.txt 可用时不应产生不可用告警")
}

func TestFetchExtractWarningsFlowToDocument(t *testing.T) {
	t.Parallel()

	// 短正文低于缺省 char_threshold(500)：readability 回落整页并记 warning。
	// respect_robots=false 以隔离 robots 相关告警。
	site := newFakeSite(map[string]fakeWebPage{
		"/short": {body: `<html><head><title>Fallback Page</title></head><body><p>short</p></body></html>`},
	})
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL + "/short")
	config["crawl"] = map[string]any{"respect_robots": false}

	document, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/short",
		sourceport.FetchOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.Contains(t, string(document.Markdown), "short")
	assert.Equal(t, "Fallback Page", document.Title)
	assert.Equal(t, []string{WarningReadabilityFallback}, document.Warnings,
		"抽取告警经 FetchedDocument.Warnings 透出（编排层并入同步结果）")
}

func TestFetchContentTooLarge(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/big": {body: "<html><body>" + strings.Repeat("very-long-content ", 100) + "</body></html>"},
	})
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL + "/big")

	_, err := connector.Fetch(t.Context(), model.SourceConnection{}, baseURL+"/big",
		sourceport.FetchOptions{SourceConfig: config, MaxContentBytes: 32})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sourceport.ErrSourceContentTooLarge))
}

func TestFetchInvalidExternalID(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{"/a": {body: "<html><body>a</body></html>"}})
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL + "/a")

	cases := []struct {
		name       string
		externalID string
	}{
		{name: "空 external_id", externalID: ""},
		{name: "非绝对 URL", externalID: "/a"},
		{name: "带凭证的 URL", externalID: "https://user:pass@example.com/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := connector.Fetch(t.Context(), model.SourceConnection{}, tc.externalID,
				sourceport.FetchOptions{SourceConfig: config})
			require.ErrorIs(t, err, sourceport.ErrSourceNotFound)
		})
	}
}
