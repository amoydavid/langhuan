-- 反向回滚 retrieval_and_search_runs：删除本切片建表的 3 张检索/检索运行表。
DROP TABLE IF EXISTS search_run_generations;
DROP TABLE IF EXISTS search_runs;
DROP TABLE IF EXISTS retrieval_entries;
