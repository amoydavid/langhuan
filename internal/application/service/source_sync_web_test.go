package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	webadapter "github.com/dajee/langhuan/internal/adapters/source/web"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件覆盖 web 内容源编排（source_sync_web.go）：
//   - 页面导入：kind=web + kb_source_id + source_meta + 同事务 content_hash；
//   - 跨源冲突（url_owned_by_other_source，先到先得，不覆盖不删除）；
//   - 失败分类映射（blocked/robots/skipped_non_html 非重试；retryable/oversize；引擎未接入 fatal）；
//   - 304 unchanged（不建 revision、source_meta 合并回写）与 hash 未变跳过；
//   - force latch 消费与运行期重置位（下一 Job 源级入队）；
//   - 删除策略 keep/remove 按源 config 执行；
//   - 单趟父先序：目录索引页（页面节点）作为 folder 链父可解析（T7 编排义务）。

// webNode/pageNode/webFolderNode 构造 web 快照节点（ObjType=page，Token=规范化 URL）。
func webPageNode(token, parent, title string) model.ExternalNode {
	return model.ExternalNode{Token: token, ParentToken: parent, Title: title, ObjType: "page", HasDocument: true}
}

func webFolderNode(token, parent string) model.ExternalNode {
	return model.ExternalNode{Token: token, ParentToken: parent, Title: "folder", ObjType: "folder", HasDocument: false}
}

type webSyncHarness struct {
	workspaceID uuid.UUID
	kb          *model.KnowledgeBase
	store       *fakeSourceSyncStore
	queue       *fakeSyncQueue
	raw         *fakeSyncRawStore
	logger      *recordingLogger
	connector   *fakeSourceConnector
	source      *model.KbSource
	svc         *SourceSyncService
}

// newWebSyncHarness 组装最小 web 源编排环境：upload KB + urls 模式源 + fake web connector。
func newWebSyncHarness(t *testing.T, tree []model.ExternalNode) *webSyncHarness {
	t.Helper()
	workspaceID := uuid.New()
	kb, err := model.NewKnowledgeBaseWithSource(
		workspaceID, "web-kb", "", uuid.New(), nil, nil,
		value.SourceTypeUpload, map[string]any{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	generationID := uuid.New()
	kb.FileTreeRootID = uuid.New()
	kb.ActiveIndexGenerationID = &generationID

	store := newFakeSourceSyncStore(kb)
	store.nodes[kb.FileTreeRootID] = &model.FileTreeNode{
		ID: kb.FileTreeRootID, WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID,
		NodeType: value.FileTreeNodeRoot,
	}
	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID,
		Kind: value.KbSourceKindWeb, Name: "产品文档站",
		Config: map[string]any{"mode": "urls", "urls": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.PutKbSource(source)

	logger := &recordingLogger{}
	raw := &fakeSyncRawStore{}
	queue := &fakeSyncQueue{}
	connector := &fakeSourceConnector{tree: tree, provider: webadapter.ProviderName}
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: kb},
		Selector:                NewSourceConnectionSelector(&fakeSyncConnRepo{}, passthroughCipher{}),
		Connector:               &fakeSourceConnector{},
		WebConnector:            connector,
		RawStore:                raw,
		Store:                   store,
		Queue:                   queue,
		Logger:                  logger,
	})
	return &webSyncHarness{
		workspaceID: workspaceID, kb: kb, store: store, queue: queue, raw: raw,
		logger: logger, connector: connector, source: source, svc: svc,
	}
}

// runSync 以源级 Job 入口执行一次同步（默认消费 latch=false）。
func (h *webSyncHarness) runSync(t *testing.T) error {
	t.Helper()
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID, KbSourceID: h.source.ID,
		Type: model.SourceSyncJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.store.jobs[job.ID] = job
	return h.svc.RunSourceSyncJobForSource(context.Background(), h.workspaceID, h.kb.ID, h.source.ID, job.ID)
}

// webDocumentByExternalID 按external_id 取本源建档后的 Document。
func (h *webSyncHarness) webDocumentByExternalID(externalID string) *model.Document {
	for _, doc := range h.store.documents {
		if doc.ExternalID == externalID {
			return doc
		}
	}
	return nil
}

