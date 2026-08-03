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

func TestZhparserMigrationCreatesPublicConfigDespiteSameNameInAnotherSchema(t *testing.T) {
	ctx := context.Background()
	database, migrator := newZhparserMigrationTest(t)

	require.NoError(t, migrator.Migrate(10))
	_, err := database.ExecContext(ctx, `
		CREATE EXTENSION zhparser;
		CREATE SCHEMA alternate;
		CREATE TEXT SEARCH CONFIGURATION alternate.zhparser (PARSER = zhparser);
		ALTER TEXT SEARCH CONFIGURATION alternate.zhparser
			ADD MAPPING FOR n,v,a,i,e,l WITH simple;
	`)
	require.NoError(t, err)

	require.NoError(t, migrator.Migrate(11))

	var publicConfigCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pg_ts_config AS config
		JOIN pg_namespace AS namespace ON namespace.oid = config.cfgnamespace
		WHERE namespace.nspname = 'public' AND config.cfgname = 'zhparser'
	`).Scan(&publicConfigCount))
	require.Equal(t, 1, publicConfigCount)

	var tokens string
	require.NoError(t, database.QueryRowContext(
		ctx,
		"SELECT to_tsvector('public.zhparser', '人工智能驱动的知识管理系统')::text",
	).Scan(&tokens))
	require.Contains(t, tokens, "人工智能")
}

func TestZhparserMigrationDownKeepsConfigUsableForPersistedGenerationSnapshots(t *testing.T) {
	ctx := context.Background()
	database, migrator := newZhparserMigrationTest(t)

	require.NoError(t, migrator.Migrate(11))
	_, err := database.ExecContext(ctx, `
		CREATE TABLE generation_snapshot (retrieval_config jsonb NOT NULL);
		INSERT INTO generation_snapshot VALUES ('{"fts_config":"zhparser"}');
	`)
	require.NoError(t, err)

	require.NoError(t, migrator.Migrate(10))

	var query string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT plainto_tsquery(
			(retrieval_config->>'fts_config')::regconfig,
			'人工智能'
		)::text
		FROM generation_snapshot
	`).Scan(&query))
	require.Contains(t, query, "人工智能")
}

func newZhparserMigrationTest(t *testing.T) (*sql.DB, *migrate.Migrate) {
	t.Helper()
	database, err := sql.Open("postgres", testsupport.NewEmptyPostgres(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	source, err := iofs.New(migrationsFS, "migrations")
	require.NoError(t, err)
	driver, err := postgres.WithInstance(database, &postgres.Config{})
	require.NoError(t, err)
	migrator, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, databaseErr := migrator.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
	})
	return database, migrator
}
