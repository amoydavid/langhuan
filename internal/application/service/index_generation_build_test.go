package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestIndexGenerationBuildRechunksFileButReusesFAQ(t *testing.T) {
	store, request := newGenerationBuildStore()
	chunker := &generationChunkerSpy{result: uuid.New()}
	sources := &generationSourceStub{sources: map[uuid.UUID]*indexport.Source{}}
	fileSetID, faqSetID := chunker.result, store.source.Documents[1].ChunkSetID
	sources.sources[fileSetID] = generationBuildIndexSource(store.source.Generation, store.source.Documents[0], fileSetID, value.DocumentKindFile)
	sources.sources[faqSetID] = generationBuildIndexSource(store.source.Generation, store.source.Documents[1], faqSetID, value.DocumentKindFAQ)
	embedder := &generationBuildEmbedder{dimension: 1024}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: embedder, ModelID: store.source.Generation.EmbeddingModelID,
		ProviderID: store.source.Generation.ProviderID, ModelName: store.source.Generation.ModelName,
		Dimensions: 1024, BatchSize: 32,
	}}
	projection := &chunkRevisionProjectionSpy{}
	builder := NewIndexGenerationBuildService(IndexGenerationBuildDeps{
		Store: store, Chunker: chunker, Sources: sources, Resolver: resolver, Index: projection,
	})

	if err := builder.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(chunker.revisionIDs) != 1 || chunker.revisionIDs[0] != store.source.Documents[0].DocumentRevisionID {
		t.Fatalf("rechunk revisions = %#v", chunker.revisionIDs)
	}
	if len(projection.entries) != 2 || store.completedEntries != 2 || store.completedDocuments != 2 {
		t.Fatalf("staged=%d completed entries=%d docs=%d", len(projection.entries), store.completedEntries, store.completedDocuments)
	}
}

func TestIndexGenerationBuildRetryAfterCompletionIsNoop(t *testing.T) {
	store, request := newGenerationBuildStore()
	store.source.Job.Status = value.JobStatusCompleted
	store.source.Generation.Status = value.IndexGenerationReady
	chunker := &generationChunkerSpy{result: uuid.New()}
	builder := NewIndexGenerationBuildService(IndexGenerationBuildDeps{
		Store: store, Chunker: chunker, Sources: &generationSourceStub{},
		Resolver: &chunkRevisionResolverStub{}, Index: &chunkRevisionProjectionSpy{},
	})
	if err := builder.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(chunker.revisionIDs) != 0 || store.completedEntries != 0 {
		t.Fatalf("retry performed work: chunks=%d entries=%d", len(chunker.revisionIDs), store.completedEntries)
	}
}

func TestIndexGenerationBuildReadyGenerationCompletesJobWithoutRebuild(t *testing.T) {
	store, request := newGenerationBuildStore()
	store.source.Job.Status = value.JobStatusRunning
	store.source.Generation.Status = value.IndexGenerationReady
	chunker := &generationChunkerSpy{result: uuid.New()}
	builder := NewIndexGenerationBuildService(IndexGenerationBuildDeps{
		Store: store, Chunker: chunker, Sources: &generationSourceStub{},
		Resolver: &chunkRevisionResolverStub{}, Index: &chunkRevisionProjectionSpy{},
	})

	if err := builder.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if store.completeCalls != 1 || len(chunker.revisionIDs) != 0 || store.failureCalls != 0 {
		t.Fatalf(
			"complete calls=%d chunk calls=%d failure calls=%d",
			store.completeCalls, len(chunker.revisionIDs), store.failureCalls,
		)
	}
}

func TestIndexGenerationBuildTransientFailureKeepsGenerationRetryable(t *testing.T) {
	tests := []struct {
		name            string
		terminalAttempt bool
		wantTerminal    bool
	}{
		{name: "retry remains", terminalAttempt: false, wantTerminal: false},
		{name: "final attempt", terminalAttempt: true, wantTerminal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, request := newGenerationBuildStore()
			request.TerminalAttempt = test.terminalAttempt
			chunker := &generationChunkerSpy{err: errors.New("temporary chunker failure")}
			builder := NewIndexGenerationBuildService(IndexGenerationBuildDeps{
				Store: store, Chunker: chunker, Sources: &generationSourceStub{},
				Resolver: &chunkRevisionResolverStub{}, Index: &chunkRevisionProjectionSpy{},
			})

			if err := builder.Run(context.Background(), request); err == nil {
				t.Fatal("Run error = nil, want transient failure")
			}
			if store.failureCalls != 1 || store.failureTerminal != test.wantTerminal {
				t.Fatalf("failure calls=%d terminal=%v, want 1/%v", store.failureCalls, store.failureTerminal, test.wantTerminal)
			}
		})
	}
}

func TestIndexGenerationBuildValidationFailureIsTerminalBeforeRetryLimit(t *testing.T) {
	store, request := newGenerationBuildStore()
	store.source.Documents[0].ChunkSetID = uuid.Nil
	store.source.Generation.ChunkingConfig = cloneGenerationConfig(store.source.BaseGeneration.ChunkingConfig)
	builder := NewIndexGenerationBuildService(IndexGenerationBuildDeps{
		Store: store, Chunker: &generationChunkerSpy{}, Sources: &generationSourceStub{},
		Resolver: &chunkRevisionResolverStub{}, Index: &chunkRevisionProjectionSpy{},
	})

	if err := builder.Run(context.Background(), request); err == nil {
		t.Fatal("Run error = nil, want validation failure")
	}
	if store.failureCalls != 1 || !store.failureTerminal {
		t.Fatalf("failure calls=%d terminal=%v, want terminal validation failure", store.failureCalls, store.failureTerminal)
	}
}

