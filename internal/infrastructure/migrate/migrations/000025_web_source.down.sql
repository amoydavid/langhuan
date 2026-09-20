-- 000025_web_source.down.sql
-- 逆序回滚：jobs 源级目标 -> documents 溯源列 -> kb_sources 表 -> provider CHECK。
-- 不触碰 knowledge_bases.source_config 等运行态 jsonb 键（由各自功能负责）。

-- (4) 回滚 jobs 源级目标：先删触发器与函数，再还原 jobs_target_check 到
-- 000022 的四选一，最后删列与索引。
DROP TRIGGER IF EXISTS jobs_source_target_on_update ON jobs;
DROP TRIGGER IF EXISTS jobs_source_target_on_insert ON jobs;
DROP FUNCTION IF EXISTS enforce_jobs_source_target();

ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_target_check;
ALTER TABLE jobs
    ADD CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_sync')
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_cleanup')
    );

DROP INDEX IF EXISTS idx_jobs_source_active;
ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_source_fk,
    DROP COLUMN IF EXISTS kb_source_id;

-- (2) 回滚 documents 溯源列。
DROP INDEX IF EXISTS idx_documents_kb_source;
ALTER TABLE documents
    DROP CONSTRAINT IF EXISTS documents_source_meta_object_check,
    DROP CONSTRAINT IF EXISTS documents_kb_source_fk,
    DROP COLUMN IF EXISTS source_meta,
    DROP COLUMN IF EXISTS kb_source_id;

-- (1) 回滚 kb_sources 表（依赖它的外键已在上面随列删除）。
DROP INDEX IF EXISTS uq_kb_sources_active_name;
DROP INDEX IF EXISTS idx_kb_sources_workspace_kb;
DROP TABLE IF EXISTS kb_sources CASCADE;

-- (3) 还原 provider CHECK 到仅 feishu。
ALTER TABLE workspace_source_connections
    DROP CONSTRAINT IF EXISTS workspace_source_connections_provider_check;
ALTER TABLE workspace_source_connections
    ADD CONSTRAINT workspace_source_connections_provider_check
        CHECK (provider IN ('feishu'));
