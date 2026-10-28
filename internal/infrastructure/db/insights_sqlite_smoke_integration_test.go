//go:build integration

package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
)

// TestInsightsSQLiteDialectSmoke 在 SQLite standalone 库上冒烟遥测写入与四类聚合 SQL
// （评审 Minor10：双方言兼容声明此前未经 SQLite 侧执行验证）。
// 夹具走最小闭环：迁移建库 → 直接 SQL 造 workspace/KB/generation/run/citation →
// 断言聚合与清理在 SQLite 方言下行为与 PG 一致。
func TestInsightsSQLiteDialectSmoke(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "insights-smoke.db") + "?cache=shared"
	require.NoError(t, migrate.Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn, AutoMigrate: true}))
	gormDB, _, err := Open(config.DatabaseConfig{Driver: "sqlite", DSN: dsn})
	require.NoError(t, err)
	sqlDB, _ := gormDB.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	// 本冒烟只验证聚合/写入 SQL 的 SQLite 方言执行，不验证 FK 语义（PG 集成已覆盖）；
	// SQLite 无 DEFERRABLE，KB<->generation<->root 的环形引用无法用插入顺序解开。
	require.NoError(t, gormDB.Exec("PRAGMA foreign_keys = OFF").Error)

	ctx := context.Background()
	ws := uuid.NewString()
	kb := uuid.NewString()
	gen := uuid.NewString()
	modelID, providerID := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()

	exec := func(query string, args ...any) {
		require.NoError(t, gormDB.WithContext(ctx).Exec(query, args...).Error)
	}
	// 最小 lineage（SQLite 快照表约束同 PG 语义：NOT NULL/CHECK/复合 FK）。
	exec(`INSERT INTO workspaces (id, name, slug) VALUES (?, 'smoke', 'smoke-ws')`, ws)
	exec(`INSERT INTO users (id, email, nickname, password_hash) VALUES (?, 'u@x.com', 'u', 'h')`, uuid.NewString())
	exec(`INSERT INTO model_providers (id, scope, workspace_id, name, provider, status, created_by) VALUES (?, 'workspace', ?, 'p', 'openai', 'active', (SELECT id FROM users LIMIT 1))`, providerID, ws)
	exec(`INSERT INTO models (id, provider_id, name, type, model_name, dimensions, status, created_by) VALUES (?, ?, 'm', 'embedding', 'e', 1024, 'active', (SELECT id FROM users LIMIT 1))`, modelID, providerID)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, active_index_generation_id, file_tree_root_id) VALUES (?, ?, 'kb', ?, ?)`, kb, ws, gen, uuid.NewString())
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ((SELECT file_tree_root_id FROM knowledge_bases WHERE id = ?), ?, ?, 'root', '')`, kb, ws, kb)
	exec(`INSERT INTO knowledge_base_index_generations (id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, embedding_dimension, model_config_hash, chunker_version, chunking_config, config_hash, status, created_at) VALUES (?, ?, ?, ?, ?, 'e', 1024, 'mh', 1, '{}', 'ch', 'ready', ?)`, gen, ws, kb, modelID, providerID, now)

	// 遥测写入：TelemetryRepository（含 transport 新列）。
	telemetry := NewTelemetryRepository(gormDB)
	runID := uuid.NewString()
	exec(`INSERT INTO search_runs (id, workspace_id, requested_scope, query_hash, query_chars, vector_top_k, keyword_top_k, final_top_k, retrieval_status, transport, created_at, expires_at) VALUES (?, ?, 'selected', 'h1', 8, 20, 20, 8, 'available', 'rest', ?, ?)`, runID, ws, now, now.Add(168*time.Hour))
	exec(`INSERT INTO search_run_generations (workspace_id, search_run_id, knowledge_base_id, generation_id, source_content_version, indexed_content_version, generation_config_hash, embedding_model_id, provider_id, model_name, model_config_hash, embedding_dimension, retrieval_config_hash) VALUES (?, ?, ?, ?, 1, 1, 'ch', ?, ?, 'm', 'mh', 1024, 'rh')`, ws, runID, kb, gen, modelID, providerID)
	require.NoError(t, telemetry.InsertCitations(ctx, []model.SearchCitation{{
		ID: uuid.New(), WorkspaceID: uuid.MustParse(ws), KbID: uuid.MustParse(kb),
		SearchRunID: uuid.MustParse(runID), DocumentID: uuid.New(), ChunkRevisionID: uuid.New(),
		Rank: 1, RetrievalStatus: value.RetrievalStatusAvailable, Transport: "rest", CreatedAt: now,
	}}))
	enabled, err := telemetry.GetQueryLogEnabled(ctx, uuid.MustParse(ws), uuid.MustParse(kb))
	require.NoError(t, err)
	require.False(t, enabled)
	batch, err := telemetry.GetQueryLogEnabledBatch(ctx, uuid.MustParse(ws), []uuid.UUID{uuid.MustParse(kb)})
	require.NoError(t, err)
	require.Len(t, batch, 1)

	// 四类聚合 SQL 在 SQLite 上执行（结果值 PG 侧已精确断言，此处冒烟方言执行）。
	insights := NewInsightsQueryRepository(gormDB)
	_, err = insights.CountRunsByStatus(ctx, uuid.MustParse(ws), uuid.MustParse(kb), now.Add(-14*24*time.Hour), now)
	require.NoError(t, err, "CountRunsByStatus SQLite")
	_, err = insights.ZeroRecallGroups(ctx, uuid.MustParse(ws), uuid.MustParse(kb), now.Add(-14*24*time.Hour), now, 10)
	require.NoError(t, err, "ZeroRecallGroups SQLite")
	_, err = insights.SleepingDocuments(ctx, uuid.MustParse(ws), uuid.MustParse(kb), now.Add(-30*24*time.Hour), 10)
	require.NoError(t, err, "SleepingDocuments SQLite")
	_, err = insights.RecentActivity(ctx, uuid.MustParse(ws), uuid.MustParse(kb), now.Add(-30*24*time.Hour), 20)
	require.NoError(t, err, "RecentActivity SQLite")
	_, _, err = insights.DocumentCitationSummary(ctx, uuid.MustParse(ws), uuid.MustParse(kb), uuid.New(), now.Add(-30*24*time.Hour))
	require.NoError(t, err, "DocumentCitationSummary SQLite")

	// 有界删除（子查询 LIMIT）与开关更新。
	require.NoError(t, telemetry.UpdateQueryLogEnabled(ctx, uuid.MustParse(ws), uuid.MustParse(kb), true))
	deleted, err := telemetry.DeleteExpiredCitations(ctx, now.Add(-35*24*time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, int64(0), deleted)
}
