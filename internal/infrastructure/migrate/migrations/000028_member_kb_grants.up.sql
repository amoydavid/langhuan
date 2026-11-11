-- 000028_member_kb_grants: 成员级知识库授权（KB 级可见性限制）。
-- workspace_memberships 增加 kb_scope（all | restricted，默认 all 保持存量行为），
-- 新增 workspace_member_knowledge_bases 绑定表存储受限 member 的显式授权集合。
-- 约束仅对 member 角色生效（admin/owner 隐含全量，由应用层 ResourceAccess 裁决）。

ALTER TABLE workspace_memberships
    ADD COLUMN kb_scope text NOT NULL DEFAULT 'all',
    ADD CONSTRAINT workspace_memberships_kb_scope_check
        CHECK (kb_scope IN ('all','restricted'));

-- 绑定表复合 FK 的引用目标（对齐 000010 workspace_api_tokens 的 (id, workspace_id) 模式）。
ALTER TABLE workspace_memberships
    ADD CONSTRAINT workspace_memberships_id_workspace_id_key UNIQUE (id, workspace_id);

CREATE TABLE workspace_member_knowledge_bases (
    membership_id     uuid NOT NULL,
    workspace_id      uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (membership_id, knowledge_base_id),
    CONSTRAINT member_kbs_membership_fk
        FOREIGN KEY (membership_id, workspace_id)
        REFERENCES workspace_memberships(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT member_kbs_knowledge_base_fk
        FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX member_kbs_workspace_kb_idx
    ON workspace_member_knowledge_bases(workspace_id, knowledge_base_id, membership_id);
