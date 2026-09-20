package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SourceConnection 表示一个 Workspace 级的外部内容源连接（如飞书内部应用），
// 携带非敏感配置与已加密的凭证密文。一个 Workspace 下可配置多个连接。
type SourceConnection struct {
	ID                    uuid.UUID
	WorkspaceID           uuid.UUID
	Provider              string
	Name                  string
	Config                map[string]any
	CredentialsCiphertext []byte
	Status                string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

// NewSourceConnectionInput 是创建 SourceConnection 的输入。
type NewSourceConnectionInput struct {
	WorkspaceID           uuid.UUID
	Provider              string
	Name                  string
	AppID                 string
	CredentialsCiphertext []byte
	// Config 是 provider 相关的非敏感配置（spec §3.1）：仅接受 endpoint_url
	// 键（firecrawl 的 endpoint 覆盖、browser 的 CDP 端点），其余键显式拒绝。
	Config map[string]any
}

// 已知的外部内容源 provider 标识。取值定义在 value.SourceProvider，
// 这里保留字符串常量：它同时被用作来源文档的 source_type 值。
const SourceProviderFeishu = string(value.SourceProviderFeishu)

// SourceConnectionConfigKeyEndpointURL 是连接 config 的唯一合同键（T12）：
// firecrawl 的 API base 覆盖与 browser 的 CDP 端点共用一个键名，同步编排
// （webSourceConfigWithCredentials）与引擎装配（connector.go）按此键消费。
const SourceConnectionConfigKeyEndpointURL = "endpoint_url"

// NewSourceConnection 按 provider 分支校验字段并创建一个 active 状态的 SourceConnection。
//
// 字段归属（spec §3.1）：
//   - feishu：app_id 必填并存入 Config（非敏感），凭证密文（app_secret）必填；
//   - firecrawl：凭证密文（api_key）必填，app_id 不适用（提供即报错，
//     避免静默丢弃调用方输入），endpoint_url 可选（缺省官方云）；
//   - jina：凭证密文（api_key）必填，app_id 与 config 不适用（首版无端点
//     覆盖入口，接受只会静默不生效）；
//   - browser：密文可空（token 是可选凭证），但 config.endpoint_url 必填
//     （CDP 端点是连接的可用性前提）。
//
// 明文凭证的加解密由 application service 负责（复用 credential_cipher），
// 本构造函数只接收已加密的 ciphertext。
func NewSourceConnection(input NewSourceConnectionInput) (*SourceConnection, error) {
	if input.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: 连接名称不能为空", domainerrors.ErrValidation)
	}
	provider := value.SourceProvider(strings.ToLower(strings.TrimSpace(input.Provider)))
	if err := provider.Validate(); err != nil {
		return nil, err
	}
	appID := strings.TrimSpace(input.AppID)
	if provider.RequiresAppID() {
		if appID == "" {
			return nil, fmt.Errorf("%w: app_id 不能为空", domainerrors.ErrValidation)
		}
	} else if appID != "" {
		// 显式拒绝优于静默丢弃：非飞书 provider 的 app_id 无处落库，
		// 静默忽略会让调用方误以为已保存。
		return nil, fmt.Errorf("%w: app_id 仅飞书连接需要", domainerrors.ErrValidation)
	}
	if provider.RequiresCredentials() && len(input.CredentialsCiphertext) == 0 {
		return nil, fmt.Errorf("%w: 凭证密文不能为空", domainerrors.ErrValidation)
	}
	config := map[string]any{}
	if provider.RequiresAppID() {
		config["app_id"] = appID
	}
	if err := applyConnectionConfig(provider, input.Config, config); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &SourceConnection{
		ID:                    id.New(),
		WorkspaceID:           input.WorkspaceID,
		Provider:              string(provider),
		Name:                  name,
		Config:                config,
		CredentialsCiphertext: append([]byte(nil), input.CredentialsCiphertext...),
		Status:                "active",
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

// applyConnectionConfig 校验并合并调用方 config 到连接 config（T12）：
//   - 仅 firecrawl（可选，http/https）与 browser（必填，ws/wss）接受
//     endpoint_url；feishu/jina 不接受任何 config 键；
//   - 未知键与非法值显式拒绝（与 app_id 同口径：拒绝优于静默丢弃）。
func applyConnectionConfig(provider value.SourceProvider, input, config map[string]any) error {
	for key, raw := range input {
		if key != SourceConnectionConfigKeyEndpointURL {
			return fmt.Errorf("%w: 不支持的连接配置键 %q", domainerrors.ErrValidation, key)
		}
		endpoint, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%w: config.endpoint_url 必须为字符串", domainerrors.ErrValidation)
		}
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			return fmt.Errorf("%w: config.endpoint_url 不能为空", domainerrors.ErrValidation)
		}
		switch provider {
		case value.SourceProviderFirecrawl:
			if err := validateEndpointScheme(endpoint, "http", "https"); err != nil {
				return fmt.Errorf("%w: config.endpoint_url 仅支持 http/https（firecrawl API base）", domainerrors.ErrValidation)
			}
			config[SourceConnectionConfigKeyEndpointURL] = endpoint
		case value.SourceProviderBrowser:
			if err := validateEndpointParsable(endpoint); err != nil {
				return fmt.Errorf("%w: config.endpoint_url 必须为合法的 CDP 端点地址", domainerrors.ErrValidation)
			}
			if err := validateEndpointScheme(endpoint, "ws", "wss"); err != nil {
				return fmt.Errorf("%w: CDP 端点仅支持 ws/wss（如 ws://host:9222）", domainerrors.ErrValidation)
			}
			config[SourceConnectionConfigKeyEndpointURL] = endpoint
		default:
			return fmt.Errorf("%w: %s 连接不支持自定义端点", domainerrors.ErrValidation, provider)
		}
	}
	if provider == value.SourceProviderBrowser && config[SourceConnectionConfigKeyEndpointURL] == nil {
		return fmt.Errorf("%w: browser 连接必须在 config.endpoint_url 提供 CDP 端点", domainerrors.ErrValidation)
	}
	return nil
}

// validateEndpointParsable 要求端点是带 scheme 与 host 的绝对地址。
func validateEndpointParsable(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("端点 %q 不可解析", raw)
	}
	return nil
}

// validateEndpointScheme 要求端点 scheme 在允许集合内（大小写不敏感）。
func validateEndpointScheme(raw string, allowed ...string) error {
	if err := validateEndpointParsable(raw); err != nil {
		return err
	}
	parsed, _ := url.Parse(raw)
	scheme := strings.ToLower(parsed.Scheme)
	for _, candidate := range allowed {
		if scheme == candidate {
			return nil
		}
	}
	return fmt.Errorf("端点 %q 的 scheme %q 不在允许集合内", raw, scheme)
}
