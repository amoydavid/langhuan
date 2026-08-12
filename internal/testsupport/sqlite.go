//go:build integration

// Package testsupport — SQLite 测试 helper。
//
// 只提供临时路径，不 import 业务包（migrate/db），避免与业务包测试形成 import cycle。
// 调用方自行 migrate.Run + db.Open 组合。
//
// 数据库隔离铁律（AGENTS 5.10）：SQLite 测试只用 t.TempDir() 临时路径，
// 严禁连 config.yaml 的库或 ~/.langhuan-data。
package testsupport

import (
	"path/filepath"
	"testing"
)

// SQLiteTestDSN 返回位于 t.TempDir() 的临时 SQLite 文件 DSN，测试结束自动清理。
// 绝不指向 config.yaml 的库或 ~/.langhuan-data。
func SQLiteTestDSN(t *testing.T) string {
	t.Helper()
	return "file:" + filepath.Join(t.TempDir(), "test.db") + "?cache=shared"
}
