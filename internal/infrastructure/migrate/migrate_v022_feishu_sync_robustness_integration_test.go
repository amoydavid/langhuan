//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestV022AddsFeishuSyncRobustnessSchema 验证迁移到 022 后：
//   - documents.content_hash 与 file_tree_nodes.external_id 列存在；
//   - 新的唯一部分索引 uq_documents_workspace_kb_external /
//     uq_file_tree_nodes_kb_external 就位；
//   - jobs_target_check 放宽，允许仅关联知识库的 source_cleanup 任务。
func TestV022AddsFeishuSyncRobustnessSchema(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(22))

	for _, tc := range []struct{ table, column string }{
		{"documents", "content_hash"},
		{"file_tree_nodes", "external_id"},
	} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
            SELECT count(*) FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
        `, tc.table, tc.column).Scan(&count))
		require.Equal(t, 1, count, "%s.%s", tc.table, tc.column)
	}

	for _, indexName := range []string{
		"uq_documents_workspace_kb_external",
		"uq_file_tree_nodes_kb_external",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
            SELECT count(*) FROM pg_indexes
            WHERE schemaname = 'public' AND indexname = $1
        `, indexName).Scan(&count))
		require.Equal(t, 1, count, "index %s", indexName)
	}

	var targetCheck string
	require.NoError(t, database.QueryRowContext(context.Background(), `
        SELECT pg_get_constraintdef(oid)
        FROM pg_constraint
        WHERE conname = 'jobs_target_check'
    `).Scan(&targetCheck))
	require.Contains(t, targetCheck, "source_cleanup")

	workspaceID := "12222222-2222-4222-8222-222222222222"
	kbID := "13333333-3333-4333-8333-333333333333"
	rootID := "14444444-4444-4444-8444-444444444444"
	ctx := context.Background()
	// knowledge_bases_file_tree_root_fk 是 DEFERRABLE INITIALLY DEFERRED，
	// 需要包在事务里以便 root/kb 互相引用能在提交时校验。
	tx, txErr := database.BeginTx(ctx, nil)
	require.NoError(t, txErr)
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(ctx, query, args...)
		require.NoError(t, execErr, "source_cleanup KB-only job must satisfy jobs_target_check")
	}
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'v022-job-ws', 'v022-job-ws')`, workspaceID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, 'v022-job-kb', $3)`, kbID, workspaceID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
	exec(`INSERT INTO jobs (workspace_id, knowledge_base_id, type, status) VALUES ($1, $2, 'source_cleanup', 'pending')`, workspaceID, kbID)
	require.NoError(t, tx.Commit())
}

// TestV022RejectsDuplicateDocumentExternalID 验证迁移到 022 时，
// 若已存在同一 (workspace, knowledge_base) 下重复的 documents.external_id，
// 迁移必须失败且错误信息含 "duplicate"，绝不静默合并或删除行。
func TestV022RejectsDuplicateDocumentExternalID(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(21))
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(ctx, query, args...)
		require.NoError(t, execErr)
	}
	workspaceID := "32222222-2222-4222-8222-222222222222"
	kbID := "33333333-3333-4333-8333-333333333333"
	rootID := "34444444-4444-4444-8444-444444444444"
	firstDocID := "35555555-5555-4555-8555-555555555555"
	secondDocID := "36666666-6666-4666-8666-666666666666"
	firstNodeID := "37888888-8888-4888-8888-888888888881"
	secondNodeID := "37888888-8888-4888-8888-888888888882"
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'v022-ws', 'v022-ws')`, workspaceID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, 'v022-kb', $3)`, kbID, workspaceID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
	exec(`INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status, external_id) VALUES ($1, $2, $3, 'file', 'one', 'feishu', 'pending', 'dup-token')`, firstDocID, workspaceID, kbID)
	exec(`INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status, external_id) VALUES ($1, $2, $3, 'file', 'two', 'feishu', 'pending', 'dup-token')`, secondDocID, workspaceID, kbID)
	// 迁移 000005 的 enforce_file_document_node 约束触发器要求每个 file 文档恰好对应一个 file 节点，
	// 这里为两个文档各挂一个文件节点以满足既有 schema，使迁移 022 的重复 external_id 检测成为唯一失败点。
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind) VALUES ($1, $2, $3, $4, 'file', 'one.md', $5, 'file')`, firstNodeID, workspaceID, kbID, rootID, firstDocID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, document_id, document_kind) VALUES ($1, $2, $3, $4, 'file', 'two.md', $5, 'file')`, secondNodeID, workspaceID, kbID, rootID, secondDocID)
	require.NoError(t, tx.Commit())
	err = migrator.Migrate(22)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}

// TestV022RejectsDuplicateFolderExternalID 验证迁移到 022 时，
// 若已存在同一 (workspace, knowledge_base) 下重复的 file_tree_nodes.external_id，
// 迁移必须失败且错误信息含 "duplicate"，绝不静默合并或删除行。
func TestV022RejectsDuplicateFolderExternalID(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(21))
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(ctx, query, args...)
		require.NoError(t, execErr)
	}
	exec(`ALTER TABLE file_tree_nodes ADD COLUMN external_id text`)
	workspaceID := "42222222-2222-4222-8222-222222222222"
	kbID := "43333333-3333-4333-8333-333333333333"
	rootID := "44444444-4444-4444-8444-444444444444"
	folderOneID := "45555555-5555-4555-8555-555555555555"
	folderTwoID := "46666666-6666-4666-8666-666666666666"
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'v022-folder-ws', 'v022-folder-ws')`, workspaceID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, 'v022-folder-kb', $3)`, kbID, workspaceID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, external_id) VALUES ($1, $2, $3, $4, 'folder', 'one', 'dup-folder')`, folderOneID, workspaceID, kbID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, external_id) VALUES ($1, $2, $3, $4, 'folder', 'two', 'dup-folder')`, folderTwoID, workspaceID, kbID, rootID)
	require.NoError(t, tx.Commit())
	err = migrator.Migrate(22)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}
