package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const crawlSeed = "https://docs.example.com/"

// crawlURL 构造 fake 站点内同 host 的规范化页面 URL。
func crawlURL(path string) string { return "https://docs.example.com" + path }

// fakePage 描述 fake 站点中一个 URL 的抓取结果；err 非 nil 表示该页抓取失败。
type fakePage struct {
	links []string
	err   error
}

// fakeCrawlFetcher 是 PageFetcher 的内存实现：fetches 记录实际发起的抓取（用于断言
// 「被过滤/超深/去重后的 URL 不得发起请求」），未登记的 URL 按 404 失败。
type fakeCrawlFetcher struct {
	site    map[string]fakePage
	fetches []string
}

func newFakeCrawlFetcher(site map[string]fakePage) *fakeCrawlFetcher {
	return &fakeCrawlFetcher{site: site}
}

func (f *fakeCrawlFetcher) Fetch(_ context.Context, request FetchRequest) (fetchedPage, error) {
	f.fetches = append(f.fetches, request.URL)
	page, ok := f.site[request.URL]
	if !ok {
		return fetchedPage{}, &PageError{Status: http.StatusNotFound, Outcome: OutcomeGone, Err: errors.New("HTTP 状态 404")}
	}
	if page.err != nil {
		return fetchedPage{}, page.err
	}
	return fetchedPage{
		FinalURL: request.URL,
		Status:   http.StatusOK,
		HTML:     []byte("<html><body>fake</body></html>"),
		Links:    page.links,
		Engine:   EngineHTTP,
	}, nil
}

// fakeRobots 是 RobotsGate 的内存实现。
type fakeRobots struct {
	disallowed     map[string]struct{}
	allowedWarning string // 非空时每次返回该告警并放行
	err            error
}

func (f *fakeRobots) Allowed(_ context.Context, target string) (bool, string, error) {
	if f.err != nil {
		return false, "", f.err
	}
	if _, blocked := f.disallowed[target]; blocked {
		return false, WarningRobotsDisallowed, nil
	}
	if f.allowedWarning != "" {
		return true, f.allowedWarning, nil
	}
	return true, "", nil
}

// crawlSiteWithLinks 构造「种子 + 一层出链」的站点：dead 集合内的出链按 404 失败。
func crawlSiteWithLinks(links []string, dead map[string]struct{}) map[string]fakePage {
	site := map[string]fakePage{crawlSeed: {links: links}}
	for _, link := range links {
		if _, isDead := dead[link]; isDead {
			site[crawlURL(link)] = fakePage{err: &PageError{Status: http.StatusNotFound, Outcome: OutcomeGone, Err: errors.New("HTTP 状态 404")}}
			continue
		}
		site[crawlURL(link)] = fakePage{}
	}
	return site
}

func TestDiscoverCrawlRespectsDepthBoundary(t *testing.T) {
	t.Parallel()

	site := map[string]fakePage{
		crawlSeed:       {links: []string{"/a", "/b"}},
		crawlURL("/a"):  {links: []string{"/a1"}},
		crawlURL("/b"):  {links: []string{"/b1"}},
		crawlURL("/a1"): {links: []string{"/deep"}},
		crawlURL("/b1"): {},
	}

	cases := []struct {
		name        string
		maxDepth    int
		wantTokens  []string
		wantFetches []string
	}{
		{
			name:        "max_depth=0 只抓种子",
			maxDepth:    0,
			wantTokens:  []string{crawlSeed},
			wantFetches: []string{crawlSeed},
		},
		{
			name:        "max_depth=1 拒绝第 2 层出链",
			maxDepth:    1,
			wantTokens:  []string{crawlSeed, crawlURL("/a"), crawlURL("/b")},
			wantFetches: []string{crawlSeed, crawlURL("/a"), crawlURL("/b")},
		},
		{
			name:        "max_depth=2 拒绝第 3 层出链",
			maxDepth:    2,
			wantTokens:  []string{crawlSeed, crawlURL("/a"), crawlURL("/b"), crawlURL("/a1"), crawlURL("/b1")},
			wantFetches: []string{crawlSeed, crawlURL("/a"), crawlURL("/b"), crawlURL("/a1"), crawlURL("/b1")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fetcher := newFakeCrawlFetcher(site)
			result, err := DiscoverCrawl(context.Background(), crawlSeed,
				CrawlScope{MaxDepth: tc.maxDepth, MaxPages: 100}, DiscoveryOptions{}, fetcher)
			require.NoError(t, err)
			assert.True(t, result.Complete)
			assert.Empty(t, result.Warnings)

			tokens := make([]string, 0, len(result.Pages))
			for _, node := range result.Pages {
				tokens = append(tokens, node.Token)
			}
			assert.Equal(t, tc.wantTokens, tokens, "FIFO BFS 顺序应确定")
			assert.Equal(t, tc.wantFetches, fetcher.fetches, "超深出链不得发起抓取")

			if tc.maxDepth == 1 {
				assert.Equal(t, "docs.example.com", result.Pages[0].Title, "根路径标题回退为 hostname")
				assert.Equal(t, "a", result.Pages[1].Title)
			}
		})
	}
}

