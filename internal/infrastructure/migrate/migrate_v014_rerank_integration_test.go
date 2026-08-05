//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestV014AddsRerankSnapshotColumnsAndConstraints(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(14))

	for _, column := range []string{
		"rerank_model_id", "rerank_provider_id", "rerank_model_name",
		"rerank_model_config_hash", "rerank_config",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'knowledge_base_index_generations' AND column_name = $1
		`, column).Scan(&count))
		require.Equal(t, 1, count, "column %s", column)
	}

	// CHECK 约束应存在。
	var constraintCount int
	require.NoError(t, database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'knowledge_base_index_generations'::regclass
		AND conname IN ('index_generations_rerank_config_object_check', 'index_generations_rerank_snapshot_shape_check')
		AND contype = 'c'
	`).Scan(&constraintCount))
	require.Equal(t, 2, constraintCount, "rerank check constraints")

	// rerank_config 列应有默认值。
	var defaultEmpty int
	require.NoError(t, database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'knowledge_base_index_generations'
		AND column_name = 'rerank_config' AND column_default IS NOT NULL
	`).Scan(&defaultEmpty))
	require.Equal(t, 1, defaultEmpty, "rerank_config default")
}

// TestV014RejectsPartialRerankSnapshot 验证全空/全非空 CHECK 约束生效：
// 只写 rerank_model_id 而其它快照字段为空应被拒绝。
func TestV014RejectsPartialRerankSnapshot(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(14))

	workspaceID := "11111111-1111-4111-8111-111111111111"
	userID := "22222222-2222-4222-8222-222222222222"
	kbID := "33333333-3333-4333-8333-333333333333"
	rootID := "44444444-4444-4444-8444-444444444444"
	generationID := "55555555-5555-4555-8555-555555555555"
	modelID := "66666666-6666-4666-8666-666666666666"
	providerID := "77777777-7777-4777-8777-777777777777"

	// 循环外键（kb <-> generation, kb <-> file_tree_node）需要放在同一事务中，
	// 利用 DEFERRABLE INITIALLY DEFERRED 约束在 commit 时统一校验。
	tx, err := database.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(context.Background(), query, args...)
		require.NoError(t, execErr)
	}
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'test', 'test-v014')`, workspaceID)
	exec(`INSERT INTO users (id, email, nickname, password_hash) VALUES ($1, 'v014@example.com', 'v014', 'hash')`, userID)
	exec(`INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) VALUES ($1, 'workspace', $2, 'rerank', 'rerank_compatible', 'active', $3)`, providerID, workspaceID, userID)
	exec(`INSERT INTO models (id, provider_id, name, type, model_name, status, created_by) VALUES ($1, $2, 'rerank', 'rerank', 'bge', 'active', $3)`, modelID, providerID, userID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) VALUES ($1, $2, 'kb', $3, $4)`, kbID, workspaceID, generationID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
	exec(`INSERT INTO knowledge_base_index_generations (id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status) VALUES ($1, $2, $3, $4, $5, 'embed', 1024, 'ehash', 1, '{}'::jsonb, 'chash', 'building')`, generationID, workspaceID, kbID, modelID, providerID)
	require.NoError(t, tx.Commit())

	// 只写 rerank_model_id 应被 CHECK 拒绝（部分快照）。
	_, partialErr := database.ExecContext(context.Background(), `UPDATE knowledge_base_index_generations SET rerank_model_id = $1 WHERE id = $2`, modelID, generationID)
	require.Error(t, partialErr, "partial rerank snapshot must fail")

	// 完整快照应被接受（四列同时非空 + rerank_config 含 candidate_top_k/failure_mode）。
	_, completeErr := database.ExecContext(context.Background(), `UPDATE knowledge_base_index_generations SET rerank_model_id = $1, rerank_provider_id = $2, rerank_model_name = 'rerank', rerank_model_config_hash = 'rhash', rerank_config = jsonb_build_object('candidate_top_k', 50, 'failure_mode', 'fallback') WHERE id = $3`, modelID, providerID, generationID)
	require.NoError(t, completeErr, "complete rerank snapshot must succeed")
}