type generationBuildStoreFake struct {
	source             *IndexGenerationBuildSource
	completedEntries   int64
	completedDocuments int64
	completeCalls      int
	failureCalls       int
	failureTerminal    bool
}

func newGenerationBuildStore() (*generationBuildStoreFake, IndexGenerationBuildRequest) {
	workspaceID, kbID, generationID, baseID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	fileRevisionID, faqRevisionID := uuid.New(), uuid.New()
	generation := &model.IndexGeneration{
		ID: generationID, WorkspaceID: workspaceID, KnowledgeBaseID: kbID, BaseGenerationID: &baseID,
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed", EmbeddingDimension: 1024,
		ChunkingConfig:  map[string]any{"chunk_size": 256, "chunk_overlap": 32},
		RetrievalConfig: map[string]any{"fts_config": "simple"}, Status: value.IndexGenerationBuilding,
	}
	base := &model.IndexGeneration{
		ID: baseID, WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		ChunkingConfig: map[string]any{"chunk_size": 512, "chunk_overlap": 80}, Status: value.IndexGenerationReady,
	}
	request := IndexGenerationBuildRequest{WorkspaceID: workspaceID, KnowledgeBaseID: kbID, GenerationID: generationID, JobID: jobID}
	return &generationBuildStoreFake{source: &IndexGenerationBuildSource{
		Job:           &model.Job{ID: jobID, WorkspaceID: workspaceID, KnowledgeBaseID: kbID, IndexGenerationID: generationID, Type: indexGenerationBuildJobType, Status: value.JobStatusPending},
		KnowledgeBase: &model.KnowledgeBase{ID: kbID, WorkspaceID: workspaceID, ActiveIndexGenerationID: &baseID},
		Generation:    generation, BaseGeneration: base,
		Documents: []IndexGenerationBuildDocument{
			{DocumentID: uuid.New(), DocumentRevisionID: fileRevisionID, Kind: value.DocumentKindFile, ChunkSetID: uuid.New()},
			{DocumentID: uuid.New(), DocumentRevisionID: faqRevisionID, Kind: value.DocumentKindFAQ, ChunkSetID: uuid.New()},
		},
	}}, request
}

func (s *generationBuildStoreFake) Load(context.Context, IndexGenerationBuildRequest) (*IndexGenerationBuildSource, error) {
	return s.source, nil
}

func (*generationBuildStoreFake) MarkRunning(context.Context, IndexGenerationBuildRequest) error {
	return nil
}

func (s *generationBuildStoreFake) Complete(
	_ context.Context,
	_ IndexGenerationBuildRequest,
	entries []*model.RetrievalEntry,
	documentCount, _ int64,
) error {
	s.completeCalls++
	s.completedEntries, s.completedDocuments = int64(len(entries)), documentCount
	return nil
}

func (s *generationBuildStoreFake) RecordFailure(
	_ context.Context,
	_ IndexGenerationBuildRequest,
	_, _ string,
	terminal bool,
) error {
	s.failureCalls++
	s.failureTerminal = terminal
	return nil
}

type generationChunkerSpy struct {
	result      uuid.UUID
	err         error
	revisionIDs []uuid.UUID
}

func (s *generationChunkerSpy) RunChunk(_ context.Context, _ uuid.UUID, revisionID, _ uuid.UUID) (uuid.UUID, error) {
	s.revisionIDs = append(s.revisionIDs, revisionID)
	return s.result, s.err
}

type generationSourceStub struct {
	sources map[uuid.UUID]*indexport.Source
}

func (s *generationSourceStub) GetReadyIndexSource(_ context.Context, _ uuid.UUID, setID uuid.UUID) (*indexport.Source, error) {
	return s.sources[setID], nil
}

func generationBuildIndexSource(
	generation *model.IndexGeneration,
	document IndexGenerationBuildDocument,
	setID uuid.UUID,
	kind value.DocumentKind,
) *indexport.Source {
	chunkID, revisionID := uuid.New(), uuid.New()
	chunk := &model.Chunk{
		ID: chunkID, WorkspaceID: generation.WorkspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
		DocumentID: document.DocumentID, DocumentRevisionID: document.DocumentRevisionID, ChunkSetID: setID,
		Sequence: 0, ActiveRevisionID: &revisionID, SourceAnchor: value.SourceAnchor{SourceType: string(kind)},
	}
	revision := &model.ChunkRevision{
		ID: revisionID, WorkspaceID: generation.WorkspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
		DocumentID: document.DocumentID, DocumentRevisionID: document.DocumentRevisionID,
		ChunkSetID: setID, ChunkID: chunkID, Content: "content", EmbeddingContent: "search", Enabled: true,
	}
	return &indexport.Source{
		ChunkSet: &model.DocumentChunkSet{
			ID: setID, WorkspaceID: generation.WorkspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
			DocumentID: document.DocumentID, DocumentRevisionID: document.DocumentRevisionID,
			Status: value.ChunkSetReady, ChunkCount: 1,
		},
		Chunks: []*model.Chunk{chunk}, Revisions: []*model.ChunkRevision{revision},
	}
}

type generationBuildEmbedder struct{ dimension int }

func (s *generationBuildEmbedder) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	vectors := make([][]float32, len(input.Texts))
	for index := range vectors {
		vectors[index] = make([]float32, s.dimension)
		vectors[index][0] = 1
	}
	return &embeddingport.EmbedResult{Vectors: vectors}, nil
}

func (s *generationBuildEmbedder) Dimension() int { return s.dimension }
