package siliconflow

import (
	"context"

	openaiembedding "github.com/dajee/langhuan/internal/adapters/embedding/openai"
	"github.com/dajee/langhuan/internal/adapters/providerutil"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

// EmbeddingFactory 把 SiliconFlow 共享连接投影到 OpenAI-compatible Embedding transport。
type EmbeddingFactory struct {
	delegate *openaiembedding.Factory
}

// NewEmbeddingFactory 创建 SiliconFlow Embedding Factory。
func NewEmbeddingFactory() *EmbeddingFactory {
	return &EmbeddingFactory{delegate: openaiembedding.NewFactoryWithProvider(ProviderKey)}
}

func (f *EmbeddingFactory) Provider() string                       { return ProviderKey }
func (f *EmbeddingFactory) CredentialFields() []string             { return []string{"api_key"} }
func (f *EmbeddingFactory) ModelCatalog() modelcatalogport.Catalog { return &modelCatalog{} }

func (f *EmbeddingFactory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	return DecodeProvider(input.Scope, input.Config, input.Credentials)
}

func (f *EmbeddingFactory) DecodeModel(input embeddingport.ModelDecodeInput) (map[string]any, error) {
	return f.delegate.DecodeModel(input)
}

func (f *EmbeddingFactory) NewClient(ctx context.Context, input embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	config, credentials, err := decodeNormalized(input.Config, input.CredentialsJSON)
	if err != nil {
		return nil, err
	}
	projectedConfig, err := providerutil.ToMap(openaiembedding.ProviderConfig{
		Mode: "standard", BaseURL: embeddingBaseURL(config), TimeoutSeconds: config.TimeoutSeconds,
	})
	if err != nil {
		return nil, err
	}
	projectedCredentials, err := providerutil.ToJSON(openaiembedding.Credentials{APIKey: credentials.APIKey})
	if err != nil {
		return nil, err
	}
	input.Config = projectedConfig
	input.CredentialsJSON = projectedCredentials
	return f.delegate.NewClient(ctx, input)
}

var _ embeddingport.Factory = (*EmbeddingFactory)(nil)
