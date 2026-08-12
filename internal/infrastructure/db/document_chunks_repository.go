package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentChunksRepository reads the authoritative effective ChunkSet for a Document.
type DocumentChunksRepository struct {
	db *gorm.DB
}

// NewDocumentChunksRepository creates a Document Chunk query repository.
func NewDocumentChunksRepository(database *gorm.DB) *DocumentChunksRepository {
	return &DocumentChunksRepository{db: database}
}

type documentChunkLineageRow struct {
	GenerationID       uuid.UUID `gorm:"column:generation_id"`
	DocumentRevisionID uuid.UUID `gorm:"column:document_revision_id"`
	DocumentKind       string    `gorm:"column:document_kind"`
	ChunkerVersion     int       `gorm:"column:chunker_version"`
	ChunkingConfig     JSONMap   `gorm:"column:chunking_config"`
}

// ListDocumentChunkFacts reads current facts and never derives Chunk authority from retrieval_entries.
func (r *DocumentChunksRepository) ListDocumentChunkFacts(
	ctx context.Context,
	workspaceID, knowledgeBaseID, documentID uuid.UUID,
	filter appservice.DocumentChunkFactsFilter,
) (*appservice.DocumentChunkFactsPage, error) {
	if workspaceID == uuid.Nil || knowledgeBaseID == uuid.Nil || documentID == uuid.Nil || filter.Limit < 1 ||
		(filter.AfterSequence == nil) != (filter.AfterID == nil) ||
		(filter.AfterRoleRank == nil) != (filter.AfterID == nil) {
		return nil, fmt.Errorf("%w: Document Chunk repository filter 无效", domainerrors.ErrValidation)
	}
	var page *appservice.DocumentChunkFactsPage
	err := NewWorkspaceTxRunner(r.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		lineage, err := loadDocumentChunkLineage(ctx, tx, workspaceID, knowledgeBaseID, documentID)
		if err != nil {
			return err
		}
		chunkSet, err := loadEffectiveDocumentChunkSet(ctx, tx, workspaceID, knowledgeBaseID, documentID, lineage)
		if err != nil {
			return err
		}
		items, err := loadDocumentChunkItems(ctx, tx, workspaceID, knowledgeBaseID, chunkSet.ID, filter)
		if err != nil {
			return err
		}
		page = &appservice.DocumentChunkFactsPage{
			GenerationID: lineage.GenerationID, DocumentRevisionID: lineage.DocumentRevisionID,
			ChunkSetID: chunkSet.ID, Items: items,
		}
		return nil
	})
	return page, err
}

func loadDocumentChunkLineage(
	ctx context.Context,
	tx *gorm.DB,
	workspaceID, knowledgeBaseID, documentID uuid.UUID,
) (*documentChunkLineageRow, error) {
	var row documentChunkLineageRow
	err := tx.WithContext(ctx).Table("knowledge_bases").
		Select(`knowledge_base_index_generations.id AS generation_id,
			documents.active_revision_id AS document_revision_id,
			documents.kind AS document_kind,
			knowledge_base_index_generations.chunker_version,
			knowledge_base_index_generations.chunking_config`).
		Joins(`JOIN documents ON documents.workspace_id = knowledge_bases.workspace_id
			AND documents.knowledge_base_id = knowledge_bases.id
			AND documents.id = ? AND documents.deleted_at IS NULL
			AND documents.active_revision_id IS NOT NULL`, documentID).
		Joins(`JOIN knowledge_base_index_generations
			ON knowledge_base_index_generations.workspace_id = knowledge_bases.workspace_id
			AND knowledge_base_index_generations.knowledge_base_id = knowledge_bases.id
			AND knowledge_base_index_generations.id = knowledge_bases.active_index_generation_id`).
		Where("knowledge_bases.workspace_id = ? AND knowledge_bases.id = ? AND knowledge_bases.deleted_at IS NULL", workspaceID, knowledgeBaseID).
		Take(&row).Error
	if err != nil {
		return nil, translateDBError(err, "读取 Document Chunk lineage 失败")
	}
	if row.GenerationID == uuid.Nil || row.DocumentRevisionID == uuid.Nil {
		return nil, domainerrors.ErrNotFound
	}
	return &row, nil
}

