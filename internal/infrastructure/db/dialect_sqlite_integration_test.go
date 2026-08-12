//go:build integration

package db

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	"github.com/dajee/langhuan/internal/testsupport"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// 这些测试验证 Repository 层 PG 专属 SQL 的 SQLite 方言分支。
//
// 注意：生产 SQLite DSN（db.Open）带 _time_format=sqlite，modernc 会把时间列以
// 字符串返回，GORM 无法直接 Scan 进 time.Time。这是既有的 SQLite 基础设施限制
// （与 _time_format pragma 相关），不属于本方言分发任务的范围。因此这些测试
// 刻意只读取 source_config / scopes / 计数等不触发 time-scan 的列，专注于验证
// json_set / json_remove / json_extract / json(?) / SUM(CASE WHEN) / datetime('now')
// 等方言分支本身的正确性。

// openSQLiteTestDB 打开一个临时 SQLite 库并迁移到最新 schema，返回可直接使用的连接。
func openSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testsupport.SQLiteTestDSN(t)
	if err := migrate.Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn}); err != nil {
		t.Fatalf("SQLite 迁移失败: %v", err)
	}
	database, _, err := Open(config.DatabaseConfig{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatalf("打开 SQLite 失败: %v", err)
	}
	// 关闭外键检查以便插入最小 KB 夹具（knowledge_bases.file_tree_root_id 指向
	// file_tree_nodes，存在循环 FK；测试只关心 source_config JSON 行为）。
	// 单连接池（MaxOpenConns=1）保证该 PRAGMA 在同一连接上持续生效。
	if err := database.Exec("PRAGMA foreign_keys=OFF").Error; err != nil {
		t.Fatalf("关闭 SQLite 外键失败: %v", err)
	}
	return database
}

