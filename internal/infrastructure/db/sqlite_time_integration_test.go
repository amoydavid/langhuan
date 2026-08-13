//go:build integration

package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	"github.com/dajee/langhuan/internal/testsupport"
)

// TestSQLiteOpenScansTimeColumns 验证生产 DSN（db.Open 注入的 pragma）下，
// 时间列能正确 Scan 为 time.Time（修复 _time_format=sqlite 导致的字符串返回）。
func TestSQLiteOpenScansTimeColumns(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "time.db") + "?cache=shared"
	cfg := config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true}
	if err := migrate.Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	gormDB, dialect, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer func() { c, _ := gormDB.DB(); c.Close() }()
	if dialect != db.DialectSQLite {
		t.Fatalf("dialect = %v", dialect)
	}

	// 插入 workspace（created_at/updated_at 用默认 strftime）
	wsID := "00000000-0000-0000-0000-000000000001"
	if err := gormDB.Exec(
		"INSERT INTO workspaces (id, name, slug) VALUES (?, ?, ?)", wsID, "ws", "ws-slug",
	).Error; err != nil {
		t.Fatalf("插入 workspace 失败: %v", err)
	}

	// 用 GORM 查询到含 time.Time 字段的 Row，验证时间扫描不报错
	type wsRow struct {
		ID        string    `gorm:"column:id"`
		Name      string    `gorm:"column:name"`
		CreatedAt time.Time `gorm:"column:created_at"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	var row wsRow
	if err := gormDB.Raw("SELECT id, name, created_at, updated_at FROM workspaces WHERE id = ?", wsID).Scan(&row).Error; err != nil {
		t.Fatalf("扫描时间列失败（_time_format 修复未生效？）: %v", err)
	}
	if row.Name != "ws" {
		t.Fatalf("name = %q", row.Name)
	}
	if row.CreatedAt.IsZero() {
		t.Fatal("created_at 不应为零值")
	}
	// created_at 应为近期时间（默认 now）
	if time.Since(row.CreatedAt) > 5*time.Minute {
		t.Fatalf("created_at = %v，非近期", row.CreatedAt)
	}
}

// 编译期引用 testsupport 防止未用 import（后续测试可能用到 SQLiteTestDSN）。
var _ = testsupport.SQLiteTestDSN
