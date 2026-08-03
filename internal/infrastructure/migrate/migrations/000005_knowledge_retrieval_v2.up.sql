-- 000005_knowledge_retrieval_v2: development-only destructive rebuild of the
-- knowledge processing schema. Authentication, Workspace and model registry
-- tables are deliberately preserved.

DROP TABLE IF EXISTS chunk_keywords CASCADE;
DROP TABLE IF EXISTS chunk_embeddings CASCADE;
DROP TABLE IF EXISTS retrieval_entries CASCADE;
DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS document_assets CASCADE;
DROP TABLE IF EXISTS faq_revision_questions CASCADE;
DROP TABLE IF EXISTS faq_revision_contents CASCADE;
DROP TABLE IF EXISTS chunk_revisions CASCADE;
DROP TABLE IF EXISTS chunks CASCADE;
DROP TABLE IF EXISTS document_chunk_sets CASCADE;
DROP TABLE IF EXISTS document_revisions CASCADE;
DROP TABLE IF EXISTS file_tree_nodes CASCADE;
DROP TABLE IF EXISTS documents CASCADE;
DROP TABLE IF EXISTS knowledge_base_index_generations CASCADE;
DROP TABLE IF EXISTS knowledge_bases CASCADE;

CREATE TABLE knowledge_bases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    content_version bigint NOT NULL DEFAULT 0,
    active_index_generation_id uuid,
    file_tree_root_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT knowledge_bases_name_check CHECK (btrim(name) <> ''),
    CONSTRAINT knowledge_bases_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT knowledge_bases_content_version_check CHECK (content_version >= 0),
    UNIQUE (workspace_id, id)
);

CREATE UNIQUE INDEX uq_knowledge_bases_active_name
    ON knowledge_bases (workspace_id, lower(name))
    WHERE deleted_at IS NULL;

CREATE TABLE knowledge_base_index_generations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    base_generation_id uuid,
    embedding_model_id uuid NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
    provider_id uuid NOT NULL REFERENCES model_providers(id) ON DELETE RESTRICT,
    model_name text NOT NULL,
    embedding_dimension integer NOT NULL,
    model_config_hash text NOT NULL,
    chunker_version integer NOT NULL,
    chunking_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    retrieval_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    config_hash text NOT NULL,
    source_content_version bigint NOT NULL DEFAULT 0,
    indexed_content_version bigint NOT NULL DEFAULT 0,
    status text NOT NULL,
    document_count bigint NOT NULL DEFAULT 0,
    chunk_count bigint NOT NULL DEFAULT 0,
    indexed_count bigint NOT NULL DEFAULT 0,
    failed_count bigint NOT NULL DEFAULT 0,
    manual_edit_count bigint NOT NULL DEFAULT 0,
    disabled_chunk_count bigint NOT NULL DEFAULT 0,
    manual_edit_disposition text NOT NULL DEFAULT 'not_applicable',
    error_class text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    ready_at timestamptz,
    activated_at timestamptz,
    retired_at timestamptz,
    CONSTRAINT index_generations_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT index_generations_dimension_check
        CHECK (embedding_dimension IN (798, 1024, 2048, 3584)),
    CONSTRAINT index_generations_chunker_version_check CHECK (chunker_version >= 1),
    CONSTRAINT index_generations_chunking_config_object_check
        CHECK (jsonb_typeof(chunking_config) = 'object'),
    CONSTRAINT index_generations_retrieval_config_object_check
        CHECK (jsonb_typeof(retrieval_config) = 'object'),
    CONSTRAINT index_generations_status_check
        CHECK (status IN ('building', 'ready', 'stale', 'failed', 'retired')),
    CONSTRAINT index_generations_manual_disposition_check
        CHECK (manual_edit_disposition IN ('not_applicable', 'pending', 'archive_confirmed')),
    CONSTRAINT index_generations_version_check
        CHECK (source_content_version >= 0 AND indexed_content_version >= 0),
    CONSTRAINT index_generations_count_check CHECK (
        document_count >= 0 AND chunk_count >= 0 AND indexed_count >= 0
        AND failed_count >= 0 AND manual_edit_count >= 0 AND disabled_chunk_count >= 0
        AND indexed_count <= chunk_count AND failed_count <= chunk_count
    ),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id)
);

