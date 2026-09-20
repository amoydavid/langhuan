//go:build integration

package migrate

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// v025World 是 000025 集成测试的最小数据夹具：workspace + KB + 根节点。
type v025World struct {
	database    *sql.DB
	workspaceID string
	kbID        string
	rootID      string
}

// newV025World 在空库迁移后的数据库中写入一组 workspace/KB/root。
// knowledge_bases 与 file_tree_root 互相引用（DEFERRABLE FK），需在同一事务内成对写入。
func newV025World(t *testing.T, database *sql.DB, tag string) *v025World {
	t.Helper()
	w := &v025World{
		database:    database,
		workspaceID: uuid.NewString(),
		kbID:        uuid.NewString(),
		rootID:      uuid.NewString(),
	}
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	require.NoError(t, err)
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(ctx, query, args...)
		require.NoError(t, execErr)
	}
	exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, $2, $2)`, w.workspaceID, "v025-"+tag)
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, $3, $4)`, w.kbID, w.workspaceID, "v025-kb-"+tag, w.rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, w.rootID, w.workspaceID, w.kbID)
	require.NoError(t, tx.Commit())
	return w
}

// addKnowledgeBase 在同一 workspace 下追加一组 KB + 根节点，返回 KB ID。
func (w *v025World) addKnowledgeBase(t *testing.T) string {
	t.Helper()
	kbID := uuid.NewString()
	rootID := uuid.NewString()
	ctx := context.Background()
	tx, err := w.database.BeginTx(ctx, nil)
	require.NoError(t, err)
	exec := func(query string, args ...any) {
		_, execErr := tx.ExecContext(ctx, query, args...)
		require.NoError(t, execErr)
	}
	exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, $3, $4)`, kbID, w.workspaceID, "v025-kb-extra-"+kbID, rootID)
	exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, w.workspaceID, kbID)
	require.NoError(t, tx.Commit())
	return kbID
}

// createSource 在默认 KB 下插入最小 kb_sources 行，返回源 ID。
func (w *v025World) createSource(t *testing.T, name string) string {
	t.Helper()
	return w.createSourceIn(t, w.kbID, name)
}

// createSourceIn 在指定 KB 下插入最小 kb_sources 行，返回源 ID。
func (w *v025World) createSourceIn(t *testing.T, kbID, name string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := w.database.ExecContext(context.Background(),
		`INSERT INTO kb_sources (id, workspace_id, kb_id, name) VALUES ($1, $2, $3, $4)`,
		id, w.workspaceID, kbID, name)
	require.NoError(t, err)
	return id
}

// createWebDocument 插入一个指向 sourceID 的 kind='web' 文档，返回文档 ID。
func (w *v025World) createWebDocument(t *testing.T, sourceID, uri string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := w.database.ExecContext(context.Background(), `
		INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status, external_id, kb_source_id)
		VALUES ($1, $2, $3, 'web', $4, 'web', $5, 'pending', $5, $6)
	`, id, w.workspaceID, w.kbID, uri, uri, sourceID)
	require.NoError(t, err)
	return id
}

// execSourceJob 插入一条目标为源的 source_sync/source_cleanup 任务。
func (w *v025World) execSourceJob(kbID, sourceID, jobType string) error {
	_, err := w.database.ExecContext(context.Background(), `
		INSERT INTO jobs (workspace_id, knowledge_base_id, kb_source_id, type, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`, w.workspaceID, kbID, sourceID, jobType)
	return err
}

// TestV025WebSourceSchemaAppliedOnEmptyDatabase 验证空库从 0 全量迁移到 25 后，
// kb_sources 表、documents/jobs 新列、索引、约束触发器与函数全部就位。
func TestV025WebSourceSchemaAppliedOnEmptyDatabase(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	ctx := context.Background()

	for _, column := range []string{
		"id", "workspace_id", "kb_id", "kind", "name", "status", "config",
		"sync_cursor", "next_sync_at", "sync_requested_force", "sync_last_result",
		"created_at", "updated_at", "deleted_at",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'kb_sources' AND column_name = $1
		`, column).Scan(&count))
		require.Equal(t, 1, count, "kb_sources.%s", column)
	}

	for _, tc := range []struct{ table, column string }{
		{"documents", "kb_source_id"},
		{"documents", "source_meta"},
		{"jobs", "kb_source_id"},
	} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		`, tc.table, tc.column).Scan(&count))
		require.Equal(t, 1, count, "%s.%s", tc.table, tc.column)
	}

	for _, indexName := range []string{
		"idx_kb_sources_workspace_kb",
		"uq_kb_sources_active_name",
		"idx_documents_kb_source",
		"idx_jobs_source_active",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = $1
		`, indexName).Scan(&count))
		require.Equal(t, 1, count, "index %s", indexName)
	}

	var functionCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_proc WHERE proname = 'enforce_jobs_source_target'
	`).Scan(&functionCount))
	require.Equal(t, 1, functionCount, "enforce_jobs_source_target 函数")

	var triggerCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_trigger
		WHERE tgname IN ('jobs_source_target_on_insert', 'jobs_source_target_on_update')
		  AND NOT tgisinternal
	`).Scan(&triggerCount))
	require.Equal(t, 2, triggerCount, "INSERT/UPDATE 两个源级目标触发器")

	var targetCheck string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'jobs_target_check'
	`).Scan(&targetCheck))
	require.Contains(t, targetCheck, "source_cleanup")
	require.Contains(t, targetCheck, "kb_source_id IS NOT NULL")
}

// TestV025KbSourcesCheckConstraints 验证 kb_sources 的 CHECK/唯一约束正反用例与默认值。
func TestV025KbSourcesCheckConstraints(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	w := newV025World(t, database, "check")
	ctx := context.Background()

	// 正例：最小插入命中全部默认值。
	sourceID := w.createSource(t, "产品文档站")
	var kind, status, config, syncCursor, syncLastResult string
	var force bool
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT kind, status, config::text, sync_cursor::text, sync_last_result::text, sync_requested_force
		FROM kb_sources WHERE id = $1
	`, sourceID).Scan(&kind, &status, &config, &syncCursor, &syncLastResult, &force))
	require.Equal(t, "web", kind)
	require.Equal(t, "active", status)
	require.Equal(t, "{}", config)
	require.Equal(t, "{}", syncCursor)
	require.Equal(t, "{}", syncLastResult)
	require.False(t, force)

	// 反例：kind 只允许 web。
	_, err := database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, kind, name) VALUES ($1, $2, 'rss', 'bad')
	`, w.workspaceID, w.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kb_sources_kind_check")

	// 反例：status 只允许 active/disabled。
	_, err = database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, name, status) VALUES ($1, $2, 'paused-src', 'paused')
	`, w.workspaceID, w.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kb_sources_status_check")

	// 反例：name 不得为空白。
	_, err = database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, name) VALUES ($1, $2, '   ')
	`, w.workspaceID, w.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kb_sources_name_check")

	// 反例：config 必须是 JSON 对象。
	_, err = database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, name, config) VALUES ($1, $2, 'arr', '[1,2]'::jsonb)
	`, w.workspaceID, w.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kb_sources_config_object_check")

	// 反例：同 KB 活源名称唯一（部分唯一索引）。
	_, err = database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, name) VALUES ($1, $2, '产品文档站')
	`, w.workspaceID, w.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "uq_kb_sources_active_name")

	// 正例：软删后名字释放，可重建同名源。
	_, err = database.ExecContext(ctx, `
		UPDATE kb_sources SET deleted_at = now() WHERE id = $1
	`, sourceID)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, name) VALUES ($1, $2, '产品文档站')
	`, w.workspaceID, w.kbID)
	require.NoError(t, err)

	// 反例：复合外键拒绝跨 workspace 引用 KB。
	other := newV025World(t, database, "check-other")
	_, err = database.ExecContext(ctx, `
		INSERT INTO kb_sources (workspace_id, kb_id, name) VALUES ($1, $2, 'cross-ws')
	`, w.workspaceID, other.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "kb_sources_kb_fk")
}

