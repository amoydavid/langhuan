package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	mu        sync.Mutex
	addresses map[string][][]netip.Addr
	calls     map[string]int
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string]int{}
	}
	sequence := r.addresses[host]
	if len(sequence) == 0 {
		return nil, errors.New("host not found")
	}
	index := r.calls[host]
	if index >= len(sequence) {
		index = len(sequence) - 1
	}
	r.calls[host]++
	return append([]netip.Addr(nil), sequence[index]...), nil
}

type recordingDialer struct {
	mu      sync.Mutex
	address string
}

func (d *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.mu.Lock()
	d.address = address
	d.mu.Unlock()
	return nil, errors.New("dial stopped by test")
}

func TestPublicHTTPSClientPinsResolvedPublicAddress(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{addresses: map[string][][]netip.Addr{
		"models.example.com": {{netip.MustParseAddr("93.184.216.34")}},
	}}
	dialer := &recordingDialer{}
	client, err := NewPublicHTTPSClient(PublicClientConfig{
		BaseURL:     "https://models.example.com/v1",
		Timeout:     time.Second,
		Resolver:    resolver,
		DialContext: dialer.DialContext,
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := publicTransportFromClient(t, client)
	_, err = transport.dialContext(context.Background(), "tcp", "models.example.com:443")
	if err == nil {
		t.Fatal("expected recording dialer error")
	}
	if dialer.address != "93.184.216.34:443" {
		t.Fatalf("dialed %q, want pinned public address", dialer.address)
	}
}

func TestPublicHTTPSClientRejectsInvalidAndLiteralPrivateTargets(t *testing.T) {
	t.Parallel()

	tests := []string{
		"http://public.example.com",
		"https://127.0.0.1",
		"https://10.0.0.8",
		"https://169.254.169.254/latest",
		"https://100.100.100.200/latest",
		"https://[::1]",
		"https://user:pass@public.example.com",
		"https://public.example.com/path#fragment",
		"ftp://public.example.com",
		"/relative",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewPublicHTTPSClient(PublicClientConfig{BaseURL: raw}); err == nil {
				t.Fatalf("accepted %s", raw)
			}
		})
	}
}

func TestPublicHTTPSClientRejectsPrivateDNSAndMixedAnswers(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{addresses: map[string][][]netip.Addr{
		"private.example.com":  {{netip.MustParseAddr("10.0.0.9")}},
		"mixed.example.com":    {{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("192.168.1.8")}},
		"reserved.example.com": {{netip.MustParseAddr("203.0.113.10")}},
	}}
	for _, host := range []string{"private.example.com", "mixed.example.com", "reserved.example.com"} {
		client, err := NewPublicHTTPSClient(PublicClientConfig{
			BaseURL:     "https://" + host,
			Resolver:    resolver,
			DialContext: (&recordingDialer{}).DialContext,
		})
		if err != nil {
			t.Fatal(err)
		}
		transport := publicTransportFromClient(t, client)
		if _, err := transport.dialContext(context.Background(), "tcp", host+":443"); err == nil || !strings.Contains(err.Error(), "公网") {
			t.Fatalf("host %s error = %v", host, err)
		}
	}
}

func TestPublicHTTPSClientRevalidatesEveryDialAndRedirect(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{addresses: map[string][][]netip.Addr{
		"rebinding.example.com": {
			{netip.MustParseAddr("93.184.216.34")},
			{netip.MustParseAddr("127.0.0.1")},
		},
		"redirect.example.com": {{netip.MustParseAddr("10.0.0.5")}},
	}}
	dialer := &recordingDialer{}
	client, err := NewPublicHTTPSClient(PublicClientConfig{
		BaseURL:      "https://rebinding.example.com",
		Resolver:     resolver,
		DialContext:  dialer.DialContext,
		MaxRedirects: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := publicTransportFromClient(t, client)
	_, _ = transport.dialContext(context.Background(), "tcp", "rebinding.example.com:443")
	if _, err := transport.dialContext(context.Background(), "tcp", "rebinding.example.com:443"); err == nil || !strings.Contains(err.Error(), "公网") {
		t.Fatalf("rebinding error = %v", err)
	}

	redirectRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://redirect.example.com/path", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(redirectRequest, []*http.Request{{URL: redirectRequest.URL}}); err == nil {
		t.Fatal("expected private redirect rejection")
	}
	publicRequest, _ := http.NewRequest(http.MethodGet, "https://rebinding.example.com", nil)
	if err := client.CheckRedirect(publicRequest, []*http.Request{{}, {}, {}}); err == nil {
		t.Fatal("expected redirect limit rejection")
	}
}

func publicTransportFromClient(t *testing.T, client *http.Client) *publicTransport {
	t.Helper()
	headerTransport, ok := client.Transport.(*headerRoundTripper)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	transport, ok := headerTransport.next.(*publicTransport)
	if !ok {
		t.Fatalf("next transport = %T", headerTransport.next)
	}
	return transport
}
