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

// Load reads the trusted inactive-build snapshot and active Document inputs.
func (s *IndexGenerationDBStore) Load(
	ctx context.Context,
	request appservice.IndexGenerationBuildRequest,
) (*appservice.IndexGenerationBuildSource, error) {
	var source appservice.IndexGenerationBuildSource
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		var kbRow KnowledgeBaseRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND id = ? AND deleted_at IS NULL", request.WorkspaceID, request.KnowledgeBaseID,
		).First(&kbRow).Error; err != nil {
			return translateDBError(err, "读取 Generation build KnowledgeBase 失败")
		}
		var generationRow IndexGenerationRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.GenerationID,
		).First(&generationRow).Error; err != nil {
			return translateDBError(err, "读取 Generation build candidate 失败")
		}
		if generationRow.BaseGenerationID == nil {
			return fmt.Errorf("%w: Generation build base 不能为空", domainerrors.ErrValidation)
		}
		var baseRow IndexGenerationRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, *generationRow.BaseGenerationID,
		).First(&baseRow).Error; err != nil {
			return translateDBError(err, "读取 Generation build base 失败")
		}
		var jobRow JobRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND index_generation_id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.JobID, request.GenerationID,
		).First(&jobRow).Error; err != nil {
			return translateDBError(err, "读取 Generation build Job 失败")
		}
		type documentRow struct {
			DocumentID         uuid.UUID
			DocumentRevisionID uuid.UUID
			ChunkSetID         *uuid.UUID
			Kind               string
		}
		var rows []documentRow
		if err := tx.WithContext(ctx).Table("documents AS d").Select(
			"d.id AS document_id, d.active_revision_id AS document_revision_id, d.kind, "+
				"COALESCE((SELECT re.chunk_set_id FROM retrieval_entries AS re "+
				"WHERE re.workspace_id = d.workspace_id AND re.index_generation_id = ? "+
				"AND re.document_id = d.id AND re.state = 'published' LIMIT 1), "+
				"(SELECT dcs.id FROM document_chunk_sets AS dcs WHERE dcs.workspace_id = d.workspace_id "+
				"AND dcs.document_revision_id = d.active_revision_id AND dcs.status = 'ready' "+
				"ORDER BY dcs.ready_at DESC, dcs.id DESC LIMIT 1)) AS chunk_set_id",
			baseRow.ID,
		).Where(
			"d.workspace_id = ? AND d.knowledge_base_id = ? AND d.active_revision_id IS NOT NULL "+
				"AND d.status = ? AND d.deleted_at IS NULL",
			request.WorkspaceID, request.KnowledgeBaseID, value.DocumentStatusReady,
		).Order("d.id ASC").Scan(&rows).Error; err != nil {
			return translateDBError(err, "列出 Generation build Documents 失败")
		}
		documents := make([]appservice.IndexGenerationBuildDocument, len(rows))
		for index, row := range rows {
			chunkSetID := uuid.Nil
			if row.ChunkSetID != nil {
				chunkSetID = *row.ChunkSetID
			}
			documents[index] = appservice.IndexGenerationBuildDocument{
				DocumentID: row.DocumentID, DocumentRevisionID: row.DocumentRevisionID,
				ChunkSetID: chunkSetID, Kind: value.DocumentKind(row.Kind),
			}
		}
		source = appservice.IndexGenerationBuildSource{
			Job: jobV2FromRow(&jobRow), KnowledgeBase: knowledgeBaseV2FromRow(&kbRow),
			Generation: indexGenerationFromRow(&generationRow), BaseGeneration: indexGenerationFromRow(&baseRow),
			Documents: documents,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &source, nil
}

