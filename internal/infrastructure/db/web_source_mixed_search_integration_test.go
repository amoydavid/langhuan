//go:build integration

package db

import (
	"context"
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

	markdownparser "github.com/dajee/langhuan/internal/adapters/parser/markdown"
	web "github.com/dajee/langhuan/internal/adapters/source/web"
	localstorage "github.com/dajee/langhuan/internal/adapters/storage/local"
	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/pipeline"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	"github.com/dajee/langhuan/internal/ports/queue"
	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

// 本文件覆盖 v1.3.0 spec §12.3 的混装检索冒烟：
// 同一 KB 内上传文件（走既有 ingest 落库路径）与 web 源页面（httptest 假站点）
// 双双经 parse→chunk→index 全管线进入 active Generation，一次 knowledge_search
// （MultiKnowledgeSearchService.Search，即 MCP knowledge_search 的应用层入口）
// 同时命中文件与页面。Embedding 用确定性 stub（全 1 向量 + FTS 关键词命中），
// 不发真实网络请求。

const smokeKeyword = "langhuan"

// fixedEmbeddingClient 返回与文本数等量的固定向量（维度匹配 Generation 快照）。
type fixedEmbeddingClient struct {
	dimension int
}

func (c *fixedEmbeddingClient) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	vectors := make([][]float32, len(input.Texts))
	for i := range vectors {
		vector := make([]float32, c.dimension)
		vector[0] = 1
		vectors[i] = vector
	}
	return &embeddingport.EmbedResult{Vectors: vectors}, nil
}

func (c *fixedEmbeddingClient) Dimension() int { return c.dimension }

// smokeResolver 按 seed 模型快照返回固定 ResolvedEmbeddingClient。
type smokeResolver struct {
	modelID, providerID uuid.UUID
	modelName           string
	dimension           int
}

func (r *smokeResolver) Resolve(_ context.Context, _ uuid.UUID, modelID uuid.UUID) (*appservice.ResolvedEmbeddingClient, error) {
	if modelID != r.modelID {
		return nil, domainerrors.ErrNotFound
	}
	return &appservice.ResolvedEmbeddingClient{
		Client:          &fixedEmbeddingClient{dimension: r.dimension},
		ModelID:         r.modelID,
		ProviderID:      r.providerID,
		ModelName:       r.modelName,
		ModelConfigHash: "model-hash",
		Dimensions:      r.dimension,
		BatchSize:       16,
	}, nil
}

