package web

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSite 启动一个 TLS 假站点（handler 必须随站点一起提供，避免启动后改
// Config.Handler 造成数据竞争）并返回站点与配套 client。
// 该夹具被本文件与 fetch_http_test.go 共用。
// 生产装配注入的是 NewPublicWebClient（SSRF-safe、HTTP(S)），
// 测试用 httptest 自签证书 client 覆盖传输层，只验证抓取语义本身。
func newTestSite(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server, server.Client()
}

func newTestRobotsChecker(t *testing.T, client *http.Client, userAgent string, ttl time.Duration) *RobotsChecker {
	t.Helper()

	checker, err := NewRobotsChecker(RobotsCheckerConfig{Client: client, UserAgent: userAgent, CacheTTL: ttl})
	require.NoError(t, err)
	return checker
}

func TestRobotsCheckerAllowsWhenRobotsPermits(t *testing.T) {
	t.Parallel()

	var robotsRequests atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		robotsRequests.Add(1)
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	server, client := newTestSite(t, mux)

	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	for _, path := range []string{"/a", "/b"} {
		allowed, warning, err := checker.Allowed(context.Background(), server.URL+path)
		require.NoError(t, err)
		assert.True(t, allowed)
		assert.Empty(t, warning)
	}
	assert.Equal(t, int64(1), robotsRequests.Load(), "同一 origin 的 robots.txt 必须命中 TTL 缓存")
}

func TestRobotsCheckerBlocksDisallowedPath(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	server, client := newTestSite(t, mux)

	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	allowed, warning, err := checker.Allowed(context.Background(), server.URL+"/private/page")
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, WarningRobotsDisallowed, warning)

	allowed, warning, err = checker.Allowed(context.Background(), server.URL+"/public/page")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Empty(t, warning)
}

func TestRobotsCheckerMatchesCrawlerUserAgentGroup(t *testing.T) {
	t.Parallel()

	robots := "User-agent: otherbot\nDisallow: /\n\n" +
		"User-agent: langhuanbot\nDisallow: /nope\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(robots))
	})
	server, client := newTestSite(t, mux)

	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	allowed, _, err := checker.Allowed(context.Background(), server.URL+"/nope")
	require.NoError(t, err)
	assert.False(t, allowed, "必须按抓取 UA 匹配最具体的 group")

	allowed, _, err = checker.Allowed(context.Background(), server.URL+"/ok")
	require.NoError(t, err)
	assert.True(t, allowed, "其它 group 的 Disallow 不应影响本 UA")
}

func TestRobotsCheckerTreatsFetchProblemsAsAllowed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "robots.txt 不存在",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.NotFound(w, nil)
			},
		},
		{
			name: "robots.txt 服务端错误",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "robots.txt 内容不可解析",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("User-agent: *\nDisallow: /a\nCrawl-delay: not-a-number\n"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/robots.txt", tc.handler)
			server, client := newTestSite(t, mux)

			checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

			allowed, warning, err := checker.Allowed(context.Background(), server.URL+"/a")
			require.NoError(t, err)
			assert.True(t, allowed, "robots 拉取失败必须按允许处理（保守可用性）")
			assert.Equal(t, WarningRobotsUnavailable, warning)
		})
	}
}

func TestRobotsCheckerTreatsNetworkErrorAsAllowed(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	target := server.URL + "/a"
	server.Close()

	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	allowed, warning, err := checker.Allowed(context.Background(), target)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, WarningRobotsUnavailable, warning)
}

func TestRobotsCheckerRevalidatesAfterTTL(t *testing.T) {
	t.Parallel()

	var body atomic.Value
	body.Store("User-agent: *\nDisallow:\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	})
	server, client := newTestSite(t, mux)

	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)
	now := time.Now()
	checker.now = func() time.Time { return now }

	allowed, _, err := checker.Allowed(context.Background(), server.URL+"/a")
	require.NoError(t, err)
	require.True(t, allowed)

	body.Store("User-agent: *\nDisallow: /a\n")

	now = now.Add(30 * time.Minute)
	allowed, _, err = checker.Allowed(context.Background(), server.URL+"/a")
	require.NoError(t, err)
	assert.True(t, allowed, "TTL 内必须沿用缓存")

	now = now.Add(2 * time.Hour)
	allowed, _, err = checker.Allowed(context.Background(), server.URL+"/a")
	require.NoError(t, err)
	assert.False(t, allowed, "TTL 过期后必须重新拉取 robots.txt")
}

func TestRobotsCheckerRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	_, client := newTestSite(t, http.NewServeMux())
	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	for _, target := range []string{"", "/relative", "ftp://example.com/a"} {
		_, _, err := checker.Allowed(context.Background(), target)
		require.Error(t, err, "target %q 必须被拒绝", target)
	}
}

func TestRobotsCheckerHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nDisallow:\n"))
	})
	server, client := newTestSite(t, mux)

	checker := newTestRobotsChecker(t, client, testUserAgent, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := checker.Allowed(ctx, server.URL+"/a")
	require.Error(t, err)
}

func TestNewRobotsCheckerRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := NewRobotsChecker(RobotsCheckerConfig{UserAgent: testUserAgent})
	require.Error(t, err)
}

const testUserAgent = "LanghuanBot/1.0 (+https://langhuan.example.com)"
