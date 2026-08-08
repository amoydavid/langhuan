-- 000022_feishu_sync_robustness: 飞书同步稳健性 schema。
-- 1) documents.content_hash：归一化 markdown 正文稳定哈希，用于同步增量去重。
-- 2) file_tree_nodes.external_id：飞书节点稳定 token，用于增量幂等与删除检测。
-- 3) 把 documents 上的旧非唯一索引替换为 (workspace_id, knowledge_base_id, external_id)
--    唯一部分索引；并为 file_tree_nodes 增加同样的唯一部分索引。
-- 4) jobs_target_check 放宽为四选一：新增 type='source_cleanup' 的仅关联知识库分支
--    （保留 source_sync 分支）。
-- 迁移前先检测历史重复 external_id，存在则直接失败，绝不静默合并或删除数据。

-- (1) 检测 documents.external_id 重复（按 workspace + knowledge_base + external_id 分组）。
DO $$
DECLARE
    dup_row RECORD;
BEGIN
    FOR dup_row IN
        SELECT workspace_id, knowledge_base_id, external_id, count(*) AS dup_count
        FROM documents
        WHERE external_id IS NOT NULL AND external_id <> ''
        GROUP BY workspace_id, knowledge_base_id, external_id
        HAVING count(*) > 1
    LOOP
        RAISE EXCEPTION 'duplicate documents.external_id: workspace=%, knowledge_base=%, external_id=%, count=%',
            dup_row.workspace_id, dup_row.knowledge_base_id, dup_row.external_id, dup_row.dup_count;
    END LOOP;
END $$;

-- (2) documents.content_hash。
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS content_hash text;

-- (3) file_tree_nodes.external_id。
ALTER TABLE file_tree_nodes
    ADD COLUMN IF NOT EXISTS external_id text;

-- (4) 检测 file_tree_nodes.external_id 重复（按 workspace + knowledge_base + external_id 分组，
--     只看非空 external_id）。root 节点的 external_id 应为空，故天然不在候选中。
DO $$
DECLARE
    dup_row RECORD;
BEGIN
    FOR dup_row IN
        SELECT workspace_id, knowledge_base_id, external_id, count(*) AS dup_count
        FROM file_tree_nodes
        WHERE external_id IS NOT NULL AND external_id <> ''
        GROUP BY workspace_id, knowledge_base_id, external_id
        HAVING count(*) > 1
    LOOP
        RAISE EXCEPTION 'duplicate file_tree_nodes.external_id: workspace=%, knowledge_base=%, external_id=%, count=%',
            dup_row.workspace_id, dup_row.knowledge_base_id, dup_row.external_id, dup_row.dup_count;
    END LOOP;
END $$;

-- (5) 替换 documents 上旧的 idx_documents_kb_external（仅按 knowledge_base_id+external_id）
--     为 (workspace_id, knowledge_base_id, external_id) 的唯一部分索引。
DROP INDEX IF EXISTS idx_documents_kb_external;
CREATE UNIQUE INDEX IF NOT EXISTS uq_documents_workspace_kb_external
    ON documents (workspace_id, knowledge_base_id, external_id)
    WHERE external_id IS NOT NULL AND external_id <> '';

-- (6) file_tree_nodes 上新增 (workspace_id, knowledge_base_id, external_id) 唯一部分索引。
CREATE UNIQUE INDEX IF NOT EXISTS uq_file_tree_nodes_kb_external
    ON file_tree_nodes (workspace_id, knowledge_base_id, external_id)
    WHERE external_id IS NOT NULL AND external_id <> '';

-- (7) 放宽 jobs_target_check：新增 source_cleanup 仅关联知识库的分支（保留 source_sync）。
ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_target_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_sync')
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_cleanup')
    );
