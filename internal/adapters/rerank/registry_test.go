package rerank_test

import (
	"context"
	"errors"
	"testing"

	rerankadapter "github.com/dajee/langhuan/internal/adapters/rerank"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

type fakeFactory struct {
	provider string
}

func (f fakeFactory) Provider() string { return f.provider }
func (f fakeFactory) CredentialFields() []string {
	return []string{"api_key"}
}
func (f fakeFactory) DecodeProvider(rerankport.ProviderDecodeInput) (map[string]any, []byte, error) {
	return map[string]any{}, []byte(`{"api_key":"secret"}`), nil
}
func (f fakeFactory) DecodeModel(rerankport.ModelDecodeInput) (map[string]any, error) {
	return map[string]any{"max_documents": 100}, nil
}
func (f fakeFactory) NewClient(context.Context, rerankport.ClientInput) (rerankport.Client, error) {
	return nil, nil
}

func TestRegistryNormalizesProviderAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	registry, err := rerankadapter.NewRegistry(fakeFactory{provider: " RERANK_COMPATIBLE "})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Factory("rerank_compatible"); err != nil {
		t.Fatal(err)
	}

	if _, err := rerankadapter.NewRegistry(
		fakeFactory{provider: "rerank_compatible"},
		fakeFactory{provider: "RERANK_COMPATIBLE"},
	); err == nil {
		t.Fatal("duplicate must fail")
	}
}

func TestRegistryRejectsEmptyAndNil(t *testing.T) {
	t.Parallel()

	if _, err := rerankadapter.NewRegistry(fakeFactory{provider: "  "}); err == nil {
		t.Fatal("empty provider must fail")
	}
	var nilFactory rerankport.Factory
	if _, err := rerankadapter.NewRegistry(nilFactory); err == nil {
		t.Fatal("nil factory must fail")
	}
}

func TestRegistryFactoryUnknownProviderIsUnsupported(t *testing.T) {
	t.Parallel()

	registry, err := rerankadapter.NewRegistry(fakeFactory{provider: "rerank_compatible"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Factory("cohere")
	if !errors.Is(err, domainerrors.ErrUnsupportedProvider) {
		t.Fatalf("unknown provider error = %v", err)
	}
}
