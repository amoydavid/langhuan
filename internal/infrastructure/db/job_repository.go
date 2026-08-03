package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
)

type JobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Get(ctx context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.Job, error) {
	var row JobRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取任务失败: %w", err)
	}
	return jobFromRow(&row), nil
}

func jobToRow(job *model.Job) *JobRow {
	return jobV2ToRow(job)
}

func jobFromRow(row *JobRow) *model.Job {
	return jobV2FromRow(row)
}
