//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// insightsWorld 复用 knowledgeSchemaSeed 构造主 KB（seed）与第二 seed
// （隔离断言用），search_run generations 指向真实 IndexGeneration。
// documents 夹具遵守真实约束：kind='file' 需恰好一个 file node（改用 faq/web），
// web 必须带 source_uri，failed run 必须带 failure_class。
type insightsWorld struct {
	t     *testing.T
	db    *gorm.DB
	repo  *InsightsQueryRepository
	seed  knowledgeSchemaSeed
	seedB knowledgeSchemaSeed
	now   time.Time
}

func (w *insightsWorld) workspaceID() uuid.UUID { return w.seed.workspaceID }
func (w *insightsWorld) kbID() uuid.UUID        { return w.seed.kbID }
func (w *insightsWorld) kbOtherID() uuid.UUID   { return w.seedB.kbID }

func newInsightsWorld(t *testing.T) *insightsWorld {
	t.Helper()
	ctx, database := openIntegrationTestDB(t)
	var seed, seedB knowledgeSchemaSeed
	require.NoError(t, database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed = insertKnowledgeSchemaSeed(t, ctx, tx)
		seedB = insertKnowledgeSchemaSeed(t, ctx, tx)
		return nil
	}))
	return &insightsWorld{
		t: t, db: database, repo: NewInsightsQueryRepository(database),
		seed: seed, seedB: seedB, now: time.Now().UTC(),
	}
}

// insertRun 插入一条终态 search_run + generation（参与 kb）。
// 参数顺序与占位符严格一致：id, workspace_id, query_hash, status, transport, created, completed, expires。
func (w *insightsWorld) insertRun(kbID uuid.UUID, status, transport, queryHash string, createdAt time.Time) string {
	w.t.Helper()
	runID := uuid.NewString()
	ctx := context.Background()
	seed := w.seed
	workspaceID := seed.workspaceID
	if kbID == w.seedB.kbID {
		seed = w.seedB
		workspaceID = w.seedB.workspaceID
	}
	// failed 必须带非空 failure_class（search_runs_failure_class 约束），用 CASE 保持顺序参数化。
	require.NoError(w.t, w.db.WithContext(ctx).Exec(`
		INSERT INTO search_runs (id, workspace_id, requested_scope, query_hash, query_chars,
			vector_top_k, keyword_top_k, final_top_k, retrieval_status, transport, failure_class,
			created_at, completed_at, expires_at)
		VALUES ($1, $2, 'selected', $3, 8, 20, 20, 8, $4, $5,
			CASE WHEN $4 = 'failed' THEN 'retrieval_error' ELSE '' END, $6, $7, $8)
	`, runID, workspaceID, queryHash, status, transport, createdAt, createdAt, createdAt.Add(168*time.Hour)).Error)
	require.NoError(w.t, w.db.WithContext(ctx).Exec(`
		INSERT INTO search_run_generations (workspace_id, search_run_id, knowledge_base_id, generation_id,
			source_content_version, indexed_content_version, generation_config_hash, embedding_model_id,
			provider_id, model_name, model_config_hash, embedding_dimension, retrieval_config_hash)
		VALUES ($1, $2, $3, $4, 1, 1, 'ch', $5, $6, 'm', 'mh', 1024, 'rh')
	`, workspaceID, runID, kbID, seed.generationID, seed.modelID, seed.providerID).Error)
	return runID
}

func (w *insightsWorld) insertCitation(kbID uuid.UUID, runID, docID string, createdAt time.Time) {
	w.t.Helper()
	require.NoError(w.t, w.db.WithContext(context.Background()).Exec(`
		INSERT INTO search_citations (workspace_id, kb_id, search_run_id, document_id, chunk_revision_id, rank, retrieval_status, created_at)
		VALUES ($1, $2, $3, $4, $5, 1, 'available', $6)
	`, w.workspaceID(), kbID, runID, docID, uuid.NewString(), createdAt).Error)
}

func (w *insightsWorld) insertQueryLog(kbID uuid.UUID, runID, text, queryHash string, createdAt time.Time) {
	w.t.Helper()
	require.NoError(w.t, w.db.WithContext(context.Background()).Exec(`
		INSERT INTO search_query_log (workspace_id, kb_id, search_run_id, query_text, query_hash, query_chars, retrieval_status, created_at)
		VALUES ($1, $2, $3, $4, $5, 8, 'empty', $6)
	`, w.workspaceID(), kbID, runID, text, queryHash, createdAt).Error)
}

