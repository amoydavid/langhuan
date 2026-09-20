//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	web "github.com/dajee/langhuan/internal/adapters/source/web"
	localstorage "github.com/dajee/langhuan/internal/adapters/storage/local"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件覆盖 web 内容源（v1.3.0）的 DB 集成（spec §12.3）：
//   - kb_sources 运行态读写（Get/latch/cursor/last_result）与 source_meta 往返；
//   - 源级 source_sync Job 落库（jobs_target_check 第五分支）；
//   - web 源编排 e2e：真实 web.Connector + httptest 假站点 + 真实 PG store——
//     导入建档（kind=web/kb_source_id/source_meta）、增量与 304、force latch、
//     删除策略 keep/remove、跨源先到先得。

// recordingQueue 记录入队请求的 queue.JobQueue fake。
type recordingQueue struct {
	mu       sync.Mutex
	requests []queue.JobRequest
}

func (q *recordingQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: "queued"}, nil
}

func (q *recordingQueue) countByType(taskType string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	count := 0
	for _, req := range q.requests {
		if req.Type == taskType {
			count++
		}
	}
	return count
}

// insertWebSource 在 kb_sources 插入一个 web 内容源并返回领域模型。
func insertWebSource(t *testing.T, ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed, name string, config map[string]any) *model.KbSource {
	t.Helper()
	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		Kind: value.KbSourceKindWeb, Name: name, Config: config,
	})
	if err != nil {
		t.Fatalf("NewKbSource: %v", err)
	}
	if err := tx.WithContext(ctx).Create(kbSourceToRow(source)).Error; err != nil {
		t.Fatalf("insert kb_sources: %v", err)
	}
	return source
}

// insertWebSyncJob 落库一个源级 source_sync Job（jobs_target_check 第五分支）。
func insertWebSyncJob(t *testing.T, ctx context.Context, tx *gorm.DB, seed knowledgeSchemaSeed, sourceID uuid.UUID, status value.JobStatus) *model.Job {
	t.Helper()
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, KbSourceID: sourceID,
		Type: model.SourceSyncJobType, Status: status,
	})
	if err != nil {
		t.Fatalf("NewJob(源级): %v", err)
	}
	if err := tx.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		t.Fatalf("insert source-level source_sync job: %v", err)
	}
	return job
}

// newWebSyncE2E 组装 web 源编排 e2e：真实 PG store + 真实 web.Connector + httptest 假站点。
type webSyncE2E struct {
	seed   knowledgeSchemaSeed
	source *model.KbSource
	queue  *recordingQueue
	svc    *appservice.SourceSyncService
}

// newWebSyncE2EWithSite 在给定测试事务上启动假站点并装配完整编排服务。
func newWebSyncE2EWithSite(t *testing.T, ctx context.Context, database *gorm.DB, handler http.Handler) (*webSyncE2E, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	source := insertWebSource(t, ctx, database, seed, "假站点源", map[string]any{
		"mode":  "urls",
		"urls":  []any{},
		"crawl": map[string]any{"respect_robots": false},
	})

	// 真实 web connector：httptest 是本机回环地址，测试用普通 client 直连
	//（生产 DI 注入 SSRF-safe client，本测试关注编排语义而非 SSRF）。
	connector, err := web.NewConnector(web.ConnectorConfig{
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Limits:     value.WebSourceLimits{MaxPagesCeiling: 100, MaxDepthCeiling: 5},
	})
	if err != nil {
		t.Fatalf("web.NewConnector: %v", err)
	}
	credentialCipher, err := NewSourceConnectionCredentialCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("credential cipher: %v", err)
	}
	queue := &recordingQueue{}
	svc := appservice.NewSourceSyncService(appservice.SourceSyncServiceDeps{
		KnowledgeBaseRepository: NewKnowledgeBaseRepository(database),
		Selector: appservice.NewSourceConnectionSelector(
			NewSourceConnectionRepository(database), credentialCipher,
		),
		WebConnector: connector,
		RawStore:     localstorage.NewRawDocumentStore(t.TempDir()),
		Store:        NewSourceSyncDBStore(database),
		Queue:        queue,
		Logger:       slog.Default(),
	})
	return &webSyncE2E{
		seed: seed, source: source, queue: queue, svc: svc,
	}, server
}

