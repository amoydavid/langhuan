//go:build integration

package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	localstorage "github.com/dajee/langhuan/internal/adapters/storage/local"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

func TestDocumentRepositoryDeleteRetiresProjectionRemovesOnlyFileNodeAndKeepsRevision(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	document, revision, node, job := newFileIngestAggregate(
		t, seed.workspaceID, seed.kbID, seed.rootID, "delete-me.md",
	)
	if err := NewDocumentIngestDBStore(database).WithinWorkspace(
		ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentIngestTx) error {
			return tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatal(err)
	}
	setID, chunk, chunkRevision := createReadyRetrievalChunk(
		t, ctx, database, seed, document.ID, revision.ID, "source", "search", "content",
	)
	entry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: document.ID, DocumentRevisionID: revision.ID,
		ChunkSetID: setID, ChunkID: chunk.ID, ChunkRevisionID: chunkRevision.ID,
		State: value.RetrievalEntryStaging, SearchContent: "search", Content: "content",
		SourceAnchor: chunk.SourceAnchor, Metadata: map[string]any{}, CreatedAt: time.Now().UTC(),
	}
	if err := NewRetrievalRepository(database).StageBatch(
		ctx, seed.workspaceID, "simple", 1024,
		[]indexport.StageEntry{{Entry: entry, Embedding: make([]float32, 1024)}},
	); err != nil {
		t.Fatal(err)
	}
	publishSearchEntries(t, ctx, database, seed.workspaceID, entry.ID)

	otherWorkspaceID := uuid.New()
	if err := NewDocumentRepository(database).Delete(ctx, otherWorkspaceID, document.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace delete error = %v", err)
	}
	if err := NewDocumentRepository(database).Delete(ctx, seed.workspaceID, document.ID); err != nil {
		t.Fatal(err)
	}

	var documentRow DocumentRow
	if err := database.WithContext(ctx).First(&documentRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatal(err)
	}
	if documentRow.Status != string(value.DocumentStatusDeleted) || documentRow.DeletedAt == nil {
		t.Fatalf("deleted document = %#v", documentRow)
	}
	assertDocumentDeleteCount(t, ctx, database, "file_tree_nodes", "document_id = ?", document.ID, 0)
	assertDocumentDeleteCount(t, ctx, database, "document_revisions", "id = ?", revision.ID, 1)

	var revisionRow DocumentRevisionRow
	if err := database.WithContext(ctx).First(&revisionRow, "workspace_id = ? AND id = ?", seed.workspaceID, revision.ID).Error; err != nil {
		t.Fatal(err)
	}
	if revisionRow.RawStorageKey == nil || *revisionRow.RawStorageKey != revision.RawStorageKey {
		t.Fatalf("raw storage key = %v, want retained %q", revisionRow.RawStorageKey, revision.RawStorageKey)
	}
	var retrievalRow RetrievalEntryRow
	if err := database.WithContext(ctx).First(&retrievalRow, "workspace_id = ? AND id = ?", seed.workspaceID, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retrievalRow.State != string(value.RetrievalEntryRetired) || retrievalRow.RetiredAt == nil {
		t.Fatalf("retrieval entry = %#v", retrievalRow)
	}
}

