//go:build integration

package db

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestDocumentAssetRepositoryCreateDeleteAndList(t *testing.T) {
	ctx, database := openIntegrationTestDB(t)

	var seed knowledgeSchemaSeed
	documentID := uuid.New()
	revisionID := uuid.New()

	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed = insertKnowledgeSchemaSeed(t, ctx, tx)
		// insertFileDocumentRevision 同时插入 document + file_tree_node + revision
		return insertFileDocumentRevision(ctx, tx, seed, documentID, revisionID, "asset-test.pdf")
	})
	if err != nil {
		t.Fatalf("seed lineage failed: %v", err)
	}

	repo := NewDocumentAssetRepository(database)

	// CreateAssets
	now := time.Now().UTC()
	assets := []model.Asset{
		{
			ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			DocumentID: documentID, DocumentRevisionID: revisionID,
			OriginalRef: "image1.png", StorageKey: "assets/img1.png",
			PublicURL: "https://cdn/img1.png", MimeType: "image/png",
			SHA256: "abc123", SizeBytes: 1024, Metadata: map[string]any{"alt": "图1"},
			CreatedAt: now,
		},
		{
			ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			DocumentID: documentID, DocumentRevisionID: revisionID,
			OriginalRef: "image2.jpg", StorageKey: "assets/img2.jpg",
			PublicURL: "https://cdn/img2.jpg", MimeType: "image/jpeg",
			SHA256: "def456", SizeBytes: 2048, Metadata: map[string]any{"alt": "图2"},
			CreatedAt: now,
		},
	}

	if err := repo.CreateAssets(ctx, assets); err != nil {
		t.Fatalf("CreateAssets() error = %v", err)
	}

	// ListAssetsByRevision
	got, err := repo.ListAssetsByRevision(ctx, seed.workspaceID, revisionID)
	if err != nil {
		t.Fatalf("ListAssetsByRevision() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list assets = %d, want 2", len(got))
	}
	// 验证字段回读
	if got[0].MimeType != "image/png" && got[1].MimeType != "image/png" {
		t.Fatalf("expected one png asset, got %s and %s", got[0].MimeType, got[1].MimeType)
	}

	// DeleteAssetsByRevision（幂等：先删再写）
	if err := repo.DeleteAssetsByRevision(ctx, seed.workspaceID, revisionID); err != nil {
		t.Fatalf("DeleteAssetsByRevision() error = %v", err)
	}
	if err := repo.CreateAssets(ctx, assets); err != nil {
		t.Fatalf("CreateAssets() after delete error = %v", err)
	}
	got, err = repo.ListAssetsByRevision(ctx, seed.workspaceID, revisionID)
	if err != nil {
		t.Fatalf("ListAssetsByRevision() after re-create error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("list assets after re-create = %d, want 2 (idempotent)", len(got))
	}
}

func TestDocumentAssetRepositoryCreateEmptyNoop(t *testing.T) {
	ctx, database := openIntegrationTestDB(t)
	repo := NewDocumentAssetRepository(database)

	// 空 slice / nil 不应报错
	if err := repo.CreateAssets(ctx, nil); err != nil {
		t.Fatalf("CreateAssets(nil) error = %v", err)
	}
	if err := repo.CreateAssets(ctx, []model.Asset{}); err != nil {
		t.Fatalf("CreateAssets(empty) error = %v", err)
	}
}
