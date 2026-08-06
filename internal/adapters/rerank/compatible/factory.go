// Package compatible 实现遵循 /v1/rerank 兼容合同的 Rerank Provider。
//
// 它不是 "OpenAI Rerank" 标准，仅为本规格第 9 节固定的请求/响应形状提供适配。
package compatible

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	openaicatalog "github.com/dajee/langhuan/internal/adapters/modelcatalog/openai"
	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

const (
	providerKey = "rerank_compatible"

	defaultEndpointPath     = "/v1/rerank"
	defaultTimeoutSeconds   = 30
	defaultRetryTimes       = 2
	defaultMaxDocuments     = 100
	defaultMaxQueryChars    = 4096
	defaultMaxDocumentChars = 8192

	minTimeoutSeconds   = 1
	maxTimeoutSeconds   = 120
	minRetryTimes       = 0
	maxRetryTimes       = 3
	minMaxDocuments     = 50
	maxMaxDocuments     = 200
	minMaxQueryChars    = 256
	maxMaxQueryChars    = 4096
	minMaxDocumentChars = 512
	maxMaxDocumentChars = 32768

	maxCustomHeaders        = 16
	maxCustomHeaderNameLen  = 128
	maxCustomHeaderValueLen = 1024
)

// ProviderConfig 是 rerank_compatible 的规范化连接配置。
type ProviderConfig struct {
	BaseURL        string `json:"base_url"`
	EndpointPath   string `json:"endpoint_path"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	RetryTimes     int    `json:"retry_times"`
}

// Credentials 包含加密保存的凭证字段。
type Credentials struct {
	APIKey        string            `json:"api_key"`
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

// ModelParameters 是 type=rerank 模型的专属参数。
type ModelParameters struct {
	MaxDocuments     int `json:"max_documents"`
	MaxQueryChars    int `json:"max_query_chars"`
	MaxDocumentChars int `json:"max_document_chars"`
}

// Factory 构建 rerank_compatible wire contract 客户端。
type Factory struct {
	provider string
}

// NewFactory 创建 rerank_compatible factory。
func NewFactory() *Factory { return NewFactoryWithProvider(providerKey) }

// NewFactoryWithProvider 创建复用 compatible wire contract、但保留独立 provider key 的 Factory。
func NewFactoryWithProvider(provider string) *Factory {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = providerKey
	}
	return &Factory{provider: provider}
}

func (f *Factory) Provider() string           { return f.provider }
func (f *Factory) CredentialFields() []string { return []string{"api_key", "custom_headers"} }

// ModelCatalog returns the optional OpenAI-compatible /models discovery adapter.
func (f *Factory) ModelCatalog() modelcatalogport.Catalog { return openaicatalog.NewCatalog() }

func (f *Factory) DecodeProvider(input rerankport.ProviderDecodeInput) (map[string]any, []byte, error) {
	config := ProviderConfig{
		EndpointPath:   defaultEndpointPath,
		TimeoutSeconds: defaultTimeoutSeconds,
		RetryTimes:     defaultRetryTimes,
	}
	if err := providerutil.DecodeStrict(input.Config, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := providerutil.DecodeStrict(input.Credentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}

	config.BaseURL = strings.TrimSpace(config.BaseURL)
	if err := validateBaseURL(config.BaseURL, input.Scope); err != nil {
		return nil, nil, err
	}
	if err := validateEndpointPath(config.EndpointPath); err != nil {
		return nil, nil, err
	}
	if err := validateTimeout(config.TimeoutSeconds); err != nil {
		return nil, nil, err
	}
	if err := validateRetryTimes(config.RetryTimes); err != nil {
		return nil, nil, err
	}

	credentials.APIKey = strings.TrimSpace(credentials.APIKey)
	if credentials.APIKey == "" {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	if err := validateCustomHeaders(credentials.CustomHeaders); err != nil {
		return nil, nil, err
	}

	// 提前验证 SSRF / trusted client 能否构造，避免保存后才发现配置非法。
	if _, err := providerutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, credentials.CustomHeaders); err != nil {
		return nil, nil, err
	}

	configMap, err := providerutil.ToMap(config)
	if err != nil {
		return nil, nil, err
	}
	credentialsJSON, err := providerutil.ToJSON(credentials)
	return configMap, credentialsJSON, err
}

func (f *Factory) DecodeModel(input rerankport.ModelDecodeInput) (map[string]any, error) {
	if strings.TrimSpace(input.ModelName) == "" {
		return nil, fmt.Errorf("%w: model_name 不能为空", domainerrors.ErrInvalidProviderConfig)
	}
	parameters := ModelParameters{
		MaxDocuments:     defaultMaxDocuments,
		MaxQueryChars:    defaultMaxQueryChars,
		MaxDocumentChars: defaultMaxDocumentChars,
	}
	if err := providerutil.DecodeStrict(input.Parameters, &parameters, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	if err := validateMaxDocuments(parameters.MaxDocuments); err != nil {
		return nil, err
	}
	if err := validateMaxQueryChars(parameters.MaxQueryChars); err != nil {
		return nil, err
	}
	if err := validateMaxDocumentChars(parameters.MaxDocumentChars); err != nil {
		return nil, err
	}
	return providerutil.ToMap(parameters)
}

func (f *Factory) NewClient(ctx context.Context, input rerankport.ClientInput) (rerankport.Client, error) {
	var config ProviderConfig
	if err := providerutil.DecodeMap(input.Config, &config); err != nil {
		return nil, err
	}
	var credentials Credentials
	if err := providerutil.DecodeStrict(json.RawMessage(input.CredentialsJSON), &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, err
	}
	var parameters ModelParameters
	if err := providerutil.DecodeMap(input.Parameters, &parameters); err != nil {
		return nil, err
	}
	httpClient, err := providerutil.NewHTTPClient(input.Scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, credentials.CustomHeaders)
	if err != nil {
		return nil, err
	}
	endpoint, err := resolveEndpoint(config.BaseURL, config.EndpointPath)
	if err != nil {
		return nil, err
	}
	return &client{
		httpClient: httpClient,
		endpoint:   endpoint,
		apiKey:     credentials.APIKey,
		provider:   f.Provider(),
		modelName:  strings.TrimSpace(input.ModelName),
		retryTimes: config.RetryTimes,
		parameters: parameters,
	}, nil
}

var _ rerankport.Factory = (*Factory)(nil)
var _ modelcatalogport.CatalogProvider = (*Factory)(nil)

func validateBaseURL(baseURL string, scope value.ModelScope) error {
	if baseURL == "" {
		if scope == value.ModelScopeWorkspace {
			return fmt.Errorf("%w: base_url 不能为空", domainerrors.ErrInvalidProviderConfig)
		}
		return nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("%w: base_url 无效", domainerrors.ErrInvalidProviderConfig)
	}
	if !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("%w: base_url 必须是绝对地址", domainerrors.ErrInvalidProviderConfig)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: base_url 不能包含凭证", domainerrors.ErrInvalidProviderConfig)
	}
	if rawQuery := strings.TrimSpace(parsed.RawQuery); rawQuery != "" {
		return fmt.Errorf("%w: base_url 不能包含 query", domainerrors.ErrInvalidProviderConfig)
	}
	if fragment := strings.TrimSpace(parsed.Fragment); fragment != "" {
		return fmt.Errorf("%w: base_url 不能包含 fragment", domainerrors.ErrInvalidProviderConfig)
	}
	return nil
}

func validateEndpointPath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: endpoint_path 不能为空", domainerrors.ErrInvalidProviderConfig)
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: endpoint_path 必须以 / 开头", domainerrors.ErrInvalidProviderConfig)
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("%w: endpoint_path 无效", domainerrors.ErrInvalidProviderConfig)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return fmt.Errorf("%w: endpoint_path 不能包含 scheme 或 host", domainerrors.ErrInvalidProviderConfig)
	}
	if strings.TrimSpace(parsed.RawQuery) != "" {
		return fmt.Errorf("%w: endpoint_path 不能包含 query", domainerrors.ErrInvalidProviderConfig)
	}
	if strings.TrimSpace(parsed.Fragment) != "" {
		return fmt.Errorf("%w: endpoint_path 不能包含 fragment", domainerrors.ErrInvalidProviderConfig)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("%w: endpoint_path 不能包含 ..", domainerrors.ErrInvalidProviderConfig)
	}
	return nil
}

func validateTimeout(seconds int) error {
	if seconds < minTimeoutSeconds || seconds > maxTimeoutSeconds {
		return fmt.Errorf("%w: timeout_seconds 必须在 %d 到 %d 之间", domainerrors.ErrInvalidProviderConfig, minTimeoutSeconds, maxTimeoutSeconds)
	}
	return nil
}

func validateRetryTimes(times int) error {
	if times < minRetryTimes || times > maxRetryTimes {
		return fmt.Errorf("%w: retry_times 必须在 %d 到 %d 之间", domainerrors.ErrInvalidProviderConfig, minRetryTimes, maxRetryTimes)
	}
	return nil
}

func validateMaxDocuments(value int) error {
	if value < minMaxDocuments || value > maxMaxDocuments {
		return fmt.Errorf("%w: max_documents 必须在 %d 到 %d 之间", domainerrors.ErrInvalidProviderConfig, minMaxDocuments, maxMaxDocuments)
	}
	return nil
}

func validateMaxQueryChars(value int) error {
	if value < minMaxQueryChars || value > maxMaxQueryChars {
		return fmt.Errorf("%w: max_query_chars 必须在 %d 到 %d 之间", domainerrors.ErrInvalidProviderConfig, minMaxQueryChars, maxMaxQueryChars)
	}
	return nil
}

func validateMaxDocumentChars(value int) error {
	if value < minMaxDocumentChars || value > maxMaxDocumentChars {
		return fmt.Errorf("%w: max_document_chars 必须在 %d 到 %d 之间", domainerrors.ErrInvalidProviderConfig, minMaxDocumentChars, maxMaxDocumentChars)
	}
	return nil
}

// resolveEndpoint 拼接 base_url 与 endpoint_path，确保结果是单一绝对 URL。
func resolveEndpoint(baseURL, endpointPath string) (string, error) {
	if baseURL == "" {
		return "", fmt.Errorf("%w: base_url 不能为空", domainerrors.ErrInvalidProviderConfig)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("%w: base_url 无效", domainerrors.ErrInvalidProviderConfig)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpointPath
	return parsed.String(), nil
}
