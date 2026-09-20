package web

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveXML 返回以 XML 响应的 handler（sitemap 文档假站点用）。
func serveXML(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(body))
	}
}

// sitemapIndexXMLAt 构造只含一个子项的 sitemapindex 文档。
func sitemapIndexXMLAt(loc string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
		`<sitemap><loc>` + loc + `</loc></sitemap>` +
		`</sitemapindex>`
}

// sitemapURLSetXML 构造含给定 <loc> 列表的 urlset 文档。
func sitemapURLSetXML(locs ...string) string {
	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`
	for _, loc := range locs {
		body += `<url><loc>` + loc + `</loc></url>`
	}
	return body + `</urlset>`
}

// discoverFromServer 启动假站点并对 sitemapPath 执行发现。
func discoverFromServer(t *testing.T, mux *http.ServeMux, sitemapPath string, opts DiscoveryOptions) (DiscoveryResult, error) {
	t.Helper()
	server, client := newTestSite(t, mux)
	return DiscoverSitemap(context.Background(), client, server.URL+sitemapPath, opts)
}

func TestDiscoverSitemapParsesURLSetWithLastmod(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", serveXML(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/docs/a</loc><lastmod>2026-09-01T12:30:00+08:00</lastmod></url>
  <url><loc>https://example.com/docs/b?id=1&amp;utm_source=x</loc><lastmod>2026-09-01</lastmod></url>
  <url><loc> https://example.com/docs/c </loc></url>
</urlset>`))

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Empty(t, result.Warnings)
	require.Len(t, result.Pages, 3)

	first := result.Pages[0]
	assert.Equal(t, "https://example.com/docs/a", first.Token)
	assert.Equal(t, "a", first.Title, "标题先用 URL 末段")
	assert.Equal(t, ObjTypePage, first.ObjType)
	assert.True(t, first.HasDocument)
	assert.True(t, first.EditTime.Equal(time.Date(2026, 9, 1, 4, 30, 0, 0, time.UTC)),
		"带时区偏移的 lastmod 必须解析为同一时刻")

	second := result.Pages[1]
	assert.Equal(t, "https://example.com/docs/b?id=1", second.Token, "跟踪参数剥离、业务参数保留")
	assert.Equal(t, "b", second.Title)
	assert.True(t, second.EditTime.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		"W3C 日期精度按 UTC 零点解析")

	third := result.Pages[2]
	assert.Equal(t, "https://example.com/docs/c", third.Token)
	assert.True(t, third.EditTime.IsZero(), "缺失 lastmod → 零值，无告警")
}

func TestDiscoverSitemapRecursesIntoSitemapIndex(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		// 子项 loc 用绝对地址；第二个子项刻意用相对地址，覆盖按所在文档 base 解析的宽容语义。
		base := "https://" + r.Host
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` +
			`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
			`<sitemap><loc>` + base + `/sitemaps/a.xml</loc></sitemap>` +
			`<sitemap><loc>/sitemaps/more.xml</loc></sitemap>` +
			`</sitemapindex>`))
	})
	mux.HandleFunc("/sitemaps/a.xml", serveXML(sitemapURLSetXML("https://example.com/a1", "https://example.com/a2")))
	mux.HandleFunc("/sitemaps/more.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sitemapIndexXMLAt("https://" + r.Host + "/sitemaps/b.xml")))
	})
	mux.HandleFunc("/sitemaps/b.xml", serveXML(sitemapURLSetXML("https://example.com/b1")))

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Empty(t, result.Warnings)

	tokens := make([]string, 0, len(result.Pages))
	for _, node := range result.Pages {
		tokens = append(tokens, node.Token)
	}
	assert.Equal(t, []string{"https://example.com/a1", "https://example.com/a2", "https://example.com/b1"}, tokens)
}