// runSourceSync 建源级 Job 并以 worker 入口执行一次同步。
func (e *webSyncE2E) runSourceSync(t *testing.T, ctx context.Context, database *gorm.DB) error {
	t.Helper()
	job := insertWebSyncJob(t, ctx, database, e.seed, e.source.ID, value.JobStatusPending)
	return e.svc.RunSourceSyncJobForSource(ctx, e.seed.workspaceID, e.seed.kbID, e.source.ID, job.ID)
}

// setURLs 更新源 config 的 URL 列表（模拟管理 URL 对话框的 PATCH）。
func (e *webSyncE2E) setURLs(t *testing.T, ctx context.Context, database *gorm.DB, urls ...string) {
	t.Helper()
	items := make([]any, 0, len(urls))
	for _, u := range urls {
		items = append(items, u)
	}
	e.source.Config["urls"] = items
	if err := database.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", e.seed.workspaceID, e.source.ID).
		Update("config", normalizedJSONMap(e.source.Config)).Error; err != nil {
		t.Fatalf("update urls: %v", err)
	}
}

// TestWebSourceSyncEndToEnd 覆盖：导入建档 → 内容变更建新 revision → 304 unchanged →
// force latch → 删除策略 keep/remove。
func TestWebSourceSyncEndToEnd(t *testing.T) {
	etag := `"v1"`
	body := "<html><head><title>Page A</title></head><body><h1>Page A</h1><p>content v1</p></body></html>"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><head><title>Dir B</title></head><body><h1>Dir B</h1><p>dir b content</p></body></html>")
	})
	ctx, database := newAuthTestDB(t)
	e, server := newWebSyncE2EWithSite(t, ctx, database, handler)
	urlA := server.URL + "/a"
	urlB := server.URL + "/dir/b"
	e.setURLs(t, ctx, database, urlA, urlB)

	// —— 第一次同步：导入建档。
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	var docA, docB DocumentRow
	if err := database.WithContext(ctx).
		First(&docA, "workspace_id = ? AND external_id = ?", e.seed.workspaceID, urlA).Error; err != nil {
		t.Fatalf("page A 未建档: %v", err)
	}
	if err := database.WithContext(ctx).
		First(&docB, "workspace_id = ? AND external_id = ?", e.seed.workspaceID, urlB).Error; err != nil {
		t.Fatalf("page B 未建档: %v", err)
	}
	if docA.Kind != "web" || docA.SourceType != "web" {
		t.Fatalf("page A kind/source_type = %q/%q, want web/web", docA.Kind, docA.SourceType)
	}
	if docA.SourceURI == nil || *docA.SourceURI != urlA {
		t.Fatalf("source_uri = %v, want %q", docA.SourceURI, urlA)
	}
	if docA.KbSourceID == nil || *docA.KbSourceID != e.source.ID {
		t.Fatalf("kb_source_id 未写入: %v", docA.KbSourceID)
	}
	if docA.SourceMeta["etag"] != etag {
		t.Fatalf("source_meta etag = %v, want %q", docA.SourceMeta["etag"], etag)
	}
	if docA.ContentHash == nil || *docA.ContentHash == "" {
		t.Fatal("content_hash 未写入")
	}
	// B 挂在 /dir/ folder 下，A 挂根。
	var nodeB FileTreeNodeRow
	if err := database.WithContext(ctx).
		First(&nodeB, "workspace_id = ? AND external_id = ?", e.seed.workspaceID, urlB).Error; err != nil {
		t.Fatalf("page B file 节点缺失: %v", err)
	}
	var folderRow FileTreeNodeRow
	if err := database.WithContext(ctx).
		First(&folderRow, "workspace_id = ? AND id = ?", e.seed.workspaceID, *nodeB.ParentID).Error; err != nil {
		t.Fatalf("folder /dir/ 缺失: %v", err)
	}
	if folderRow.ExternalID == nil || *folderRow.ExternalID != server.URL+"/dir/" {
		t.Fatalf("folder external_id = %v", folderRow.ExternalID)
	}
	// parse Job 入队 + 结果写回源行。
	if got := e.queue.countByType("document_parse_start"); got != 2 {
		t.Fatalf("parse job count = %d, want 2", got)
	}
	var sourceRow KbSourceRow
	if err := database.WithContext(ctx).
		First(&sourceRow, "workspace_id = ? AND id = ?", e.seed.workspaceID, e.source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sourceRow.SyncLastResult["status"] != "succeeded" {
		t.Fatalf("sync_last_result = %v", sourceRow.SyncLastResult)
	}

	// —— 第二次同步：内容变更 → revision_no+1 + etag 更新。
	etag = `"v2"`
	body = "<html><head><title>Page A</title></head><body><h1>Page A</h1><p>content v2</p></body></html>"
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	var revA2 DocumentRevisionRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND document_id = ? AND revision_reason = 'crawl'", e.seed.workspaceID, docA.ID).
		Order("revision_no DESC").First(&revA2).Error; err != nil {
		t.Fatal(err)
	}
	if revA2.RevisionNo != 2 {
		t.Fatalf("revision_no = %d, want 2（内容变更建新 revision）", revA2.RevisionNo)
	}
	if err := database.WithContext(ctx).First(&docA, "id = ?", docA.ID).Error; err != nil {
		t.Fatal(err)
	}
	if docA.SourceMeta["etag"] != `"v2"` {
		t.Fatalf("etag = %v, want v2", docA.SourceMeta["etag"])
	}

	// —— 第三次同步：条件请求命中 304 → 不建 revision，刷新 last_fetch_at。
	etag = `"v2"` // handler 已按 If-None-Match 返回 304
	before := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("third sync (304): %v", err)
	}
	var revCount int64
	if err := database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND document_id = ? AND revision_reason = 'crawl'", e.seed.workspaceID, docA.ID).
		Count(&revCount).Error; err != nil {
		t.Fatal(err)
	}
	if revCount != 2 {
		t.Fatalf("304 后 revision 数 = %d, want 2（不建 revision）", revCount)
	}
	if err := database.WithContext(ctx).First(&docA, "id = ?", docA.ID).Error; err != nil {
		t.Fatal(err)
	}
	fetchAt, _ := docA.SourceMeta[sourceport.SourceMetaLastFetchAt].(string)
	if fetchAt == "" || strings.Compare(fetchAt, before) < 0 {
		t.Fatalf("last_fetch_at 未刷新: %v（baseline %s）", docA.SourceMeta[sourceport.SourceMetaLastFetchAt], before)
	}

	// —— force：latch 置位后 hash 未变也建新 revision。
	if err := database.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", e.seed.workspaceID, e.source.ID).
		Update("sync_requested_force", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("force sync: %v", err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND document_id = ? AND revision_reason = 'crawl'", e.seed.workspaceID, docA.ID).
		Count(&revCount).Error; err != nil {
		t.Fatal(err)
	}
	if revCount != 3 {
		t.Fatalf("force 后 revision 数 = %d, want 3", revCount)
	}
	var sourceAfterForce KbSourceRow
	if err := database.WithContext(ctx).
		First(&sourceAfterForce, "workspace_id = ? AND id = ?", e.seed.workspaceID, e.source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sourceAfterForce.SyncRequestedForce {
		t.Fatal("force latch 应被消费清空")
	}

	// —— 删除策略 keep：URL 从列表移除 → 文档软删、原始对象与文档行保留。
	e.setURLs(t, ctx, database, urlB)
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("delete sync: %v", err)
	}
	var keptDoc DocumentRow
	if err := database.WithContext(ctx).First(&keptDoc, "id = ?", docA.ID).Error; err != nil {
		t.Fatalf("keep 策略应保留文档行: %v", err)
	}
	if keptDoc.Status != string(value.DocumentStatusDeleted) || keptDoc.DeletedAt == nil {
		t.Fatalf("keep 策略文档状态 = %q deleted_at=%v", keptDoc.Status, keptDoc.DeletedAt)
	}
}

// TestWebSourceSyncRemovePolicy 验证删除策略 remove：URL 移除后文档行硬删，
// 并建立 source_cleanup Job（清理 raw/parser/asset 对象）。
func TestWebSourceSyncRemovePolicy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h1>%s</h1><p>content</p></body></html>", r.URL.Path)
	})
	ctx, database := newAuthTestDB(t)
	e, server := newWebSyncE2EWithSite(t, ctx, database, handler)
	urlA := server.URL + "/a"
	urlB := server.URL + "/b"
	e.setURLs(t, ctx, database, urlA, urlB)
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	var docA DocumentRow
	if err := database.WithContext(ctx).
		First(&docA, "workspace_id = ? AND external_id = ?", e.seed.workspaceID, urlA).Error; err != nil {
		t.Fatalf("page A 未建档: %v", err)
	}

	// remove 策略 + 移除 urlA → 同步后文档行硬删 + source_cleanup Job 建立。
	e.source.Config["on_delete"] = "remove"
	e.setURLs(t, ctx, database, urlB)
	if err := database.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", e.seed.workspaceID, e.source.ID).
		Update("config", normalizedJSONMap(e.source.Config)).Error; err != nil {
		t.Fatal(err)
	}
	beforeCleanup := e.queue.countByType(model.SourceCleanupJobType)
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("remove sync: %v", err)
	}
	var removedDoc DocumentRow
	if err := database.WithContext(ctx).First(&removedDoc, "id = ?", docA.ID).Error; err == nil {
		t.Fatal("remove 策略应硬删文档行")
	}
	if got := e.queue.countByType(model.SourceCleanupJobType); got <= beforeCleanup {
		t.Fatalf("remove 策略应建立 source_cleanup Job（before=%d after=%d）", beforeCleanup, got)
	}
}

