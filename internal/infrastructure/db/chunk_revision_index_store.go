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

// Load reads and validates the persisted lineage for one targeted indexing task.
func (s *ChunkRevisionDBStore) Load(
	ctx context.Context,
	request appservice.ChunkRevisionIndexRequest,
) (*appservice.ChunkRevisionIndexSource, error) {
	var source appservice.ChunkRevisionIndexSource
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		var kb KnowledgeBaseRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND id = ? AND deleted_at IS NULL",
			request.WorkspaceID, request.KnowledgeBaseID,
		).First(&kb).Error; err != nil {
			return translateDBError(err, "读取 ChunkRevision 任务 KnowledgeBase 失败")
		}
		source.KnowledgeBase = knowledgeBaseV2FromRow(&kb)

		var jobRow JobRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.JobID,
		).First(&jobRow).Error; err != nil {
			return translateDBError(err, "读取 ChunkRevision 任务 Job 失败")
		}
		var generationRow IndexGenerationRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.GenerationID,
		).First(&generationRow).Error; err != nil {
			return translateDBError(err, "读取 ChunkRevision 任务 Generation 失败")
		}
		var chunkRow ChunkRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.ChunkID,
		).First(&chunkRow).Error; err != nil {
			return translateDBError(err, "读取 ChunkRevision 任务 Chunk 失败")
		}
		var revisionRows []ChunkRevisionRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND chunk_id = ? AND id IN ?",
			request.WorkspaceID, request.ChunkID, []uuid.UUID{request.BaseRevisionID, request.NewRevisionID},
		).Find(&revisionRows).Error; err != nil {
			return translateDBError(err, "读取 ChunkRevision 任务 revisions 失败")
		}
		if len(revisionRows) != 2 {
			return domainerrors.ErrNotFound
		}
		for index := range revisionRows {
			revision := chunkRevisionFromRow(&revisionRows[index])
			switch revision.ID {
			case request.BaseRevisionID:
				source.BaseRevision = revision
			case request.NewRevisionID:
				source.NewRevision = revision
			}
		}
		source.Generation = indexGenerationFromRow(&generationRow)
		var err error
		source.Chunk, err = chunkV2FromRow(&chunkRow)
		if err != nil {
			return err
		}
		source.Job = jobV2FromRow(&jobRow)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &source, nil
}

