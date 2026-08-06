DROP INDEX IF EXISTS uq_workspace_source_connections_app_id;

ALTER TABLE jobs
    DROP INDEX IF EXISTS idx_jobs_conn_active,
    DROP COLUMN IF EXISTS source_connection_id;

DROP INDEX IF EXISTS idx_documents_kb_external;
ALTER TABLE documents DROP COLUMN IF EXISTS external_id;

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS knowledge_bases_source_type_check,
    DROP COLUMN IF EXISTS source_connection_id,
    DROP COLUMN IF EXISTS source_config,
    DROP COLUMN IF EXISTS source_type;

DROP TABLE IF EXISTS workspace_source_connections CASCADE;