// seedWebDocument 预置一个归属某源的既有页面文档（含 crawl revision 与 file 节点）。
func (h *webSyncHarness) seedWebDocument(t *testing.T, ownerSourceID uuid.UUID, externalID, contentHash string, meta map[string]any) *model.Document {
	t.Helper()
	doc, err := model.NewDocumentIdentityWithExternal(
		h.workspaceID, h.kb.ID, value.DocumentKindWeb, "旧标题",
		string(value.KbSourceKindWeb), externalID, externalID, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	owner := ownerSourceID
	doc.KbSourceID = &owner
	doc.ContentHash = contentHash
	doc.SourceMeta = meta
	h.store.documents[doc.ID] = doc
	h.store.existingDocuments = append(h.store.existingDocuments, doc)

	revisionID := uuid.New()
	rev, err := model.NewDocumentRevisionWithID(revisionID, model.NewDocumentRevisionInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID, DocumentID: doc.ID,
		Kind: value.DocumentKindWeb, DocumentKind: value.DocumentKindWeb,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonCrawl,
		ContentType:   "text/markdown",
		RawStorageKey: "raw/" + doc.ID.String(), SHA256: contentHash,
		ProcessingVersion: model.CurrentProcessingVersion,
		Status:            value.DocumentRevisionReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.store.revisions[rev.ID] = rev

	nodeID := uuid.New()
	docID := doc.ID
	h.store.nodes[nodeID] = &model.FileTreeNode{
		ID: nodeID, WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		NodeType: value.FileTreeNodeFile, Name: "旧标题",
		DocumentID: &docID, ExternalID: externalID,
	}
	return doc
}

// TestSyncWebSourceImportsPages 验证 web 源首次同步：建档 kind=web/kb_source_id/
// source_meta/content_hash，folder/page 父子定位，parse Job 入队，结果写回源行。
func TestSyncWebSourceImportsPages(t *testing.T) {
	const (
		root   = "https://docs.example.com/"
		apiURL = "https://docs.example.com/api/x"
	)
	h := newWebSyncHarness(t, []model.ExternalNode{
		webFolderNode("https://docs.example.com/api/", ""),
		webPageNode(apiURL, "https://docs.example.com/api/", "API"),
	})
	h.connector.fetchFn = func(externalID string) (model.FetchedDocument, error) {
		return model.FetchedDocument{
			Markdown: []byte("# x\n内容"), Title: "API", ObjType: "page",
			Meta: map[string]any{"etag": `"v1"`, "last_engine": "http", "last_http_status": 200},
		}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("RunSourceSyncJobForSource err = %v", err)
	}

	doc := h.webDocumentByExternalID(apiURL)
	if doc == nil {
		t.Fatal("web 页面未建档")
	}
	if doc.Kind != value.DocumentKindWeb {
		t.Fatalf("kind = %q, want web", doc.Kind)
	}
	if doc.SourceURI != apiURL {
		t.Fatalf("source_uri = %q, want %q", doc.SourceURI, apiURL)
	}
	if doc.KbSourceID == nil || *doc.KbSourceID != h.source.ID {
		t.Fatalf("kb_source_id 未写入: %v", doc.KbSourceID)
	}
	if doc.SourceMeta["etag"] != `"v1"` {
		t.Fatalf("source_meta etag = %v", doc.SourceMeta["etag"])
	}
	if doc.ContentHash == "" {
		t.Fatal("content_hash 未写入")
	}
	// folder 节点建立 + 页面挂载到 folder。
	folder, ok := h.store.folderByExternal["https://docs.example.com/api/"]
	if !ok {
		t.Fatal("folder 节点未建立")
	}
	var pageNode *model.FileTreeNode
	for _, node := range h.store.nodes {
		if node.ExternalID == apiURL {
			pageNode = node
		}
	}
	if pageNode == nil || pageNode.ParentID == nil || *pageNode.ParentID != folder {
		t.Fatalf("页面未挂载到 folder: node=%v", pageNode)
	}
	// parse Job 入队（document_parse_start）。
	jobCount := 0
	for _, job := range h.store.jobs {
		if job.Type == "document_parse_start" && job.Status == value.JobStatusPending {
			jobCount++
		}
	}
	if jobCount != 1 {
		t.Fatalf("parse job count = %d, want 1", jobCount)
	}
	// 结果写回源行：succeeded + synced=1。
	result, ok := h.store.sourceSyncResults[h.source.ID]
	if !ok {
		t.Fatal("sync_last_result 未写回源行")
	}
	if result.Status != "succeeded" || result.SyncedDocuments != 1 {
		t.Fatalf("result = %+v", result)
	}
}

// TestSyncWebSourceURLOwnedByOtherSource 验证跨源冲突：同 KB 两源 diff，
// URL 已属另一源 → 跳过 + warning，不更新不删除（先到先得，spec §3.2）。
func TestSyncWebSourceURLOwnedByOtherSource(t *testing.T) {
	const apiURL = "https://docs.example.com/api/x"
	otherSource, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		Kind: value.KbSourceKindWeb, Name: "另一源",
	})
	if err != nil {
		t.Fatal(err)
	}

	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(apiURL, "", "API")})
	h.connector.fetchFn = func(externalID string) (model.FetchedDocument, error) {
		return model.FetchedDocument{Markdown: []byte("新内容"), Title: "API 新"}, nil
	}
	seeded := h.seedWebDocument(t, otherSource.ID, apiURL, "hash-old", nil)

	if err := h.runSync(t); err != nil {
		t.Fatalf("RunSourceSyncJobForSource err = %v", err)
	}

	// 不覆盖：内容 hash 与标题保持原状；不删除：文档仍存在。
	if got := h.store.documents[seeded.ID]; got.ContentHash != "hash-old" || got.Title != "旧标题" {
		t.Fatalf("他源文档被覆盖: hash=%q title=%q", got.ContentHash, got.Title)
	}
	// 本源没有建档。
	if doc := h.webDocumentByExternalID(apiURL); doc != nil && doc.ID != seeded.ID {
		t.Fatal("冲突 URL 被重复建档")
	}
	// 跳过 + warning：结果 succeeded 且 skipped=1，告警含 url_owned_by_other_source。
	result := h.store.sourceSyncResults[h.source.ID]
	if result.Status != "succeeded" || result.SkippedDocuments != 1 {
		t.Fatalf("result = %+v, want skipped=1/succeeded", result)
	}
	found := false
	for _, w := range h.logger.warnings {
		if strings.Contains(w.msg, "占用") || strings.Contains(fmt.Sprint(w.fields), webWarningURLOwnedByOtherSource) {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 url_owned_by_other_source 告警, warnings=%d", len(h.logger.warnings))
	}
}

