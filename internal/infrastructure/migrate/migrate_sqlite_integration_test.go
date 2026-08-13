//go:build integration

package migrate_test

import (
	"context"
	"database/sql"
	"errors"
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
	if version != 5 {
		t.Fatalf("schema_migrations version = %d, want 5", version)
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

// TestRunSQLiteDownRollsBackToZero 验证 SQLite 迁移 DOWN 能完整回滚到版本 0，
// 回滚后业务表（workspaces/retrieval_entries）已删除。
func TestRunSQLiteDownRollsBackToZero(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "m.db") + "?cache=shared"
	cfg := config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true}

	// 1. up 到最新
	if err := migrate.Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run up 失败: %v", err)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 2. 验证 up 后业务表存在
	for _, table := range []string{"workspaces", "retrieval_entries"} {
		if !sqliteTableExists(t, db, table) {
			t.Fatalf("up 后表 %s 应存在", table)
		}
	}

	// 3. down 到 0
	if err := migrate.RunDown(context.Background(), cfg); err != nil {
		t.Fatalf("RunDown 失败: %v", err)
	}

	// 4. 验证 down 后业务表已删除
	for _, table := range []string{"workspaces", "retrieval_entries"} {
		if sqliteTableExists(t, db, table) {
			t.Fatalf("down 后表 %s 应已删除", table)
		}
	}

	// 5. schema_migrations 回到版本 0（golang-migrate 可能让该表无行或值为 0）
	var version int
	err = db.QueryRow("SELECT version FROM schema_migrations").Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		// 无行视为版本 0（完全回滚）
	} else if err != nil {
		// schema_migrations 表本身可能被 driver 删除，也视为完全回滚
		if !sqliteTableExists(t, db, "schema_migrations") {
			// 表已删除，正常
		} else {
			t.Fatalf("查询 schema_migrations 失败: %v", err)
		}
	} else if version != 0 {
		t.Fatalf("down 后 version = %d, want 0", version)
	}

	// 6. 重复 down 应幂等（ErrNoChange）
	if err := migrate.RunDown(context.Background(), cfg); err != nil {
		t.Fatalf("重复 RunDown 失败: %v", err)
	}
}

// sqliteTableExists 检查 SQLite master 表中是否存在指定业务表。
func sqliteTableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatalf("查询表 %s 是否存在失败: %v", name, err)
	}
	return true
}
