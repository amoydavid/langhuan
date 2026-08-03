-- Active Generation statistics are derived from the currently published
-- Document/Chunk/ChunkRevision/RetrievalEntry projection. Earlier incremental
-- document publication advanced indexed_content_version without refreshing
-- these counters, so repair every active Generation from authoritative facts.
-- RetrievalEntry stats are derived here only for indexed_count; chunk_count
-- includes disabled Chunks, which intentionally have no published entry.
WITH active_generations AS (
    SELECT
        generation.id AS generation_id,
        generation.workspace_id,
        generation.knowledge_base_id,
        generation.chunker_version,
        generation.chunking_config
    FROM knowledge_bases AS knowledge_base
    JOIN knowledge_base_index_generations AS generation
      ON generation.workspace_id = knowledge_base.workspace_id
     AND generation.knowledge_base_id = knowledge_base.id
     AND generation.id = knowledge_base.active_index_generation_id
    WHERE knowledge_base.deleted_at IS NULL
), active_documents AS (
    SELECT
        generation.generation_id,
        generation.workspace_id,
        generation.knowledge_base_id,
        generation.chunker_version,
        generation.chunking_config,
        document.id AS document_id,
        document.kind,
        document.active_revision_id
    FROM active_generations AS generation
    JOIN documents AS document
      ON document.workspace_id = generation.workspace_id
     AND document.knowledge_base_id = generation.knowledge_base_id
     AND document.active_revision_id IS NOT NULL
     AND document.deleted_at IS NULL
), effective_chunks AS (
    SELECT
        generation.generation_id,
        generation.workspace_id,
        document.document_id,
        chunk.id AS chunk_id,
        revision.edit_source,
        revision.enabled
    FROM active_generations AS generation
    JOIN active_documents AS document
      ON document.workspace_id = generation.workspace_id
     AND document.generation_id = generation.generation_id
    JOIN LATERAL (
        SELECT chunk_set.id
        FROM document_chunk_sets AS chunk_set
        WHERE chunk_set.workspace_id = document.workspace_id
          AND chunk_set.knowledge_base_id = document.knowledge_base_id
          AND chunk_set.document_id = document.document_id
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
            chunk_set.created_at DESC,
            chunk_set.id DESC
        LIMIT 1
    ) AS effective_set ON true
    JOIN chunks AS chunk
      ON chunk.workspace_id = generation.workspace_id
     AND chunk.chunk_set_id = effective_set.id
    JOIN chunk_revisions AS revision
      ON revision.workspace_id = chunk.workspace_id
     AND revision.chunk_id = chunk.id
     AND revision.id = chunk.active_revision_id
), document_stats AS (
    SELECT
        generation.generation_id,
        COUNT(document.document_id) AS document_count
    FROM active_generations AS generation
    LEFT JOIN active_documents AS document
      ON document.workspace_id = generation.workspace_id
     AND document.generation_id = generation.generation_id
    GROUP BY generation.generation_id
), chunk_stats AS (
    SELECT
        generation.generation_id,
        COUNT(chunk.chunk_id) AS chunk_count,
        COUNT(*) FILTER (WHERE chunk.edit_source = 'user') AS manual_edit_count,
        COUNT(*) FILTER (WHERE chunk.enabled = false) AS disabled_chunk_count
    FROM active_generations AS generation
    LEFT JOIN effective_chunks AS chunk
      ON chunk.workspace_id = generation.workspace_id
     AND chunk.generation_id = generation.generation_id
    GROUP BY generation.generation_id
), entry_stats AS (
    SELECT
        generation.generation_id,
        COUNT(entry.id) AS indexed_count
    FROM active_generations AS generation
    LEFT JOIN retrieval_entries AS entry
      ON entry.workspace_id = generation.workspace_id
     AND entry.knowledge_base_id = generation.knowledge_base_id
     AND entry.index_generation_id = generation.generation_id
     AND entry.state = 'published'
    GROUP BY generation.generation_id
), generation_stats AS (
    SELECT
        generation.generation_id,
        document.document_count,
        chunk.chunk_count,
        entry.indexed_count,
        chunk.manual_edit_count,
        chunk.disabled_chunk_count
    FROM active_generations AS generation
    JOIN document_stats AS document USING (generation_id)
    JOIN chunk_stats AS chunk USING (generation_id)
    JOIN entry_stats AS entry USING (generation_id)
)
UPDATE knowledge_base_index_generations AS generation
SET
    document_count = stats.document_count,
    chunk_count = stats.chunk_count,
    indexed_count = stats.indexed_count,
    manual_edit_count = stats.manual_edit_count,
    disabled_chunk_count = stats.disabled_chunk_count
FROM generation_stats AS stats
WHERE generation.id = stats.generation_id;
