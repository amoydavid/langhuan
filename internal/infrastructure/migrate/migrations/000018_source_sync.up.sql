-- 飞书多应用内容源连接与知识库来源字段。
CREATE TABLE IF NOT EXISTS workspace_source_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider text NOT NULL,
    name text NOT NULL,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    credentials_ciphertext bytea,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT workspace_source_connections_provider_check CHECK (provider IN ('feishu')),
    CONSTRAINT workspace_source_connections_status_check CHECK (status IN ('active', 'disabled')),
    UNIQUE (workspace_id, provider, name)
);

-- 同一 workspace + provider 下 app_id 唯一（表达式唯一约束需用独立索引）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_workspace_source_connections_app_id
    ON workspace_source_connections(workspace_id, provider, (config->>'app_id'))
    WHERE deleted_at IS NULL;

-- 知识库来源字段。
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS source_type text NOT NULL DEFAULT 'upload',
    ADD COLUMN IF NOT EXISTS source_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS source_connection_id uuid REFERENCES workspace_source_connections(id) ON DELETE SET NULL;

ALTER TABLE knowledge_bases
    DROP CONSTRAINT IF EXISTS knowledge_bases_source_type_check;
ALTER TABLE knowledge_bases
    ADD CONSTRAINT knowledge_bases_source_type_check CHECK (source_type IN ('upload', 'feishu_drive', 'feishu_wiki'));

-- 文档外部 ID（飞书节点稳定 token），用于增量幂等。
ALTER TABLE documents ADD COLUMN IF NOT EXISTS external_id text;
CREATE INDEX IF NOT EXISTS idx_documents_kb_external
    ON documents(knowledge_base_id, external_id)
    WHERE external_id IS NOT NULL;

-- 任务按连接维度统计（Meta Scheduler 限流查询）。
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS source_connection_id uuid;
CREATE INDEX IF NOT EXISTS idx_jobs_conn_active
    ON jobs(workspace_id, source_connection_id, type, status)
    WHERE source_connection_id IS NOT NULL;
