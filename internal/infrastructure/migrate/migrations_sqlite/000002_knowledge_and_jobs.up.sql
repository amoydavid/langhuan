-- workspace_source_connections：内容源连接（飞书等）
CREATE TABLE workspace_source_connections (
    id                     TEXT PRIMARY KEY,
    workspace_id           TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    provider               TEXT NOT NULL CHECK (provider IN ('feishu')),
    name                   TEXT NOT NULL,
    config                 TEXT NOT NULL DEFAULT '{}' CHECK (json_type(config) = 'object'),
    credentials_ciphertext BLOB,
    status                 TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    deleted_at             TEXT,
    UNIQUE (workspace_id, provider, name)
);
CREATE UNIQUE INDEX uq_workspace_source_connections_app_id
    ON workspace_source_connections(workspace_id, provider, json_extract(config, '$.app_id'))
    WHERE deleted_at IS NULL;

-- knowledge_bases：知识库（合并 000005 + 000018 的 source_* 列）
CREATE TABLE knowledge_bases (
    id                          TEXT PRIMARY KEY,
    workspace_id                TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                        TEXT NOT NULL CHECK (trim(name) <> ''),
    description                 TEXT NOT NULL DEFAULT '',
    metadata                    TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metadata) = 'object'),
    content_version             INTEGER NOT NULL DEFAULT 0 CHECK (content_version >= 0),
    active_index_generation_id  TEXT,
    file_tree_root_id           TEXT NOT NULL,
    source_type                 TEXT NOT NULL DEFAULT 'upload'
        CHECK (source_type IN ('upload','feishu_drive','feishu_wiki')),
    source_config               TEXT NOT NULL DEFAULT '{}' CHECK (json_type(source_config) = 'object'),
    source_connection_id        TEXT REFERENCES workspace_source_connections(id) ON DELETE SET NULL,
    created_at                  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at                  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    deleted_at                  TEXT,
    UNIQUE (workspace_id, id),
    -- 自引用/前向引用复合 FK（去掉 DEFERRABLE）：
    FOREIGN KEY (workspace_id, id, active_index_generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
        ON DELETE SET NULL,
    FOREIGN KEY (workspace_id, id, file_tree_root_id)
        REFERENCES file_tree_nodes (workspace_id, knowledge_base_id, id)
        ON DELETE SET NULL
);
CREATE UNIQUE INDEX uq_knowledge_bases_active_name
    ON knowledge_bases (workspace_id, lower(name)) WHERE deleted_at IS NULL;

-- knowledge_base_index_generations：索引版本/快照（含 000006/7 SET NULL、000008 删 failed_count、000014 rerank）
CREATE TABLE knowledge_base_index_generations (
    id                          TEXT PRIMARY KEY,
    workspace_id                TEXT NOT NULL,
    knowledge_base_id           TEXT NOT NULL,
    base_generation_id          TEXT,
    embedding_model_id          TEXT NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
    provider_id                 TEXT NOT NULL REFERENCES model_providers(id) ON DELETE RESTRICT,
    model_name                  TEXT NOT NULL,
    embedding_dimension         INTEGER NOT NULL CHECK (embedding_dimension IN (798,1024,2048,3584)),
    model_config_hash           TEXT NOT NULL,
    chunker_version             INTEGER NOT NULL CHECK (chunker_version >= 1),
    chunking_config             TEXT NOT NULL DEFAULT '{}' CHECK (json_type(chunking_config) = 'object'),
    retrieval_config            TEXT NOT NULL DEFAULT '{}' CHECK (json_type(retrieval_config) = 'object'),
    config_hash                 TEXT NOT NULL,
    source_content_version      INTEGER NOT NULL DEFAULT 0,
    indexed_content_version     INTEGER NOT NULL DEFAULT 0,
    status                      TEXT NOT NULL CHECK (status IN ('building','ready','stale','failed','retired')),
    document_count              INTEGER NOT NULL DEFAULT 0,
    chunk_count                 INTEGER NOT NULL DEFAULT 0,
    indexed_count               INTEGER NOT NULL DEFAULT 0,
    manual_edit_count           INTEGER NOT NULL DEFAULT 0,
    disabled_chunk_count        INTEGER NOT NULL DEFAULT 0,
    manual_edit_disposition     TEXT NOT NULL DEFAULT 'not_applicable'
        CHECK (manual_edit_disposition IN ('not_applicable','pending','archive_confirmed')),
    error_class                 TEXT NOT NULL DEFAULT '',
    error_message               TEXT NOT NULL DEFAULT '',
    rerank_model_id             TEXT REFERENCES models(id) ON DELETE RESTRICT,
    rerank_provider_id          TEXT REFERENCES model_providers(id) ON DELETE RESTRICT,
    rerank_model_name           TEXT,
    rerank_model_config_hash    TEXT,
    rerank_config               TEXT NOT NULL DEFAULT '{}' CHECK (json_type(rerank_config) = 'object'),
    created_at                  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ready_at                    TEXT,
    activated_at                TEXT,
    retired_at                  TEXT,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id),
    CONSTRAINT index_generations_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    -- 自引用 base FK：PG 为 ON DELETE SET NULL (base_generation_id)
    -- ⚠ SQLite 不支持列级 SET NULL，会把整组引用列置 NULL（workspace_id NOT NULL → 实际删除会失败）。
    --    效果近似 RESTRICT；建议应用层处理。这里按 SET NULL 翻译并保留分歧。
    CONSTRAINT index_generations_base_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, base_generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
        ON DELETE SET NULL,
    CHECK (source_content_version >= 0 AND indexed_content_version >= 0),
    CHECK (
        document_count >= 0 AND chunk_count >= 0 AND indexed_count >= 0
        AND manual_edit_count >= 0 AND disabled_chunk_count >= 0
        AND indexed_count <= chunk_count
    )
);
CREATE UNIQUE INDEX uq_index_generations_one_building
    ON knowledge_base_index_generations (workspace_id, knowledge_base_id) WHERE status = 'building';
