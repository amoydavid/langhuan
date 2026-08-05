-- 000013_parent_child_chunking introduces explicit context, retrieval and flat chunk roles.
ALTER TABLE chunks
    ADD COLUMN role text NOT NULL DEFAULT 'flat',
    ADD COLUMN parent_chunk_id uuid;

ALTER TABLE chunks
    ADD CONSTRAINT chunks_role_check CHECK (role IN ('parent', 'child', 'flat')),
    ADD CONSTRAINT chunks_parent_shape_check CHECK (
        (role IN ('parent', 'flat') AND parent_chunk_id IS NULL)
        OR (role = 'child' AND parent_chunk_id IS NOT NULL)
    ),
    ADD CONSTRAINT chunks_parent_fk FOREIGN KEY (
        workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, parent_chunk_id
    ) REFERENCES chunks (
        workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id
    ) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE chunks DROP CONSTRAINT chunks_workspace_id_chunk_set_id_sequence_key;
ALTER TABLE chunks ADD CONSTRAINT chunks_role_sequence_key UNIQUE (workspace_id, chunk_set_id, role, sequence);

CREATE INDEX idx_chunks_parent_lineage
    ON chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, parent_chunk_id, sequence);
