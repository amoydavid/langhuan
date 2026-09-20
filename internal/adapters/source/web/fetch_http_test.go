package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

func newTestFetcher(t *testing.T, client *http.Client, mutate func(*HTTPFetcherConfig)) *HTTPFetcher {
	t.Helper()

	cfg := HTTPFetcherConfig{Client: client, UserAgent: testUserAgent}
	if mutate != nil {
		mutate(&cfg)
	}
	fetcher, err := NewHTTPFetcher(cfg)
	require.NoError(t, err)
	return fetcher
}

func TestNewHTTPFetcherRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPFetcher(HTTPFetcherConfig{UserAgent: testUserAgent})
	require.Error(t, err)
}

func TestHTTPFetcherFetchesPageWithLinksAndMetadata(t *testing.T) {
	t.Parallel()

	const page = `<!DOCTYPE html><html><body>
<a href="/next">下一页</a>
<a href="sub/page">子页</a>
<a href="#top">锚点</a>
<a href="javascript:void(0)">脚本</a>
</body></html>`
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testUserAgent, r.Header.Get("User-Agent"))
		assert.Empty(t, r.Header.Get("If-None-Match"))
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Tue, 01 Sep 2026 00:00:00 GMT")
		_, _ = w.Write([]byte(page))
	})
	server, client := newTestSite(t, mux)

	page1, err := newTestFetcher(t, client, nil).Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})
	require.NoError(t, err)

	assert.Equal(t, EngineHTTP, page1.Engine)
	assert.Equal(t, http.StatusOK, page1.Status)
	assert.Equal(t, server.URL+"/page", page1.FinalURL)
	assert.False(t, page1.NotModified)
	assert.Contains(t, string(page1.HTML), "下一页")
	assert.Equal(t, `"v1"`, page1.ETag)
	assert.Equal(t, "Tue, 01 Sep 2026 00:00:00 GMT", page1.LastModified)
	assert.Equal(t, []string{"/next", "sub/page", "#top", "javascript:void(0)"}, page1.Links,
		"出链必须是过滤前的原始 href 集合")
}

func TestHTTPFetcherFollowsRedirectsAndReportsFinalURL(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>done</body></html>"))
	})
	server, client := newTestSite(t, mux)

	page, err := newTestFetcher(t, client, nil).Fetch(context.Background(), FetchRequest{URL: server.URL + "/a"})
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/b", page.FinalURL)
	assert.Equal(t, http.StatusOK, page.Status)
}

func TestHTTPFetcherConditionalRequestReturnsNotModified(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, `"v1"`, r.Header.Get("If-None-Match"))
		assert.Equal(t, "Tue, 01 Sep 2026 00:00:00 GMT", r.Header.Get("If-Modified-Since"))
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusNotModified)
	})
	server, client := newTestSite(t, mux)

	page, err := newTestFetcher(t, client, nil).Fetch(context.Background(), FetchRequest{
		URL:             server.URL + "/page",
		IfNoneMatch:     `"v1"`,
		IfModifiedSince: "Tue, 01 Sep 2026 00:00:00 GMT",
	})
	require.NoError(t, err)

	assert.True(t, page.NotModified)
	assert.Equal(t, http.StatusNotModified, page.Status)
	assert.Empty(t, page.HTML)
	assert.Equal(t, `"v1"`, page.ETag)
}

func TestHTTPFetcherRetriesRetryableStatuses(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})
	server, client := newTestSite(t, mux)

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		cfg.RetryBaseDelay = time.Millisecond
	})
	page, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Equal(t, int64(3), attempts.Load())
}

func TestHTTPFetcherStopsAtRetryLimit(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	server, client := newTestSite(t, mux)

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		cfg.RetryBaseDelay = time.Millisecond
		cfg.MaxRetries = 3
	})
	_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})

	require.Error(t, err)
	assert.Equal(t, int64(4), attempts.Load(), "重试上限 3 次 = 首次 + 3 次重试")
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, http.StatusServiceUnavailable, pageErr.Status)
	assert.Equal(t, OutcomeRetryable, pageErr.Outcome)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err))
}

func TestHTTPFetcherHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})
	server, client := newTestSite(t, mux)

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		// 退避基数刻意远大于 Retry-After: 0：若实现忽略了 Retry-After，
		// 记录到的等待值会是 300ms 的指数退避而非 0。
		cfg.RetryBaseDelay = 300 * time.Millisecond
	})

	// 确定性断言：记录 sleep 的计算值而非测量总耗时。此前版本断言整体耗时 <150ms，
	// 在整包 -race/-count=10 下会被 HTTP 往返与调度抖动突破（与退避语义无关）。
	var (
		sleepsMu sync.Mutex
		sleeps   []time.Duration
	)
	fetcher.sleep = func(_ context.Context, d time.Duration) error {
		sleepsMu.Lock()
		sleeps = append(sleeps, d)
		sleepsMu.Unlock()
		return nil
	}

	page, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Equal(t, int64(2), attempts.Load())

	sleepsMu.Lock()
	defer sleepsMu.Unlock()
	assert.Equal(t, []time.Duration{0}, sleeps, "重试等待必须优先采用 Retry-After: 0，而非指数退避基数")
}

func TestHTTPFetcherClassifiesFailureStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status  int
		outcome PageOutcome
	}{
		{status: http.StatusNotFound, outcome: OutcomeGone},
		{status: http.StatusGone, outcome: OutcomeGone},
		{status: http.StatusUnauthorized, outcome: OutcomeBlocked},
		{status: http.StatusForbidden, outcome: OutcomeBlocked},
		{status: http.StatusBadRequest, outcome: OutcomeSkipped},
		{status: http.StatusInternalServerError, outcome: OutcomeRetryable},
	}

	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})
			server, client := newTestSite(t, mux)

			fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
				cfg.RetryBaseDelay = time.Millisecond
			})
			_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})

			require.Error(t, err)
			var pageErr *PageError
			require.ErrorAs(t, err, &pageErr)
			assert.Equal(t, tc.status, pageErr.Status)
			assert.Equal(t, tc.outcome, pageErr.Outcome)
			assert.Equal(t, tc.outcome, ClassifyError(err))
		})
	}
}

func TestHTTPFetcherRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>" + longText(400) + "</body></html>"))
	})
	server, client := newTestSite(t, mux)

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		cfg.MaxContentBytes = 64
	})
	_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})

	require.Error(t, err)
	assert.ErrorIs(t, err, sourceport.ErrSourceContentTooLarge)
	assert.Equal(t, OutcomeOversize, ClassifyError(err))

	// 请求级上限覆盖配置上限。
	_, err = fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page", MaxContentBytes: 4096})
	require.NoError(t, err)
}

func TestHTTPFetcherConvertsCharsetToUTF8(t *testing.T) {
	t.Parallel()

	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("<html><body><p>中文正文</p></body></html>"))
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=gbk")
		_, _ = w.Write(encoded)
	})
	server, client := newTestSite(t, mux)

	page, err := newTestFetcher(t, client, nil).Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})
	require.NoError(t, err)
	assert.Contains(t, string(page.HTML), "中文正文", "响应体必须按声明字符集转为 UTF-8")
}

func TestHTTPFetcherClassifiesNetworkErrorAsRetryable(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	target := server.URL + "/page"
	server.Close()

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		cfg.RetryBaseDelay = time.Millisecond
	})
	_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: target})

	require.Error(t, err)
	var pageErr *PageError
	require.ErrorAs(t, err, &pageErr)
	assert.Equal(t, 0, pageErr.Status)
	assert.Equal(t, OutcomeRetryable, pageErr.Outcome, "网络错误不在单页层重试，交给 Job 级 RetryRequired")
}

func TestHTTPFetcherRejectsInvalidRequestURL(t *testing.T) {
	t.Parallel()

	_, client := newTestSite(t, http.NewServeMux())
	fetcher := newTestFetcher(t, client, nil)

	for _, raw := range []string{"", "/relative", "ftp://example.com/a"} {
		_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: raw})
		require.Error(t, err, "URL %q 必须被拒绝", raw)
		var pageErr *PageError
		require.ErrorAs(t, err, &pageErr)
		assert.Equal(t, OutcomeSkipped, pageErr.Outcome)
	}
}