CREATE INDEX idx_index_generations_rerank_model_id
    ON knowledge_base_index_generations (rerank_model_id) WHERE rerank_model_id IS NOT NULL;
CREATE INDEX idx_index_generations_rerank_provider_id
    ON knowledge_base_index_generations (rerank_provider_id) WHERE rerank_provider_id IS NOT NULL;

-- documents：文档（含 000018 external_id、000022 content_hash）
CREATE TABLE documents (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL,
    knowledge_base_id   TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('file','faq','web')),
    title               TEXT NOT NULL CHECK (trim(title) <> ''),
    source_type         TEXT NOT NULL CHECK (trim(source_type) <> ''),
    source_uri          TEXT,
    status              TEXT NOT NULL
        CHECK (status IN ('pending','processing','ready','failed','deleting','deleted')),
    active_revision_id  TEXT,
    metadata            TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metadata) = 'object'),
    external_id         TEXT,
    content_hash        TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    deleted_at          TEXT,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id, kind),
    CONSTRAINT documents_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    -- documents.active_revision_id 复合 FK（PG 用 ALTER + DEFERRABLE；SQLite 不支持
    -- ALTER TABLE ADD CONSTRAINT，改为内联表级 FK，前向引用 document_revisions 合法）：
    CONSTRAINT documents_active_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, id, active_revision_id, kind)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id, kind)
        ON DELETE NO ACTION,
    CHECK (
        (kind = 'web' AND source_uri IS NOT NULL AND trim(source_uri) <> '')
        OR (kind IN ('file','faq') AND source_uri IS NULL)
    )
);
CREATE UNIQUE INDEX uq_documents_active_web_uri
    ON documents (workspace_id, knowledge_base_id, source_uri)
    WHERE kind = 'web' AND deleted_at IS NULL;
CREATE INDEX idx_documents_kb_kind_status
    ON documents (workspace_id, knowledge_base_id, kind, status);
CREATE UNIQUE INDEX uq_documents_workspace_kb_external
    ON documents (workspace_id, knowledge_base_id, external_id)
    WHERE external_id IS NOT NULL AND external_id <> '';

