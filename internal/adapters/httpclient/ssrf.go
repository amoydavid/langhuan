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

// PublicClientConfig configures a Workspace-owned public HTTPS client.
type PublicClientConfig struct {
	BaseURL      string
	Timeout      time.Duration
	MaxRedirects int
	Headers      map[string]string
	Resolver     Resolver
	DialContext  DialContextFunc
}

type publicTransport struct {
	next          *http.Transport
	resolver      Resolver
	dialContextFn DialContextFunc
}

func (t *publicTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := validatePublicHTTPSURL(request.URL); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(request)
}

func (t *publicTransport) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("解析模型服务地址失败: %w", err)
	}
	addresses, err := t.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	return t.dialContextFn(ctx, network, net.JoinHostPort(addresses[0].String(), port))
}

func (t *publicTransport) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		if err := validatePublicAddress(literal); err != nil {
			return nil, err
		}
		return []netip.Addr{literal.Unmap()}, nil
	}
	addresses, err := t.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("解析模型服务域名失败: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("模型服务域名没有可用的公网地址")
	}
	for index, address := range addresses {
		addresses[index] = address.Unmap()
		if err := validatePublicAddress(address); err != nil {
			return nil, err
		}
	}
	return addresses, nil
}

// NewPublicHTTPSClient creates an HTTPS-only client that resolves and validates
// every dial and pins the connection to the validated public IP address.
func NewPublicHTTPSClient(config PublicClientConfig) (*http.Client, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("模型服务地址无效: %w", err)
	}
	if err := validatePublicHTTPSURL(parsed); err != nil {
		return nil, err
	}
	if literal, err := netip.ParseAddr(parsed.Hostname()); err == nil {
		if err := validatePublicAddress(literal); err != nil {
			return nil, err
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
	transport := &publicTransport{resolver: resolver, dialContextFn: dialContext}
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
			return fmt.Errorf("模型服务重定向次数超过限制")
		}
		if err := validatePublicHTTPSURL(request.URL); err != nil {
			return err
		}
		_, err := transport.resolve(request.Context(), request.URL.Hostname())
		return err
	}
	return client, nil
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

func validatePublicHTTPSURL(target *url.URL) error {
	if target == nil || !target.IsAbs() || !strings.EqualFold(target.Scheme, "https") || target.Hostname() == "" {
		return fmt.Errorf("Workspace 模型服务地址必须是绝对公网 HTTPS URL")
	}
	if target.User != nil || target.Fragment != "" {
		return fmt.Errorf("Workspace 模型服务地址不能包含用户信息或 fragment")
	}
	if port := target.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("模型服务端口无效")
		}
	}
	return nil
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

func validatePublicAddress(address netip.Addr) error {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return fmt.Errorf("模型服务地址必须解析到公网 IP")
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return fmt.Errorf("模型服务地址必须解析到公网 IP")
		}
	}
	return nil
}
