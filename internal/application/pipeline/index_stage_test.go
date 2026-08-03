package pipeline

import (
	"context"
	"testing"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestIndexStageEmbedsFAQQuestionsAndStagesAnswerAsReturnContent(t *testing.T) {
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	revisionID, chunkSetID, chunkID, chunkRevisionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	generation := &model.IndexGeneration{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		EmbeddingModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "text-embedding",
		EmbeddingDimension: 1024, RetrievalConfig: map[string]any{"fts_config": "simple"},
		Status: value.IndexGenerationReady,
	}
	chunkSet := &model.DocumentChunkSet{
		ID: chunkSetID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		Strategy: value.ChunkStrategyFAQ, Status: value.ChunkSetReady, ChunkCount: 1,
	}
	chunk := &model.Chunk{
		ID: chunkID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: chunkSetID,
		Sequence: 0, SourceContent: "Q: 如何退款？\nQ: 退款流程是什么？\nA: 请在订单页申请退款。",
		SourceAnchor: value.SourceAnchor{SourceType: "faq"}, Metadata: map[string]any{},
		ActiveRevisionID: &chunkRevisionID,
	}
	chunkRevision := &model.ChunkRevision{
		ID: chunkRevisionID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: revisionID, ChunkSetID: chunkSetID, ChunkID: chunkID,
		RevisionNo: 1, Content: "请在订单页申请退款。",
		EmbeddingContent: "如何退款？\n退款流程是什么？", Enabled: true,
		Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem,
	}
	embedder := &indexStageEmbeddingClient{dimension: 1024}
	resolver := &indexStageResolver{resolved: &appservice.ResolvedEmbeddingClient{
		Client: embedder, ModelID: generation.EmbeddingModelID, ProviderID: generation.ProviderID,
		ModelName: generation.ModelName, Dimensions: 1024, BatchSize: 32,
	}}
	projection := &indexStageProjectionSpy{}
	publisher := &indexStagePublishStore{
		document: &model.Document{
			ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
			Kind: value.DocumentKindFAQ, Status: value.DocumentStatusPending,
		},
		knowledgeBase: &model.KnowledgeBase{
			ID: knowledgeBaseID, WorkspaceID: workspaceID,
			ActiveIndexGenerationID: &generation.ID,
		},
	}
	stage := NewIndexStage(IndexStageDeps{
		Generations: &fakeIndexGenerationGetter{generation: generation},
		Sources: &indexStageSourceRepository{source: &IndexSource{
			ChunkSet: chunkSet, Chunks: []*model.Chunk{chunk}, Revisions: []*model.ChunkRevision{chunkRevision},
		}},
		Resolver:  resolver,
		Index:     projection,
		Publisher: publisher,
	})

	entries, err := stage.Run(context.Background(), workspaceID, generation.ID, chunkSetID)
	if err != nil {
		t.Fatal(err)
	}
	questions := "如何退款？\n退款流程是什么？"
	answer := "请在订单页申请退款。"
	if len(embedder.inputs) != 1 || len(embedder.inputs[0].Texts) != 1 || embedder.inputs[0].Texts[0] != questions {
		t.Fatalf("embedding inputs = %#v", embedder.inputs)
	}
	if len(entries) != 1 || entries[0].SearchContent != questions || entries[0].Content != answer {
		t.Fatalf("entries = %#v", entries)
	}
	if projection.ftsConfig != "simple" || len(projection.entries) != 1 ||
		projection.entries[0].Entry.SearchContent != questions || projection.entries[0].Entry.Content != answer {
		t.Fatalf("staged = config %q entries %#v", projection.ftsConfig, projection.entries)
	}
	if publisher.publishCalls != 1 || publisher.document.ActiveRevisionID == nil ||
		*publisher.document.ActiveRevisionID != revisionID || publisher.document.Status != value.DocumentStatusReady {
		t.Fatalf("publisher calls=%d document=%#v", publisher.publishCalls, publisher.document)
	}
}

type indexStageResolver struct {
	resolved *appservice.ResolvedEmbeddingClient
}

func (r *indexStageResolver) Resolve(context.Context, uuid.UUID, uuid.UUID) (*appservice.ResolvedEmbeddingClient, error) {
	return r.resolved, nil
}

type indexStageEmbeddingClient struct {
	dimension int
	inputs    []embeddingport.EmbedInput
}

func (c *indexStageEmbeddingClient) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	c.inputs = append(c.inputs, input)
	vectors := make([][]float32, len(input.Texts))
	for index := range input.Texts {
		vectors[index] = make([]float32, c.dimension)
		vectors[index][0] = float32(index + 1)
	}
	return &embeddingport.EmbedResult{Vectors: vectors}, nil
}

func (c *indexStageEmbeddingClient) Dimension() int { return c.dimension }

type indexStageSourceRepository struct {
	source *IndexSource
}

func (r *indexStageSourceRepository) GetReadyIndexSource(
	context.Context, uuid.UUID, uuid.UUID,
) (*IndexSource, error) {
	return r.source, nil
}

type indexStageProjectionSpy struct {
	workspaceID uuid.UUID
	ftsConfig   string
	dimension   int
	entries     []indexport.StageEntry
}

type indexStagePublishStore struct {
	document      *model.Document
	knowledgeBase *model.KnowledgeBase
	publishCalls  int
}

func (s *indexStagePublishStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.DocumentPublishTx) error,
) error {
	if s.document.WorkspaceID != workspaceID {
		return context.Canceled
	}
	return fn(ctx, s)
}

func (s *indexStagePublishStore) GetDocumentForUpdate(context.Context, uuid.UUID) (*model.Document, error) {
	return s.document, nil
}

func (s *indexStagePublishStore) GetKnowledgeBaseForUpdate(context.Context, uuid.UUID) (*model.KnowledgeBase, error) {
	return s.knowledgeBase, nil
}

func (s *indexStagePublishStore) PublishDocument(
	_ context.Context,
	_ *model.Document,
	_ *model.DocumentChunkSet,
	_ []*model.Chunk,
	_ []*model.ChunkRevision,
	_ []*model.RetrievalEntry,
) error {
	s.publishCalls++
	return nil
}

func (s *indexStageProjectionSpy) StageBatch(
	_ context.Context,
	workspaceID uuid.UUID,
	ftsConfig string,
	dimension int,
	entries []indexport.StageEntry,
) error {
	s.workspaceID, s.ftsConfig, s.dimension = workspaceID, ftsConfig, dimension
	s.entries = entries
	return nil
}
