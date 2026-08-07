//go:build integration

package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	markdownparser "github.com/dajee/langhuan/internal/adapters/parser/markdown"
	"github.com/dajee/langhuan/internal/adapters/storage/local"
	"github.com/dajee/langhuan/internal/application/pipeline"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	"github.com/dajee/langhuan/internal/ports/queue"
	"github.com/dajee/langhuan/internal/testsupport"
	"gorm.io/gorm"
)

func TestV2DocumentRevisionChunkingIntegration(t *testing.T) {
	ctx := context.Background()
	testDSN := testsupport.NewMigratedPostgres(t)
	gormDB, err := db.Open(testDSN)
	if err != nil {
		t.Fatal(err)
	}

	tx := gormDB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()

	if err := db.EnsureDefaultWorkspace(ctx, tx); err != nil {
		t.Fatal(err)
	}

	workspaceRepo := db.NewWorkspaceRepository(tx)
	kbRepo := db.NewKnowledgeBaseRepository(tx)
	documentRepo := db.NewDocumentRepository(tx)
	documentRevisionRepo := db.NewDocumentRevisionRepository(tx)
	indexGenerationRepo := db.NewIndexGenerationRepository(tx)
	chunkSetRepo := db.NewChunkSetRepository(tx)
	chunkRevisionRepo := db.NewChunkRevisionRepository(tx)
	faqRepo := db.NewFAQRepository(tx)
	chunkRepo := db.NewChunkRepository(tx)

	workspaceService := service.NewWorkspaceService(workspaceRepo, false)
	kbService := service.NewKnowledgeBaseService(kbRepo)
	queue := &integrationJobQueue{}
	rawStore := local.NewRawDocumentStore(t.TempDir())
	documentPipeline := pipeline.NewDocumentPipeline(pipeline.DocumentPipelineDeps{
		Documents:        documentRepo,
		Revisions:        documentRevisionRepo,
		Generations:      indexGenerationRepo,
		ChunkSets:        chunkSetRepo,
		FAQRevisions:     faqRepo,
		Parser:           markdownparser.New(),
		RawStore:         rawStore,
		MaxFileSizeBytes: 1024,
	})
	ingestService := service.NewDocumentIngestService(service.DocumentIngestServiceDeps{
		Store: db.NewDocumentIngestDBStore(tx), RawStore: rawStore,
		Queue: queue, TempDir: t.TempDir(),
	})

	workspace, err := workspaceService.Create(ctx, service.CreateWorkspaceInput{
		Name: "v0.2.0 integration workspace",
		Slug: "v020-integration-workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &workspace.ID, "worker-openai", "Worker OpenAI", "", "openai", map[string]any{}, []byte("cipher"), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	provider.CreatedBy = nil
	if err := db.NewModelProviderRepository(tx).Create(ctx, provider); err != nil {
		t.Fatal(err)
	}
	dimensions := 1024
	embeddingModel, err := model.NewModel(provider.ID, "worker-embed", "Worker Embedding", "", value.ModelTypeEmbedding, "worker-embed", &dimensions, map[string]any{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	embeddingModel.CreatedBy = nil
	if err := db.NewModelRepository(tx).Create(ctx, embeddingModel); err != nil {
		t.Fatal(err)
	}

	kb, err := kbService.Create(ctx, service.CreateKnowledgeBaseInput{
		WorkspaceID: workspace.ID, Name: "v0.2.0 documents",
		Description: "integration test knowledge base", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := ingestService.Ingest(ctx, service.IngestDocumentInput{
		WorkspaceID:     workspace.ID,
		KnowledgeBaseID: kb.ID,
		Title:           "state-path.md",
		FileName:        "state-path.md",
		ContentType:     "text/markdown",
		SourceType:      "upload",
		Reader:          strings.NewReader("# State Path\n\nintegration body"),
		SizeBytes:       int64(len("# State Path\n\nintegration body")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Status != value.DocumentStatusPending {
		t.Fatalf("initial document status = %s, want %s", result.Document.Status, value.DocumentStatusPending)
	}
	if queue.Len() != 1 {
		t.Fatalf("queued jobs = %d, want 1", queue.Len())
	}
	request, ok := queue.Pop()
	if !ok || request.Type != TaskDocumentParseStart {
		t.Fatalf("queued request = %#v, want %s", request, TaskDocumentParseStart)
	}
	var payload DocumentTaskPayload
	if err := json.Unmarshal(request.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.WorkspaceID != workspace.ID || payload.KnowledgeBaseID != kb.ID ||
		payload.DocumentID != result.Document.ID || payload.DocumentRevisionID == uuid.Nil ||
		kb.ActiveIndexGenerationID == nil || payload.GenerationID != *kb.ActiveIndexGenerationID ||
		payload.JobID != result.Job.ID {
		t.Fatalf("queued lineage = %#v", payload)
	}
	if err := documentPipeline.RunParse(ctx, payload.WorkspaceID, payload.DocumentRevisionID); err != nil {
		t.Fatal(err)
	}
	chunkSetID, err := documentPipeline.RunChunk(
		ctx, payload.WorkspaceID, payload.DocumentRevisionID, payload.GenerationID,
	)
	if err != nil {
		t.Fatal(err)
	}
	retriedChunkSetID, err := documentPipeline.RunChunk(
		ctx, payload.WorkspaceID, payload.DocumentRevisionID, payload.GenerationID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if retriedChunkSetID != chunkSetID {
		t.Fatalf("retried chunk set id = %s, want %s", retriedChunkSetID, chunkSetID)
	}

	document, err := documentRepo.Get(ctx, workspace.ID, result.Document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if document.Status != value.DocumentStatusPending {
		t.Fatalf("document status = %s, want unpublished pending", document.Status)
	}
	if result.Document.ActiveRevision == nil {
		t.Fatal("ingest result active revision summary is nil")
	}
	revisionID := payload.DocumentRevisionID
	revision, err := documentRevisionRepo.Get(ctx, workspace.ID, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if revision.Status != value.DocumentRevisionReady || revision.NormalizedMarkdown != "# State Path\n\nintegration body" {
		t.Fatalf("revision status/markdown = %s %q", revision.Status, revision.NormalizedMarkdown)
	}

	persistedChunkSetID := assertSingleIntegrationChunk(
		t, ctx, tx, chunkRepo, chunkRevisionRepo, workspace.ID,
		result.Document.ID, revisionID, "# State Path\n\nintegration body",
	)
	if persistedChunkSetID != chunkSetID {
		t.Fatalf("persisted chunk set id = %s, want %s", persistedChunkSetID, chunkSetID)
	}
}

func assertSingleIntegrationChunk(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	chunkRepo *db.ChunkRepository,
	chunkRevisionRepo *db.ChunkRevisionRepository,
	workspaceID, documentID, revisionID uuid.UUID,
	content string,
) uuid.UUID {
	t.Helper()

	var chunkSets []db.DocumentChunkSetRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND document_id = ? AND document_revision_id = ?", workspaceID, documentID, revisionID).
		Find(&chunkSets).Error; err != nil {
		t.Fatal(err)
	}
	if len(chunkSets) != 1 || chunkSets[0].Status != string(value.ChunkSetReady) || chunkSets[0].ChunkCount != 2 {
		t.Fatalf("chunk sets = %#v, want one ready set", chunkSets)
	}
	chunks, err := chunkRepo.ListByChunkSet(ctx, workspaceID, chunkSets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := chunkRevisionRepo.ListByChunkSet(ctx, workspaceID, chunkSets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || len(revisions) != 2 {
		t.Fatalf("chunks/revisions = %d/%d, want 2/2", len(chunks), len(revisions))
	}
	byRole := map[value.ChunkRole]*model.Chunk{}
	for _, chunk := range chunks {
		byRole[chunk.Role] = chunk
	}
	parent, child := byRole[value.ChunkRoleParent], byRole[value.ChunkRoleChild]
	if parent == nil || child == nil || child.ParentChunkID == nil || *child.ParentChunkID != parent.ID {
		t.Fatalf("parent/child lineage = %#v", chunks)
	}
	if parent.DocumentID != documentID || parent.Sequence != 0 || parent.SourceContent != content {
		t.Fatalf("parent = %#v", parent)
	}
	for _, revision := range revisions {
		if revision.ChunkID == child.ID && revision.EmbeddingContent == "" {
			t.Fatal("child embedding content is empty")
		}
		if revision.ChunkID == parent.ID && revision.Content != content {
			t.Fatalf("parent revision = %#v", revision)
		}
	}
	return chunkSets[0].ID
}

type integrationJobQueue struct {
	requests []queue.JobRequest
}

func (q *integrationJobQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: uuid.NewString()}, nil
}

func (q *integrationJobQueue) Len() int {
	return len(q.requests)
}

func (q *integrationJobQueue) Pop() (queue.JobRequest, bool) {
	if len(q.requests) == 0 {
		return queue.JobRequest{}, false
	}
	req := q.requests[0]
	q.requests = q.requests[1:]
	return req, true
}
