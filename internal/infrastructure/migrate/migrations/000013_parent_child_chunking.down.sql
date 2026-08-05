-- Revert parent-child chunk facts. This removes v3 hierarchy data and is only safe before use.
DROP INDEX IF EXISTS idx_chunks_parent_lineage;
ALTER TABLE chunks DROP CONSTRAINT IF EXISTS chunks_parent_fk;
ALTER TABLE chunks DROP CONSTRAINT IF EXISTS chunks_parent_shape_check;
ALTER TABLE chunks DROP CONSTRAINT IF EXISTS chunks_role_check;
ALTER TABLE chunks DROP CONSTRAINT IF EXISTS chunks_role_sequence_key;
ALTER TABLE chunks ADD CONSTRAINT chunks_workspace_id_chunk_set_id_sequence_key UNIQUE (workspace_id, chunk_set_id, sequence);
ALTER TABLE chunks DROP COLUMN IF EXISTS parent_chunk_id;
ALTER TABLE chunks DROP COLUMN IF EXISTS role;
