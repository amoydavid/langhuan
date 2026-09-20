package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件覆盖 connector 的 ListTree 侧（三模式快照形状、目录投影、MaxEditTime、
// Complete 语义、非法 SourceConfig）；共享假站点与 connector 构造辅助也在此，
// Fetch 侧测试见 connector_fetch_test.go。

// fakeWebPage 描述假站点中一个 path 的响应。
type fakeWebPage struct {
	body        string
	contentType string // 缺省 text/html; charset=utf-8
	etag        string
	// ifNoneMatchValue 非空时：请求 If-None-Match 与之相等则返回 304。
	ifNoneMatchValue string
}

// fakeSite 是可断言抓取次数与透传头的 httptest 假站点（SSRF-safe client 会阻断
// 127.0.0.1，故测试注入普通 client——生产 DI 注入 NewPublicWebClient 的产物）。
type fakeSite struct {
	pages  map[string]fakeWebPage
	robots string // 非空时作为 /robots.txt 响应

	mu                  sync.Mutex
	counts              map[string]int
	lastIfNoneMatch     string
	lastIfModifiedSince string
}

func newFakeSite(pages map[string]fakeWebPage) *fakeSite {
	return &fakeSite{pages: pages, counts: map[string]int{}}
}

func (s *fakeSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.counts[r.URL.Path]++
	if ine := r.Header.Get("If-None-Match"); ine != "" {
		s.lastIfNoneMatch = ine
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		s.lastIfModifiedSince = ims
	}
	s.mu.Unlock()

	if r.URL.Path == "/robots.txt" && s.robots != "" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(s.robots))
		return
	}
	page, ok := s.pages[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if page.ifNoneMatchValue != "" && r.Header.Get("If-None-Match") == page.ifNoneMatchValue {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	contentType := page.contentType
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	if page.etag != "" {
		w.Header().Set("ETag", page.etag)
	}
	_, _ = w.Write([]byte(page.body))
}

// hits 返回某 path 被请求的次数（0 路径安全）。
func (s *fakeSite) hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[path]
}

// totalHits 返回全部路径的请求总数。
func (s *fakeSite) totalHits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, count := range s.counts {
		total += count
	}
	return total
}

// newTestConnector 起假站点并构造注入普通 client 的 web connector。
func newTestConnector(t *testing.T, site *fakeSite) (*Connector, string) {
	t.Helper()
	server := httptest.NewServer(site)
	t.Cleanup(server.Close)
	connector, err := NewConnector(ConnectorConfig{
		HTTPClient:      &http.Client{Timeout: 5 * time.Second},
		UserAgent:       "LanghuanBot/1.0 (+https://langhuan.test)",
		MaxContentBytes: 1 << 20,
		Limits:          value.WebSourceLimits{MaxPagesCeiling: 1000, MaxDepthCeiling: 10},
	})
	require.NoError(t, err)
	return connector, server.URL
}

func crawlSourceConfig(seedURL string, extra map[string]any) map[string]any {
	crawl := map[string]any{
		"seed_url":       seedURL,
		"max_depth":      1,
		"max_pages":      50,
		"respect_robots": false,
	}
	for key, val := range extra {
		crawl[key] = val
	}
	return map[string]any{"mode": "crawl", "crawl": crawl}
}

func sitemapSourceConfig(sitemapURL string) map[string]any {
	return map[string]any{
		"mode":    "sitemap",
		"sitemap": map[string]any{"url": sitemapURL},
		"crawl":   map[string]any{"respect_robots": false},
	}
}

func urlsSourceConfig(urls ...string) map[string]any {
	return map[string]any{"mode": "urls", "urls": urls}
}

// nodesByToken 把快照节点按 Token 建索引。
func nodesByToken(snapshot sourceport.TreeSnapshot) map[string]model.ExternalNode {
	indexed := make(map[string]model.ExternalNode, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		indexed[node.Token] = node
	}
	return indexed
}

