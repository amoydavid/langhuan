package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
)

type ChunkRepository struct {
	db *gorm.DB
}

func NewChunkRepository(db *gorm.DB) *ChunkRepository {
	return &ChunkRepository{db: db}
}

// ListByChunkSet returns Chunks in deterministic sequence order inside one Workspace.
func (r *ChunkRepository) ListByChunkSet(ctx context.Context, workspaceID, chunkSetID uuid.UUID) ([]*model.Chunk, error) {
	var rows []ChunkRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND chunk_set_id = ?", workspaceID, chunkSetID).
		Order("CASE role WHEN 'parent' THEN 0 WHEN 'child' THEN 1 ELSE 2 END, sequence ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出 ChunkSet 分块失败: %w", err)
	}
	chunks := make([]*model.Chunk, len(rows))
	for index := range rows {
		chunk, err := chunkV2FromRow(&rows[index])
		if err != nil {
			return nil, err
		}
		chunks[index] = chunk
	}
	return chunks, nil
}

func chunkToRow(chunk *model.Chunk) (*ChunkRow, error) {
	if chunk.SourceContent == "" {
		copy := *chunk
		copy.SourceContent = chunk.Content
		return chunkV2ToRow(&copy)
	}
	return chunkV2ToRow(chunk)
}

func chunkFromRow(row *ChunkRow) (*model.Chunk, error) {
	chunk, err := chunkV2FromRow(row)
	if err != nil {
		return nil, err
	}
	chunk.Content = chunk.SourceContent
	chunk.EmbeddingContent = chunk.SourceContent
	return chunk, nil
}
