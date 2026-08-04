package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelProvider 表示一套模型服务连接、非敏感配置与加密凭证。
type ModelProvider struct {
	ID                    uuid.UUID
	Scope                 value.ModelScope
	WorkspaceID           *uuid.UUID
	Name                  string
	DisplayName           string
	Description           string
	Provider              string
	Config                map[string]any
	CredentialsCiphertext []byte
	Status                value.ModelStatus
	CreatedBy             *uuid.UUID
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// NewModelProvider 创建一个 active Provider，并规范化稳定标识和展示字段。
func NewModelProvider(
	scope value.ModelScope,
	workspaceID *uuid.UUID,
	name string,
	displayName string,
	description string,
	provider string,
	config map[string]any,
	credentialsCiphertext []byte,
	createdBy uuid.UUID,
) (*ModelProvider, error) {
	if !scope.IsValid() {
		return nil, fmt.Errorf("%w: scope 无效", domainerrors.ErrValidation)
	}
	if scope == value.ModelScopePlatform && workspaceID != nil {
		return nil, fmt.Errorf("%w: platform Provider 不得设置 workspace_id", domainerrors.ErrValidation)
	}
	if scope == value.ModelScopeWorkspace && (workspaceID == nil || *workspaceID == uuid.Nil) {
		return nil, fmt.Errorf("%w: workspace Provider 必须设置 workspace_id", domainerrors.ErrValidation)
	}
	if createdBy == uuid.Nil {
		return nil, fmt.Errorf("%w: created_by 不能为空", domainerrors.ErrValidation)
	}

	normalizedName, normalizedDisplayName, normalizedDescription, err := normalizeModelText(name, displayName, description)
	if err != nil {
		return nil, err
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if !modelIdentifierPattern.MatchString(provider) {
		return nil, fmt.Errorf("%w: provider 必须是 1 到 64 位小写 ASCII 标识", domainerrors.ErrValidation)
	}

	now := time.Now().UTC()
	actorID := createdBy
	return &ModelProvider{
		ID:                    id.New(),
		Scope:                 scope,
		WorkspaceID:           cloneUUIDPointer(workspaceID),
		Name:                  normalizedName,
		DisplayName:           normalizedDisplayName,
		Description:           normalizedDescription,
		Provider:              provider,
		Config:                cloneMap(config),
		CredentialsCiphertext: append([]byte(nil), credentialsCiphertext...),
		Status:                value.ModelStatusActive,
		CreatedBy:             &actorID,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, nil
}
