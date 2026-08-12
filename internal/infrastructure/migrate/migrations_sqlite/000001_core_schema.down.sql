-- 反向回滚 core_schema：删除本切片建表的核心身份/工作空间/模型/API Token 表。
-- 注意：workspace_api_token_knowledge_bases 引用 knowledge_bases（knowledge 切片），
-- 但 SQLite DROP TABLE 不受 FK 引用计数影响，此处直接 DROP 即可。
DROP TABLE IF EXISTS workspace_api_token_knowledge_bases;
DROP TABLE IF EXISTS workspace_search_settings;
DROP TABLE IF EXISTS workspace_api_tokens;
DROP TABLE IF EXISTS external_identities;
DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS model_providers;
DROP TABLE IF EXISTS workspace_invitations;
DROP TABLE IF EXISTS workspace_memberships;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS workspaces;