// TestV025WorkspaceSourceConnectionsProviderCheckRelaxed 验证 provider CHECK
// 放宽为 feishu/firecrawl/jina/browser 四值。
func TestV025WorkspaceSourceConnectionsProviderCheckRelaxed(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	w := newV025World(t, database, "conn")
	ctx := context.Background()

	for _, provider := range []string{"feishu", "firecrawl", "jina", "browser"} {
		_, err := database.ExecContext(ctx, `
			INSERT INTO workspace_source_connections (workspace_id, provider, name)
			VALUES ($1, $2, $3)
		`, w.workspaceID, provider, "conn-"+provider)
		require.NoError(t, err, "provider %s 必须被接受", provider)
	}

	_, err := database.ExecContext(ctx, `
		INSERT INTO workspace_source_connections (workspace_id, provider, name)
		VALUES ($1, 'notion', 'conn-notion')
	`, w.workspaceID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "workspace_source_connections_provider_check")
}

// TestV025JobsTargetCheckSourceBranch 验证 jobs_target_check 的源级分支：
// source_sync/source_cleanup 可携带 kb_source_id；其它类型或与文档目标混用被拒绝；
// KB 级（无源目标）分支语义原样保留。
func TestV025JobsTargetCheckSourceBranch(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	w := newV025World(t, database, "jobcheck")
	ctx := context.Background()
	sourceID := w.createSource(t, "docs-site")

	// 正例：源级 source_sync / source_cleanup。
	for _, jobType := range []string{"source_sync", "source_cleanup"} {
		require.NoError(t, w.execSourceJob(w.kbID, sourceID, jobType), "%s 源级目标必须被接受", jobType)
	}

	// 正例：KB 级 source_sync（飞书路径）仍可用。
	_, err := database.ExecContext(ctx, `
		INSERT INTO jobs (workspace_id, knowledge_base_id, type, status)
		VALUES ($1, $2, 'source_sync', 'pending')
	`, w.workspaceID, w.kbID)
	require.NoError(t, err)

	// 反例：非源任务类型携带源目标。
	_, err = database.ExecContext(ctx, `
		INSERT INTO jobs (workspace_id, knowledge_base_id, kb_source_id, type, status)
		VALUES ($1, $2, $3, 'document_parse_start', 'pending')
	`, w.workspaceID, w.kbID, sourceID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "jobs_target_check")

	// 反例：document 目标与源目标互斥（revision 为 NULL 使文档分支不成立）。
	_, err = database.ExecContext(ctx, `
		INSERT INTO jobs (workspace_id, knowledge_base_id, document_id, kb_source_id, type, status)
		VALUES ($1, $2, $3, $4, 'source_sync', 'pending')
	`, w.workspaceID, w.kbID, uuid.NewString(), sourceID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "jobs_target_check")

	// 反例：三目标皆空的非源任务仍被拒绝（既有语义）。
	_, err = database.ExecContext(ctx, `
		INSERT INTO jobs (workspace_id, knowledge_base_id, type, status)
		VALUES ($1, $2, 'document_parse_start', 'pending')
	`, w.workspaceID, w.kbID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "jobs_target_check")
}

