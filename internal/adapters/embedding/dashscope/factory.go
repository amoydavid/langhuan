package dashscope

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	einodashscope "github.com/cloudwego/eino-ext/components/embedding/dashscope"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// ProviderConfig is the normalized DashScope connection configuration.
type ProviderConfig struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

// Credentials contains encrypted DashScope credential fields.
type Credentials struct {
	APIKey string `json:"api_key"`
}

// ModelParameters contains DashScope model-specific controls.
type ModelParameters struct {
	BatchSize int `json:"batch_size"`
}

// Factory builds DashScope Embedding clients.
type Factory struct{}

// NewFactory creates a DashScope factory.
func NewFactory() *Factory                    { return &Factory{} }
func (f *Factory) Provider() string           { return "dashscope" }
func (f *Factory) CredentialFields() []string { return []string{"api_key"} }

func (f *Factory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	config := ProviderConfig{TimeoutSeconds: 60}
	if err := providerutil.DecodeStrict(input.Config, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := providerutil.DecodeStrict(input.Credentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	if err := providerutil.ValidateTimeout(config.TimeoutSeconds); err != nil {
		return nil, nil, err
	}
	credentials.APIKey = strings.TrimSpace(credentials.APIKey)
	if credentials.APIKey == "" {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	configMap, err := providerutil.ToMap(config)
	if err != nil {
		return nil, nil, err
	}
	credentialsJSON, err := providerutil.ToJSON(credentials)
	return configMap, credentialsJSON, err
}

func (f *Factory) DecodeModel(input embeddingport.ModelDecodeInput) (map[string]any, error) {
	parameters := ModelParameters{BatchSize: 32}
	if err := providerutil.DecodeStrict(input.Parameters, &parameters, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ModelName) == "" {
		return nil, domainerrors.ErrInvalidProviderConfig
	}
	if input.Dimensions != 1024 {
		return nil, domainerrors.ErrUnsupportedEmbeddingDimension
	}
	if err := providerutil.ValidateBatchSize(parameters.BatchSize); err != nil {
		return nil, err
	}
	return providerutil.ToMap(parameters)
}

func (f *Factory) NewClient(ctx context.Context, input embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	var config ProviderConfig
	var credentials Credentials
	var parameters ModelParameters
	if err := providerutil.DecodeMap(input.Config, &config); err != nil {
		return nil, err
	}
	if err := providerutil.DecodeStrict(json.RawMessage(input.CredentialsJSON), &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if err := providerutil.DecodeMap(input.Parameters, &parameters); err != nil {
		return nil, err
	}
	httpClient, err := providerutil.NewHTTPClient(input.Scope, "", time.Duration(config.TimeoutSeconds)*time.Second, nil)
	if err != nil {
		return nil, err
	}
	dimensions := input.Dimensions
	embedder, err := einodashscope.NewEmbedder(ctx, &einodashscope.EmbeddingConfig{HTTPClient: httpClient, APIKey: credentials.APIKey, Model: strings.TrimSpace(input.ModelName), Dimensions: &dimensions})
	if err != nil {
		return nil, embeddingadapter.SanitizeProviderError(f.Provider(), err)
	}
	return embeddingadapter.NewValidatedClient(func(callCtx context.Context, texts []string) ([][]float64, error) {
		vectors, callErr := embedder.EmbedStrings(callCtx, texts)
		return vectors, embeddingadapter.SanitizeProviderError(f.Provider(), callErr)
	}, input.Dimensions, parameters.BatchSize), nil
}

var _ embeddingport.Factory = (*Factory)(nil)