func TestWebSourceMixedRetrievalSmoke(t *testing.T) {
	// 事务夹具：与既有编排 e2e 同口径（同事务种子 + WithinWorkspace 走 SAVEPOINT）。
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)

	// Generation 快照对齐当前 chunker v3 + 检索配置（simple FTS：内容含 ASCII
	// 关键词 langhuan，命中稳定；seed 的 v1 chunker 仅供建表测试）。
	if err := database.WithContext(ctx).Exec(
		"UPDATE knowledge_base_index_generations "+
			"SET retrieval_config = ?::jsonb, chunker_version = 3, "+
			"chunking_config = ?::jsonb WHERE id = ?",
		`{"fts_config":"simple","vector_top_k":10,"keyword_top_k":10,"final_top_k":5,"rrf_k":60}`,
		`{"strategy":"heading","enable_parent_child":true,"parent_chunk_size":512,"child_chunk_size":256,"chunk_size":512,"chunk_overlap":80}`,
		seed.generationID,
	).Error; err != nil {
		t.Fatalf("update generation snapshot: %v", err)
	}

	rawStore := localstorage.NewRawDocumentStore(t.TempDir())
	pl := newSmokePipeline(t, database, rawStore, seed)

	// ---- 文件侧：走既有 ingest 落库路径（Document + file node + Revision + Job）----
	fileMarkdown := "# Langhuan Install Guide\n\n" +
		"langhuan install steps are documented here for the file upload path.\n\n" +
		"## Requirements\n\nlanghuan requires docker and a workspace token."
	fileDoc := newSmokeFileDocument(t, ctx, database, rawStore, seed, "install-guide.md", fileMarkdown)
	if err := pl.RunParse(ctx, seed.workspaceID, fileDoc.revisionID); err != nil {
		t.Fatalf("file RunParse: %v", err)
	}
	runSmokeChunkAndIndex(t, pl, ctx, seed, fileDoc.revisionID)

	// ---- web 源侧：httptest 假站点 → 建源（service 全流程）→ 源级同步 ----
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/install":
			fmt.Fprint(w, "<html><head><title>Install</title></head><body>"+
				"<h1>Install</h1><p>langhuan install guide rendered as a web page for the crawler path.</p>"+
				"</body></html>")
		case "/api":
			fmt.Fprint(w, "<html><head><title>API</title></head><body>"+
				"<h1>API</h1><p>langhuan api reference web page with keyword langhuan for retrieval.</p>"+
				"</body></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)

	testQueue := &countingSmokeQueue{}
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
	sourceService := appservice.NewKbSourceService(appservice.KbSourceServiceDeps{
		KBs:   NewKnowledgeBaseRepository(database),
		Store: NewKbSourceRepository(database),
		SyncEnqueuer: appservice.NewSourceSyncService(appservice.SourceSyncServiceDeps{
			KnowledgeBaseRepository: NewKnowledgeBaseRepository(database),
			Store:                   NewSourceSyncDBStore(database),
			Queue:                   testQueue,
		}),
		Limits:          value.WebSourceLimits{MaxPagesCeiling: 100, MaxDepthCeiling: 5},
		MaxSourcesPerKB: 10,
	})
	source, _, err := sourceService.CreateWebSource(ctx, appservice.CreateWebSourceInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, Name: "smoke 站点",
		Config: map[string]any{
			"mode":  "urls",
			"urls":  []any{site.URL + "/install", site.URL + "/api"},
			"crawl": map[string]any{"respect_robots": false},
		},
	})
	if err != nil {
		t.Fatalf("CreateWebSource: %v", err)
	}

	syncService := appservice.NewSourceSyncService(appservice.SourceSyncServiceDeps{
		KnowledgeBaseRepository: NewKnowledgeBaseRepository(database),
		Selector: appservice.NewSourceConnectionSelector(
			NewSourceConnectionRepository(database), credentialCipher,
		),
		WebConnector: connector,
		RawStore:     rawStore,
		Store:        NewSourceSyncDBStore(database),
		Queue:        testQueue,
		Logger:       slog.Default(),
	})
	job, err := syncService.EnqueueSyncForSource(ctx, seed.workspaceID, seed.kbID, source.ID, appservice.SyncOptions{})
	if err != nil {
		t.Fatalf("EnqueueSyncForSource: %v", err)
	}
	if err := syncService.RunSourceSyncJobForSource(ctx, seed.workspaceID, seed.kbID, source.ID, job.ID); err != nil {
		t.Fatalf("RunSourceSyncJobForSource: %v", err)
	}

	// 页面 revision 经同一管线入库（web revision 无 file_type，parse 按 markdown 处理）。
	webRevisions := listPendingWebRevisions(t, ctx, database, source.ID)
	if len(webRevisions) != 2 {
		t.Fatalf("web 页面 revision 数 = %d, want 2", len(webRevisions))
	}
	for _, revisionID := range webRevisions {
		if err := pl.RunParse(ctx, seed.workspaceID, revisionID); err != nil {
			t.Fatalf("web RunParse: %v", err)
		}
		runSmokeChunkAndIndex(t, pl, ctx, seed, revisionID)
	}

	// ---- knowledge_search：一次查询同时命中文件与页面 ----
	multiSearch := appservice.NewMultiKnowledgeSearchService(
		NewRetrievalRepository(database, nil),
		&smokeResolver{modelID: seed.modelID, providerID: seed.providerID, modelName: "text-embedding", dimension: 1024},
		nil, nil,
		NewAPIKeyNameStoreDB(database),
		config.SearchConfig{},
		slog.Default(),
		NewSearchRunRepository(database),
		0,
	)
	response, err := multiSearch.Search(ctx, appservice.MultiKnowledgeSearchInput{
		WorkspaceID:      seed.workspaceID,
		Access:           value.ResourceAccess{WorkspaceID: seed.workspaceID, Unrestricted: true},
		KnowledgeBaseIDs: []uuid.UUID{seed.kbID},
		Query:            smokeKeyword,
	})
	if err != nil {
		t.Fatalf("knowledge_search: %v", err)
	}

	hitFile, hitWeb := false, false
	for _, result := range response.Results {
		if result.DocumentID == fileDoc.documentID {
			hitFile = true
		}
		if result.DocumentKind == value.DocumentKindWeb {
			hitWeb = true
		}
	}
	if !hitFile {
		t.Fatalf("检索未命中上传文件：results=%s", summarizeSmokeResults(response.Results))
	}
	if !hitWeb {
		t.Fatalf("检索未命中 web 页面：results=%s", summarizeSmokeResults(response.Results))
	}

	// 页面可按源溯源：web 命中的文档归属该源。
	var ownedCount int
	if err := database.WithContext(ctx).Raw(
		"SELECT count(*) FROM documents WHERE knowledge_base_id = ? AND kb_source_id = ?",
		seed.kbID, source.ID,
	).Scan(&ownedCount).Error; err != nil {
		t.Fatalf("count source documents: %v", err)
	}
	if ownedCount != 2 {
		t.Fatalf("源下页面数 = %d, want 2", ownedCount)
	}
}