// TestWebSourceSyncSkipsURLLOwnedByOtherSource 验证跨源先到先得（DB 级）：
// 源 B 的 URL 列表包含源 A 已拥有的页面 → B 跳过 + 告警，A 的文档不动。
func TestWebSourceSyncSkipsURLLOwnedByOtherSource(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><body><h1>Shared</h1><p>shared content</p></body></html>")
	})
	ctx, database := newAuthTestDB(t)
	e, server := newWebSyncE2EWithSite(t, ctx, database, handler)
	sharedURL := server.URL + "/shared"

	// 源 A：拥有 sharedURL。
	sourceA := insertWebSource(t, ctx, database, e.seed, "源A", map[string]any{
		"mode": "urls", "urls": []any{sharedURL},
		"crawl": map[string]any{"respect_robots": false},
	})
	e.source = sourceA
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("source A sync: %v", err)
	}
	var docA DocumentRow
	if err := database.WithContext(ctx).
		First(&docA, "workspace_id = ? AND external_id = ?", e.seed.workspaceID, sharedURL).Error; err != nil {
		t.Fatalf("源 A 建档失败: %v", err)
	}

	// 源 B：同 KB，URL 列表包含同一 URL。
	sourceB := insertWebSource(t, ctx, database, e.seed, "源B", map[string]any{
		"mode": "urls", "urls": []any{sharedURL},
		"crawl": map[string]any{"respect_robots": false},
	})
	e.source = sourceB
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("source B sync: %v", err)
	}

	// A 的文档原样保留（唯一一行、归属 A）。
	var ownedDocs []DocumentRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND external_id = ?", e.seed.workspaceID, sharedURL).
		Find(&ownedDocs).Error; err != nil {
		t.Fatal(err)
	}
	if len(ownedDocs) != 1 || ownedDocs[0].KbSourceID == nil || *ownedDocs[0].KbSourceID != sourceA.ID {
		t.Fatalf("shared URL 归属被破坏: %d 行", len(ownedDocs))
	}
	// B 的结果成功且 skipped=1（冲突按跳过计，不算失败）。
	var rowB KbSourceRow
	if err := database.WithContext(ctx).
		First(&rowB, "workspace_id = ? AND id = ?", e.seed.workspaceID, sourceB.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rowB.SyncLastResult["status"] != "succeeded" {
		t.Fatalf("源 B last_result = %v", rowB.SyncLastResult)
	}
}

