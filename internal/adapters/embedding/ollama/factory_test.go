package ollama

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

func TestOllamaRejectsWorkspaceScope(t *testing.T) {
	t.Parallel()
	_, _, err := NewFactory().DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"base_url":"https://ollama.example.com","timeout_seconds":60}`), Credentials: json.RawMessage(`{}`),
	})
	if !errors.Is(err, domainerrors.ErrProviderScopeNotAllowed) {
		t.Fatalf("error = %v", err)
	}
}

func TestOllamaNewClientBuildsNativeEmbedder(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	config, credentials, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{Scope: value.ModelScopePlatform, Config: json.RawMessage(`{"base_url":"http://127.0.0.1:11434","timeout_seconds":60}`), Credentials: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "bge-m3", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":16}`)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), embeddingport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopePlatform, Config: config, CredentialsJSON: credentials, ModelName: "bge-m3", Dimensions: 1024, Parameters: parameters})
	if err != nil {
		t.Fatal(err)
	}
	if client.Dimension() != 1024 {
		t.Fatalf("dimension = %d", client.Dimension())
	}
}

func TestOllamaPlatformContract(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, _, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopePlatform, Config: json.RawMessage(`{"base_url":"http://127.0.0.1:11434","timeout_seconds":60}`), Credentials: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopePlatform, Config: json.RawMessage(`{"base_url":"http://127.0.0.1:11434","extra":true}`), Credentials: json.RawMessage(`{}`),
	}); !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("unknown field error = %v", err)
	}
	if fields := factory.CredentialFields(); len(fields) != 0 {
		t.Fatalf("credential fields = %#v", fields)
	}
}

func TestOllamaModelParameters(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "bge-m3", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":16,"truncate":true,"keep_alive_seconds":300}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "bge-m3", Dimensions: 1024, Parameters: json.RawMessage(`{"keep_alive_seconds":-2}`)}); !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("error = %v", err)
	}
}
