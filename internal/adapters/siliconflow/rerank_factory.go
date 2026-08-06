package siliconflow

import (
	"context"

	"github.com/dajee/langhuan/internal/adapters/providerutil"
	rerankcompatible "github.com/dajee/langhuan/internal/adapters/rerank/compatible"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// RerankFactory 把 SiliconFlow 共享连接投影到 compatible Rerank transport。
type RerankFactory struct {
	delegate *rerankcompatible.Factory
}

// NewRerankFactory 创建 SiliconFlow Rerank Factory。
func NewRerankFactory() *RerankFactory {
	return &RerankFactory{delegate: rerankcompatible.NewFactoryWithProvider(ProviderKey)}
}

func (f *RerankFactory) Provider() string                       { return ProviderKey }
func (f *RerankFactory) CredentialFields() []string             { return []string{"api_key"} }
func (f *RerankFactory) ModelCatalog() modelcatalogport.Catalog { return &modelCatalog{} }

func (f *RerankFactory) DecodeProvider(input rerankport.ProviderDecodeInput) (map[string]any, []byte, error) {
	return DecodeProvider(input.Scope, input.Config, input.Credentials)
}

func (f *RerankFactory) DecodeModel(input rerankport.ModelDecodeInput) (map[string]any, error) {
	return f.delegate.DecodeModel(input)
}

func (f *RerankFactory) NewClient(ctx context.Context, input rerankport.ClientInput) (rerankport.Client, error) {
	config, credentials, err := decodeNormalized(input.Config, input.CredentialsJSON)
	if err != nil {
		return nil, err
	}
	projectedConfig, err := providerutil.ToMap(rerankcompatible.ProviderConfig{
		BaseURL: config.BaseURL, EndpointPath: config.RerankEndpointPath,
		TimeoutSeconds: config.TimeoutSeconds, RetryTimes: config.RetryTimes,
	})
	if err != nil {
		return nil, err
	}
	projectedCredentials, err := providerutil.ToJSON(rerankcompatible.Credentials{APIKey: credentials.APIKey})
	if err != nil {
		return nil, err
	}
	input.Config = projectedConfig
	input.CredentialsJSON = projectedCredentials
	return f.delegate.NewClient(ctx, input)
}

var _ rerankport.Factory = (*RerankFactory)(nil)