-- document_revisions：文档版本（含 000012 parser_raw_markdown_key）
CREATE TABLE document_revisions (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    knowledge_base_id        TEXT NOT NULL,
    document_id              TEXT NOT NULL,
    kind                     TEXT NOT NULL CHECK (kind IN ('file','faq','web')),
    revision_no              INTEGER NOT NULL CHECK (revision_no >= 1),
    revision_reason          TEXT NOT NULL
        CHECK (revision_reason IN ('ingest','replace','reparse','crawl','edit')),
    original_filename        TEXT,
    file_type                TEXT,
    content_type             TEXT,
    raw_storage_key          TEXT,
    sha256                   TEXT,
    size_bytes               INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    normalized_markdown      TEXT,
    parse_manifest           TEXT CHECK (parse_manifest IS NULL OR json_type(parse_manifest) = 'object'),
    parser_raw_markdown_key  TEXT,
    processing_version       INTEGER NOT NULL CHECK (processing_version >= 1),
    status                   TEXT NOT NULL CHECK (status IN ('pending','parsing','ready','failed')),
    error_class              TEXT NOT NULL DEFAULT '',
    error_message            TEXT NOT NULL DEFAULT '',
    created_by               TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    completed_at             TEXT,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, document_id, revision_no),
    UNIQUE (workspace_id, knowledge_base_id, document_id, id),
    UNIQUE (workspace_id, knowledge_base_id, document_id, id, kind),
    CONSTRAINT document_revisions_document_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, kind)
        REFERENCES documents (workspace_id, knowledge_base_id, id, kind) ON DELETE CASCADE,
    CHECK (
        (kind = 'file'
            AND file_type IS NOT NULL AND trim(file_type) <> ''
            AND original_filename IS NOT NULL AND trim(original_filename) <> ''
            AND raw_storage_key IS NOT NULL AND trim(raw_storage_key) <> '')
        OR (kind = 'faq'
            AND file_type IS NULL AND original_filename IS NULL AND content_type IS NULL
            AND raw_storage_key IS NULL AND sha256 IS NULL AND size_bytes = 0
            AND normalized_markdown IS NULL AND parse_manifest IS NULL)
        OR (kind = 'web'
            AND file_type IS NULL AND original_filename IS NULL)
    )
);

-- faq_revision_contents：FAQ 答案（1:1）
CREATE TABLE faq_revision_contents (
    document_revision_id TEXT PRIMARY KEY,
    workspace_id         TEXT NOT NULL,
    knowledge_base_id    TEXT NOT NULL,
    document_id          TEXT NOT NULL,
    kind                 TEXT NOT NULL DEFAULT 'faq' CHECK (kind = 'faq'),
    answer               TEXT NOT NULL CHECK (trim(answer) <> ''),
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, document_revision_id),
    CONSTRAINT faq_revision_contents_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, kind)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id, kind)
        ON DELETE CASCADE
);

-- faq_revision_questions：FAQ 问题
CREATE TABLE faq_revision_questions (
    id                    TEXT PRIMARY KEY,
    workspace_id          TEXT NOT NULL,
    knowledge_base_id     TEXT NOT NULL,
    document_id           TEXT NOT NULL,
    document_revision_id  TEXT NOT NULL,
    kind                  TEXT NOT NULL DEFAULT 'faq' CHECK (kind = 'faq'),
    sequence              INTEGER NOT NULL CHECK (sequence >= 0),
    question              TEXT NOT NULL CHECK (trim(question) <> ''),
    normalized_question   TEXT NOT NULL CHECK (trim(normalized_question) <> ''),
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, document_revision_id, sequence),
    UNIQUE (workspace_id, document_revision_id, normalized_question),
    CONSTRAINT faq_revision_questions_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, kind)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id, kind)
        ON DELETE CASCADE
);

-- document_chunk_sets：分块集
CREATE TABLE document_chunk_sets (
    id                      TEXT PRIMARY KEY,
    workspace_id            TEXT NOT NULL,
    knowledge_base_id       TEXT NOT NULL,
    document_id             TEXT NOT NULL,
    document_revision_id    TEXT NOT NULL,
    strategy                TEXT NOT NULL CHECK (strategy IN ('standard','faq')),
    chunker_version         INTEGER NOT NULL CHECK (chunker_version >= 1),
    chunking_config         TEXT NOT NULL DEFAULT '{}' CHECK (json_type(chunking_config) = 'object'),
    config_hash             TEXT NOT NULL,
    status                  TEXT NOT NULL CHECK (status IN ('building','ready','failed','archived')),
    chunk_count             INTEGER NOT NULL DEFAULT 0 CHECK (chunk_count >= 0),
    error_class             TEXT NOT NULL DEFAULT '',
    error_message           TEXT NOT NULL DEFAULT '',
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ready_at                TEXT,
    archived_at             TEXT,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, document_id, document_revision_id, id),
    UNIQUE (workspace_id, document_revision_id, strategy, chunker_version, config_hash),
    CONSTRAINT document_chunk_sets_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id)
        ON DELETE CASCADE
);

