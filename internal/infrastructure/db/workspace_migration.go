package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var DefaultWorkspaceID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func EnsureDefaultWorkspace(ctx context.Context, gormDB *gorm.DB) error {
	now := time.Now().UTC()
	row := WorkspaceRow{
		ID:        DefaultWorkspaceID,
		Name:      "default",
		Slug:      "default",
		Metadata:  JSONMap{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := gormDB.WithContext(ctx).FirstOrCreate(&row, "id = ?", DefaultWorkspaceID).Error; err != nil {
		return fmt.Errorf("确保默认 workspace 失败: %w", err)
	}

	if err := gormDB.WithContext(ctx).
		Model(&KnowledgeBaseRow{}).
		Where("workspace_id = ?", uuid.Nil).
		Update("workspace_id", DefaultWorkspaceID).
		Error; err != nil {
		return fmt.Errorf("回填知识库默认 workspace 失败: %w", err)
	}
	return nil
}
