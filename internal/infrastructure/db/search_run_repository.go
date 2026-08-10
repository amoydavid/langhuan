package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchRunRepository 持久化检索运行快照，所有读写都显式带 workspace_id。
type SearchRunRepository struct {
	db *gorm.DB
}

// NewSearchRunRepository creates a SearchRun repository.
func NewSearchRunRepository(database *gorm.DB) *SearchRunRepository {
	return &SearchRunRepository{db: database}
}

// Create 创建一个 SearchRun 及其 Generation 快照。
func (r *SearchRunRepository) Create(ctx context.Context, run *model.SearchRun) error {
	if run == nil {
		return fmt.Errorf("%w: SearchRun 不能为空", domainerrors.ErrValidation)
	}
	if err := run.Validate(); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := searchRunToRow(run)
		if err := tx.Create(row).Error; err != nil {
			return translateDBError(err, "创建 SearchRun 失败")
		}
		for _, gen := range run.Generations {
			genRow := searchRunGenerationToRow(gen)
			if err := tx.Create(&genRow).Error; err != nil {
				return translateDBError(err, "创建 SearchRunGeneration 失败")
			}
		}
		return nil
	})
}

// Complete 在 Workspace transaction 中把 running SearchRun 推进到终态。
func (r *SearchRunRepository) Complete(
	ctx context.Context,
	workspaceID, searchRunID uuid.UUID,
	completion model.SearchRunCompletion,
) error {
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var row SearchRunRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND id = ?", workspaceID, searchRunID).
			First(&row).Error; err != nil {
			return translateDBError(err, "锁定 SearchRun 失败")
		}
		if value.RetrievalStatus(row.RetrievalStatus) != value.RetrievalStatusRunning {
			return fmt.Errorf("%w: SearchRun 已是终态", domainerrors.ErrConflict)
		}
		if completion.Status == value.RetrievalStatusFailed && completion.FailureClass == "" {
			return fmt.Errorf("%w: failed SearchRun 必须有 failure_class", domainerrors.ErrValidation)
		}
		if completion.Status != value.RetrievalStatusFailed && completion.FailureClass != "" {
			return fmt.Errorf("%w: 非 failed SearchRun 不能有 failure_class", domainerrors.ErrValidation)
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"retrieval_status": string(completion.Status),
			"failure_class":    completion.FailureClass,
			"ranking_stage":    string(completion.RankingStage),
			"result_count":     completion.ResultCount,
			"completed_at":     now,
		}
		if err := tx.Model(&SearchRunRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, searchRunID).
			Updates(updates).Error; err != nil {
			return translateDBError(err, "更新 SearchRun 失败")
		}
		for _, gen := range completion.Generations {
			genRow := searchRunGenerationToRow(gen)
			if err := tx.Create(&genRow).Error; err != nil {
				return translateDBError(err, "创建 SearchRunGeneration 失败")
			}
		}
		return nil
	})
}

// Get 读取一个 SearchRun 及其 Generation 快照；跨 Workspace 返回 ErrNotFound。
func (r *SearchRunRepository) Get(ctx context.Context, workspaceID, searchRunID uuid.UUID) (*model.SearchRun, error) {
	var row SearchRunRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, searchRunID).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取 SearchRun 失败")
	}
	var genRows []SearchRunGenerationRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND search_run_id = ?", workspaceID, searchRunID).
		Find(&genRows).Error; err != nil {
		return nil, translateDBError(err, "读取 SearchRunGeneration 失败")
	}
	generations := make([]model.SearchRunGeneration, len(genRows))
	for i, genRow := range genRows {
		generations[i] = searchRunGenerationFromRow(&genRow)
	}
	return searchRunFromRow(&row, generations), nil
}

// DeleteExpired 批量删除已过 expires_at 的 SearchRun（级联删除 generations），返回删除行数。
func (r *SearchRunRepository) DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("%w: DeleteExpired limit 必须为正", domainerrors.ErrValidation)
	}
	result := r.db.WithContext(ctx).
		Where("expires_at < ?", before).
		Limit(limit).
		Delete(&SearchRunRow{})
	if result.Error != nil {
		return 0, translateDBError(result.Error, "删除过期 SearchRun 失败")
	}
	return result.RowsAffected, nil
}

var _ appservice.SearchRunStore = (*SearchRunRepository)(nil)