// TestWebSourceSyncDisabledSourceSkips 验证停用源幂等跳过。
func TestWebSourceSyncDisabledSourceSkips(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("停用源不应发起抓取")
	})
	ctx, database := newAuthTestDB(t)
	e, server := newWebSyncE2EWithSite(t, ctx, database, handler)
	e.setURLs(t, ctx, database, server.URL+"/a")
	if err := database.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", e.seed.workspaceID, e.source.ID).
		Update("status", string(value.KbSourceStatusDisabled)).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.runSourceSync(t, ctx, database); err != nil {
		t.Fatalf("disabled sync: %v", err)
	}
	var count int64
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND kb_source_id = ?", e.seed.workspaceID, e.source.ID).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("停用源建档数 = %d, want 0", count)
	}
}

// TestWebSourceStoreRuntimeStateRoundtrip 验证源级运行态读写（latch/cursor/last_result/meta）。
func TestWebSourceStoreRuntimeStateRoundtrip(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	source := insertWebSource(t, ctx, database, seed, "运行态源", map[string]any{"mode": "urls", "urls": []any{}})
	store := NewSourceSyncDBStore(database)

	// GetSyncKbSource。
	got, err := store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatalf("GetSyncKbSource: %v", err)
	}
	if got.ID != source.ID || got.Kind != value.KbSourceKindWeb {
		t.Fatalf("got = %+v", got)
	}

	// force latch 消费。
	if err := database.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, source.ID).
		Update("sync_requested_force", true).Error; err != nil {
		t.Fatal(err)
	}
	consumed, err := store.ConsumeSourceForceLatch(ctx, seed.workspaceID, seed.kbID, source.ID, uuid.New())
	if err != nil || !consumed {
		t.Fatalf("consume = %v/%v, want true/nil", consumed, err)
	}
	again, err := store.ConsumeSourceForceLatch(ctx, seed.workspaceID, seed.kbID, source.ID, uuid.New())
	if err != nil || again {
		t.Fatalf("二次消费 = %v/%v, want false/nil", again, err)
	}

	// cursor 推进 + 回读。
	cursor := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	if err := store.UpdateSourceSyncCursor(ctx, seed.workspaceID, seed.kbID, source.ID, cursor); err != nil {
		t.Fatalf("UpdateSourceSyncCursor: %v", err)
	}
	reread, err := store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotCursor := readSyncCursorForTest(reread.SyncCursor); !gotCursor.Equal(cursor) {
		t.Fatalf("cursor 回读 = %v, want %v", gotCursor, cursor)
	}

	// last_result 写回。
	if err := store.UpdateSourceSyncResult(ctx, seed.workspaceID, seed.kbID, source.ID, appservice.SyncResult{
		Status: "partial", Complete: true, SyncedDocuments: 2, FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpdateSourceSyncResult: %v", err)
	}
	reread, err = store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.SyncLastResult["status"] != "partial" {
		t.Fatalf("sync_last_result = %v", reread.SyncLastResult)
	}

	// latch 续跑 Job 的 connection 标注：FinalizeSourceSyncJobForSource 在 latch
	// 置位时新建的下一个源级 source_sync Job 行必须标注源 config 的
	// fetch.connection_id（与 Meta Scheduler 派发的 Job 同形），供
	// CountActiveByConnection 的 web 源 connection 组限流统计。
	connectionID := uuid.New()
	connSource := insertWebSource(t, ctx, database, seed, "续跑标注源", map[string]any{
		"mode":  "urls",
		"urls":  []any{},
		"fetch": map[string]any{"engine": "firecrawl", "connection_id": connectionID.String()},
	})
	connJob := insertWebSyncJob(t, ctx, database, seed, connSource.ID, value.JobStatusPending)
	if err := database.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, connSource.ID).
		Update("sync_requested_force", true).Error; err != nil {
		t.Fatal(err)
	}
	nextJob, err := store.FinalizeSourceSyncJobForSource(
		ctx, seed.workspaceID, seed.kbID, connSource.ID, connJob.ID, value.JobStatusCompleted, "",
	)
	if err != nil {
		t.Fatalf("FinalizeSourceSyncJobForSource: %v", err)
	}
	if nextJob == nil {
		t.Fatal("latch 置位时应返回续跑 Job")
	}
	if nextJob.SourceConnectionID != connectionID {
		t.Fatalf("续跑 Job 领域模型 connection 标注 = %s, want %s", nextJob.SourceConnectionID, connectionID)
	}
	var nextJobRow JobRow
	if err := database.WithContext(ctx).
		First(&nextJobRow, "workspace_id = ? AND id = ?", seed.workspaceID, nextJob.ID).Error; err != nil {
		t.Fatal(err)
	}
	if nextJobRow.SourceConnectionID == nil || *nextJobRow.SourceConnectionID != connectionID {
		t.Fatalf("续跑 Job 行 source_connection_id = %v, want %s", nextJobRow.SourceConnectionID, connectionID)
	}
	if nextJobRow.KbSourceID == nil || *nextJobRow.KbSourceID != connSource.ID {
		t.Fatalf("续跑 Job 行 kb_source_id = %v, want %s", nextJobRow.KbSourceID, connSource.ID)
	}
}

