package db

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// RetrievalRepository persists the rebuildable FTS/vector projection.
type RetrievalRepository struct {
	db *gorm.DB
}

// NewRetrievalRepository creates a RetrievalEntry repository.
func NewRetrievalRepository(database *gorm.DB) *RetrievalRepository {
	return &RetrievalRepository{db: database}
}

// StageBatch atomically replaces staging entries for the same Generation/Chunks.
func (r *RetrievalRepository) StageBatch(
	ctx context.Context,
	workspaceID uuid.UUID,
	ftsConfig string,
	dimension int,
	entries []indexport.StageEntry,
) error {
	if workspaceID == uuid.Nil || strings.TrimSpace(ftsConfig) == "" || !value.IsSupportedEmbeddingDimension(dimension) {
		return fmt.Errorf("%w: Retrieval staging Workspace/FTS/dimension 无效", domainerrors.ErrValidation)
	}
	rows := make([]*RetrievalEntryRow, len(entries))
	vectors := make([]string, len(entries))
	for index, staged := range entries {
		if err := validateStagingEntry(workspaceID, dimension, staged); err != nil {
			return fmt.Errorf("Retrieval staging entry %d: %w", index, err)
		}
		row, err := retrievalEntryToRow(staged.Entry)
		if err != nil {
			return err
		}
		row.FTSDocument = ""
		row.Embedding = nil
		row.Dimension = nil
		rows[index] = row
		vectors[index] = halfVectorLiteral(staged.Embedding)
	}
	if len(rows) == 0 {
		return nil
	}
	return NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		for _, row := range rows {
			if err := tx.WithContext(ctx).
				Where(
					"workspace_id = ? AND index_generation_id = ? AND chunk_id = ? AND state = ?",
					workspaceID, row.IndexGenerationID, row.ChunkID, value.RetrievalEntryStaging,
				).Delete(&RetrievalEntryRow{}).Error; err != nil {
				return translateDBError(err, "清理旧 RetrievalEntry staging 失败")
			}
		}
		if err := tx.WithContext(ctx).CreateInBatches(rows, 200).Error; err != nil {
			return translateDBError(err, "批量创建 RetrievalEntry staging 失败")
		}
		for index, row := range rows {
			result := tx.WithContext(ctx).Exec(
				"UPDATE retrieval_entries "+
					"SET fts_document = to_tsvector(?::regconfig, search_content), "+
					"embedding = ?::halfvec, dimension = ? "+
					"WHERE workspace_id = ? AND id = ? AND state = ?",
				ftsConfig, vectors[index], dimension, workspaceID, row.ID, value.RetrievalEntryStaging,
			)
			if result.Error != nil {
				return translateFTSConfigError(
					result.Error, ftsConfig, "写入 RetrievalEntry FTS/Embedding 失败",
				)
			}
			if result.RowsAffected != 1 {
				return domainerrors.ErrNotFound
			}
		}
		return nil
	})
}

func validateStagingEntry(workspaceID uuid.UUID, dimension int, staged indexport.StageEntry) error {
	entry := staged.Entry
	if entry == nil || entry.ID == uuid.Nil || entry.WorkspaceID != workspaceID ||
		entry.KnowledgeBaseID == uuid.Nil || entry.IndexGenerationID == uuid.Nil ||
		entry.DocumentID == uuid.Nil || entry.DocumentRevisionID == uuid.Nil ||
		entry.ChunkSetID == uuid.Nil || entry.ChunkID == uuid.Nil || entry.ChunkRevisionID == uuid.Nil ||
		entry.State != value.RetrievalEntryStaging || strings.TrimSpace(entry.SearchContent) == "" ||
		strings.TrimSpace(entry.Content) == "" || len(staged.Embedding) != dimension {
		return fmt.Errorf("%w: RetrievalEntry lineage/content/vector 无效", domainerrors.ErrValidation)
	}
	for _, component := range staged.Embedding {
		if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
			return domainerrors.ErrInvalidEmbeddingResponse
		}
	}
	return nil
}

func halfVectorLiteral(vector []float32) string {
	buffer := make([]byte, 0, len(vector)*8+2)
	buffer = append(buffer, '[')
	for index, component := range vector {
		if index > 0 {
			buffer = append(buffer, ',')
		}
		buffer = strconv.AppendFloat(buffer, float64(component), 'g', -1, 32)
	}
	buffer = append(buffer, ']')
	return string(buffer)
}
