package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestRestrictedDocumentLookupReturnsNotFoundOutsideBindings(t *testing.T) {
	workspaceID := uuid.New()
	allowedKB := uuid.New()
	otherKB := uuid.New()
	repo := newFakeDocumentQueryRepository()
	doc := &model.Document{ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: otherKB}
	repo.items[doc.ID] = doc
	repo.workspaceIDs[doc.ID] = workspaceID
	svc := NewDocumentService(repo, newFakeKnowledgeBaseRepository())

	access := value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{allowedKB}}
	_, err := svc.Get(context.Background(), access, doc.ID)
	if err != domainerrors.ErrNotFound {
		t.Fatalf("restricted get error = %v, want ErrNotFound", err)
	}
}

func TestUnrestrictedDocumentLookupReturnsDocument(t *testing.T) {
	workspaceID := uuid.New()
	repo := newFakeDocumentQueryRepository()
	doc := &model.Document{ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: uuid.New()}
	repo.items[doc.ID] = doc
	repo.workspaceIDs[doc.ID] = workspaceID
	svc := NewDocumentService(repo, newFakeKnowledgeBaseRepository())

	access := value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}
	got, err := svc.Get(context.Background(), access, doc.ID)
	if err != nil {
		t.Fatalf("unrestricted get error = %v", err)
	}
	if got.ID != doc.ID {
		t.Fatalf("got = %#v", got)
	}
}

func TestRestrictedDocumentDeleteReturnsNotFoundOutsideBindings(t *testing.T) {
	workspaceID := uuid.New()
	allowedKB := uuid.New()
	otherKB := uuid.New()
	repo := newFakeDocumentQueryRepository()
	doc := &model.Document{ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: otherKB}
	repo.items[doc.ID] = doc
	repo.workspaceIDs[doc.ID] = workspaceID
	svc := NewDocumentService(repo, newFakeKnowledgeBaseRepository())

	access := value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{allowedKB}}
	if err := svc.Delete(context.Background(), access, doc.ID); err != domainerrors.ErrNotFound {
		t.Fatalf("restricted delete error = %v, want ErrNotFound", err)
	}
	if repo.deletedDocumentID == doc.ID {
		t.Fatal("越界文档不应被删除")
	}
}
