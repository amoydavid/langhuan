CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workspace_api_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS knowledge_bases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    embedding_dimension integer NOT NULL CHECK (embedding_dimension > 0),
    chunking_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_base_id uuid NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
    title text NOT NULL,
    file_type text NOT NULL,
    source_type text NOT NULL,
    status text NOT NULL,
    sha256 text NOT NULL DEFAULT '',
    raw_storage_key text NOT NULL DEFAULT '',
    size_bytes bigint NOT NULL DEFAULT 0,
    content_type text NOT NULL DEFAULT '',
    normalized_markdown text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chunks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    sequence integer NOT NULL,
    content text NOT NULL,
    context_header text NOT NULL DEFAULT '',
    source_anchor jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, sequence)
);

CREATE TABLE IF NOT EXISTS document_assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    original_ref text NOT NULL,
    storage_key text NOT NULL,
    public_url text NOT NULL,
    mime_type text NOT NULL,
    sha256 text NOT NULL DEFAULT '',
    size_bytes bigint NOT NULL DEFAULT 0,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid REFERENCES documents(id) ON DELETE CASCADE,
    type text NOT NULL,
    status text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    external_job_id text NOT NULL DEFAULT '',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chunk_embeddings (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    embedding halfvec NOT NULL,
    dimension integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chunk_keywords (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    content text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE IF EXISTS knowledge_bases
    ADD COLUMN IF NOT EXISTS workspace_id uuid;

ALTER TABLE IF EXISTS documents
    ADD COLUMN IF NOT EXISTS raw_storage_key text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS size_bytes bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS content_type text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_workspaces_name ON workspaces(name);
CREATE INDEX IF NOT EXISTS idx_workspace_api_tokens_workspace_id ON workspace_api_tokens(workspace_id);
CREATE INDEX IF NOT EXISTS idx_documents_knowledge_base_id ON documents(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_workspace_id ON knowledge_bases(workspace_id);
CREATE INDEX IF NOT EXISTS idx_chunks_document_id ON chunks(document_id);
CREATE INDEX IF NOT EXISTS idx_document_assets_document_id ON document_assets(document_id);
CREATE INDEX IF NOT EXISTS idx_jobs_document_id ON jobs(document_id);

-- chunk_embeddings 向量检索：同表混存多维度向量，按 dimension 建部分索引。
-- 列类型用 halfvec（pgvector，每元素 2 字节，上限 4000 维），以便覆盖 2048/3584
-- 这类超过 vector 类型 2000 维上限的模型。
-- 每个维度一个 HNSW 部分索引，WHERE dimension = N 让索引只覆盖对应维度的行。
-- 查询侧必须用与索引完全一致的表达式 embedding::halfvec(N) + WHERE dimension = N，
-- 否则规划器无法命中索引、退化为全表顺序扫描。
-- 维度不在下列列表内时向量仍可写入，但检索不走 HNSW（顺序扫描）；新增主流维度时
-- 应补一个迁移增加对应的部分索引。
CREATE INDEX IF NOT EXISTS idx_chunk_embeddings_hnsw_798 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(798)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE (dimension = 798);

CREATE INDEX IF NOT EXISTS idx_chunk_embeddings_hnsw_1024 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(1024)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE (dimension = 1024);

CREATE INDEX IF NOT EXISTS idx_chunk_embeddings_hnsw_2048 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(2048)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE (dimension = 2048);

CREATE INDEX IF NOT EXISTS idx_chunk_embeddings_hnsw_3584 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(3584)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE (dimension = 3584);
