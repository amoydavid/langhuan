//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestV017AllowsSourceSyncJobWithOnlyKnowledgeBase 验证迁移到 017 后，
// jobs_target_check 放宽，允许插入仅关联 knowledge_base 的 source_sync 任务。
func TestV017AllowsSourceSyncJobWithOnlyKnowledgeBase(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(17))

	workspaceID := "11111111-1111-4111-8111-111111111117"
	userID := "22222222-2222-4222-8222-222222222217"
	kbID := "33333333-3333-4333-8333-333333333337"
	rootID := "44444444-4444-4444-8444-444444444447"
	generationID := "55555555-5555-4555-8555-555555555557"
	modelID := "66666666-6666-4666-8666-666666666667"
	providerID := "77777777-7777-4777-8777-777777777777"

	tx, err := database.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(context.Background(), query, args...)
		require.NoError(t, execErr)
	}
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'test', 'test-v017')`, workspaceID)
	exec(`INSERT INTO users (id, email, nickname, password_hash) VALUES ($1, 'v017@example.com', 'v017', 'hash')`, userID)
	exec(`INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) VALUES ($1, 'workspace', $2, 'rerank', 'rerank_compatible', 'active', $3)`, providerID, workspaceID, userID)
	exec(`INSERT INTO models (id, provider_id, name, type, model_name, status, created_by) VALUES ($1, $2, 'rerank', 'embedding', 'bge', 'active', $3)`, modelID, providerID, userID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) VALUES ($1, $2, 'kb', $3, $4)`, kbID, workspaceID, generationID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
	exec(`INSERT INTO knowledge_base_index_generations (id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status) VALUES ($1, $2, $3, $4, $5, 'embed', 1024, 'ehash', 1, '{}'::jsonb, 'chash', 'building')`, generationID, workspaceID, kbID, modelID, providerID)
	require.NoError(t, tx.Commit())

	// source_sync 任务：三者皆 nil，应被接受。
	_, syncErr := database.ExecContext(context.Background(), `
		INSERT INTO jobs (workspace_id, knowledge_base_id, type, status)
		VALUES ($1, $2, 'source_sync', 'pending')
	`, workspaceID, kbID)
	require.NoError(t, syncErr, "source_sync job with only knowledge_base_id must be allowed after v017")

	// 非 source_sync 类型三者皆 nil，仍应被拒绝。
	_, otherErr := database.ExecContext(context.Background(), `
		INSERT INTO jobs (workspace_id, knowledge_base_id, type, status)
		VALUES ($1, $2, 'document_parse_start', 'pending')
	`, workspaceID, kbID)
	require.Error(t, otherErr, "non-source_sync job with all-nil targets must still be rejected")
}