func TestDiscoverSitemapChildFailureMarksIncomplete(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` +
			`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
			`<sitemap><loc>` + base + `/sitemaps/ok.xml</loc></sitemap>` +
			`<sitemap><loc>` + base + `/sitemaps/broken.xml</loc></sitemap>` +
			`</sitemapindex>`))
	})
	mux.HandleFunc("/sitemaps/ok.xml", serveXML(sitemapURLSetXML("https://example.com/a1", "https://example.com/a2")))
	mux.HandleFunc("/sitemaps/broken.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err, "单个子 sitemap 失败不是整个 run 的 error")
	assert.False(t, result.Complete, "子树缺失必须置 Complete=false 封住删除判定")
	assert.Len(t, result.Pages, 2, "失败的子树不产出节点，其余子树照常")
	assert.Equal(t, []string{WarningSitemapSubtreeFailed}, result.Warnings)
}

func TestDiscoverSitemapMainFailuresReturnError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		errPart string
	}{
		{
			name:    "主文件拉取失败",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
			errPart: "拉取",
		},
		{
			name:    "主文件不是 XML",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not xml at all")) },
			errPart: "解析",
		},
		{
			name: "主文件根元素未知",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel/></rss>`))
			},
			errPart: "根元素",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/sitemap.xml", tc.handler)
			_, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errPart)
		})
	}
}

func TestDiscoverSitemapDeduplicatesNormalizedURLs(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", serveXML(sitemapURLSetXML(
		"https://example.com/a",
		"https://example.com/a#frag",
		"https://example.com/a?utm_source=news",
		"https://example.com/a?keep=1",
	)))

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err)
	assert.True(t, result.Complete)

	tokens := make([]string, 0, len(result.Pages))
	for _, node := range result.Pages {
		tokens = append(tokens, node.Token)
	}
	assert.Equal(t, []string{"https://example.com/a", "https://example.com/a?keep=1"}, tokens)
	assert.Equal(t, []string{WarningDuplicateURL, WarningDuplicateURL}, result.Warnings, "重复出现丢弃并逐条告警")
}

func TestDiscoverSitemapAppliesURLFilter(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", serveXML(sitemapURLSetXML(
		"https://example.com/docs/a",
		"https://example.com/blog/b",
		"https://other.com/docs/c",
		"https://example.com/docs",
		"https://example.com/docs/draft/x",
	)))

	opts := DiscoveryOptions{Filter: URLFilter{
		SeedHost:     "example.com",
		IncludePaths: []string{"/docs/*"},
		ExcludePaths: []string{"/docs/draft/**"},
	}}
	result, err := discoverFromServer(t, mux, "/sitemap.xml", opts)
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Empty(t, result.Warnings, "被 filter 配置性排除属于预期行为，不告警")

	tokens := make([]string, 0, len(result.Pages))
	for _, node := range result.Pages {
		tokens = append(tokens, node.Token)
	}
	assert.Equal(t, []string{"https://example.com/docs/a"}, tokens)
}

func TestDiscoverSitemapHonorsRobots(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		_, _ = w.Write([]byte(sitemapURLSetXML(base+"/private/p", base+"/public/q")))
	})

	server, client := newTestSite(t, mux)
	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	result, err := DiscoverSitemap(context.Background(), client, server.URL+"/sitemap.xml", DiscoveryOptions{Robots: checker})
	require.NoError(t, err)
	assert.True(t, result.Complete, "robots 拒绝是有意排除而非缺失，快照仍完整")

	require.Len(t, result.Pages, 1)
	assert.Equal(t, server.URL+"/public/q", result.Pages[0].Token)
	assert.Equal(t, []string{WarningRobotsDisallowed}, result.Warnings)
}

func TestDiscoverSitemapRobotsUnavailableStillAllows(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		base := "https://" + r.Host
		_, _ = w.Write([]byte(sitemapURLSetXML(base+"/a", base+"/b")))
	})

	server, client := newTestSite(t, mux)
	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	result, err := DiscoverSitemap(context.Background(), client, server.URL+"/sitemap.xml", DiscoveryOptions{Robots: checker})
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Len(t, result.Pages, 2, "robots 不可用按允许处理")
	assert.Equal(t, []string{WarningRobotsUnavailable, WarningRobotsUnavailable}, result.Warnings, "每个受检 URL 逐条告警")
}

func TestDiscoverSitemapIndexDepthLimit(t *testing.T) {
	t.Parallel()

	// buildChain 注册 /idx0.xml（主文件）到 /idxN.xml 的 index 链，最末端为 urlset 页面文档；
	// 返回记录末端文档被请求次数的计数器。
	buildChain := func(mux *http.ServeMux, lastIndex int) *atomic.Int64 {
		for i := 0; i < lastIndex; i++ {
			next := fmt.Sprintf("/idx%d.xml", i+1)
			mux.HandleFunc(fmt.Sprintf("/idx%d.xml", i), func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(sitemapIndexXMLAt("https://" + r.Host + next)))
			})
		}
		var served atomic.Int64
		terminal := fmt.Sprintf("/idx%d.xml", lastIndex)
		mux.HandleFunc(terminal, func(w http.ResponseWriter, _ *http.Request) {
			served.Add(1)
			_, _ = w.Write([]byte(sitemapURLSetXML("https://example.com/deep")))
		})
		return &served
	}

	t.Run("恰好 5 层全部展开", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		buildChain(mux, 5) // 主文件深度 0，末端文档深度 5
		result, err := discoverFromServer(t, mux, "/idx0.xml", DiscoveryOptions{})
		require.NoError(t, err)
		assert.True(t, result.Complete)
		require.Len(t, result.Pages, 1)
		assert.Equal(t, "https://example.com/deep", result.Pages[0].Token)
	})

	t.Run("超过 5 层按子树缺失处理", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		served := buildChain(mux, 6) // 末端文档深度 6，超限
		result, err := discoverFromServer(t, mux, "/idx0.xml", DiscoveryOptions{})
		require.NoError(t, err)
		assert.False(t, result.Complete)
		assert.Empty(t, result.Pages)
		assert.Equal(t, []string{WarningSitemapSubtreeFailed}, result.Warnings)
		assert.Zero(t, served.Load(), "超限的子 sitemap 不得发起请求")
	})
}

func TestDiscoverSitemapDropsInvalidEntriesWithWarning(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", serveXML(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc></loc></url>
  <url><loc>ftp://example.com/f</loc></url>
  <url><loc>https://user:pass@example.com/x</loc></url>
  <url><loc>https://example.com/ok</loc></url>
</urlset>`))

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err)
	assert.True(t, result.Complete, "单条脏数据丢弃不构成子树缺失")
	require.Len(t, result.Pages, 1)
	assert.Equal(t, "https://example.com/ok", result.Pages[0].Token)
	assert.Equal(t, []string{WarningInvalidURL, WarningInvalidURL, WarningInvalidURL}, result.Warnings)
}

func TestDiscoverSitemapWarnsOnUnparsableLastmod(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", serveXML(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/a</loc><lastmod>yesterday</lastmod></url>
</urlset>`))

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	assert.True(t, result.Pages[0].EditTime.IsZero(), "解析失败的 lastmod 忽略该字段")
	assert.Equal(t, []string{WarningInvalidLastmod}, result.Warnings)
}

func TestDiscoverSitemapInvalidChildMarksSubtreeFailed(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", serveXML(`<?xml version="1.0" encoding="UTF-8"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>   </loc></sitemap>
</sitemapindex>`))

	result, err := discoverFromServer(t, mux, "/sitemap.xml", DiscoveryOptions{})
	require.NoError(t, err)
	assert.False(t, result.Complete, "子项 loc 非法 = 子树不可达，必须封住删除判定")
	assert.Empty(t, result.Pages)
	assert.Equal(t, []string{WarningSitemapSubtreeFailed}, result.Warnings)
}

func TestDiscoverSitemapRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := DiscoverSitemap(context.Background(), nil, "https://example.com/sitemap.xml", DiscoveryOptions{})
	require.Error(t, err)
}
