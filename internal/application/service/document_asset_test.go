package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type fakeAssetReader struct {
	assets map[uuid.UUID][]*model.Asset // key: revisionID
}

func (r *fakeAssetReader) ListAssetsByRevision(_ context.Context, workspaceID, revisionID uuid.UUID) ([]*model.Asset, error) {
	return r.assets[revisionID], nil
}

type fakeAssetDocumentReader struct {
	docs map[uuid.UUID]*model.Document
}

func (r *fakeAssetDocumentReader) Get(_ context.Context, workspaceID, id uuid.UUID) (*model.Document, error) {
	doc, ok := r.docs[id]
	if !ok || doc.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return doc, nil
}

func TestDocumentAssetServiceListsActiveRevisionAssets(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()
	documentID := uuid.New()
	revisionID := uuid.New()

	doc := &model.Document{
		ID:               documentID,
		WorkspaceID:      workspaceID,
		KnowledgeBaseID:  uuid.New(),
		Kind:             value.DocumentKindFile,
		ActiveRevisionID: &revisionID,
	}

	asset := &model.Asset{
		ID:                 uuid.New(),
		DocumentID:         documentID,
		DocumentRevisionID: revisionID,
		WorkspaceID:        workspaceID,
		OriginalRef:        "img.png",
		PublicURL:          "https://cdn/img.png",
		MimeType:           "image/png",
		SizeBytes:          100,
	}

	service := NewDocumentAssetService(
		&fakeAssetReader{assets: map[uuid.UUID][]*model.Asset{revisionID: {asset}}},
		&fakeAssetDocumentReader{docs: map[uuid.UUID]*model.Document{documentID: doc}},
	)

	got, err := service.ListByDocument(ctx, workspaceID, documentID)
	if err != nil {
		t.Fatalf("ListByDocument() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("assets = %d, want 1", len(got))
	}
	if got[0].OriginalRef != "img.png" {
		t.Fatalf("original_ref = %q", got[0].OriginalRef)
	}
}

func TestDocumentAssetServiceEmptyWhenNoActiveRevision(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()
	documentID := uuid.New()

	doc := &model.Document{
		ID:          documentID,
		WorkspaceID: workspaceID,
		Kind:        value.DocumentKindFile,
	}

	service := NewDocumentAssetService(
		&fakeAssetReader{},
		&fakeAssetDocumentReader{docs: map[uuid.UUID]*model.Document{documentID: doc}},
	)

	got, err := service.ListByDocument(ctx, workspaceID, documentID)
	if err != nil {
		t.Fatalf("ListByDocument() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("assets = %d, want 0 (no active revision)", len(got))
	}
}

func TestDocumentAssetServiceNotFound(t *testing.T) {
	ctx := context.Background()
	service := NewDocumentAssetService(&fakeAssetReader{}, &fakeAssetDocumentReader{})

	_, err := service.ListByDocument(ctx, uuid.New(), uuid.New())
	if !isNotFound(err) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
