-- 000023_search_runs.up.sql
-- 检索证据血缘与可回放检索：持久化检索运行快照（SearchRun）和 Generation 快照。
-- 严禁保存原始 query、正文、向量、API Key secret 或完整第三方响应。

CREATE TABLE IF NOT EXISTS search_runs (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    requested_scope text NOT NULL CHECK (requested_scope IN ('selected', 'api_key_bound_all')),
    query_hash text NOT NULL,
    query_chars integer NOT NULL CHECK (query_chars >= 0),
    vector_top_k integer NOT NULL CHECK (vector_top_k > 0),
    keyword_top_k integer NOT NULL CHECK (keyword_top_k > 0),
    final_top_k integer NOT NULL CHECK (final_top_k > 0),
    retrieval_status text NOT NULL CHECK (retrieval_status IN ('running', 'available', 'empty', 'degraded', 'failed')),
    failure_class text NOT NULL DEFAULT '',
    ranking_stage text NOT NULL DEFAULT '',
    result_count integer NOT NULL DEFAULT 0 CHECK (result_count >= 0),
    request_id text NOT NULL DEFAULT '',
    transport text NOT NULL DEFAULT '',
    principal_kind text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    replay_of_id uuid,
    UNIQUE (workspace_id, id),
    -- failed 必须有非空 failure_class；其他终态 failure_class 必须为空。
    CONSTRAINT search_runs_failure_class CHECK (
        (retrieval_status = 'failed' AND failure_class <> '') OR
        (retrieval_status <> 'failed' AND failure_class = '')
    ),
    -- replay_of_id 只能指向同一 Workspace 的 SearchRun。
    CONSTRAINT search_runs_replay_fk FOREIGN KEY (workspace_id, replay_of_id)
        REFERENCES search_runs (workspace_id, id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS search_run_generations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    search_run_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    generation_id uuid NOT NULL,
    source_content_version bigint NOT NULL,
    indexed_content_version bigint NOT NULL,
    generation_config_hash text NOT NULL,
    embedding_model_id uuid NOT NULL,
    provider_id uuid NOT NULL,
    model_name text NOT NULL,
    model_config_hash text NOT NULL,
    embedding_dimension integer NOT NULL,
    retrieval_config_hash text NOT NULL,
    rerank_snapshot jsonb,
    UNIQUE (workspace_id, search_run_id, knowledge_base_id),
    CONSTRAINT search_run_generations_run_fk FOREIGN KEY (workspace_id, search_run_id)
        REFERENCES search_runs (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT search_run_generations_generation_fk FOREIGN KEY (workspace_id, knowledge_base_id, generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS search_runs_expiry_idx ON search_runs (workspace_id, expires_at);
CREATE INDEX IF NOT EXISTS search_runs_query_hash_idx ON search_runs (workspace_id, query_hash, created_at);
CREATE INDEX IF NOT EXISTS search_run_generations_lookup_idx ON search_run_generations (workspace_id, search_run_id);
