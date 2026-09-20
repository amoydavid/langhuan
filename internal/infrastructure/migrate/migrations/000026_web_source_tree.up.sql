-- 000026_web_source_tree: file_tree_nodes 允许挂载 web 页面文档（v1.3.0）。
-- 背景：web 内容源的页面按 spec §3.3 目录投影进 KB 文件树，file 节点的
-- document_kind 是 documents(ws,kb,id,kind) 复合外键的一部分；原 shape_check
-- 将 document_kind 硬编码为 'file'，web 文档（kind='web'）无法建节点。
-- 本迁移把 file 节点的 document_kind 放宽为 file|web（幂等：仅替换约束定义）。

ALTER TABLE file_tree_nodes
    DROP CONSTRAINT IF EXISTS file_tree_nodes_shape_check;

ALTER TABLE file_tree_nodes
    ADD CONSTRAINT file_tree_nodes_shape_check CHECK (
        (node_type = 'root' AND parent_id IS NULL AND name = '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'folder' AND parent_id IS NOT NULL AND btrim(name) <> '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'file' AND parent_id IS NOT NULL AND btrim(name) <> '' AND document_id IS NOT NULL AND document_kind IN ('file', 'web'))
    );
