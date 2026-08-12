package db

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// IndexGenerationDBStore persists double-buffer Generation lifecycle operations.
type IndexGenerationDBStore struct{ db *gorm.DB }

// NewIndexGenerationStore creates a Workspace-scoped Generation store.
func NewIndexGenerationStore(database *gorm.DB) *IndexGenerationDBStore {
	return &IndexGenerationDBStore{db: database}
}

// WithinWorkspace runs one Generation transaction with tenant-local PostgreSQL context.
func (s *IndexGenerationDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.IndexGenerationTx) error,
) error {
	if fn == nil {
		return fmt.Errorf("%w: IndexGeneration transaction callback 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &indexGenerationDBTx{db: tx, workspaceID: workspaceID})
	})
}

// List returns newest Generations under one explicit Workspace/KB lineage.
func (s *IndexGenerationDBStore) List(
	ctx context.Context,
	workspaceID, knowledgeBaseID uuid.UUID,
) ([]*model.IndexGeneration, error) {
	var rows []IndexGenerationRow
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var kbCount int64
		if err := tx.WithContext(ctx).Model(&KnowledgeBaseRow{}).Where(
			"workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, knowledgeBaseID,
		).Count(&kbCount).Error; err != nil {
			return translateDBError(err, "校验 Generation KnowledgeBase 失败")
		}
		if kbCount != 1 {
			return domainerrors.ErrNotFound
		}
		if err := tx.WithContext(ctx).Where(
			"workspace_id = ? AND knowledge_base_id = ?", workspaceID, knowledgeBaseID,
		).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
			return translateDBError(err, "列出 IndexGenerations 失败")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := make([]*model.IndexGeneration, len(rows))
	for index := range rows {
		result[index] = indexGenerationFromRow(&rows[index])
	}
	return result, nil
}

type indexGenerationDBTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *indexGenerationDBTx) GetKnowledgeBaseForUpdate(
	ctx context.Context,
	id uuid.UUID,
) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id,
	).First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定 Generation KnowledgeBase 失败")
	}
	return knowledgeBaseV2FromRow(&row), nil
}

func (tx *indexGenerationDBTx) GetIndexGeneration(
	ctx context.Context,
	id uuid.UUID,
) (*model.IndexGeneration, error) {
	var row IndexGenerationRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"workspace_id = ? AND id = ?", tx.workspaceID, id,
	).First(&row).Error; err != nil {
		return nil, translateDBError(err, "锁定 IndexGeneration 失败")
	}
	return indexGenerationFromRow(&row), nil
}

func (tx *indexGenerationDBTx) GetActiveManualEditStats(
	ctx context.Context,
	knowledgeBaseID uuid.UUID,
) (int64, int64, error) {
	type statsRow struct {
		ManualEditCount    int64
		DisabledChunkCount int64
	}
	var stats statsRow
	// SQLite 不支持 COUNT(*) FILTER，改用 SUM(CASE WHEN ...)，谓词保持一致。
	chunkStatsSelect := "COUNT(*) FILTER (WHERE cr.edit_source = 'user') AS manual_edit_count, " +
		"COUNT(*) FILTER (WHERE cr.enabled = false) AS disabled_chunk_count"
	if tx.db.Dialector.Name() == "sqlite" {
		chunkStatsSelect = "SUM(CASE WHEN cr.edit_source = 'user' THEN 1 ELSE 0 END) AS manual_edit_count, " +
			"SUM(CASE WHEN cr.enabled = false THEN 1 ELSE 0 END) AS disabled_chunk_count"
	}
	if err := tx.db.WithContext(ctx).Table("chunks AS c").Select(chunkStatsSelect).Joins(
		"JOIN documents AS d ON d.workspace_id = c.workspace_id AND d.knowledge_base_id = c.knowledge_base_id "+
			"AND d.id = c.document_id AND d.active_revision_id = c.document_revision_id",
	).Joins(
		"JOIN chunk_revisions AS cr ON cr.workspace_id = c.workspace_id AND cr.chunk_id = c.id "+
			"AND cr.id = c.active_revision_id",
	).Where(
		"c.workspace_id = ? AND c.knowledge_base_id = ? AND d.deleted_at IS NULL",
		tx.workspaceID, knowledgeBaseID,
	).Scan(&stats).Error; err != nil {
		return 0, 0, translateDBError(err, "统计 active Chunk 人工编辑失败")
	}
	return stats.ManualEditCount, stats.DisabledChunkCount, nil
}

