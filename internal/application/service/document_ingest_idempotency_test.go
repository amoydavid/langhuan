package service

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// fakeIdempotencyReplayStore is a minimal IdempotencyReplayStore for the service
// tests. It returns whatever rows the test seeds, keyed by idempotency key.
type fakeIdempotencyReplayStore struct {
	mu   sync.Mutex
	rows map[string]IdempotentIngestReplay
	err  error
}

func (s *fakeIdempotencyReplayStore) ReplayIdempotentIngest(
	_ context.Context,
	_, _, _ uuid.UUID,
	key string,
) (IdempotentIngestReplay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return IdempotentIngestReplay{}, s.err
	}
	if replay, ok := s.rows[key]; ok {
		return replay, nil
	}
	return IdempotentIngestReplay{}, domainerrors.ErrNotFound
}

func newIdempotencyServiceHarness(t *testing.T) (*documentIngestHarness, *fakeDocumentIngestStoreV2, *fakeIdempotencyReplayStore) {
	t.Helper()
	h, store, generationID := newV2DocumentIngestHarness(t)
	replay := &fakeIdempotencyReplayStore{}
	h.service = NewDocumentIngestService(DocumentIngestServiceDeps{
		Store: store, ReplayStore: replay, RawStore: h.raw, Queue: h.queue, TempDir: h.tempDir,
	})
	_ = generationID
	return h, store, replay
}

func apiKeyPtr(id uuid.UUID) *uuid.UUID { return &id }

// TestIngestIdempotentReplayReturnsSameDocument verifies that a second call with
// the same key+body returns the stored document/job with Deduped=true, without
// creating a new document or enqueuing a new job.
func TestIngestIdempotentReplayReturnsSameDocument(t *testing.T) {
	h, store, _ := newIdempotencyServiceHarness(t)
	apiKeyID := uuid.New()
	input := IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "ticket", FileName: "ticket.md", ContentType: "text/markdown", SourceType: "api",
		Reader: bytes.NewBufferString("# body"), SizeBytes: 5,
		CallerAPIKeyID: apiKeyPtr(apiKeyID), IdempotencyKey: "k-replay",
	}
	first, err := h.service.Ingest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Deduped {
		t.Fatalf("first call should not be deduped")
	}
	if first.Document == nil || first.Job == nil {
		t.Fatalf("first result missing doc/job")
	}
	originalDocID := first.Document.ID
	originalJobID := first.Job.ID

	// Seed the replay store so the second call's fast-path finds the stored row.
	replayRow := IdempotentIngestReplay{
		Record:   store.idempotencyRows["k-replay"],
		Document: store.document,
		Revision: store.revision,
		Job:      store.job,
	}
	// Recreate the harness replay store with the seeded row.
	h2, store2, replay2 := newIdempotencyServiceHarness(t)
	store2.idempotencyRows = map[string]DocumentIngestIdempotency{
		"k-replay": {DocumentID: replayRow.Document.ID, JobID: replayRow.Job.ID, RequestSHA256: replayRow.Record.RequestSHA256},
	}
	replay2.rows = map[string]IdempotentIngestReplay{"k-replay": replayRow}
	store2.document = replayRow.Document
	store2.revision = replayRow.Revision
	store2.job = replayRow.Job

	second, err := h2.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h2.workspaceID, KnowledgeBaseID: h2.kb.ID,
		Title: "ticket", FileName: "ticket.md", ContentType: "text/markdown", SourceType: "api",
		Reader: bytes.NewBufferString("# body"), SizeBytes: 5,
		CallerAPIKeyID: apiKeyPtr(apiKeyID), IdempotencyKey: "k-replay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Deduped {
		t.Fatalf("second call should be deduped")
	}
	if second.Document.ID != originalDocID || second.Job.ID != originalJobID {
		t.Fatalf("replay returned different ids doc=%s job=%s", second.Document.ID, second.Job.ID)
	}
	if len(h2.queue.requests) != 0 {
		t.Fatalf("replay should not enqueue a new job: %d", len(h2.queue.requests))
	}
}

// TestIngestIdempotentBodyConflictReturns409 verifies that the same key with a
// different body returns ErrIdempotencyConflict.
func TestIngestIdempotentBodyConflictReturns409(t *testing.T) {
	h, store, replay := newIdempotencyServiceHarness(t)
	apiKeyID := uuid.New()
	input := IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "ticket", FileName: "ticket.md", ContentType: "text/markdown", SourceType: "api",
		Reader: bytes.NewBufferString("# body one"), SizeBytes: 11,
		CallerAPIKeyID: apiKeyPtr(apiKeyID), IdempotencyKey: "k-conflict",
	}
	first, err := h.service.Ingest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	// Seed replay store to return the FIRST request's hash, so the second call
	// (different body) detects the mismatch.
	replay.rows = map[string]IdempotentIngestReplay{
		"k-conflict": {
			Record: DocumentIngestIdempotency{
				RequestSHA256: store.idempotencyRows["k-conflict"].RequestSHA256,
				DocumentID:    first.Document.ID, JobID: first.Job.ID,
			},
			Document: store.document,
			Revision: store.revision,
			Job:      store.job,
		},
	}

	_, err = h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "ticket", FileName: "ticket.md", ContentType: "text/markdown", SourceType: "api",
		Reader: bytes.NewBufferString("# body two DIFFERENT"), SizeBytes: 19,
		CallerAPIKeyID: apiKeyPtr(apiKeyID), IdempotencyKey: "k-conflict",
	})
	if !errors.Is(err, domainerrors.ErrIdempotencyConflict) {
		t.Fatalf("second call err = %v, want ErrIdempotencyConflict", err)
	}
}

