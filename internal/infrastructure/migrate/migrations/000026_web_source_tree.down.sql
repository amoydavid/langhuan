-- 000026_web_source_tree down：还原 file 节点 document_kind = 'file' 的 shape 约束。
-- 注意：若存在 document_kind='web' 的行，还原会失败（需先清理 web 页面节点）。

ALTER TABLE file_tree_nodes
    DROP CONSTRAINT IF EXISTS file_tree_nodes_shape_check;

ALTER TABLE file_tree_nodes
    ADD CONSTRAINT file_tree_nodes_shape_check CHECK (
        (node_type = 'root' AND parent_id IS NULL AND name = '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'folder' AND parent_id IS NOT NULL AND btrim(name) <> '' AND document_id IS NULL AND document_kind IS NULL)
        OR (node_type = 'file' AND parent_id IS NOT NULL AND btrim(name) <> '' AND document_id IS NOT NULL AND document_kind = 'file')
    );