// MarkIndexing atomically starts one targeted task and its immutable revision.
func (s *ChunkRevisionDBStore) MarkIndexing(
	ctx context.Context,
	request appservice.ChunkRevisionIndexRequest,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		jobResult := tx.WithContext(ctx).Model(&JobRow{}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND type = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.JobID, chunkRevisionIndexJobTypeDB,
		).Updates(map[string]any{
			"status": string(value.JobStatusRunning), "attempts": gorm.Expr("attempts + 1"),
			"error_class": "", "error_message": "", "updated_at": now,
		})
		if jobResult.Error != nil {
			return translateDBError(jobResult.Error, "启动 ChunkRevision Job 失败")
		}
		if jobResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		revisionResult := tx.WithContext(ctx).Model(&ChunkRevisionRow{}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND chunk_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.ChunkID, request.NewRevisionID,
		).Updates(map[string]any{
			"status": string(value.ChunkRevisionIndexing), "error_class": "", "error_message": "",
		})
		if revisionResult.Error != nil {
			return translateDBError(revisionResult.Error, "启动 ChunkRevision 索引失败")
		}
		if revisionResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// Publish atomically retires the old projection and switches one Chunk revision pointer.
func (s *ChunkRevisionDBStore) Publish(
	ctx context.Context,
	input appservice.PublishChunkRevisionInput,
	entry *model.RetrievalEntry,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, input.WorkspaceID, func(tx *gorm.DB) error {
		var kb KnowledgeBaseRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workspace_id = ? AND id = ? AND deleted_at IS NULL", input.WorkspaceID, input.KnowledgeBaseID,
		).First(&kb).Error; err != nil {
			return translateDBError(err, "锁定 Chunk 发布 KnowledgeBase 失败")
		}
		if kb.ActiveIndexGenerationID == nil || *kb.ActiveIndexGenerationID != input.GenerationID {
			return domainerrors.ErrGenerationStale
		}
		var generation IndexGenerationRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND status IN ?",
			input.WorkspaceID, input.KnowledgeBaseID, input.GenerationID,
			[]string{string(value.IndexGenerationReady), string(value.IndexGenerationBuilding)},
		).First(&generation).Error; err != nil {
			return translateDBError(err, "锁定 Chunk 发布 Generation 失败")
		}
		var chunk ChunkRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			input.WorkspaceID, input.KnowledgeBaseID, input.ChunkID,
		).First(&chunk).Error; err != nil {
			return translateDBError(err, "锁定待发布 Chunk 失败")
		}
		if chunk.ActiveRevisionID == nil || *chunk.ActiveRevisionID != input.BaseRevisionID {
			return domainerrors.ErrRevisionConflict
		}
		if kb.ContentVersion != input.ExpectedContentVersion ||
			generation.IndexedContentVersion != input.ExpectedContentVersion {
			return domainerrors.ErrGenerationStale
		}
		var revisions []ChunkRevisionRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workspace_id = ? AND chunk_id = ? AND id IN ?",
			input.WorkspaceID, input.ChunkID, []uuid.UUID{input.BaseRevisionID, input.NewRevisionID},
		).Find(&revisions).Error; err != nil {
			return translateDBError(err, "锁定待发布 ChunkRevisions 失败")
		}
		if len(revisions) != 2 {
			return domainerrors.ErrNotFound
		}
		var next ChunkRevisionRow
		for index := range revisions {
			if revisions[index].ID == input.NewRevisionID {
				next = revisions[index]
			}
		}
		if next.ID == uuid.Nil || next.BaseRevisionID == nil || *next.BaseRevisionID != input.BaseRevisionID ||
			next.KnowledgeBaseID != input.KnowledgeBaseID || next.DocumentID != chunk.DocumentID ||
			next.DocumentRevisionID != chunk.DocumentRevisionID || next.ChunkSetID != chunk.ChunkSetID {
			return domainerrors.ErrRevisionConflict
		}
		if err := validateChunkRevisionStaging(ctx, tx, input, next, entry); err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := tx.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
			"workspace_id = ? AND index_generation_id = ? AND chunk_id = ? AND state = ?",
			input.WorkspaceID, input.GenerationID, input.ChunkID, value.RetrievalEntryPublished,
		).Updates(map[string]any{
			"state": string(value.RetrievalEntryRetired), "retired_at": now,
		}).Error; err != nil {
			return translateDBError(err, "退役 Chunk 旧 RetrievalEntry 失败")
		}
		if next.Enabled {
			result := tx.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
				"workspace_id = ? AND id = ? AND state = ?",
				input.WorkspaceID, entry.ID, value.RetrievalEntryStaging,
			).Updates(map[string]any{
				"state": string(value.RetrievalEntryPublished), "published_at": now, "retired_at": nil,
			})
			if result.Error != nil {
				return translateDBError(result.Error, "发布 Chunk RetrievalEntry 失败")
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: Chunk RetrievalEntry staging 数量变化", domainerrors.ErrConflict)
			}
		}
		chunkResult := tx.WithContext(ctx).Model(&ChunkRow{}).Where(
			"workspace_id = ? AND id = ? AND active_revision_id = ?",
			input.WorkspaceID, input.ChunkID, input.BaseRevisionID,
		).Update("active_revision_id", input.NewRevisionID)
		if chunkResult.Error != nil {
			return translateDBError(chunkResult.Error, "切换 Chunk active revision 失败")
		}
		if chunkResult.RowsAffected != 1 {
			return domainerrors.ErrRevisionConflict
		}
		revisionUpdates := map[string]any{
			"status": string(value.ChunkRevisionReady), "error_class": "", "error_message": "",
		}
		if next.Enabled {
			revisionUpdates["indexed_at"] = now
		} else {
			revisionUpdates["indexed_at"] = nil
		}
		if err := tx.WithContext(ctx).Model(&ChunkRevisionRow{}).Where(
			"workspace_id = ? AND id = ?", input.WorkspaceID, input.NewRevisionID,
		).Updates(revisionUpdates).Error; err != nil {
			return translateDBError(err, "完成 ChunkRevision 发布失败")
		}
		nextVersion := input.ExpectedContentVersion + 1
		kbResult := tx.WithContext(ctx).Model(&KnowledgeBaseRow{}).Where(
			"workspace_id = ? AND id = ? AND content_version = ?",
			input.WorkspaceID, input.KnowledgeBaseID, input.ExpectedContentVersion,
		).Updates(map[string]any{"content_version": nextVersion, "updated_at": now})
		if kbResult.Error != nil {
			return translateDBError(kbResult.Error, "推进 Chunk KnowledgeBase content version 失败")
		}
		if kbResult.RowsAffected != 1 {
			return domainerrors.ErrGenerationStale
		}
		generationResult := tx.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
			"workspace_id = ? AND id = ? AND indexed_content_version = ?",
			input.WorkspaceID, input.GenerationID, input.ExpectedContentVersion,
		).Update("indexed_content_version", nextVersion)
		if generationResult.Error != nil {
			return translateDBError(generationResult.Error, "推进 Chunk Generation indexed version 失败")
		}
		if generationResult.RowsAffected != 1 {
			return domainerrors.ErrGenerationStale
		}
		return nil
	})
}