// TestV025JobsSourceTargetEnforcement 验证 enforce_jobs_source_target 约束触发器：
// 源级任务目标必须是未软删的源、且所属 KB 未软删；UPDATE 仅在目标列变化时校验——
// 源/KB 软删后不冻结在飞任务的状态推进；kb_source_id 为 NULL 的行（KB 级飞书任务）
// 不做该校验，原样保留既有行为。
func TestV025JobsSourceTargetEnforcement(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	w := newV025World(t, database, "trigger")
	ctx := context.Background()

	// 正例：活源 + 活 KB。
	kbLive := w.addKnowledgeBase(t)
	liveSource := w.createSourceIn(t, kbLive, "live")
	require.NoError(t, w.execSourceJob(kbLive, liveSource, "source_sync"))

	// 反例：源已软删。
	deletedSource := w.createSourceIn(t, kbLive, "deleted")
	_, err := database.ExecContext(ctx, `
		UPDATE kb_sources SET deleted_at = now() WHERE id = $1
	`, deletedSource)
	require.NoError(t, err)
	err = w.execSourceJob(kbLive, deletedSource, "source_sync")
	require.Error(t, err)
	require.Contains(t, err.Error(), "has been deleted")

	// 反例：源存活但所属 KB 已软删。
	kbSoft := w.addKnowledgeBase(t)
	softKBSource := w.createSourceIn(t, kbSoft, "kb-soft")
	_, err = database.ExecContext(ctx, `
		UPDATE knowledge_bases SET deleted_at = now() WHERE id = $1
	`, kbSoft)
	require.NoError(t, err)
	err = w.execSourceJob(kbSoft, softKBSource, "source_sync")
	require.Error(t, err)
	require.Contains(t, err.Error(), "belongs to a deleted knowledge base")

	// 正例：KB 级（飞书）任务不受软删 KB 校验——原样保留的既有行为。
	_, err = database.ExecContext(ctx, `
		INSERT INTO jobs (workspace_id, knowledge_base_id, type, status)
		VALUES ($1, $2, 'source_sync', 'pending')
	`, w.workspaceID, kbSoft)
	require.NoError(t, err)

	// 反例：源目标被另一 workspace 的任务引用时，复合外键拒绝。
	other := newV025World(t, database, "trigger-other")
	_, err = database.ExecContext(ctx, `
		INSERT INTO jobs (workspace_id, knowledge_base_id, kb_source_id, type, status)
		VALUES ($1, $2, $3, 'source_sync', 'pending')
	`, other.workspaceID, other.kbID, liveSource)
	require.Error(t, err)
	require.Contains(t, err.Error(), "jobs_source_fk")

	// UPDATE 路径（目标列不变）：源软删后任务状态仍可推进到终态，不得被触发器冻结。
	runningSource := w.createSourceIn(t, kbLive, "running")
	require.NoError(t, w.execSourceJob(kbLive, runningSource, "source_sync"))
	_, err = database.ExecContext(ctx, `
		UPDATE kb_sources SET deleted_at = now() WHERE id = $1
	`, runningSource)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		UPDATE jobs SET status = 'running' WHERE kb_source_id = $1
	`, runningSource)
	require.NoError(t, err, "源软删后推进任务到 running 不得被触发器拒绝")
	_, err = database.ExecContext(ctx, `
		UPDATE jobs SET status = 'completed', error_message = '' WHERE kb_source_id = $1
	`, runningSource)
	require.NoError(t, err, "源软删后推进任务到终态不得被触发器拒绝")

	// UPDATE 路径（目标列变化）：改指软删源仍被拒绝。
	_, err = database.ExecContext(ctx, `
		UPDATE jobs SET kb_source_id = $1 WHERE kb_source_id = $2
	`, deletedSource, runningSource)
	require.Error(t, err)
	require.Contains(t, err.Error(), "has been deleted")

	// UPDATE 路径（目标列变化）：清空源目标（撤销溯源）无需源存活，放行。
	_, err = database.ExecContext(ctx, `
		UPDATE jobs SET kb_source_id = NULL WHERE kb_source_id = $1
	`, runningSource)
	require.NoError(t, err, "清空源目标应被放行")
}

// TestV025DocumentsSourceLineage 验证 documents 溯源列：
// kb_source_id 必须指向同 KB 的源；源硬删除时仅置空 kb_source_id（SET NULL 限定列），
// 租户键保持不变；指向源的任务随源级联删除。
func TestV025DocumentsSourceLineage(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	w := newV025World(t, database, "lineage")
	ctx := context.Background()

	sourceID := w.createSource(t, "site")
	docID := w.createWebDocument(t, sourceID, "https://docs.example.com/getting-started")

	var storedSource sql.NullString
	var sourceMeta string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT kb_source_id::text, source_meta::text FROM documents WHERE id = $1
	`, docID).Scan(&storedSource, &sourceMeta))
	require.True(t, storedSource.Valid)
	require.Equal(t, sourceID, storedSource.String)
	require.Equal(t, "{}", sourceMeta, "source_meta 默认空对象")

	// 反例：引用另一 KB 的源（同 workspace）被复合外键拒绝。
	otherKB := w.addKnowledgeBase(t)
	otherSource := w.createSourceIn(t, otherKB, "other-kb")
	_, err := database.ExecContext(ctx, `
		INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status, external_id, kb_source_id)
		VALUES ($1, $2, $3, 'web', $4, 'web', $5, 'pending', $5, $6)
	`, uuid.NewString(), w.workspaceID, w.kbID, "cross", "https://other.example.com/x", otherSource)
	require.Error(t, err)
	require.Contains(t, err.Error(), "documents_kb_source_fk")

	// 源硬删除：文档保留、仅 kb_source_id 置空；任务级联删除。
	require.NoError(t, w.execSourceJob(w.kbID, sourceID, "source_sync"))
	_, err = database.ExecContext(ctx, `DELETE FROM kb_sources WHERE id = $1`, sourceID)
	require.NoError(t, err)

	var kbSource sql.NullString
	var workspaceID, kbID string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT workspace_id::text, knowledge_base_id::text, kb_source_id::text FROM documents WHERE id = $1
	`, docID).Scan(&workspaceID, &kbID, &kbSource))
	require.False(t, kbSource.Valid, "源删除后 kb_source_id 必须置 NULL")
	require.Equal(t, w.workspaceID, workspaceID, "SET NULL 不得置空租户键 workspace_id")
	require.Equal(t, w.kbID, kbID, "SET NULL 不得置空租户键 knowledge_base_id")

	var jobCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM jobs WHERE kb_source_id = $1
	`, sourceID).Scan(&jobCount))
	require.Equal(t, 0, jobCount, "指向源的任务应随源级联删除")
}

