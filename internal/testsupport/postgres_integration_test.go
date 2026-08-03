//go:build integration

package testsupport_test

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"testing"

	"github.com/lib/pq"

	"github.com/dajee/langhuan/internal/testsupport"
)

func TestNewEmptyPostgresUsesSameServerAndIndependentDatabases(t *testing.T) {
	ctx := context.Background()
	firstDSN := testsupport.NewEmptyPostgres(t)
	secondDSN := testsupport.NewEmptyPostgres(t)
	firstURL, err := url.Parse(firstDSN)
	if err != nil {
		t.Fatal(err)
	}
	secondURL, err := url.Parse(secondDSN)
	if err != nil {
		t.Fatal(err)
	}
	if firstURL.Host != secondURL.Host {
		t.Fatalf("database hosts differ: %q != %q", firstURL.Host, secondURL.Host)
	}
	if firstURL.Path == secondURL.Path {
		t.Fatalf("database names are equal: %q", firstURL.Path)
	}

	first := openPostgres(t, firstDSN)
	second := openPostgres(t, secondDSN)

	if _, err := first.ExecContext(ctx, "CREATE TABLE isolation_probe (id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ExecContext(ctx, "SELECT id FROM isolation_probe"); err == nil {
		t.Fatal("第二个隔离数据库能看到第一个数据库的表")
	} else {
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "42P01" {
			t.Fatalf("第二个数据库查询错误 = %v, 期望 undefined_table(42P01)", err)
		}
	}
}

func TestMigratedAndEmptyPostgresHaveExpectedSchemas(t *testing.T) {
	ctx := context.Background()
	migrated := openPostgres(t, testsupport.NewMigratedPostgres(t))
	empty := openPostgres(t, testsupport.NewEmptyPostgres(t))

	var migratedWorkspaceTable, emptyWorkspaceTable bool
	if err := migrated.QueryRowContext(ctx, "SELECT to_regclass('workspaces') IS NOT NULL").Scan(&migratedWorkspaceTable); err != nil {
		t.Fatal(err)
	}
	if err := empty.QueryRowContext(ctx, "SELECT to_regclass('workspaces') IS NOT NULL").Scan(&emptyWorkspaceTable); err != nil {
		t.Fatal(err)
	}
	if !migratedWorkspaceTable || emptyWorkspaceTable {
		t.Fatalf("workspaces table: migrated=%t empty=%t", migratedWorkspaceTable, emptyWorkspaceTable)
	}
}

func openPostgres(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
