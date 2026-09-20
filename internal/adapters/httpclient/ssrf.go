package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Resolver resolves endpoint host names for SSRF validation.
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// DialContextFunc is the network dial function used after address validation.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// PublicClientConfig configures a Workspace-owned public client.
type PublicClientConfig struct {
	// BaseURL 在 requireBaseURL 的策略下必填，用于装配期尽早校验端点；
	// 网页抓取没有单一服务端点（target 由 connector 逐个给出），可为空。
	BaseURL      string
	Timeout      time.Duration
	MaxRedirects int
	Headers      map[string]string
	Resolver     Resolver
	DialContext  DialContextFunc
}

// publicClientPolicy 描述公网 client 的 scheme 策略与错误文案主体。
//
// 两类调用方共用同一份 SSRF 防护（dial 期 IP 校验 + 结果 IP 钉住 + redirect 逐跳
// 重校验 + 私网/环回/链路本地/保留地址阻断），差异只在 scheme 判定与文案：
//   - 模型服务端点（modelEndpointPolicy）：仅公网 HTTPS，有确定的服务端点 BaseURL；
//   - 网页抓取目标（webFetchPolicy）：公网 HTTP(S)，无单一服务端点。
//
// 放开 scheme 不削弱 SSRF 强度：私网地址在 IP 层被阻断，与 scheme 无关；
// 代价是明文抓取内容可能被中间人篡改——公开网页本身即公开内容，与主流爬虫一致。
type publicClientPolicy struct {
	// allowPlainHTTP 为真时接受 http scheme。
	allowPlainHTTP bool
	// requireBaseURL 为真时构造期必须提供可校验的 BaseURL。
	requireBaseURL bool
	// subject 是错误文案里对目标地址的称谓（如 "Workspace 模型服务地址"）。
	subject string
	// service 是解析/域名/重定向相关错误文案里的主体称谓（如 "模型服务"）。
	service string
}

var (
	// modelEndpointPolicy 是 Workspace 模型服务端点的策略：仅公网 HTTPS。
	modelEndpointPolicy = publicClientPolicy{
		requireBaseURL: true,
		subject:        "Workspace 模型服务地址",
		service:        "模型服务",
	}
	// webFetchPolicy 是网页抓取目标的策略：公网 HTTP(S)（spec §6.4）。
	webFetchPolicy = publicClientPolicy{
		allowPlainHTTP: true,
		subject:        "抓取地址",
		service:        "抓取目标",
	}
)

// schemeRequirement 返回文案里对 scheme 的要求描述。
func (p publicClientPolicy) schemeRequirement() string {
	if p.allowPlainHTTP {
		return "HTTP(S)"
	}
	return "HTTPS"
}

// validateURL 校验目标 URL：必须是绝对的公网 HTTP(S) URL、无用户信息与 fragment、
// 端口合法。scheme 判定按策略区分（HTTPS-only / HTTP(S)）。
func (p publicClientPolicy) validateURL(target *url.URL) error {
	publicScheme := target != nil &&
		(strings.EqualFold(target.Scheme, "https") ||
			(p.allowPlainHTTP && strings.EqualFold(target.Scheme, "http")))
	if target == nil || !target.IsAbs() || !publicScheme || target.Hostname() == "" {
		return fmt.Errorf("%s必须是绝对公网 %s URL", p.subject, p.schemeRequirement())
	}
	if target.User != nil || target.Fragment != "" {
		return fmt.Errorf("%s不能包含用户信息或 fragment", p.subject)
	}
	if port := target.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("%s端口无效", p.service)
		}
	}
	return nil
}

type publicTransport struct {
	next          *http.Transport
	resolver      Resolver
	dialContextFn DialContextFunc
	policy        publicClientPolicy
}

func (t *publicTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := t.policy.validateURL(request.URL); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(request)
}

func (t *publicTransport) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("解析%s地址失败: %w", t.policy.service, err)
	}
	addresses, err := t.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	return t.dialContextFn(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}

func (t *publicTransport) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		if err := validatePublicAddress(literal, t.policy); err != nil {
			return nil, err
		}
		return []netip.Addr{literal.Unmap()}, nil
	}
	addresses, err := t.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("解析%s域名失败: %w", t.policy.service, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%s域名没有可用的公网地址", t.policy.service)
	}
	for index, address := range addresses {
		addresses[index] = address.Unmap()
		if err := validatePublicAddress(addresses[index], t.policy); err != nil {
			return nil, err
		}
	}
	return addresses, nil
}

