package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
	"github.com/dajee/langhuan/internal/ports/storage"
)

func TestIngestCreatesDocumentRevisionAndWorkspaceTask(t *testing.T) {
	t.Parallel()

	h, store, generationID := newV2DocumentIngestHarness(t)
	result, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "legacy title", FileName: " Guide.MD ", ContentType: "text/markdown", SourceType: "upload",
		Reader: bytes.NewBufferString("hello v2"), SizeBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.document == nil || store.revision == nil || store.node == nil || store.job == nil {
		t.Fatalf("created aggregate = (%#v, %#v, %#v, %#v)", store.document, store.node, store.revision, store.job)
	}
	if store.document.WorkspaceID != h.workspaceID || store.document.Kind != value.DocumentKindFile || store.document.ActiveRevisionID != nil {
		t.Fatalf("document = %#v", store.document)
	}
	if store.node.ParentID == nil || *store.node.ParentID != h.kb.FileTreeRootID || store.node.Name != "Guide.MD" ||
		store.node.DocumentID == nil || *store.node.DocumentID != store.document.ID {
		t.Fatalf("file node = %#v", store.node)
	}
	if store.revision.DocumentID != store.document.ID || store.revision.RawStorageKey == "" ||
		store.revision.OriginalFilename != "Guide.MD" || store.revision.FileType != "markdown" {
		t.Fatalf("revision = %#v", store.revision)
	}
	if store.job.WorkspaceID != h.workspaceID || store.job.DocumentRevisionID != store.revision.ID ||
		store.job.IndexGenerationID != uuid.Nil || store.job.Payload["index_generation_id"] != generationID.String() {
		t.Fatalf("job = %#v", store.job)
	}
	if result.Document.RawStorageKey != "" {
		t.Fatalf("API document exposed raw_storage_key: %#v", result.Document)
	}
	var payload map[string]string
	if err := json.Unmarshal(h.queue.requests[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"generation_id": generationID.String(),
		"workspace_id":  h.workspaceID.String(), "knowledge_base_id": h.kb.ID.String(),
		"document_id": store.document.ID.String(), "document_revision_id": store.revision.ID.String(),
		"job_id": store.job.ID.String(),
	} {
		if payload[key] != want {
			t.Fatalf("queue payload[%q] = %q, want %q", key, payload[key], want)
		}
	}
	if _, exists := payload["index_generation_id"]; exists {
		t.Fatalf("queue payload contains legacy index_generation_id: %#v", payload)
	}
	wantTaskID := queue.DocumentTaskID(
		documentParseStartJobType, h.workspaceID, store.revision.ID, generationID,
	)
	if h.queue.requests[0].TaskID != wantTaskID {
		t.Fatalf("queue task id = %q, want %q", h.queue.requests[0].TaskID, wantTaskID)
	}
	assertTempDirEmpty(t, h.tempDir)
}

func TestIngestPreAllocatesRevisionIDIntoRawKeyAndRevision(t *testing.T) {
	t.Parallel()

	h, store, _ := newV2DocumentIngestHarness(t)
	_, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "rev-scoped", FileName: "rev.md", ContentType: "text/markdown", SourceType: "upload",
		Reader: bytes.NewBufferString("body"), SizeBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.raw.puts) != 1 {
		t.Fatalf("raw puts = %d, want 1", len(h.raw.puts))
	}
	rawInput := h.raw.puts[0]
	if rawInput.RevisionID == uuid.Nil {
		t.Fatalf("raw input RevisionID is nil; expected pre-allocated revision id")
	}
	if store.revision == nil {
		t.Fatalf("revision was not persisted")
	}
	if rawInput.RevisionID != store.revision.ID {
		t.Fatalf("raw input RevisionID = %s, revision ID = %s; must match (pre-allocated)",
			rawInput.RevisionID, store.revision.ID)
	}
	// raw key 应包含预分配的 revision id（fake store 把它附加到 key 末尾）。
	if !strings.Contains(store.revision.RawStorageKey, store.revision.ID.String()) {
		t.Fatalf("RawStorageKey = %q does not contain revision id %s",
			store.revision.RawStorageKey, store.revision.ID)
	}
}

