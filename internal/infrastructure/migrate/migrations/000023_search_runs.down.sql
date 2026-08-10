-- 000023_search_runs.down.sql
-- 按子表、主表逆序回滚检索运行快照表。

DROP INDEX IF EXISTS search_run_generations_lookup_idx;
DROP INDEX IF EXISTS search_runs_query_hash_idx;
DROP INDEX IF EXISTS search_runs_expiry_idx;

DROP TABLE IF EXISTS search_run_generations;
DROP TABLE IF EXISTS search_runs;
