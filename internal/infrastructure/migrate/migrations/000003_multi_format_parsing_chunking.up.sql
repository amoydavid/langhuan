ALTER TABLE IF EXISTS documents
    ADD COLUMN IF NOT EXISTS processing_version integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS parse_manifest jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE IF EXISTS chunks
    ADD COLUMN IF NOT EXISTS embedding_content text NOT NULL DEFAULT '';
