//go:build integration

package migrate

import (
	"context"
	"database/sql"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/testsupport"
)

// TestWorkspaceAPIKeyMigrationUpDownUp 验证 v0.6.0 API Key 迁移可重复执行，
// 且 up 后表结构与 down 后占位表形状一致。
func TestWorkspaceAPIKeyMigrationUpDownUp(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("postgres", testsupport.NewEmptyPostgres(t))
	require.NoError(t, err)

	source, err := iofs.New(migrationsFS, "migrations")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	require.NoError(t, err)
	defer migrator.Close()

	// 先到 9（000010 之前）。
	require.NoError(t, migrator.Migrate(9))
	// up 到 10：表已重建。
	require.NoError(t, migrator.Migrate(10))
	assertWorkspaceAPIKeyV010Columns(t, ctx, database, true)

	// down 回 9：恢复占位表。
	require.NoError(t, migrator.Migrate(9))
	assertWorkspaceAPIKeyV010Columns(t, ctx, database, false)

	// 再次 up，证明 down/up 幂等。
	require.NoError(t, migrator.Migrate(10))
	assertWorkspaceAPIKeyV010Columns(t, ctx, database, true)
}

// assertWorkspaceAPIKeyV010Columns 校验 v0.6.0 列存在性。
func assertWorkspaceAPIKeyV010Columns(t *testing.T, ctx context.Context, db *sql.DB, wantV010 bool) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'workspace_api_tokens'
		ORDER BY column_name
	`)
	require.NoError(t, err)
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		cols[name] = true
	}
	require.NoError(t, rows.Err())

	if wantV010 {
		for _, c := range []string{"scopes", "token_secret_ciphertext", "expires_at", "created_by", "revoked_by", "updated_at"} {
			if !cols[c] {
				t.Fatalf("v0.6.0 API Key 表缺少列 %q", c)
			}
		}
		// 绑定表应存在。
		var bindCount int
		require.NoError(t, db.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.tables WHERE table_name = 'workspace_api_token_knowledge_bases'
		`).Scan(&bindCount))
		if bindCount != 1 {
			t.Fatal("绑定表 workspace_api_token_knowledge_bases 应在 v0.6.0 存在")
		}
	} else {
		// 占位表不应有 v0.6.0 新列。
		for _, c := range []string{"scopes", "token_secret_ciphertext", "expires_at"} {
			if cols[c] {
				t.Fatalf("占位 API Key 表不应包含 v0.6.0 列 %q", c)
			}
		}
	}
}
