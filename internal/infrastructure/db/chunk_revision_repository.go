package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
)

type chunkRevisionWithEditorRow struct {
	ChunkRevisionRow
	EditorNickname *string `gorm:"column:editor_nickname"`
}

// ChunkRevisionRepository reads immutable Chunk revisions.
type ChunkRevisionRepository struct {
	db *gorm.DB
}

// NewChunkRevisionRepository creates a ChunkRevision repository.
func NewChunkRevisionRepository(database *gorm.DB) *ChunkRevisionRepository {
	return &ChunkRevisionRepository{db: database}
}

// ListByChunkSet returns revisions ordered by Chunk sequence and revision number.
func (r *ChunkRevisionRepository) ListByChunkSet(
	ctx context.Context,
	workspaceID, chunkSetID uuid.UUID,
) ([]*model.ChunkRevision, error) {
	var rows []ChunkRevisionRow
	if err := r.db.WithContext(ctx).Table("chunk_revisions").
		Select("chunk_revisions.*").
		Joins("JOIN chunks ON chunks.workspace_id = chunk_revisions.workspace_id AND chunks.id = chunk_revisions.chunk_id").
		Where("chunk_revisions.workspace_id = ? AND chunk_revisions.chunk_set_id = ?", workspaceID, chunkSetID).
		Order("chunks.sequence ASC, chunk_revisions.revision_no ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出 ChunkRevision 失败: %w", err)
	}
	result := make([]*model.ChunkRevision, len(rows))
	for index := range rows {
		result[index] = chunkRevisionFromRow(&rows[index])
	}
	return result, nil
}

func chunkRevisionFactsQuery(database *gorm.DB) *gorm.DB {
	return database.Table("chunk_revisions").
		Select("chunk_revisions.*, users.nickname AS editor_nickname").
		Joins("LEFT JOIN users ON users.id = chunk_revisions.editor_user_id")
}

func scanChunkRevisionFacts(ctx context.Context, query *gorm.DB) ([]*appservice.ChunkRevisionFacts, error) {
	var rows []chunkRevisionWithEditorRow
	if err := query.WithContext(ctx).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取 ChunkRevision 编辑人失败: %w", err)
	}
	result := make([]*appservice.ChunkRevisionFacts, len(rows))
	for index := range rows {
		result[index] = &appservice.ChunkRevisionFacts{
			Revision:       chunkRevisionFromRow(&rows[index].ChunkRevisionRow),
			EditorNickname: rows[index].EditorNickname,
		}
	}
	return result, nil
}
