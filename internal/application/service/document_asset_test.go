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
	byID   map[uuid.UUID]*model.Asset   // key: assetID
}

func (r *fakeAssetReader) ListAssetsByRevision(_ context.Context, workspaceID, revisionID uuid.UUID) ([]*model.Asset, error) {
	return r.assets[revisionID], nil
}

func (r *fakeAssetReader) GetByID(_ context.Context, workspaceID, assetID uuid.UUID) (*model.Asset, error) {
	asset, ok := r.byID[assetID]
	if !ok || asset.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return asset, nil
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

func TestDocumentAssetServiceGetByID(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()
	assetID := uuid.New()
	asset := &model.Asset{ID: assetID, WorkspaceID: workspaceID, StorageKey: "assets/ws/doc/rev/a.png"}
	reader := &fakeAssetReader{
		byID: map[uuid.UUID]*model.Asset{assetID: asset},
	}
	service := NewDocumentAssetService(reader, &fakeAssetDocumentReader{})

	got, err := service.GetByID(ctx, workspaceID, assetID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ID != assetID || got.StorageKey != asset.StorageKey {
		t.Fatalf("asset = %#v, want %#v", got, asset)
	}
}

func TestDocumentAssetServiceGetByIDCrossWorkspaceRejected(t *testing.T) {
	ctx := context.Background()
	assetID := uuid.New()
	reader := &fakeAssetReader{
		byID: map[uuid.UUID]*model.Asset{assetID: {ID: assetID, WorkspaceID: uuid.New()}},
	}
	service := NewDocumentAssetService(reader, &fakeAssetDocumentReader{})

	_, err := service.GetByID(ctx, uuid.New(), assetID)
	if !isNotFound(err) {
		t.Fatalf("error = %v, want ErrNotFound (cross-workspace)", err)
	}
}