ALTER TABLE knowledge_base_index_generations
    ADD CONSTRAINT index_generations_base_fk
    FOREIGN KEY (workspace_id, knowledge_base_id, base_generation_id)
    REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE UNIQUE INDEX uq_index_generations_one_building
    ON knowledge_base_index_generations (workspace_id, knowledge_base_id)
    WHERE status = 'building';

CREATE TABLE documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    kind text NOT NULL,
    title text NOT NULL,
    source_type text NOT NULL,
    source_uri text,
    status text NOT NULL,
    active_revision_id uuid,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT documents_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT documents_kind_check CHECK (kind IN ('file', 'faq', 'web')),
    CONSTRAINT documents_title_check CHECK (btrim(title) <> ''),
    CONSTRAINT documents_source_type_check CHECK (btrim(source_type) <> ''),
    CONSTRAINT documents_source_uri_check CHECK (
        (kind = 'web' AND source_uri IS NOT NULL AND btrim(source_uri) <> '')
        OR (kind IN ('file', 'faq') AND source_uri IS NULL)
    ),
    CONSTRAINT documents_status_check
        CHECK (status IN ('pending', 'processing', 'ready', 'failed', 'deleting', 'deleted')),
    CONSTRAINT documents_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id, kind)
);

CREATE UNIQUE INDEX uq_documents_active_web_uri
    ON documents (workspace_id, knowledge_base_id, source_uri)
    WHERE kind = 'web' AND deleted_at IS NULL;

CREATE INDEX idx_documents_kb_kind_status
    ON documents (workspace_id, knowledge_base_id, kind, status);

CREATE TABLE document_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    kind text NOT NULL,
    revision_no bigint NOT NULL,
    revision_reason text NOT NULL,
    original_filename text,
    file_type text,
    content_type text,
    raw_storage_key text,
    sha256 text,
    size_bytes bigint NOT NULL DEFAULT 0,
    normalized_markdown text,
    parse_manifest jsonb,
    processing_version integer NOT NULL,
    status text NOT NULL,
    error_class text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CONSTRAINT document_revisions_document_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, kind)
        REFERENCES documents (workspace_id, knowledge_base_id, id, kind) ON DELETE CASCADE,
    CONSTRAINT document_revisions_kind_check CHECK (kind IN ('file', 'faq', 'web')),
    CONSTRAINT document_revisions_reason_check
        CHECK (revision_reason IN ('ingest', 'replace', 'reparse', 'crawl', 'edit')),
    CONSTRAINT document_revisions_status_check
        CHECK (status IN ('pending', 'parsing', 'ready', 'failed')),
    CONSTRAINT document_revisions_revision_no_check CHECK (revision_no >= 1),
    CONSTRAINT document_revisions_processing_version_check CHECK (processing_version >= 1),
    CONSTRAINT document_revisions_size_check CHECK (size_bytes >= 0),
    CONSTRAINT document_revisions_manifest_object_check
        CHECK (parse_manifest IS NULL OR jsonb_typeof(parse_manifest) = 'object'),
    CONSTRAINT document_revisions_kind_fields_check CHECK (
        (
            kind = 'file'
            AND file_type IS NOT NULL AND btrim(file_type) <> ''
            AND original_filename IS NOT NULL AND btrim(original_filename) <> ''
            AND raw_storage_key IS NOT NULL AND btrim(raw_storage_key) <> ''
        )
        OR (
            kind = 'faq'
            AND file_type IS NULL AND original_filename IS NULL AND content_type IS NULL
            AND raw_storage_key IS NULL AND sha256 IS NULL AND size_bytes = 0
            AND normalized_markdown IS NULL AND parse_manifest IS NULL
        )
        OR (
            kind = 'web'
            AND file_type IS NULL AND original_filename IS NULL
        )
    ),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, document_id, revision_no),
    UNIQUE (workspace_id, knowledge_base_id, document_id, id),
    UNIQUE (workspace_id, knowledge_base_id, document_id, id, kind)
);

