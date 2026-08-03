//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestRetrievalCleanupDeletesOnlyExpiredWorkspaceDataAndPreservesActiveGeneration(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	now := time.Now().UTC()
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	other := insertKnowledgeSchemaSeed(t, ctx, database)

	oldRetiredGenerationID := insertCleanupGeneration(t, ctx, database, seed, now.Add(-8*24*time.Hour))
	freshRetiredGenerationID := insertCleanupGeneration(t, ctx, database, seed, now.Add(-6*24*time.Hour))
	otherRetiredGenerationID := insertCleanupGeneration(t, ctx, database, other, now.Add(-8*24*time.Hour))
	if err := database.WithContext(ctx).Model(&IndexGenerationRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).
		Update("base_generation_id", oldRetiredGenerationID).Error; err != nil {
		t.Fatal(err)
	}

	expiredStagingID := insertCleanupEntry(
		t, ctx, database, seed, seed.generationID, value.RetrievalEntryStaging,
		now.Add(-25*time.Hour), nil,
	)
	freshFailedID := insertCleanupEntry(
		t, ctx, database, seed, seed.generationID, value.RetrievalEntryFailed,
		now.Add(-23*time.Hour), nil,
	)
	expiredRetiredAt := now.Add(-8 * 24 * time.Hour)
	expiredRetiredID := insertCleanupEntry(
		t, ctx, database, seed, seed.generationID, value.RetrievalEntryRetired,
		now.Add(-10*24*time.Hour), &expiredRetiredAt,
	)
	oldGenerationEntryID := insertCleanupEntry(
		t, ctx, database, seed, oldRetiredGenerationID, value.RetrievalEntryPublished,
		now.Add(-10*24*time.Hour), nil,
	)

	repository := NewRetrievalCleanupRepository(database)
	first, err := repository.Cleanup(ctx, appservice.RetrievalCleanupRequest{
		WorkspaceID:         seed.workspaceID,
		FailedStagingBefore: now.Add(-24 * time.Hour),
		RetiredBefore:       now.Add(-7 * 24 * time.Hour),
		BatchSize:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.DeletedEntries != 1 || first.DeletedGenerations != 0 {
		t.Fatalf("first bounded cleanup result = %#v", first)
	}
	result, err := repository.Cleanup(ctx, appservice.RetrievalCleanupRequest{
		WorkspaceID:         seed.workspaceID,
		FailedStagingBefore: now.Add(-24 * time.Hour),
		RetiredBefore:       now.Add(-7 * 24 * time.Hour),
		BatchSize:           10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedEntries != 1 || result.DeletedGenerations != 1 {
		t.Fatalf("second cleanup result = %#v", result)
	}
	if err := database.WithContext(ctx).Exec("SET CONSTRAINTS index_generations_base_fk IMMEDIATE").Error; err != nil {
		t.Fatal(err)
	}

	assertCleanupRowExists(t, ctx, database, "retrieval_entries", expiredStagingID, false)
	assertCleanupRowExists(t, ctx, database, "retrieval_entries", expiredRetiredID, false)
	assertCleanupRowExists(t, ctx, database, "retrieval_entries", freshFailedID, true)
	assertCleanupRowExists(t, ctx, database, "retrieval_entries", oldGenerationEntryID, false)
	assertCleanupRowExists(t, ctx, database, "knowledge_base_index_generations", oldRetiredGenerationID, false)
	assertCleanupRowExists(t, ctx, database, "knowledge_base_index_generations", freshRetiredGenerationID, true)
	assertCleanupRowExists(t, ctx, database, "knowledge_base_index_generations", otherRetiredGenerationID, true)
	assertCleanupRowExists(t, ctx, database, "knowledge_base_index_generations", seed.generationID, true)

	var active IndexGenerationRow
	if err := database.WithContext(ctx).First(&active, "workspace_id = ? AND id = ?", seed.workspaceID, seed.generationID).Error; err != nil {
		t.Fatal(err)
	}
	if active.BaseGenerationID != nil {
		t.Fatalf("active base_generation_id = %v, want nil after retained base cleanup", active.BaseGenerationID)
	}
}

func insertCleanupGeneration(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	seed knowledgeSchemaSeed,
	retiredAt time.Time,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO knowledge_base_index_generations "+
			"(id, workspace_id, knowledge_base_id, embedding_model_id, provider_id, model_name, "+
			"embedding_dimension, model_config_hash, chunker_version, config_hash, status, created_at, retired_at) "+
			"VALUES (?, ?, ?, ?, ?, 'text-embedding', 1024, 'model-hash', 1, ?, 'retired', ?, ?)",
		id, seed.workspaceID, seed.kbID, seed.modelID, seed.providerID,
		"cleanup-"+id.String(), retiredAt.Add(-time.Hour), retiredAt,
	).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCleanupEntry(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	seed knowledgeSchemaSeed,
	generationID uuid.UUID,
	state value.RetrievalEntryState,
	createdAt time.Time,
	retiredAt *time.Time,
) uuid.UUID {
	t.Helper()
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, uuid.NewString()+".md"); err != nil {
		t.Fatal(err)
	}
	setID, chunk, revision := createReadyRetrievalChunk(
		t, ctx, database, seed, documentID, revisionID, "source", "search", "content",
	)
	entry := &model.RetrievalEntry{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		IndexGenerationID: generationID, DocumentID: documentID, DocumentRevisionID: revisionID,
		ChunkSetID: setID, ChunkID: chunk.ID, ChunkRevisionID: revision.ID,
		State: value.RetrievalEntryStaging, SearchContent: "search", Content: "content",
		SourceAnchor: chunk.SourceAnchor, Metadata: map[string]any{}, CreatedAt: createdAt,
	}
	if err := NewRetrievalRepository(database).StageBatch(
		ctx, seed.workspaceID, "simple", 1024,
		[]indexport.StageEntry{{Entry: entry, Embedding: make([]float32, 1024)}},
	); err != nil {
		t.Fatal(err)
	}
	updates := map[string]any{"state": string(state), "retired_at": retiredAt}
	if state == value.RetrievalEntryPublished {
		updates["published_at"] = createdAt
	}
	if err := database.WithContext(ctx).Model(&RetrievalEntryRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, entry.ID).
		Updates(updates).Error; err != nil {
		t.Fatal(err)
	}
	return entry.ID
}

func assertCleanupRowExists(
	t *testing.T,
	ctx context.Context,
	database *gorm.DB,
	table string,
	id uuid.UUID,
	want bool,
) {
	t.Helper()
	var count int64
	if err := database.WithContext(ctx).Table(table).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if (count == 1) != want {
		t.Fatalf("%s id %s exists = %v, want %v", table, id, count == 1, want)
	}
}
