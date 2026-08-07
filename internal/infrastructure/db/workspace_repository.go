package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type WorkspaceRepository struct {
	db *gorm.DB
}

func NewWorkspaceRepository(db *gorm.DB) *WorkspaceRepository {
	return &WorkspaceRepository{db: db}
}

func (r *WorkspaceRepository) Create(ctx context.Context, ws *model.Workspace) error {
	row := workspaceToRow(ws)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("创建工作区失败: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) Get(ctx context.Context, id uuid.UUID) (*model.Workspace, error) {
	var row WorkspaceRow
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取工作区失败: %w", err)
	}
	return workspaceFromRow(&row), nil
}

func (r *WorkspaceRepository) GetDefault(ctx context.Context) (*model.Workspace, error) {
	return r.Get(ctx, DefaultWorkspaceID)
}

// GetBySlug 按 slug 查询 workspace。未找到返回 ErrNotFound。
func (r *WorkspaceRepository) GetBySlug(ctx context.Context, slug string) (*model.Workspace, error) {
	var row WorkspaceRow
	if err := r.db.WithContext(ctx).First(&row, "slug = ?", slug).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("按 slug 读取工作区失败: %w", err)
	}
	return workspaceFromRow(&row), nil
}

// CreateWithOwner 在单个事务内创建 workspace 与对应的 owner 成员关系。
// 任一步失败则整体回滚。
func (r *WorkspaceRepository) CreateWithOwner(ctx context.Context, ws *model.Workspace, ownerUserID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(workspaceToRow(ws)).Error; err != nil {
			return translateDBError(err, "创建工作区失败")
		}
		membership, err := model.NewMembership(ws.ID, ownerUserID, value.RoleOwner)
		if err != nil {
			return err
		}
		if err := tx.Create(membershipToRow(membership)).Error; err != nil {
			return translateDBError(err, "创建工作区 owner 失败")
		}
		return nil
	})
}

// workspaceLimitLockKey 是单租户 workspace 数量限制使用的 advisory lock key。
const workspaceLimitLockKey = "langhuan:workspace-limit"

// CreateWithOwnerIfEmpty 仅当平台尚无任何 workspace 时，在同一事务内创建
// workspace 与 owner 成员关系；已存在时返回 domainerrors.ErrWorkspaceLimitReached。
// advisory transaction lock 保证并发创建时只有一个成功，其余获得锁后重读计数被拒。
func (r *WorkspaceRepository) CreateWithOwnerIfEmpty(ctx context.Context, ws *model.Workspace, ownerUserID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", workspaceLimitLockKey).Error; err != nil {
			return fmt.Errorf("获取 workspace 数量限制锁失败: %w", err)
		}
		var count int64
		if err := tx.Model(&WorkspaceRow{}).Count(&count).Error; err != nil {
			return fmt.Errorf("统计工作区失败: %w", err)
		}
		if count > 0 {
			return domainerrors.ErrWorkspaceLimitReached
		}
		if err := tx.Create(workspaceToRow(ws)).Error; err != nil {
			return translateDBError(err, "创建工作区失败")
		}
		membership, err := model.NewMembership(ws.ID, ownerUserID, value.RoleOwner)
		if err != nil {
			return err
		}
		if err := tx.Create(membershipToRow(membership)).Error; err != nil {
			return translateDBError(err, "创建工作区 owner 失败")
		}
		return nil
	})
}

func workspaceToRow(ws *model.Workspace) *WorkspaceRow {
	return &WorkspaceRow{
		ID:        ws.ID,
		Name:      ws.Name,
		Slug:      ws.Slug,
		Metadata:  JSONMap(ws.Metadata),
		CreatedAt: ws.CreatedAt,
		UpdatedAt: ws.UpdatedAt,
	}
}

func workspaceFromRow(row *WorkspaceRow) *model.Workspace {
	return &model.Workspace{
		ID:        row.ID,
		Name:      row.Name,
		Slug:      row.Slug,
		Metadata:  map[string]any(row.Metadata),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
