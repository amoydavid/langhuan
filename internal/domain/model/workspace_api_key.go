package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// WorkspaceAPIKey 描述 Workspace 级程序化访问凭证的领域事实。
//
// 该模型不带任何 GORM/JSON tag，持久化由 infrastructure 层的 Row 负责
// 转换；对外 DTO 只包含安全字段。ExpiresAt=nil 表示不限期。
type WorkspaceAPIKey struct {
	ID               uuid.UUID
	WorkspaceID      uuid.UUID
	Name             string
	TokenHash        string
	TokenPrefix      string
	Scopes           []value.APIScope
	KnowledgeBaseIDs []uuid.UUID
	ExpiresAt        *time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
	CreatedBy        *uuid.UUID
	RevokedBy        *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
