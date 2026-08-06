package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dajee/langhuan/internal/domain/model"
)

// WorkspaceSearchSettingsRepository 持久化 Workspace 默认检索策略。
type WorkspaceSearchSettingsRepository struct{ db *gorm.DB }

// NewWorkspaceSearchSettingsRepository 创建 Workspace Search Settings repository。
func NewWorkspaceSearchSettingsRepository(db *gorm.DB) *WorkspaceSearchSettingsRepository {
	return &WorkspaceSearchSettingsRepository{db: db}
}

// Get 在 Workspace transaction 中读取设置；不存在返回领域 ErrNotFound。
func (r *WorkspaceSearchSettingsRepository) Get(ctx context.Context, workspaceID uuid.UUID) (*model.WorkspaceSearchSettings, error) {
	var row WorkspaceSearchSettingsRow
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Where("workspace_id = ?", workspaceID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRepositoryNotFound
			}
			return fmt.Errorf("读取 Workspace 检索策略失败: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return workspaceSearchSettingsFromRow(&row)
}

// Upsert 在 Workspace transaction 中原子创建或替换设置。
func (r *WorkspaceSearchSettingsRepository) Upsert(ctx context.Context, settings *model.WorkspaceSearchSettings) error {
	row, err := workspaceSearchSettingsToRow(settings)
	if err != nil {
		return err
	}
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, settings.WorkspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		row.UpdatedAt = now
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		return translateDBError(tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "workspace_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"rerank_model_id", "rerank_provider_id", "rerank_model_name",
				"rerank_model_config_hash", "rerank_config", "updated_by", "updated_at",
			}),
		}).Create(row).Error, "保存 Workspace 检索策略失败")
	})
}
