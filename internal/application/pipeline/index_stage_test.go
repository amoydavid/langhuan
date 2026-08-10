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

// positionalEmbedClient 用 text 内容的 ASCII 码作为向量标记，保证并发执行下每个 text
// 的向量是确定性的（不依赖执行顺序），从而可断言最终结果顺序与输入对齐。
type positionalEmbedClient struct {
	dimension int
	callCount int
}

func (c *positionalEmbedClient) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	c.callCount++
	vectors := make([][]float32, len(input.Texts))
	for i, text := range input.Texts {
		vectors[i] = make([]float32, c.dimension)
		// 第一个分量编码 text 的首字符 ASCII（确定性，与执行顺序无关）。
		if len(text) > 0 {
			vectors[i][0] = float32(text[0])
		}
	}
	return &embeddingport.EmbedResult{Vectors: vectors}, nil
}

func (c *positionalEmbedClient) Dimension() int { return c.dimension }

// TestEmbedIndexTextsConcurrencyPreservesOrder 验证并发 embedding 时结果顺序与输入一致。
// 用 batch size=2 切成 3 批，concurrency=2 并发执行，断言最终向量顺序正确。
func TestEmbedIndexTextsConcurrencyPreservesOrder(t *testing.T) {
	embedder := &positionalEmbedClient{dimension: 4}
	resolved := &appservice.ResolvedEmbeddingClient{
		Client: embedder, Dimensions: 4, BatchSize: 2,
	}
	texts := []string{"a", "b", "c", "d", "e"}

	// concurrency=2，batchLimit 不覆盖（用模型级 BatchSize=2）。
	vectors, err := embedIndexTexts(context.Background(), resolved, texts, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != len(texts) {
		t.Fatalf("vectors len = %d, want %d", len(vectors), len(texts))
	}
	// 每个向量的第 0 分量应等于对应 text 的首字符 ASCII（断言顺序对齐）。
	for i, vec := range vectors {
		if vec[0] != float32(texts[i][0]) {
			t.Fatalf("vector[%d][0] = %v, want %d (顺序错乱)", i, vec[0], texts[i][0])
		}
	}
	// 3 批（2+2+1），应触发 3 次 Embed 调用。
	if embedder.callCount != 3 {
		t.Fatalf("callCount = %d, want 3 batches", embedder.callCount)
	}
}

// TestEmbedIndexTextsBatchLimitOverridesModel 验证 batchLimit 覆盖模型级 batch size。
func TestEmbedIndexTextsBatchLimitOverridesModel(t *testing.T) {
	embedder := &positionalEmbedClient{dimension: 4}
	resolved := &appservice.ResolvedEmbeddingClient{
		Client: embedder, Dimensions: 4, BatchSize: 100, // 模型级很大
	}
	texts := []string{"a", "b", "c", "d", "e"}

	// batchLimit=2 应覆盖模型级 100，切成 3 批。
	_, err := embedIndexTexts(context.Background(), resolved, texts, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if embedder.callCount != 3 {
		t.Fatalf("callCount = %d, want 3 (batchLimit=2 切 5 文本为 3 批)", embedder.callCount)
	}
}

// TestEmbedIndexTextsSerialPath 验证 concurrency<=1 走串行路径。
func TestEmbedIndexTextsSerialPath(t *testing.T) {
	embedder := &positionalEmbedClient{dimension: 4}
	resolved := &appservice.ResolvedEmbeddingClient{
		Client: embedder, Dimensions: 4, BatchSize: 2,
	}
	texts := []string{"a", "b", "c"}
	vectors, err := embedIndexTexts(context.Background(), resolved, texts, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 3 {
		t.Fatalf("vectors len = %d, want 3", len(vectors))
	}
	for i, vec := range vectors {
		if vec[0] != float32(texts[i][0]) {
			t.Fatalf("vector[%d][0] = %v, want %d", i, vec[0], texts[i][0])
		}
	}
}
