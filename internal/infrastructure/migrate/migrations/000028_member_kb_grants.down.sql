-- 000028_member_kb_grants 回滚：删除绑定表与 kb_scope 列。

DROP TABLE IF EXISTS workspace_member_knowledge_bases;

ALTER TABLE workspace_memberships
    DROP CONSTRAINT IF EXISTS workspace_memberships_id_workspace_id_key;

ALTER TABLE workspace_memberships
    DROP CONSTRAINT IF EXISTS workspace_memberships_kb_scope_check;

ALTER TABLE workspace_memberships
    DROP COLUMN IF EXISTS kb_scope;
