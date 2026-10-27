-- 反向回滚 retrieval_and_search_runs：删除本切片建表的检索/检索运行/遥测表。
DROP TABLE IF EXISTS search_query_log;
DROP TABLE IF EXISTS search_citations;
DROP TABLE IF EXISTS search_run_generations;
DROP TABLE IF EXISTS search_runs;
DROP TABLE IF EXISTS retrieval_entries;