// TestSyncWebSourceFailureClassification 表驱动验证失败分类映射（spec §6.5）。
func TestSyncWebSourceFailureClassification(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantStatus   string
		wantFailed   int
		wantSkipped  int
		wantOversize int
		fatal        bool
	}{
		{
			name:       "blocked 401 跳过不算失败",
			err:        &webadapter.PageError{Outcome: webadapter.OutcomeBlocked, Status: 401, Err: errors.New("401")},
			wantStatus: "succeeded", wantSkipped: 1,
		},
		{
			name:       "robots 禁止跳过不算失败",
			err:        &webadapter.PageError{Outcome: webadapter.OutcomeRobotsDisallowed, Err: errors.New("robots")},
			wantStatus: "succeeded", wantSkipped: 1,
		},
		{
			name:       "非 HTML 跳过不算失败",
			err:        &webadapter.PageError{Outcome: webadapter.OutcomeSkippedNonHTML, Status: 200, Err: errors.New("pdf")},
			wantStatus: "succeeded", wantSkipped: 1,
		},
		{
			name:       "5xx 走可重试失败",
			err:        &webadapter.PageError{Outcome: webadapter.OutcomeRetryable, Status: 503, Err: errors.New("503")},
			wantStatus: "partial", wantFailed: 1,
		},
		{
			name:       "超限走 oversize（结果计数，整体不算 failed——与飞书同语义）",
			err:        fmt.Errorf("wrapped: %w", sourceport.ErrSourceContentTooLarge),
			wantStatus: "succeeded", wantOversize: 1,
		},
		{
			name:       "引擎未接入判源配置性 failed",
			err:        fmt.Errorf("%w: 引擎尚未接入", domainerrors.ErrValidation),
			wantStatus: "failed", fatal: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const pageURL = "https://docs.example.com/page"
			h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "P")})
			h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
				return model.FetchedDocument{}, tc.err
			}
			err := h.runSync(t)
			if tc.fatal {
				if err == nil || !errors.Is(err, domainerrors.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation 链（worker SkipRetry）", err)
				}
			} else if err != nil {
				t.Fatalf("err = %v, want nil（节点级失败不终止同步）", err)
			}
			result := h.store.sourceSyncResults[h.source.ID]
			if result.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", result.Status, tc.wantStatus)
			}
			if result.FailedDocuments != tc.wantFailed || result.OversizeDocuments != tc.wantOversize {
				t.Fatalf("failed/oversize = %d/%d, want %d/%d",
					result.FailedDocuments, result.OversizeDocuments, tc.wantFailed, tc.wantOversize)
			}
			if result.SkippedDocuments != tc.wantSkipped {
				t.Fatalf("skipped = %d, want %d", result.SkippedDocuments, tc.wantSkipped)
			}
		})
	}
}