-- chunks：分块（含 000013 role/parent_chunk_id；唯一键改为含 role）
CREATE TABLE chunks (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    knowledge_base_id        TEXT NOT NULL,
    document_id              TEXT NOT NULL,
    document_revision_id     TEXT NOT NULL,
    chunk_set_id             TEXT NOT NULL,
    sequence                 INTEGER NOT NULL CHECK (sequence >= 0),
    role                     TEXT NOT NULL DEFAULT 'flat' CHECK (role IN ('parent','child','flat')),
    parent_chunk_id          TEXT,
    source_content           TEXT NOT NULL,
    source_anchor            TEXT NOT NULL DEFAULT '{}' CHECK (json_type(source_anchor) = 'object'),
    metadata                 TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metadata) = 'object'),
    active_revision_id       TEXT,
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, chunk_set_id, role, sequence),
    UNIQUE (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id),
    CONSTRAINT chunks_chunk_set_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id)
        REFERENCES document_chunk_sets (workspace_id, knowledge_base_id, document_id, document_revision_id, id)
        ON DELETE CASCADE,
    CHECK (
        (role IN ('parent','flat') AND parent_chunk_id IS NULL)
        OR (role = 'child' AND parent_chunk_id IS NOT NULL)
    ),
    CONSTRAINT chunks_parent_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, parent_chunk_id)
        REFERENCES chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
        ON DELETE NO ACTION,
    -- chunks.active_revision_id 复合 FK（PG 用 ALTER + DEFERRABLE；SQLite 不支持
    -- ALTER TABLE ADD CONSTRAINT，改为内联表级 FK，前向引用 chunk_revisions 合法）：
    CONSTRAINT chunks_active_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id, active_revision_id)
        REFERENCES chunk_revisions (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, id)
        ON DELETE NO ACTION
);
CREATE INDEX idx_chunks_parent_lineage
    ON chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, parent_chunk_id, sequence);

-- chunk_revisions：分块版本（embedding_content 保留为业务文本列；enabled→INTEGER）
CREATE TABLE chunk_revisions (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    knowledge_base_id        TEXT NOT NULL,
    document_id              TEXT NOT NULL,
    document_revision_id     TEXT NOT NULL,
    chunk_set_id             TEXT NOT NULL,
    chunk_id                 TEXT NOT NULL,
    revision_no              INTEGER NOT NULL CHECK (revision_no >= 1),
    base_revision_id         TEXT,
    content                  TEXT NOT NULL,
    context_header           TEXT NOT NULL DEFAULT '',
    embedding_content        TEXT NOT NULL,
    enabled                  INTEGER NOT NULL DEFAULT 1,
    status                   TEXT NOT NULL CHECK (status IN ('pending','indexing','ready','failed')),
    edit_source              TEXT NOT NULL CHECK (edit_source IN ('system','user')),
    editor_user_id           TEXT REFERENCES users(id) ON DELETE SET NULL,
    error_class              TEXT NOT NULL DEFAULT '',
    error_message            TEXT NOT NULL DEFAULT '',
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    indexed_at               TEXT,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, chunk_id, revision_no),
    UNIQUE (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, id),
    CONSTRAINT chunk_revisions_chunk_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id)
        REFERENCES chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chunk_revisions_base_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, base_revision_id)
        REFERENCES chunk_revisions (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, id)
        ON DELETE NO ACTION,
    CHECK (NOT enabled OR trim(content) <> ''),
    CHECK (NOT enabled OR trim(embedding_content) <> ''),
    CHECK (
        (edit_source = 'system' AND editor_user_id IS NULL)
        OR (edit_source = 'user' AND editor_user_id IS NOT NULL AND base_revision_id IS NOT NULL)
    )
);

