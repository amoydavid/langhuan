package tencentcloud

import (
	"context"
	"encoding/json"
	"strings"

	einotencentcloud "github.com/cloudwego/eino-ext/components/embedding/tencentcloud"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// ProviderConfig is the normalized TencentCloud connection configuration.
type ProviderConfig struct {
	Region string `json:"region"`
}

// Credentials contains encrypted TencentCloud credential fields.
type Credentials struct {
	SecretID  string `json:"secret_id"`
	SecretKey string `json:"secret_key"`
}

// ModelParameters documents that TencentCloud exposes no model controls.
type ModelParameters struct{}

// Factory builds TencentCloud Embedding clients.
type Factory struct{}

// NewFactory creates a TencentCloud factory.
func NewFactory() *Factory                    { return &Factory{} }
func (f *Factory) Provider() string           { return "tencentcloud" }
func (f *Factory) CredentialFields() []string { return []string{"secret_id", "secret_key"} }

func (f *Factory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	config := ProviderConfig{}
	if err := providerutil.DecodeStrict(input.Config, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := providerutil.DecodeStrict(input.Credentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	config.Region = strings.TrimSpace(config.Region)
	credentials.SecretID = strings.TrimSpace(credentials.SecretID)
	credentials.SecretKey = strings.TrimSpace(credentials.SecretKey)
	if config.Region == "" {
		return nil, nil, domainerrors.ErrInvalidProviderConfig
	}
	if credentials.SecretID == "" || credentials.SecretKey == "" {
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
	parameters := ModelParameters{}
	if err := providerutil.DecodeStrict(input.Parameters, &parameters, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ModelName) != "hunyuan-embedding" {
		return nil, domainerrors.ErrInvalidProviderConfig
	}
	if input.Dimensions != 1024 {
		return nil, domainerrors.ErrUnsupportedEmbeddingDimension
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
	embedder, err := einotencentcloud.NewEmbedder(ctx, &einotencentcloud.EmbeddingConfig{SecretID: credentials.SecretID, SecretKey: credentials.SecretKey, Region: config.Region})
	if err != nil {
		return nil, embeddingadapter.SanitizeProviderError(f.Provider(), err)
	}
	const providerManagedBatchSize = int(^uint(0) >> 1)
	return embeddingadapter.NewValidatedClient(func(callCtx context.Context, texts []string) ([][]float64, error) {
		vectors, callErr := embedder.EmbedStrings(callCtx, texts)
		return vectors, embeddingadapter.SanitizeProviderError(f.Provider(), callErr)
	}, input.Dimensions, providerManagedBatchSize), nil
}

var _ embeddingport.Factory = (*Factory)(nil)
