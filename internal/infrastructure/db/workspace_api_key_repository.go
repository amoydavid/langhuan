package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// WorkspaceAPIKeyRepository 是 service.WorkspaceAPIKeyStore 的 GORM 实现。
//
// 普通鉴权查询 (FindByTokenHashWithBindings) 只选择安全列，绝不读取
// token_secret_ciphertext；reveal 是唯一读取密文的路径。管理查询、吊销与
// 绑定查询始终显式带 workspace_id 约束。
type WorkspaceAPIKeyRepository struct {
	db *gorm.DB
}

// NewWorkspaceAPIKeyRepository 构造 API Key 持久化实现。
func NewWorkspaceAPIKeyRepository(db *gorm.DB) *WorkspaceAPIKeyRepository {
	return &WorkspaceAPIKeyRepository{db: db}
}

// workspaceAPIKeyTx 是一次 Workspace-bound 事务的句柄。
type workspaceAPIKeyTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

// WithinWorkspace 在显式 workspace_id 约束下执行一组事务操作。
func (r *WorkspaceAPIKeyRepository) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, service.WorkspaceAPIKeyTx) error,
) error {
	runner := NewWorkspaceTxRunner(r.db)
	return runner.WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &workspaceAPIKeyTx{db: tx, workspaceID: workspaceID})
	})
}

// CreateWithKnowledgeBaseBindings 在当前事务内原子写入 key、密文与全部绑定。
// 任一绑定或 key 写入失败整体回滚。
func (tx *workspaceAPIKeyTx) CreateWithKnowledgeBaseBindings(ctx context.Context, record service.WorkspaceAPIKeyCreateRecord) error {
	if err := validateWorkspaceAPIKeyRecord(record); err != nil {
		return err
	}
	row := workspaceAPIKeyToRow(record.Key)
	row.TokenSecretCiphertext = record.SecretCiphertext
	if err := tx.db.WithContext(ctx).Create(row).Error; err != nil {
		return translateDBError(err, "创建工作区 API Key 失败")
	}
	bindings := make([]*WorkspaceAPIKeyKnowledgeBaseRow, 0, len(record.KnowledgeBaseIDs))
	for _, kbID := range record.KnowledgeBaseIDs {
		if kbID == uuid.Nil {
			return fmt.Errorf("%w: 绑定的知识库 ID 不能为空", domainerrors.ErrValidation)
		}
		bindings = append(bindings, &WorkspaceAPIKeyKnowledgeBaseRow{
			APITokenID:      record.Key.ID,
			WorkspaceID:     tx.workspaceID,
			KnowledgeBaseID: kbID,
			CreatedAt:       record.Key.CreatedAt,
		})
	}
	if err := tx.db.WithContext(ctx).Create(&bindings).Error; err != nil {
		return translateDBError(err, "创建工作区 API Key 知识库绑定失败")
	}
	return nil
}

// FindByTokenHashWithBindings 是唯一的全局 hash lookup，仅用于鉴权建立身份。
// 它只选择安全列与绑定 ID，不读取 token_secret_ciphertext。
func (r *WorkspaceAPIKeyRepository) FindByTokenHashWithBindings(ctx context.Context, tokenHash string) (*model.WorkspaceAPIKey, error) {
	if tokenHash == "" {
		return nil, fmt.Errorf("%w: token_hash 不能为空", domainerrors.ErrValidation)
	}
	var row WorkspaceAPIKeyRow
	// 显式列出安全列，排除 token_secret_ciphertext。
	err := r.db.WithContext(ctx).
		Select("id", "workspace_id", "name", "token_hash", "token_prefix", "scopes", "expires_at", "last_used_at", "revoked_at", "created_by", "revoked_by", "created_at", "updated_at").
		Where("token_hash = ?", tokenHash).
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("按 hash 查询 API Key 失败: %w", err)
	}
	key := workspaceAPIKeyFromRow(&row)
	bindings, err := r.loadKnowledgeBaseIDs(ctx, r.db, key.ID)
	if err != nil {
		return nil, err
	}
	key.KnowledgeBaseIDs = bindings
	return key, nil
}

// RevealSecretCiphertext 按 workspace_id + id 读取密文，是唯一读取密文的路径。
func (r *WorkspaceAPIKeyRepository) RevealSecretCiphertext(ctx context.Context, workspaceID, keyID uuid.UUID) ([]byte, error) {
	var row struct {
		Ciphertext []byte `gorm:"column:token_secret_ciphertext"`
	}
	err := r.db.WithContext(ctx).
		Table("workspace_api_tokens").
		Select("token_secret_ciphertext").
		Where("workspace_id = ? AND id = ?", workspaceID, keyID).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("读取 API Key 密文失败: %w", err)
	}
	if len(row.Ciphertext) == 0 {
		return nil, domainerrors.ErrNotFound
	}
	return row.Ciphertext, nil
}