// TestSyncWebSourceNotModified304 验证 304 unchanged：不建 revision、
// last_fetch_at 刷新且旧 etag 保留（source_meta 合并语义，spec §8）。
func TestSyncWebSourceNotModified304(t *testing.T) {
	const pageURL = "https://docs.example.com/page"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "P")})
	seeded := h.seedWebDocument(t, h.source.ID, pageURL, "hash-1", map[string]any{
		"etag": `"old-etag"`, "last_modified": "Tue, 01 Sep 2026 00:00:00 GMT",
		"last_fetch_at": "2026-09-01T00:00:00Z",
	})
	h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
		return model.FetchedDocument{
			NotModified: true, ObjType: "page",
			Meta: map[string]any{
				"last_engine": "http", "last_fetch_at": "2026-09-16T08:00:00Z",
				"last_http_status": 304,
			},
		}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}

	if got := h.latestRevisionCount(seeded.ID); got != 1 {
		t.Fatalf("revision 数 = %d, want 1（304 不建 revision）", got)
	}
	merged := h.store.lastSourceMetaWrites[seeded.ID]
	if merged == nil {
		t.Fatal("source_meta 未回写")
	}
	if merged["etag"] != `"old-etag"` || merged["last_modified"] != "Tue, 01 Sep 2026 00:00:00 GMT" {
		t.Fatalf("旧条件请求凭据丢失: %v", merged)
	}
	if merged["last_fetch_at"] != "2026-09-16T08:00:00Z" {
		t.Fatalf("last_fetch_at 未刷新: %v", merged)
	}
	result := h.store.sourceSyncResults[h.source.ID]
	if result.SkippedDocuments != 1 || result.Status != "succeeded" {
		t.Fatalf("result = %+v", result)
	}
}

// TestSyncWebSourceHashUnchangedSkipsButRefreshesMeta 验证 hash 未变跳过仍刷新 source_meta。
func TestSyncWebSourceHashUnchangedSkipsButRefreshesMeta(t *testing.T) {
	const pageURL = "https://docs.example.com/page"
	body := "稳定内容"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "P")})
	seeded := h.seedWebDocument(t, h.source.ID, pageURL, contentHashOf([]byte(body)), nil)
	h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
		return model.FetchedDocument{
			Markdown: []byte(body), Title: "P",
			Meta: map[string]any{"etag": `"fresh"`, "last_engine": "http"},
		}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := h.latestRevisionCount(seeded.ID); got != 1 {
		t.Fatalf("revision 数 = %d, want 1（hash 未变不建 revision）", got)
	}
	if got := h.store.lastSourceMetaWrites[seeded.ID]["etag"]; got != `"fresh"` {
		t.Fatalf("source_meta etag = %v, want 刷新", got)
	}
}

