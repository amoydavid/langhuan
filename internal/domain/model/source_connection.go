package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
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
}

// 已知的外部内容源 provider 标识。
const (
	SourceProviderFeishu = "feishu"
)

// NewSourceConnection 创建一个 active 状态的 SourceConnection。
// AppID 作为非敏感配置存入 Config；凭证以已加密的 ciphertext 形式提供，
// 明文 app_secret 的加解密由 application service 负责（复用 credential_cipher）。
func NewSourceConnection(input NewSourceConnectionInput) (*SourceConnection, error) {
	if input.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: 连接名称不能为空", domainerrors.ErrValidation)
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if !knownSourceProvider(provider) {
		return nil, fmt.Errorf("%w: 不支持的来源 provider", domainerrors.ErrValidation)
	}
	appID := strings.TrimSpace(input.AppID)
	if appID == "" {
		return nil, fmt.Errorf("%w: app_id 不能为空", domainerrors.ErrValidation)
	}
	if len(input.CredentialsCiphertext) == 0 {
		return nil, fmt.Errorf("%w: 凭证密文不能为空", domainerrors.ErrValidation)
	}
	now := time.Now().UTC()
	return &SourceConnection{
		ID:          id.New(),
		WorkspaceID: input.WorkspaceID,
		Provider:    provider,
		Name:        name,
		Config: map[string]any{
			"app_id": appID,
		},
		CredentialsCiphertext: append([]byte(nil), input.CredentialsCiphertext...),
		Status:                "active",
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}

func knownSourceProvider(provider string) bool {
	return provider == SourceProviderFeishu
}
