-- 000002_multi_tenant_auth: 多租户认证持久化。
-- 新增 users / sessions / workspace_memberships / workspace_invitations 四张表，
-- 并为已存在的 workspaces 表补 slug 列（可空 -> 回填 -> NOT NULL）。

CREATE TABLE users (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email             text NOT NULL UNIQUE,
    nickname          text NOT NULL,
    password_hash     text NOT NULL,
    is_platform_admin boolean NOT NULL DEFAULT false,
    last_login_at     timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    user_agent   text NOT NULL DEFAULT '',
    ip_addr      inet,
    revoked_at   timestamptz
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE workspace_memberships (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id),
    CONSTRAINT workspace_memberships_role_check CHECK (role IN ('owner','admin','member'))
);

CREATE INDEX idx_memberships_user_id ON workspace_memberships(user_id);
CREATE INDEX idx_memberships_workspace_id ON workspace_memberships(workspace_id);

CREATE TABLE workspace_invitations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    invited_email    text NOT NULL,
    role             text NOT NULL DEFAULT 'member',
    token_hash       text NOT NULL,
    token_prefix     text NOT NULL,
    expires_at       timestamptz NOT NULL,
    accepted_at      timestamptz,
    accepted_user_id uuid REFERENCES users(id),
    revoked_at       timestamptz,
    created_by       uuid NOT NULL REFERENCES users(id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, invited_email),
    CONSTRAINT workspace_invitations_role_check CHECK (role IN ('owner','admin','member'))
);

CREATE INDEX idx_invitations_token_hash ON workspace_invitations(token_hash);

-- workspaces.slug：先加可空列、回填、再设 NOT NULL，避免既有行违反约束。
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS slug text;
UPDATE workspaces SET slug = CASE
    WHEN id = '00000000-0000-0000-0000-000000000001'::uuid THEN 'default'
    ELSE 'legacy-' || id::text
END
WHERE slug IS NULL OR slug = '';
ALTER TABLE workspaces ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX idx_workspaces_slug ON workspaces(slug);