func TestNewConnectorRequiresHTTPClient(t *testing.T) {
	t.Parallel()
	_, err := NewConnector(ConnectorConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF-safe")
}

func TestListTreeCrawlProjectsDirectoryTree(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/": {body: `<html><head><title>Home</title></head><body>` +
			`<a href="/help/a">a</a><a href="/help/c">c</a><a href="/help/sub/b">b</a>` +
			`</body></html>`},
		"/help/a":     {body: "<html><body>page a</body></html>"},
		"/help/c":     {body: "<html><body>page c</body></html>"},
		"/help/sub/b": {body: "<html><body>page b</body></html>"},
	})
	connector, baseURL := newTestConnector(t, site)
	config := crawlSourceConfig(baseURL+"/", nil)

	snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb, Token: baseURL + "/"},
		sourceport.ListOptions{SourceConfig: config})
	require.NoError(t, err)

	assert.True(t, snapshot.Complete)
	assert.Empty(t, snapshot.Warnings)
	assert.True(t, snapshot.MaxEditTime.IsZero(), "crawl 无 lastmod，EditTime 应为零值")

	nodes := nodesByToken(snapshot)
	require.Len(t, snapshot.Nodes, 6, "2 个目录 + 4 个页面")

	// 目录投影：help/、help/sub/ 逐级产生；help/ 只出现一次（a、c 两页共享）。
	folderHelp := nodes[baseURL+"/help/"]
	assert.Equal(t, model.ExternalNode{
		Token:       baseURL + "/help/",
		ParentToken: "",
		Title:       "help",
		ObjType:     ObjTypeFolder,
		HasDocument: false,
	}, folderHelp)

	assert.Equal(t, baseURL+"/help/sub/", nodes[baseURL+"/help/sub/b"].ParentToken,
		"页面挂在其直接所属目录")
	subFolder := nodes[baseURL+"/help/sub/"]
	assert.Equal(t, ObjTypeFolder, subFolder.ObjType)
	assert.False(t, subFolder.HasDocument)
	assert.Equal(t, baseURL+"/help/", subFolder.ParentToken, "嵌套目录逐级挂父")
	assert.Equal(t, "sub", subFolder.Title)

	// 页面：HasDocument=true、ParentToken 指向所属目录、标题先用 URL 末段。
	pageA := nodes[baseURL+"/help/a"]
	assert.Equal(t, ObjTypePage, pageA.ObjType)
	assert.True(t, pageA.HasDocument)
	assert.Equal(t, baseURL+"/help/", pageA.ParentToken)
	assert.Equal(t, "a", pageA.Title)
	assert.Equal(t, baseURL+"/help/", nodes[baseURL+"/help/c"].ParentToken,
		"同目录多页共享同一 folder，不产生重复节点")

	// 根页面直接挂根：根目录不产生 folder，ParentToken 为空。
	rootPage := nodes[baseURL+"/"]
	require.NotNil(t, rootPage)
	assert.Empty(t, rootPage.ParentToken)
	assert.Equal(t, "127.0.0.1", rootPage.Title, "根路径标题回退 hostname")
	assert.True(t, rootPage.HasDocument)

	// 目录先于页面（先序），供调用方按序建树。
	var folderSeen bool
	for _, node := range snapshot.Nodes {
		if node.Token == baseURL+"/help/sub/" {
			folderSeen = true
		}
		if node.Token == baseURL+"/help/sub/b" {
			assert.True(t, folderSeen, "父目录应先于其页面出现")
		}
	}
}

// F1 回归：目录索引页（尾斜杠 URL）的页面 Token 与目录投影产出的 folder Token
// 完全相同——冲突目录不得再产生 folder 节点，否则编排层会以同一 external_id
// 建 FileTreeNode，撞 uq_file_tree_nodes_kb_external 唯一索引。
func TestListTreeSkipsFolderConflictingWithIndexPage(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{
		"/help/": {body: `<html><body>` +
			`<a href="/help/getting-started">gs</a><a href="/help/sub/">sub</a>` +
			`</body></html>`},
		"/help/getting-started": {body: "<html><body>gs page</body></html>"},
		"/help/sub/":            {body: `<html><body><a href="/help/sub/x">x</a></body></html>`},
		"/help/sub/x":           {body: "<html><body>x page</body></html>"},
	})
	connector, baseURL := newTestConnector(t, site)
	config := crawlSourceConfig(baseURL+"/help/", map[string]any{"max_depth": 2})

	snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb, Token: baseURL + "/help/"},
		sourceport.ListOptions{SourceConfig: config})
	require.NoError(t, err)
	assert.True(t, snapshot.Complete)
	assert.Empty(t, snapshot.Warnings)

	// (1) 不存在与页面 Token 冲突的 folder 节点：本站点 help/ 与 help/sub/ 都是
	// 目录索引页，冲突目录被跳过后快照只剩 4 个页面节点、零 folder。
	nodes := nodesByToken(snapshot)
	require.Len(t, snapshot.Nodes, 4)
	for _, node := range snapshot.Nodes {
		assert.NotEqual(t, ObjTypeFolder, node.ObjType, "Token=%s", node.Token)
	}

	// (2) ParentToken 精确语义：仍为目录 Token，但解析到目录索引页的文件节点
	//（目录索引页即该目录的代表文档）；索引页自身（path 尾段即文档）按普通页挂上层。
	assert.Empty(t, nodes[baseURL+"/help/"].ParentToken, "索引页按普通页面挂根")
	assert.Equal(t, baseURL+"/help/", nodes[baseURL+"/help/getting-started"].ParentToken)
	assert.Equal(t, baseURL+"/help/", nodes[baseURL+"/help/sub/"].ParentToken)
	assert.Equal(t, baseURL+"/help/sub/", nodes[baseURL+"/help/sub/x"].ParentToken,
		"深层页面的 ParentToken 落到子目录索引页的文件节点")

	// (3) 所有 ParentToken 都指向快照中存在的节点（空串=根），且无重复 Token。
	seen := make(map[string]struct{}, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		_, duplicated := seen[node.Token]
		assert.False(t, duplicated, "快照存在重复 Token: %s", node.Token)
		seen[node.Token] = struct{}{}
		if node.ParentToken != "" {
			_, parentExists := nodes[node.ParentToken]
			assert.True(t, parentExists, "ParentToken %s 未解析到任何节点", node.ParentToken)
		}
	}
}

