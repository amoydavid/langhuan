CREATE TABLE IF NOT EXISTS model_providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scope text NOT NULL,
    workspace_id uuid REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    provider text NOT NULL,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    credentials_ciphertext bytea,
    status text NOT NULL DEFAULT 'active',
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT model_providers_scope_check
        CHECK (scope IN ('platform', 'workspace')),
    CONSTRAINT model_providers_workspace_check
        CHECK (
            (scope = 'platform' AND workspace_id IS NULL)
            OR
            (scope = 'workspace' AND workspace_id IS NOT NULL)
        ),
    CONSTRAINT model_providers_status_check
        CHECK (status IN ('active', 'disabled'))
);

CREATE UNIQUE INDEX uq_model_providers_platform_name
    ON model_providers (lower(name))
    WHERE scope = 'platform';

CREATE UNIQUE INDEX uq_model_providers_workspace_name
    ON model_providers (workspace_id, lower(name))
    WHERE scope = 'workspace';

CREATE INDEX idx_model_providers_workspace_visibility
    ON model_providers (workspace_id, status, provider);

CREATE INDEX idx_model_providers_platform_visibility
    ON model_providers (status, provider)
    WHERE scope = 'platform';

CREATE TABLE IF NOT EXISTS models (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id uuid NOT NULL REFERENCES model_providers(id) ON DELETE RESTRICT,
    name text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    type text NOT NULL,
    model_name text NOT NULL,
    dimensions integer,
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'active',
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT models_type_check
        CHECK (type IN ('embedding', 'llm', 'rerank')),
    CONSTRAINT models_dimensions_check
        CHECK (
            (
                type = 'embedding'
                AND dimensions IN (798, 1024, 2048, 3584)
            )
            OR
            (
                type IN ('llm', 'rerank')
                AND dimensions IS NULL
            )
        ),
    CONSTRAINT models_status_check
        CHECK (status IN ('active', 'disabled'))
);

CREATE UNIQUE INDEX uq_models_provider_type_name
    ON models (provider_id, type, lower(name));

CREATE INDEX idx_models_provider_type_status
    ON models (provider_id, type, status);

ALTER TABLE knowledge_bases
    DROP COLUMN IF EXISTS embedding_dimension;

ALTER TABLE knowledge_bases
    ADD COLUMN embedding_model_id uuid NOT NULL
    REFERENCES models(id) ON DELETE RESTRICT;

CREATE INDEX idx_knowledge_bases_embedding_model_id
    ON knowledge_bases (embedding_model_id);