// TestV025DownRollsBackWebSourceSchema 验证 down 迁移在带数据的库上可回滚到 24：
// 表/列/触发器/函数删除，jobs_target_check 与 provider CHECK 还原，且可重新 up。
func TestV025DownRollsBackWebSourceSchema(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(25))
	ctx := context.Background()

	// 预置数据：源、web 文档、源级任务，验证 down 不被残留引用阻断。
	w := newV025World(t, database, "down")
	sourceID := w.createSource(t, "site")
	docID := w.createWebDocument(t, sourceID, "https://example.com/a")
	require.NoError(t, w.execSourceJob(w.kbID, sourceID, "source_sync"))

	require.NoError(t, migrator.Migrate(24))

	var tableCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'kb_sources'
	`).Scan(&tableCount))
	require.Equal(t, 0, tableCount, "kb_sources 表应被删除")

	for _, tc := range []struct{ table, column string }{
		{"documents", "kb_source_id"},
		{"documents", "source_meta"},
		{"jobs", "kb_source_id"},
	} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		`, tc.table, tc.column).Scan(&count))
		require.Equal(t, 0, count, "%s.%s 应被删除", tc.table, tc.column)
	}

	var objectCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_proc WHERE proname = 'enforce_jobs_source_target'
	`).Scan(&objectCount))
	require.Equal(t, 0, objectCount, "enforce_jobs_source_target 函数应被删除")
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_trigger
		WHERE tgname IN ('jobs_source_target_on_insert', 'jobs_source_target_on_update')
		  AND NOT tgisinternal
	`).Scan(&objectCount))
	require.Equal(t, 0, objectCount, "jobs_source_target INSERT/UPDATE 触发器应被删除")

	var targetCheck string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'jobs_target_check'
	`).Scan(&targetCheck))
	require.Contains(t, targetCheck, "source_cleanup")
	require.NotContains(t, targetCheck, "kb_source_id")

	var providerCheck string
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname = 'workspace_source_connections_provider_check'
	`).Scan(&providerCheck))
	require.Contains(t, providerCheck, "feishu")
	require.NotContains(t, providerCheck, "firecrawl")

	// 功能验证：down 后四值 provider 被拒、feishu 仍可用。
	_, err := database.ExecContext(ctx, `
		INSERT INTO workspace_source_connections (workspace_id, provider, name)
		VALUES ($1, 'firecrawl', 'down-conn')
	`, w.workspaceID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "workspace_source_connections_provider_check")
	_, err = database.ExecContext(ctx, `
		INSERT INTO workspace_source_connections (workspace_id, provider, name)
		VALUES ($1, 'feishu', 'down-conn-feishu')
	`, w.workspaceID)
	require.NoError(t, err)

	// down 后 web 文档主体仍在（溯源列已随 down 移除）。
	var docCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM documents WHERE id = $1
	`, docID).Scan(&docCount))
	require.Equal(t, 1, docCount)

	// 可重新 up 到 25。
	require.NoError(t, migrator.Migrate(25))
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'kb_sources'
	`).Scan(&tableCount))
	require.Equal(t, 1, tableCount, "重新 up 后 kb_sources 应恢复")
}