// TestSyncWebSourceUpdateWritesSourceMetaInTransaction 验证内容变更路径：
// revision_no+1 且 source_meta/kb_source_id 归属校验同事务写入。
// 同时钉住：非 force 的 update 必须回传条件请求头（取自 source_meta）。
func TestSyncWebSourceUpdateWritesSourceMetaInTransaction(t *testing.T) {
	const pageURL = "https://docs.example.com/page"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "P")})
	seeded := h.seedWebDocument(t, h.source.ID, pageURL, "hash-old",
		map[string]any{"etag": `"old"`, "last_modified": "Tue, 01 Sep 2026 00:00:00 GMT"})
	h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
		return model.FetchedDocument{
			Markdown: []byte("全新内容"), Title: "P v2",
			Meta: map[string]any{"etag": `"new"`},
		}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := h.latestRevisionCount(seeded.ID); got != 2 {
		t.Fatalf("revision 数 = %d, want 2", got)
	}
	// 非 force：条件请求头按 source_meta 回传。
	if got := h.connector.lastOptions.IfNoneMatch; got != `"old"` {
		t.Fatalf("IfNoneMatch = %q, want \"old\"", got)
	}
	if got := h.connector.lastOptions.IfModifiedSince; got != "Tue, 01 Sep 2026 00:00:00 GMT" {
		t.Fatalf("IfModifiedSince = %q, want Last-Modified 回传", got)
	}
	// 更新路径经 CreateSyncedDocumentRevisionJob 同事务写入 source_meta。
	merged := h.store.documents[seeded.ID].SourceMeta
	if merged["etag"] != `"new"` {
		t.Fatalf("source_meta etag = %v, want \"new\"", merged["etag"])
	}
}

// TestSyncWebSourceUpdateConflictWarns 验证 update 路径跨源冲突（ErrConflict）：
// 跳过 + warning url_owned_by_other_source，不算失败（spec §3.2，与 add 路径同语义）。
func TestSyncWebSourceUpdateConflictWarns(t *testing.T) {
	const pageURL = "https://docs.example.com/page"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "P")})
	seeded := h.seedWebDocument(t, h.source.ID, pageURL, "hash-old", nil)
	h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
		return model.FetchedDocument{Markdown: []byte("全新内容"), Title: "P v2"}, nil
	}
	// 模拟同事务归属校验失败：URL 已被其它源占用（跨源竞态兜底路径）。
	h.store.createRevErr = fmt.Errorf("%w: 更新同步文档归属其它内容源（url_owned_by_other_source）", domainerrors.ErrConflict)

	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}

	// 冲突按跳过计，不算失败；revision 不变。
	if got := h.latestRevisionCount(seeded.ID); got != 1 {
		t.Fatalf("revision 数 = %d, want 1（冲突不建 revision）", got)
	}
	result := h.store.sourceSyncResults[h.source.ID]
	if result.Status != "succeeded" || result.SkippedDocuments != 1 || result.FailedDocuments != 0 {
		t.Fatalf("result = %+v, want skipped=1/failed=0/succeeded", result)
	}
	// 告警含 url_owned_by_other_source。
	found := false
	for _, w := range h.logger.warnings {
		if strings.Contains(w.msg, "冲突") || strings.Contains(fmt.Sprint(w.fields), webWarningURLOwnedByOtherSource) {
			found = true
		}
	}
	if !found {
		t.Fatalf("update 路径冲突缺少 url_owned_by_other_source 告警, warnings=%d", len(h.logger.warnings))
	}
}

// TestSyncWebSourceForceLatch 验证源级 force latch：hash 未变也建新 revision；
// 运行期间 latch 重置位时创建下一个源级 Job 并入队。
// 同时钉住：force 绕过条件请求（不回传 IfNoneMatch/IfModifiedSince），
// 否则 304 短路会让「全量重抓 + hash 判断」无法落地。
func TestSyncWebSourceForceLatch(t *testing.T) {
	const pageURL = "https://docs.example.com/page"
	body := "稳定内容"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "P")})
	seeded := h.seedWebDocument(t, h.source.ID, pageURL, contentHashOf([]byte(body)),
		map[string]any{"etag": `"stale-etag"`, "last_modified": "Tue, 01 Sep 2026 00:00:00 GMT"})
	h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
		return model.FetchedDocument{Markdown: []byte(body), Title: "P"}, nil
	}
	// 运行期间把 latch 重新置位（模拟并发 force 请求），Finalize 时应产出下一个 Job。
	h.connector.listTreeHook = func() { h.store.sourceForceLatch[h.source.ID] = true }
	h.store.sourceForceLatch[h.source.ID] = true

	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID, KbSourceID: h.source.ID,
		Type: model.SourceSyncJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.store.jobs[job.ID] = job
	if err := h.svc.RunSourceSyncJobForSource(context.Background(), h.workspaceID, h.kb.ID, h.source.ID, job.ID); err != nil {
		t.Fatalf("err = %v", err)
	}
	// force：hash 未变也建新 revision（revision_no=2）。
	if got := h.latestRevisionCount(seeded.ID); got != 2 {
		t.Fatalf("revision 数 = %d, want 2（force 全量重抓）", got)
	}
	// force 不回传条件请求头（即使 source_meta 持有 etag/last_modified）。
	if got := h.connector.lastOptions.IfNoneMatch; got != "" {
		t.Fatalf("force 不应回传 IfNoneMatch, got %q", got)
	}
	if got := h.connector.lastOptions.IfModifiedSince; got != "" {
		t.Fatalf("force 不应回传 IfModifiedSince, got %q", got)
	}
	// 后续 Job：源级目标 + source_id 载荷。
	var enqueuedSourceSync int
	for _, req := range h.queue.requests {
		if req.Type == model.SourceSyncJobType && strings.Contains(req.TaskID, h.source.ID.String()) {
			enqueuedSourceSync++
			if !strings.Contains(string(req.Payload), h.source.ID.String()) {
				t.Fatalf("后续任务 payload 缺少 source_id: %s", req.Payload)
			}
		}
	}
	if enqueuedSourceSync != 1 {
		t.Fatalf("源级后续任务入队数 = %d, want 1", enqueuedSourceSync)
	}
	if h.store.sourceForceLatch[h.source.ID] {
		t.Fatal("latch 应被消费清空")
	}
}

