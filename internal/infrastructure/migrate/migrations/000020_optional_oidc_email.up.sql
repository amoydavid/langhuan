-- 000020_optional_oidc_email: OIDC 用户允许无 email。
-- 背景：内部 IdP 出于隐私（email 视为敏感字段）可能不在 id_token/UserInfo
-- 返回 email claim。放宽 users.email 与 external_identities.email 约束，
-- 使 OIDC JIT 建号支持无 email 用户。
--
-- users.email 允许 NULL：PostgreSQL 的 UNIQUE 约束对 NULL 不生效，因此
-- 多个无 email 用户不会互相冲突；非空 email 仍保持全局唯一。
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

-- external_identities.email 允许 NULL，并移除强制非空的 CHECK 约束。
ALTER TABLE external_identities ALTER COLUMN email DROP NOT NULL;
ALTER TABLE external_identities DROP CONSTRAINT IF EXISTS external_identities_email_nonempty;