func TestDiscoverCrawlTruncatesAtMaxPages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		links         []string
		maxPages      int
		wantComplete  bool
		wantNodes     int
		wantFetches   int
		wantTruncated bool
	}{
		{
			name:          "达到 max_pages 截断",
			links:         []string{"/p1", "/p2", "/p3", "/p4", "/p5"},
			maxPages:      3,
			wantComplete:  false,
			wantNodes:     3,
			wantFetches:   3,
			wantTruncated: true,
		},
		{
			name:          "恰好等于上限且无剩余出链",
			links:         []string{"/p1", "/p2"},
			maxPages:      3,
			wantComplete:  true,
			wantNodes:     3,
			wantFetches:   3,
			wantTruncated: false,
		},
		{
			name:          "上限 1 且种子仍有未探明出链",
			links:         []string{"/p1"},
			maxPages:      1,
			wantComplete:  false,
			wantNodes:     1,
			wantFetches:   1,
			wantTruncated: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			site := map[string]fakePage{crawlSeed: {links: tc.links}}
			for _, link := range tc.links {
				site[crawlURL(link)] = fakePage{}
			}
			fetcher := newFakeCrawlFetcher(site)
			result, err := DiscoverCrawl(context.Background(), crawlSeed,
				CrawlScope{MaxDepth: 1, MaxPages: tc.maxPages}, DiscoveryOptions{}, fetcher)
			require.NoError(t, err)

			assert.Equal(t, tc.wantComplete, result.Complete)
			assert.Len(t, result.Pages, tc.wantNodes)
			assert.Len(t, fetcher.fetches, tc.wantFetches, "截断后不得继续抓取")
			if tc.wantTruncated {
				assert.Equal(t, []string{WarningTruncated}, result.Warnings)
			} else {
				assert.Empty(t, result.Warnings)
			}
		})
	}
}

func TestDiscoverCrawlConsecutiveFailureThreshold(t *testing.T) {
	t.Parallel()

	build := func(good, dead int) map[string]fakePage {
		var links []string
		for i := 1; i <= good; i++ {
			links = append(links, fmt.Sprintf("/g%d", i))
		}
		var deadSet map[string]struct{}
		for i := 1; i <= dead; i++ {
			links = append(links, fmt.Sprintf("/d%d", i))
		}
		if dead > 0 {
			deadSet = make(map[string]struct{}, dead)
			for i := good + 1; i <= good+dead; i++ {
				deadSet[links[i-1]] = struct{}{}
			}
		}
		return crawlSiteWithLinks(links, deadSet)
	}

	t.Run("连续失败达到 3 触发（失败率低于阈值，隔离归因）", func(t *testing.T) {
		t.Parallel()

		// 16 次抓取失败 3 次（18.75% ≤ 20%），连续失败达到 3 → 只能由连续阈值触发。
		fetcher := newFakeCrawlFetcher(build(12, 3))
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{}, fetcher)
		require.NoError(t, err)
		assert.False(t, result.Complete)
		assert.Equal(t, []string{"gone", "gone", "gone"}, result.Warnings, "失败页逐条告警且不进 Nodes")
		assert.Len(t, result.Pages, 13, "种子与 12 个成功页")
	})

	t.Run("连续失败 2 次不触发", func(t *testing.T) {
		t.Parallel()

		fetcher := newFakeCrawlFetcher(build(13, 2))
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{}, fetcher)
		require.NoError(t, err)
		assert.True(t, result.Complete)
		assert.Len(t, result.Pages, 14)
	})
}

