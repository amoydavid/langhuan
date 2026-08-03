-- 回滚 v0.6.0 API Key 重建：删除知识库绑定表与重建后的 token 表，
-- 再恢复 000001 的占位结构，使 down/up 可重复执行。
-- 占位表从未启用，恢复最小原始形状即可。

DROP TABLE IF EXISTS workspace_api_token_knowledge_bases;
DROP TABLE IF EXISTS workspace_api_tokens;

CREATE TABLE workspace_api_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_workspace_api_tokens_workspace_id
    ON workspace_api_tokens(workspace_id);
