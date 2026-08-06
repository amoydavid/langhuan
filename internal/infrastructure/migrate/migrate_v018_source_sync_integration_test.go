//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestV018AddsSourceSyncSchema 验证迁移到 018 后，多应用连接表与来源字段就位。
func TestV018AddsSourceSyncSchema(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(18))

	// workspace_source_connections 表与列。
	for _, column := range []string{
		"id", "workspace_id", "provider", "name", "config",
		"credentials_ciphertext", "status", "created_at", "updated_at", "deleted_at",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'workspace_source_connections' AND column_name = $1
		`, column).Scan(&count))
		require.Equal(t, 1, count, "column %s", column)
	}

	// knowledge_bases 新增来源字段。
	for _, column := range []string{"source_type", "source_config", "source_connection_id"} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'knowledge_bases' AND column_name = $1
		`, column).Scan(&count))
		require.Equal(t, 1, count, "column %s", column)
	}

	// documents.external_id 与 jobs.source_connection_id。
	for _, tc := range []struct{ table, column string }{
		{"documents", "external_id"},
		{"jobs", "source_connection_id"},
	} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		`, tc.table, tc.column).Scan(&count))
		require.Equal(t, 1, count, "%s.%s", tc.table, tc.column)
	}

	// CHECK 约束。
	var constraintCount int
	require.NoError(t, database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pg_constraint
		WHERE conname IN (
            'workspace_source_connections_provider_check',
            'workspace_source_connections_status_check',
            'knowledge_bases_source_type_check'
        ) AND contype = 'c'
	`).Scan(&constraintCount))
	require.Equal(t, 3, constraintCount, "source sync check constraints")

	// 默认 upload：既有 KB 不带来源信息时 source_type 默认为 upload。
	workspaceID := "1aaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	kbID := "1bbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rootID := "1ccccccc-cccc-4ccc-8ccc-cccccccccccc"
	generationID := "1ddddddd-dddd-4ddd-8ddd-dddddddddddd"
	modelID := "1eeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	providerID := "1fffffff-ffff-4fff-8fff-ffffffffffff"
	userID := "12222222-2222-4222-8222-222222222222"

	tx, err := database.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	exec := func(query string, args ...any) {
		_, e := tx.ExecContext(context.Background(), query, args...)
		require.NoError(t, e)
	}
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'ws-v018', 'ws-v018')`, workspaceID)
	exec(`INSERT INTO users (id, email, nickname, password_hash) VALUES ($1, 'v018@example.com', 'v018', 'hash')`, userID)
	exec(`INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) VALUES ($1, 'workspace', $2, 'embed', 'openai', 'active', $3)`, providerID, workspaceID, userID)
	exec(`INSERT INTO models (id, provider_id, name, type, model_name, status, created_by) VALUES ($1, $2, 'embed', 'embedding', 'bge', 'active', $3)`, modelID, providerID, userID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) VALUES ($1, $2, 'kb', $3, $4)`, kbID, workspaceID, generationID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
	exec(`INSERT INTO knowledge_base_index_generations (id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status) VALUES ($1, $2, $3, $4, $5, 'embed', 1024, 'ehash', 1, '{}'::jsonb, 'chash', 'building')`, generationID, workspaceID, kbID, modelID, providerID)
	require.NoError(t, tx.Commit())

	var sourceType string
	require.NoError(t, database.QueryRowContext(context.Background(),
		`SELECT source_type FROM knowledge_bases WHERE id = $1`, kbID).Scan(&sourceType))
	require.Equal(t, "upload", sourceType, "default source_type should be upload")
}
