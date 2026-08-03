ALTER TABLE IF EXISTS chunks
    DROP COLUMN IF EXISTS embedding_content;

ALTER TABLE IF EXISTS documents
    DROP COLUMN IF EXISTS parse_manifest,
    DROP COLUMN IF EXISTS processing_version;
