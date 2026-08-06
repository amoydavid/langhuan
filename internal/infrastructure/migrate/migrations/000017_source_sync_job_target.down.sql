-- 回退到只允许 document 或 generation 的严格二选一约束。
ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_target_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
    );
