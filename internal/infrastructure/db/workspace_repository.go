package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

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
