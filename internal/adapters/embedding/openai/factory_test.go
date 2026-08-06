package openai

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

func TestOpenAIProviderModesAndCredentials(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	for _, input := range []embeddingport.ProviderDecodeInput{
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"mode":"standard","base_url":"https://api.example.com/v1","timeout_seconds":60}`), Credentials: json.RawMessage(`{"api_key":"secret","custom_headers":{"X-Tenant":"acme"}}`)},
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"mode":"azure","base_url":"https://acme.openai.azure.com","api_version":"2025-04-01-preview","timeout_seconds":60}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)},
	} {
		if _, _, err := factory.DecodeProvider(input); err != nil {
			t.Fatalf("DecodeProvider() error = %v", err)
		}
	}
	if fields := factory.CredentialFields(); len(fields) != 2 || fields[0] != "api_key" || fields[1] != "custom_headers" {
		t.Fatalf("credential fields = %#v", fields)
	}
}

func TestOpenAIFactorySupportsExplicitProviderKey(t *testing.T) {
	t.Parallel()
	if got := NewFactoryWithProvider("siliconflow").Provider(); got != "siliconflow" {
		t.Fatalf("provider = %q", got)
	}
}

func TestOpenAINewClientBuildsNativeEmbedder(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	config, credentials, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"mode":"standard","base_url":"https://api.example.com/v1","timeout_seconds":60}`), Credentials: json.RawMessage(`{"api_key":"secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "text-embedding-3-large", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":32}`)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), embeddingport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopeWorkspace, Config: config, CredentialsJSON: credentials, ModelName: "text-embedding-3-large", Dimensions: 1024, Parameters: parameters})
	if err != nil {
		t.Fatal(err)
	}
	if client.Dimension() != 1024 {
		t.Fatalf("dimension = %d", client.Dimension())
	}
}

func TestOpenAIRejectsUnknownFieldsAndInvalidAzureConfig(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	for _, input := range []embeddingport.ProviderDecodeInput{
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"mode":"standard","unknown":true}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)},
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"mode":"azure","base_url":"https://azure.example.com"}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)},
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"mode":"standard"}`), Credentials: json.RawMessage(`{}`)},
	} {
		_, _, err := factory.DecodeProvider(input)
		if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) && !errors.Is(err, domainerrors.ErrCredentialsRequired) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestOpenAIModelContract(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "text-embedding-3-large", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":32}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "text-embedding-3-large", Dimensions: 1536, Parameters: json.RawMessage(`{"batch_size":32}`)}); !errors.Is(err, domainerrors.ErrUnsupportedEmbeddingDimension) {
		t.Fatalf("dimension error = %v", err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":0}`)}); !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("model error = %v", err)
	}
}
