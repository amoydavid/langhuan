-- 000006_member_kb_grants 回滚。

DROP TABLE IF EXISTS workspace_member_knowledge_bases;

DROP INDEX IF EXISTS uq_memberships_id_workspace_id;

ALTER TABLE workspace_memberships DROP COLUMN kb_scope;