ALTER TABLE documents
    ADD CONSTRAINT documents_active_revision_fk
    FOREIGN KEY (workspace_id, knowledge_base_id, id, active_revision_id, kind)
    REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id, kind)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE faq_revision_contents (
    document_revision_id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    kind text NOT NULL DEFAULT 'faq',
    answer text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT faq_revision_contents_kind_check CHECK (kind = 'faq'),
    CONSTRAINT faq_revision_contents_answer_check CHECK (btrim(answer) <> ''),
    CONSTRAINT faq_revision_contents_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, kind)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id, kind)
        ON DELETE CASCADE,
    UNIQUE (workspace_id, document_revision_id)
);

CREATE TABLE faq_revision_questions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    document_revision_id uuid NOT NULL,
    kind text NOT NULL DEFAULT 'faq',
    sequence integer NOT NULL,
    question text NOT NULL,
    normalized_question text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT faq_revision_questions_kind_check CHECK (kind = 'faq'),
    CONSTRAINT faq_revision_questions_sequence_check CHECK (sequence >= 0),
    CONSTRAINT faq_revision_questions_question_check CHECK (btrim(question) <> ''),
    CONSTRAINT faq_revision_questions_normalized_check CHECK (btrim(normalized_question) <> ''),
    CONSTRAINT faq_revision_questions_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, kind)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id, kind)
        ON DELETE CASCADE,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, document_revision_id, sequence),
    UNIQUE (workspace_id, document_revision_id, normalized_question)
);

CREATE TABLE document_chunk_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    document_revision_id uuid NOT NULL,
    strategy text NOT NULL,
    chunker_version integer NOT NULL,
    chunking_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    config_hash text NOT NULL,
    status text NOT NULL,
    chunk_count bigint NOT NULL DEFAULT 0,
    error_class text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    ready_at timestamptz,
    archived_at timestamptz,
    CONSTRAINT document_chunk_sets_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id)
        ON DELETE CASCADE,
    CONSTRAINT document_chunk_sets_strategy_check CHECK (strategy IN ('standard', 'faq')),
    CONSTRAINT document_chunk_sets_chunker_version_check CHECK (chunker_version >= 1),
    CONSTRAINT document_chunk_sets_config_object_check CHECK (jsonb_typeof(chunking_config) = 'object'),
    CONSTRAINT document_chunk_sets_status_check
        CHECK (status IN ('building', 'ready', 'failed', 'archived')),
    CONSTRAINT document_chunk_sets_count_check CHECK (chunk_count >= 0),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, document_id, document_revision_id, id),
    UNIQUE (workspace_id, document_revision_id, strategy, chunker_version, config_hash)
);

CREATE TABLE chunks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    document_revision_id uuid NOT NULL,
    chunk_set_id uuid NOT NULL,
    sequence integer NOT NULL,
    source_content text NOT NULL,
    source_anchor jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    active_revision_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chunks_chunk_set_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id)
        REFERENCES document_chunk_sets (workspace_id, knowledge_base_id, document_id, document_revision_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chunks_sequence_check CHECK (sequence >= 0),
    CONSTRAINT chunks_source_anchor_object_check CHECK (jsonb_typeof(source_anchor) = 'object'),
    CONSTRAINT chunks_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, chunk_set_id, sequence),
    UNIQUE (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
);

CREATE TABLE chunk_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    document_revision_id uuid NOT NULL,
    chunk_set_id uuid NOT NULL,
    chunk_id uuid NOT NULL,
    revision_no bigint NOT NULL,
    base_revision_id uuid,
    content text NOT NULL,
    context_header text NOT NULL DEFAULT '',
    embedding_content text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    status text NOT NULL,
    edit_source text NOT NULL,
    editor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    error_class text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    indexed_at timestamptz,
    CONSTRAINT chunk_revisions_chunk_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id)
        REFERENCES chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chunk_revisions_revision_no_check CHECK (revision_no >= 1),
    CONSTRAINT chunk_revisions_enabled_content_check CHECK (NOT enabled OR btrim(content) <> ''),
    CONSTRAINT chunk_revisions_enabled_embedding_check CHECK (NOT enabled OR btrim(embedding_content) <> ''),
    CONSTRAINT chunk_revisions_status_check CHECK (status IN ('pending', 'indexing', 'ready', 'failed')),
    CONSTRAINT chunk_revisions_edit_source_check CHECK (edit_source IN ('system', 'user')),
    CONSTRAINT chunk_revisions_editor_check CHECK (
        (edit_source = 'system' AND editor_user_id IS NULL)
        OR (edit_source = 'user' AND editor_user_id IS NOT NULL AND base_revision_id IS NOT NULL)
    ),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, chunk_id, revision_no),
    UNIQUE (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id, id)
);

