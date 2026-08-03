ALTER TABLE knowledge_base_index_generations
    DROP CONSTRAINT IF EXISTS index_generations_base_fk;

ALTER TABLE knowledge_base_index_generations
    ADD CONSTRAINT index_generations_base_fk
    FOREIGN KEY (workspace_id, knowledge_base_id, base_generation_id)
    REFERENCES knowledge_base_index_generations (workspace_id, knowledge_base_id, id)
    DEFERRABLE INITIALLY DEFERRED;
