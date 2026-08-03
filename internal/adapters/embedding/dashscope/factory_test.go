package dashscope

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	"github.com/google/uuid"
)

func TestDashScopeProviderContract(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, _, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"timeout_seconds":60}`), Credentials: json.RawMessage(`{"api_key":"secret"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"endpoint":"https://example.com"}`), Credentials: json.RawMessage(`{"api_key":"secret"}`),
	}); !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("error = %v", err)
	}
}

func TestDashScopeNewClientBuildsNativeEmbedder(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	config, credentials, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"timeout_seconds":60}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "text-embedding-v4", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":32}`)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), embeddingport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopeWorkspace, Config: config, CredentialsJSON: credentials, ModelName: "text-embedding-v4", Dimensions: 1024, Parameters: parameters})
	if err != nil {
		t.Fatal(err)
	}
	if client.Dimension() != 1024 {
		t.Fatalf("dimension = %d", client.Dimension())
	}
}

func TestDashScopeOnlyAccepts1024(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "text-embedding-v4", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":32}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "text-embedding-v4", Dimensions: 2048, Parameters: json.RawMessage(`{"batch_size":32}`)}); !errors.Is(err, domainerrors.ErrUnsupportedEmbeddingDimension) {
		t.Fatalf("error = %v", err)
	}
}