-- file_tree_nodes：文件树（含 000022 external_id）
CREATE TABLE file_tree_nodes (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    parent_id         TEXT,
    node_type         TEXT NOT NULL CHECK (node_type IN ('root','folder','file')),
    name              TEXT NOT NULL,
    document_id       TEXT,
    document_kind     TEXT,
    external_id       TEXT,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id),
    CONSTRAINT file_tree_nodes_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT file_tree_nodes_parent_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, parent_id)
        REFERENCES file_tree_nodes (workspace_id, knowledge_base_id, id)
        ON DELETE NO ACTION,
    CONSTRAINT file_tree_nodes_document_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_kind)
        REFERENCES documents (workspace_id, knowledge_base_id, id, kind)
        ON DELETE CASCADE,
    CHECK (
        (node_type = 'root' AND parent_id IS NULL AND name = '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'folder' AND parent_id IS NOT NULL AND trim(name) <> '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'file' AND parent_id IS NOT NULL AND trim(name) <> '' AND document_id IS NOT NULL AND document_kind = 'file')
    )
);
CREATE UNIQUE INDEX uq_file_tree_nodes_root
    ON file_tree_nodes (workspace_id, knowledge_base_id) WHERE node_type = 'root';
CREATE UNIQUE INDEX uq_file_tree_nodes_document
    ON file_tree_nodes (workspace_id, knowledge_base_id, document_id) WHERE node_type = 'file';
CREATE UNIQUE INDEX uq_file_tree_nodes_sibling_name
    ON file_tree_nodes (workspace_id, knowledge_base_id, parent_id, lower(name)) WHERE node_type IN ('folder','file');
CREATE UNIQUE INDEX uq_file_tree_nodes_kb_external
    ON file_tree_nodes (workspace_id, knowledge_base_id, external_id)
    WHERE external_id IS NOT NULL AND external_id <> '';

-- document_assets：文档资产
CREATE TABLE document_assets (
    id                    TEXT PRIMARY KEY,
    workspace_id          TEXT NOT NULL,
    knowledge_base_id     TEXT NOT NULL,
    document_id           TEXT NOT NULL,
    document_revision_id  TEXT NOT NULL,
    original_ref          TEXT NOT NULL,
    storage_key           TEXT NOT NULL,
    public_url            TEXT NOT NULL,
    mime_type             TEXT NOT NULL,
    sha256                TEXT NOT NULL DEFAULT '',
    size_bytes            INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    metadata              TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metadata) = 'object'),
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, id),
    CONSTRAINT document_assets_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id)
        ON DELETE CASCADE
);

-- jobs：异步任务（target_check 为 000022 的四分支；含 000018 source_connection_id）
CREATE TABLE jobs (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL,
    knowledge_base_id        TEXT NOT NULL,
    document_id              TEXT,
    document_revision_id     TEXT,
    index_generation_id      TEXT,
    source_connection_id     TEXT,
    type                     TEXT NOT NULL,
    status                   TEXT NOT NULL CHECK (status IN ('pending','running','completed','failed')),
    attempts                 INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    external_job_id          TEXT NOT NULL DEFAULT '',
    payload                  TEXT NOT NULL DEFAULT '{}' CHECK (json_type(payload) = 'object'),
    error_class              TEXT NOT NULL DEFAULT '',
    error_message            TEXT NOT NULL DEFAULT '',
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, id),
    CONSTRAINT jobs_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT jobs_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id)
        ON DELETE CASCADE,
    CONSTRAINT jobs_generation_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, index_generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
        ON DELETE CASCADE,
    CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_sync')
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_cleanup')
    )
);
CREATE INDEX idx_jobs_conn_active
    ON jobs (workspace_id, source_connection_id, type, status)
    WHERE source_connection_id IS NOT NULL;

-- document_ingest_idempotencies：导入幂等（正则 CHECK 移除）
CREATE TABLE document_ingest_idempotencies (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL,
    api_key_id        TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    key               TEXT NOT NULL
        CHECK (trim(key) <> '')
        CHECK (length(key) BETWEEN 1 AND 128)
        CHECK (key NOT LIKE '%' || char(10) || '%' AND key NOT LIKE '%' || char(13) || '%'),
    request_sha256    TEXT NOT NULL,        -- 正则 CHECK 移除
    document_id       TEXT NOT NULL,
    job_id            TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
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
        REFERENCES jobs(workspace_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX document_ingest_idempotencies_natural_key_idx
    ON document_ingest_idempotencies(workspace_id, api_key_id, knowledge_base_id, key);
CREATE INDEX document_ingest_idempotencies_document_idx
    ON document_ingest_idempotencies(document_id);
CREATE INDEX document_ingest_idempotencies_job_idx
    ON document_ingest_idempotencies(job_id);