// TestSyncWebSourceDeletePolicies 验证删除按源 config 的 on_delete 执行（keep/remove）。
func TestSyncWebSourceDeletePolicies(t *testing.T) {
	const (
		keptURL    = "https://docs.example.com/kept"
		removedURL = "https://docs.example.com/removed"
	)
	for _, tc := range []struct {
		policy string
		want   value.SourceDeletePolicy
	}{
		{policy: "keep", want: value.SourceDeleteKeep},
		{policy: "remove", want: value.SourceDeleteRemove},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(keptURL, "", "K")})
			cfg := map[string]any{"mode": "urls", "on_delete": tc.policy}
			h.source.Config = cfg
			h.seedWebDocument(t, h.source.ID, removedURL, "hash-x", nil)
			h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
				return model.FetchedDocument{Markdown: []byte("k")}, nil
			}
			if err := h.runSync(t); err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(h.store.deleteCalls) != 1 {
				t.Fatalf("delete calls = %d, want 1", len(h.store.deleteCalls))
			}
			if h.store.deleteCalls[0].Policy != tc.want {
				t.Fatalf("policy = %v, want %v", h.store.deleteCalls[0].Policy, tc.want)
			}
		})
	}
}

// TestSyncWebSourceIndexPageAsFolderParent 验证单趟父先序编排义务：
// 目录索引页（页面节点）作为 folder 链的父——更新/跳过路径的索引页 Token
// 也必须可解析（T7 评审移交）。
func TestSyncWebSourceIndexPageAsFolderParent(t *testing.T) {
	const (
		indexPage = "https://docs.example.com/help/"      // 目录索引页（更新路径）
		apiDir    = "https://docs.example.com/help/api/"  // folder，父为索引页
		apiDoc    = "https://docs.example.com/help/api/x" // 新增页
	)
	h := newWebSyncHarness(t, []model.ExternalNode{
		webPageNode(indexPage, "", "Help"),
		webFolderNode(apiDir, indexPage),
		webPageNode(apiDoc, apiDir, "X"),
	})
	// 索引页 hash 未变 → 走跳过路径（跳过路径不新建节点，Token 必须靠预注册可解析）。
	seeded := h.seedWebDocument(t, h.source.ID, indexPage, contentHashOf([]byte("x")), nil)
	h.connector.fetchFn = func(externalID string) (model.FetchedDocument, error) {
		return model.FetchedDocument{Markdown: []byte("x"), Title: externalID}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}
	// 索引页 file 节点（预注册来源）。
	indexNodeID := uuid.Nil
	for _, node := range h.store.nodes {
		if node.ExternalID == indexPage && node.DocumentID != nil && *node.DocumentID == seeded.ID {
			indexNodeID = node.ID
		}
	}
	if indexNodeID == uuid.Nil {
		t.Fatal("索引页 file 节点缺失")
	}
	// folder /help/api/ 的父必须是索引页文件节点。
	folderID, ok := h.store.folderByExternal[apiDir]
	if !ok {
		t.Fatal("folder /help/api/ 未建立（索引页父定位失败）")
	}
	if h.store.nodes[folderID].ParentID == nil || *h.store.nodes[folderID].ParentID != indexNodeID {
		t.Fatalf("folder 父 = %v, want 索引页节点 %s", h.store.nodes[folderID].ParentID, indexNodeID)
	}
	// 新增页挂载到 folder。
	if result := h.store.sourceSyncResults[h.source.ID]; result.SyncedDocuments != 1 || result.Status != "succeeded" {
		t.Fatalf("result = %+v", result)
	}
}