// ---- 夹具 ----

type smokeFileDocument struct {
	documentID uuid.UUID
	revisionID uuid.UUID
}

// newSmokeFileDocument 走既有 ingest 落库路径创建文件文档（markdown revision）。
func newSmokeFileDocument(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	rawStore *localstorage.RawDocumentStore,
	seed knowledgeSchemaSeed,
	name, markdown string,
) smokeFileDocument {
	t.Helper()
	document, err := model.NewDocumentIdentity(seed.workspaceID, seed.kbID, value.DocumentKindFile, name, "upload", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest, OriginalFilename: name,
		FileType: "markdown", ContentType: "text/markdown", RawStorageKey: "pending",
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	object, err := rawStore.Put(ctx, portstorage.RawDocumentInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, RevisionID: revision.ID,
		FileName: name, ContentType: "text/markdown",
		Reader: strings.NewReader(markdown), SizeBytes: int64(len(markdown)),
	})
	if err != nil {
		t.Fatal(err)
	}
	revision.RawStorageKey = object.Key
	revision.SHA256 = object.SHA256
	revision.SizeBytes = object.SizeBytes

	documentID := document.ID
	node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, ParentID: &seed.rootID,
		NodeType: value.FileTreeNodeFile, Name: name, DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	ingestStore := NewDocumentIngestDBStore(database)
	if err := ingestStore.WithinWorkspace(
		ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentIngestTx) error {
			return tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatal(err)
	}
	return smokeFileDocument{documentID: document.ID, revisionID: revision.ID}
}

// newSmokePipeline 组装 parse→chunk→index 全真实仓储的管线（embedding 为 stub）。
func newSmokePipeline(t *testing.T, database *gorm.DB, rawStore *localstorage.RawDocumentStore, seed knowledgeSchemaSeed) *pipeline.DocumentPipeline {
	t.Helper()
	return pipeline.NewDocumentPipeline(pipeline.DocumentPipelineDeps{
		Documents:         NewDocumentRepository(database),
		Revisions:         NewDocumentRevisionRepository(database),
		Generations:       NewIndexGenerationRepository(database),
		ChunkSets:         NewChunkSetRepository(database),
		FAQRevisions:      NewFAQRepository(database),
		IndexSources:      NewChunkSetRepository(database),
		EmbeddingResolver: &smokeResolver{modelID: seed.modelID, providerID: seed.providerID, modelName: "text-embedding", dimension: 1024},
		RetrievalIndex:    NewRetrievalRepository(database, nil),
		Publisher:         NewDocumentPublishDBStore(database),
		Parser:            markdownparser.New(),
		RawStore:          rawStore,
		MaxFileSizeBytes:  20 * 1024 * 1024,
	})
}

func runSmokeChunkAndIndex(t *testing.T, pl *pipeline.DocumentPipeline, ctx context.Context, seed knowledgeSchemaSeed, revisionID uuid.UUID) {
	t.Helper()
	chunkSetID, err := pl.RunChunk(ctx, seed.workspaceID, revisionID, seed.generationID)
	if err != nil {
		t.Fatalf("RunChunk(revision=%s): %v", revisionID, err)
	}
	if _, err := pl.RunIndex(ctx, seed.workspaceID, seed.generationID, chunkSetID); err != nil {
		t.Fatalf("RunIndex(revision=%s): %v", revisionID, err)
	}
}

// listPendingWebRevisions 返回源下页面文档的 pending revision ID。
func listPendingWebRevisions(t *testing.T, ctx context.Context, database *gorm.DB, sourceID uuid.UUID) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	if err := database.WithContext(ctx).Raw(
		"SELECT r.id FROM document_revisions r "+
			"JOIN documents d ON d.workspace_id = r.workspace_id AND d.knowledge_base_id = r.knowledge_base_id "+
			"AND d.id = r.document_id AND d.kind = r.kind "+
			"WHERE d.kb_source_id = ? AND r.status = ? ORDER BY r.created_at ASC",
		sourceID, string(value.DocumentRevisionPending),
	).Scan(&ids).Error; err != nil {
		t.Fatalf("list web revisions: %v", err)
	}
	return ids
}

func summarizeSmokeResults(results []*dto.SearchResult) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		parts = append(parts, fmt.Sprintf("%s(%s)", result.DocumentID, result.DocumentKind))
	}
	return strings.Join(parts, ", ")
}

// countingSmokeQueue 记录入队请求（smoke 专用，避免与 recordingQueue 耦合）。
type countingSmokeQueue struct {
	mu       sync.Mutex
	requests []queue.JobRequest
}

func (q *countingSmokeQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: "queued"}, nil
}
