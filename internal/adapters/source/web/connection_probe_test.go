package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 T12 的连接测试探测（spec §9.4）：firecrawl 调 /v2/scrape 单页并断言
// markdown、jina 调 reader、browser 握手 CDP。外部调用全部经 httptest 假端点或
// 注入 seam，不发真实网络请求（fetch_provider_test.go 同口径）。

// noopValidateTarget 绕过真实 DNS 预校验（测试目标 example.com 是固定字面量）。
func noopValidateTarget(context.Context, string) error { return nil }

func newProbeConnector(t *testing.T) *ConnectionProber {
	t.Helper()
	prober := NewConnectionProber(5 * time.Second)
	prober.validateTarget = noopValidateTarget
	return prober
}

func TestConnectionProberFirecrawlScrapeSuccess(t *testing.T) {
	t.Parallel()

	var (
		gotPath    string
		gotAuth    string
		gotPayload firecrawlScrapeRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"markdown":"# Example Domain","metadata":{"statusCode":200,"url":"https://example.com"}}}`))
	}))
	defer server.Close()

	prober := newProbeConnector(t)
	started := time.Now()
	err := prober.Probe(t.Context(), value.SourceProviderFirecrawl, "fc-key", server.URL)
	require.NoError(t, err)
	assert.Equal(t, "/v2/scrape", gotPath)
	assert.Equal(t, "Bearer fc-key", gotAuth)
	assert.Equal(t, "https://example.com", gotPayload.URL)
	assert.GreaterOrEqual(t, time.Since(started), time.Duration(0))
}

func TestConnectionProberFirecrawlRejectsEmptyMarkdown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"markdown":"   ","metadata":{"statusCode":200}}}`))
	}))
	defer server.Close()

	prober := newProbeConnector(t)
	err := prober.Probe(t.Context(), value.SourceProviderFirecrawl, "fc-key", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "markdown")
}

func TestConnectionProberFirecrawlNon2xxFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	prober := newProbeConnector(t)
	err := prober.Probe(t.Context(), value.SourceProviderFirecrawl, "bad-key", server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestConnectionProberJinaReader(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("Title: Example\n\nURL Source: https://example.com\n\nMarkdown Content:\n# Example\n"))
	}))
	defer server.Close()

	prober := newProbeConnector(t)
	prober.jinaEndpoint = server.URL // 测试 seam：jina 连接无端点覆盖入口
	err := prober.Probe(t.Context(), value.SourceProviderJina, "jina-key", "")
	require.NoError(t, err)
	assert.Equal(t, "/https://example.com", gotPath)
	assert.Equal(t, "Bearer jina-key", gotAuth)

	// 非 2xx → 失败。
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer failing.Close()
	prober.jinaEndpoint = failing.URL
	err = prober.Probe(t.Context(), value.SourceProviderJina, "jina-key", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestConnectionProberBrowserHandshake(t *testing.T) {
	t.Parallel()

	prober := newProbeConnector(t)
	var gotEndpoint, gotToken string
	prober.testCDP = func(_ context.Context, endpointURL, token string, _ time.Duration) error {
		gotEndpoint = endpointURL
		gotToken = token
		return nil
	}
	require.NoError(t, prober.Probe(t.Context(), value.SourceProviderBrowser, "tok", "ws://cdp:9222"))
	assert.Equal(t, "ws://cdp:9222", gotEndpoint)
	assert.Equal(t, "tok", gotToken)

	prober.testCDP = func(context.Context, string, string, time.Duration) error {
		return errors.New("CDP 端点不可达")
	}
	err := prober.Probe(t.Context(), value.SourceProviderBrowser, "tok", "ws://cdp:9222")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "连接测试失败")

	// 缺 endpoint_url 的防御路径（连接层校验兜底）。
	prober.testCDP = func(context.Context, string, string, time.Duration) error { return nil }
	err = prober.Probe(t.Context(), value.SourceProviderBrowser, "", "  ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint_url")
}

func TestConnectionProberUnsupportedProvider(t *testing.T) {
	t.Parallel()

	prober := newProbeConnector(t)
	err := prober.Probe(t.Context(), value.SourceProviderFeishu, "s", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支持连接测试")
}

// TestTestCDPEndpointRejectsNonCDPServer 用普通 HTTP 服务冒充 CDP 端点：
// 握手必失败且错误脱敏（不含完整 URL 的 query）。
func TestTestCDPEndpointRejectsNonCDPServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not a cdp endpoint"))
	}))
	defer server.Close()

	err := TestCDPEndpoint(t.Context(), server.URL+"?secret=should-not-leak", "tok", time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CDP 端点不可达")
	assert.NotContains(t, err.Error(), "should-not-leak")
}