func validateChunkRevisionStaging(
	ctx context.Context,
	tx *gorm.DB,
	input appservice.PublishChunkRevisionInput,
	next ChunkRevisionRow,
	entry *model.RetrievalEntry,
) error {
	if !next.Enabled {
		if entry != nil {
			return fmt.Errorf("%w: disabled ChunkRevision 不能发布 RetrievalEntry", domainerrors.ErrValidation)
		}
		return nil
	}
	if entry == nil || entry.WorkspaceID != input.WorkspaceID || entry.KnowledgeBaseID != input.KnowledgeBaseID ||
		entry.IndexGenerationID != input.GenerationID || entry.ChunkID != input.ChunkID ||
		entry.ChunkRevisionID != input.NewRevisionID || entry.State != value.RetrievalEntryStaging {
		return fmt.Errorf("%w: Chunk Revision staging lineage 无效", domainerrors.ErrValidation)
	}
	var count int64
	// SQLite 的 embedding/fts_document 移到独立表，改用 EXISTS 校验（同 requireCompleteStaging）。
	cond := "workspace_id = ? AND knowledge_base_id = ? AND index_generation_id = ? AND id = ? " +
		"AND chunk_id = ? AND chunk_revision_id = ? AND state = ?"
	if tx.Dialector.Name() == "sqlite" {
		cond += " AND dimension IS NOT NULL" +
			" AND EXISTS (SELECT 1 FROM retrieval_embeddings WHERE entry_id = retrieval_entries.id)" +
			" AND EXISTS (SELECT 1 FROM retrieval_fts WHERE entry_id = retrieval_entries.id)"
	} else {
		cond += " AND embedding IS NOT NULL AND dimension IS NOT NULL AND fts_document IS NOT NULL"
	}
	if err := tx.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
		cond,
		input.WorkspaceID, input.KnowledgeBaseID, input.GenerationID, entry.ID,
		input.ChunkID, input.NewRevisionID, value.RetrievalEntryStaging,
	).Count(&count).Error; err != nil {
		return translateDBError(err, "校验 Chunk Revision staging 失败")
	}
	if count != 1 {
		return fmt.Errorf("%w: Chunk Revision staging 不完整", domainerrors.ErrConflict)
	}
	return nil
}

// MarkSucceeded marks a targeted indexing Job complete.
func (s *ChunkRevisionDBStore) MarkSucceeded(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	return s.updateChunkRevisionJob(ctx, workspaceID, jobID, map[string]any{
		"status": string(value.JobStatusCompleted), "error_class": "", "error_message": "",
		"updated_at": time.Now().UTC(),
	}, "完成 ChunkRevision Job 失败")
}

// MarkFailed records one targeted indexing failure on both Job and immutable Revision.
func (s *ChunkRevisionDBStore) MarkFailed(
	ctx context.Context,
	request appservice.ChunkRevisionIndexRequest,
	errorClass, message string,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		jobResult := tx.WithContext(ctx).Model(&JobRow{}).Where(
			"workspace_id = ? AND id = ?", request.WorkspaceID, request.JobID,
		).Updates(map[string]any{
			"status": string(value.JobStatusFailed), "error_class": errorClass,
			"error_message": message, "updated_at": now,
		})
		if jobResult.Error != nil {
			return translateDBError(jobResult.Error, "标记 ChunkRevision Job 失败")
		}
		if jobResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		revisionResult := tx.WithContext(ctx).Model(&ChunkRevisionRow{}).Where(
			"workspace_id = ? AND id = ? AND status <> ?",
			request.WorkspaceID, request.NewRevisionID, value.ChunkRevisionReady,
		).Updates(map[string]any{
			"status": string(value.ChunkRevisionFailed), "error_class": errorClass, "error_message": message,
		})
		if revisionResult.Error != nil {
			return translateDBError(revisionResult.Error, "标记 ChunkRevision 失败")
		}
		return nil
	})
}

func (s *ChunkRevisionDBStore) updateChunkRevisionJob(
	ctx context.Context,
	workspaceID, jobID uuid.UUID,
	updates map[string]any,
	description string,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&JobRow{}).Where(
			"workspace_id = ? AND id = ?", workspaceID, jobID,
		).Updates(updates)
		if result.Error != nil {
			return translateDBError(result.Error, description)
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

const chunkRevisionIndexJobTypeDB = "chunk_revision_index"
