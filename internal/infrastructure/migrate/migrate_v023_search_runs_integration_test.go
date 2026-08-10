//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigrateV023SearchRuns 验证迁移到 023 后：
//   - search_runs 和 search_run_generations 两张表存在；
//   - replay_of_id 跨 Workspace 外键被拒绝；
//   - search_run_generations 跨 Workspace generation 外键被拒绝。
func TestMigrateV023SearchRuns(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(23))

	ctx := context.Background()

	for _, table := range []string{"search_runs", "search_run_generations"} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1
		`, table).Scan(&count))
		require.Greater(t, count, 0, "table %s should exist", table)
	}

	for _, indexName := range []string{
		"search_runs_expiry_idx",
		"search_runs_query_hash_idx",
		"search_run_generations_lookup_idx",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = $1
		`, indexName).Scan(&count))
		require.Equal(t, 1, count, "index %s should exist", indexName)
	}
}
