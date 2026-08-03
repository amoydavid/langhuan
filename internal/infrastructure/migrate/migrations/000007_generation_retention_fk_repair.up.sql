-- Development databases may already have applied version 6 before its
-- composite SET NULL action was narrowed to base_generation_id.
ALTER TABLE knowledge_base_index_generations
    DROP CONSTRAINT IF EXISTS index_generations_base_fk;

ALTER TABLE knowledge_base_index_generations
    ADD CONSTRAINT index_generations_base_fk
    FOREIGN KEY (workspace_id, knowledge_base_id, base_generation_id)
    REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
    ON DELETE SET NULL (base_generation_id)
    DEFERRABLE INITIALLY DEFERRED;
