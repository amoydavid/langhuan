ALTER TABLE knowledge_bases
    ADD COLUMN embedding_dimension integer;

UPDATE knowledge_bases AS kb
SET embedding_dimension = m.dimensions
FROM models AS m
WHERE kb.embedding_model_id = m.id;

ALTER TABLE knowledge_bases
    ALTER COLUMN embedding_dimension SET NOT NULL,
    ALTER COLUMN embedding_dimension SET DEFAULT 1024;

ALTER TABLE knowledge_bases
    ADD CONSTRAINT knowledge_bases_embedding_dimension_check
    CHECK (embedding_dimension > 0);

DROP INDEX IF EXISTS idx_knowledge_bases_embedding_model_id;

ALTER TABLE knowledge_bases
    DROP COLUMN embedding_model_id;

DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS model_providers;