// insertDocument 插入 ready 文档。kind 只用 faq/web（file 需要恰好一个 file node，
// 夹具不建树）；web 携带固定 source_uri（单测试内只插一个 web 文档避免唯一冲突）。
func (w *insightsWorld) insertDocument(kbID uuid.UUID, kind, title string, updatedAt time.Time, deleted bool) string {
	w.t.Helper()
	docID := uuid.NewString()
	query := `
		INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri,
			status, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, CASE WHEN $4 = 'web' THEN 'web' ELSE 'upload' END,
			CASE WHEN $4 = 'web' THEN 'https://example.com/insights-page' ELSE NULL END,
			'ready', $6, $6, `
	args := []any{docID, w.workspaceID(), kbID, kind, title, updatedAt}
	if deleted {
		query += `$7)`
		args = append(args, updatedAt)
	} else {
		query += `NULL)`
	}
	require.NoError(w.t, w.db.WithContext(context.Background()).Exec(query, args...).Error)
	return docID
}

func TestInsightsCountRunsByStatusFiltersConsoleAndScope(t *testing.T) {
	w := newInsightsWorld(t)
	ctx := context.Background()
	from := w.now.Add(-14 * 24 * time.Hour)

	w.insertRun(w.kbID(), "empty", "rest", "h1", w.now.Add(-2*24*time.Hour))
	w.insertRun(w.kbID(), "available", "rest", "h2", w.now.Add(-3*24*time.Hour))
	w.insertRun(w.kbID(), "empty", "console", "h3", w.now.Add(-1*24*time.Hour))   // 调试流量：排除
	w.insertRun(w.kbID(), "failed", "rest", "h4", w.now.Add(-1*24*time.Hour))     // 计数但 service 剔除分母
	w.insertRun(w.kbOtherID(), "empty", "rest", "h5", w.now.Add(-1*24*time.Hour)) // 其它 workspace 的 KB：排除

	rows, err := w.repo.CountRunsByStatus(ctx, w.workspaceID(), w.kbID(), from, w.now)
	require.NoError(t, err)
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.RetrievalStatus] = row.Runs
	}
	require.Equal(t, int64(1), counts["empty"], "console 流量与跨 KB run 必须被排除")
	require.Equal(t, int64(1), counts["available"])
	require.Equal(t, int64(1), counts["failed"])
	require.Equal(t, int64(0), counts["degraded"])
}

func TestInsightsZeroRecallGroupsAndLatestTexts(t *testing.T) {
	w := newInsightsWorld(t)
	ctx := context.Background()
	from := w.now.Add(-14 * 24 * time.Hour)

	run1 := w.insertRun(w.kbID(), "empty", "rest", "hA", w.now.Add(-5*24*time.Hour))
	run2 := w.insertRun(w.kbID(), "empty", "rest", "hA", w.now.Add(-1*24*time.Hour))
	w.insertRun(w.kbID(), "available", "rest", "hB", w.now.Add(-1*24*time.Hour)) // 非 empty 不进组

	groups, err := w.repo.ZeroRecallGroups(ctx, w.workspaceID(), w.kbID(), from, w.now, 10)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "hA", groups[0].QueryHash)
	require.Equal(t, int64(2), groups[0].Hits)

	w.insertQueryLog(w.kbID(), run1, "较早的问法", "hA", w.now.Add(-5*24*time.Hour))
	w.insertQueryLog(w.kbID(), run2, "发票抬头怎么改", "hA", w.now.Add(-1*24*time.Hour))

	texts, err := w.repo.LatestQueryTexts(ctx, w.workspaceID(), w.kbID(), from)
	require.NoError(t, err)
	require.Len(t, texts, 1)
	require.Equal(t, "发票抬头怎么改", texts[0].QueryText, "同 hash 取最新一条原文")
}

