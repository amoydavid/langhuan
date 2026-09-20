//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestV026FileTreeShapeCheckAllowsWebNodes 验证 000026 的 shape_check 放宽 round-trip：
// up 后 file 节点可挂载 document_kind='web'（web 页面进 KB 文件树，spec §3.3 目录投影），
// down 还原 `document_kind = 'file'` 后 web 节点重新被拒（web 节点须先清理，见 down 注释），
// 可重新 up；file 节点不受影响。
func TestV026FileTreeShapeCheckAllowsWebNodes(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(26))
	// newV025World 是通用最小夹具（workspace + KB + root），v026 复用。
	w := newV025World(t, database, "v026-shape")
	ctx := context.Background()

	sourceID := w.createSource(t, "docs-site")
	docID := w.createWebDocument(t, sourceID, "https://docs.example.com/getting-started")

	// insertFileNode 直插一个 file 节点。
	insertFileNode := func(name, documentID, documentKind string) error {
		_, err := database.ExecContext(ctx, `
			INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind)
			VALUES ($1, $2, $3, $4, 'file', $5, $6, $7)
		`, uuid.NewString(), w.workspaceID, w.kbID, w.rootID, name, documentID, documentKind)
		return err
	}
	// insertFileDocumentWithNode 在单事务内成对写入 file 文档与节点
	//（file_document_node 延迟触发器在提交时校验「恰一个节点」）。
	insertFileDocumentWithNode := func(name string) {
		docID := uuid.NewString()
		nodeID := uuid.NewString()
		tx, err := database.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status)
			VALUES ($1, $2, $3, 'file', $4, 'upload', 'pending')
		`, docID, w.workspaceID, w.kbID, name)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind)
			VALUES ($1, $2, $3, $4, 'file', $5, $6, 'file')
		`, nodeID, w.workspaceID, w.kbID, w.rootID, name, docID)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}

	// up：file 节点挂载 web 文档被放行（修复前被 file_tree_nodes_shape_check 拒绝）。
	require.NoError(t, insertFileNode("getting-started", docID, "web"))

	// up：file 节点挂载 file 文档不受影响（既有语义回归）。
	insertFileDocumentWithNode("guide.pdf")

	// down：shape_check 还原为 document_kind = 'file'——web 节点须先清理（down 注释契约），
	// 还原后 web 节点重新被拒，file 节点仍放行。
	_, err := database.ExecContext(ctx, `DELETE FROM file_tree_nodes WHERE document_id = $1`, docID)
	require.NoError(t, err)
	require.NoError(t, migrator.Migrate(25))

	var shapeCheck string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'file_tree_nodes_shape_check'
	`).Scan(&shapeCheck))
	require.Contains(t, shapeCheck, "document_kind = 'file'")
	require.NotContains(t, shapeCheck, "'file', 'web'")

	err = insertFileNode("another-web-page", docID, "web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "file_tree_nodes_shape_check")

	insertFileDocumentWithNode("another-file.pdf")

	// 重新 up：web 节点恢复放行（round-trip 闭环）。
	require.NoError(t, migrator.Migrate(26))
	require.NoError(t, insertFileNode("getting-started", docID, "web"))
}