func TestIngestV2QueueFailureAtomicallyMarksCreatedAggregateFailed(t *testing.T) {
	h, store, _ := newV2DocumentIngestHarness(t)
	h.queue.err = errors.New("queue down")

	_, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "queue failure", FileName: "queue.md", ContentType: "text/markdown",
		Reader: bytes.NewBufferString("content"), SizeBytes: 7,
	})
	if !errors.Is(err, h.queue.err) {
		t.Fatalf("Ingest error = %v, want queue error", err)
	}
	if store.failCalls != 1 || store.failedErrorClass != "enqueue_error" || store.failedMessage == "" {
		t.Fatalf("failure call = %d class=%q message=%q", store.failCalls, store.failedErrorClass, store.failedMessage)
	}
	if store.document.Status != value.DocumentStatusFailed {
		t.Fatalf("document status = %s, want failed", store.document.Status)
	}
	if store.revision.Status != value.DocumentRevisionFailed || store.revision.ErrorClass != "enqueue_error" || store.revision.ErrorMessage == "" {
		t.Fatalf("revision failure = %#v", store.revision)
	}
	if store.job.Status != value.JobStatusFailed || store.job.ErrorClass != "enqueue_error" || store.job.ErrorMessage == "" {
		t.Fatalf("job failure = %#v", store.job)
	}
	if len(h.raw.deletes) != 0 {
		t.Fatalf("raw deletes = %d, want 0", len(h.raw.deletes))
	}
}

func TestIngestV2QueueFailureJoinsPersistenceFailure(t *testing.T) {
	h, store, _ := newV2DocumentIngestHarness(t)
	h.queue.err = errors.New("queue down")
	store.failErr = errors.New("database down")

	_, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "queue failure", FileName: "queue.md", ContentType: "text/markdown",
		Reader: bytes.NewBufferString("content"), SizeBytes: 7,
	})
	if !errors.Is(err, h.queue.err) || !errors.Is(err, store.failErr) {
		t.Fatalf("Ingest error = %v, want queue and persistence errors", err)
	}
}

func TestIngestV2ReusesReadyRevision(t *testing.T) {
	h, store, _ := newV2DocumentIngestHarness(t)
	document, err := model.NewDocumentIdentity(
		h.workspaceID, h.kb.ID, value.DocumentKindFile, "existing.md", "upload", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		OriginalFilename: "existing.md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: "raw/existing", SHA256: sha256Hex("same"), SizeBytes: 4,
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: documentParseStartJobType, Status: value.JobStatusCompleted,
		Payload: map[string]any{"document_id": document.ID.String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.reusableDocument, store.reusableRevision, store.reusableJob = document, revision, job

	result, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "same", FileName: "same.md", ContentType: "text/markdown",
		Reader: bytes.NewBufferString("same"), SizeBytes: 4, Dedupe: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Deduped || result.Document.ID != document.ID || result.Job.ID != job.ID {
		t.Fatalf("dedupe result = %#v", result)
	}
	if len(h.raw.puts) != 0 || len(h.queue.requests) != 0 {
		t.Fatalf("unexpected side effects: raw=%d queue=%d", len(h.raw.puts), len(h.queue.requests))
	}
	assertTempDirEmpty(t, h.tempDir)
}

func TestDocumentIngestRejectsPDFBeforeSideEffects(t *testing.T) {
	testDocumentIngestRejectsFileTypeBeforeSideEffects(t, "report.PDF", "application/pdf", "%PDF")
}

func TestDocumentIngestRejectsUnknownFileTypeBeforeSideEffects(t *testing.T) {
	testDocumentIngestRejectsFileTypeBeforeSideEffects(t, "archive.xyz", "application/octet-stream", "unknown")
}

func testDocumentIngestRejectsFileTypeBeforeSideEffects(t *testing.T, fileName, contentType, body string) {
	t.Helper()
	h := newDocumentIngestHarness(t)
	tempDir := filepath.Join(t.TempDir(), "ingest-temp")
	h.service = NewDocumentIngestService(DocumentIngestServiceDeps{
		RawStore: h.raw, Queue: h.queue, TempDir: tempDir,
		AllowedFileTypes: []string{"markdown", "md", "txt", "csv", "xlsx", "docx"},
	})

	_, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "unsupported", FileName: fileName, ContentType: contentType,
		Reader: bytes.NewBufferString(body), SizeBytes: int64(len(body)), Dedupe: true,
	})
	if !errors.Is(err, domainerrors.ErrUnsupportedFileType) {
		t.Fatalf("error = %v, want ErrUnsupportedFileType", err)
	}
	if len(h.raw.puts) != 0 || len(h.queue.requests) != 0 {
		t.Fatalf("unexpected side effects: raw=%d queue=%d", len(h.raw.puts), len(h.queue.requests))
	}
	if _, statErr := os.Stat(tempDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("temporary directory was created before rejection: stat error = %v", statErr)
	}
}