// NewPublicHTTPSClient creates an HTTPS-only client that resolves and validates
// every dial and pins the connection to the validated public IP address.
func NewPublicHTTPSClient(config PublicClientConfig) (*http.Client, error) {
	return newPublicClient(config, modelEndpointPolicy)
}

// NewPublicWebClient creates a public HTTP(S) client for web content fetching
// (spec §6.4) with the same SSRF protection as NewPublicHTTPSClient:
//
//   - every dial re-resolves and validates the target address (private, loopback,
//     link-local, multicast and reserved ranges are rejected), and the connection is
//     pinned to the validated public IP;
//   - every redirect hop is re-validated before it is followed.
//
// It differs from NewPublicHTTPSClient in exactly two ways: it also accepts the
// plain http scheme, and BaseURL is optional (web fetching has no single service
// endpoint — each target URL is validated per request and per dial). Both clients
// coexist: model-service callers keep the HTTPS-only guarantee, while §5.1/§3.3
// require crawling to accept http URLs (non-http(s) schemes are skipped upstream).
func NewPublicWebClient(config PublicClientConfig) (*http.Client, error) {
	return newPublicClient(config, webFetchPolicy)
}

func newPublicClient(config PublicClientConfig, policy publicClientPolicy) (*http.Client, error) {
	if config.BaseURL != "" || policy.requireBaseURL {
		parsed, err := url.Parse(config.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("%s无效: %w", policy.service, err)
		}
		if err := policy.validateURL(parsed); err != nil {
			return nil, err
		}
		if literal, err := netip.ParseAddr(parsed.Hostname()); err == nil {
			if err := validatePublicAddress(literal, policy); err != nil {
				return nil, err
			}
		}
	}
	resolver := config.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialContext := config.DialContext
	if dialContext == nil {
		dialContext = (&net.Dialer{}).DialContext
	}
	transport := &publicTransport{resolver: resolver, dialContextFn: dialContext, policy: policy}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.DialContext = transport.dialContext
	base.TLSClientConfig = cloneTLSConfig(base.TLSClientConfig)
	transport.next = base
	headerTransport, err := newHeaderRoundTripper(transport, config.Headers)
	if err != nil {
		return nil, err
	}
	maxRedirects := config.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 5
	}
	client := &http.Client{Transport: headerTransport, Timeout: config.Timeout}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("%s重定向次数超过限制", policy.service)
		}
		if err := policy.validateURL(request.URL); err != nil {
			return err
		}
		_, err := transport.resolve(request.Context(), request.URL.Hostname())
		return err
	}
	return client, nil
}

// ValidateWebTarget 校验网页抓取目标（spec §6.4）：必须是绝对公网 HTTP(S) URL、
// 无用户信息与 fragment，且 host 解析到的全部地址均为公网（私网/环回/链路本地/
// 组播/保留地址拒绝）。判定语义与 NewPublicWebClient 的 dial 期防护完全一致。
//
// 供不经本包 dial 防护的通道做发送/导航前预校验：render 引擎（导航前琅嬛侧
// host 拒绝）与 firecrawl/jina 引擎（把目标 URL 交给 provider 前预校验）。
// 这是这些通道仅存的 SSRF 缓解，属降级语义——浏览器进程侧子请求与 provider
// 基础设施内的后续请求不受本检查约束，不得声称端到端保证。
func ValidateWebTarget(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("抓取地址无效: %w", err)
	}
	if err := webFetchPolicy.validateURL(parsed); err != nil {
		return err
	}
	transport := &publicTransport{resolver: net.DefaultResolver, policy: webFetchPolicy}
	_, err = transport.resolve(ctx, parsed.Hostname())
	return err
}

func cloneTLSConfig(config *tls.Config) *tls.Config {
	if config == nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	clone := config.Clone()
	if clone.MinVersion < tls.VersionTLS12 {
		clone.MinVersion = tls.VersionTLS12
	}
	return clone
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func validatePublicAddress(address netip.Addr, policy publicClientPolicy) error {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return fmt.Errorf("%s地址必须解析到公网 IP", policy.service)
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return fmt.Errorf("%s地址必须解析到公网 IP", policy.service)
		}
	}
	return nil
}
