package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type indexGenerationProjectionStats struct {
	DocumentCount        int64
	ChunkCount           int64
	IndexedCount         int64
	ManualEditCount      int64
	DisabledChunkCount   int64
	MissingChunkSetCount int64
	MissingRevisionCount int64
}

// indexGenerationProjectionStatsQueryPG 是 PG 专属投影统计：使用 LATERAL 取每个文档
// 的最佳 chunk_set，用 COUNT(*) FILTER 做条件聚合。占位符顺序见 loadIndexGenerationProjectionStats。
const indexGenerationProjectionStatsQueryPG = `
		WITH target_generation AS (
			SELECT id, workspace_id, knowledge_base_id, chunker_version, chunking_config
			FROM knowledge_base_index_generations
			WHERE workspace_id = ? AND knowledge_base_id = ? AND id = ?
		), active_documents AS (
			SELECT document.id, document.workspace_id, document.knowledge_base_id,
				document.kind, document.active_revision_id
			FROM documents AS document
			WHERE document.workspace_id = ? AND document.knowledge_base_id = ?
				AND document.active_revision_id IS NOT NULL AND document.deleted_at IS NULL
		), effective_sets AS (
			SELECT document.id AS document_id, effective_set.id AS chunk_set_id
			FROM active_documents AS document
			CROSS JOIN target_generation AS generation
			LEFT JOIN LATERAL (
				SELECT chunk_set.id
				FROM document_chunk_sets AS chunk_set
				WHERE chunk_set.workspace_id = document.workspace_id
					AND chunk_set.knowledge_base_id = document.knowledge_base_id
					AND chunk_set.document_id = document.id
					AND chunk_set.document_revision_id = document.active_revision_id
					AND chunk_set.status = 'ready'
					AND (
						(document.kind = 'faq' AND chunk_set.strategy = 'faq')
						OR (
							document.kind IN ('file', 'web')
							AND chunk_set.strategy = 'standard'
							AND chunk_set.chunker_version = generation.chunker_version
							AND chunk_set.chunking_config = generation.chunking_config
						)
					)
				ORDER BY
					CASE WHEN document.kind = 'faq' THEN chunk_set.chunker_version ELSE 0 END DESC,
					chunk_set.created_at DESC, chunk_set.id DESC
				LIMIT 1
			) AS effective_set ON true
		), chunk_stats AS (
			SELECT
				COUNT(chunk.id) AS chunk_count,
				COUNT(*) FILTER (WHERE revision.edit_source = 'user') AS manual_edit_count,
				COUNT(*) FILTER (WHERE revision.enabled = false) AS disabled_chunk_count,
				COUNT(*) FILTER (WHERE chunk.id IS NOT NULL AND revision.id IS NULL) AS missing_revision_count
			FROM effective_sets AS effective_set
			LEFT JOIN chunks AS chunk
				ON chunk.workspace_id = ? AND chunk.chunk_set_id = effective_set.chunk_set_id
			LEFT JOIN chunk_revisions AS revision
				ON revision.workspace_id = chunk.workspace_id
				AND revision.chunk_id = chunk.id AND revision.id = chunk.active_revision_id
		), entry_stats AS (
			SELECT COUNT(*) AS indexed_count
			FROM retrieval_entries
			WHERE workspace_id = ? AND knowledge_base_id = ?
				AND index_generation_id = ? AND state = 'published'
		)
		SELECT
			(SELECT COUNT(*) FROM active_documents) AS document_count,
			COALESCE(chunk_stats.chunk_count, 0) AS chunk_count,
			COALESCE(entry_stats.indexed_count, 0) AS indexed_count,
			COALESCE(chunk_stats.manual_edit_count, 0) AS manual_edit_count,
			COALESCE(chunk_stats.disabled_chunk_count, 0) AS disabled_chunk_count,
			(SELECT COUNT(*) FROM effective_sets WHERE chunk_set_id IS NULL) AS missing_chunk_set_count,
			COALESCE(chunk_stats.missing_revision_count, 0) AS missing_revision_count
		FROM chunk_stats CROSS JOIN entry_stats`

