package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestChunkRevisionIndexPublishesEnabledRevision(t *testing.T) {
	store, request := newFakeChunkRevisionIndexStore(true)
	embedder := &chunkRevisionEmbeddingSpy{dimension: 1024}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: embedder, ModelID: store.source.Generation.EmbeddingModelID,
		ProviderID: store.source.Generation.ProviderID, ModelName: store.source.Generation.ModelName,
		Dimensions: 1024, BatchSize: 16,
	}}
	projection := &chunkRevisionProjectionSpy{}
	indexer := NewChunkRevisionIndexService(store, resolver, projection)

	if err := indexer.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(embedder.inputs) != 1 || len(embedder.inputs[0].Texts) != 1 ||
		embedder.inputs[0].Texts[0] != store.source.NewRevision.EmbeddingContent {
		t.Fatalf("embedding inputs = %#v", embedder.inputs)
	}
	if len(projection.entries) != 1 || projection.entries[0].Entry.ChunkRevisionID != request.NewRevisionID {
		t.Fatalf("staging entries = %#v", projection.entries)
	}
	if store.publishedEntry == nil || store.publishedEntry.Content != store.source.NewRevision.Content ||
		store.succeededJobID != request.JobID {
		t.Fatalf("published=%#v succeeded=%s", store.publishedEntry, store.succeededJobID)
	}
}

func TestChunkRevisionIndexDisablesWithoutEmbedding(t *testing.T) {
	store, request := newFakeChunkRevisionIndexStore(false)
	embedder := &chunkRevisionEmbeddingSpy{dimension: 1024}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{
		Client: embedder, ModelID: store.source.Generation.EmbeddingModelID,
		ProviderID: store.source.Generation.ProviderID, ModelName: store.source.Generation.ModelName,
		Dimensions: 1024, BatchSize: 16,
	}}
	projection := &chunkRevisionProjectionSpy{}
	indexer := NewChunkRevisionIndexService(store, resolver, projection)

	if err := indexer.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(embedder.inputs) != 0 || len(projection.entries) != 0 {
		t.Fatalf("disabled revision embedded=%d staged=%d", len(embedder.inputs), len(projection.entries))
	}
	if store.publishCalls != 1 || store.publishedEntry != nil || store.succeededJobID != request.JobID {
		t.Fatalf("publish calls=%d entry=%#v succeeded=%s", store.publishCalls, store.publishedEntry, store.succeededJobID)
	}
}

func TestChunkRevisionIndexRetryAfterPublishOnlyCompletesJob(t *testing.T) {
	store, request := newFakeChunkRevisionIndexStore(true)
	store.source.Job.Status = value.JobStatusRunning
	store.source.NewRevision.Status = value.ChunkRevisionReady
	store.source.Chunk.ActiveRevisionID = &request.NewRevisionID
	store.source.KnowledgeBase.ContentVersion = request.ExpectedContentVersion + 1
	store.source.Generation.IndexedContentVersion = request.ExpectedContentVersion + 1
	embedder := &chunkRevisionEmbeddingSpy{dimension: 1024}
	resolver := &chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{Client: embedder}}
	projection := &chunkRevisionProjectionSpy{}
	indexer := NewChunkRevisionIndexService(store, resolver, projection)

	if err := indexer.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if store.succeededJobID != request.JobID || store.publishCalls != 0 ||
		len(embedder.inputs) != 0 || len(projection.entries) != 0 {
		t.Fatalf("succeeded=%s publish=%d embed=%d stage=%d",
			store.succeededJobID, store.publishCalls, len(embedder.inputs), len(projection.entries))
	}
}

func TestChunkRevisionIndexRetryRejectsMismatchedJobBeforeCompleting(t *testing.T) {
	store, request := newFakeChunkRevisionIndexStore(true)
	store.source.Job.DocumentID = uuid.New()
	store.source.NewRevision.Status = value.ChunkRevisionReady
	store.source.Chunk.ActiveRevisionID = &request.NewRevisionID
	indexer := NewChunkRevisionIndexService(
		store,
		&chunkRevisionResolverStub{resolved: &ResolvedEmbeddingClient{}},
		&chunkRevisionProjectionSpy{},
	)

	if err := indexer.Run(context.Background(), request); err == nil {
		t.Fatal("mismatched Job must be rejected")
	}
	if store.succeededJobID != uuid.Nil {
		t.Fatalf("mismatched job was completed: %s", store.succeededJobID)
	}
}

