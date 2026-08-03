package embedding

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestProviderErrorMapsStableKindsWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind ProviderErrorKind
		want error
	}{
		{ProviderErrorAuthentication, domainerrors.ErrAuthenticationFailed},
		{ProviderErrorTimeout, domainerrors.ErrRequestTimeout},
		{ProviderErrorRateLimited, domainerrors.ErrRateLimited},
		{ProviderErrorRejected, domainerrors.ErrProviderRejected},
		{ProviderErrorUnreachable, domainerrors.ErrEndpointUnreachable},
		{ProviderErrorInvalidResponse, domainerrors.ErrInvalidEmbeddingResponse},
	}

	for _, tt := range tests {
		err := NewProviderError("openai", tt.kind)
		if !errors.Is(err, tt.want) {
			t.Fatalf("kind %q error = %v, want %v", tt.kind, err, tt.want)
		}
		if strings.Contains(err.Error(), "secret-response-body") {
			t.Fatalf("provider error leaked details: %v", err)
		}
	}
}

func TestSanitizeProviderErrorUsesStableClassWithoutOriginalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want error
	}{
		{context.DeadlineExceeded, domainerrors.ErrRequestTimeout},
		{&net.DNSError{Err: "secret-response-body", Name: "provider.example"}, domainerrors.ErrEndpointUnreachable},
		{errors.New("secret-response-body"), domainerrors.ErrProviderRejected},
	}
	for _, tt := range tests {
		err := SanitizeProviderError("openai", tt.err)
		if !errors.Is(err, tt.want) {
			t.Fatalf("error = %v, want %v", err, tt.want)
		}
		if strings.Contains(err.Error(), "secret-response-body") {
			t.Fatalf("provider error leaked details: %v", err)
		}
	}
}

func TestProviderErrorClassifiesHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		kind   ProviderErrorKind
	}{
		{401, ProviderErrorAuthentication},
		{403, ProviderErrorAuthentication},
		{408, ProviderErrorTimeout},
		{429, ProviderErrorRateLimited},
		{400, ProviderErrorRejected},
		{500, ProviderErrorRejected},
	}
	for _, tt := range tests {
		if got := ProviderErrorKindForHTTPStatus(tt.status); got != tt.kind {
			t.Fatalf("status %d kind = %q, want %q", tt.status, got, tt.kind)
		}
	}
}