// MarkRunning records one full rebuild attempt.
func (s *IndexGenerationDBStore) MarkRunning(
	ctx context.Context,
	request appservice.IndexGenerationBuildRequest,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&JobRow{}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND index_generation_id = ? AND type = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.JobID, request.GenerationID,
			indexGenerationBuildJobTypeDB,
		).Updates(map[string]any{
			"status": string(value.JobStatusRunning), "attempts": gorm.Expr("attempts + 1"),
			"error_class": "", "error_message": "", "updated_at": time.Now().UTC(),
		})
		if result.Error != nil {
			return translateDBError(result.Error, "启动 Generation build Job 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// Complete publishes all inactive entries and marks the Generation ready atomically.
func (s *IndexGenerationDBStore) Complete(
	ctx context.Context,
	request appservice.IndexGenerationBuildRequest,
	entries []*model.RetrievalEntry,
	documentCount, chunkCount int64,
) error {
	var outcome error
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		var kb KnowledgeBaseRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workspace_id = ? AND id = ? AND deleted_at IS NULL", request.WorkspaceID, request.KnowledgeBaseID,
		).First(&kb).Error; err != nil {
			return translateDBError(err, "锁定 Generation build KnowledgeBase 失败")
		}
		var generation IndexGenerationRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
			request.WorkspaceID, request.KnowledgeBaseID, request.GenerationID,
		).First(&generation).Error; err != nil {
			return translateDBError(err, "锁定 Generation build candidate 失败")
		}
		if generation.Status == string(value.IndexGenerationReady) {
			return s.updateGenerationBuildJobTx(ctx, tx, request, value.JobStatusCompleted, "", "")
		}
		if generation.Status != string(value.IndexGenerationBuilding) {
			return domainerrors.ErrGenerationNotReady
		}
		if kb.ActiveIndexGenerationID == nil || generation.BaseGenerationID == nil ||
			*kb.ActiveIndexGenerationID != *generation.BaseGenerationID ||
			kb.ContentVersion != generation.SourceContentVersion {
			outcome = domainerrors.ErrGenerationStale
			if err := tx.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
				"workspace_id = ? AND id = ?", request.WorkspaceID, request.GenerationID,
			).Updates(map[string]any{
				"status": string(value.IndexGenerationStale), "error_class": "generation_stale",
				"error_message": domainerrors.ErrGenerationStale.Error(),
			}).Error; err != nil {
				return translateDBError(err, "标记 Generation build stale 失败")
			}
			return s.updateGenerationBuildJobTx(
				ctx, tx, request, value.JobStatusFailed, "generation_stale", domainerrors.ErrGenerationStale.Error(),
			)
		}
		entryIDs := make([]uuid.UUID, len(entries))
		for index, entry := range entries {
			if entry == nil || entry.WorkspaceID != request.WorkspaceID ||
				entry.KnowledgeBaseID != request.KnowledgeBaseID || entry.IndexGenerationID != request.GenerationID ||
				entry.State != value.RetrievalEntryStaging {
				return fmt.Errorf("%w: Generation build entry lineage 无效", domainerrors.ErrValidation)
			}
			entryIDs[index] = entry.ID
		}
		if len(entryIDs) > 0 {
			var count int64
			for start := 0; start < len(entryIDs); start += publishEntryBatchSize {
				end := min(start+publishEntryBatchSize, len(entryIDs))
				var batchCount int64
				if err := tx.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
					"workspace_id = ? AND index_generation_id = ? AND id IN ? AND state = ? "+
						"AND embedding IS NOT NULL AND dimension IS NOT NULL AND fts_document IS NOT NULL",
					request.WorkspaceID, request.GenerationID, entryIDs[start:end], value.RetrievalEntryStaging,
				).Count(&batchCount).Error; err != nil {
					return translateDBError(err, "校验 Generation staging entries 失败")
				}
				count += batchCount
			}
			if count != int64(len(entryIDs)) {
				return fmt.Errorf("%w: Generation staging entries 不完整", domainerrors.ErrConflict)
			}
			// 与文档发布一致按批 UPDATE：整 generation 的 entries 可能上万，
			// 一次性 id IN 会生成超大报文并锁全库行。
			publishedAt := time.Now().UTC()
			var published int64
			for start := 0; start < len(entryIDs); start += publishEntryBatchSize {
				end := min(start+publishEntryBatchSize, len(entryIDs))
				result := tx.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
					"workspace_id = ? AND index_generation_id = ? AND id IN ? AND state = ?",
					request.WorkspaceID, request.GenerationID, entryIDs[start:end], value.RetrievalEntryStaging,
				).Updates(map[string]any{
					"state": string(value.RetrievalEntryPublished), "published_at": publishedAt, "retired_at": nil,
				})
				if result.Error != nil {
					return translateDBError(result.Error, "发布 inactive Generation entries 失败")
				}
				published += result.RowsAffected
			}
			if published != int64(len(entryIDs)) {
				return fmt.Errorf("%w: Generation staging 数量变化", domainerrors.ErrConflict)
			}
		}
		now := time.Now().UTC()
		result := tx.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
			"workspace_id = ? AND id = ? AND status = ?",
			request.WorkspaceID, request.GenerationID, value.IndexGenerationBuilding,
		).Updates(map[string]any{
			"status": string(value.IndexGenerationReady), "document_count": documentCount,
			"chunk_count": chunkCount, "indexed_count": len(entryIDs),
			"indexed_content_version": generation.SourceContentVersion,
			"error_class":             "", "error_message": "", "ready_at": now,
		})
		if result.Error != nil {
			return translateDBError(result.Error, "完成 IndexGeneration build 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrGenerationNotReady
		}
		return s.updateGenerationBuildJobTx(ctx, tx, request, value.JobStatusCompleted, "", "")
	})
	if err != nil {
		return err
	}
	return outcome
}

// RecordFailure persists one build-attempt failure and optionally terminates the Generation.
func (s *IndexGenerationDBStore) RecordFailure(
	ctx context.Context,
	request appservice.IndexGenerationBuildRequest,
	errorClass, message string,
	terminal bool,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		var generation IndexGenerationRow
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND id = ?", request.WorkspaceID, request.GenerationID,
		).First(&generation).Error; err != nil {
			return translateDBError(err, "读取失败 Generation build 失败")
		}
		if generation.Status == string(value.IndexGenerationReady) {
			return s.updateGenerationBuildJobTx(ctx, tx, request, value.JobStatusCompleted, "", "")
		}
		if terminal && generation.Status == string(value.IndexGenerationBuilding) {
			if err := tx.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
				"workspace_id = ? AND id = ?", request.WorkspaceID, request.GenerationID,
			).Updates(map[string]any{
				"status": string(value.IndexGenerationFailed), "error_class": errorClass, "error_message": message,
			}).Error; err != nil {
				return translateDBError(err, "标记 Generation build 失败")
			}
		}
		return s.updateGenerationBuildJobTx(ctx, tx, request, value.JobStatusFailed, errorClass, message)
	})
}

func (s *IndexGenerationDBStore) updateGenerationBuildJobTx(
	ctx context.Context,
	tx *gorm.DB,
	request appservice.IndexGenerationBuildRequest,
	status value.JobStatus,
	errorClass, message string,
) error {
	result := tx.WithContext(ctx).Model(&JobRow{}).Where(
		"workspace_id = ? AND id = ? AND index_generation_id = ?",
		request.WorkspaceID, request.JobID, request.GenerationID,
	).Updates(map[string]any{
		"status": string(status), "error_class": errorClass, "error_message": message,
		"updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return translateDBError(result.Error, "更新 Generation build Job 失败")
	}
	if result.RowsAffected != 1 {
		return domainerrors.ErrNotFound
	}
	return nil
}

const indexGenerationBuildJobTypeDB = "index_generation_build"