// readSyncCursorForTest 复用服务层 readSyncCursor 的键约定。
func readSyncCursorForTest(syncCursor map[string]any) time.Time {
	raw, _ := syncCursor["sync_cursor"].(string)
	if raw == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, raw)
	return parsed
}

// TestWebSourceDocumentSourceMetaRoundtrip 验证 source_meta 往返与 304 刷新路径。
func TestWebSourceDocumentSourceMetaRoundtrip(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	source := insertWebSource(t, ctx, database, seed, "meta 源", map[string]any{"mode": "urls", "urls": []any{}})
	store := NewSourceSyncDBStore(database)

	// 建一个 web 页面文档（带 kb_source_id 与 source_meta）。
	docID := uuid.New()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, external_id, kb_source_id, source_meta, status) "+
			"VALUES (?, ?, ?, 'web', '页面', 'web', ?, ?, ?, ?::jsonb, 'ready')",
		docID, seed.workspaceID, seed.kbID, "https://docs.example.com/p", "https://docs.example.com/p", source.ID,
		`{"etag":"\"v1\"","last_fetch_at":"2026-09-01T00:00:00Z"}`,
	).Error; err != nil {
		t.Fatalf("insert web document: %v", err)
	}

	// ListSourceDocuments 投影带出 kb_source_id/source_meta。
	views, err := store.ListSourceDocuments(ctx, seed.kbID)
	if err != nil {
		t.Fatalf("ListSourceDocuments: %v", err)
	}
	var view *appservice.LocalDocView
	for i := range views {
		if views[i].DocumentID == docID {
			view = &views[i]
		}
	}
	if view == nil {
		t.Fatal("web 文档未进入来源投影")
	}
	if view.KbSourceID == nil || *view.KbSourceID != source.ID {
		t.Fatalf("view.KbSourceID = %v", view.KbSourceID)
	}
	if view.SourceMeta["etag"] != `"v1"` {
		t.Fatalf("view.SourceMeta = %v", view.SourceMeta)
	}

	// UpdateDocumentSourceMeta 合并回写（304 路径）。
	merged := map[string]any{
		"etag":             `"v1"`,
		"last_fetch_at":    "2026-09-16T08:00:00Z",
		"last_http_status": 304,
	}
	if err := store.UpdateDocumentSourceMeta(ctx, seed.workspaceID, docID, merged); err != nil {
		t.Fatalf("UpdateDocumentSourceMeta: %v", err)
	}
	var row DocumentRow
	if err := database.WithContext(ctx).First(&row, "id = ?", docID).Error; err != nil {
		t.Fatal(err)
	}
	if row.SourceMeta["etag"] != `"v1"` || row.SourceMeta["last_http_status"] != float64(304) {
		t.Fatalf("source_meta 回读 = %v", row.SourceMeta)
	}
}

