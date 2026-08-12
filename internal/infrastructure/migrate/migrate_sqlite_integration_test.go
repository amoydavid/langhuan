//go:build integration

package migrate_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	_ "modernc.org/sqlite"
)

func TestRunSQLiteAppliesPlaceholderMigration(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "m.db") + "?cache=shared"
	err := migrate.Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("SELECT version FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("查询 schema_migrations 失败: %v", err)
	}
	if version != 3 {
		t.Fatalf("schema_migrations version = %d, want 3", version)
	}

	// 重复 up 应幂等（ErrNoChange）
	if err := migrate.Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn}); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}
}

func TestRunSQLiteRejectsUnknownDriver(t *testing.T) {
	err := migrate.Run(context.Background(), config.DatabaseConfig{Driver: "mysql", DSN: "x"})
	if err == nil {
		t.Fatal("未知 driver 应报错")
	}
}
