-- 000021_document_ingest_idempotency: 文档导入幂等表。
-- 背景：jinshu 通过 Bearer API Key 把工单沉淀为 langhuan 文档时，网络重试
-- 不得产生重复文档。调用方在 POST /documents/text 上携带
-- Idempotency-Key 头；服务端在写入文档血缘的同一 Workspace 事务内追加一行
-- 幂等记录。同一 (workspace, api_key, knowledge_base, key) 再次到达：
--   - 请求体哈希相同 -> 返回原 document/job，deduped=true；
--   - 请求体哈希不同 -> 409 idempotency_conflict。
-- Session 主体不参与幂等合同，仅 Bearer API Key 生效。
CREATE TABLE document_ingest_idempotencies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    api_key_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    key text NOT NULL,
    request_sha256 text NOT NULL,
    document_id uuid NOT NULL,
    job_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT document_ingest_idempotencies_workspace_fk
        FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
    CONSTRAINT document_ingest_idempotencies_api_key_fk
        FOREIGN KEY (workspace_id, api_key_id)
        REFERENCES workspace_api_tokens(workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT document_ingest_idempotencies_knowledge_base_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases(workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT document_ingest_idempotencies_document_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id)
        REFERENCES documents(workspace_id, knowledge_base_id, id) ON DELETE CASCADE,
    CONSTRAINT document_ingest_idempotencies_job_fk
        FOREIGN KEY (workspace_id, job_id)
        REFERENCES jobs(workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT document_ingest_idempotencies_key_nonempty
        CHECK (btrim(key) <> ''),
    CONSTRAINT document_ingest_idempotencies_key_length
        CHECK (char_length(key) BETWEEN 1 AND 128),
    CONSTRAINT document_ingest_idempotencies_key_no_newlines
        CHECK (key NOT LIKE '%' || chr(10) || '%' AND key NOT LIKE '%' || chr(13) || '%'),
    CONSTRAINT document_ingest_idempotencies_request_sha256_check
        CHECK (request_sha256 ~ '^[0-9a-f]{64}$')
);

-- 幂等自然键：同一 Workspace/API Key/KB 下，一个 key 只能绑定一次结果。
CREATE UNIQUE INDEX document_ingest_idempotencies_natural_key_idx
    ON document_ingest_idempotencies(workspace_id, api_key_id, knowledge_base_id, key);

CREATE INDEX document_ingest_idempotencies_document_idx
    ON document_ingest_idempotencies(document_id);

CREATE INDEX document_ingest_idempotencies_job_idx
    ON document_ingest_idempotencies(job_id);
