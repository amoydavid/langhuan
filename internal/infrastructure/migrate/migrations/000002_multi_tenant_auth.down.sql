-- 回滚 000002_multi_tenant_auth：仅逆转本版本新增的对象。
-- 删除 slug 唯一索引与列、再按依赖反序删除四张认证表。
-- 不触碰 000001_init 创建的表结构（workspaces 仅去掉 slug 列）。

DROP INDEX IF EXISTS idx_workspaces_slug;
ALTER TABLE workspaces DROP COLUMN IF EXISTS slug;

DROP TABLE IF EXISTS workspace_invitations;
DROP TABLE IF EXISTS workspace_memberships;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