// TestSQLiteJSONOpsDialect 覆盖 knowledge_base_repository 与 source_sync_store 中
// 所有 jsonb_set / ->> / ::timestamptz / ::boolean / ::jsonb 的 SQLite 分支：
// json_set / json_remove / json_extract / json(?)。
func TestSQLiteJSONOpsDialect(t *testing.T) {
	database := openSQLiteTestDB(t)
	ctx := context.Background()
	wsID := uuid.New()
	kbID := uuid.New()
	connID := uuid.New()
	rootNode := uuid.New()

	if err := database.Exec(
		"INSERT INTO workspaces (id, name, slug) VALUES (?, ?, ?)",
		wsID, "ws", "slug-"+wsID.String()[:8],
	).Error; err != nil {
		t.Fatalf("插入 workspace 失败: %v", err)
	}
	if err := database.Exec(
		"INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id, source_type, source_config, source_connection_id) "+
			"VALUES (?, ?, 'kb', ?, 'feishu_drive', '{}', ?)",
		kbID, wsID, rootNode, connID,
	).Error; err != nil {
		t.Fatalf("插入 knowledge_base 失败: %v", err)
	}

	kbRepo := NewKnowledgeBaseRepository(database)
	syncStore := NewSourceSyncDBStore(database)
	readConfig := func() map[string]any {
		t.Helper()
		var raw string
		if err := database.Raw("SELECT source_config FROM knowledge_bases WHERE id = ?", kbID).Scan(&raw).Error; err != nil {
			t.Fatalf("读取 source_config 失败: %v", err)
		}
		var cfg map[string]any
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("解析 source_config 失败: %v", err)
		}
		return cfg
	}

	// 1) UpdateNextSyncAt：写入过去时间，ListDueFeishuKBs（json_extract 比较）应命中。
	past := time.Now().UTC().Add(-1 * time.Hour)
	if err := kbRepo.UpdateNextSyncAt(ctx, wsID, kbID, past); err != nil {
		t.Fatalf("UpdateNextSyncAt(set) 失败: %v", err)
	}
	cfg := readConfig()
	if got, ok := cfg["next_sync_at"].(string); !ok || !strings.HasPrefix(got, past.Format(time.RFC3339Nano)[:19]) {
		t.Fatalf("next_sync_at 未以 RFC3339Nano 字符串写入: %#v", cfg["next_sync_at"])
	}
	due, err := kbRepo.ListDueFeishuKBs(ctx, time.Now().UTC(), uuid.Nil)
	if err != nil {
		t.Fatalf("ListDueFeishuKBs 失败: %v", err)
	}
	if len(due) != 1 || due[0].ID != kbID {
		t.Fatalf("ListDueFeishuKBs(json_extract<=) 未命中到期 KB: %+v", due)
	}

	// 2) UpdateNextSyncAt：零值 → json_remove 清除键，ListDueFeishuKBs 不再命中。
	if err := kbRepo.UpdateNextSyncAt(ctx, wsID, kbID, time.Time{}); err != nil {
		t.Fatalf("UpdateNextSyncAt(clear) 失败: %v", err)
	}
	if _, ok := readConfig()["next_sync_at"]; ok {
		t.Fatalf("json_remove 未删除 next_sync_at")
	}
	due, _ = kbRepo.ListDueFeishuKBs(ctx, time.Now().UTC(), uuid.Nil)
	if len(due) != 0 {
		t.Fatalf("清除 next_sync_at 后仍被 ListDueFeishuKBs 命中: %+v", due)
	}

	// 3) UpdateSyncCursor（json_set 写时间字符串）。
	cursor := time.Now().UTC().Add(-30 * time.Minute)
	if err := kbRepo.UpdateSyncCursor(ctx, wsID, kbID, cursor); err != nil {
		t.Fatalf("UpdateSyncCursor 失败: %v", err)
	}
	if _, ok := readConfig()["sync_cursor"].(string); !ok {
		t.Fatalf("sync_cursor 未写入")
	}

	// 4) UpdateSourceDeletePolicy（json_set 写字符串）。
	if err := kbRepo.UpdateSourceDeletePolicy(ctx, wsID, kbID, "remove"); err != nil {
		t.Fatalf("UpdateSourceDeletePolicy 失败: %v", err)
	}
	if got := readConfig()["on_delete"]; got != "remove" {
		t.Fatalf("on_delete 写入错误: %#v", got)
	}

	// 5) force latch：RequestSourceSync 写 JSON 布尔 true（json(?)）。
	job, created, err := syncStore.RequestSourceSync(ctx, wsID, kbID, connID, true)
	if err != nil {
		t.Fatalf("RequestSourceSync 失败: %v", err)
	}
	if !created {
		t.Fatalf("首次 RequestSourceSync 应创建 Job")
	}
	cfg = readConfig()
	if latch, ok := cfg["sync_requested_force"].(bool); !ok || !latch {
		t.Fatalf("force latch 未以 JSON 布尔写入: %#v（应为 bool true）", cfg["sync_requested_force"])
	}
	// RequestSourceSync 创建了进行中 Job，把该 Job 置为终态（直接 update），
	// 让 ListFeishuKBsWithForceLatchAndNoActiveJob 的 NOT EXISTS 子查询为真，
	// 从而验证 json_extract(...) = 1 的布尔比较命中 latch KB。
	if err := database.Exec(
		"UPDATE jobs SET status = 'completed' WHERE id = ?", job.ID,
	).Error; err != nil {
		t.Fatalf("置 Job 终态失败: %v", err)
	}
	latched, err := syncStore.ListFeishuKBsWithForceLatchAndNoActiveJob(ctx)
	if err != nil {
		t.Fatalf("ListFeishuKBsWithForceLatchAndNoActiveJob 失败: %v", err)
	}
	if len(latched) != 1 || latched[0].ID != kbID {
		t.Fatalf("json_extract 布尔比较未命中 latch KB: %+v", latched)
	}

	// 6) ConsumeForceLatch 读到 true 并清空（readForceLatch 依赖 .(bool) 断言）。
	consumed, err := syncStore.ConsumeForceLatch(ctx, wsID, kbID, uuid.New())
	if err != nil {
		t.Fatalf("ConsumeForceLatch 失败: %v", err)
	}
	if !consumed {
		t.Fatalf("ConsumeForceLatch 应读到 true")
	}
	if latch, ok := readConfig()["sync_requested_force"].(bool); !ok || latch {
		t.Fatalf("ConsumeForceLatch 后 latch 未清空: %#v", readConfig()["sync_requested_force"])
	}

	// 7) UpdateSyncResult：json(?) 写入 JSON 对象，回读为嵌套 map。
	result := appservice.SyncResult{
		Status: "succeeded", Complete: true, SyncedDocuments: 3, SkippedDocuments: 1,
		FinishedAt: time.Now().UTC(),
	}
	if err := syncStore.UpdateSyncResult(ctx, wsID, kbID, result); err != nil {
		t.Fatalf("UpdateSyncResult 失败: %v", err)
	}
	last := readConfig()["sync_last_result"]
	asMap, ok := last.(map[string]any)
	if !ok {
		t.Fatalf("sync_last_result 未以 JSON 对象回读: %#v", last)
	}
	if got := asMap["synced_documents"]; got != float64(3) {
		t.Fatalf("sync_last_result.synced_documents 错误: %#v", got)
	}
	if got := asMap["status"]; got != "succeeded" {
		t.Fatalf("sync_last_result.status 错误: %#v", got)
	}
}

