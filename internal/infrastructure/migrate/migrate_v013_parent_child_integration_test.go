//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParentChildMigrationAddsChunkRoleAndLineageColumns(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(13))

	for _, column := range []string{"role", "parent_chunk_id"} {
		var count int
		require.NoError(t, database.QueryRowContext(context.Background(), `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'chunks' AND column_name = $1
		`, column).Scan(&count))
		require.Equal(t, 1, count, "column %s", column)
	}
}
