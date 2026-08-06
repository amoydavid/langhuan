package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentServiceGetReturnsDocumentDTO(t *testing.T) {
	repo := newFakeDocumentQueryRepository()
	svc := NewDocumentService(repo, newFakeKnowledgeBaseRepository())
	workspaceID := uuid.New()
	doc := validDocumentForServiceTest(t)
	repo.items[doc.ID] = doc
	repo.workspaceIDs[doc.ID] = workspaceID

	got, err := svc.Get(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, doc.ID)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got.ID != doc.ID {
		t.Fatalf("ID = %s, want %s", got.ID, doc.ID)
	}
	if got.Title != doc.Title {
		t.Fatalf("Title = %q, want %q", got.Title, doc.Title)
	}
}

func TestDocumentServiceGetReturnsWebSourceURI(t *testing.T) {
	repo := newFakeDocumentQueryRepository()
	service := NewDocumentService(repo, newFakeKnowledgeBaseRepository())
	workspaceID := uuid.New()
	document, err := model.NewDocumentIdentity(
		workspaceID, uuid.New(), value.DocumentKindWeb, "网页", "crawler", "https://example.com/page", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	repo.items[document.ID] = document
	repo.workspaceIDs[document.ID] = workspaceID

	got, err := service.Get(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceURI != "https://example.com/page" {
		t.Fatalf("source_uri = %q", got.SourceURI)
	}
}

func TestDocumentServiceListReturnsTypedFAQQuestionCount(t *testing.T) {
	workspaceID := uuid.New()
	kbRepo := newFakeKnowledgeBaseRepository()
	kb, err := model.NewKnowledgeBase(workspaceID, "faq", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	kbRepo.items[kb.ID] = kb
	docRepo := newFakeDocumentQueryRepository()
	document, err := model.NewDocumentIdentity(
		workspaceID, kb.ID, value.DocumentKindFAQ, "退款政策", "api", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	document.FAQQuestionCount = 3
	docRepo.items[document.ID] = document
	docRepo.workspaceIDs[document.ID] = workspaceID

	items, err := NewDocumentService(docRepo, kbRepo).List(context.Background(), DocumentListFilter{WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FAQQuestionCount != 3 {
		t.Fatalf("FAQ question count = %#v, want 3", items)
	}
}

func TestDocumentServiceGetPropagatesNotFound(t *testing.T) {
	repo := newFakeDocumentQueryRepository()
	svc := NewDocumentService(repo, newFakeKnowledgeBaseRepository())

	_, err := svc.Get(context.Background(), value.ResourceAccess{WorkspaceID: uuid.New(), Unrestricted: true}, uuid.New())
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDocumentServiceDeleteUsesWorkspaceScopedRepository(t *testing.T) {
	repo := newFakeDocumentQueryRepository()
	service := NewDocumentService(repo, newFakeKnowledgeBaseRepository())
	workspaceID, documentID := uuid.New(), uuid.New()

	if err := service.Delete(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, documentID); err != nil {
		t.Fatal(err)
	}
	if repo.deletedWorkspaceID != workspaceID || repo.deletedDocumentID != documentID || repo.deleteCalls != 1 {
		t.Fatalf("delete call = workspace %s document %s calls %d", repo.deletedWorkspaceID, repo.deletedDocumentID, repo.deleteCalls)
	}
}

func TestDocumentServiceDeleteRejectsEmptyIDsBeforeRepository(t *testing.T) {
	repo := newFakeDocumentQueryRepository()
	service := NewDocumentService(repo, newFakeKnowledgeBaseRepository())

	if err := service.Delete(context.Background(), value.ResourceAccess{WorkspaceID: uuid.Nil, Unrestricted: true}, uuid.New()); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("empty workspace error = %v", err)
	}
	if err := service.Delete(context.Background(), value.ResourceAccess{WorkspaceID: uuid.New(), Unrestricted: true}, uuid.Nil); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("empty document error = %v", err)
	}
	if repo.deleteCalls != 0 {
		t.Fatalf("delete calls = %d, want 0", repo.deleteCalls)
	}
}

type fakeDocumentQueryRepository struct {
	items        map[uuid.UUID]*model.Document
	workspaceIDs map[uuid.UUID]uuid.UUID
	listCalls    int
	deleteCalls  int

	deletedWorkspaceID uuid.UUID
	deletedDocumentID  uuid.UUID
}

func (r *fakeDocumentQueryRepository) List(_ context.Context, filter DocumentListFilter) ([]*model.Document, error) {
	r.listCalls++
	result := make([]*model.Document, 0)
	for id, doc := range r.items {
		if r.workspaceIDs[id] == filter.WorkspaceID && doc.KnowledgeBaseID == filter.KnowledgeBaseID && (filter.Kind == nil || doc.Kind == *filter.Kind) {
			result = append(result, doc)
		}
	}
	return result, nil
}

func newFakeDocumentQueryRepository() *fakeDocumentQueryRepository {
	return &fakeDocumentQueryRepository{
		items:        make(map[uuid.UUID]*model.Document),
		workspaceIDs: make(map[uuid.UUID]uuid.UUID),
	}
}

func (r *fakeDocumentQueryRepository) Get(_ context.Context, workspaceID uuid.UUID, id uuid.UUID) (*model.Document, error) {
	doc, ok := r.items[id]
	if !ok || r.workspaceIDs[id] != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return doc, nil
}

func (r *fakeDocumentQueryRepository) Delete(_ context.Context, workspaceID, documentID uuid.UUID) error {
	r.deleteCalls++
	r.deletedWorkspaceID = workspaceID
	r.deletedDocumentID = documentID
	return nil
}

func validDocumentForServiceTest(t *testing.T) *model.Document {
	t.Helper()
	doc, err := model.NewDocument(model.NewDocumentInput{
		KnowledgeBaseID: uuid.New(),
		Title:           "Launch notes",
		FileType:        "md",
		SourceType:      "upload",
		Status:          value.DocumentStatusCompleted,
		SHA256:          "abc123",
		RawStorageKey:   "raw/doc.md",
		SizeBytes:       42,
		ContentType:     "text/markdown",
		Metadata:        map[string]any{"owner": "ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDocumentServiceListValidatesKnowledgeBaseAndReturnsDTOs(t *testing.T) {
	workspaceID := uuid.New()
	kbRepo := newFakeKnowledgeBaseRepository()
	kb, err := model.NewKnowledgeBase(workspaceID, "docs", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	kbRepo.items[kb.ID] = kb
	docRepo := newFakeDocumentQueryRepository()
	doc := validDocumentForServiceTest(t)
	doc.KnowledgeBaseID = kb.ID
	docRepo.items[doc.ID] = doc
	docRepo.workspaceIDs[doc.ID] = workspaceID

	got, err := NewDocumentService(docRepo, kbRepo).List(context.Background(), DocumentListFilter{WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != doc.ID {
		t.Fatalf("List() = %#v", got)
	}
}

func TestDocumentServiceListRejectsKnowledgeBaseFromAnotherWorkspaceBeforeQuery(t *testing.T) {
	ownerWorkspaceID := uuid.New()
	kbRepo := newFakeKnowledgeBaseRepository()
	kb, err := model.NewKnowledgeBase(ownerWorkspaceID, "docs", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	kbRepo.items[kb.ID] = kb
	docRepo := newFakeDocumentQueryRepository()

	_, err = NewDocumentService(docRepo, kbRepo).List(context.Background(), DocumentListFilter{WorkspaceID: uuid.New(), KnowledgeBaseID: kb.ID})
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("List() error = %v, want ErrNotFound", err)
	}
	if docRepo.listCalls != 0 {
		t.Fatalf("document List called %d times before knowledge-base ownership validation", docRepo.listCalls)
	}
}
