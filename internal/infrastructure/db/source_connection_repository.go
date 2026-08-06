package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
)

// SourceConnectionRepository 是 workspace_source_connections 表的薄封装。
type SourceConnectionRepository struct {
	db *gorm.DB
}

func NewSourceConnectionRepository(db *gorm.DB) *SourceConnectionRepository {
	return &SourceConnectionRepository{db: db}
}

// Create 写入一条连接记录。
func (r *SourceConnectionRepository) Create(ctx context.Context, conn *model.SourceConnection) error {
	if err := r.db.WithContext(ctx).Create(sourceConnectionToRow(conn)).Error; err != nil {
		return fmt.Errorf("创建来源连接失败: %w", err)
	}
	return nil
}

// Get 按 workspace + id 读取单条。
func (r *SourceConnectionRepository) Get(ctx context.Context, workspaceID, id uuid.UUID) (*model.SourceConnection, error) {
	var row SourceConnectionRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取来源连接失败: %w", err)
	}
	return sourceConnectionFromRow(&row), nil
}

// List 按 workspace 列出所有未软删的连接。
func (r *SourceConnectionRepository) List(ctx context.Context, workspaceID uuid.UUID) ([]*model.SourceConnection, error) {
	var rows []SourceConnectionRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出来源连接失败: %w", err)
	}
	result := make([]*model.SourceConnection, len(rows))
	for i, row := range rows {
		result[i] = sourceConnectionFromRow(&row)
	}
	return result, nil
}

// Update 更新连接的非凭证字段与凭证密文。
func (r *SourceConnectionRepository) Update(ctx context.Context, conn *model.SourceConnection) error {
	if err := r.db.WithContext(ctx).Save(sourceConnectionToRow(conn)).Error; err != nil {
		return fmt.Errorf("更新来源连接失败: %w", err)
	}
	return nil
}

// SoftDelete 软删连接。
func (r *SourceConnectionRepository) SoftDelete(ctx context.Context, workspaceID, id uuid.UUID) error {
	res := r.db.WithContext(ctx).
		Model(&SourceConnectionRow{}).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Update("deleted_at", time.Now().UTC())
	if res.Error != nil {
		return fmt.Errorf("删除来源连接失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}
