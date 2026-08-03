ALTER TABLE knowledge_base_index_generations
    DROP CONSTRAINT IF EXISTS index_generations_count_check;

ALTER TABLE knowledge_base_index_generations
    ADD COLUMN failed_count bigint NOT NULL DEFAULT 0;

ALTER TABLE knowledge_base_index_generations
    ADD CONSTRAINT index_generations_count_check CHECK (
        document_count >= 0 AND chunk_count >= 0 AND indexed_count >= 0
        AND failed_count >= 0 AND manual_edit_count >= 0 AND disabled_chunk_count >= 0
        AND indexed_count <= chunk_count AND failed_count <= chunk_count
    );