func TestDocumentRepositoryDeleteFAQAndWebLeavesUnrelatedTreeNodes(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	folderID := uuid.New()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name) "+
			"VALUES (?, ?, ?, ?, 'folder', 'keep')",
		folderID, seed.workspaceID, seed.kbID, seed.rootID,
	).Error; err != nil {
		t.Fatal(err)
	}

	faq, err := model.NewDocumentIdentity(
		seed.workspaceID, seed.kbID, value.DocumentKindFAQ, "FAQ", "api", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	web, err := model.NewDocumentIdentity(
		seed.workspaceID, seed.kbID, value.DocumentKindWeb, "Web", "crawler", "https://example.com/page", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewDocumentRepository(database)
	if err := repository.Create(ctx, faq); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, web); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(ctx, seed.workspaceID, faq.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(ctx, seed.workspaceID, web.ID); err != nil {
		t.Fatal(err)
	}
	assertDocumentDeleteCount(t, ctx, database, "file_tree_nodes", "id = ?", folderID, 1)
}

func assertDocumentDeleteCount(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	table, condition string,
	arg any,
	want int64,
) {
	t.Helper()
	var count int64
	if err := database.WithContext(ctx).Table(table).Where(condition, arg).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

func TestDocumentAndJobRepositoriesScopeByWorkspaceIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "document-scope")
	otherWorkspaceID := uuid.New()
	provider := createProviderForTest(t, ctx, NewModelProviderRepository(tx), value.ModelScopeWorkspace, &workspaceID, "document-scope")
	embeddingModel := createModelForTest(t, ctx, NewModelRepository(tx), provider.ID, "document-scope", value.ModelStatusActive)
	kbRepo := NewKnowledgeBaseRepository(tx)
	createdKB, err := appservice.NewKnowledgeBaseService(kbRepo, kbRepo).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "docs", Description: "desc", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	document, revision, node, job := newFileIngestAggregate(t, workspaceID, createdKB.ID, createdKB.FileTreeRootID, "a.pdf")
	store := NewDocumentIngestDBStore(tx)
	if err := store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, ingestTx appservice.DocumentIngestTx) error {
		return ingestTx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
	}); err != nil {
		t.Fatal(err)
	}

	docRepo := NewDocumentRepository(tx)
	jobRepo := NewJobRepository(tx)
	if _, err := docRepo.Get(ctx, workspaceID, document.ID); err != nil {
		t.Fatalf("document in workspace should be readable: %v", err)
	}
	if _, err := docRepo.Get(ctx, otherWorkspaceID, document.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("cross-workspace document err = %v", err)
	}
	if _, err := jobRepo.Get(ctx, workspaceID, job.ID); err != nil {
		t.Fatalf("job in workspace should be readable: %v", err)
	}
	if _, err := jobRepo.Get(ctx, otherWorkspaceID, job.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("cross-workspace job err = %v", err)
	}
}

