//go:build integration

package db

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestChunkSetRepositoryBuildsOneReadySetAtomically(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "guide.md"); err != nil {
		t.Fatal(err)
	}

	repository := NewChunkSetRepository(database)
	candidate := &model.DocumentChunkSet{
		ID: uuid.New(), WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		Strategy: value.ChunkStrategyStandard, ChunkerVersion: 1,
		ChunkingConfig: map[string]any{"chunk_size": 512, "chunk_overlap": 80},
		ConfigHash:     "standard-config", Status: value.ChunkSetBuilding, CreatedAt: time.Now().UTC(),
	}
	first, err := repository.GetOrCreate(ctx, seed.workspaceID, candidate)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := *candidate
	duplicate.ID = uuid.New()
	second, err := repository.GetOrCreate(ctx, seed.workspaceID, &duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != candidate.ID || second.ID != first.ID {
		t.Fatalf("chunk set IDs = %s / %s, candidate %s", first.ID, second.ID, candidate.ID)
	}

	chunkID := uuid.New()
	chunkRevision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		ChunkSetID: first.ID, ChunkID: chunkID, RevisionNo: 1,
		Content: "正文", ContextHeader: "指南", EmbeddingContent: "指南\n\n正文",
		Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem,
	})
	if err != nil {
		t.Fatal(err)
	}
	activeRevisionID := chunkRevision.ID
	chunk := &model.Chunk{
		ID: chunkID, WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: first.ID,
		Sequence: 0, SourceContent: "正文", ActiveRevisionID: &activeRevisionID,
		SourceAnchor: value.SourceAnchor{SourceType: "markdown"}, Metadata: map[string]any{},
		CreatedAt: time.Now().UTC(),
	}
	ready, err := repository.Complete(
		ctx, seed.workspaceID, first.ID,
		[]*model.Chunk{chunk}, []*model.ChunkRevision{chunkRevision},
	)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != value.ChunkSetReady || ready.ChunkCount != 1 || ready.ReadyAt == nil {
		t.Fatalf("ready set = %#v", ready)
	}

	retried, err := repository.Complete(ctx, seed.workspaceID, first.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != ready.ID || retried.ChunkCount != 1 {
		t.Fatalf("retried set = %#v", retried)
	}
	chunks, err := NewChunkRepository(database).ListByChunkSet(ctx, seed.workspaceID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := NewChunkRevisionRepository(database).ListByChunkSet(ctx, seed.workspaceID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || len(revisions) != 1 || chunks[0].ActiveRevisionID == nil ||
		*chunks[0].ActiveRevisionID != revisions[0].ID {
		t.Fatalf("persisted chunks=%#v revisions=%#v", chunks, revisions)
	}
}
