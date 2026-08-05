package requestmeta_test

import (
	"context"
	"testing"

	"github.com/dajee/langhuan/internal/application/requestmeta"
)

func TestRequestMetaRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := requestmeta.With(context.Background(), requestmeta.Meta{
		RequestID: "req-1", Transport: "rest", PrincipalKind: "user",
	})
	got := requestmeta.From(ctx)
	if got.RequestID != "req-1" || got.Transport != "rest" || got.PrincipalKind != "user" {
		t.Fatalf("got = %#v", got)
	}
}

func TestRequestMetaFromEmptyContext(t *testing.T) {
	t.Parallel()
	got := requestmeta.From(context.Background())
	if got != (requestmeta.Meta{}) {
		t.Fatalf("got = %#v", got)
	}
}

func TestRequestMetaWithZeroValueIsNoop(t *testing.T) {
	t.Parallel()
	ctx := requestmeta.With(context.Background(), requestmeta.Meta{})
	if requestmeta.From(ctx) != (requestmeta.Meta{}) {
		t.Fatal("zero meta should be noop")
	}
}
