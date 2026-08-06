package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]any(m))
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("unsupported JSONMap scan type %T", value)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = JSONMap(decoded)
	return nil
}

type WorkspaceRow struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name      string    `gorm:"index"`
	Slug      string    `gorm:"uniqueIndex:idx_workspaces_slug"`
	Metadata  JSONMap   `gorm:"type:jsonb"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (WorkspaceRow) TableName() string {
	return "workspaces"
}

// WorkspaceAPIKeyRow 与绑定表定义在 workspace_api_key_rows.go，见 v0.6.0。

// ModelProviderRow 映射模型连接表；凭证字段只保存 AES-GCM 密文。
type ModelProviderRow struct {
	ID                    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Scope                 string
	WorkspaceID           *uuid.UUID `gorm:"type:uuid;index"`
	Name                  string
	DisplayName           string
	Description           string
	Provider              string
	Config                JSONMap `gorm:"type:jsonb"`
	CredentialsCiphertext []byte
	Status                string
	CreatedBy             *uuid.UUID `gorm:"type:uuid"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (ModelProviderRow) TableName() string {
	return "model_providers"
}

// ModelRow 映射 Provider 下的具体模型实例。
type ModelRow struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProviderID  uuid.UUID `gorm:"type:uuid;index"`
	Name        string
	DisplayName string
	Description string
	Type        string
	ModelName   string
	Dimensions  *int
	Parameters  JSONMap `gorm:"type:jsonb"`
	Status      string
	CreatedBy   *uuid.UUID `gorm:"type:uuid"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (ModelRow) TableName() string {
	return "models"
}

// UserRow 对应 users 表，承载多租户认证的用户信息。
// PasswordHash 仅存 argon2id 编码串（自带 salt），明文密码绝不入库。
type UserRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	Email           string    `gorm:"uniqueIndex"`
	Nickname        string
	PasswordHash    string
	IsPlatformAdmin bool
	LastLoginAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (UserRow) TableName() string {
	return "users"
}

// SessionRow 对应 sessions 表。会话 ID 不得进入日志/响应。
type SessionRow struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID `gorm:"type:uuid;index"`
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	IPAddr     string `gorm:"type:inet"`
	RevokedAt  *time.Time
}

func (SessionRow) TableName() string {
	return "sessions"
}

// MembershipRow 对应 workspace_memberships 表。
type MembershipRow struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID uuid.UUID `gorm:"type:uuid;index"`
	UserID      uuid.UUID `gorm:"type:uuid;index"`
	Role        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (MembershipRow) TableName() string {
	return "workspace_memberships"
}

// InvitationRow 对应 workspace_invitations 表。
// TokenHash 为邀请 token 的 SHA-256，明文 token 绝不入库/日志。
type InvitationRow struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID    uuid.UUID `gorm:"type:uuid;index"`
	InvitedEmail   string
	Role           string
	TokenHash      string `gorm:"index"`
	TokenPrefix    string
	ExpiresAt      time.Time
	AcceptedAt     *time.Time
	AcceptedUserID *uuid.UUID
	RevokedAt      *time.Time
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
}

func (InvitationRow) TableName() string {
	return "workspace_invitations"
}

func AutoMigratedModels() []any {
	return []any{
		&WorkspaceRow{},
		&WorkspaceAPIKeyRow{},
		&WorkspaceAPIKeyKnowledgeBaseRow{},
		&UserRow{},
		&SessionRow{},
		&MembershipRow{},
		&InvitationRow{},
		&ModelProviderRow{},
		&ModelRow{},
		&SourceConnectionRow{},
		&WorkspaceSearchSettingsRow{},
		&KnowledgeBaseRow{},
		&IndexGenerationRow{},
		&DocumentRow{},
		&DocumentRevisionRow{},
		&FAQRevisionContentRow{},
		&FAQRevisionQuestionRow{},
		&FileTreeNodeRow{},
		&DocumentChunkSetRow{},
		&ChunkRow{},
		&ChunkRevisionRow{},
		&DocumentAssetRow{},
		&JobRow{},
		&RetrievalEntryRow{},
	}
}
