package tencentcloud

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

func TestTencentCloudProviderContract(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, _, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"region":"ap-guangzhou"}`), Credentials: json.RawMessage(`{"secret_id":"id","secret_key":"key"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{
		Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"region":""}`), Credentials: json.RawMessage(`{"secret_id":"id"}`),
	}); !errors.Is(err, domainerrors.ErrInvalidProviderConfig) && !errors.Is(err, domainerrors.ErrCredentialsRequired) {
		t.Fatalf("error = %v", err)
	}
}

func TestTencentCloudNewClientBuildsNativeEmbedder(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	config, credentials, err := factory.DecodeProvider(embeddingport.ProviderDecodeInput{Scope: value.ModelScopeWorkspace, Config: json.RawMessage(`{"region":"ap-guangzhou"}`), Credentials: json.RawMessage(`{"secret_id":"id","secret_key":"key"}`)})
	if err != nil {
		t.Fatal(err)
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "hunyuan-embedding", Dimensions: 1024, Parameters: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := factory.NewClient(context.Background(), embeddingport.ClientInput{ProviderID: uuid.New(), Scope: value.ModelScopeWorkspace, Config: config, CredentialsJSON: credentials, ModelName: "hunyuan-embedding", Dimensions: 1024, Parameters: parameters})
	if err != nil {
		t.Fatal(err)
	}
	if client.Dimension() != 1024 {
		t.Fatalf("dimension = %d", client.Dimension())
	}
}

func TestTencentCloudOnlyAcceptsHunyuan1024(t *testing.T) {
	t.Parallel()
	factory := NewFactory()
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "hunyuan-embedding", Dimensions: 1024, Parameters: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "other", Dimensions: 1024, Parameters: json.RawMessage(`{}`)}); !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("model error = %v", err)
	}
	if _, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: "hunyuan-embedding", Dimensions: 2048, Parameters: json.RawMessage(`{}`)}); !errors.Is(err, domainerrors.ErrUnsupportedEmbeddingDimension) {
		t.Fatalf("dimension error = %v", err)
	}
}
