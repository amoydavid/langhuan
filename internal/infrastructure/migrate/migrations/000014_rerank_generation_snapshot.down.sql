-- 000014 down: 按 indexes -> constraints -> columns 逆序移除 Rerank 快照。

DROP INDEX IF EXISTS idx_index_generations_rerank_provider_id;
DROP INDEX IF EXISTS idx_index_generations_rerank_model_id;

ALTER TABLE knowledge_base_index_generations
  DROP CONSTRAINT IF EXISTS index_generations_rerank_snapshot_shape_check,
  DROP CONSTRAINT IF EXISTS index_generations_rerank_config_object_check;

ALTER TABLE knowledge_base_index_generations
  DROP COLUMN IF EXISTS rerank_config,
  DROP COLUMN IF EXISTS rerank_model_config_hash,
  DROP COLUMN IF EXISTS rerank_model_name,
  DROP COLUMN IF EXISTS rerank_provider_id,
  DROP COLUMN IF EXISTS rerank_model_id;