// 确定性父先序（评审缺陷 F1 后续）：发现顺序任意（urls 列表把子页排在父索引页/
// 祖先目录之前）时，快照仍必须保证父节点先于子节点——编排层按快照顺序逐个
// upsert folder / 建档，子页先到会因父定位失败而确定性 partial。
func TestListTreeEmitsParentsBeforeChildrenRegardlessOfDiscoveryOrder(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{})
	connector, baseURL := newTestConnector(t, site)
	// 刻意的逆序：深层子页 → 子目录索引页 → 浅层页 → 根页。
	config := urlsSourceConfig(
		baseURL+"/help/sub/x",
		baseURL+"/help/getting-started",
		baseURL+"/help/sub/",
		baseURL+"/help/",
		baseURL+"/",
	)

	snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb},
		sourceport.ListOptions{SourceConfig: config})
	require.NoError(t, err)

	// 不变量一：每个非空 ParentToken 的节点，其父在快照中先于它出现。
	position := make(map[string]int, len(snapshot.Nodes))
	for i, node := range snapshot.Nodes {
		position[node.Token] = i
		if node.ParentToken == "" {
			continue
		}
		parentPos, ok := position[node.ParentToken]
		require.True(t, ok, "ParentToken %s 未解析到任何节点", node.ParentToken)
		assert.Less(t, parentPos, i, "父节点 %s 应先于子节点 %s", node.ParentToken, node.Token)
	}

	// 不变量二：同一输入两次列举的顺序完全一致（编排层可复现）。
	repeat, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb},
		sourceport.ListOptions{SourceConfig: config})
	require.NoError(t, err)
	require.Len(t, repeat.Nodes, len(snapshot.Nodes))
	for i := range repeat.Nodes {
		assert.Equal(t, snapshot.Nodes[i].Token, repeat.Nodes[i].Token, "第 %d 个节点顺序不稳定", i)
	}

	// 逆序输入下的期望形态：根页(0 段) → help/(1 段) → 2 段组按 Token 字典序 → 3 段页。
	tokens := make([]string, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		tokens = append(tokens, node.Token)
	}
	assert.Equal(t, []string{
		baseURL + "/",
		baseURL + "/help/",
		baseURL + "/help/getting-started",
		baseURL + "/help/sub/",
		baseURL + "/help/sub/x",
	}, tokens, "按（path 段数升序, Token 字典序）确定性排序")
}

