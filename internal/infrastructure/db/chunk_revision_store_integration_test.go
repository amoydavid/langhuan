//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestChunkRevisionStoreAppendsUnderWorkspaceLineage(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, documentRevisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, documentRevisionID, "edit.md"); err != nil {
		t.Fatal(err)
	}
	setID, chunk, base := createReadyRetrievalChunk(
		t, ctx, database, seed, documentID, documentRevisionID, "source", "base", "base",
	)
	editorID, baseID := seed.userID, base.ID
	created, err := model.NewChunkRevision(model.NewChunkRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID,
		ChunkSetID: setID, ChunkID: chunk.ID, RevisionNo: 2, BaseRevisionID: &baseID,
		Content: "edited", EmbeddingContent: "edited", Enabled: true,
		Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceUser, EditorUserID: &editorID,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID,
		Type: "chunk_revision_index", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewChunkRevisionStore(database)
	err = store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.ChunkEditTx) error {
		locked, err := tx.GetChunkForUpdate(txCtx, chunk.ID)
		if err != nil {
			return err
		}
		if locked.ActiveRevisionID == nil || *locked.ActiveRevisionID != base.ID {
			t.Fatalf("active revision = %v", locked.ActiveRevisionID)
		}
		return tx.CreateChunkRevisionAndJob(txCtx, created, job)
	})
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := store.ListChunkRevisions(ctx, seed.workspaceID, seed.kbID, chunk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].Revision.ID != created.ID || revisions[0].EditorNickname == nil || *revisions[0].EditorNickname != "schema-v2" {
		t.Fatalf("revisions = %#v", revisions)
	}
}

func TestChunkRevisionStorePublishesEnabledAndDisabledWithContentVersionCAS(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{name: "enabled publishes replacement", enabled: true},
		{name: "disabled retires without replacement", enabled: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, database := newAuthTestDB(t)
			seed := insertKnowledgeSchemaSeed(t, ctx, database)
			documentID, documentRevisionID := uuid.New(), uuid.New()
			if err := insertFileDocumentRevision(ctx, database, seed, documentID, documentRevisionID, "publish-edit.md"); err != nil {
				t.Fatal(err)
			}
			setID, chunk, base := createReadyRetrievalChunk(
				t, ctx, database, seed, documentID, documentRevisionID, "source", "base", "base",
			)
			oldEntry := stageChunkRevisionEntry(t, ctx, database, seed, chunk, base)
			now := time.Now().UTC()
			if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).
				Where("workspace_id = ? AND id = ?", seed.workspaceID, oldEntry.ID).
				Updates(map[string]any{
					"state": string(value.RetrievalEntryPublished), "published_at": now,
				}).Error; err != nil {
				t.Fatal(err)
			}
			editorID, baseID := seed.userID, base.ID
			newRevision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
				WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
				DocumentID: documentID, DocumentRevisionID: documentRevisionID,
				ChunkSetID: setID, ChunkID: chunk.ID, RevisionNo: 2, BaseRevisionID: &baseID,
				Content: "edited", EmbeddingContent: "edited", Enabled: test.enabled,
				Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceUser, EditorUserID: &editorID,
			})
			if err != nil {
				t.Fatal(err)
			}
			job, err := model.NewJob(model.NewJobInput{
				WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
				DocumentID: documentID, DocumentRevisionID: documentRevisionID,
				Type: "chunk_revision_index", Status: value.JobStatusPending,
			})
			if err != nil {
				t.Fatal(err)
			}
			store := NewChunkRevisionStore(database)
			if err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.ChunkEditTx) error {
				return tx.CreateChunkRevisionAndJob(txCtx, newRevision, job)
			}); err != nil {
				t.Fatal(err)
			}
			request := appservice.ChunkRevisionIndexRequest{
				WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, GenerationID: seed.generationID,
				DocumentID: documentID, DocumentRevisionID: documentRevisionID, ChunkSetID: setID,
				ChunkID: chunk.ID, BaseRevisionID: base.ID, NewRevisionID: newRevision.ID,
				ExpectedContentVersion: 0, JobID: job.ID,
			}
			if _, err := store.Load(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := store.MarkIndexing(ctx, request); err != nil {
				t.Fatal(err)
			}
			var newEntry *model.RetrievalEntry
			if test.enabled {
				newEntry = stageChunkRevisionEntry(t, ctx, database, seed, chunk, newRevision)
			}
			publishInput := appservice.PublishChunkRevisionInput{
				WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, GenerationID: seed.generationID,
				ChunkID: chunk.ID, BaseRevisionID: base.ID, NewRevisionID: newRevision.ID,
				ExpectedContentVersion: 0,
			}
			if err := store.Publish(ctx, publishInput, newEntry); err != nil {
				t.Fatal(err)
			}
			assertChunkRevisionPublication(t, ctx, database, seed, chunk.ID, base.ID, newRevision.ID, test.enabled)
			if err := store.Publish(ctx, publishInput, newEntry); !errors.Is(err, domainerrors.ErrRevisionConflict) {
				t.Fatalf("second publish error = %v, want revision conflict", err)
			}
		})
	}
}

