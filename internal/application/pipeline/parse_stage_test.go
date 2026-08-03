package pipeline

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func TestParseStageCompletesRevisionWithoutActivatingDocument(t *testing.T) {
	workspaceID := uuid.New()
	documentID := uuid.New()
	revision := testDocumentRevision(workspaceID, documentID, value.DocumentKindFile, value.DocumentRevisionPending)
	document := &model.Document{
		ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		Kind: value.DocumentKindFile, Title: "指南", Status: value.DocumentStatusPending,
	}
	revisions := &fakeRevisionRepository{revision: revision}
	documents := &fakeRevisionDocumentGetter{document: document}
	raw := &countingRawStore{content: []byte("原始内容")}
	parser := &countingDocumentParser{markdown: "规范化内容"}
	stage := NewParseStage(revisions, documents, raw, parser, 1024)

	got, err := stage.Run(context.Background(), workspaceID, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != value.DocumentRevisionReady || got.NormalizedMarkdown != parser.markdown || got.ParseManifest == nil {
		t.Fatalf("completed revision = %#v", got)
	}
	if document.ActiveRevisionID != nil {
		t.Fatalf("active_revision_id = %v, want nil before publish", document.ActiveRevisionID)
	}
	if revisions.completeCalls != 1 || raw.openCalls != 1 || parser.calls != 1 {
		t.Fatalf("calls: complete=%d raw=%d parser=%d", revisions.completeCalls, raw.openCalls, parser.calls)
	}
	if raw.openedKey != revision.RawStorageKey {
		t.Fatalf("opened key = %q, want %q", raw.openedKey, revision.RawStorageKey)
	}
	if parser.input.Title != document.Title || parser.input.FileType != revision.FileType {
		t.Fatalf("parse input = %#v", parser.input)
	}
}

func TestParseStageRejectsContentBeyondLimit(t *testing.T) {
	workspaceID := uuid.New()
	revision := testDocumentRevision(workspaceID, uuid.New(), value.DocumentKindFile, value.DocumentRevisionPending)
	revisions := &fakeRevisionRepository{revision: revision}
	parser := &countingDocumentParser{}
	stage := NewParseStage(
		revisions,
		&fakeRevisionDocumentGetter{document: &model.Document{
			ID: revision.DocumentID, WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
			Kind: value.DocumentKindFile, Title: "Guide",
		}},
		&countingRawStore{content: []byte("12345")}, parser, 4,
	)

	_, err := stage.Run(context.Background(), workspaceID, revision.ID)
	if !errors.Is(err, parserport.ErrParseLimitExceeded) {
		t.Fatalf("Run error = %v, want ErrParseLimitExceeded", err)
	}
	if parser.calls != 0 || revisions.completeCalls != 0 {
		t.Fatalf("side effects: parser=%d complete=%d", parser.calls, revisions.completeCalls)
	}
}

func TestParseStageReadyRevisionRetrySkipsRawAndParser(t *testing.T) {
	workspaceID := uuid.New()
	revision := testDocumentRevision(workspaceID, uuid.New(), value.DocumentKindWeb, value.DocumentRevisionReady)
	parsed := parsedTestDocument("already parsed")
	revision.NormalizedMarkdown = parsed.Markdown
	revision.ParseManifest = &parsed.Manifest
	revisions := &fakeRevisionRepository{revision: revision}
	raw := &countingRawStore{content: []byte("must not open")}
	parser := &countingDocumentParser{markdown: "must not parse"}
	stage := NewParseStage(revisions, &fakeRevisionDocumentGetter{}, raw, parser, 1024)

	got, err := stage.Run(context.Background(), workspaceID, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != revision.ID || raw.openCalls != 0 || parser.calls != 0 || revisions.completeCalls != 0 {
		t.Fatalf("retry side effects: got=%s raw=%d parser=%d complete=%d", got.ID, raw.openCalls, parser.calls, revisions.completeCalls)
	}
}

func TestParseStageRejectsFAQBeforeSideEffects(t *testing.T) {
	workspaceID := uuid.New()
	revision := testDocumentRevision(workspaceID, uuid.New(), value.DocumentKindFAQ, value.DocumentRevisionPending)
	documents := &fakeRevisionDocumentGetter{}
	raw := &countingRawStore{content: []byte("must not open")}
	parser := &countingDocumentParser{markdown: "must not parse"}
	stage := NewParseStage(&fakeRevisionRepository{revision: revision}, documents, raw, parser, 1024)

	_, err := stage.Run(context.Background(), workspaceID, revision.ID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("Run error = %v, want ErrValidation", err)
	}
	if documents.getCalls != 0 || raw.openCalls != 0 || parser.calls != 0 {
		t.Fatalf("FAQ side effects: document=%d raw=%d parser=%d", documents.getCalls, raw.openCalls, parser.calls)
	}
}

func testDocumentRevision(workspaceID, documentID uuid.UUID, kind value.DocumentKind, status value.DocumentRevisionStatus) *model.DocumentRevision {
	return &model.DocumentRevision{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: uuid.New(), DocumentID: documentID,
		Kind: kind, RevisionNo: 1, FileType: "txt", RawStorageKey: "raw/document.txt",
		SHA256: "abc123", ProcessingVersion: 1, Status: status,
	}
}

type fakeRevisionRepository struct {
	revision      *model.DocumentRevision
	completeCalls int
}

func (r *fakeRevisionRepository) Get(_ context.Context, workspaceID, revisionID uuid.UUID) (*model.DocumentRevision, error) {
	if r.revision == nil || r.revision.WorkspaceID != workspaceID || r.revision.ID != revisionID {
		return nil, domainerrors.ErrNotFound
	}
	return r.revision, nil
}

func (r *fakeRevisionRepository) CompleteParse(
	_ context.Context,
	workspaceID, revisionID uuid.UUID,
	markdown string,
	manifest model.ParseManifest,
) error {
	if r.revision == nil || r.revision.WorkspaceID != workspaceID || r.revision.ID != revisionID {
		return domainerrors.ErrNotFound
	}
	r.completeCalls++
	r.revision.NormalizedMarkdown = markdown
	r.revision.ParseManifest = &manifest
	r.revision.Status = value.DocumentRevisionReady
	return nil
}

type fakeRevisionDocumentGetter struct {
	document *model.Document
	getCalls int
}

func (g *fakeRevisionDocumentGetter) Get(_ context.Context, workspaceID, documentID uuid.UUID) (*model.Document, error) {
	g.getCalls++
	if g.document == nil || g.document.WorkspaceID != workspaceID || g.document.ID != documentID {
		return nil, domainerrors.ErrNotFound
	}
	return g.document, nil
}

type countingRawStore struct {
	content   []byte
	openedKey string
	openCalls int
}

func (s *countingRawStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.openCalls++
	s.openedKey = key
	return io.NopCloser(bytes.NewReader(s.content)), nil
}

type countingDocumentParser struct {
	markdown string
	input    parserport.ParseInput
	calls    int
}

func (p *countingDocumentParser) Parse(_ context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	p.calls++
	p.input = input
	return parsedTestDocument(p.markdown), nil
}

func (*countingDocumentParser) Supports(string) bool { return true }