func loadEffectiveDocumentChunkSet(
	ctx context.Context,
	tx *gorm.DB,
	workspaceID, knowledgeBaseID, documentID uuid.UUID,
	lineage *documentChunkLineageRow,
) (*DocumentChunkSetRow, error) {
	query := tx.WithContext(ctx).Where(
		"workspace_id = ? AND knowledge_base_id = ? AND document_id = ? AND document_revision_id = ? AND status = ?",
		workspaceID, knowledgeBaseID, documentID, lineage.DocumentRevisionID, value.ChunkSetReady,
	)
	switch value.DocumentKind(lineage.DocumentKind) {
	case value.DocumentKindFAQ:
		query = query.Where("strategy = ?", value.ChunkStrategyFAQ).
			Order("chunker_version DESC, created_at DESC, id DESC")
	case value.DocumentKindFile, value.DocumentKindWeb:
		encoded, err := json.Marshal(lineage.ChunkingConfig)
		if err != nil {
			return nil, fmt.Errorf("编码 active Generation ChunkingConfig 失败: %w", err)
		}
		configCompare := "chunking_config = CAST(? AS jsonb)"
		if tx.Dialector.Name() == "sqlite" {
			// SQLite 把 chunking_config 当 JSON 文本存储，Go json.Marshal 产生确定性
			//（键排序、紧凑）文本，可直接做相等比较。
			configCompare = "chunking_config = ?"
		}
		query = query.Where(
			"strategy = ? AND chunker_version = ? AND "+configCompare,
			value.ChunkStrategyStandard, lineage.ChunkerVersion, string(encoded),
		).Order("created_at DESC, id DESC")
	default:
		return nil, domainerrors.ErrNotFound
	}
	var row DocumentChunkSetRow
	if err := query.First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取有效 Document ChunkSet 失败")
	}
	return &row, nil
}

func loadDocumentChunkItems(
	ctx context.Context,
	tx *gorm.DB,
	workspaceID, knowledgeBaseID, chunkSetID uuid.UUID,
	filter appservice.DocumentChunkFactsFilter,
) ([]appservice.DocumentChunkFacts, error) {
	query := tx.WithContext(ctx).Table("chunks").Select("chunks.*").
		Joins(`JOIN chunk_revisions active_revisions
			ON active_revisions.workspace_id = chunks.workspace_id
			AND active_revisions.knowledge_base_id = chunks.knowledge_base_id
			AND active_revisions.chunk_id = chunks.id
			AND active_revisions.id = chunks.active_revision_id`).
		Where("chunks.workspace_id = ? AND chunks.knowledge_base_id = ? AND chunks.chunk_set_id = ?", workspaceID, knowledgeBaseID, chunkSetID)
	if filter.Enabled != nil {
		query = query.Where("active_revisions.enabled = ?", *filter.Enabled)
	}
	roleRank := "CASE chunks.role WHEN 'parent' THEN 0 WHEN 'child' THEN 1 ELSE 2 END"
	if filter.AfterRoleRank != nil && filter.AfterSequence != nil && filter.AfterID != nil {
		query = query.Where(
			"("+roleRank+" > ? OR ("+roleRank+" = ? AND (chunks.sequence > ? OR (chunks.sequence = ? AND chunks.id > ?))))",
			*filter.AfterRoleRank, *filter.AfterRoleRank, *filter.AfterSequence, *filter.AfterSequence, *filter.AfterID,
		)
	}
	var chunkRows []ChunkRow
	if err := query.Order(roleRank + " ASC, chunks.sequence ASC, chunks.id ASC").Limit(filter.Limit).Scan(&chunkRows).Error; err != nil {
		return nil, fmt.Errorf("列出 Document Chunks 失败: %w", err)
	}
	if len(chunkRows) == 0 {
		return []appservice.DocumentChunkFacts{}, nil
	}
	revisionIDs := make([]uuid.UUID, 0, len(chunkRows))
	for index := range chunkRows {
		if chunkRows[index].ActiveRevisionID == nil {
			return nil, fmt.Errorf("%w: Chunk 缺少 active Revision", domainerrors.ErrConflict)
		}
		revisionIDs = append(revisionIDs, *chunkRows[index].ActiveRevisionID)
	}
	revisionFacts, err := scanChunkRevisionFacts(ctx, chunkRevisionFactsQuery(tx).
		Where("chunk_revisions.workspace_id = ? AND chunk_revisions.id IN ?", workspaceID, revisionIDs))
	if err != nil {
		return nil, err
	}
	revisionByID := make(map[uuid.UUID]*appservice.ChunkRevisionFacts, len(revisionFacts))
	for _, facts := range revisionFacts {
		revisionByID[facts.Revision.ID] = facts
	}
	items := make([]appservice.DocumentChunkFacts, 0, len(chunkRows))
	for index := range chunkRows {
		chunk, err := chunkV2FromRow(&chunkRows[index])
		if err != nil {
			return nil, err
		}
		facts := revisionByID[*chunkRows[index].ActiveRevisionID]
		if facts == nil {
			return nil, fmt.Errorf("%w: active ChunkRevision lineage 不完整", domainerrors.ErrConflict)
		}
		items = append(items, appservice.DocumentChunkFacts{Chunk: chunk, ActiveRevision: *facts})
	}
	return items, nil
}
