package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestChunkStageBuildsIdempotentSet(t *testing.T) {
	workspaceID := uuid.New()
	documentID := uuid.New()
	revision := testDocumentRevision(workspaceID, documentID, value.DocumentKindFile, value.DocumentRevisionReady)
	parsed := parsedTestDocument("第一段内容。第二段内容。")
	revision.NormalizedMarkdown = parsed.Markdown
	revision.ParseManifest = &parsed.Manifest
	document := &model.Document{
		ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		Kind: value.DocumentKindFile, Title: "指南",
	}
	generation := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		ChunkerVersion: CurrentStandardChunkerVersion,
		ChunkingConfig: map[string]any{"chunk_size": float64(8), "chunk_overlap": float64(2)},
	}
	sets := &fakeChunkSetRepository{}
	stage := NewChunkStage(
		&fakeRevisionRepository{revision: revision},
		&fakeRevisionDocumentGetter{document: document},
		&fakeIndexGenerationGetter{generation: generation},
		sets,
		NewChunker(),
	)

	firstID, err := stage.Run(context.Background(), workspaceID, revision.ID, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := stage.Run(context.Background(), workspaceID, revision.ID, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == uuid.Nil || secondID != firstID {
		t.Fatalf("chunk set IDs = %s / %s", firstID, secondID)
	}
	if sets.set == nil || sets.set.Strategy != value.ChunkStrategyStandard || sets.set.Status != value.ChunkSetReady {
		t.Fatalf("chunk set = %#v", sets.set)
	}
	if sets.completeCalls != 1 || sets.set.ChunkCount != int64(len(sets.chunks)) || len(sets.chunks) == 0 {
		t.Fatalf("build calls=%d count=%d chunks=%d", sets.completeCalls, sets.set.ChunkCount, len(sets.chunks))
	}
	if len(sets.revisions) != len(sets.chunks) {
		t.Fatalf("revisions=%d chunks=%d", len(sets.revisions), len(sets.chunks))
	}
	for index, chunk := range sets.chunks {
		chunkRevision := sets.revisions[index]
		if chunk.WorkspaceID != workspaceID || chunk.DocumentRevisionID != revision.ID || chunk.ChunkSetID != firstID {
			t.Fatalf("chunk lineage = %#v", chunk)
		}
		if chunkRevision.ChunkID != chunk.ID || chunkRevision.RevisionNo != 1 || chunkRevision.EditSource != value.ChunkEditSourceSystem {
			t.Fatalf("system revision = %#v", chunkRevision)
		}
		if chunk.ActiveRevisionID == nil || *chunk.ActiveRevisionID != chunkRevision.ID {
			t.Fatalf("active revision pointer = %v, want %s", chunk.ActiveRevisionID, chunkRevision.ID)
		}
	}
}

func TestChunkStageBuildsLegacyV2FlatChunkSet(t *testing.T) {
	workspaceID := uuid.New()
	documentID := uuid.New()
	revision := testDocumentRevision(workspaceID, documentID, value.DocumentKindFile, value.DocumentRevisionReady)
	parsed := parsedTestDocument("正文")
	revision.NormalizedMarkdown = parsed.Markdown
	revision.ParseManifest = &parsed.Manifest
	generation := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		ChunkerVersion: CurrentStandardChunkerVersion - 1,
		ChunkingConfig: map[string]any{"chunk_size": float64(512), "chunk_overlap": float64(80)},
	}
	sets := &fakeChunkSetRepository{}
	stage := NewChunkStage(
		&fakeRevisionRepository{revision: revision},
		&fakeRevisionDocumentGetter{document: &model.Document{
			ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
			Kind: value.DocumentKindFile, Title: "指南",
		}},
		&fakeIndexGenerationGetter{generation: generation},
		sets,
		NewChunker(),
	)

	chunkSetID, err := stage.Run(context.Background(), workspaceID, revision.ID, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if chunkSetID == uuid.Nil || sets.getOrCreateCalls != 1 {
		t.Fatalf("chunk set id = %s, GetOrCreate calls = %d", chunkSetID, sets.getOrCreateCalls)
	}
	if sets.set.ChunkerVersion != 2 || sets.set.ChunkingConfig["chunk_size"] != 512 || sets.set.ChunkingConfig["chunk_overlap"] != 80 {
		t.Fatalf("legacy chunk set = %#v", sets.set)
	}
	for _, chunk := range sets.chunks {
		if chunk.Role != value.ChunkRoleFlat || chunk.ParentChunkID != nil {
			t.Fatalf("legacy chunk role = %#v", chunk)
		}
	}
}

func TestChunkStageRejectsFAQBeforeSetCreation(t *testing.T) {
	workspaceID := uuid.New()
	revision := testDocumentRevision(workspaceID, uuid.New(), value.DocumentKindFAQ, value.DocumentRevisionReady)
	sets := &fakeChunkSetRepository{}
	stage := NewChunkStage(
		&fakeRevisionRepository{revision: revision},
		&fakeRevisionDocumentGetter{},
		&fakeIndexGenerationGetter{},
		sets,
		NewChunker(),
	)

	_, err := stage.Run(context.Background(), workspaceID, revision.ID, uuid.New())
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("Run error = %v, want ErrValidation", err)
	}
	if sets.getOrCreateCalls != 0 {
		t.Fatalf("GetOrCreate calls = %d, want 0", sets.getOrCreateCalls)
	}
}

