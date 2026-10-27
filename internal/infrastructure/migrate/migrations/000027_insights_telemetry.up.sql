-- 000027_insights_telemetry: v1.4.0 检索遥测与知识健康回路。
-- 1) search_citations：chunk 级引用遥测（每次检索最终返回的结果行，含 rank）。
-- 2) search_query_log：opt-in 查询原文日志（仅 knowledge_bases.query_log_enabled=true
--    的库写入；search_runs 本体继续零明文，v0.9.0 隐私合同不变）。
-- 3) knowledge_bases.query_log_enabled：查询原文记录开关（默认关闭）。
--
-- 生命周期（spec §4.2/§4.3/D3）：两张遥测表都不建跨表级联 FK——引用历史必须活过
-- search_run 过期清理与文档清理（删除预警依赖"文档已删、引用历史还在"）；生命周期
-- 由 insights.citation_retention_days / query_log_retention_days 独立清理任务管理。
-- workspace 隔离靠 (workspace_id, kb_id) 列 + 查询侧显式过滤（RLS 就绪形态）。

-- (1) search_citations。主键沿用全库 uuid 惯例（SQLite 快照为 TEXT，bigint identity
-- 在跨方言快照模式下无对应惯例，回退决策记录于实现计划 T1）。
CREATE TABLE IF NOT EXISTS search_citations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    kb_id uuid NOT NULL,
    search_run_id uuid NOT NULL,
    document_id uuid NOT NULL,
    chunk_revision_id uuid NOT NULL,
    rank integer NOT NULL CHECK (rank >= 1),
    retrieval_status text NOT NULL CHECK (retrieval_status IN ('available', 'degraded')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (search_run_id, chunk_revision_id)
);

-- 文档引用计数/最近引用/删除预警（主查询路径）。
CREATE INDEX IF NOT EXISTS idx_citations_doc_window
    ON search_citations (workspace_id, kb_id, document_id, created_at DESC);

-- 回放/排查用。
CREATE INDEX IF NOT EXISTS idx_citations_run
    ON search_citations (search_run_id);

-- 保留期清理。
CREATE INDEX IF NOT EXISTS idx_citations_created
    ON search_citations (created_at);

-- (2) search_query_log。query_text 仅在开关开启时由投递侧构造写入（门控在构造时，
-- 非事后过滤）；任何日志/错误输出不得包含该列内容。
CREATE TABLE IF NOT EXISTS search_query_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    kb_id uuid NOT NULL,
    search_run_id uuid NOT NULL,
    query_text text NOT NULL CHECK (btrim(query_text) <> ''),
    query_hash text NOT NULL,
    query_chars integer NOT NULL CHECK (query_chars >= 0),
    retrieval_status text NOT NULL
        CHECK (retrieval_status IN ('running', 'available', 'empty', 'degraded', 'failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 零召回聚合（按 hash 关联 search_runs）与最近原文读取。
CREATE INDEX IF NOT EXISTS idx_query_log_kb_time
    ON search_query_log (workspace_id, kb_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_query_log_hash
    ON search_query_log (workspace_id, kb_id, query_hash, created_at DESC);

-- 保留期清理。
CREATE INDEX IF NOT EXISTS idx_query_log_created
    ON search_query_log (created_at);

-- (3) 查询原文开关：默认关闭；开启前的历史不回填。
ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS query_log_enabled boolean NOT NULL DEFAULT false;
