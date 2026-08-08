-- 回滚 000022：恢复 jobs_target_check 到 000017（三选一），
-- 恢复 documents 上旧的 idx_documents_kb_external（非唯一），删除新增的列。
-- 不触碰 source_config JSONB 键（由各自功能负责）。

-- 恢复 jobs_target_check：去掉 source_cleanup 分支。
ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_target_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_sync')
    );

-- 删除新增的唯一部分索引。
DROP INDEX IF EXISTS uq_file_tree_nodes_kb_external;
DROP INDEX IF EXISTS uq_documents_workspace_kb_external;

-- 恢复 documents 上旧的（非唯一）idx_documents_kb_external，匹配 000018 定义。
CREATE INDEX IF NOT EXISTS idx_documents_kb_external
    ON documents(knowledge_base_id, external_id)
    WHERE external_id IS NOT NULL;

-- 删除新增列。
ALTER TABLE file_tree_nodes DROP COLUMN IF EXISTS external_id;
ALTER TABLE documents DROP COLUMN IF EXISTS content_hash;