func TestDiscoverCrawlFailureRateThreshold(t *testing.T) {
	t.Parallel()

	t.Run("失败率超过 20% 触发（连续失败最多 1 次，隔离归因）", func(t *testing.T) {
		t.Parallel()

		// 失败率按运行过程在线评估：首次失败发生在第 2 次抓取（1/2=50% > 20%），
		// 当场触发且不可逆；最终失败率 3/10 同样超限。连续失败全程最多 1 →
		// 只能由失败率触发。
		links := []string{"/d1", "/g1", "/d2", "/g2", "/d3", "/g3", "/g4", "/g5", "/g6"}
		dead := map[string]struct{}{"/d1": {}, "/d2": {}, "/d3": {}}
		fetcher := newFakeCrawlFetcher(crawlSiteWithLinks(links, dead))
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{}, fetcher)
		require.NoError(t, err)
		assert.False(t, result.Complete)
		assert.Len(t, result.Pages, 7)
	})

	t.Run("失败率恰为 20% 不触发（严格大于）", func(t *testing.T) {
		t.Parallel()

		// 失败率按运行过程在线评估（任一失败时点的比率超限即触发，且不可逆）：
		// 因此本例把两次失败安排在「当时比率恰为 20%」的位置——第 5 次抓取（1/5）
		// 与第 10 次抓取（2/10）。全程无一次超限 → Complete 保持 true。
		links := []string{"/g1", "/g2", "/g3", "/g4", "/d1", "/g5", "/g6", "/g7", "/d2"}
		dead := map[string]struct{}{"/d1": {}, "/d2": {}}
		fetcher := newFakeCrawlFetcher(crawlSiteWithLinks(links, dead))
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{}, fetcher)
		require.NoError(t, err)
		assert.True(t, result.Complete)
		assert.Len(t, result.Pages, 8)
		assert.Equal(t, []string{"gone", "gone"}, result.Warnings, "失败页仍逐条告警，但不构成不完整快照")
	})
}

func TestDiscoverCrawlHonorsRobots(t *testing.T) {
	t.Parallel()

	t.Run("Disallow 的 URL 不进 Nodes 也不抓取", func(t *testing.T) {
		t.Parallel()

		site := map[string]fakePage{
			crawlSeed:             {links: []string{"/private/p", "/public/q"}},
			crawlURL("/public/q"): {},
		}
		robots := &fakeRobots{disallowed: map[string]struct{}{crawlURL("/private/p"): {}}}
		fetcher := newFakeCrawlFetcher(site)
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{Robots: robots}, fetcher)
		require.NoError(t, err)
		assert.True(t, result.Complete, "robots 拒绝是有意排除而非缺失，快照仍完整")
		require.Len(t, result.Pages, 2)
		assert.Equal(t, crawlSeed, result.Pages[0].Token)
		assert.Equal(t, crawlURL("/public/q"), result.Pages[1].Token)
		assert.Equal(t, []string{WarningRobotsDisallowed}, result.Warnings)
		assert.NotContains(t, fetcher.fetches, crawlURL("/private/p"), "被 robots 拒绝的 URL 不得发起抓取")
	})

	t.Run("种子被 Disallow → Complete=false", func(t *testing.T) {
		t.Parallel()

		// 种子被拒 = BFS 无法开始、快照没有任何边界知识；
		// 若以「完整空树」参与 diff 会误删整源页面，必须置 Complete=false。
		site := map[string]fakePage{crawlSeed: {links: []string{"/a"}}}
		robots := &fakeRobots{disallowed: map[string]struct{}{crawlSeed: {}}}
		fetcher := newFakeCrawlFetcher(site)
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{Robots: robots}, fetcher)
		require.NoError(t, err)
		assert.False(t, result.Complete)
		assert.Empty(t, result.Pages)
		assert.Equal(t, []string{WarningRobotsDisallowed}, result.Warnings)
		assert.Empty(t, fetcher.fetches, "robots 校验先于抓取")
	})

	t.Run("robots 不可用放行并告警", func(t *testing.T) {
		t.Parallel()

		site := map[string]fakePage{
			crawlSeed:      {links: []string{"/a"}},
			crawlURL("/a"): {},
		}
		robots := &fakeRobots{allowedWarning: WarningRobotsUnavailable}
		fetcher := newFakeCrawlFetcher(site)
		result, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{Robots: robots}, fetcher)
		require.NoError(t, err)
		assert.True(t, result.Complete)
		assert.Len(t, result.Pages, 2)
		assert.Equal(t, []string{WarningRobotsUnavailable, WarningRobotsUnavailable}, result.Warnings)
	})

	t.Run("robots 校验错误中断发现", func(t *testing.T) {
		t.Parallel()

		site := map[string]fakePage{crawlSeed: {links: []string{"/a"}}}
		robots := &fakeRobots{err: errors.New("robots 存储故障")}
		fetcher := newFakeCrawlFetcher(site)
		_, err := DiscoverCrawl(context.Background(), crawlSeed,
			CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{Robots: robots}, fetcher)
		require.Error(t, err, "只有 context 中断/目标非法才走 error 通道，发现必须停止")
	})
}