func TestInsightsSleepingDocumentsAntiJoin(t *testing.T) {
	w := newInsightsWorld(t)
	ctx := context.Background()
	windowStart := w.now.Add(-30 * 24 * time.Hour)

	sleeping := w.insertDocument(w.kbID(), "faq", "2024 报销制度（旧版）", windowStart.Add(-24*time.Hour), false)
	citedDoc := w.insertDocument(w.kbID(), "web", "常被引用的页面", windowStart.Add(-24*time.Hour), false)
	// 窗口内新入（不满足"窗口起点前已存在"）。
	w.insertDocument(w.kbID(), "faq", "新入库问题", w.now.Add(-1*24*time.Hour), false)
	// 已软删。
	w.insertDocument(w.kbID(), "faq", "已删条目", windowStart.Add(-24*time.Hour), true)

	// 给"常被引用的页面"一条窗口内引用。
	runID := w.insertRun(w.kbID(), "available", "rest", "h1", w.now.Add(-2*24*time.Hour))
	w.insertCitation(w.kbID(), runID, citedDoc, w.now.Add(-2*24*time.Hour))

	rows, err := w.repo.SleepingDocuments(ctx, w.workspaceID(), w.kbID(), windowStart, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1, "只剩窗口前就绪+零引用+未删的文档")
	require.Equal(t, sleeping, rows[0].DocumentID.String())
	require.Equal(t, "faq", rows[0].Kind)
}

func TestInsightsRecentActivityThreeKinds(t *testing.T) {
	w := newInsightsWorld(t)
	ctx := context.Background()
	since := w.now.Add(-30 * 24 * time.Hour)

	// sync：源级 web 同步完成。
	sourceID := uuid.NewString()
	require.NoError(t, w.db.WithContext(ctx).Exec(`
		INSERT INTO kb_sources (id, workspace_id, kb_id, name) VALUES ($1, $2, $3, '产品文档站')
	`, sourceID, w.workspaceID(), w.kbID()).Error)
	require.NoError(t, w.db.WithContext(ctx).Exec(`
		INSERT INTO jobs (workspace_id, knowledge_base_id, kb_source_id, type, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'source_sync', 'completed', $4, $4)
	`, w.workspaceID(), w.kbID(), sourceID, w.now.Add(-1*time.Hour)).Error)

	// revision：web 文档新版本（faq revision 需要 questions/answers 子表配套，夹具用 web）。
	docID := w.insertDocument(w.kbID(), "web", "退款政策", w.now.Add(-2*24*time.Hour), false)
	require.NoError(t, w.db.WithContext(ctx).Exec(`
		INSERT INTO document_revisions (id, workspace_id, knowledge_base_id, document_id, kind, revision_no, revision_reason, processing_version, status, created_at)
		VALUES ($1, $2, $3, $4, 'web', 2, 'edit', 1, 'pending', $5)
	`, uuid.NewString(), w.workspaceID(), w.kbID(), docID, w.now.Add(-2*time.Hour)).Error)

	// delete：软删。
	w.insertDocument(w.kbID(), "faq", "过期文件", w.now.Add(-3*time.Hour), true)

	events, err := w.repo.RecentActivity(ctx, w.workspaceID(), w.kbID(), since, 20)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 3)
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
		switch event.Kind {
		case "sync":
			require.Equal(t, "产品文档站", event.SourceName)
			require.True(t, event.Success)
		case "revision":
			require.Equal(t, "退款政策", event.DocumentTitle)
			require.Equal(t, "edit", event.RevisionReason)
		case "delete":
			require.Equal(t, "过期文件", event.DocumentTitle)
		}
	}
	require.True(t, kinds["sync"] && kinds["revision"] && kinds["delete"], "三类事件齐备: %v", kinds)

	// 时间倒序。
	for i := 1; i < len(events); i++ {
		require.False(t, events[i-1].OccurredAt.Before(events[i].OccurredAt), "feed 必须按时间倒序")
	}
}

func TestInsightsDocumentCitationSummary(t *testing.T) {
	w := newInsightsWorld(t)
	ctx := context.Background()
	docID := w.insertDocument(w.kbID(), "faq", "预警目标", w.now.Add(-40*24*time.Hour), false)
	runID := w.insertRun(w.kbID(), "available", "rest", "h1", w.now.Add(-2*24*time.Hour))
	w.insertCitation(w.kbID(), runID, docID, w.now.Add(-2*24*time.Hour))

	count, lastCited, err := w.repo.DocumentCitationSummary(ctx, w.workspaceID(), w.kbID(), uuid.MustParse(docID), w.now.Add(-30*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.NotNil(t, lastCited)

	// 窗口外引用不计。
	count, _, err = w.repo.DocumentCitationSummary(ctx, w.workspaceID(), w.kbID(), uuid.MustParse(docID), w.now.Add(-1*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
}
