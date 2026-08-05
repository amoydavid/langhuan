package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// ChunkSetRepository persists idempotent complete chunking results.
type ChunkSetRepository struct {
	db *gorm.DB
}

// GetReadyIndexSource loads one ready ChunkSet and each Chunk's active revision in sequence order.
func (r *ChunkSetRepository) GetReadyIndexSource(
	ctx context.Context,
	workspaceID, chunkSetID uuid.UUID,
) (*indexport.Source, error) {
	var source *indexport.Source
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var setRow DocumentChunkSetRow
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND id = ? AND status = ?", workspaceID, chunkSetID, value.ChunkSetReady).
			First(&setRow).Error; err != nil {
			return translateDBError(err, "读取 ready ChunkSet 失败")
		}
		var chunkRows []ChunkRow
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND chunk_set_id = ?", workspaceID, chunkSetID).
			Order("sequence ASC").Find(&chunkRows).Error; err != nil {
			return translateDBError(err, "读取 ChunkSet Chunks 失败")
		}
		var revisionRows []ChunkRevisionRow
		if len(chunkRows) > 0 {
			if err := tx.WithContext(ctx).Table("chunk_revisions").
				Select("chunk_revisions.*").
				Joins("JOIN chunks ON chunks.workspace_id = chunk_revisions.workspace_id AND chunks.active_revision_id = chunk_revisions.id").
				Where("chunks.workspace_id = ? AND chunks.chunk_set_id = ?", workspaceID, chunkSetID).
				Order("chunks.sequence ASC").Find(&revisionRows).Error; err != nil {
				return translateDBError(err, "读取 active ChunkRevisions 失败")
			}
		}
		if len(chunkRows) != len(revisionRows) || int64(len(chunkRows)) != setRow.ChunkCount {
			return fmt.Errorf("%w: ready ChunkSet active revision 不完整", domainerrors.ErrValidation)
		}
		chunks := make([]*model.Chunk, len(chunkRows))
		revisions := make([]*model.ChunkRevision, len(revisionRows))
		for index := range chunkRows {
			chunk, err := chunkV2FromRow(&chunkRows[index])
			if err != nil {
				return err
			}
			chunks[index] = chunk
			revisions[index] = chunkRevisionFromRow(&revisionRows[index])
		}
		source = &indexport.Source{
			ChunkSet: documentChunkSetFromRow(&setRow), Chunks: chunks, Revisions: revisions,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return source, nil
}

// NewChunkSetRepository creates a ChunkSet repository.
func NewChunkSetRepository(database *gorm.DB) *ChunkSetRepository {
	return &ChunkSetRepository{db: database}
}

// GetOrCreate locks or creates the unique ChunkSet build row.
func (r *ChunkSetRepository) GetOrCreate(
	ctx context.Context,
	workspaceID uuid.UUID,
	candidate *model.DocumentChunkSet,
) (*model.DocumentChunkSet, error) {
	if err := validateChunkSetCandidate(workspaceID, candidate); err != nil {
		return nil, err
	}
	var result *model.DocumentChunkSet
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		row := documentChunkSetToRow(candidate)
		if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "workspace_id"}, {Name: "document_revision_id"}, {Name: "strategy"},
				{Name: "chunker_version"}, {Name: "config_hash"},
			},
			DoNothing: true,
		}).Create(row).Error; err != nil {
			return translateDBError(err, "创建 ChunkSet 失败")
		}
		var stored DocumentChunkSetRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(
				"workspace_id = ? AND document_revision_id = ? AND strategy = ? AND chunker_version = ? AND config_hash = ?",
				workspaceID, candidate.DocumentRevisionID, candidate.Strategy, candidate.ChunkerVersion, candidate.ConfigHash,
			).First(&stored).Error; err != nil {
			return translateDBError(err, "锁定 ChunkSet 失败")
		}
		if stored.KnowledgeBaseID != candidate.KnowledgeBaseID || stored.DocumentID != candidate.DocumentID {
			return fmt.Errorf("%w: ChunkSet lineage 不一致", domainerrors.ErrValidation)
		}
		switch value.ChunkSetStatus(stored.Status) {
		case value.ChunkSetReady, value.ChunkSetBuilding:
		case value.ChunkSetFailed:
			if err := tx.WithContext(ctx).Model(&DocumentChunkSetRow{}).
				Where("workspace_id = ? AND id = ?", workspaceID, stored.ID).
				Updates(map[string]any{
					"status": string(value.ChunkSetBuilding), "chunk_count": 0,
					"error_class": "", "error_message": "", "ready_at": nil,
				}).Error; err != nil {
				return translateDBError(err, "重开 ChunkSet 构建失败")
			}
			stored.Status = string(value.ChunkSetBuilding)
			stored.ChunkCount = 0
			stored.ErrorClass = ""
			stored.ErrorMessage = ""
			stored.ReadyAt = nil
		case value.ChunkSetArchived:
			return fmt.Errorf("%w: 已归档 ChunkSet 不能原地重建", domainerrors.ErrConflict)
		default:
			return fmt.Errorf("%w: ChunkSet status=%q 无效", domainerrors.ErrValidation, stored.Status)
		}
		result = documentChunkSetFromRow(&stored)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Complete atomically replaces an unfinished set's Chunks/system revisions and marks it ready.