// Get 读取单条安全视图（不含密文/hash 写入路径，hash 字段不返回给客户端）。
func (r *WorkspaceAPIKeyRepository) Get(ctx context.Context, workspaceID, keyID uuid.UUID) (*model.WorkspaceAPIKey, error) {
	var row WorkspaceAPIKeyRow
	err := r.db.WithContext(ctx).
		Select("id", "workspace_id", "name", "token_hash", "token_prefix", "scopes", "expires_at", "last_used_at", "revoked_at", "created_by", "revoked_by", "created_at", "updated_at").
		Where("workspace_id = ? AND id = ?", workspaceID, keyID).
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domainerrors.ErrNotFound
		}
		return nil, fmt.Errorf("读取 API Key 失败: %w", err)
	}
	key := workspaceAPIKeyFromRow(&row)
	bindings, err := r.loadKnowledgeBaseIDs(ctx, r.db, key.ID)
	if err != nil {
		return nil, err
	}
	key.KnowledgeBaseIDs = bindings
	return key, nil
}

// List 按 created_at DESC, id DESC 列出 Workspace 内全部 key。
func (r *WorkspaceAPIKeyRepository) List(ctx context.Context, workspaceID uuid.UUID) ([]*model.WorkspaceAPIKey, error) {
	var rows []WorkspaceAPIKeyRow
	err := r.db.WithContext(ctx).
		Select("id", "workspace_id", "name", "token_hash", "token_prefix", "scopes", "expires_at", "last_used_at", "revoked_at", "created_by", "revoked_by", "created_at", "updated_at").
		Where("workspace_id = ?", workspaceID).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("列出 API Key 失败: %w", err)
	}
	keys := make([]*model.WorkspaceAPIKey, 0, len(rows))
	for i := range rows {
		key := workspaceAPIKeyFromRow(&rows[i])
		bindings, err := r.loadKnowledgeBaseIDs(ctx, r.db, key.ID)
		if err != nil {
			return nil, err
		}
		key.KnowledgeBaseIDs = bindings
		keys = append(keys, key)
	}
	return keys, nil
}

// Revoke 幂等吊销；已吊销返回 nil。actorID 为空表示未知 actor。
func (r *WorkspaceAPIKeyRepository) Revoke(ctx context.Context, workspaceID, keyID, actorID uuid.UUID, now time.Time) error {
	updates := map[string]any{
		"revoked_at": now,
		"updated_at": now,
	}
	if actorID != uuid.Nil {
		updates["revoked_by"] = actorID
	}
	result := r.db.WithContext(ctx).
		Model(&WorkspaceAPIKeyRow{}).
		Where("workspace_id = ? AND id = ?", workspaceID, keyID).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("吊销 API Key 失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domainerrors.ErrNotFound
	}
	return nil
}

// CountActive 返回未吊销且未过期的 key 数量，用于上限校验。
func (r *WorkspaceAPIKeyRepository) CountActive(ctx context.Context, workspaceID uuid.UUID, now time.Time) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&WorkspaceAPIKeyRow{}).
		Where("workspace_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", workspaceID, now).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("统计活跃 API Key 失败: %w", err)
	}
	return int(count), nil
}

// TouchLastUsed 条件更新最近使用时间：仅当 last_used_at 为空或距 now 至少
// minInterval 才更新。更新失败只返回错误供 service 记录 warning，不影响主请求。
func (r *WorkspaceAPIKeyRepository) TouchLastUsed(ctx context.Context, workspaceID, keyID uuid.UUID, now time.Time, minInterval time.Duration) error {
	result := r.db.WithContext(ctx).
		Model(&WorkspaceAPIKeyRow{}).
		Where("workspace_id = ? AND id = ? AND (last_used_at IS NULL OR last_used_at <= ?)", workspaceID, keyID, now.Add(-minInterval)).
		Update("last_used_at", now)
	if result.Error != nil {
		return fmt.Errorf("更新 API Key 最近使用时间失败: %w", result.Error)
	}
	return nil
}

// loadKnowledgeBaseIDs 加载某 key 的全部绑定知识库 ID。
func (r *WorkspaceAPIKeyRepository) loadKnowledgeBaseIDs(ctx context.Context, db *gorm.DB, keyID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.WithContext(ctx).
		Model(&WorkspaceAPIKeyKnowledgeBaseRow{}).
		Where("api_token_id = ?", keyID).
		Order("knowledge_base_id").
		Pluck("knowledge_base_id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("读取 API Key 知识库绑定失败: %w", err)
	}
	return ids, nil
}

// 编译期断言：确保 repository 与 tx 实现了 service 端口。
var (
	_ service.WorkspaceAPIKeyStore = (*WorkspaceAPIKeyRepository)(nil)
	_ service.WorkspaceAPIKeyTx    = (*workspaceAPIKeyTx)(nil)
)
