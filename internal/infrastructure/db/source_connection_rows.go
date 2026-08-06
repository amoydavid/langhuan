package db

import (
	"time"

	"github.com/google/uuid"
)

// SourceConnectionRow 映射 workspace_source_connections 表；
// 凭证字段只保存 AES-GCM 密文（复用 credential_cipher）。
type SourceConnectionRow struct {
	ID                    uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID           uuid.UUID `gorm:"type:uuid;not null;index"`
	Provider              string
	Name                  string
	Config                JSONMap `gorm:"type:jsonb"`
	CredentialsCiphertext []byte
	Status                string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

func (SourceConnectionRow) TableName() string { return "workspace_source_connections" }
