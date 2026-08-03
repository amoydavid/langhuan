-- Restore the empty knowledge schema expected immediately after migration 4.
-- Data removed by the destructive up migration cannot be restored.

DROP FUNCTION IF EXISTS enforce_knowledge_base_root() CASCADE;
DROP FUNCTION IF EXISTS enforce_file_document_node() CASCADE;
DROP FUNCTION IF EXISTS enforce_faq_revision_complete() CASCADE;

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
    workspace_id uuid REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    embedding_model_id uuid NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
    chunking_config jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE documents (
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
    processing_version integer NOT NULL DEFAULT 0,
    parse_manifest jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE chunks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    sequence integer NOT NULL,
    content text NOT NULL,
    embedding_content text NOT NULL DEFAULT '',
    context_header text NOT NULL DEFAULT '',
    source_anchor jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, sequence)
);

CREATE TABLE document_assets (
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

CREATE TABLE jobs (
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

CREATE TABLE chunk_embeddings (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    embedding halfvec NOT NULL,
    dimension integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE chunk_keywords (
    chunk_id uuid PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    content text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_knowledge_bases_workspace_id ON knowledge_bases(workspace_id);
CREATE INDEX idx_knowledge_bases_embedding_model_id ON knowledge_bases(embedding_model_id);
CREATE INDEX idx_documents_knowledge_base_id ON documents(knowledge_base_id);
CREATE INDEX idx_chunks_document_id ON chunks(document_id);
CREATE INDEX idx_document_assets_document_id ON document_assets(document_id);
CREATE INDEX idx_jobs_document_id ON jobs(document_id);

CREATE INDEX idx_chunk_embeddings_hnsw_798 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(798)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 798;

CREATE INDEX idx_chunk_embeddings_hnsw_1024 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(1024)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 1024;

CREATE INDEX idx_chunk_embeddings_hnsw_2048 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(2048)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 2048;

CREATE INDEX idx_chunk_embeddings_hnsw_3584 ON chunk_embeddings
    USING hnsw ((embedding::halfvec(3584)) halfvec_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE dimension = 3584;
