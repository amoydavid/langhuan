-- 回滚：恢复 email 必填约束。
-- 注意：若已存在无 email 用户，回滚会失败，需先清理或回填。
ALTER TABLE external_identities
    ADD CONSTRAINT external_identities_email_nonempty CHECK (btrim(email) <> ''),
    ALTER COLUMN email SET NOT NULL;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
