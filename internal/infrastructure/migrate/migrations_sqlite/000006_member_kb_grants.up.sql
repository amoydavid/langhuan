-- 000006_member_kb_grants: 成员级知识库授权（对齐 PG 000028 终态语义）。
-- workspace_memberships 增加 kb_scope（all | restricted，默认 all 保持存量行为），
-- 新增 workspace_member_knowledge_bases 绑定表存储受限 member 的显式授权集合。

ALTER TABLE workspace_memberships
    ADD COLUMN kb_scope TEXT NOT NULL DEFAULT 'all'
    CHECK (kb_scope IN ('all','restricted'));

-- 绑定表复合 FK 的引用目标（SQLite 无 ADD CONSTRAINT，用 UNIQUE INDEX 表达；
-- 语义对齐 PG 000028 的 UNIQUE (id, workspace_id)）。
CREATE UNIQUE INDEX uq_memberships_id_workspace_id
    ON workspace_memberships(id, workspace_id);

CREATE TABLE workspace_member_knowledge_bases (
    membership_id     TEXT NOT NULL,
    workspace_id      TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (membership_id, knowledge_base_id),
    FOREIGN KEY (membership_id, workspace_id)
        REFERENCES workspace_memberships(id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX member_kbs_workspace_kb_idx
    ON workspace_member_knowledge_bases(workspace_id, knowledge_base_id, membership_id);
