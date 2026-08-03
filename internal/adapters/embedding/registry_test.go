package embedding

import (
	"context"
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

type fakeFactory struct {
	provider string
}

func (f fakeFactory) Provider() string           { return f.provider }
func (f fakeFactory) CredentialFields() []string { return []string{"api_key"} }
func (f fakeFactory) DecodeProvider(embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	return map[string]any{}, []byte(`{}`), nil
}
func (f fakeFactory) DecodeModel(embeddingport.ModelDecodeInput) (map[string]any, error) {
	return map[string]any{}, nil
}
func (f fakeFactory) NewClient(context.Context, embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	return nil, nil
}

func TestRegistryUsesModelTypeAndProviderKey(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(fakeFactory{provider: "OpenAI"})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := registry.Factory(value.ModelTypeEmbedding, " OPENAI ")
	if err != nil {
		t.Fatal(err)
	}
	if factory.Provider() != "OpenAI" {
		t.Fatalf("factory provider = %q", factory.Provider())
	}

	if _, err := registry.Factory(value.ModelTypeEmbedding, "qianfan"); !errors.Is(err, domainerrors.ErrUnsupportedProvider) {
		t.Fatalf("qianfan error = %v", err)
	}
	if _, err := registry.Factory(value.ModelTypeLLM, "openai"); !errors.Is(err, domainerrors.ErrUnsupportedModelType) {
		t.Fatalf("llm error = %v", err)
	}
}

func TestRegistryRejectsDuplicateAndEmptyFactories(t *testing.T) {
	t.Parallel()

	if _, err := NewRegistry(fakeFactory{provider: "openai"}, fakeFactory{provider: "OPENAI"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	if _, err := NewRegistry(fakeFactory{}); err == nil {
		t.Fatal("expected empty provider error")
	}
	var nilFactory embeddingport.Factory
	if _, err := NewRegistry(nilFactory); err == nil {
		t.Fatal("expected nil factory error")
	}
}
