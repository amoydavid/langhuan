//go:build integration

package migrate

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestMigrateV027InsightsTelemetry 验证迁移到 027 后：
//   - search_citations 与 search_query_log 两张表存在，索引就位；
//   - knowledge_bases.query_log_enabled 默认 false、可更新；
//   - CHECK 约束（rank>=1、citations retrieval_status 枚举、query_text 非空白）正反用例；
//   - D3：search_run 删除后 citation 行仍在（无级联 FK，生命周期独立）；
//   - down 迁移可还原（两表删除、开关列删除），且可重新 up。
func TestMigrateV027InsightsTelemetry(t *testing.T) {
	database, migrator := newZhparserMigrationTest(t)
	require.NoError(t, migrator.Migrate(27))

	ctx := context.Background()

	for _, table := range []string{"search_citations", "search_query_log"} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		`, table).Scan(&count))
		require.Equal(t, 1, count, "table %s should exist", table)
	}

	for _, indexName := range []string{
		"idx_citations_doc_window",
		"idx_citations_run",
		"idx_citations_created",
		"idx_query_log_kb_time",
		"idx_query_log_hash",
		"idx_query_log_created",
	} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = $1
		`, indexName).Scan(&count))
		require.Equal(t, 1, count, "index %s should exist", indexName)
	}

	w := newV025World(t, database, "v027")
	kbID := w.kbID

	// query_log_enabled 默认 false，可切换。
	var enabled bool
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT query_log_enabled FROM knowledge_bases WHERE id = $1
	`, kbID).Scan(&enabled))
	require.False(t, enabled)
	_, err := database.ExecContext(ctx, `
		UPDATE knowledge_bases SET query_log_enabled = true WHERE id = $1
	`, kbID)
	require.NoError(t, err)
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT query_log_enabled FROM knowledge_bases WHERE id = $1
	`, kbID).Scan(&enabled))
	require.True(t, enabled)

	// 遥测行夹具：一个 search_run + 一个文档。
	runID := uuid.NewString()
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_runs (id, workspace_id, requested_scope, query_hash, query_chars,
			vector_top_k, keyword_top_k, final_top_k, retrieval_status, created_at, expires_at)
		VALUES ($1, $2, 'selected', 'h1', 4, 20, 20, 8, 'available', now(), now())
	`, runID, w.workspaceID)
	require.NoError(t, err)
	// document_id 无外键（D3），用随机 uuid 即可。
	docID := uuid.NewString()

	// 正例：合法 citation 行。
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_citations (workspace_id, kb_id, search_run_id, document_id, chunk_revision_id, rank, retrieval_status)
		VALUES ($1, $2, $3, $4, $5, 1, 'available')
	`, w.workspaceID, kbID, runID, docID, uuid.NewString())
	require.NoError(t, err)

	// 反例：rank=0 违反 CHECK。
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_citations (workspace_id, kb_id, search_run_id, document_id, chunk_revision_id, rank, retrieval_status)
		VALUES ($1, $2, $3, $4, $5, 0, 'available')
	`, w.workspaceID, kbID, runID, docID, uuid.NewString())
	require.Error(t, err)

	// 反例：citations 不接受 empty/failed 状态。
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_citations (workspace_id, kb_id, search_run_id, document_id, chunk_revision_id, rank, retrieval_status)
		VALUES ($1, $2, $3, $4, $5, 1, 'empty')
	`, w.workspaceID, kbID, runID, docID, uuid.NewString())
	require.Error(t, err)

	// 反例：同 run 同 chunk_revision 唯一。
	chunkRev := uuid.NewString()
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_citations (workspace_id, kb_id, search_run_id, document_id, chunk_revision_id, rank, retrieval_status)
		VALUES ($1, $2, $3, $4, $5, 1, 'available')
	`, w.workspaceID, kbID, runID, docID, chunkRev)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_citations (workspace_id, kb_id, search_run_id, document_id, chunk_revision_id, rank, retrieval_status)
		VALUES ($1, $2, $3, $4, $5, 2, 'available')
	`, w.workspaceID, kbID, runID, docID, chunkRev)
	require.Error(t, err)

	// 正例：query_log 行；反例：空白 query_text。
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_query_log (workspace_id, kb_id, search_run_id, query_text, query_hash, query_chars, retrieval_status)
		VALUES ($1, $2, $3, '发票抬头怎么改', 'h1', 7, 'empty')
	`, w.workspaceID, kbID, runID)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		INSERT INTO search_query_log (workspace_id, kb_id, search_run_id, query_text, query_hash, query_chars, retrieval_status)
		VALUES ($1, $2, $3, '   ', 'h2', 3, 'empty')
	`, w.workspaceID, kbID, runID)
	require.Error(t, err)

	// D3：search_run 删除后 citation 行仍在。
	_, err = database.ExecContext(ctx, `DELETE FROM search_runs WHERE id = $1`, runID)
	require.NoError(t, err)
	var citationCount int
	require.NoError(t, database.QueryRowContext(ctx, `SELECT count(*) FROM search_citations`).Scan(&citationCount))
	require.Equal(t, 2, citationCount, "citation 行应活过 search_run 删除（D3）")
	var queryLogCount int
	require.NoError(t, database.QueryRowContext(ctx, `SELECT count(*) FROM search_query_log`).Scan(&queryLogCount))
	require.Equal(t, 1, queryLogCount)

	// down 还原：回到 26 后两表消失、开关列消失，且可重新 up。
	require.NoError(t, migrator.Migrate(26))
	for _, table := range []string{"search_citations", "search_query_log"} {
		var count int
		require.NoError(t, database.QueryRowContext(ctx, `
			SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		`, table).Scan(&count))
		require.Equal(t, 0, count, "table %s should be dropped", table)
	}
	var colCount int
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'knowledge_bases' AND column_name = 'query_log_enabled'
	`).Scan(&colCount))
	require.Equal(t, 0, colCount, "query_log_enabled should be dropped")

	require.NoError(t, migrator.Migrate(27))
	require.NoError(t, database.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'search_citations'
	`).Scan(&colCount))
	require.Equal(t, 1, colCount, "重新 up 后 search_citations 应恢复")
}