// indexGenerationProjectionStatsQuerySQLite 是等价的 SQLite 投影统计：
//   - LATERAL 改写为 SELECT 内的标量相关子查询（SQLite 不支持 LATERAL），仍取每个文档
//     按 chunker_version/created_at/id 排序的最佳 chunk_set，缺失返回 NULL。
//   - COUNT(*) FILTER 改写为 SUM(CASE WHEN ...)。
//   - chunking_config 按 JSON 文本相等比较（Go json.Marshal 产生确定性文本）。
//
// 占位符顺序与 PG 版本完全一致。
const indexGenerationProjectionStatsQuerySQLite = `
		WITH target_generation AS (
			SELECT id, workspace_id, knowledge_base_id, chunker_version, chunking_config
			FROM knowledge_base_index_generations
			WHERE workspace_id = ? AND knowledge_base_id = ? AND id = ?
		), active_documents AS (
			SELECT document.id, document.workspace_id, document.knowledge_base_id,
				document.kind, document.active_revision_id
			FROM documents AS document
			WHERE document.workspace_id = ? AND document.knowledge_base_id = ?
				AND document.active_revision_id IS NOT NULL AND document.deleted_at IS NULL
		), effective_sets AS (
			SELECT document.id AS document_id, (
				SELECT chunk_set.id
				FROM document_chunk_sets AS chunk_set
				WHERE chunk_set.workspace_id = document.workspace_id
					AND chunk_set.knowledge_base_id = document.knowledge_base_id
					AND chunk_set.document_id = document.id
					AND chunk_set.document_revision_id = document.active_revision_id
					AND chunk_set.status = 'ready'
					AND (
						(document.kind = 'faq' AND chunk_set.strategy = 'faq')
						OR (
							document.kind IN ('file', 'web')
							AND chunk_set.strategy = 'standard'
							AND chunk_set.chunker_version = generation.chunker_version
							AND chunk_set.chunking_config = generation.chunking_config
						)
					)
				ORDER BY
					CASE WHEN document.kind = 'faq' THEN chunk_set.chunker_version ELSE 0 END DESC,
					chunk_set.created_at DESC, chunk_set.id DESC
				LIMIT 1
			) AS chunk_set_id
			FROM active_documents AS document
			CROSS JOIN target_generation AS generation
		), chunk_stats AS (
			SELECT
				COUNT(chunk.id) AS chunk_count,
				SUM(CASE WHEN revision.edit_source = 'user' THEN 1 ELSE 0 END) AS manual_edit_count,
				SUM(CASE WHEN revision.enabled = false THEN 1 ELSE 0 END) AS disabled_chunk_count,
				SUM(CASE WHEN chunk.id IS NOT NULL AND revision.id IS NULL THEN 1 ELSE 0 END) AS missing_revision_count
			FROM effective_sets AS effective_set
			LEFT JOIN chunks AS chunk
				ON chunk.workspace_id = ? AND chunk.chunk_set_id = effective_set.chunk_set_id
			LEFT JOIN chunk_revisions AS revision
				ON revision.workspace_id = chunk.workspace_id
				AND revision.chunk_id = chunk.id AND revision.id = chunk.active_revision_id
		), entry_stats AS (
			SELECT COUNT(*) AS indexed_count
			FROM retrieval_entries
			WHERE workspace_id = ? AND knowledge_base_id = ?
				AND index_generation_id = ? AND state = 'published'
		)
		SELECT
			(SELECT COUNT(*) FROM active_documents) AS document_count,
			COALESCE(chunk_stats.chunk_count, 0) AS chunk_count,
			COALESCE(entry_stats.indexed_count, 0) AS indexed_count,
			COALESCE(chunk_stats.manual_edit_count, 0) AS manual_edit_count,
			COALESCE(chunk_stats.disabled_chunk_count, 0) AS disabled_chunk_count,
			(SELECT COUNT(*) FROM effective_sets WHERE chunk_set_id IS NULL) AS missing_chunk_set_count,
			COALESCE(chunk_stats.missing_revision_count, 0) AS missing_revision_count
		FROM chunk_stats CROSS JOIN entry_stats`

func loadIndexGenerationProjectionStats(
	ctx context.Context,
	tx *gorm.DB,
	workspaceID, knowledgeBaseID, generationID uuid.UUID,
) (indexGenerationProjectionStats, error) {
	var stats indexGenerationProjectionStats
	query := indexGenerationProjectionStatsQueryPG
	if tx.Dialector.Name() == "sqlite" {
		query = indexGenerationProjectionStatsQuerySQLite
	}
	err := tx.WithContext(ctx).Raw(query,
		workspaceID, knowledgeBaseID, generationID,
		workspaceID, knowledgeBaseID,
		workspaceID,
		workspaceID, knowledgeBaseID, generationID,
	).Scan(&stats).Error
	if err != nil {
		return indexGenerationProjectionStats{}, fmt.Errorf("统计 active Generation 投影失败: %w", err)
	}
	if stats.MissingChunkSetCount != 0 || stats.MissingRevisionCount != 0 ||
		stats.IndexedCount > stats.ChunkCount {
		return indexGenerationProjectionStats{}, fmt.Errorf(
			"active Generation 投影不完整: missing_chunk_sets=%d missing_revisions=%d indexed=%d chunks=%d",
			stats.MissingChunkSetCount, stats.MissingRevisionCount, stats.IndexedCount, stats.ChunkCount,
		)
	}
	return stats, nil
}
