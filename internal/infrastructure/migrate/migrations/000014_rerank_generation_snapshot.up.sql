-- 000014: 为 knowledge_base_index_generations 增加 Rerank 快照列。
-- 既有 Generation 默认关闭 Rerank（四列 NULL + rerank_config='{}'），无需回填。

ALTER TABLE knowledge_base_index_generations
  ADD COLUMN rerank_model_id uuid REFERENCES models(id) ON DELETE RESTRICT,
  ADD COLUMN rerank_provider_id uuid REFERENCES model_providers(id) ON DELETE RESTRICT,
  ADD COLUMN rerank_model_name text,
  ADD COLUMN rerank_model_config_hash text,
  ADD COLUMN rerank_config jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE knowledge_base_index_generations
  ADD CONSTRAINT index_generations_rerank_config_object_check
    CHECK (jsonb_typeof(rerank_config) = 'object'),
  ADD CONSTRAINT index_generations_rerank_snapshot_shape_check CHECK (
    (rerank_model_id IS NULL AND rerank_provider_id IS NULL AND rerank_model_name IS NULL
      AND rerank_model_config_hash IS NULL AND rerank_config = '{}'::jsonb)
    OR
    (rerank_model_id IS NOT NULL AND rerank_provider_id IS NOT NULL
      AND btrim(rerank_model_name) <> '' AND btrim(rerank_model_config_hash) <> ''
      AND rerank_config ? 'candidate_top_k' AND rerank_config ? 'failure_mode')
  );

CREATE INDEX idx_index_generations_rerank_model_id
  ON knowledge_base_index_generations (rerank_model_id) WHERE rerank_model_id IS NOT NULL;
CREATE INDEX idx_index_generations_rerank_provider_id
  ON knowledge_base_index_generations (rerank_provider_id) WHERE rerank_provider_id IS NOT NULL;
