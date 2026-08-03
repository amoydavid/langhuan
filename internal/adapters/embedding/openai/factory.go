package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/embedding/openai"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	"github.com/dajee/langhuan/internal/adapters/embedding/internal/factoryutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

const defaultBatchSize = 32

// ProviderConfig is the normalized OpenAI connection configuration.
type ProviderConfig struct {
	Mode           string `json:"mode"`
	BaseURL        string `json:"base_url,omitempty"`
	APIVersion     string `json:"api_version,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Credentials contains encrypted OpenAI credential fields.
type Credentials struct {
	APIKey        string            `json:"api_key"`
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

// ModelParameters contains OpenAI model-specific controls.
type ModelParameters struct {
	BatchSize int `json:"batch_size"`
}

// Factory builds OpenAI Embedding clients.
type Factory struct{}

// NewFactory creates an OpenAI factory.
func NewFactory() *Factory { return &Factory{} }

func (f *Factory) Provider() string { return "openai" }

func (f *Factory) CredentialFields() []string { return []string{"api_key", "custom_headers"} }

func (f *Factory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	config := ProviderConfig{TimeoutSeconds: 60}
	if err := factoryutil.DecodeStrict(input.Config, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := factoryutil.DecodeStrict(input.Credentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.APIVersion = strings.TrimSpace(config.APIVersion)
	if err := factoryutil.ValidateTimeout(config.TimeoutSeconds); err != nil {
		return nil, nil, err
	}
	switch config.Mode {
	case "standard":
		if config.APIVersion != "" {
			return nil, nil, fmt.Errorf("%w: standard 模式不能设置 api_version", domainerrors.ErrInvalidProviderConfig)
		}
	case "azure":
		if config.BaseURL == "" || config.APIVersion == "" {
			return nil, nil, fmt.Errorf("%w: azure 模式需要 base_url 和 api_version", domainerrors.ErrInvalidProviderConfig)
		}
	default:
		return nil, nil, fmt.Errorf("%w: mode 必须是 standard 或 azure", domainerrors.ErrInvalidProviderConfig)
	}
	credentials.APIKey = strings.TrimSpace(credentials.APIKey)
	if credentials.APIKey == "" {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	if _, err := factoryutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, credentials.CustomHeaders); err != nil {
		return nil, nil, err
	}
	configMap, err := factoryutil.ToMap(config)
	if err != nil {
		return nil, nil, err
	}
	credentialsJSON, err := factoryutil.ToJSON(credentials)
	return configMap, credentialsJSON, err
}

func (f *Factory) DecodeModel(input embeddingport.ModelDecodeInput) (map[string]any, error) {
	parameters := ModelParameters{BatchSize: defaultBatchSize}
	if err := factoryutil.DecodeStrict(input.Parameters, &parameters, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if err := factoryutil.ValidateEmbeddingModel(input.ModelName, input.Dimensions); err != nil {
		return nil, err
	}
	if err := factoryutil.ValidateBatchSize(parameters.BatchSize); err != nil {
		return nil, err
	}
	return factoryutil.ToMap(parameters)
}

func (f *Factory) NewClient(ctx context.Context, input embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	var config ProviderConfig
	if err := factoryutil.DecodeMap(input.Config, &config); err != nil {
		return nil, err
	}
	var credentials Credentials
	if err := factoryutil.DecodeStrict(json.RawMessage(input.CredentialsJSON), &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	var parameters ModelParameters
	if err := factoryutil.DecodeMap(input.Parameters, &parameters); err != nil {
		return nil, err
	}
	httpClient, err := factoryutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, credentials.CustomHeaders)
	if err != nil {
		return nil, err
	}
	encodingFormat := einoopenai.EmbeddingEncodingFormatFloat
	dimensions := input.Dimensions
	embedder, err := einoopenai.NewEmbedder(ctx, &einoopenai.EmbeddingConfig{
		HTTPClient: httpClient, APIKey: credentials.APIKey, ByAzure: config.Mode == "azure",
		BaseURL: config.BaseURL, APIVersion: config.APIVersion, Model: strings.TrimSpace(input.ModelName),
		Dimensions: &dimensions, EncodingFormat: &encodingFormat,
	})
	if err != nil {
		return nil, embeddingadapter.SanitizeProviderError(f.Provider(), err)
	}
	return embeddingadapter.NewValidatedClient(func(callCtx context.Context, texts []string) ([][]float64, error) {
		vectors, callErr := embedder.EmbedStrings(callCtx, texts)
		return vectors, embeddingadapter.SanitizeProviderError(f.Provider(), callErr)
	}, input.Dimensions, parameters.BatchSize), nil
}

var _ embeddingport.Factory = (*Factory)(nil)
