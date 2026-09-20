package httpclient

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

// 网页抓取通道（spec §6.4）的 client：与 NewPublicHTTPSClient 共用同一份 SSRF 防护，
// 仅放开 scheme 判定（HTTP(S)）且不要求服务端点 BaseURL。

func TestPublicWebClientAcceptsPublicHTTPAndHTTPS(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{addresses: map[string][][]netip.Addr{
		"example.com":      {{netip.MustParseAddr("93.184.216.34")}},
		"docs.example.com": {{netip.MustParseAddr("93.184.216.35")}},
	}}
	cases := []struct {
		baseURL string
		port    string
		want    string
	}{
		{baseURL: "http://example.com/", port: "80", want: "93.184.216.34:80"},
		{baseURL: "https://docs.example.com/", port: "443", want: "93.184.216.35:443"},
	}

	for _, tc := range cases {
		t.Run(tc.baseURL, func(t *testing.T) {
			t.Parallel()

			dialer := &recordingDialer{}
			client, err := NewPublicWebClient(PublicClientConfig{
				BaseURL:     tc.baseURL,
				Resolver:    resolver,
				DialContext: dialer.DialContext,
			})
			if err != nil {
				t.Fatalf("NewPublicWebClient(%s) error = %v", tc.baseURL, err)
			}
			transport := publicTransportFromClient(t, client)
			if _, err := transport.dialContext(context.Background(), "tcp", hostOf(t, tc.baseURL)+":"+tc.port); err == nil {
				t.Fatal("expected recording dialer error")
			}
			if dialer.address != tc.want {
				t.Fatalf("dialed %q, want pinned public address %q", dialer.address, tc.want)
			}
		})
	}
}

func TestPublicWebClientAllowsEmptyBaseURL(t *testing.T) {
	t.Parallel()

	// 网页抓取没有单一服务端点：BaseURL 可空，安全保证来自逐请求校验与 dial 期校验。
	client, err := NewPublicWebClient(PublicClientConfig{Resolver: &fakeResolver{}})
	if err != nil {
		t.Fatalf("empty BaseURL must be accepted for web fetching: %v", err)
	}
	if client.Transport == nil {
		t.Fatal("transport must be configured")
	}

	// 模型服务端点（HTTPS-only）保持必填 BaseURL 的既有行为。
	if _, err := NewPublicHTTPSClient(PublicClientConfig{}); err == nil {
		t.Fatal("model endpoint client must still require BaseURL")
	}
}

func TestPublicWebClientRejectsInvalidTargetURLs(t *testing.T) {
	t.Parallel()

	tests := []string{
		"ftp://example.com/a",
		"file:///etc/passwd",
		"/relative",
		"example.com/a",
		"https://user:pass@example.com/",
		"https://example.com/path#fragment",
		"",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			target := mustParseURL(t, raw)
			if err := webFetchPolicy.validateURL(target); err == nil {
				t.Fatalf("validateURL accepted %q", raw)
			}
		})
	}

	// HTTP(S) 目标本身合法：scheme 判定的放开点。
	for _, raw := range []string{"http://example.com/a", "https://example.com/a"} {
		if err := webFetchPolicy.validateURL(mustParseURL(t, raw)); err != nil {
			t.Fatalf("validateURL(%s) error = %v", raw, err)
		}
	}
}

func TestPublicWebClientRejectsPrivateLiteralAddresses(t *testing.T) {
	t.Parallel()

	client, err := NewPublicWebClient(PublicClientConfig{Resolver: &fakeResolver{}})
	if err != nil {
		t.Fatal(err)
	}
	transport := publicTransportFromClient(t, client)

	tests := []string{
		"127.0.0.1:80",
		"10.0.0.8:80",
		"192.168.1.1:80",
		"169.254.169.254:80",
		"100.100.100.200:80",
		"0.0.0.0:80",
		"[::1]:80",
		"198.51.100.7:80",
	}
	for _, address := range tests {
		t.Run(address, func(t *testing.T) {
			if _, err := transport.dialContext(context.Background(), "tcp", address); err == nil ||
				!strings.Contains(err.Error(), "公网") {
				t.Fatalf("dial %s error = %v", address, err)
			}
		})
	}
}

