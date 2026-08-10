package pipeline

import (
	"context"
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// IndexSource is the ready fact input consumed by IndexStage.
type IndexSource = indexport.Source

// IndexStageDeps contains generation, fact, model and projection dependencies.
type IndexStageDeps struct {
	Generations IndexGenerationGetter
	Sources     indexport.SourceRepository
	Resolver    appservice.EmbeddingClientResolver
	Index       indexport.RetrievalIndex
	Publisher   appservice.DocumentPublishStore
}

// IndexStage builds one Generation's staging projection from active ChunkRevisions.
type IndexStage struct {
	generations          IndexGenerationGetter
	sources              indexport.SourceRepository
	resolver             appservice.EmbeddingClientResolver
	index                indexport.RetrievalIndex
	publisher            appservice.DocumentPublishStore
	embeddingConcurrency int // 并发 embedding batch 数上限；<=1 时串行
	embeddingBatchLimit  int // 单次 embedding batch 上限（与模型级 batch 取 min）；<=0 不限制
}

// NewIndexStage creates a revision-aware index stage.
func NewIndexStage(deps IndexStageDeps) IndexStage {
	return IndexStage{
		generations: deps.Generations, sources: deps.Sources,
		resolver: deps.Resolver, index: deps.Index,
		publisher: deps.Publisher,
	}
}

// WithEmbeddingLimits 设置 embedding 阶段的并发上限与单次 batch 上限。
// concurrency<=1 表示串行；batchLimit<=0 表示不覆盖模型级 batch size。
func (s IndexStage) WithEmbeddingLimits(concurrency, batchLimit int) IndexStage {
	s.embeddingConcurrency = concurrency
	s.embeddingBatchLimit = batchLimit
	return s
}

// Run embeds only search content, stages FTS/vector entries, then publishes atomically.
func (s IndexStage) Run(
	ctx context.Context,
	workspaceID, generationID, chunkSetID uuid.UUID,
) ([]*model.RetrievalEntry, error) {
	if workspaceID == uuid.Nil || generationID == uuid.Nil || chunkSetID == uuid.Nil {
		return nil, fmt.Errorf("%w: IndexStage lineage 不能为空", domainerrors.ErrValidation)
	}
	generation, err := s.generations.Get(ctx, workspaceID, generationID)
	if err != nil {
		return nil, err
	}
	if generation.Status != value.IndexGenerationReady && generation.Status != value.IndexGenerationBuilding {
		return nil, fmt.Errorf("%w: Generation status=%q 不可写入", domainerrors.ErrConflict, generation.Status)
	}
	source, err := s.sources.GetReadyIndexSource(ctx, workspaceID, chunkSetID)
	if err != nil {
		return nil, err
	}
	if err := validateIndexSource(workspaceID, generation, source); err != nil {
		return nil, err
	}
	resolved, err := s.resolver.Resolve(ctx, workspaceID, generation.EmbeddingModelID)
	if err != nil {
		return nil, err
	}
	if resolved.ProviderID != generation.ProviderID || resolved.ModelName != generation.ModelName ||
		resolved.Dimensions != generation.EmbeddingDimension {
		return nil, fmt.Errorf("%w: Generation/Model snapshot 不一致", domainerrors.ErrDimensionMismatch)
	}
	ftsConfig, err := generationFTSConfig(generation)
	if err != nil {
		return nil, err
	}

	entries := make([]*model.RetrievalEntry, 0, len(source.Chunks))
	texts := make([]string, 0, len(source.Chunks))
	for index, chunk := range source.Chunks {
		revision := source.Revisions[index]
		role := chunk.Role
		if role == "" {
			role = value.ChunkRoleFlat
		}
		if !role.IsRetrievable() {
			continue
		}
		if !revision.Enabled {
			continue
		}
		searchContent := strings.TrimSpace(revision.EmbeddingContent)
		content := strings.TrimSpace(revision.Content)
		if searchContent == "" || content == "" {
			return nil, fmt.Errorf("%w: enabled ChunkRevision content 不能为空", domainerrors.ErrValidation)
		}
		entries = append(entries, &model.RetrievalEntry{
			ID: id.New(), WorkspaceID: workspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
			IndexGenerationID: generation.ID, DocumentID: chunk.DocumentID,
			DocumentRevisionID: chunk.DocumentRevisionID, ChunkSetID: chunk.ChunkSetID,
			ChunkID: chunk.ID, ChunkRevisionID: revision.ID, State: value.RetrievalEntryStaging,
			SearchContent: searchContent, Content: content, SourceAnchor: chunk.SourceAnchor,
			Metadata: cloneIndexMetadata(chunk.Metadata), CreatedAt: time.Now().UTC(),
		})
		texts = append(texts, searchContent)
	}
	vectors, err := embedIndexTexts(ctx, resolved, texts, s.embeddingConcurrency, s.embeddingBatchLimit)
	if err != nil {
		return nil, err
	}
	staged := make([]indexport.StageEntry, len(entries))
	for index := range entries {
		staged[index] = indexport.StageEntry{Entry: entries[index], Embedding: vectors[index]}
	}
	if err := s.index.StageBatch(ctx, workspaceID, ftsConfig, resolved.Dimensions, staged); err != nil {
		return nil, err
	}
	if err := s.publish(ctx, workspaceID, generation, source, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s IndexStage) publish(
	ctx context.Context,
	workspaceID uuid.UUID,
	generation *model.IndexGeneration,
	source *indexport.Source,
	entries []*model.RetrievalEntry,
) error {
	if s.publisher == nil {
		return fmt.Errorf("%w: Document publisher 不能为空", domainerrors.ErrValidation)
	}
	return s.publisher.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, tx appservice.DocumentPublishTx) error {
		document, err := tx.GetDocumentForUpdate(txCtx, source.ChunkSet.DocumentID)
		if err != nil {
			return err
		}
		knowledgeBase, err := tx.GetKnowledgeBaseForUpdate(txCtx, generation.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if document.WorkspaceID != workspaceID || document.KnowledgeBaseID != generation.KnowledgeBaseID ||
			knowledgeBase.WorkspaceID != workspaceID || knowledgeBase.ActiveIndexGenerationID == nil ||
			*knowledgeBase.ActiveIndexGenerationID != generation.ID {
			return domainerrors.ErrGenerationStale
		}
		revisionID := source.ChunkSet.DocumentRevisionID
		document.ActiveRevisionID = &revisionID
		document.Status = value.DocumentStatusReady
		document.UpdatedAt = time.Now().UTC()
		return tx.PublishDocument(
			txCtx, document, source.ChunkSet, source.Chunks, source.Revisions, entries,
		)
	})
}

func validateIndexSource(workspaceID uuid.UUID, generation *model.IndexGeneration, source *indexport.Source) error {
	if generation == nil || source == nil || source.ChunkSet == nil ||
		source.ChunkSet.WorkspaceID != workspaceID || source.ChunkSet.KnowledgeBaseID != generation.KnowledgeBaseID ||
		source.ChunkSet.Status != value.ChunkSetReady || int64(len(source.Chunks)) != source.ChunkSet.ChunkCount ||
		len(source.Chunks) != len(source.Revisions) {
		return fmt.Errorf("%w: IndexSource/Generation lineage 无效", domainerrors.ErrValidation)
	}
	for index, chunk := range source.Chunks {
		revision := source.Revisions[index]
		if chunk == nil || revision == nil ||
			chunk.WorkspaceID != workspaceID || chunk.KnowledgeBaseID != generation.KnowledgeBaseID ||
			chunk.ChunkSetID != source.ChunkSet.ID || chunk.DocumentID != source.ChunkSet.DocumentID ||
			chunk.DocumentRevisionID != source.ChunkSet.DocumentRevisionID ||
			revision.WorkspaceID != workspaceID || revision.ChunkID != chunk.ID ||
			revision.ChunkSetID != chunk.ChunkSetID || chunk.ActiveRevisionID == nil ||
			*chunk.ActiveRevisionID != revision.ID {
			return fmt.Errorf("%w: IndexSource chunk %d lineage 无效", domainerrors.ErrValidation, index)
		}
		role := chunk.Role
		if role == "" {
			role = value.ChunkRoleFlat
		}
		if err := role.Validate(); err != nil {
			return fmt.Errorf("%w: IndexSource chunk %d role 无效", domainerrors.ErrValidation, index)
		}
		if role == value.ChunkRoleChild && chunk.ParentChunkID == nil {
			return fmt.Errorf("%w: IndexSource child chunk %d 缺少 parent", domainerrors.ErrValidation, index)
		}
	}
	return nil
}

func generationFTSConfig(generation *model.IndexGeneration) (string, error) {
	raw, ok := generation.RetrievalConfig["fts_config"]
	if !ok {
		return "", fmt.Errorf("%w: Generation fts_config 缺失", domainerrors.ErrValidation)
	}
	config, ok := raw.(string)
	config = strings.TrimSpace(config)
	if !ok || config == "" {
		return "", fmt.Errorf("%w: Generation fts_config 无效", domainerrors.ErrValidation)
	}
	return config, nil
}

// embedIndexTexts 把文本分批 embedding，返回与 texts 顺序对齐的向量。
// concurrency<=1 时串行；>1 时用 errgroup + semaphore 并发执行。
// batchLimit>0 时与模型级 resolved.BatchSize 取 min，作为实际单批大小上限。
func embedIndexTexts(
	ctx context.Context,
	resolved *appservice.ResolvedEmbeddingClient,
	texts []string,
	concurrency int,
	batchLimit int,
) ([][]float32, error) {
	if len(texts) == 0 {
		return make([][]float32, 0), nil
	}
	batchSize := resolved.BatchSize
	if batchLimit > 0 && (batchSize <= 0 || batchLimit < batchSize) {
		batchSize = batchLimit
	}
	if batchSize <= 0 {
		batchSize = len(texts)
	}

	// 切分批次。
	type batch struct {
		start int
		texts []string
	}
	batches := make([]batch, 0, (len(texts)+batchSize-1)/batchSize)
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, batch{start: start, texts: texts[start:end]})
	}

	// results 按 batch 索引存放，保证最终顺序与输入一致。
	results := make([][][]float32, len(batches))
	embedOne := func(ctx context.Context, b batch) error {
		result, err := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: b.texts})
		if err != nil {
			return err
		}
		if result == nil || len(result.Vectors) != len(b.texts) {
			return domainerrors.ErrInvalidEmbeddingResponse
		}
		for _, vector := range result.Vectors {
			if len(vector) != resolved.Dimensions || !finiteVector(vector) {
				return domainerrors.ErrDimensionMismatch
			}
		}
		results[b.start/batchSize] = result.Vectors
		return nil
	}

	if concurrency <= 1 || len(batches) <= 1 {
		for _, b := range batches {
			if err := embedOne(ctx, b); err != nil {
				return nil, err
			}
		}
	} else {
		// errgroup + semaphore 限制并发。
		group, groupCtx := errgroup.WithContext(ctx)
		sem := make(chan struct{}, concurrency)
		for _, b := range batches {
			select {
			case sem <- struct{}{}:
			case <-groupCtx.Done():
				group.Wait()
				return nil, groupCtx.Err()
			}
			bb := b
			group.Go(func() error {
				defer func() { <-sem }()
				return embedOne(groupCtx, bb)
			})
		}
		if err := group.Wait(); err != nil {
			return nil, err
		}
	}

	vectors := make([][]float32, 0, len(texts))
	for _, vec := range results {
		vectors = append(vectors, vec...)
	}
	return vectors, nil
}

func finiteVector(vector []float32) bool {
	for _, component := range vector {
		if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
			return false
		}
	}
	return true
}

func cloneIndexMetadata(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
