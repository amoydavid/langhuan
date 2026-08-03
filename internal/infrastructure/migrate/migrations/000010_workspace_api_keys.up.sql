-- v0.6.0: 重建 Workspace API Key 表，支持 scope、可恢复密文、知识库绑定、到期与吊销。
-- 旧的占位表 workspace_api_tokens 从未正式启用（无 Repository/服务/鉴权），
-- 因此安全地 DROP 并按 v0.6.0 合同重建。

DROP TABLE IF EXISTS workspace_api_token_knowledge_bases;
DROP TABLE IF EXISTS workspace_api_tokens;

CREATE TABLE workspace_api_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_secret_ciphertext bytea NOT NULL,
    token_prefix text NOT NULL,
    scopes text[] NOT NULL,
    expires_at timestamptz,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    revoked_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id),
    CONSTRAINT workspace_api_tokens_name_check
        CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    CONSTRAINT workspace_api_tokens_hash_check
        CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT workspace_api_tokens_prefix_check
        CHECK (token_prefix ~ '^lhk_[A-Za-z0-9_-]{8}$'),
    CONSTRAINT workspace_api_tokens_scopes_check
        CHECK (
            cardinality(scopes) > 0 AND
            scopes <@ ARRAY[
                'knowledge_bases:write',
                'documents:read',
                'documents:write',
                'search:read'
            ]::text[]
        ),
    CONSTRAINT workspace_api_tokens_expiry_check
        CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE UNIQUE INDEX idx_workspace_api_tokens_token_hash
    ON workspace_api_tokens(token_hash);
CREATE INDEX workspace_api_tokens_workspace_created_idx
    ON workspace_api_tokens(workspace_id, created_at DESC, id DESC);

CREATE TABLE workspace_api_token_knowledge_bases (
    api_token_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (api_token_id, knowledge_base_id),
    CONSTRAINT workspace_api_token_kbs_token_fk
        FOREIGN KEY (api_token_id, workspace_id)
        REFERENCES workspace_api_tokens(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT workspace_api_token_kbs_knowledge_base_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX workspace_api_token_kbs_workspace_kb_idx
    ON workspace_api_token_knowledge_bases(workspace_id, knowledge_base_id, api_token_id);
