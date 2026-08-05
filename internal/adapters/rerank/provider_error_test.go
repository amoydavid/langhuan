package rerank_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	rerankadapter "github.com/dajee/langhuan/internal/adapters/rerank"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestProviderErrorMapsWithoutUpstreamBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind rerankadapter.ProviderErrorKind
		want error
	}{
		{rerankadapter.ProviderErrorRateLimited, domainerrors.ErrRerankRateLimited},
		{rerankadapter.ProviderErrorTimeout, domainerrors.ErrRequestTimeout},
		{rerankadapter.ProviderErrorUnreachable, domainerrors.ErrRerankUnavailable},
		{rerankadapter.ProviderErrorInvalidResponse, domainerrors.ErrInvalidRerankResponse},
		{rerankadapter.ProviderErrorInputTooLarge, domainerrors.ErrRerankInputTooLarge},
		{rerankadapter.ProviderErrorAuthentication, domainerrors.ErrAuthenticationFailed},
	}
	for _, tt := range tests {
		err := rerankadapter.NewProviderError("rerank_compatible", tt.kind)
		if !errors.Is(err, tt.want) {
			t.Fatalf("kind %q error = %v, want %v", tt.kind, err, tt.want)
		}
		if strings.Contains(err.Error(), "upstream secret body") {
			t.Fatalf("provider error leaked details: %v", err)
		}
	}
}

func TestSanitizeProviderErrorUsesStableClassWithoutOriginalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{"deadline", context.DeadlineExceeded, domainerrors.ErrRequestTimeout},
		{"dns", &net.DNSError{Err: "upstream secret body", Name: "provider.example"}, domainerrors.ErrRerankUnavailable},
		{"generic", errors.New("upstream secret body"), domainerrors.ErrRerankUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rerankadapter.SanitizeProviderError("rerank_compatible", tt.err)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if strings.Contains(err.Error(), "upstream secret body") {
				t.Fatalf("provider error leaked details: %v", err)
			}
		})
	}
}

func TestSanitizeProviderErrorPreservesContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := rerankadapter.SanitizeProviderError("rerank_compatible", ctx.Err())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancel must be preserved, got %v", err)
	}
}

func TestProviderErrorClassifiesHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		kind   rerankadapter.ProviderErrorKind
	}{
		{401, rerankadapter.ProviderErrorAuthentication},
		{403, rerankadapter.ProviderErrorAuthentication},
		{408, rerankadapter.ProviderErrorTimeout},
		{429, rerankadapter.ProviderErrorRateLimited},
		{413, rerankadapter.ProviderErrorInputTooLarge},
		{400, rerankadapter.ProviderErrorRejected},
		{500, rerankadapter.ProviderErrorRejected},
	}
	for _, tt := range tests {
		if got := rerankadapter.ProviderErrorKindForHTTPStatus(tt.status); got != tt.kind {
			t.Fatalf("status %d kind = %q, want %q", tt.status, got, tt.kind)
		}
	}
}
