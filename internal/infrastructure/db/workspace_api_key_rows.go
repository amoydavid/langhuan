package db

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// WorkspaceAPIKeyRow 是 workspace_api_tokens 的 GORM Row。
//
// token_secret_ciphertext 只服务 reveal，普通鉴权查询不读取该列。
// scopes 使用 text[] (pq.StringArray)；token_hash 是唯一索引。
type WorkspaceAPIKeyRow struct {
	ID                    uuid.UUID      `gorm:"type:uuid;primaryKey"`
	WorkspaceID           uuid.UUID      `gorm:"type:uuid;not null;index:idx_workspace_api_tokens_workspace"`
	Name                  string         `gorm:"type:text;not null"`
	TokenHash             string         `gorm:"type:text;not null;uniqueIndex"`
	TokenSecretCiphertext []byte         `gorm:"type:bytea;not null;column:token_secret_ciphertext"`
	TokenPrefix           string         `gorm:"type:text;not null"`
	Scopes                pq.StringArray `gorm:"type:text[];not null"`
	ExpiresAt             *time.Time
	LastUsedAt            *time.Time
	RevokedAt             *time.Time
	CreatedBy             *uuid.UUID `gorm:"type:uuid"`
	RevokedBy             *uuid.UUID `gorm:"type:uuid"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// TableName 固定为保留的占位表名，避免重命名带来的无谓迁移成本。
func (WorkspaceAPIKeyRow) TableName() string { return "workspace_api_tokens" }

// WorkspaceAPIKeyKnowledgeBaseRow 是 API Key 与知识库绑定的 join table Row。
type WorkspaceAPIKeyKnowledgeBaseRow struct {
	APITokenID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null;primaryKey"`
	KnowledgeBaseID uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt       time.Time
}

// TableName 固定为 workspace_api_token_knowledge_bases。
func (WorkspaceAPIKeyKnowledgeBaseRow) TableName() string {
	return "workspace_api_token_knowledge_bases"
}