func (tx *indexGenerationDBTx) CreateIndexGeneration(
	ctx context.Context,
	generation *model.IndexGeneration,
	job *model.Job,
) error {
	if generation == nil || job == nil || generation.WorkspaceID != tx.workspaceID ||
		job.WorkspaceID != tx.workspaceID || generation.KnowledgeBaseID != job.KnowledgeBaseID ||
		generation.ID != job.IndexGenerationID || generation.Status != value.IndexGenerationBuilding ||
		job.DocumentID != uuid.Nil || job.DocumentRevisionID != uuid.Nil {
		return fmt.Errorf("%w: IndexGeneration/Job lineage 无效", domainerrors.ErrValidation)
	}
	var buildingCount int64
	if err := tx.db.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
		"workspace_id = ? AND knowledge_base_id = ? AND status = ?",
		tx.workspaceID, generation.KnowledgeBaseID, value.IndexGenerationBuilding,
	).Count(&buildingCount).Error; err != nil {
		return translateDBError(err, "检查 building Generation 失败")
	}
	if buildingCount > 0 {
		return domainerrors.ErrGenerationBuildInProgress
	}
	if err := tx.db.WithContext(ctx).Create(indexGenerationToRow(generation)).Error; err != nil {
		return translateDBError(err, "创建 IndexGeneration 失败")
	}
	if err := tx.db.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		return translateDBError(err, "创建 IndexGeneration Job 失败")
	}
	return nil
}

func (tx *indexGenerationDBTx) ActivateIndexGeneration(
	ctx context.Context,
	kb *model.KnowledgeBase,
	candidate *model.IndexGeneration,
	base *model.IndexGeneration,
) error {
	if kb == nil || candidate == nil || kb.WorkspaceID != tx.workspaceID ||
		candidate.WorkspaceID != tx.workspaceID || candidate.KnowledgeBaseID != kb.ID {
		return fmt.Errorf("%w: Generation activation lineage 无效", domainerrors.ErrValidation)
	}
	if candidate.Status == value.IndexGenerationStale {
		result := tx.db.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND status <> ?",
			tx.workspaceID, kb.ID, candidate.ID, value.IndexGenerationRetired,
		).Updates(map[string]any{
			"status": string(value.IndexGenerationStale), "error_class": "generation_stale",
			"error_message": domainerrors.ErrGenerationStale.Error(),
		})
		if result.Error != nil {
			return translateDBError(result.Error, "标记 IndexGeneration stale 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	}
	if base == nil || kb.ActiveIndexGenerationID == nil || *kb.ActiveIndexGenerationID != base.ID ||
		candidate.BaseGenerationID == nil || *candidate.BaseGenerationID != base.ID ||
		candidate.SourceContentVersion != kb.ContentVersion || candidate.Status != value.IndexGenerationReady ||
		candidate.ManualEditDisposition == value.ManualEditPending {
		return domainerrors.ErrGenerationStale
	}
	now := time.Now().UTC()
	baseResult := tx.db.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
		"workspace_id = ? AND knowledge_base_id = ? AND id = ?",
		tx.workspaceID, kb.ID, base.ID,
	).Updates(map[string]any{
		"status": string(value.IndexGenerationRetired), "retired_at": now,
	})
	if baseResult.Error != nil {
		return translateDBError(baseResult.Error, "退役旧 IndexGeneration 失败")
	}
	if baseResult.RowsAffected != 1 {
		return domainerrors.ErrNotFound
	}
	candidateResult := tx.db.WithContext(ctx).Model(&IndexGenerationRow{}).Where(
		"workspace_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?",
		tx.workspaceID, kb.ID, candidate.ID, value.IndexGenerationReady,
	).Updates(map[string]any{
		"manual_edit_disposition": string(candidate.ManualEditDisposition),
		"activated_at":            now, "error_class": "", "error_message": "",
	})
	if candidateResult.Error != nil {
		return translateDBError(candidateResult.Error, "激活 IndexGeneration 失败")
	}
	if candidateResult.RowsAffected != 1 {
		return domainerrors.ErrGenerationNotReady
	}
	kbResult := tx.db.WithContext(ctx).Model(&KnowledgeBaseRow{}).Where(
		"workspace_id = ? AND id = ? AND active_index_generation_id = ? AND content_version = ?",
		tx.workspaceID, kb.ID, base.ID, candidate.SourceContentVersion,
	).Updates(map[string]any{
		"active_index_generation_id": candidate.ID, "updated_at": now,
	})
	if kbResult.Error != nil {
		return translateDBError(kbResult.Error, "切换 KnowledgeBase active Generation 失败")
	}
	if kbResult.RowsAffected != 1 {
		return domainerrors.ErrGenerationStale
	}
	if !reflect.DeepEqual(base.ChunkingConfig, candidate.ChunkingConfig) {
		if err := tx.db.WithContext(ctx).Model(&DocumentChunkSetRow{}).Where(
			"workspace_id = ? AND knowledge_base_id = ? AND status = ? "+
				"AND document_id IN (SELECT id FROM documents WHERE workspace_id = ? AND knowledge_base_id = ? AND kind IN ('file','web')) "+
				"AND id NOT IN (SELECT DISTINCT chunk_set_id FROM retrieval_entries WHERE workspace_id = ? AND index_generation_id = ? AND state = ?)",
			tx.workspaceID, kb.ID, value.ChunkSetReady,
			tx.workspaceID, kb.ID, tx.workspaceID, candidate.ID, value.RetrievalEntryPublished,
		).Updates(map[string]any{
			"status": string(value.ChunkSetArchived), "archived_at": now,
		}).Error; err != nil {
			return translateDBError(err, "归档旧 File/Web ChunkSets 失败")
		}
	}
	return nil
}
