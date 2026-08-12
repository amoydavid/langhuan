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

// selectExpiredEntriesSQL 返回锁定过期 retrieval_entries 的 SQL。
// PG 用 FOR UPDATE SKIP LOCKED 保证并发清理安全；
// SQLite 无行锁，靠 _txlock=immediate 单写锁串行化，去掉该子句（spec §9）。
func selectExpiredEntriesSQL(dialector string) string {
	base := "SELECT id FROM retrieval_entries " +
		"WHERE workspace_id = ? AND (" +
		"(state IN (?, ?) AND created_at < ?) OR " +
		"(state = ? AND COALESCE(retired_at, created_at) < ?)) " +
		"ORDER BY COALESCE(retired_at, created_at), id "
	if dialector == "sqlite" {
		return base + "LIMIT ?"
	}
	return base + "FOR UPDATE SKIP LOCKED LIMIT ?"
}

// selectExpiredGenerationsSQL 返回锁定过期 retired Generation 的 SQL（同上 SKIP LOCKED 分流）。
func selectExpiredGenerationsSQL(dialector string) string {
	base := "SELECT g.id FROM knowledge_base_index_generations AS g " +
		"WHERE g.workspace_id = ? AND g.status = ? AND g.retired_at < ? " +
		"AND NOT EXISTS (" +
		"SELECT 1 FROM knowledge_bases AS kb " +
		"WHERE kb.workspace_id = g.workspace_id " +
		"AND kb.id = g.knowledge_base_id " +
		"AND kb.active_index_generation_id = g.id) " +
		"ORDER BY g.retired_at, g.id "
	if dialector == "sqlite" {
		return base + "LIMIT ?"
	}
	return base + "FOR UPDATE OF g SKIP LOCKED LIMIT ?"
}

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
	if r.db.Dialector.Name() == "sqlite" {
		// retrieval_fts 是 FTS5 虚拟表，无 FK 级联，删 entry 前先清 FTS 孤儿（对齐同批删除条件）。
		if err := r.db.WithContext(ctx).Exec(
			"DELETE FROM retrieval_fts WHERE entry_id IN ("+
				"SELECT id FROM retrieval_entries WHERE state IN (?, ?) AND created_at < ?)",
			value.RetrievalEntryStaging, value.RetrievalEntryFailed, request.FailedStagingBefore,
		).Error; err != nil {
			return appservice.RetrievalCleanupResult{}, translateDBError(err, "全局清理过期 RetrievalEntry FTS 孤儿失败")
		}
	}
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
		if r.db.Dialector.Name() == "sqlite" {
			// 同上，retired 批次的 FTS 孤儿清理。
			if err := r.db.WithContext(ctx).Exec(
				"DELETE FROM retrieval_fts WHERE entry_id IN ("+
					"SELECT id FROM retrieval_entries WHERE state = ? AND COALESCE(retired_at, created_at) < ?)",
				value.RetrievalEntryRetired, request.RetiredBefore,
			).Error; err != nil {
				return appservice.RetrievalCleanupResult{}, translateDBError(err, "全局清理过期 retired RetrievalEntry FTS 孤儿失败")
			}
		}
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
			if tx.Dialector.Name() == "sqlite" {
				// retrieval_fts 是 FTS5 虚拟表，无 FK 级联，删 entry 前先清 FTS 孤儿。
				if err := tx.WithContext(ctx).Exec(
					"DELETE FROM retrieval_fts WHERE entry_id IN ?", entryIDs,
				).Error; err != nil {
					return translateDBError(err, "清理过期 RetrievalEntry FTS 孤儿失败")
				}
			}
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
		selectExpiredEntriesSQL(tx.Dialector.Name()),
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
		selectExpiredGenerationsSQL(tx.Dialector.Name()),
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