type fakeChunkRevisionIndexStore struct {
	source         *ChunkRevisionIndexSource
	publishCalls   int
	publishedEntry *model.RetrievalEntry
	succeededJobID uuid.UUID
}

func newFakeChunkRevisionIndexStore(enabled bool) (*fakeChunkRevisionIndexStore, ChunkRevisionIndexRequest) {
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	documentRevisionID, chunkSetID, chunkID := uuid.New(), uuid.New(), uuid.New()
	baseID, newID, generationID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	base := &model.ChunkRevision{
		ID: baseID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID,
		ChunkSetID: chunkSetID, ChunkID: chunkID, RevisionNo: 1,
		Content: "base", EmbeddingContent: "base", Enabled: true, Status: value.ChunkRevisionReady,
	}
	newRevision := &model.ChunkRevision{
		ID: newID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID,
		ChunkSetID: chunkSetID, ChunkID: chunkID, RevisionNo: 2, BaseRevisionID: &baseID,
		Content: "new content", ContextHeader: "header", EmbeddingContent: "header\n\nnew content",
		Enabled: enabled, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceUser,
	}
	generation := &model.IndexGeneration{
		ID: generationID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "embed-v2",
		EmbeddingDimension: 1024, RetrievalConfig: map[string]any{"fts_config": "simple"},
		IndexedContentVersion: 7, Status: value.IndexGenerationReady,
	}
	job := &model.Job{
		ID: jobID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID,
		Type: chunkRevisionIndexJobType, Status: value.JobStatusPending,
	}
	chunk := &model.Chunk{
		ID: chunkID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID, ChunkSetID: chunkSetID,
		ActiveRevisionID: &baseID, SourceAnchor: value.SourceAnchor{SourceType: "file"},
		Metadata: map[string]any{"heading": "h1"},
	}
	request := ChunkRevisionIndexRequest{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, GenerationID: generationID,
		DocumentID: documentID, DocumentRevisionID: documentRevisionID, ChunkSetID: chunkSetID,
		ChunkID: chunkID, BaseRevisionID: baseID, NewRevisionID: newID,
		ExpectedContentVersion: 7, JobID: jobID,
	}
	return &fakeChunkRevisionIndexStore{source: &ChunkRevisionIndexSource{
		Job: job,
		KnowledgeBase: &model.KnowledgeBase{
			ID: knowledgeBaseID, WorkspaceID: workspaceID,
			ActiveIndexGenerationID: &generationID, ContentVersion: 7,
		},
		Generation: generation, Chunk: chunk, BaseRevision: base, NewRevision: newRevision,
	}}, request
}

func (s *fakeChunkRevisionIndexStore) Load(context.Context, ChunkRevisionIndexRequest) (*ChunkRevisionIndexSource, error) {
	return s.source, nil
}

func (*fakeChunkRevisionIndexStore) MarkIndexing(context.Context, ChunkRevisionIndexRequest) error {
	return nil
}

func (s *fakeChunkRevisionIndexStore) Publish(
	_ context.Context,
	_ PublishChunkRevisionInput,
	entry *model.RetrievalEntry,
) error {
	s.publishCalls++
	s.publishedEntry = entry
	return nil
}

func (s *fakeChunkRevisionIndexStore) MarkSucceeded(_ context.Context, _ uuid.UUID, jobID uuid.UUID) error {
	s.succeededJobID = jobID
	return nil
}

func (*fakeChunkRevisionIndexStore) MarkFailed(context.Context, ChunkRevisionIndexRequest, string, string) error {
	return nil
}

type chunkRevisionResolverStub struct{ resolved *ResolvedEmbeddingClient }

func (s *chunkRevisionResolverStub) Resolve(context.Context, uuid.UUID, uuid.UUID) (*ResolvedEmbeddingClient, error) {
	return s.resolved, nil
}

type chunkRevisionEmbeddingSpy struct {
	dimension int
	inputs    []embeddingport.EmbedInput
}

func (s *chunkRevisionEmbeddingSpy) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	s.inputs = append(s.inputs, input)
	vector := make([]float32, s.dimension)
	vector[0] = 1
	return &embeddingport.EmbedResult{Vectors: [][]float32{vector}}, nil
}

func (s *chunkRevisionEmbeddingSpy) Dimension() int { return s.dimension }

type chunkRevisionProjectionSpy struct{ entries []indexport.StageEntry }

func (s *chunkRevisionProjectionSpy) StageBatch(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	_ int,
	entries []indexport.StageEntry,
) error {
	s.entries = entries
	return nil
}