ALTER TABLE chunk_revisions
    ADD CONSTRAINT chunk_revisions_base_fk
    FOREIGN KEY (
        workspace_id, knowledge_base_id, document_id, document_revision_id,
        chunk_set_id, chunk_id, base_revision_id
    )
    REFERENCES chunk_revisions (
        workspace_id, knowledge_base_id, document_id, document_revision_id,
        chunk_set_id, chunk_id, id
    )
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE chunks
    ADD CONSTRAINT chunks_active_revision_fk
    FOREIGN KEY (
        workspace_id, knowledge_base_id, document_id, document_revision_id,
        chunk_set_id, id, active_revision_id
    )
    REFERENCES chunk_revisions (
        workspace_id, knowledge_base_id, document_id, document_revision_id,
        chunk_set_id, chunk_id, id
    )
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE file_tree_nodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    parent_id uuid,
    node_type text NOT NULL,
    name text NOT NULL,
    document_id uuid,
    document_kind text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT file_tree_nodes_kb_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT file_tree_nodes_type_check CHECK (node_type IN ('root', 'folder', 'file')),
    CONSTRAINT file_tree_nodes_shape_check CHECK (
        (node_type = 'root' AND parent_id IS NULL AND name = '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'folder' AND parent_id IS NOT NULL AND btrim(name) <> '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'file' AND parent_id IS NOT NULL AND btrim(name) <> '' AND document_id IS NOT NULL AND document_kind = 'file')
    ),
    CONSTRAINT file_tree_nodes_parent_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, parent_id)
        REFERENCES file_tree_nodes (workspace_id, knowledge_base_id, id)
        DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT file_tree_nodes_document_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_kind)
        REFERENCES documents (workspace_id, knowledge_base_id, id, kind)
        ON DELETE CASCADE,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, knowledge_base_id, id)
);

CREATE UNIQUE INDEX uq_file_tree_nodes_root
    ON file_tree_nodes (workspace_id, knowledge_base_id)
    WHERE node_type = 'root';

CREATE UNIQUE INDEX uq_file_tree_nodes_document
    ON file_tree_nodes (workspace_id, knowledge_base_id, document_id)
    WHERE node_type = 'file';

CREATE UNIQUE INDEX uq_file_tree_nodes_sibling_name
    ON file_tree_nodes (workspace_id, knowledge_base_id, parent_id, lower(name))
    WHERE node_type IN ('folder', 'file');

ALTER TABLE knowledge_bases
    ADD CONSTRAINT knowledge_bases_active_generation_fk
    FOREIGN KEY (workspace_id, id, active_index_generation_id)
    REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
    DEFERRABLE INITIALLY DEFERRED,
    ADD CONSTRAINT knowledge_bases_file_tree_root_fk
    FOREIGN KEY (workspace_id, id, file_tree_root_id)
    REFERENCES file_tree_nodes (workspace_id, knowledge_base_id, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE document_assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid NOT NULL,
    document_revision_id uuid NOT NULL,
    original_ref text NOT NULL,
    storage_key text NOT NULL,
    public_url text NOT NULL,
    mime_type text NOT NULL,
    sha256 text NOT NULL DEFAULT '',
    size_bytes bigint NOT NULL DEFAULT 0,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT document_assets_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id)
        ON DELETE CASCADE,
    CONSTRAINT document_assets_size_check CHECK (size_bytes >= 0),
    CONSTRAINT document_assets_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    UNIQUE (workspace_id, id)
);