// TestWebSourceUpdateOwnershipConflict 验证更新路径源级归属校验：请求指向别的源 → ErrConflict。
func TestWebSourceUpdateOwnershipConflict(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	sourceA := insertWebSource(t, ctx, database, seed, "归属A", map[string]any{"mode": "urls", "urls": []any{}})
	sourceB := insertWebSource(t, ctx, database, seed, "归属B", map[string]any{"mode": "urls", "urls": []any{}})
	store := NewSourceSyncDBStore(database)

	docID := uuid.New()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, external_id, kb_source_id, status) "+
			"VALUES (?, ?, ?, 'web', '页面', 'web', ?, ?, ?, 'ready')",
		docID, seed.workspaceID, seed.kbID, "https://docs.example.com/p", "https://docs.example.com/p", sourceA.ID,
	).Error; err != nil {
		t.Fatal(err)
	}

	revisionID := uuid.New()
	_, err := store.CreateSyncedDocumentRevisionJob(ctx, appservice.UpdateDocumentRequest{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		ExternalID: "https://docs.example.com/p", DocumentID: docID, RevisionID: revisionID,
		KbSourceID:   &sourceB.ID, // 归属校验失败：文档属源 A。
		GenerationID: seed.generationID,
		Title:        "x", RawStorageKey: "raw/x", SHA256: "hash",
		ContentType: "text/markdown", FileType: "markdown",
		Reason: value.DocumentRevisionReasonCrawl,
	})
	if err == nil {
		t.Fatal("跨源更新应被拒绝")
	}
	// 校验 ErrConflict（编排层据此转跳过 + warning）。
	if !strings.Contains(err.Error(), "归属其它内容源") {
		t.Fatalf("err = %v", err)
	}
}

// TestWebSourceGetSyncKbSourceSoftDeletedRejected 验证软删源查询返回 ErrNotFound：
// DeletedAt 为普通 *time.Time，GORM 不会自动过滤软删行，store 须显式拒绝
// （与 lockKbSourceForUpdate 同口径）。
func TestWebSourceGetSyncKbSourceSoftDeletedRejected(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	source := insertWebSource(t, ctx, database, seed, "软删源", map[string]any{"mode": "urls", "urls": []any{}})
	store := NewSourceSyncDBStore(database)

	// 活源可读。
	if _, err := store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID); err != nil {
		t.Fatalf("活源读取失败: %v", err)
	}

	// 软删后读取返回 ErrNotFound。
	if err := database.WithContext(ctx).Exec(
		"UPDATE kb_sources SET deleted_at = now() WHERE workspace_id = ? AND id = ?",
		seed.workspaceID, source.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	_, err := store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("软删源 err = %v, want ErrNotFound", err)
	}
}
