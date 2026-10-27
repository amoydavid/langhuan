-- 000027_insights_telemetry down：逆序还原（删两遥测表、删开关列）。
-- 遥测数据无外键依赖，直接删除即可。

DROP TABLE IF EXISTS search_query_log;
DROP TABLE IF EXISTS search_citations;

ALTER TABLE knowledge_bases
    DROP COLUMN IF EXISTS query_log_enabled;
