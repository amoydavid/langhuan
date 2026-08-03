package ark

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

func TestARKAcceptsAPIKeyAndAKSKModes(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	for _, input := range []embeddingport.ProviderDecodeInput{
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"base_url":"https://ark.example.com/api/v3","region":"cn-beijing","auth_mode":"api_key","timeout_seconds":60,"retry_times":2}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)},
		{Scope: value.ModelScopePlatform, Config: json.RawMessage(`{"region":"cn-beijing","auth_mode":"ak_sk","timeout_seconds":60,"retry_times":2}`), Credentials: json.RawMessage(`{"access_key":"ak","secret_key":"sk"}`)},
	} {
		if _, _, err := factory.DecodeProvider(input); err != nil {
			t.Fatalf("DecodeProvider() error = %v", err)
		}
	}
}

func TestARKNewClientBuildsNativeEmbedder(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	config, credentials, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{Scope: value.ModelScopePlatform, Config: json.RawMessage(`{"auth_mode":"api_key","timeout_seconds":60,"retry_times":2}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "ep-123", Dimensions: 1024, Parameters: json.RawMessage(`{"batch_size":32}`)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), embeddingport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopePlatform, Config: config, CredentialsJSON: credentials, ModelName: "ep-123", Dimensions: 1024, Parameters: parameters})
	if err != nil {
		t.Fatal(err)
	}
	if client.Dimension() != 1024 {
		t.Fatalf("dimension = %d", client.Dimension())
	}
}

func TestARKRejectsWrongCredentialModeAndUnknownFields(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	for _, input := range []embeddingport.ProviderDecodeInput{
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"auth_mode":"api_key","retry_times":11}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)},
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"auth_mode":"ak_sk"}`), Credentials: json.RawMessage(`{"api_key":"secret"}`)},
		{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"auth_mode":"api_key"}`), Credentials: json.RawMessage(`{"api_key":"secret","extra":"x"}`)},
	} {
		_, _, err := factory.DecodeProvider(input)
		if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) && !errors.Is(err, domainerrors.ErrCredentialsRequired) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestARKModelContract(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "doubao-embedding-large", Dimensions: 2048, Parameters: json.RawMessage(`{"batch_size":64}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "doubao", Dimensions: 768, Parameters: json.RawMessage(`{"batch_size":64}`)}); !errors.Is(err, domainerrors.ErrUnsupportedEmbeddingDimension) {
		t.Fatalf("error = %v", err)
	}
}