// TestSQLiteNowFunctionDialect 验证 now() → datetime('now') 分支。
// 用 past/future 两个会话验证比较方向（避开 time-scan 既有限制）。
func TestSQLiteNowFunctionDialect(t *testing.T) {
	database := openSQLiteTestDB(t)
	ctx := context.Background()
	uid := uuid.New()
	now := time.Now().UTC()
	if err := database.Exec(
		"INSERT INTO users (id, nickname, password_hash, is_platform_admin, created_at, updated_at) VALUES (?,?,?,?,?,?)",
		uid, "n", "x", 0, now, now,
	).Error; err != nil {
		t.Fatalf("插入 user 失败: %v", err)
	}
	pastSession := uuid.New()
	futureSession := uuid.New()
	if err := database.Exec(
		"INSERT INTO sessions (id, user_id, expires_at, created_at, last_seen_at) VALUES (?,?,?,?,?)",
		pastSession, uid, now.Add(-time.Hour), now, now,
	).Error; err != nil {
		t.Fatalf("插入过期会话失败: %v", err)
	}
	if err := database.Exec(
		"INSERT INTO sessions (id, user_id, expires_at, created_at, last_seen_at) VALUES (?,?,?,?,?)",
		futureSession, uid, now.Add(time.Hour), now, now,
	).Error; err != nil {
		t.Fatalf("插入有效会话失败: %v", err)
	}

	// datetime('now') 比较方向：future 命中、past 不命中。
	var n int64
	if err := database.Raw("SELECT COUNT(*) FROM sessions WHERE expires_at > datetime('now')").Scan(&n).Error; err != nil {
		t.Fatalf("datetime('now') 查询失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("expires_at > datetime('now') 命中数 = %d, 期望 1", n)
	}

	// session_repository.FindActive 对过期会话返回 ErrRepositoryNotFound，
	// 证明 SQLite 分支使用了 datetime('now') 而非未翻译的 now()（若未翻译，
	// SQLite 会把 now() 当作字符串列名比较，行为不一致）。
	sessRepo := NewSessionRepository(database)
	if _, err := sessRepo.FindActive(ctx, pastSession); err != ErrRepositoryNotFound {
		t.Fatalf("过期会话应返回 ErrRepositoryNotFound, got %v", err)
	}
}

// TestSQLiteWorkspaceAPIKeyScopesRoundTrip 验证 pq.StringArray 在 SQLite 下无需改造：
// PG 数组字面量 {a,b} 存入 TEXT 列，pq.StringArray.Scan 回读为 []string。
func TestSQLiteWorkspaceAPIKeyScopesRoundTrip(t *testing.T) {
	database := openSQLiteTestDB(t)
	ctx := context.Background()
	wsID := uuid.New()
	now := time.Now().UTC()
	if err := database.Exec(
		"INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		wsID, "ws", "s-"+wsID.String()[:8], now, now,
	).Error; err != nil {
		t.Fatalf("插入 workspace 失败: %v", err)
	}
	row := &WorkspaceAPIKeyRow{
		ID:                    uuid.New(),
		WorkspaceID:           wsID,
		Name:                  "k",
		TokenHash:             "h" + uuid.NewString(),
		TokenPrefix:           "p",
		TokenSecretCiphertext: []byte("dummy-cipher-data-0123456789"),
		Scopes:                []string{"documents:read", "documents:write"},
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := database.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("创建 API Key 失败: %v", err)
	}
	type only struct {
		ID     uuid.UUID      `gorm:"column:id"`
		Scopes pq.StringArray `gorm:"column:scopes"`
	}
	var got only
	if err := database.WithContext(ctx).Table("workspace_api_tokens").
		Select("id, scopes").Where("id = ?", row.ID).Scan(&got).Error; err != nil {
		t.Fatalf("读取 scopes 失败: %v", err)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "documents:read" || got.Scopes[1] != "documents:write" {
		t.Fatalf("scopes 回读错误: %#v", []string(got.Scopes))
	}
}

// TestSQLiteSumCaseAggregation 验证 COUNT(*) FILTER 的 SQLite 等价写法
// SUM(CASE WHEN ... THEN 1 ELSE 0 END) 在 documents 上正确聚合。
func TestSQLiteSumCaseAggregation(t *testing.T) {
	database := openSQLiteTestDB(t)
	wsID := uuid.New()
	kbID := uuid.New()
	if err := database.Exec("INSERT INTO workspaces (id, name, slug) VALUES (?, ?, ?)",
		wsID, "ws", "s-"+wsID.String()[:8]).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(
		"INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id, source_type, source_config) "+
			"VALUES (?, ?, 'kb', ?, 'upload', '{}')", kbID, wsID, uuid.New(),
	).Error; err != nil {
		t.Fatal(err)
	}
	// 插入不同 kind/status 的文档（FK 已关闭，无需上游关联存在）。
	docs := []struct{ kind, status string }{
		{"file", "ready"}, {"file", "ready"}, {"file", "failed"},
		{"faq", "ready"}, {"web", "pending"},
	}
	for i, d := range docs {
		var sourceURI any
		if d.kind == "web" {
			sourceURI = "https://example.com/" + uuid.NewString()
		}
		if err := database.Exec(
			"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status) "+
				"VALUES (?, ?, ?, ?, ?, 'upload', ?, ?)",
			uuid.New(), wsID, kbID, d.kind, "d"+strconv.Itoa(i), sourceURI, d.status,
		).Error; err != nil {
			t.Fatalf("插入 document %d 失败: %v", i, err)
		}
	}

	// 与 knowledge_base_summary_repository SQLite 分支相同的聚合表达式。
	var counts struct {
		Total int64
		File  int64
		FAQ   int64
		Web   int64
		Ready int64
	}
	if err := database.Raw(`
		SELECT
			COUNT(*) AS total,
			SUM(CASE WHEN kind = 'file' THEN 1 ELSE 0 END) AS file,
			SUM(CASE WHEN kind = 'faq' THEN 1 ELSE 0 END) AS faq,
			SUM(CASE WHEN kind = 'web' THEN 1 ELSE 0 END) AS web,
			SUM(CASE WHEN status IN ('ready', 'completed') THEN 1 ELSE 0 END) AS ready
		FROM documents
		WHERE workspace_id = ? AND knowledge_base_id = ? AND deleted_at IS NULL AND status <> 'deleted'`,
		wsID, kbID,
	).Scan(&counts).Error; err != nil {
		t.Fatalf("SUM(CASE WHEN) 聚合失败: %v", err)
	}
	if counts.Total != 5 || counts.File != 3 || counts.FAQ != 1 || counts.Web != 1 || counts.Ready != 3 {
		t.Fatalf("聚合结果错误: %+v", counts)
	}
}