CREATE TABLE jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    document_id uuid,
    document_revision_id uuid,
    index_generation_id uuid,
    type text NOT NULL,
    status text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    external_job_id text NOT NULL DEFAULT '',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_class text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
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
    CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
    ),
    CONSTRAINT jobs_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    CONSTRAINT jobs_attempts_check CHECK (attempts >= 0),
    CONSTRAINT jobs_payload_object_check CHECK (jsonb_typeof(payload) = 'object'),
    UNIQUE (workspace_id, id)
);

CREATE TABLE retrieval_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    index_generation_id uuid NOT NULL,
    document_id uuid NOT NULL,
    document_revision_id uuid NOT NULL,
    chunk_set_id uuid NOT NULL,
    chunk_id uuid NOT NULL,
    chunk_revision_id uuid NOT NULL,
    state text NOT NULL,
    search_content text NOT NULL,
    content text NOT NULL,
    source_anchor jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    fts_document tsvector,
    embedding halfvec,
    dimension integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    retired_at timestamptz,
    CONSTRAINT retrieval_entries_generation_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, index_generation_id)
        REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_revision_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
        REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id)
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_chunk_set_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id)
        REFERENCES document_chunk_sets (workspace_id, knowledge_base_id, document_id, document_revision_id, id)
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_chunk_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, chunk_id)
        REFERENCES chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_chunk_revision_fk
        FOREIGN KEY (
            workspace_id, knowledge_base_id, document_id, document_revision_id,
            chunk_set_id, chunk_id, chunk_revision_id
        )
        REFERENCES chunk_revisions (
            workspace_id, knowledge_base_id, document_id, document_revision_id,
            chunk_set_id, chunk_id, id
        )
        ON DELETE CASCADE,
    CONSTRAINT retrieval_entries_state_check
        CHECK (state IN ('staging', 'published', 'retired', 'failed')),
    CONSTRAINT retrieval_entries_search_content_check CHECK (btrim(search_content) <> ''),
    CONSTRAINT retrieval_entries_content_check CHECK (btrim(content) <> ''),
    CONSTRAINT retrieval_entries_source_anchor_object_check CHECK (jsonb_typeof(source_anchor) = 'object'),
    CONSTRAINT retrieval_entries_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT retrieval_entries_dimension_check
        CHECK (dimension IS NULL OR dimension IN (798, 1024, 2048, 3584)),
    CONSTRAINT retrieval_entries_published_check CHECK (
        state <> 'published'
        OR (embedding IS NOT NULL AND dimension IS NOT NULL AND fts_document IS NOT NULL AND published_at IS NOT NULL)
    ),
    UNIQUE (workspace_id, id)
);

CREATE UNIQUE INDEX uq_retrieval_entries_staging
    ON retrieval_entries (workspace_id, index_generation_id, chunk_id)
    WHERE state = 'staging';

CREATE UNIQUE INDEX uq_retrieval_entries_published
    ON retrieval_entries (workspace_id, index_generation_id, chunk_id)
    WHERE state = 'published';

CREATE INDEX idx_retrieval_entries_scope
    ON retrieval_entries (workspace_id, knowledge_base_id, index_generation_id, state);

CREATE INDEX idx_retrieval_entries_document
    ON retrieval_entries (workspace_id, index_generation_id, document_id, state);

CREATE INDEX idx_retrieval_entries_chunk
    ON retrieval_entries (workspace_id, index_generation_id, chunk_id, state);

CREATE INDEX idx_retrieval_entries_fts
    ON retrieval_entries USING gin (fts_document)
    WHERE state = 'published';

