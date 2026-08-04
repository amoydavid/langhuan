// Package mineru 提供 MinerU Cloud PDF 解析器的 ParserProvider Factory。
// 它负责凭据解码、校验和 ParserClient 构造，把 token 等敏感信息加密后交给 model_providers 表存储。
package mineru

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
)

const (
	// ProviderName 是 model_providers.provider 列中 MinerU 的标识符。
	ProviderName = "mineru"

	defaultBaseURL = "https://mineru.net"
)

// mineruCredentials 是 MinerU Provider 的敏感凭据结构（加密前）。
type mineruCredentials struct {
	Token string `json:"token"`
}

// mineruConfig 是 MinerU Provider 的非敏感运行配置。
type mineruConfig struct {
	BaseURL      string `json:"base_url"`
	ModelVersion string `json:"model_version"`
}

// Factory 实现 parserprovider.Factory。
type Factory struct {
	httpTimeout time.Duration
}

// NewFactory 创建 MinerU ParserProvider Factory。
func NewFactory() *Factory {
	return &Factory{httpTimeout: 120 * time.Second}
}

func (f *Factory) Provider() string           { return ProviderName }
func (f *Factory) CredentialFields() []string { return []string{"token"} }

func (f *Factory) DecodeProvider(input parserproviderport.ProviderDecodeInput) (map[string]any, []byte, error) {
	// 解码非敏感 config
	var cfg mineruConfig
	if len(input.Config) > 0 && string(input.Config) != "null" {
		if err := json.Unmarshal(input.Config, &cfg); err != nil {
			return nil, nil, fmt.Errorf("%w: MinerU config 解码失败: %v", domainerrors.ErrInvalidProviderConfig, err)
		}
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if strings.TrimSpace(cfg.ModelVersion) == "" {
		cfg.ModelVersion = "vlm"
	}

	// 解码敏感凭据
	var creds mineruCredentials
	if len(input.Credentials) > 0 && string(input.Credentials) != "null" {
		if err := json.Unmarshal(input.Credentials, &creds); err != nil {
			return nil, nil, fmt.Errorf("%w: MinerU credentials 解码失败: %v", domainerrors.ErrInvalidProviderConfig, err)
		}
	}
	if strings.TrimSpace(creds.Token) == "" {
		return nil, nil, fmt.Errorf("%w: MinerU token 不能为空", domainerrors.ErrCredentialsRequired)
	}

	configMap := map[string]any{
		"base_url":      cfg.BaseURL,
		"model_version": cfg.ModelVersion,
	}
	credentialsJSON, err := json.Marshal(mineruCredentials{Token: creds.Token})
	if err != nil {
		return nil, nil, fmt.Errorf("编码 MinerU credentials 失败: %w", err)
	}
	return configMap, credentialsJSON, nil
}

func (f *Factory) NewClient(ctx context.Context, input parserproviderport.ClientInput) (parserproviderport.ParserClient, error) {
	baseURL := defaultBaseURL
	modelVersion := "vlm"
	if input.Config != nil {
		if v, ok := input.Config["base_url"].(string); ok && v != "" {
			baseURL = v
		}
		if v, ok := input.Config["model_version"].(string); ok && v != "" {
			modelVersion = v
		}
	}

	var creds mineruCredentials
	if len(input.CredentialsJSON) > 0 {
		if err := json.Unmarshal(input.CredentialsJSON, &creds); err != nil {
			return nil, fmt.Errorf("解码 MinerU credentials 失败: %w", err)
		}
	}
	if creds.Token == "" {
		return nil, fmt.Errorf("%w: MinerU token 为空", domainerrors.ErrCredentialsRequired)
	}

	return NewClient(ClientConfig{
		BaseURL:      baseURL,
		Token:        creds.Token,
		ModelVersion: modelVersion,
		HTTPTimeout:  f.httpTimeout,
	}), nil
}

// 确保 Factory 实现 parserprovider.Factory 接口
var _ parserproviderport.Factory = (*Factory)(nil)
