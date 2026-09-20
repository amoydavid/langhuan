package web

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewURLFilterRejectsInvalidSeed(t *testing.T) {
	t.Parallel()

	for _, seed := range []string{"", "ftp://example.com/a", "/relative", "example.com/a", "https://user:pass@example.com/"} {
		t.Run(seed, func(t *testing.T) {
			_, err := NewURLFilter(value.WebSourceCrawlConfig{SeedURL: seed})
			require.Error(t, err, "seed %q 必须被拒绝", seed)
		})
	}
}

func TestURLFilterAllow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		crawl   value.WebSourceCrawlConfig
		rawURL  string
		allowed bool
	}{
		{
			name:    "同 host 无规则放行",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/docs/"},
			rawURL:  "https://example.com/blog/a",
			allowed: true,
		},
		{
			name:    "异 host 默认拒绝",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/"},
			rawURL:  "https://other.com/",
			allowed: false,
		},
		{
			name:    "allow_subdomains 允许子域",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", AllowSubdomains: true},
			rawURL:  "https://docs.example.com/a",
			allowed: true,
		},
		{
			name:    "allow_subdomains 不放行后缀伪装域",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", AllowSubdomains: true},
			rawURL:  "https://example.com.evil.net/a",
			allowed: false,
		},
		{
			name:    "allow_subdomains=false 拒绝子域",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/"},
			rawURL:  "https://docs.example.com/a",
			allowed: false,
		},
		{
			name:    "host 匹配忽略端口",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com:8443/"},
			rawURL:  "https://example.com/a",
			allowed: true,
		},
		{
			name:    "host 匹配大小写不敏感",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://Example.COM/"},
			rawURL:  "https://example.com/a",
			allowed: true,
		},
		{
			name:    "非 http(s) scheme 拒绝",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/"},
			rawURL:  "mailto:hi@example.com",
			allowed: false,
		},
		{
			name:    "http scheme 放行",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "http://example.com/"},
			rawURL:  "http://example.com/a",
			allowed: true,
		},
		{
			name:    "带凭证 URL 拒绝",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/"},
			rawURL:  "https://user:pass@example.com/a",
			allowed: false,
		},
		{
			name:    "include 命中放行",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/docs/", IncludePaths: []string{"/docs/**"}},
			rawURL:  "https://example.com/docs/guide/intro",
			allowed: true,
		},
		{
			name:    "include 未命中拒绝",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/docs/", IncludePaths: []string{"/docs/**"}},
			rawURL:  "https://example.com/blog/a",
			allowed: false,
		},
		{
			name:    "exclude 命中拒绝",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", ExcludePaths: []string{"/private/**"}},
			rawURL:  "https://example.com/private/a",
			allowed: false,
		},
		{
			name: "exclude 优先于 include",
			crawl: value.WebSourceCrawlConfig{
				SeedURL:      "https://example.com/",
				IncludePaths: []string{"/docs/**"},
				ExcludePaths: []string{"/docs/draft/**"},
			},
			rawURL:  "https://example.com/docs/draft/a",
			allowed: false,
		},
		{
			name: "exclude 优先于 include（同级）",
			crawl: value.WebSourceCrawlConfig{
				SeedURL:      "https://example.com/",
				IncludePaths: []string{"/docs/**"},
				ExcludePaths: []string{"/docs/**"},
			},
			rawURL:  "https://example.com/docs/a",
			allowed: false,
		},
		{
			name:    "单星不跨路径段",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", IncludePaths: []string{"/*"}},
			rawURL:  "https://example.com/a",
			allowed: true,
		},
		{
			name:    "单星不跨路径段（深层拒绝）",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", IncludePaths: []string{"/*"}},
			rawURL:  "https://example.com/a/b",
			allowed: false,
		},
		{
			name:    "双星匹配零段",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", IncludePaths: []string{"/docs/**"}},
			rawURL:  "https://example.com/docs",
			allowed: true,
		},
		{
			name:    "段内通配匹配文件名",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", IncludePaths: []string{"/blog/*.html"}},
			rawURL:  "https://example.com/blog/post.html",
			allowed: true,
		},
		{
			name:    "段内通配不匹配子目录",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", IncludePaths: []string{"/blog/*.html"}},
			rawURL:  "https://example.com/blog/2026/post.html",
			allowed: false,
		},
		{
			name:    "查询串不参与过滤",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/", IncludePaths: []string{"/search"}},
			rawURL:  "https://example.com/search?q=1",
			allowed: true,
		},
		{
			name:    "相对 URL 拒绝",
			crawl:   value.WebSourceCrawlConfig{SeedURL: "https://example.com/"},
			rawURL:  "/a",
			allowed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filter, err := NewURLFilter(tc.crawl)
			require.NoError(t, err)
			assert.Equal(t, tc.allowed, filter.Allow(tc.rawURL))
		})
	}
}

func TestURLFilterZeroValueAllowsEveryPublicHTTPSURL(t *testing.T) {
	t.Parallel()

	// 零值过滤器（SeedHost 为空）不做 host 限制，仅按 scheme 判定：
	// sitemap/urls 模式复用同一套出链过滤时不需重复注入种子 host。
	var filter URLFilter
	assert.True(t, filter.Allow("https://any.example.com/a?b=1"))
	assert.False(t, filter.Allow("ftp://any.example.com/a"))
	assert.False(t, filter.Allow("javascript:void(0)"))
}

func TestResolveURLResolvesRelativeAndNormalizes(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.com/docs/guide/")
	require.NoError(t, err)

	cases := []struct {
		name string
		base *url.URL
		raw  string
		want string
		ok   bool
	}{
		{name: "相对路径", base: base, raw: "intro.html", want: "https://example.com/docs/guide/intro.html", ok: true},
		{name: "站内绝对路径", base: base, raw: "/blog/a", want: "https://example.com/blog/a", ok: true},
		{name: "协议相对", base: base, raw: "//cdn.example.com/a", want: "https://cdn.example.com/a", ok: true},
		{name: "去 fragment", base: base, raw: "/blog/a#section", want: "https://example.com/blog/a", ok: true},
		{name: "剥跟踪参数", base: base, raw: "/blog/a?utm_source=x&id=2", want: "https://example.com/blog/a?id=2", ok: true},
		{name: "host 小写与去默认端口", base: base, raw: "https://EXAMPLE.com:443/a", want: "https://example.com/a", ok: true},
		{name: "非 http scheme 拒绝", base: base, raw: "javascript:void(0)", ok: false},
		{name: "凭证 URL 拒绝", base: base, raw: "https://u:p@example.com/a", ok: false},
		{name: "空引用回退 base", base: base, raw: "", want: "https://example.com/docs/guide/", ok: true},
		{name: "无 base 的相对引用拒绝", base: nil, raw: "intro.html", ok: false},
		{name: "无 base 的绝对 URL", base: nil, raw: "https://example.com/a", want: "https://example.com/a", ok: true},
		{name: "非法 URL 拒绝", base: base, raw: "http://[::1", ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ResolveURL(tc.base, tc.raw)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveLinksFiltersDedupesAndKeepsOrder(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://example.com/docs/")
	require.NoError(t, err)
	filter, err := NewURLFilter(value.WebSourceCrawlConfig{
		SeedURL:      "https://example.com/docs/",
		IncludePaths: []string{"/docs/**"},
	})
	require.NoError(t, err)

	links := ResolveLinks(base, []string{
		"guide",
		"/docs/intro",
		"./guide",
		"/blog/out-of-scope",
		"https://other.com/docs/x",
		"mailto:hi@example.com",
		"#anchor",
		"/docs/intro?utm_source=news",
		"",
	}, filter)

	assert.Equal(t, []string{
		"https://example.com/docs/guide",
		"https://example.com/docs/intro",
	}, links)
}