func (r *ChunkSetRepository) Complete(
	ctx context.Context,
	workspaceID, chunkSetID uuid.UUID,
	chunks []*model.Chunk,
	revisions []*model.ChunkRevision,
) (*model.DocumentChunkSet, error) {
	if workspaceID == uuid.Nil || chunkSetID == uuid.Nil {
		return nil, fmt.Errorf("%w: Workspace/ChunkSet ID 不能为空", domainerrors.ErrValidation)
	}
	var result *model.DocumentChunkSet
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		var setRow DocumentChunkSetRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND id = ?", workspaceID, chunkSetID).
			First(&setRow).Error; err != nil {
			return translateDBError(err, "锁定 ChunkSet 失败")
		}
		if value.ChunkSetStatus(setRow.Status) == value.ChunkSetReady {
			result = documentChunkSetFromRow(&setRow)
			return nil
		}
		if status := value.ChunkSetStatus(setRow.Status); status != value.ChunkSetBuilding && status != value.ChunkSetFailed {
			return fmt.Errorf("%w: ChunkSet status=%q 不能完成", domainerrors.ErrConflict, status)
		}
		chunkRows, revisionRows, err := encodeChunkSetBuild(workspaceID, &setRow, chunks, revisions)
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).
			Where("workspace_id = ? AND chunk_set_id = ?", workspaceID, chunkSetID).
			Delete(&ChunkRow{}).Error; err != nil {
			return translateDBError(err, "清理未完成 ChunkSet 分块失败")
		}
		if len(chunkRows) > 0 {
			if err := tx.WithContext(ctx).CreateInBatches(chunkRows, 200).Error; err != nil {
				return translateDBError(err, "批量创建 Chunk 失败")
			}
			if err := tx.WithContext(ctx).CreateInBatches(revisionRows, 200).Error; err != nil {
				return translateDBError(err, "批量创建 system ChunkRevision 失败")
			}
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"status": string(value.ChunkSetReady), "chunk_count": len(chunkRows),
			"error_class": "", "error_message": "", "ready_at": now,
		}
		if err := tx.WithContext(ctx).Model(&DocumentChunkSetRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, chunkSetID).
			Updates(updates).Error; err != nil {
			return translateDBError(err, "完成 ChunkSet 失败")
		}
		setRow.Status = string(value.ChunkSetReady)
		setRow.ChunkCount = int64(len(chunkRows))
		setRow.ErrorClass = ""
		setRow.ErrorMessage = ""
		setRow.ReadyAt = &now
		result = documentChunkSetFromRow(&setRow)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validateChunkSetCandidate(workspaceID uuid.UUID, set *model.DocumentChunkSet) error {
	if set == nil || workspaceID == uuid.Nil || set.ID == uuid.Nil || set.WorkspaceID != workspaceID ||
		set.KnowledgeBaseID == uuid.Nil || set.DocumentID == uuid.Nil || set.DocumentRevisionID == uuid.Nil {
		return fmt.Errorf("%w: ChunkSet lineage 不能为空", domainerrors.ErrValidation)
	}
	if set.Strategy != value.ChunkStrategyStandard && set.Strategy != value.ChunkStrategyFAQ {
		return fmt.Errorf("%w: ChunkSet strategy=%q 无效", domainerrors.ErrValidation, set.Strategy)
	}
	if set.ChunkerVersion < 1 || set.ConfigHash == "" || set.Status != value.ChunkSetBuilding {
		return fmt.Errorf("%w: ChunkSet version/hash/status 无效", domainerrors.ErrValidation)
	}
	return nil
}

func encodeChunkSetBuild(
	workspaceID uuid.UUID,
	set *DocumentChunkSetRow,
	chunks []*model.Chunk,
	revisions []*model.ChunkRevision,
) ([]*ChunkRow, []*ChunkRevisionRow, error) {
	if len(chunks) != len(revisions) {
		return nil, nil, fmt.Errorf("%w: Chunk 与 system revision 数量不一致", domainerrors.ErrValidation)
	}
	chunkRows := make([]*ChunkRow, len(chunks))
	revisionRows := make([]*ChunkRevisionRow, len(revisions))
	for index := range chunks {
		chunk, revision := chunks[index], revisions[index]
		if chunk == nil || revision == nil || chunk.WorkspaceID != workspaceID ||
			chunk.KnowledgeBaseID != set.KnowledgeBaseID || chunk.DocumentID != set.DocumentID ||
			chunk.DocumentRevisionID != set.DocumentRevisionID || chunk.ChunkSetID != set.ID ||
			chunk.Sequence < 0 || revision.WorkspaceID != workspaceID ||
			revision.KnowledgeBaseID != set.KnowledgeBaseID || revision.DocumentID != set.DocumentID ||
			revision.DocumentRevisionID != set.DocumentRevisionID || revision.ChunkSetID != set.ID ||
			revision.ChunkID != chunk.ID || revision.RevisionNo != 1 ||
			revision.EditSource != value.ChunkEditSourceSystem || chunk.ActiveRevisionID == nil ||
			*chunk.ActiveRevisionID != revision.ID {
			return nil, nil, fmt.Errorf("%w: ChunkSet 第 %d 个 Chunk/system revision lineage 无效", domainerrors.ErrValidation, index)
		}
		if chunk.Role != "" {
			if err := chunk.ValidateLineage(); err != nil {
				return nil, nil, err
			}
		}
		row, err := chunkV2ToRow(chunk)
		if err != nil {
			return nil, nil, err
		}
		chunkRows[index] = row
		revisionRows[index] = chunkRevisionToRow(revision)
	}
	return chunkRows, revisionRows, nil
}