type documentIngestHarness struct {
	workspaceID uuid.UUID
	kb          *model.KnowledgeBase
	raw         *fakeRawDocumentStore
	queue       *fakeJobQueue
	tempDir     string
	service     *DocumentIngestService
}

func newDocumentIngestHarness(t *testing.T) *documentIngestHarness {
	t.Helper()
	workspaceID := uuid.New()
	kb, err := model.NewKnowledgeBase(workspaceID, "kb", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &documentIngestHarness{
		workspaceID: workspaceID,
		kb:          kb,
		raw:         &fakeRawDocumentStore{},
		queue:       &fakeJobQueue{},
		tempDir:     t.TempDir(),
	}
}

func newV2DocumentIngestHarness(t *testing.T) (*documentIngestHarness, *fakeDocumentIngestStoreV2, uuid.UUID) {
	t.Helper()
	h := newDocumentIngestHarness(t)
	root := &model.FileTreeNode{
		ID: uuid.New(), WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		NodeType: value.FileTreeNodeRoot,
	}
	h.kb.FileTreeRootID = root.ID
	generationID := uuid.New()
	h.kb.ActiveIndexGenerationID = &generationID
	store := &fakeDocumentIngestStoreV2{
		kb: h.kb, nodes: map[uuid.UUID]*model.FileTreeNode{root.ID: root},
	}
	h.service = NewDocumentIngestService(DocumentIngestServiceDeps{
		Store: store, RawStore: h.raw, Queue: h.queue, TempDir: h.tempDir,
	})
	return h, store, generationID
}

type fakeDocumentIngestStoreV2 struct {
	kb               *model.KnowledgeBase
	nodes            map[uuid.UUID]*model.FileTreeNode
	document         *model.Document
	node             *model.FileTreeNode
	revision         *model.DocumentRevision
	job              *model.Job
	reusableDocument *model.Document
	reusableRevision *model.DocumentRevision
	reusableJob      *model.Job
	failCalls        int
	failedErrorClass string
	failedMessage    string
	failErr          error
	// idempotency tracking
	idempotencyRows      map[string]DocumentIngestIdempotency
	idempotencyInsertErr error
}

func (s *fakeDocumentIngestStoreV2) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, DocumentIngestTx) error,
) error {
	if s.kb == nil || s.kb.WorkspaceID != workspaceID {
		return domainerrors.ErrNotFound
	}
	return fn(ctx, s)
}

func (s *fakeDocumentIngestStoreV2) GetKnowledgeBase(_ context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	if s.kb == nil || s.kb.ID != id {
		return nil, domainerrors.ErrNotFound
	}
	return s.kb, nil
}

func (s *fakeDocumentIngestStoreV2) FindReusableRevision(context.Context, uuid.UUID, string, int) (*model.Document, *model.DocumentRevision, *model.Job, error) {
	if s.reusableDocument == nil || s.reusableRevision == nil || s.reusableJob == nil {
		return nil, nil, nil, domainerrors.ErrNotFound
	}
	return s.reusableDocument, s.reusableRevision, s.reusableJob, nil
}

