-- retrieval_entries：检索条目（不建 embedding/fts_document 列；dimension 保留）
-- ⚠ embedding(halfvec) 与 fts_document(tsvector) 由切片6 独立表承载，本表省略。
-- ⚠ 引用这三列的 retrieval_entries_published_check 一并移除（不变式下沉切片6 表）。
CREATE TABLE retrieval_entries (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    knowledge_base_id        TEXT NOT NULL,
    index_generation_id      TEXT NOT NULL,
    document_id              TEXT NOT NULL,
    document_revision_id     TEXT NOT NULL,
    chunk_set_id             TEXT NOT NULL,
    chunk_id                 TEXT NOT NULL,
    chunk_revision_id        TEXT NOT NULL,
    state                    TEXT NOT NULL CHECK (state IN ('staging','published','retired','failed')),
    search_content           TEXT NOT NULL CHECK (trim(search_content) <> ''),
    content                  TEXT NOT NULL CHECK (trim(content) <> ''),
    source_anchor            TEXT NOT NULL DEFAULT '{}' CHECK (json_type(source_anchor) = 'object'),
    metadata                 TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metadata) = 'object'),
    dimension                INTEGER CHECK (dimension IS NULL OR dimension IN (798,1024,2048,3584)),
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    published_at             TEXT,
    retired_at               TEXT,
    UNIQUE (workspace_id, id),
    CONSTRAINT retrieval_entries_generation_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, index_generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id) ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id) ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_chunk_set_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id)
        REFERENCES document_chunk_sets (workspace_id, knowledge_base_id, document_id, document_revision_id, id)
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_chunk_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id)
        REFERENCES chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_chunk_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, chunk_revision_id)
        REFERENCES chunk_revisions (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, id)
        ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_retrieval_entries_staging
    ON retrieval_entries (workspace_id, index_generation_id, chunk_id) WHERE state = 'staging';
CREATE UNIQUE INDEX uq_retrieval_entries_published
    ON retrieval_entries (workspace_id, index_generation_id, chunk_id) WHERE state = 'published';
CREATE INDEX idx_retrieval_entries_scope
    ON retrieval_entries (workspace_id, knowledge_base_id, index_generation_id, state);
CREATE INDEX idx_retrieval_entries_document
    ON retrieval_entries (workspace_id, index_generation_id, document_id, state);
CREATE INDEX idx_retrieval_entries_chunk
    ON retrieval_entries (workspace_id, index_generation_id, chunk_id, state);
-- idx_retrieval_entries_fts(GIN tsvector) 与 idx_retrieval_entries_hnsw_*（HNSW halfvec）跳过，
-- 切片6 用 FTS5 虚拟表 + 独立向量表替代。

-- search_runs：检索运行快照（replay 自引用）
CREATE TABLE search_runs (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL,
    requested_scope   TEXT NOT NULL CHECK (requested_scope IN ('selected','api_key_bound_all')),
    query_hash        TEXT NOT NULL,
    query_chars       INTEGER NOT NULL CHECK (query_chars >= 0),
    vector_top_k      INTEGER NOT NULL CHECK (vector_top_k > 0),
    keyword_top_k     INTEGER NOT NULL CHECK (keyword_top_k > 0),
    final_top_k       INTEGER NOT NULL CHECK (final_top_k > 0),
    retrieval_status  TEXT NOT NULL
        CHECK (retrieval_status IN ('running','available','empty','degraded','failed')),
    failure_class     TEXT NOT NULL DEFAULT '',
    ranking_stage     TEXT NOT NULL DEFAULT '',
    result_count      INTEGER NOT NULL DEFAULT 0 CHECK (result_count >= 0),
    request_id        TEXT NOT NULL DEFAULT '',
    transport         TEXT NOT NULL DEFAULT '',
    principal_kind    TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    completed_at      TEXT,
    expires_at        TEXT NOT NULL,
    replay_of_id      TEXT,
    UNIQUE (workspace_id, id),
    CHECK (
        (retrieval_status = 'failed' AND failure_class <> '')
        OR (retrieval_status <> 'failed' AND failure_class = '')
    ),
    CONSTRAINT search_runs_replay_fk
        FOREIGN KEY (workspace_id, replay_of_id)
        REFERENCES search_runs (workspace_id, id) ON DELETE SET NULL
);
CREATE INDEX search_runs_expiry_idx ON search_runs (workspace_id, expires_at);
CREATE INDEX search_runs_query_hash_idx ON search_runs (workspace_id, query_hash, created_at);

-- search_run_generations：SearchRun 关联的 Generation 快照
CREATE TABLE search_run_generations (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    search_run_id            TEXT NOT NULL,
    knowledge_base_id        TEXT NOT NULL,
    generation_id            TEXT NOT NULL,
    source_content_version   INTEGER NOT NULL,
    indexed_content_version  INTEGER NOT NULL,
    generation_config_hash   TEXT NOT NULL,
    embedding_model_id       TEXT NOT NULL,
    provider_id              TEXT NOT NULL,
    model_name               TEXT NOT NULL,
    model_config_hash        TEXT NOT NULL,
    embedding_dimension      INTEGER NOT NULL,
    retrieval_config_hash    TEXT NOT NULL,
    rerank_snapshot          TEXT CHECK (rerank_snapshot IS NULL OR json_type(rerank_snapshot) = 'object'),
    UNIQUE (workspace_id, search_run_id, knowledge_base_id),
    CONSTRAINT search_run_generations_run_fk
        FOREIGN KEY (workspace_id, search_run_id)
        REFERENCES search_runs (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT search_run_generations_generation_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
        ON DELETE RESTRICT
);
CREATE INDEX search_run_generations_lookup_idx
    ON search_run_generations (workspace_id, search_run_id);
