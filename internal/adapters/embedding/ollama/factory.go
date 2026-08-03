package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	einoollama "github.com/cloudwego/eino-ext/components/embedding/ollama"

	embeddingadapter "github.com/dajee/langhuan/internal/adapters/embedding"
	"github.com/dajee/langhuan/internal/adapters/embedding/internal/factoryutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

// ProviderConfig is the normalized Ollama connection configuration.
type ProviderConfig struct {
	BaseURL        string `json:"base_url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Credentials documents that Ollama has no credential fields.
type Credentials struct{}

// ModelParameters contains Ollama model-specific controls.
type ModelParameters struct {
	BatchSize        int   `json:"batch_size"`
	Truncate         *bool `json:"truncate,omitempty"`
	KeepAliveSeconds *int  `json:"keep_alive_seconds,omitempty"`
}

// Factory builds Ollama Embedding clients.
type Factory struct{}

// NewFactory creates an Ollama factory.
func NewFactory() *Factory                    { return &Factory{} }
func (f *Factory) Provider() string           { return "ollama" }
func (f *Factory) CredentialFields() []string { return []string{} }

func (f *Factory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	if input.Scope != value.ModelScopePlatform {
		return nil, nil, domainerrors.ErrProviderScopeNotAllowed
	}
	config := ProviderConfig{BaseURL: "http://localhost:11434", TimeoutSeconds: 60}
	if err := factoryutil.DecodeStrict(input.Config, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := factoryutil.DecodeStrict(input.Credentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, nil, fmt.Errorf("%w: Ollama base_url 必须是绝对 HTTP(S) URL", domainerrors.ErrInvalidProviderConfig)
	}
	if err := factoryutil.ValidateTimeout(config.TimeoutSeconds); err != nil {
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
	parameters := ModelParameters{BatchSize: 32}
	if err := factoryutil.DecodeStrict(input.Parameters, &parameters, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if err := factoryutil.ValidateEmbeddingModel(input.ModelName, input.Dimensions); err != nil {
		return nil, err
	}
	if err := factoryutil.ValidateBatchSize(parameters.BatchSize); err != nil {
		return nil, err
	}
	if parameters.KeepAliveSeconds != nil && (*parameters.KeepAliveSeconds < 0 || *parameters.KeepAliveSeconds > 86400) {
		return nil, fmt.Errorf("%w: keep_alive_seconds 必须在 0 到 86400 之间", domainerrors.ErrInvalidProviderConfig)
	}
	return factoryutil.ToMap(parameters)
}

func (f *Factory) NewClient(ctx context.Context, input embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	if input.Scope != value.ModelScopePlatform {
		return nil, domainerrors.ErrProviderScopeNotAllowed
	}
	var config ProviderConfig
	var credentials Credentials
	var parameters ModelParameters
	if err := factoryutil.DecodeMap(input.Config, &config); err != nil {
		return nil, err
	}
	if err := factoryutil.DecodeStrict(json.RawMessage(input.CredentialsJSON), &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if err := factoryutil.DecodeMap(input.Parameters, &parameters); err != nil {
		return nil, err
	}
	httpClient, err := factoryutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, nil)
	if err != nil {
		return nil, err
	}
	var keepAlive *time.Duration
	if parameters.KeepAliveSeconds != nil {
		duration := time.Duration(*parameters.KeepAliveSeconds) * time.Second
		keepAlive = &duration
	}
	embedder, err := einoollama.NewEmbedder(ctx, &einoollama.EmbeddingConfig{
		HTTPClient: httpClient, BaseURL: config.BaseURL, Model: strings.TrimSpace(input.ModelName),
		Truncate: parameters.Truncate, KeepAlive: keepAlive,
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