func TestPublicWebClientRejectsPrivateDNSAnswers(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{addresses: map[string][][]netip.Addr{
		"private.example.com":  {{netip.MustParseAddr("10.0.0.9")}},
		"mixed.example.com":    {{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("192.168.1.8")}},
		"reserved.example.com": {{netip.MustParseAddr("203.0.113.10")}},
		"loopback.example.com": {{netip.MustParseAddr("127.0.0.1")}},
		"v6local.example.com":  {{netip.MustParseAddr("fe80::1")}},
	}}
	client, err := NewPublicWebClient(PublicClientConfig{Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	transport := publicTransportFromClient(t, client)

	for host := range resolver.addresses {
		if _, err := transport.dialContext(context.Background(), "tcp", host+":80"); err == nil ||
			!strings.Contains(err.Error(), "公网") {
			t.Fatalf("host %s error = %v", host, err)
		}
	}
}

func TestPublicWebClientRevalidatesRedirectHops(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{addresses: map[string][][]netip.Addr{
		"public.example.com":   {{netip.MustParseAddr("93.184.216.34")}},
		"private.example.com":  {{netip.MustParseAddr("10.0.0.5")}},
		"internal.example.com": {{netip.MustParseAddr("169.254.169.254")}},
	}}
	client, err := NewPublicWebClient(PublicClientConfig{Resolver: resolver, MaxRedirects: 3})
	if err != nil {
		t.Fatal(err)
	}

	// 公网 http/https 跳转允许（HTTP(S) 变体的差异点）。
	for _, raw := range []string{"http://public.example.com/next", "https://public.example.com/next"} {
		target := mustParseURL(t, raw)
		if err := client.CheckRedirect(newRequest(t, target), []*http.Request{{URL: target}}); err != nil {
			t.Fatalf("public redirect %s rejected: %v", raw, err)
		}
	}

	// 私网/链路本地跳转必须被逐跳重校验拦下。
	for _, raw := range []string{"http://private.example.com/", "https://internal.example.com/"} {
		target := mustParseURL(t, raw)
		if err := client.CheckRedirect(newRequest(t, target), []*http.Request{{URL: target}}); err == nil {
			t.Fatalf("private redirect accepted: %s", raw)
		}
	}

	// 重定向次数上限保持既有语义。
	over := mustParseURL(t, "https://public.example.com/next")
	if err := client.CheckRedirect(newRequest(t, over), []*http.Request{{}, {}, {}}); err == nil {
		t.Fatal("expected redirect limit rejection")
	}
}

func TestPublicHTTPSClientStillRejectsPlainHTTP(t *testing.T) {
	t.Parallel()

	// 对照：HTTPS-only 版本的行为不因新增变体而放宽。
	if _, err := NewPublicHTTPSClient(PublicClientConfig{BaseURL: "http://example.com/"}); err == nil {
		t.Fatal("model endpoint client must reject plain http")
	}
	if err := modelEndpointPolicy.validateURL(mustParseURL(t, "http://example.com/a")); err == nil {
		t.Fatal("model endpoint policy must reject plain http")
	}
	if err := modelEndpointPolicy.validateURL(mustParseURL(t, "https://example.com/a")); err != nil {
		t.Fatalf("model endpoint policy rejected https: %v", err)
	}
}

func TestValidateWebTarget(t *testing.T) {
	t.Parallel()

	// 合法目标：公网字面量（无需 DNS）。
	for _, raw := range []string{
		"http://93.184.216.34/a",
		"https://93.184.216.35/",
		"https://93.184.216.34:8443/path",
	} {
		if err := ValidateWebTarget(context.Background(), raw); err != nil {
			t.Fatalf("ValidateWebTarget(%s) error = %v", raw, err)
		}
	}

	// 拒绝项：非 http(s) scheme、相对地址、用户信息/fragment、私网/环回/链路本地字面量、
	// 无法解析的主机名。判定语义必须与 NewPublicWebClient 的逐请求校验一致。
	rejected := []string{
		"ftp://93.184.216.34/a",
		"/relative",
		"example.com/a",
		"",
		"https://user:pass@93.184.216.34/",
		"https://93.184.216.34/path#fragment",
		"http://127.0.0.1/",
		"http://10.0.0.8/",
		"http://192.168.1.1/",
		"http://169.254.169.254/",
		"http://100.100.100.200/",
		"http://0.0.0.0/",
		"http://[::1]/",
		"http://198.51.100.7/",
		"http://nonexistent.invalid/",
	}
	for _, raw := range rejected {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateWebTarget(context.Background(), raw); err == nil {
				t.Fatalf("ValidateWebTarget(%s) accepted, want rejection", raw)
			}
		})
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return parsed
}

func newRequest(t *testing.T, target *url.URL) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target.String(), nil)
	if err != nil {
		t.Fatalf("build request %q: %v", target, err)
	}
	return request
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()

	target := mustParseURL(t, raw)
	host := target.Hostname()
	if host == "" {
		t.Fatalf("no host in %q", raw)
	}
	return host
}