func TestDecodeChunkingConfigRejectsUnknownFields(t *testing.T) {
	_, err := decodeChunkingConfig(map[string]any{
		"chunk_size": 512, "chunk_overlap": 80, "future_option": true,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("decode error = %v, want ErrValidation", err)
	}
}

func TestDecodeChunkingConfigAcceptsExpectedFields(t *testing.T) {
	config, err := decodeChunkingConfig(map[string]any{
		"chunk_size": 256, "chunk_overlap": 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.ChunkSize != 256 || config.ChunkOverlap != 32 {
		t.Fatalf("config = %#v", config)
	}
}

type fakeIndexGenerationGetter struct {
	generation *model.IndexGeneration
	getCalls   int
}

func (g *fakeIndexGenerationGetter) Get(_ context.Context, workspaceID, generationID uuid.UUID) (*model.IndexGeneration, error) {
	g.getCalls++
	if g.generation == nil || g.generation.WorkspaceID != workspaceID || g.generation.ID != generationID {
		return nil, domainerrors.ErrNotFound
	}
	return g.generation, nil
}

type fakeChunkSetRepository struct {
	set              *model.DocumentChunkSet
	chunks           []*model.Chunk
	revisions        []*model.ChunkRevision
	getOrCreateCalls int
	completeCalls    int
	markFailedCalls  int
}

func (r *fakeChunkSetRepository) GetOrCreate(
	_ context.Context,
	workspaceID uuid.UUID,
	candidate *model.DocumentChunkSet,
) (*model.DocumentChunkSet, error) {
	r.getOrCreateCalls++
	if candidate == nil || candidate.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrValidation
	}
	if r.set == nil {
		copy := *candidate
		r.set = &copy
	}
	return r.set, nil
}

func (r *fakeChunkSetRepository) Complete(
	_ context.Context,
	workspaceID, chunkSetID uuid.UUID,
	chunks []*model.Chunk,
	revisions []*model.ChunkRevision,
) (*model.DocumentChunkSet, error) {
	if r.set == nil || r.set.WorkspaceID != workspaceID || r.set.ID != chunkSetID {
		return nil, domainerrors.ErrNotFound
	}
	r.completeCalls++
	r.chunks = chunks
	r.revisions = revisions
	r.set.Status = value.ChunkSetReady
	r.set.ChunkCount = int64(len(chunks))
	return r.set, nil
}

func (r *fakeChunkSetRepository) MarkFailed(
	_ context.Context,
	workspaceID, chunkSetID uuid.UUID,
	_ string, _ string,
) error {
	if r.set == nil || r.set.WorkspaceID != workspaceID || r.set.ID != chunkSetID {
		return domainerrors.ErrNotFound
	}
	r.markFailedCalls++
	r.set.Status = value.ChunkSetFailed
	return nil
}

// TestChunkStageRejectsExceedingMaxChunks 验证单文档 chunk 数量上限：
// 超限时返回 ErrValidation（terminal），不调用 Complete。
func TestChunkStageRejectsExceedingMaxChunks(t *testing.T) {
	workspaceID := uuid.New()
	documentID := uuid.New()
	revision := testDocumentRevision(workspaceID, documentID, value.DocumentKindFile, value.DocumentRevisionReady)
	parsed := parsedTestDocument("第一段内容。第二段内容。第三段内容。")
	revision.NormalizedMarkdown = parsed.Markdown
	revision.ParseManifest = &parsed.Manifest
	document := &model.Document{
		ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		Kind: value.DocumentKindFile, Title: "超限文档",
	}
	generation := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: revision.KnowledgeBaseID,
		ChunkerVersion: CurrentStandardChunkerVersion,
		ChunkingConfig: map[string]any{"chunk_size": float64(8), "chunk_overlap": float64(2)},
	}
	sets := &fakeChunkSetRepository{}
	stage := NewChunkStage(
		&fakeRevisionRepository{revision: revision},
		&fakeRevisionDocumentGetter{document: document},
		&fakeIndexGenerationGetter{generation: generation},
		sets,
		NewChunker(),
	).WithMaxChunksPerDocument(1) // 仅允许 1 个 chunk

	_, err := stage.Run(context.Background(), workspaceID, revision.ID, generation.ID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if sets.completeCalls != 0 {
		t.Fatalf("Complete 应未被调用，实际调用 %d 次", sets.completeCalls)
	}
	if sets.markFailedCalls != 1 {
		t.Fatalf("MarkFailed 应被调用 1 次（标记 ChunkSet 失败），实际 %d 次", sets.markFailedCalls)
	}
	if sets.set == nil || sets.set.Status != value.ChunkSetFailed {
		t.Fatalf("ChunkSet 状态应为 failed，实际 %#v", sets.set)
	}
}
