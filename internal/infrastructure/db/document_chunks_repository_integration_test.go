//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/pipeline"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentChunksRepositoryReadsActiveGenerationChunkSetIncludingDisabled(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, documentRevisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, documentRevisionID, "installation.md"); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, documentID).
		Updates(map[string]any{"active_revision_id": documentRevisionID, "status": string(value.DocumentStatusReady)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).
		Update("chunking_config", JSONMap{"chunk_size": 512, "chunk_overlap": 80}).Error; err != nil {
		t.Fatal(err)
	}
	inactiveRevisionID := uuid.New()
	if err := database.WithContext(ctx).Create(&DocumentRevisionRow{
		ID: inactiveRevisionID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, Kind: string(value.DocumentKindFile), RevisionNo: 2,
		RevisionReason: "ingest", OriginalFilename: stringPointer("old.md"), FileType: stringPointer("markdown"),
		RawStorageKey: stringPointer("raw/" + inactiveRevisionID.String()), ProcessingVersion: 1,
		Status: string(value.DocumentRevisionReady),
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, _, _ = createReadyRetrievalChunk(
		t, ctx, database, seed, documentID, inactiveRevisionID, "old source", "old", "old",
	)
	setID, chunk, base := createReadyRetrievalChunk(
		t, ctx, database, seed, documentID, documentRevisionID, "source", "base", "base",
	)
	baseID, editorID := base.ID, seed.userID
	userRevision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID,
		ChunkSetID: setID, ChunkID: chunk.ID, RevisionNo: 2, BaseRevisionID: &baseID,
		Content: "disabled edit", EmbeddingContent: "disabled edit", Enabled: false,
		Status: value.ChunkRevisionReady, EditSource: value.ChunkEditSourceUser, EditorUserID: &editorID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(chunkRevisionToRow(userRevision)).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&ChunkRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, chunk.ID).
		Update("active_revision_id", userRevision.ID).Error; err != nil {
		t.Fatal(err)
	}

	repository := NewDocumentChunksRepository(database)
	page, err := repository.ListDocumentChunkFacts(ctx, seed.workspaceID, seed.kbID, documentID, appservice.DocumentChunkFactsFilter{Limit: 51})
	if err != nil {
		t.Fatal(err)
	}
	if page.GenerationID != seed.generationID || page.DocumentRevisionID != documentRevisionID || page.ChunkSetID != setID {
		t.Fatalf("page lineage = %#v", page)
	}
	if len(page.Items) != 1 || page.Items[0].ActiveRevision.Revision.ID != userRevision.ID ||
		page.Items[0].ActiveRevision.Revision.Enabled || page.Items[0].ActiveRevision.EditorNickname == nil ||
		*page.Items[0].ActiveRevision.EditorNickname != "schema-v2" {
		t.Fatalf("items = %#v", page.Items)
	}
	if page.Items[0].Chunk.ID != chunk.ID || page.Items[0].Chunk.Sequence != 0 {
		t.Fatalf("chunk = %#v", page.Items[0].Chunk)
	}

	disabled := false
	filtered, err := repository.ListDocumentChunkFacts(ctx, seed.workspaceID, seed.kbID, documentID, appservice.DocumentChunkFactsFilter{Enabled: &disabled, Limit: 51})
	if err != nil || len(filtered.Items) != 1 {
		t.Fatalf("disabled filter page = %#v error = %v", filtered, err)
	}
	enabled := true
	filtered, err = repository.ListDocumentChunkFacts(ctx, seed.workspaceID, seed.kbID, documentID, appservice.DocumentChunkFactsFilter{Enabled: &enabled, Limit: 51})
	if err != nil || len(filtered.Items) != 0 {
		t.Fatalf("enabled filter page = %#v error = %v", filtered, err)
	}

	otherWorkspaceID := createWorkspaceRow(t, ctx, database, "chunks-other-"+uuid.NewString())
	if _, err := repository.ListDocumentChunkFacts(ctx, otherWorkspaceID, seed.kbID, documentID, appservice.DocumentChunkFactsFilter{Limit: 51}); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace error = %v, want not found", err)
	}
}

func TestDocumentChunksRepositoryReadsFAQChunkWithoutRetrievalProjection(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	faqRepository := NewFAQRepository(database)
	document, faq, job := testFAQPersistenceAggregate(t, seed)
	if err := faqRepository.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.FAQRevisionTx) error {
		return tx.CreateFAQRevisionAggregate(txCtx, document, faq, job)
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, document.ID).
		Updates(map[string]any{
			"active_revision_id": faq.DocumentRevision.ID,
			"status":             string(value.DocumentStatusReady),
		}).Error; err != nil {
		t.Fatal(err)
	}
	chunkSetID, err := pipeline.NewFAQChunkStage(faqRepository, NewChunkSetRepository(database)).Build(
		ctx, seed.workspaceID, faq.DocumentRevision.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	page, err := NewDocumentChunksRepository(database).ListDocumentChunkFacts(
		ctx, seed.workspaceID, seed.kbID, document.ID, appservice.DocumentChunkFactsFilter{Limit: 51},
	)
	if err != nil {
		t.Fatal(err)
	}
	if page.ChunkSetID != chunkSetID || page.DocumentRevisionID != faq.DocumentRevision.ID || len(page.Items) != 1 {
		t.Fatalf("FAQ page = %#v", page)
	}
	if page.Items[0].ActiveRevision.Revision.EditSource != value.ChunkEditSourceSystem ||
		page.Items[0].ActiveRevision.Revision.Content != faq.Answer {
		t.Fatalf("FAQ revision = %#v", page.Items[0].ActiveRevision)
	}
	var retrievalCount int64
	if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).
		Where("workspace_id = ? AND document_id = ?", seed.workspaceID, document.ID).
		Count(&retrievalCount).Error; err != nil {
		t.Fatal(err)
	}
	if retrievalCount != 0 {
		t.Fatalf("retrieval entries = %d, want zero", retrievalCount)
	}
}

func stringPointer(value string) *string { return &value }
