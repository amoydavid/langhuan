-- 回滚 000001_init：按依赖反序删除表。
-- 不删除 vector / pgcrypto 扩展：它们可能被库内其他对象或迁移共享依赖，
-- 在共享 PostgreSQL 实例上 DROP EXTENSION 风险过高，由运维按需手动清理。

DROP TABLE IF EXISTS chunk_keywords;
DROP TABLE IF EXISTS chunk_embeddings;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS document_assets;
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS knowledge_bases;
DROP TABLE IF EXISTS workspace_api_tokens;
DROP TABLE IF EXISTS workspaces;
