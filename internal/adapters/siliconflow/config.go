// Package siliconflow 实现一条共享连接下的 Embedding 与 Rerank 能力。
package siliconflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/adapters/providerutil"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

const (
	// ProviderKey 是 SiliconFlow 的稳定 provider 标识。
	ProviderKey = "siliconflow"

	defaultBaseURL               = "https://api.siliconflow.cn"
	defaultEmbeddingEndpointPath = "/v1/embeddings"
	defaultRerankEndpointPath    = "/v1/rerank"
	defaultTimeoutSeconds        = 60
	defaultRetryTimes            = 2
)

// ProviderConfig 是 Embedding 与 Rerank 共用的连接配置。
type ProviderConfig struct {
	BaseURL               string `json:"base_url"`
	EmbeddingEndpointPath string `json:"embedding_endpoint_path"`
	RerankEndpointPath    string `json:"rerank_endpoint_path"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	RetryTimes            int    `json:"retry_times"`
}

// Credentials 是 SiliconFlow 共享凭证。
type Credentials struct {
	APIKey string `json:"api_key"`
}

// DecodeProvider 严格解码并规范化一次共享连接配置与凭证。
func DecodeProvider(scope value.ModelScope, rawConfig, rawCredentials json.RawMessage) (map[string]any, []byte, error) {
	config := ProviderConfig{
		BaseURL:               defaultBaseURL,
		EmbeddingEndpointPath: defaultEmbeddingEndpointPath,
		RerankEndpointPath:    defaultRerankEndpointPath,
		TimeoutSeconds:        defaultTimeoutSeconds,
		RetryTimes:            defaultRetryTimes,
	}
	if err := providerutil.DecodeStrict(rawConfig, &config, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}
	credentials := Credentials{}
	if err := providerutil.DecodeStrict(rawCredentials, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return nil, nil, err
	}

	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.EmbeddingEndpointPath = strings.TrimSpace(config.EmbeddingEndpointPath)
	config.RerankEndpointPath = strings.TrimSpace(config.RerankEndpointPath)
	if err := validateBaseURL(config.BaseURL); err != nil {
		return nil, nil, err
	}
	if err := validateEndpointPath("embedding_endpoint_path", config.EmbeddingEndpointPath, "/embeddings"); err != nil {
		return nil, nil, err
	}
	if err := validateEndpointPath("rerank_endpoint_path", config.RerankEndpointPath, "/rerank"); err != nil {
		return nil, nil, err
	}
	config.BaseURL = normalizeRepeatedEndpointPrefix(
		config.BaseURL,
		config.EmbeddingEndpointPath,
		config.RerankEndpointPath,
	)
	if err := providerutil.ValidateTimeout(config.TimeoutSeconds); err != nil {
		return nil, nil, err
	}
	if config.RetryTimes < 0 || config.RetryTimes > 3 {
		return nil, nil, fmt.Errorf("%w: retry_times 必须在 0 到 3 之间", domainerrors.ErrInvalidProviderConfig)
	}

	credentials.APIKey = strings.TrimSpace(credentials.APIKey)
	if credentials.APIKey == "" {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	if _, err := providerutil.NewHTTPClient(scope, config.BaseURL, time.Duration(config.TimeoutSeconds)*time.Second, nil); err != nil {
		return nil, nil, err
	}
	configMap, err := providerutil.ToMap(config)
	if err != nil {
		return nil, nil, err
	}
	credentialsJSON, err := providerutil.ToJSON(credentials)
	return configMap, credentialsJSON, err
}

func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("%w: base_url 必须是绝对地址", domainerrors.ErrInvalidProviderConfig)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%w: base_url 不能包含凭证、query 或 fragment", domainerrors.ErrInvalidProviderConfig)
	}
	return nil
}

func validateEndpointPath(field, path, suffix string) error {
	parsed, err := url.Parse(path)
	if err != nil || path == "" || !strings.HasPrefix(path, "/") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(path, "..") {
		return fmt.Errorf("%w: %s 必须是安全的绝对路径", domainerrors.ErrInvalidProviderConfig, field)
	}
	if !strings.HasSuffix(strings.TrimRight(path, "/"), suffix) {
		return fmt.Errorf("%w: %s 必须以 %s 结尾", domainerrors.ErrInvalidProviderConfig, field, suffix)
	}
	return nil
}

func decodeNormalized(config map[string]any, credentialsJSON []byte) (ProviderConfig, Credentials, error) {
	var typedConfig ProviderConfig
	if err := providerutil.DecodeMap(config, &typedConfig); err != nil {
		return ProviderConfig{}, Credentials{}, err
	}
	typedConfig.BaseURL = normalizeRepeatedEndpointPrefix(
		typedConfig.BaseURL,
		typedConfig.EmbeddingEndpointPath,
		typedConfig.RerankEndpointPath,
	)
	var credentials Credentials
	if err := providerutil.DecodeStrict(credentialsJSON, &credentials, domainerrors.ErrInvalidProviderConfig); err != nil {
		return ProviderConfig{}, Credentials{}, err
	}
	return typedConfig, credentials, nil
}

func embeddingBaseURL(config ProviderConfig) string {
	path := strings.TrimSuffix(strings.TrimRight(config.EmbeddingEndpointPath, "/"), "/embeddings")
	return strings.TrimRight(config.BaseURL, "/") + path
}

// normalizeRepeatedEndpointPrefix 兼容旧 OpenAI-compatible 连接：旧配置的
// base_url 常以 /v1 结尾，而 SiliconFlow 的两条 endpoint path 也以 /v1 开头。
// 当两条能力共享同一前缀且 base_url 已包含它时，只保留一份，避免请求落到
// /v1/v1/embeddings 或 /v1/v1/rerank。自定义反向代理前缀会被保留。
func normalizeRepeatedEndpointPrefix(baseURL, embeddingPath, rerankPath string) string {
	embeddingPrefix := strings.TrimSuffix(strings.TrimRight(embeddingPath, "/"), "/embeddings")
	rerankPrefix := strings.TrimSuffix(strings.TrimRight(rerankPath, "/"), "/rerank")
	if embeddingPrefix == "" || embeddingPrefix != rerankPrefix {
		return baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(basePath, embeddingPrefix) {
		return baseURL
	}
	parsed.Path = strings.TrimSuffix(basePath, embeddingPrefix)
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/")
}
