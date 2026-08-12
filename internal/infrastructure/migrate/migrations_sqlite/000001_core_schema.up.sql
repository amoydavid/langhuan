-- workspaces：工作空间/租户（保留 000001+000002.slug）
CREATE TABLE workspaces (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    metadata   TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metadata) = 'object'),
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_workspaces_name ON workspaces(name);
CREATE UNIQUE INDEX idx_workspaces_slug ON workspaces(slug);

-- users：平台用户（email 经 000020 放宽为可空，非空仍唯一）
CREATE TABLE users (
    id                TEXT PRIMARY KEY,
    email             TEXT UNIQUE,                       -- 允许多个 NULL
    nickname          TEXT NOT NULL,
    password_hash     TEXT NOT NULL,
    is_platform_admin INTEGER NOT NULL DEFAULT 0,         -- boolean
    last_login_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- sessions：登录会话（ip_addr inet→TEXT）
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_seen_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    user_agent   TEXT NOT NULL DEFAULT '',
    ip_addr      TEXT,
    revoked_at DATETIME
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- workspace_memberships：成员与角色
CREATE TABLE workspace_memberships (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, user_id)
);
CREATE INDEX idx_memberships_user_id ON workspace_memberships(user_id);
CREATE INDEX idx_memberships_workspace_id ON workspace_memberships(workspace_id);

-- workspace_invitations：邀请令牌
CREATE TABLE workspace_invitations (
    id               TEXT PRIMARY KEY,
    workspace_id     TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    invited_email    TEXT NOT NULL,
    role             TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
    token_hash       TEXT NOT NULL,
    token_prefix     TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    accepted_at DATETIME,
    accepted_user_id TEXT REFERENCES users(id),
    revoked_at DATETIME,
    created_by       TEXT NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (workspace_id, invited_email)
);
CREATE INDEX idx_invitations_token_hash ON workspace_invitations(token_hash);

-- model_providers：模型 Provider（credentials_ciphertext bytea→BLOB）
CREATE TABLE model_providers (
    id                     TEXT PRIMARY KEY,
    scope                  TEXT NOT NULL CHECK (scope IN ('platform','workspace')),
    workspace_id           TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
    name                   TEXT NOT NULL,
    display_name           TEXT NOT NULL DEFAULT '',
    description            TEXT NOT NULL DEFAULT '',
    provider               TEXT NOT NULL,
    config                 TEXT NOT NULL DEFAULT '{}' CHECK (json_type(config) = 'object'),
    credentials_ciphertext BLOB,
    status                 TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_by             TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (
        (scope = 'platform' AND workspace_id IS NULL)
        OR (scope = 'workspace' AND workspace_id IS NOT NULL)
    )
);
CREATE UNIQUE INDEX uq_model_providers_platform_name
    ON model_providers (lower(name)) WHERE scope = 'platform';
CREATE UNIQUE INDEX uq_model_providers_workspace_name
    ON model_providers (workspace_id, lower(name)) WHERE scope = 'workspace';
CREATE INDEX idx_model_providers_workspace_visibility
    ON model_providers (workspace_id, status, provider);
CREATE INDEX idx_model_providers_platform_visibility
    ON model_providers (status, provider) WHERE scope = 'platform';

-- models：模型注册
CREATE TABLE models (
    id           TEXT PRIMARY KEY,
    provider_id  TEXT NOT NULL REFERENCES model_providers(id) ON DELETE RESTRICT,
    name         TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL CHECK (type IN ('embedding','llm','rerank')),
    model_name   TEXT NOT NULL,
    dimensions   INTEGER,
    parameters   TEXT NOT NULL DEFAULT '{}' CHECK (json_type(parameters) = 'object'),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    created_by   TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (
        (type = 'embedding' AND dimensions IN (798,1024,2048,3584))
        OR (type IN ('llm','rerank') AND dimensions IS NULL)
    )
);
CREATE UNIQUE INDEX uq_models_provider_type_name
    ON models (provider_id, type, lower(name));
CREATE INDEX idx_models_provider_type_status
    ON models (provider_id, type, status);

-- external_identities：OIDC 身份绑定（email 经 000020 可空）
CREATE TABLE external_identities (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issuer         TEXT NOT NULL CHECK (trim(issuer) <> ''),
    subject        TEXT NOT NULL CHECK (trim(subject) <> ''),
    email          TEXT,
    email_verified INTEGER NOT NULL DEFAULT 0,
    raw_profile    TEXT NOT NULL DEFAULT '{}' CHECK (json_type(raw_profile) = 'object'),
    last_auth_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (issuer, subject)
);
CREATE INDEX idx_external_identities_user_id ON external_identities(user_id);

-- workspace_api_tokens：API Key（scopes text[]→TEXT，数组 CHECK 移除）
CREATE TABLE workspace_api_tokens (
    id                         TEXT PRIMARY KEY,
    workspace_id               TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                       TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    token_hash                 TEXT NOT NULL,
    token_secret_ciphertext    BLOB NOT NULL,
    token_prefix               TEXT NOT NULL,
    scopes                     TEXT NOT NULL,                 -- JSON 数组串，枚举校验下沉应用层
    expires_at DATETIME,
    last_used_at DATETIME,
    revoked_at DATETIME,
    created_by                 TEXT REFERENCES users(id) ON DELETE SET NULL,
    revoked_by                 TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (id, workspace_id),
    -- 原 token_hash/token_prefix 正则 CHECK 移除（见跳过清单）；expiry CHECK 保留：
    CHECK (expires_at IS NULL OR expires_at > created_at)
);
CREATE UNIQUE INDEX idx_workspace_api_tokens_token_hash
    ON workspace_api_tokens(token_hash);
CREATE INDEX workspace_api_tokens_workspace_created_idx
    ON workspace_api_tokens(workspace_id, created_at DESC, id DESC);

-- workspace_api_token_knowledge_bases：API Key ↔ 知识库
-- 注意：引用 knowledge_bases，需在 knowledge 切片之后或依赖 SQLite 前向引用。
CREATE TABLE workspace_api_token_knowledge_bases (
    api_token_id      TEXT NOT NULL,
    workspace_id      TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (api_token_id, knowledge_base_id),
    FOREIGN KEY (api_token_id, workspace_id)
        REFERENCES workspace_api_tokens(id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX workspace_api_token_kbs_workspace_kb_idx
    ON workspace_api_token_knowledge_bases(workspace_id, knowledge_base_id, api_token_id);

-- workspace_search_settings：Workspace 级检索/Rerank 默认（rerank shape CHECK 移除）
CREATE TABLE workspace_search_settings (
    workspace_id            TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    rerank_model_id         TEXT REFERENCES models(id) ON DELETE RESTRICT,
    rerank_provider_id      TEXT REFERENCES model_providers(id) ON DELETE RESTRICT,
    rerank_model_name       TEXT,
    rerank_model_config_hash TEXT,
    rerank_config           TEXT NOT NULL DEFAULT '{}' CHECK (json_type(rerank_config) = 'object'),
    updated_by              TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_workspace_search_settings_rerank_model
    ON workspace_search_settings (rerank_model_id) WHERE rerank_model_id IS NOT NULL;
CREATE INDEX idx_workspace_search_settings_rerank_provider
    ON workspace_search_settings (rerank_provider_id) WHERE rerank_provider_id IS NOT NULL;
