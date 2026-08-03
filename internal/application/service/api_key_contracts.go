package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// APIKeySecretCipher 是 Workspace API Key 可恢复明文的加密边界。
//
// 接口定义在使用方（application），实现位于 adapters/auth；它与模型 Provider
// 凭证加密器严格分离，使用 HKDF 派生的独立子密钥与绑定 Workspace/key ID 的
// AAD，禁止误用 Provider 专用 AAD。普通鉴权路径绝不调用本接口。
type APIKeySecretCipher interface {
	// Encrypt 用绑定 (workspaceID, apiKeyID) 的 AAD 加密明文，返回版本化密文。
	Encrypt(workspaceID, apiKeyID uuid.UUID, plaintext []byte) ([]byte, error)
	// Decrypt 用相同 AAD 解密密文；跨 Workspace/key ID 复制密文必须失败。
	Decrypt(workspaceID, apiKeyID uuid.UUID, ciphertext []byte) ([]byte, error)
}

// WorkspaceAPIKeyCreateRecord 是持久化 API Key 的输入，包含领域事实、可恢复
// 密文与去重后的知识库绑定。
type WorkspaceAPIKeyCreateRecord struct {
	Key              *model.WorkspaceAPIKey
	SecretCiphertext []byte
	KnowledgeBaseIDs []uuid.UUID
}

// WorkspaceAPIKeyAuthenticated 是 Authenticate 成功后返回的鉴权事实。
type WorkspaceAPIKeyAuthenticated struct {
	Key *model.WorkspaceAPIKey
}

// WorkspaceAPIKeyTx 描述一次 Workspace-bound 事务内可执行的操作集合。
//
// 只有 FindByTokenHashWithBindings 是建立租户身份前的唯一全局 hash lookup；
// 其余方法都必须在 WithinWorkspace 提供的 workspace_id 约束下执行。
type WorkspaceAPIKeyTx interface {
	CreateWithKnowledgeBaseBindings(ctx context.Context, record WorkspaceAPIKeyCreateRecord) error
}

// WorkspaceAPIKeyStore 是 application 协调 API Key 生命周期的持久化端口。
type WorkspaceAPIKeyStore interface {
	// WithinWorkspace 在显式 workspace_id 约束下执行一组事务操作。
	WithinWorkspace(ctx context.Context, workspaceID uuid.UUID, fn func(context.Context, WorkspaceAPIKeyTx) error) error
	// FindByTokenHashWithBindings 是唯一的全局 hash lookup，仅用于鉴权建立身份。
	FindByTokenHashWithBindings(ctx context.Context, tokenHash string) (*model.WorkspaceAPIKey, error)
	// RevealSecretCiphertext 按 workspace_id + id 读取密文，是唯一读取密文的路径。
	RevealSecretCiphertext(ctx context.Context, workspaceID, keyID uuid.UUID) ([]byte, error)
	// Get 读取单条安全视图（不含密文/hash）。
	Get(ctx context.Context, workspaceID, keyID uuid.UUID) (*model.WorkspaceAPIKey, error)
	// List 按 created_at DESC, id DESC 列出 Workspace 内全部 key。
	List(ctx context.Context, workspaceID uuid.UUID) ([]*model.WorkspaceAPIKey, error)
	// Revoke 幂等吊销；已吊销返回 nil。
	Revoke(ctx context.Context, workspaceID, keyID, actorID uuid.UUID, now time.Time) error
	// CountActive 返回未吊销且未过期的 key 数量，用于上限校验。
	CountActive(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error)
	// TouchLastUsed 条件更新最近使用时间；更新失败不影响主请求。
	TouchLastUsed(ctx context.Context, workspaceID, keyID uuid.UUID, now time.Time, minInterval time.Duration) error
}

// APIKeyExpiration 描述创建时的到期方式，是 days|never 判别联合。
type APIKeyExpiration struct {
	Type string
	Days int
}

// 到期方式常量。
const (
	ExpirationDays  = "days"
	ExpirationNever = "never"
)

// CreateAPIKeyInput 是创建 API Key 的协议中立输入。
type CreateAPIKeyInput struct {
	WorkspaceID      uuid.UUID
	ActorID          uuid.UUID
	ActorRole        value.WorkspaceRole
	Name             string
	KnowledgeBaseIDs []uuid.UUID
	Scopes           []value.APIScope
	Expiration       APIKeyExpiration
}