func TestHTTPFetcherSerializesSameHostAndKeepsDelay(t *testing.T) {
	t.Parallel()

	const (
		concurrency  = 3
		requestDelay = 60 * time.Millisecond
	)

	var (
		mu         sync.Mutex
		inFlight   int
		maxInFligh int
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFligh {
			maxInFligh = inFlight
		}
		mu.Unlock()

		// 让 handler 占用一段时间：若 host 串行锁失效，并发请求会在 handler 内重叠，
		// maxInFlight 断言才有检出能力（串行锁存在时该值结构性地不可能超过 1）。
		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})
	server, client := newTestSite(t, mux)

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		cfg.RequestDelay = requestDelay
	})

	// 确定性断言：经 now/sleep 注入缝替换时钟并记录 sleep 的计算值，不测墙钟到达间隔。
	// 此前版本测量真实到达间隔（阈值 delay-10ms），在整包 -race 下被调度/GC 抖动压缩
	// 到 34.6ms 而间歇失败——限速语义本身与墙钟无关，应校验 lockHost 的计算结果。
	var (
		sleepsMu sync.Mutex
		sleeps   []time.Duration
		clock    atomic.Int64 // 假时钟，UnixNano；仅由 fake sleep 前推
	)
	clock.Store(time.Now().UnixNano())
	fetcher.now = func() time.Time { return time.Unix(0, clock.Load()) }
	fetcher.sleep = func(_ context.Context, d time.Duration) error {
		sleepsMu.Lock()
		sleeps = append(sleeps, d)
		sleepsMu.Unlock()
		clock.Add(int64(d))
		return nil
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency+1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	sleepsMu.Lock()
	defer sleepsMu.Unlock()
	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 1, maxInFligh, "同 host 必须串行")

	// 首个占用 host 锁的请求不等待（host 无历史请求）；其后每个请求精确等待
	// requestDelay——间隔从上一次请求的开始时刻起算（lastRequestAt 在获得 host 锁时
	// 记录），而非完成时刻。假时钟下该值为精确相等（次次如此，无噪声容忍）。
	require.Len(t, sleeps, concurrency, "首个请求不等待，其后每个请求各等待一次")
	for i, d := range sleeps {
		assert.Equal(t, requestDelay, d, "第 %d 次等待应精确等于 requestDelay", i+1)
	}
}

func TestHTTPFetcherRespectsContextCancellationDuringDelay(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})
	server, client := newTestSite(t, mux)

	fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
		cfg.RequestDelay = 5 * time.Second
	})
	_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = fetcher.Fetch(ctx, FetchRequest{URL: server.URL + "/page"})
	require.Error(t, err)
	assert.Equal(t, OutcomeRetryable, ClassifyError(err))
}

func TestExtractHrefsKeepsRawOrderAndValues(t *testing.T) {
	t.Parallel()

	htmlBody := []byte(`<html><body>
<a href="/a">a</a>
<a href=/b>b</a>
<a href="/a?x=1&amp;y=2">c</a>
<a>无 href</a>
<a href="">空</a>
<a href="  /spaced  ">空白</a>
</body></html>`)

	assert.Equal(t, []string{"/a", "/b", "/a?x=1&y=2", "/spaced"}, extractHrefs(htmlBody))
	assert.Empty(t, extractHrefs([]byte("not html at all")))
}

func TestHTTPFetcherSkipsNonHTMLResponses(t *testing.T) {
	t.Parallel()

	const body = "<html><body>正文标记 lanhuan-marker</body></html>"
	cases := []struct {
		name        string
		contentType string
		wantSkip    bool
	}{
		{name: "PDF 链接", contentType: "application/pdf", wantSkip: true},
		{name: "DOC 链接", contentType: "application/msword", wantSkip: true},
		{name: "图片", contentType: "image/png", wantSkip: true},
		{name: "纯文本", contentType: "text/plain; charset=utf-8", wantSkip: true},
		{name: "无法解析的 Content-Type 声明", contentType: "not-a-media-type", wantSkip: true},
		{name: "HTML", contentType: "text/html", wantSkip: false},
		{name: "HTML 带字符集", contentType: "text/html; charset=utf-8", wantSkip: false},
		{name: "XHTML", contentType: "application/xhtml+xml", wantSkip: false},
		{name: "缺失 Content-Type 按 HTML 尝试", contentType: "", wantSkip: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var attempts atomic.Int64
			mux := http.NewServeMux()
			mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				_, _ = w.Write([]byte(body))
			})
			server, client := newTestSite(t, mux)

			fetcher := newTestFetcher(t, client, func(cfg *HTTPFetcherConfig) {
				cfg.RetryBaseDelay = time.Millisecond
			})
			page, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/page"})

			if !tc.wantSkip {
				require.NoError(t, err)
				assert.Contains(t, string(page.HTML), "lanhuan-marker")
				return
			}

			require.Error(t, err)
			var pageErr *PageError
			require.ErrorAs(t, err, &pageErr)
			assert.Equal(t, http.StatusOK, pageErr.Status, "状态仍为 200，编排层必须按 Outcome 判定")
			assert.Equal(t, OutcomeSkippedNonHTML, pageErr.Outcome)
			assert.Equal(t, OutcomeSkippedNonHTML, ClassifyError(err))
			assert.Equal(t, string(OutcomeSkippedNonHTML), WarningSkippedNonHTML)
			assert.Equal(t, int64(1), attempts.Load(), "非 HTML 跳过不算 retryable，不得重试")
		})
	}
}
