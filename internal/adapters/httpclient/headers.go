// Package httpclient provides HTTP clients with endpoint and header policies.
package httpclient

import (
	"fmt"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

var forbiddenHeaders = map[string]struct{}{
	"host":                {},
	"content-length":      {},
	"connection":          {},
	"proxy-authorization": {},
	"proxy-authenticate":  {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"te":                  {},
	"trailer":             {},
}

type headerRoundTripper struct {
	next    http.RoundTripper
	headers http.Header
}

func newHeaderRoundTripper(next http.RoundTripper, headers map[string]string) (*headerRoundTripper, error) {
	if next == nil {
		next = http.DefaultTransport
	}
	validated := make(http.Header, len(headers))
	for name, value := range headers {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		if canonical == "" || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("自定义 HTTP Header 无效: %q", name)
		}
		if _, forbidden := forbiddenHeaders[strings.ToLower(name)]; forbidden {
			return nil, fmt.Errorf("不允许设置 HTTP Header: %s", canonical)
		}
		validated.Set(canonical, value)
	}
	return &headerRoundTripper{next: next, headers: validated}, nil
}

func (t *headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		clone.Header.Del(name)
		for _, value := range values {
			clone.Header.Add(name, value)
		}
	}
	return t.next.RoundTrip(clone)
}

// NewTrustedClient creates a client for platform-managed endpoints. It permits
// local and plain HTTP targets, but applies the same custom-header restrictions.
func NewTrustedClient(timeout time.Duration, headers map[string]string) (*http.Client, error) {
	transport, err := newHeaderRoundTripper(http.DefaultTransport, headers)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