// TestSyncWebSourceCursorAdvancesOnSitemapLastmod 验证 sitemap lastmod 场景：
// 全部成功时 cursor 推进到快照最大 lastmod。
func TestSyncWebSourceCursorAdvancesOnSitemapLastmod(t *testing.T) {
	editTime := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	pageURL := "https://docs.example.com/a"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "A")})
	for i := range h.connector.tree {
		h.connector.tree[i].EditTime = editTime
	}
	h.connector.fetchFn = func(string) (model.FetchedDocument, error) {
		return model.FetchedDocument{Markdown: []byte("a")}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}
	got := h.store.lastSourceSyncCursor[h.source.ID]
	if got.IsZero() || !got.Equal(editTime) {
		t.Fatalf("cursor = %v, want %v", got, editTime)
	}
}

// latestRevisionCount 统计文档的 crawl revision 数。
func (h *webSyncHarness) latestRevisionCount(documentID uuid.UUID) int {
	count := 0
	for _, rev := range h.store.revisions {
		if rev.DocumentID == documentID && rev.Reason == value.DocumentRevisionReasonCrawl {
			count++
		}
	}
	return count
}

// TestSyncWebSourceDisabledSourceSkips 验证停用源幂等跳过（不爬取、Job 正常完成）。
func TestSyncWebSourceDisabledSourceSkips(t *testing.T) {
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode("https://docs.example.com/a", "", "A")})
	h.source.Status = value.KbSourceStatusDisabled
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(h.connector.fetchedTokens) != 0 {
		t.Fatal("停用源不应触发抓取")
	}
	if _, ok := h.store.sourceSyncResults[h.source.ID]; ok {
		t.Fatal("停用源跳过时不应写同步结果")
	}
}

// TestSyncWebSourceCredentialInjection 验证凭证预解密注入：__credentials 进 config 副本、
// 原 Config 不被修改、密钥不进日志字段。
func TestSyncWebSourceCredentialInjection(t *testing.T) {
	const pageURL = "https://docs.example.com/a"
	h := newWebSyncHarness(t, []model.ExternalNode{webPageNode(pageURL, "", "A")})
	connID := uuid.New()
	h.source.Config["fetch"] = map[string]any{"engine": "firecrawl", "connection_id": connID.String()}
	h.svc = NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: h.kb},
		Selector: NewSourceConnectionSelector(
			&fakeSyncConnRepo{conn: model.SourceConnection{
				ID: connID, WorkspaceID: h.workspaceID, Provider: string(value.SourceProviderFirecrawl),
				Status:                "active",
				CredentialsCiphertext: []byte("plain-api-key"),
			}},
			passthroughCipher{},
		),
		Connector:    &fakeSourceConnector{},
		WebConnector: h.connector,
		RawStore:     h.raw,
		Store:        h.store,
		Queue:        h.queue,
		Logger:       h.logger,
	})
	h.connector.fetchFn = func(externalID string) (model.FetchedDocument, error) {
		return model.FetchedDocument{Markdown: []byte("a")}, nil
	}
	if err := h.runSync(t); err != nil {
		t.Fatalf("err = %v", err)
	}
	if doc := h.webDocumentByExternalID(pageURL); doc == nil {
		t.Fatal("页面未建档")
	}
	// 原 Config 不含注入键（__credentials 仅内存传递）。
	if _, leaked := h.source.Config["__credentials"]; leaked {
		t.Fatal("__credentials 泄漏进源 Config")
	}
	for _, entry := range h.logger.warnings {
		if strings.Contains(fmt.Sprint(entry.fields), "plain-api-key") {
			t.Fatal("凭证出现在日志字段")
		}
	}
	for _, entry := range h.logger.infos {
		if strings.Contains(fmt.Sprint(entry.fields), "plain-api-key") {
			t.Fatal("凭证出现在日志字段")
		}
	}
}