func TestListTreeSitemapLastmodAndProjection(t *testing.T) {
	t.Parallel()

	sitemapTemplate := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>` + "__BASE__/docs/guide" + `</loc><lastmod>2026-09-01T00:00:00Z</lastmod></url>
  <url><loc>` + "__BASE__/docs/api" + `</loc><lastmod>2026-09-10T12:00:00Z</lastmod></url>
  <url><loc>` + "__BASE__/old" + `</loc><lastmod>not-a-date</lastmod></url>
</urlset>`
	site := newFakeSite(map[string]fakeWebPage{})
	connector, baseURL := newTestConnector(t, site)
	site.pages["/sitemap.xml"] = fakeWebPage{
		body:        strings.ReplaceAll(sitemapTemplate, "__BASE__", baseURL),
		contentType: "application/xml; charset=utf-8",
	}

	snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb},
		sourceport.ListOptions{SourceConfig: sitemapSourceConfig(baseURL + "/sitemap.xml")})
	require.NoError(t, err)

	assert.True(t, snapshot.Complete)
	assert.Equal(t, []string{WarningInvalidLastmod}, snapshot.Warnings)
	assert.True(t, snapshot.MaxEditTime.Equal(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)),
		"MaxEditTime 应取 lastmod 最大值")

	nodes := nodesByToken(snapshot)
	require.Len(t, snapshot.Nodes, 4, "1 个目录 + 3 个页面")
	assert.Equal(t, baseURL+"/docs/", nodes[baseURL+"/docs/api"].ParentToken)
	assert.Equal(t, baseURL+"/docs/", nodes[baseURL+"/docs/guide"].ParentToken)
	assert.Empty(t, nodes[baseURL+"/old"].ParentToken, "根级页面直接挂根")
	assert.True(t, nodes[baseURL+"/old"].EditTime.IsZero(), "lastmod 解析失败时 EditTime 为零值")

	assert.Zero(t, site.totalHits()-site.hits("/sitemap.xml"),
		"sitemap 模式仅做发现，不抓取页面正文")
}

func TestListTreeURLsModePassesListThrough(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{})
	connector, baseURL := newTestConnector(t, site)
	config := urlsSourceConfig(baseURL+"/a", baseURL+"/help/b")

	snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{},
		model.SyncRoot{Kind: sourceport.SyncRootWeb},
		sourceport.ListOptions{SourceConfig: config})
	require.NoError(t, err)

	assert.True(t, snapshot.Complete, "urls 模式列表即事实源，Complete 恒 true")
	assert.Empty(t, snapshot.Warnings)
	assert.True(t, snapshot.MaxEditTime.IsZero())
	assert.Zero(t, site.totalHits(), "urls 模式 ListTree 不发起任何抓取")

	nodes := nodesByToken(snapshot)
	require.Len(t, snapshot.Nodes, 3, "1 个目录 + 2 个页面")
	assert.Equal(t, baseURL+"/help/", nodes[baseURL+"/help/b"].ParentToken)
	assert.True(t, nodes[baseURL+"/help/b"].HasDocument)
	assert.Empty(t, nodes[baseURL+"/a"].ParentToken)
}

func TestListTreeRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	site := newFakeSite(map[string]fakeWebPage{"/a": {body: "<html><body>a</body></html>"}})
	connector, baseURL := newTestConnector(t, site)

	cases := []struct {
		name    string
		root    model.SyncRoot
		config  map[string]any
		wantErr string
		// wantValidation 为 true 时额外断言错误链含 domainerrors.ErrValidation
		//（编排层据此判为配置性永久失败而非 retryable）。
		wantValidation bool
	}{
		{
			name:    "不支持的 sync root kind",
			root:    model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: "folder"},
			config:  urlsSourceConfig(baseURL + "/a"),
			wantErr: "不支持的 sync root kind",
		},
		{
			name:    "缺 mode",
			root:    model.SyncRoot{Kind: sourceport.SyncRootWeb},
			config:  map[string]any{"crawl": map[string]any{}},
			wantErr: "mode 必填",
		},
		{
			name:    "crawl 缺 seed_url",
			root:    model.SyncRoot{Kind: sourceport.SyncRootWeb},
			config:  map[string]any{"mode": "crawl", "crawl": map[string]any{"respect_robots": false}},
			wantErr: "seed_url",
		},
		{
			name:    "未知配置键",
			root:    model.SyncRoot{Kind: sourceport.SyncRootWeb},
			config:  map[string]any{"mode": "urls", "urls": []string{baseURL + "/a"}, "url": "typo"},
			wantErr: "未知键",
		},
		{
			name:    "max_pages 超过天花板",
			root:    model.SyncRoot{Kind: sourceport.SyncRootWeb},
			config:  crawlSourceConfig(baseURL+"/", map[string]any{"max_pages": 1001}),
			wantErr: "天花板",
		},
		{
			name: "显式高级引擎缺连接凭证（配置性永久失败）",
			root: model.SyncRoot{Kind: sourceport.SyncRootWeb},
			config: map[string]any{
				"mode":  "urls",
				"urls":  []string{baseURL + "/a"},
				"fetch": map[string]any{"engine": "render", "connection_id": "0b5a4d38-2c5f-4a5e-9a2b-8f1c3d4e5f60"},
			},
			wantErr:        "无法服务",
			wantValidation: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{}, tc.root,
				sourceport.ListOptions{SourceConfig: tc.config})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			if tc.wantValidation {
				assert.ErrorIs(t, err, domainerrors.ErrValidation,
					"未接入引擎应可判为配置性永久失败，避免 Job 重试循环")
			}
			assert.Empty(t, snapshot.Nodes, "致命错误不返回可应用快照")
		})
	}

	t.Run("__credentials 注入键被剥离后正常解析", func(t *testing.T) {
		t.Parallel()
		config := urlsSourceConfig(baseURL + "/a")
		config["__credentials"] = map[string]any{"api_key": "secret"}
		snapshot, err := connector.ListTree(t.Context(), model.SourceConnection{},
			model.SyncRoot{Kind: sourceport.SyncRootWeb},
			sourceport.ListOptions{SourceConfig: config})
		require.NoError(t, err)
		assert.Len(t, snapshot.Nodes, 1)
	})
}
