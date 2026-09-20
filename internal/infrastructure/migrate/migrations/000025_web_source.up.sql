-- 000025_web_source: 知识库内网站内容源（v1.3.0）。
-- 1) kb_sources：挂在知识库下的内容源实体（本版仅 kind='web'），承载源级配置与
--    同步运行态（cursor/force latch/last_result）；deleted_at IS NULL 为活行。
-- 2) documents.kb_source_id + source_meta：页面文档溯源与每文档抓取元数据。
-- 3) workspace_source_connections.provider CHECK 放宽为四值（feishu/firecrawl/jina/browser）。
-- 4) jobs.kb_source_id + jobs_target_check 源级分支 + enforce_jobs_source_target
--    约束触发器：source_sync/source_cleanup 任务 target 可为 kb_sources.id，
--    写入或改指目标时校验源未软删、所属 KB 未软删；UPDATE 仅在目标列变化时
--    触发，避免软删源冻结在飞任务（KB 级飞书分支语义原样保留）。

-- (1) kb_sources 表。
-- 遵循库内复合外键惯例：kb_sources -> knowledge_bases 携带完整 lineage
-- (workspace_id, kb_id)；UNIQUE (workspace_id, kb_id, id) 供 documents/jobs
-- 的复合外键引用，保证引用行的 KB 与源所属 KB 一致。
CREATE TABLE IF NOT EXISTS kb_sources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    kb_id uuid NOT NULL,
    kind text NOT NULL DEFAULT 'web',
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    sync_cursor jsonb NOT NULL DEFAULT '{}'::jsonb,
    next_sync_at timestamptz,
    sync_requested_force boolean NOT NULL DEFAULT false,
    sync_last_result jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT kb_sources_kb_fk
        FOREIGN KEY (workspace_id, kb_id)
        REFERENCES knowledge_bases (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT kb_sources_kind_check CHECK (kind IN ('web')),
    CONSTRAINT kb_sources_name_check CHECK (btrim(name) <> ''),
    CONSTRAINT kb_sources_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT kb_sources_config_object_check CHECK (jsonb_typeof(config) = 'object'),
    CONSTRAINT kb_sources_sync_cursor_object_check CHECK (jsonb_typeof(sync_cursor) = 'object'),
    CONSTRAINT kb_sources_sync_last_result_object_check
        CHECK (jsonb_typeof(sync_last_result) = 'object'),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, kb_id, id)
);

-- 按库内列出惯例：按 (workspace_id, kb_id) 定位库下全部源。
CREATE INDEX IF NOT EXISTS idx_kb_sources_workspace_kb
    ON kb_sources (workspace_id, kb_id);

-- 同库活源名称唯一（软删后释放名字，供重建同名源）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_kb_sources_active_name
    ON kb_sources (workspace_id, kb_id, lower(name))
    WHERE deleted_at IS NULL;

-- (2) documents 溯源列：kb_source_id 指向产生该页面的源；上传文档为 NULL。
-- ON DELETE SET NULL 必须限定列：源行硬删除断开溯源，但不得置空租户键
-- workspace_id/knowledge_base_id（与 000007 对 base_generation_id 的处理同理）。
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS kb_source_id uuid,
    ADD COLUMN IF NOT EXISTS source_meta jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE documents
    DROP CONSTRAINT IF EXISTS documents_kb_source_fk;
ALTER TABLE documents
    ADD CONSTRAINT documents_kb_source_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, kb_source_id)
        REFERENCES kb_sources (workspace_id, kb_id, id)
        ON DELETE SET NULL (kb_source_id);

ALTER TABLE documents
    DROP CONSTRAINT IF EXISTS documents_source_meta_object_check;
ALTER TABLE documents
    ADD CONSTRAINT documents_source_meta_object_check
        CHECK (jsonb_typeof(source_meta) = 'object');

-- 按源统计页面数 / 按源清理（部分索引跟随库内 external_id/conn 惯例）。
CREATE INDEX IF NOT EXISTS idx_documents_kb_source
    ON documents (workspace_id, kb_source_id)
    WHERE kb_source_id IS NOT NULL;

-- (3) provider CHECK 放宽为四值。
ALTER TABLE workspace_source_connections
    DROP CONSTRAINT IF EXISTS workspace_source_connections_provider_check;
ALTER TABLE workspace_source_connections
    ADD CONSTRAINT workspace_source_connections_provider_check
        CHECK (provider IN ('feishu', 'firecrawl', 'jina', 'browser'));