func TestDiscoverCrawlFiltersAndDeduplicatesLinks(t *testing.T) {
	t.Parallel()

	filter := URLFilter{SeedHost: "docs.example.com", IncludePaths: []string{"/docs/**"}}
	site := map[string]fakePage{
		crawlSeed: {links: []string{
			"/docs/a",
			"/blog/b",
			"/docs/a?utm_campaign=x#top",
			"https://other.example.com/x",
			"mailto:a@b.c",
			"javascript:void(0)",
			"#frag",
			"   ",
		}},
		crawlURL("/docs/a"): {},
	}

	fetcher := newFakeCrawlFetcher(site)
	result, err := DiscoverCrawl(context.Background(), crawlSeed,
		CrawlScope{MaxDepth: 1, MaxPages: 10}, DiscoveryOptions{Filter: filter}, fetcher)
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Empty(t, result.Warnings)

	tokens := make([]string, 0, len(result.Pages))
	for _, node := range result.Pages {
		tokens = append(tokens, node.Token)
	}
	assert.Equal(t, []string{crawlSeed, crawlURL("/docs/a")}, tokens, "仅范围内 URL 入队")
	assert.Equal(t, []string{crawlSeed, crawlURL("/docs/a")}, fetcher.fetches,
		"范围外、非 http(s) 与去重后的出链不得发起抓取")
}

func TestDiscoverCrawlSeedFailureReturnsError(t *testing.T) {
	t.Parallel()

	fetcher := newFakeCrawlFetcher(map[string]fakePage{})
	_, err := DiscoverCrawl(context.Background(), crawlSeed,
		CrawlScope{MaxDepth: 2, MaxPages: 10}, DiscoveryOptions{}, fetcher)
	require.Error(t, err, "种子抓取失败 = 无任何边界知识，必须作为 error 失败整个 run")
}

func TestDiscoverCrawlNonHTMLSkipsAreNotFailures(t *testing.T) {
	t.Parallel()

	nonHTML := &PageError{Status: http.StatusOK, Outcome: OutcomeSkippedNonHTML, Err: errors.New("Content-Type: application/pdf")}
	site := map[string]fakePage{crawlSeed: {links: []string{"/n1", "/n2", "/n3", "/n4"}}}
	for _, link := range []string{"/n1", "/n2", "/n3", "/n4"} {
		site[crawlURL(link)] = fakePage{err: nonHTML}
	}

	fetcher := newFakeCrawlFetcher(site)
	result, err := DiscoverCrawl(context.Background(), crawlSeed,
		CrawlScope{MaxDepth: 1, MaxPages: 100}, DiscoveryOptions{}, fetcher)
	require.NoError(t, err)
	assert.True(t, result.Complete, "内容类型跳过是已知无文档，不算抓取失败")
	require.Len(t, result.Pages, 1)
	assert.Equal(t, []string{"skipped_non_html", "skipped_non_html", "skipped_non_html", "skipped_non_html"}, result.Warnings)
}

func TestDiscoverCrawlDeduplicatesAcrossBranches(t *testing.T) {
	t.Parallel()

	site := map[string]fakePage{
		crawlSeed:           {links: []string{"/x", "/y"}},
		crawlURL("/x"):      {links: []string{"/shared"}},
		crawlURL("/y"):      {links: []string{"/shared?utm_source=t"}},
		crawlURL("/shared"): {},
	}

	fetcher := newFakeCrawlFetcher(site)
	result, err := DiscoverCrawl(context.Background(), crawlSeed,
		CrawlScope{MaxDepth: 2, MaxPages: 100}, DiscoveryOptions{}, fetcher)
	require.NoError(t, err)
	assert.True(t, result.Complete)
	assert.Len(t, result.Pages, 4)
	assert.Equal(t, []string{crawlSeed, crawlURL("/x"), crawlURL("/y"), crawlURL("/shared")}, fetcher.fetches,
		"同一规范化 URL 全局只抓取一次")
}

func TestDiscoverCrawlValidatesInputs(t *testing.T) {
	t.Parallel()

	fetcher := newFakeCrawlFetcher(map[string]fakePage{crawlSeed: {}})
	ctx := context.Background()

	cases := []struct {
		name    string
		seed    string
		scope   CrawlScope
		fetcher PageFetcher
	}{
		{name: "种子为空", seed: "", scope: CrawlScope{MaxDepth: 1, MaxPages: 10}, fetcher: fetcher},
		{name: "种子非 http(s)", seed: "ftp://example.com/a", scope: CrawlScope{MaxDepth: 1, MaxPages: 10}, fetcher: fetcher},
		{name: "max_depth 为负", seed: crawlSeed, scope: CrawlScope{MaxDepth: -1, MaxPages: 10}, fetcher: fetcher},
		{name: "max_pages 非正", seed: crawlSeed, scope: CrawlScope{MaxDepth: 1, MaxPages: 0}, fetcher: fetcher},
		{name: "未注入抓取器", seed: crawlSeed, scope: CrawlScope{MaxDepth: 1, MaxPages: 10}, fetcher: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := DiscoverCrawl(ctx, tc.seed, tc.scope, DiscoveryOptions{}, tc.fetcher)
			require.Error(t, err)
		})
	}
}