func stageChunkRevisionEntry(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	seed knowledgeSchemaSeed,
	chunk *model.Chunk,
	revision *model.ChunkRevision,
) *model.RetrievalEntry {
	t.Helper()
	entry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: seed.generationID, DocumentID: chunk.DocumentID,
		DocumentRevisionID: chunk.DocumentRevisionID, ChunkSetID: chunk.ChunkSetID,
		ChunkID: chunk.ID, ChunkRevisionID: revision.ID, State: value.RetrievalEntryStaging,
		SearchContent: revision.EmbeddingContent, Content: revision.Content,
		SourceAnchor: chunk.SourceAnchor, Metadata: map[string]any{}, CreatedAt: time.Now().UTC(),
	}
	vector := make([]float32, 1024)
	vector[0] = 1
	if err := NewRetrievalRepository(database, nil).StageBatch(ctx, seed.workspaceID, "simple", 1024, []indexport.StageEntry{{
		Entry: entry, Embedding: vector,
	}}); err != nil {
		t.Fatal(err)
	}
	return entry
}

func assertChunkRevisionPublication(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	seed knowledgeSchemaSeed,
	chunkID, baseRevisionID, newRevisionID uuid.UUID,
	enabled bool,
) {
	t.Helper()
	var chunkRow ChunkRow
	if err := database.WithContext(ctx).First(&chunkRow, "workspace_id = ? AND id = ?", seed.workspaceID, chunkID).Error; err != nil {
		t.Fatal(err)
	}
	if chunkRow.ActiveRevisionID == nil || *chunkRow.ActiveRevisionID != newRevisionID {
		t.Fatalf("active revision = %v, want %s", chunkRow.ActiveRevisionID, newRevisionID)
	}
	var revisionRow ChunkRevisionRow
	if err := database.WithContext(ctx).First(&revisionRow, "workspace_id = ? AND id = ?", seed.workspaceID, newRevisionID).Error; err != nil {
		t.Fatal(err)
	}
	if revisionRow.Status != string(value.ChunkRevisionReady) || (enabled && revisionRow.IndexedAt == nil) {
		t.Fatalf("new revision = %#v", revisionRow)
	}
	var oldRow RetrievalEntryRow
	if err := database.WithContext(ctx).First(&oldRow,
		"workspace_id = ? AND index_generation_id = ? AND chunk_revision_id = ?",
		seed.workspaceID, seed.generationID, baseRevisionID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if oldRow.State != string(value.RetrievalEntryRetired) || oldRow.RetiredAt == nil {
		t.Fatalf("old entry = %#v", oldRow)
	}
	var publishedCount int64
	if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).Where(
		"workspace_id = ? AND index_generation_id = ? AND chunk_id = ? AND state = ?",
		seed.workspaceID, seed.generationID, chunkID, value.RetrievalEntryPublished,
	).Count(&publishedCount).Error; err != nil {
		t.Fatal(err)
	}
	wantPublished := int64(0)
	if enabled {
		wantPublished = 1
	}
	if publishedCount != wantPublished {
		t.Fatalf("published entries = %d, want %d", publishedCount, wantPublished)
	}
	var kb KnowledgeBaseRow
	var generation IndexGenerationRow
	if err := database.WithContext(ctx).First(&kb, "workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).First(&generation, "workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).Error; err != nil {
		t.Fatal(err)
	}
	if kb.ContentVersion != 1 || generation.IndexedContentVersion != 1 {
		t.Fatalf("versions = kb %d generation %d", kb.ContentVersion, generation.IndexedContentVersion)
	}
}