// TestIngestIdempotentUniqueRaceReplays verifies the concurrent-race path: the
// idempotency insert fails with ErrConflict, and the service resolves by
// reloading the stored row. Same hash -> deduped; this exercises the
// resolveIdempotencyRace path.
func TestIngestIdempotentUniqueRaceReplays(t *testing.T) {
	h, store, replay := newIdempotencyServiceHarness(t)
	apiKeyID := uuid.New()
	// Force the insert to fail with ErrConflict, simulating a concurrent insert.
	store.idempotencyInsertErr = domainerrors.ErrConflict

	// Seed replay store to return a matching hash (same body).
	// We must compute the same request hash the service will compute. Build the
	// replay after the first (and only) ingest creates the lineage.
	replay.rows = map[string]IdempotentIngestReplay{}

	// Pre-seed what the replay store returns. Because the insert fails, the
	// service will call resolveIdempotencyRace. We construct a matching record
	// by computing the same canonical hash the service uses.
	// The content hash for "# race body" via the service's copyToTemp:
	contentHash := sha256Hex("# race body")
	parentNodeID := ""
	requestHash := computeRequestSHA256ForTest(t, "race", "text/markdown", parentNodeID, contentHash)

	doc, err := model.NewDocumentIdentity(h.workspaceID, h.kb.ID, value.DocumentKindFile, "race.md", "api", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID, DocumentID: doc.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
		OriginalFilename: "race.md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: "raw/race", SHA256: contentHash, SizeBytes: 11,
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		DocumentID: doc.ID, DocumentRevisionID: rev.ID,
		Type: documentParseStartJobType, Status: value.JobStatusPending,
		Payload: map[string]any{"document_id": doc.ID.String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay.rows["k-race"] = IdempotentIngestReplay{
		Record: DocumentIngestIdempotency{
			RequestSHA256: requestHash, DocumentID: doc.ID, JobID: job.ID,
		},
		Document: doc, Revision: rev, Job: job,
	}

	result, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "race", FileName: "race.md", ContentType: "text/markdown", SourceType: "api",
		Reader: bytes.NewBufferString("# race body"), SizeBytes: 11,
		CallerAPIKeyID: apiKeyPtr(apiKeyID), IdempotencyKey: "k-race",
	})
	if err != nil {
		t.Fatalf("race resolve err = %v", err)
	}
	if !result.Deduped {
		t.Fatalf("race should resolve to deduped")
	}
	if result.Document.ID != doc.ID || result.Job.ID != job.ID {
		t.Fatalf("race resolved to wrong ids doc=%s job=%s", result.Document.ID, result.Job.ID)
	}
}

// TestIngestIdempotentMissingKeyStaysBackwardCompatible verifies that requests
// without an Idempotency-Key (and Session callers) never touch the idempotency
// path.
func TestIngestIdempotentMissingKeyStaysBackwardCompatible(t *testing.T) {
	h, store, _ := newIdempotencyServiceHarness(t)
	result, err := h.service.Ingest(context.Background(), IngestDocumentInput{
		WorkspaceID: h.workspaceID, KnowledgeBaseID: h.kb.ID,
		Title: "plain", FileName: "plain.md", ContentType: "text/markdown", SourceType: "upload",
		Reader: bytes.NewBufferString("plain"), SizeBytes: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deduped {
		t.Fatalf("plain ingest should not be deduped")
	}
	if len(store.idempotencyRows) != 0 {
		t.Fatalf("no idempotency row should be created without a key: %d", len(store.idempotencyRows))
	}
}

// computeRequestSHA256ForTest mirrors computeRequestSHA256 for the test's seed.
func computeRequestSHA256ForTest(t *testing.T, title, contentType, parentNodeID, contentSHA256 string) string {
	t.Helper()
	// Replicate the canonical struct used by the production helper.
	in := IngestDocumentInput{Title: title, ContentType: contentType, ParentNodeID: nil}
	if parentNodeID != "" {
		id, err := uuid.Parse(parentNodeID)
		if err == nil {
			in.ParentNodeID = &id
		}
	}
	return computeRequestSHA256(in, contentSHA256)
}
