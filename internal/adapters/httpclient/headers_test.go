package httpclient

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCustomHeadersAreClonedAndInjected(t *testing.T) {
	t.Parallel()

	original, err := http.NewRequest(http.MethodGet, "https://models.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	original.Header.Set("X-Request", "original")
	transport, err := newHeaderRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer configured" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("X-Request") != "original" {
			t.Fatalf("request header = %q", request.Header.Get("X-Request"))
		}
		request.Header.Set("X-Request", "mutated")
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	}), map[string]string{"Authorization": "Bearer configured"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(original); err != nil {
		t.Fatal(err)
	}
	if original.Header.Get("Authorization") != "" || original.Header.Get("X-Request") != "original" {
		t.Fatalf("original request mutated: %#v", original.Header)
	}
}

func TestCustomHeadersRejectRoutingAndHopByHopFields(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"Host", "Content-Length", "Connection", "Proxy-Authorization", "Proxy-Authenticate",
		"Transfer-Encoding", "Upgrade", "TE", "Trailer",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newHeaderRoundTripper(http.DefaultTransport, map[string]string{name: "value"}); err == nil {
				t.Fatalf("accepted forbidden header %s", name)
			}
		})
	}
	if _, err := newHeaderRoundTripper(http.DefaultTransport, map[string]string{"X-Test": "safe\r\nInjected: yes"}); err == nil {
		t.Fatal("accepted CRLF header value")
	}
}

func TestTrustedClientAllowsPlatformHTTPButStillValidatesHeaders(t *testing.T) {
	t.Parallel()

	client, err := NewTrustedClient(time.Second, map[string]string{"X-Platform": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != time.Second {
		t.Fatalf("timeout = %v", client.Timeout)
	}
	if _, err := NewTrustedClient(time.Second, map[string]string{"Host": "private"}); err == nil {
		t.Fatal("trusted client must still reject Host override")
	}
}
