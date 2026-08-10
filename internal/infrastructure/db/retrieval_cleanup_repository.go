package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// RetrievalCleanupRepository removes expired rebuildable retrieval data.
type RetrievalCleanupRepository struct {
	db *gorm.DB
}

// NewRetrievalCleanupRepository creates a cleanup repository.
func NewRetrievalCleanupRepository(database *gorm.DB) *RetrievalCleanupRepository {
	return &RetrievalCleanupRepository{db: database}
}

// CleanupGlobal removes one bounded batch of expired data across all workspaces.
// 与 Cleanup 不同，它不走 WithinWorkspace 事务，而是直接批量删除过期的 staging/failed/retired 投影，
// 供定时调度周期性收敛全库的 rebuildable 投影。FOR UPDATE SKIP LOCKED 保证并发安全。
func (r *RetrievalCleanupRepository) CleanupGlobal(
	ctx context.Context,
	request appservice.RetrievalCleanupGlobalRequest,
) (appservice.RetrievalCleanupResult, error) {
	if request.FailedStagingBefore.IsZero() || request.RetiredBefore.IsZero() ||
		request.BatchSize < 1 || request.BatchSize > 10000 {
		return appservice.RetrievalCleanupResult{}, fmt.Errorf("%w: Retrieval cleanup global 请求无效", domainerrors.ErrValidation)
	}
	var result appservice.RetrievalCleanupResult
	// 过期 staging/failed entries（跨 workspace）。
	deletedEntries := r.db.WithContext(ctx).
		Where("state IN (?, ?) AND created_at < ?",
			value.RetrievalEntryStaging, value.RetrievalEntryFailed, request.FailedStagingBefore).
		Limit(request.BatchSize).
		Delete(&RetrievalEntryRow{})
	if deletedEntries.Error != nil {
		return appservice.RetrievalCleanupResult{}, translateDBError(deletedEntries.Error, "全局删除过期 RetrievalEntry 失败")
	}
	result.DeletedEntries = deletedEntries.RowsAffected

	// 过期 retired entries（跨 workspace）。
	remaining := request.BatchSize - int(deletedEntries.RowsAffected)
	if remaining > 0 {
		deletedRetired := r.db.WithContext(ctx).
			Where("state = ? AND COALESCE(retired_at, created_at) < ?",
				value.RetrievalEntryRetired, request.RetiredBefore).
			Limit(remaining).
			Delete(&RetrievalEntryRow{})
		if deletedRetired.Error != nil {
			return appservice.RetrievalCleanupResult{}, translateDBError(deletedRetired.Error, "全局删除过期 retired RetrievalEntry 失败")
		}
		result.DeletedEntries += deletedRetired.RowsAffected
	}
	return result, nil
}

// Cleanup removes one bounded batch inside a transaction-local Workspace context.
func (r *RetrievalCleanupRepository) Cleanup(
	ctx context.Context,
	request appservice.RetrievalCleanupRequest,
) (appservice.RetrievalCleanupResult, error) {
	if request.WorkspaceID == uuid.Nil || request.FailedStagingBefore.IsZero() || request.RetiredBefore.IsZero() ||
		request.BatchSize < 1 || request.BatchSize > 10000 {
		return appservice.RetrievalCleanupResult{}, fmt.Errorf("%w: Retrieval cleanup 请求无效", domainerrors.ErrValidation)
	}
	var result appservice.RetrievalCleanupResult
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		entryIDs, err := lockExpiredRetrievalEntryIDs(ctx, tx, request)
		if err != nil {
			return err
		}
		if len(entryIDs) > 0 {
			deleted := tx.WithContext(ctx).
				Where("workspace_id = ? AND id IN ?", request.WorkspaceID, entryIDs).
				Delete(&RetrievalEntryRow{})
			if deleted.Error != nil {
				return translateDBError(deleted.Error, "删除过期 RetrievalEntry 失败")
			}
			result.DeletedEntries = deleted.RowsAffected
		}

		remaining := request.BatchSize - len(entryIDs)
		if remaining == 0 {
			return nil
		}
		generationIDs, err := lockExpiredRetiredGenerationIDs(ctx, tx, request, remaining)
		if err != nil {
			return err
		}
		if len(generationIDs) == 0 {
			return nil
		}
		deleted := tx.WithContext(ctx).
			Where("workspace_id = ? AND id IN ?", request.WorkspaceID, generationIDs).
			Delete(&IndexGenerationRow{})
		if deleted.Error != nil {
			return translateDBError(deleted.Error, "删除过期 retired Generation 失败")
		}
		result.DeletedGenerations = deleted.RowsAffected
		return nil
	})
	if err != nil {
		return appservice.RetrievalCleanupResult{}, err
	}
	return result, nil
}

func lockExpiredRetrievalEntryIDs(
	ctx context.Context,
	tx *gorm.DB,
	request appservice.RetrievalCleanupRequest,
) ([]uuid.UUID, error) {
	var rows []struct {
		ID uuid.UUID
	}
	err := tx.WithContext(ctx).Raw(
		"SELECT id FROM retrieval_entries "+
			"WHERE workspace_id = ? AND ("+
			"(state IN (?, ?) AND created_at < ?) OR "+
			"(state = ? AND COALESCE(retired_at, created_at) < ?)) "+
			"ORDER BY COALESCE(retired_at, created_at), id "+
			"FOR UPDATE SKIP LOCKED LIMIT ?",
		request.WorkspaceID,
		value.RetrievalEntryStaging, value.RetrievalEntryFailed, request.FailedStagingBefore,
		value.RetrievalEntryRetired, request.RetiredBefore,
		request.BatchSize,
	).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "锁定过期 RetrievalEntry 失败")
	}
	ids := make([]uuid.UUID, len(rows))
	for index := range rows {
		ids[index] = rows[index].ID
	}
	return ids, nil
}

func lockExpiredRetiredGenerationIDs(
	ctx context.Context,
	tx *gorm.DB,
	request appservice.RetrievalCleanupRequest,
	limit int,
) ([]uuid.UUID, error) {
	var rows []struct {
		ID uuid.UUID
	}
	err := tx.WithContext(ctx).Raw(
		"SELECT g.id FROM knowledge_base_index_generations AS g "+
			"WHERE g.workspace_id = ? AND g.status = ? AND g.retired_at < ? "+
			"AND NOT EXISTS ("+
			"SELECT 1 FROM knowledge_bases AS kb "+
			"WHERE kb.workspace_id = g.workspace_id "+
			"AND kb.id = g.knowledge_base_id "+
			"AND kb.active_index_generation_id = g.id) "+
			"ORDER BY g.retired_at, g.id "+
			"FOR UPDATE OF g SKIP LOCKED LIMIT ?",
		request.WorkspaceID, value.IndexGenerationRetired, request.RetiredBefore, limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "锁定过期 retired Generation 失败")
	}
	ids := make([]uuid.UUID, len(rows))
	for index := range rows {
		ids[index] = rows[index].ID
	}
	return ids, nil
}