-- (4) jobs 源级目标：source_sync/source_cleanup 的 target 可为 kb_sources.id。
ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS kb_source_id uuid;

ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_source_fk;
ALTER TABLE jobs
    ADD CONSTRAINT jobs_source_fk
        FOREIGN KEY (workspace_id, knowledge_base_id, kb_source_id)
        REFERENCES kb_sources (workspace_id, kb_id, id)
        ON DELETE CASCADE;

-- jobs_target_check 五选一：原有四分支语义不变（对既有行等价，新列全 NULL），
-- 各分支补 kb_source_id IS NULL 使目标种类互斥；新增第五分支 = 源级目标，
-- 仅 source_sync/source_cleanup 可用，且与 document/generation/KB 目标互斥。
ALTER TABLE jobs
    DROP CONSTRAINT IF EXISTS jobs_target_check;
ALTER TABLE jobs
    ADD CONSTRAINT jobs_target_check CHECK (
        (document_id IS NOT NULL AND document_revision_id IS NOT NULL AND index_generation_id IS NULL AND kb_source_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NOT NULL AND kb_source_id IS NULL)
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND kb_source_id IS NULL AND type = 'source_sync')
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND kb_source_id IS NULL AND type = 'source_cleanup')
        OR (document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND kb_source_id IS NOT NULL AND type IN ('source_sync', 'source_cleanup'))
    );

-- 按源统计 active 任务（CountActiveBySource，镜像 000018 的 idx_jobs_conn_active）。
CREATE INDEX IF NOT EXISTS idx_jobs_source_active
    ON jobs (workspace_id, kb_source_id, type, status)
    WHERE kb_source_id IS NOT NULL;

-- (5) 源级目标软删校验：CHECK 无法跨表，用约束触发器校验源存在
-- （jobs_source_fk 之外的双保险）、源未软删、所属 KB 未软删。
-- 触发面刻意收窄：
--   - INSERT 仅在携带源目标（kb_source_id IS NOT NULL）时校验；
--   - UPDATE 仅在目标列变化（kb_source_id IS DISTINCT FROM OLD.kb_source_id）时校验。
-- 由此，任务落库后源/KB 被软删不会冻结在飞任务——worker 仍可推进 status/
-- error 等列到终态（同步执行按既有幂等规则跳过已删目标）；只有"把目标指到
-- 某个源"（新建或改指）才需要证明该目标当前有效。
-- kb_source_id 为 NULL（文档/Generation/KB 级飞书目标）的行不进入该校验——
-- KB 级飞书分支行为原样保留。
-- 注：PostgreSQL 禁止含 INSERT 事件的触发器在 WHEN 中引用 OLD，且 UPDATE 事件
-- 不能只靠 INSERT 语义区分，故拆成 INSERT/UPDATE 两个触发器。
CREATE OR REPLACE FUNCTION enforce_jobs_source_target()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    source_deleted_at timestamptz;
    kb_deleted_at timestamptz;
BEGIN
    -- 目标列被清空（UPDATE 置 NULL，撤销源目标）时无需源存活，直接放行。
    IF NEW.kb_source_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT src.deleted_at, kb.deleted_at
    INTO source_deleted_at, kb_deleted_at
    FROM kb_sources AS src
    JOIN knowledge_bases AS kb
      ON kb.workspace_id = src.workspace_id AND kb.id = src.kb_id
    WHERE src.workspace_id = NEW.workspace_id
      AND src.id = NEW.kb_source_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'source job target % does not exist', NEW.kb_source_id
            USING ERRCODE = '23514';
    END IF;
    IF source_deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'source job target % has been deleted', NEW.kb_source_id
            USING ERRCODE = '23514';
    END IF;
    IF kb_deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'source job target % belongs to a deleted knowledge base', NEW.kb_source_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS jobs_source_target_on_insert ON jobs;
CREATE CONSTRAINT TRIGGER jobs_source_target_on_insert
    AFTER INSERT ON jobs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    WHEN (NEW.kb_source_id IS NOT NULL)
    EXECUTE FUNCTION enforce_jobs_source_target();

DROP TRIGGER IF EXISTS jobs_source_target_on_update ON jobs;
CREATE CONSTRAINT TRIGGER jobs_source_target_on_update
    AFTER UPDATE ON jobs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    WHEN (NEW.kb_source_id IS DISTINCT FROM OLD.kb_source_id)
    EXECUTE FUNCTION enforce_jobs_source_target();