func (s *fakeDocumentIngestStoreV2) GetFileTreeNodeForUpdate(_ context.Context, id uuid.UUID) (*model.FileTreeNode, error) {
	node := s.nodes[id]
	if node == nil {
		return nil, domainerrors.ErrNotFound
	}
	return node, nil
}

func (s *fakeDocumentIngestStoreV2) CreateFileDocumentNodeRevisionAndJob(
	_ context.Context,
	document *model.Document,
	node *model.FileTreeNode,
	revision *model.DocumentRevision,
	job *model.Job,
) error {
	s.document, s.node, s.revision, s.job = document, node, revision, job
	return nil
}

func (s *fakeDocumentIngestStoreV2) GetIdempotencyRecord(
	_ context.Context,
	_, _, _ uuid.UUID,
	key string,
) (DocumentIngestIdempotency, error) {
	if s.idempotencyRows == nil {
		return DocumentIngestIdempotency{}, domainerrors.ErrNotFound
	}
	if row, ok := s.idempotencyRows[key]; ok {
		return row, nil
	}
	return DocumentIngestIdempotency{}, domainerrors.ErrNotFound
}

func (s *fakeDocumentIngestStoreV2) CreateIdempotencyRecord(_ context.Context, record DocumentIngestIdempotency) error {
	if s.idempotencyInsertErr != nil {
		return s.idempotencyInsertErr
	}
	if s.idempotencyRows == nil {
		s.idempotencyRows = map[string]DocumentIngestIdempotency{}
	}
	if _, exists := s.idempotencyRows[record.Key]; exists {
		return domainerrors.ErrConflict
	}
	s.idempotencyRows[record.Key] = record
	return nil
}

func (s *fakeDocumentIngestStoreV2) FailCreatedIngest(
	_ context.Context,
	_, documentID, revisionID, jobID uuid.UUID,
	errorClass, message string,
) error {
	s.failCalls++
	s.failedErrorClass = errorClass
	s.failedMessage = message
	if s.failErr != nil {
		return s.failErr
	}
	if s.document == nil || s.document.ID != documentID ||
		s.revision == nil || s.revision.ID != revisionID ||
		s.job == nil || s.job.ID != jobID {
		return domainerrors.ErrNotFound
	}
	s.document.Status = value.DocumentStatusFailed
	s.revision.Status = value.DocumentRevisionFailed
	s.revision.ErrorClass = errorClass
	s.revision.ErrorMessage = message
	s.job.Status = value.JobStatusFailed
	s.job.ErrorClass = errorClass
	s.job.ErrorMessage = message
	return nil
}

type fakeRawDocumentStore struct {
	puts      []storage.RawDocumentInput
	deletes   []string
	deleteErr error
}

func (s *fakeRawDocumentStore) Put(_ context.Context, input storage.RawDocumentInput) (*storage.RawDocumentObject, error) {
	body, err := io.ReadAll(input.Reader)
	if err != nil {
		return nil, err
	}
	s.puts = append(s.puts, storage.RawDocumentInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: input.DocumentID, RevisionID: input.RevisionID,
		FileName: input.FileName, ContentType: input.ContentType,
		Reader: bytes.NewReader(body), SizeBytes: input.SizeBytes,
	})
	// 当调用方预分配了 RevisionID 时，raw key 必须带上它，
	// 这样真实 adapter 才能区分同一文档的不同 revision。
	key := "raw/" + input.DocumentID.String()
	if input.RevisionID != uuid.Nil {
		key = "raw/" + input.DocumentID.String() + "/" + input.RevisionID.String()
	}
	return &storage.RawDocumentObject{
		Key: key, SizeBytes: int64(len(body)),
		SHA256: sha256Hex(string(body)), ContentType: input.ContentType,
	}, nil
}

func (s *fakeRawDocumentStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeRawDocumentStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return s.deleteErr
}

type fakeJobQueue struct {
	requests []queue.JobRequest
	err      error
}

func (q *fakeJobQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: "queued"}, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func assertTempDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temp dir has %d entries, want 0", len(entries))
	}
}
