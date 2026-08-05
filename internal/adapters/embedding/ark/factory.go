package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einoark "github.com/cloudwego/eino-ext/components/embedding/ark"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// ProviderConfig is the normalized ARK connection configuration.
type ProviderConfig struct {
	BaseURL        string `json:"base_url,omitempty"`
	Region         string `json:"region"`
	AuthMode       string `json:"auth_mode"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	RetryTimes     int    `json:"retry_times"`
}

// Credentials contains encrypted ARK credential fields.
type Credentials struct {
	APIKey    string `json:"api_key,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}

// ModelParameters contains ARK model-specific controls.
type ModelParameters struct {
	BatchSize int `json:"batch_size"`
}

// Factory builds ARK Embedding clients.
type Factory struct{}

// NewFactory creates an ARK factory.
func NewFactory() *Factory                    { return &Factory{} }
func (f *Factory) Provider() string           { return "ark" }
func (f *Factory) CredentialFields() []string { return []string{"api_key", "access_key", "secret_key"} }

func (f *Factory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	config := ProviderConfig{Region: "cn-beijing", TimeoutSeconds: 60, RetryTimes: 2}
	if err := providerutil.DecodeStrict(input.Config, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := providerutil.DecodeStrict(input.Credentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.Region = strings.TrimSpace(config.Region)
	config.AuthMode = strings.ToLower(strings.TrimSpace(config.AuthMode))
	if config.Region == "" {
		config.Region = "cn-beijing"
	}
	if err := providerutil.ValidateTimeout(config.TimeoutSeconds); err != nil {
		return nil, nil, err
	}
	if config.RetryTimes < 0 || config.RetryTimes > 5 {
		return nil, nil, fmt.Errorf("%w: retry_times 必须在 0 到 5 之间", domainerrors.ErrInvalidProviderConfig)
	}
	switch config.AuthMode {
	case "api_key":
		if strings.TrimSpace(credentials.APIKey) == "" {
			return nil, nil, domainerrors.ErrCredentialsRequired
		}
		credentials.AccessKey, credentials.SecretKey = "", ""
	case "ak_sk":
		if strings.TrimSpace(credentials.AccessKey) == "" || strings.TrimSpace(credentials.SecretKey) == "" {
			return nil, nil, domainerrors.ErrCredentialsRequired
		}
		credentials.APIKey = ""
	default:
		return nil, nil, fmt.Errorf("%w: auth_mode 必须是 api_key 或 ak_sk", domainerrors.ErrInvalidProviderConfig)
	}
	if _, err := providerutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, nil); err != nil {
		return nil, nil, err
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
	if err := providerutil.ValidateEmbeddingModel(input.ModelName, input.Dimensions); err != nil {
		return nil, err
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
	httpClient, err := providerutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, nil)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	retries := config.RetryTimes
	dimensions := input.Dimensions
	apiType := einoark.APITypeText
	embedder, err := einoark.NewEmbedder(ctx, &einoark.EmbeddingConfig{
		HTTPClient: httpClient, Timeout: &timeout, RetryTimes: &retries, BaseURL: config.BaseURL,
		Region: config.Region, APIKey: credentials.APIKey, AccessKey: credentials.AccessKey,
		SecretKey: credentials.SecretKey, Model: strings.TrimSpace(input.ModelName), APIType: &apiType, Dimensions: &dimensions,
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
