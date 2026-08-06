-- 000015: Workspace 查询阶段默认策略；每个 Workspace 最多一行。

CREATE TABLE workspace_search_settings (
  workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
  rerank_model_id uuid REFERENCES models(id) ON DELETE RESTRICT,
  rerank_provider_id uuid REFERENCES model_providers(id) ON DELETE RESTRICT,
  rerank_model_name text,
  rerank_model_config_hash text,
  rerank_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workspace_search_settings_config_object_check
    CHECK (jsonb_typeof(rerank_config) = 'object'),
  CONSTRAINT workspace_search_settings_rerank_shape_check CHECK (
    (rerank_model_id IS NULL AND rerank_provider_id IS NULL
      AND rerank_model_name IS NULL AND rerank_model_config_hash IS NULL
      AND rerank_config = '{}'::jsonb)
    OR
    (rerank_model_id IS NOT NULL AND rerank_provider_id IS NOT NULL
      AND btrim(rerank_model_name) <> '' AND btrim(rerank_model_config_hash) <> ''
      AND rerank_config ? 'candidate_top_k'
      AND (rerank_config->>'candidate_top_k') ~ '^[0-9]+$'
      AND (rerank_config->>'candidate_top_k')::integer BETWEEN 50 AND 200
      AND rerank_config->>'failure_mode' IN ('fallback', 'fail'))
  )
);

CREATE INDEX idx_workspace_search_settings_rerank_model
  ON workspace_search_settings (rerank_model_id) WHERE rerank_model_id IS NOT NULL;
CREATE INDEX idx_workspace_search_settings_rerank_provider
  ON workspace_search_settings (rerank_provider_id) WHERE rerank_provider_id IS NOT NULL;