func TestDocumentIngestPersistsRevisionJobWithoutGenerationTarget(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, database, "document-job-target")
	provider := createProviderForTest(
		t, ctx, NewModelProviderRepository(database), value.ModelScopeWorkspace,
		&workspaceID, "document-job-target",
	)
	embeddingModel := createModelForTest(
		t, ctx, NewModelRepository(database), provider.ID,
		"document-job-target", value.ModelStatusActive,
	)
	knowledgeBaseRepository := NewKnowledgeBaseRepository(database)
	knowledgeBase, err := appservice.NewKnowledgeBaseService(
		knowledgeBaseRepository, knowledgeBaseRepository,
	).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "job targets", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	queued := &documentIngestIntegrationQueue{}
	ingest := appservice.NewDocumentIngestService(appservice.DocumentIngestServiceDeps{
		Store:    NewDocumentIngestDBStore(database),
		RawStore: localstorage.NewRawDocumentStore(t.TempDir()),
		Queue:    queued,
		TempDir:  t.TempDir(),
	})
	result, err := ingest.Ingest(ctx, appservice.IngestDocumentInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBase.ID,
		Title: "guide", FileName: "guide.md", ContentType: "text/markdown",
		SourceType: "upload", Reader: strings.NewReader("# Guide"), SizeBytes: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(queued.requests) != 1 {
		t.Fatalf("queued requests = %d, want 1", len(queued.requests))
	}
	var row JobRow
	if err := database.WithContext(ctx).First(&row, "workspace_id = ? AND id = ?", workspaceID, result.Job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.DocumentID == nil || row.DocumentRevisionID == nil || row.IndexGenerationID != nil {
		t.Fatalf("job targets = document %v revision %v generation %v", row.DocumentID, row.DocumentRevisionID, row.IndexGenerationID)
	}
	if generationID, ok := row.Payload["index_generation_id"].(string); !ok ||
		knowledgeBase.ActiveIndexGenerationID == nil || generationID != knowledgeBase.ActiveIndexGenerationID.String() {
		t.Fatalf("payload generation = %#v, active = %v", row.Payload["index_generation_id"], knowledgeBase.ActiveIndexGenerationID)
	}
}

type documentIngestIntegrationQueue struct {
	requests []queueport.JobRequest
}

func (q *documentIngestIntegrationQueue) Enqueue(_ context.Context, request queueport.JobRequest) (*queueport.JobHandle, error) {
	q.requests = append(q.requests, request)
	return &queueport.JobHandle{ID: uuid.NewString()}, nil
}

func TestDocumentRepositoryListScopesAndOrders(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceA := createWorkspaceRow(t, ctx, tx, "document-list-a")
	workspaceB := createWorkspaceRow(t, ctx, tx, "document-list-b")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	providerA := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceA, "document-list-a")
	providerB := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceB, "document-list-b")
	modelA := createModelForTest(t, ctx, modelRepo, providerA.ID, "document-list-a", value.ModelStatusActive)
	modelB := createModelForTest(t, ctx, modelRepo, providerB.ID, "document-list-b", value.ModelStatusActive)
	createKB := func(workspaceID, modelID uuid.UUID, name string) uuid.UUID {
		t.Helper()
		repository := NewKnowledgeBaseRepository(tx)
		created, err := appservice.NewKnowledgeBaseService(repository, repository).Create(ctx, appservice.CreateKnowledgeBaseInput{
			WorkspaceID: workspaceID, Name: name, EmbeddingModelID: modelID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	kbA := createKB(workspaceA, modelA.ID, "docs-a")
	kbB := createKB(workspaceB, modelB.ID, "docs-b")
	createdAt := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)
	createDocument := func(id, workspaceID, kbID uuid.UUID, title string) {
		t.Helper()
		document, err := model.NewDocumentIdentity(workspaceID, kbID, value.DocumentKindFile, title, "upload", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		document.ID, document.CreatedAt, document.UpdatedAt = id, createdAt, createdAt
		if err := tx.WithContext(ctx).Create(documentV2ToRow(document)).Error; err != nil {
			t.Fatal(err)
		}
	}
	id1 := uuid.MustParse("40000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("50000000-0000-0000-0000-000000000002")
	createDocument(id1, workspaceA, kbA, "one")
	createDocument(id2, workspaceA, kbA, "two")
	createDocument(uuid.MustParse("60000000-0000-0000-0000-000000000003"), workspaceB, kbB, "other")

	docRepo := NewDocumentRepository(tx)
	got, err := docRepo.List(ctx, appservice.DocumentListFilter{WorkspaceID: workspaceA, KnowledgeBaseID: kbA})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != id2 || got[1].ID != id1 || got[0].Kind != value.DocumentKindFile {
		t.Fatalf("List() = %#v, want workspace/KB scoped DESC order", got)
	}
	crossWorkspace, err := docRepo.List(ctx, appservice.DocumentListFilter{WorkspaceID: workspaceB, KnowledgeBaseID: kbA})
	if err != nil {
		t.Fatal(err)
	}
	if len(crossWorkspace) != 0 {
		t.Fatalf("cross-workspace List() = %#v", crossWorkspace)
	}
}

func TestDocumentRepositoryGetAndListAttachActiveRevisionSummary(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	document, revision, node, job := newFileIngestAggregate(
		t, seed.workspaceID, seed.kbID, seed.rootID, "active.md",
	)
	if err := NewDocumentIngestDBStore(database).WithinWorkspace(
		ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentIngestTx) error {
			return tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, revision.ID).
		Updates(map[string]any{"status": string(value.DocumentRevisionReady), "completed_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, document.ID).
		Updates(map[string]any{"status": string(value.DocumentStatusReady), "active_revision_id": revision.ID}).Error; err != nil {
		t.Fatal(err)
	}

	repository := NewDocumentRepository(database)
	got, err := repository.Get(ctx, seed.workspaceID, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveRevision == nil || got.ActiveRevision.ID != revision.ID || got.ActiveRevision.FileType != "pdf" {
		t.Fatalf("Get active revision = %#v", got.ActiveRevision)
	}
	items, err := repository.List(ctx, appservice.DocumentListFilter{WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ActiveRevision == nil || items[0].ActiveRevision.ID != revision.ID {
		t.Fatalf("List documents = %#v", items)
	}
}

func TestDocumentRepositoryListAttachesActiveFAQQuestionCount(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	document, faq, job := testFAQPersistenceAggregate(t, seed)
	if err := NewFAQRepository(database).WithinWorkspace(
		ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.FAQRevisionTx) error {
			return tx.CreateFAQRevisionAggregate(txCtx, document, faq, job)
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, document.ID).
		Updates(map[string]any{
			"status":             string(value.DocumentStatusReady),
			"active_revision_id": faq.DocumentRevision.ID,
		}).Error; err != nil {
		t.Fatal(err)
	}

	items, err := NewDocumentRepository(database).List(ctx, appservice.DocumentListFilter{WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FAQQuestionCount != 2 {
		t.Fatalf("FAQ question count = %#v, want 2", items)
	}
}

func TestDocumentIngestStoreRollsBackAggregateOnSiblingNameConflict(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "document-conflict")
	provider := createProviderForTest(t, ctx, NewModelProviderRepository(tx), value.ModelScopeWorkspace, &workspaceID, "document-conflict")
	embeddingModel := createModelForTest(t, ctx, NewModelRepository(tx), provider.ID, "document-conflict", value.ModelStatusActive)
	kbRepo := NewKnowledgeBaseRepository(tx)
	kb, err := appservice.NewKnowledgeBaseService(kbRepo, kbRepo).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "conflict", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewDocumentIngestDBStore(tx)
	firstDocument, firstRevision, firstNode, firstJob := newFileIngestAggregate(t, workspaceID, kb.ID, kb.FileTreeRootID, "Guide.pdf")
	if err := store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, ingestTx appservice.DocumentIngestTx) error {
		return ingestTx.CreateFileDocumentNodeRevisionAndJob(txCtx, firstDocument, firstNode, firstRevision, firstJob)
	}); err != nil {
		t.Fatal(err)
	}
	secondDocument, secondRevision, secondNode, secondJob := newFileIngestAggregate(t, workspaceID, kb.ID, kb.FileTreeRootID, "guide.PDF")
	err = store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, ingestTx appservice.DocumentIngestTx) error {
		return ingestTx.CreateFileDocumentNodeRevisionAndJob(txCtx, secondDocument, secondNode, secondRevision, secondJob)
	})
	if !errors.Is(err, domainerrors.ErrFileTreeNameConflict) {
		t.Fatalf("conflicting ingest error = %v", err)
	}
	for table, id := range map[string]uuid.UUID{
		"documents": secondDocument.ID, "document_revisions": secondRevision.ID, "jobs": secondJob.ID,
	} {
		var count int64
		if err := tx.WithContext(ctx).Table(table).Where("workspace_id = ? AND id = ?", workspaceID, id).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d rows after conflict", table, count)
		}
	}
}

func newFileIngestAggregate(t *testing.T, workspaceID, knowledgeBaseID, rootID uuid.UUID, name string) (*model.Document, *model.DocumentRevision, *model.FileTreeNode, *model.Job) {
	t.Helper()
	document, err := model.NewDocumentIdentity(workspaceID, knowledgeBaseID, value.DocumentKindFile, name, "upload", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest, OriginalFilename: name,
		FileType: "pdf", ContentType: "application/pdf", RawStorageKey: "raw/" + name,
		SHA256: "abc", SizeBytes: 3, ProcessingVersion: model.CurrentProcessingVersion,
		Status: value.DocumentRevisionPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	documentID := document.ID
	node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, ParentID: &rootID,
		NodeType: value.FileTreeNodeFile, Name: name, DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	return document, revision, node, job
}