CREATE INDEX idx_retrieval_entries_hnsw_798 ON retrieval_entries
    USING hnsw ((embedding::halfvec(798)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 798 AND state = 'published';

CREATE INDEX idx_retrieval_entries_hnsw_1024 ON retrieval_entries
    USING hnsw ((embedding::halfvec(1024)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 1024 AND state = 'published';

CREATE INDEX idx_retrieval_entries_hnsw_2048 ON retrieval_entries
    USING hnsw ((embedding::halfvec(2048)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 2048 AND state = 'published';

CREATE INDEX idx_retrieval_entries_hnsw_3584 ON retrieval_entries
    USING hnsw ((embedding::halfvec(3584)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 3584 AND state = 'published';

CREATE OR REPLACE FUNCTION enforce_faq_revision_complete()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    revision_id uuid;
    revision_kind text;
BEGIN
    IF TG_TABLE_NAME = 'document_revisions' THEN
        revision_id := COALESCE(NEW.id, OLD.id);
    ELSE
        revision_id := COALESCE(NEW.document_revision_id, OLD.document_revision_id);
    END IF;

    SELECT kind INTO revision_kind FROM document_revisions WHERE id = revision_id;
    IF revision_kind = 'faq' THEN
        IF (SELECT count(*) FROM faq_revision_contents WHERE document_revision_id = revision_id) <> 1 THEN
            RAISE EXCEPTION 'FAQ revision % must have exactly one answer', revision_id
                USING ERRCODE = '23514';
        END IF;
        IF (SELECT count(*) FROM faq_revision_questions WHERE document_revision_id = revision_id) < 1 THEN
            RAISE EXCEPTION 'FAQ revision % must have at least one question', revision_id
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER faq_revision_complete_on_revision
    AFTER INSERT OR UPDATE ON document_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_faq_revision_complete();

CREATE CONSTRAINT TRIGGER faq_revision_complete_on_content
    AFTER INSERT OR UPDATE OR DELETE ON faq_revision_contents
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_faq_revision_complete();

CREATE CONSTRAINT TRIGGER faq_revision_complete_on_question
    AFTER INSERT OR UPDATE OR DELETE ON faq_revision_questions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_faq_revision_complete();

CREATE OR REPLACE FUNCTION enforce_file_document_node()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_document_id uuid;
    target_kind text;
    target_deleted_at timestamptz;
BEGIN
    IF TG_TABLE_NAME = 'documents' THEN
        target_document_id := COALESCE(NEW.id, OLD.id);
    ELSE
        target_document_id := COALESCE(NEW.document_id, OLD.document_id);
    END IF;

    IF target_document_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT kind, deleted_at INTO target_kind, target_deleted_at
    FROM documents WHERE id = target_document_id;
    IF target_kind = 'file' AND target_deleted_at IS NULL THEN
        IF (SELECT count(*) FROM file_tree_nodes WHERE document_id = target_document_id AND node_type = 'file') <> 1 THEN
            RAISE EXCEPTION 'File document % must have exactly one file node', target_document_id
                USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER file_document_node_on_document
    AFTER INSERT OR UPDATE ON documents
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_file_document_node();

CREATE CONSTRAINT TRIGGER file_document_node_on_node
    AFTER INSERT OR UPDATE OR DELETE ON file_tree_nodes
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_file_document_node();

CREATE OR REPLACE FUNCTION enforce_knowledge_base_root()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_kb_id uuid;
    root_id uuid;
    root_type text;
BEGIN
    IF TG_TABLE_NAME = 'knowledge_bases' THEN
        target_kb_id := COALESCE(NEW.id, OLD.id);
    ELSE
        target_kb_id := COALESCE(NEW.knowledge_base_id, OLD.knowledge_base_id);
    END IF;

    SELECT file_tree_root_id INTO root_id FROM knowledge_bases WHERE id = target_kb_id;
    IF root_id IS NULL THEN
        RETURN NULL;
    END IF;
    SELECT node_type INTO root_type FROM file_tree_nodes WHERE id = root_id;
    IF root_type IS DISTINCT FROM 'root' THEN
        RAISE EXCEPTION 'Knowledge base % root pointer must reference root node', target_kb_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER knowledge_base_root_on_kb
    AFTER INSERT OR UPDATE ON knowledge_bases
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_knowledge_base_root();

CREATE CONSTRAINT TRIGGER knowledge_base_root_on_node
    AFTER INSERT OR UPDATE OR DELETE ON file_tree_nodes
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION enforce_knowledge_base_root();
