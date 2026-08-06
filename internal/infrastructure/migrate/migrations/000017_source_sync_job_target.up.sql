-- 放宽 jobs 目标约束，允许 source_sync 这类仅关联知识库的同步任务
-- （document_id / document_revision_id / index_generation_id 三者皆 nil）。
ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_target_check;

ALTER TABLE jobs
    ADD CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_sync')
    );
