package pipeline

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

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
	generations IndexGenerationGetter
	sources     indexport.SourceRepository
	resolver    appservice.EmbeddingClientResolver
	index       indexport.RetrievalIndex
	publisher   appservice.DocumentPublishStore
}

// NewIndexStage creates a revision-aware index stage.
func NewIndexStage(deps IndexStageDeps) IndexStage {
	return IndexStage{
		generations: deps.Generations, sources: deps.Sources,
		resolver: deps.Resolver, index: deps.Index,
		publisher: deps.Publisher,
	}
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
		if !revision.Enabled {
			continue
		}
		searchContent := strings.TrimSpace(revision.EmbeddingContent)
		content := strings.TrimSpace(revision.Content)
		if searchContent == "" || content == "" {
			return nil, fmt.Errorf("%w: enabled ChunkRevision content 不能为空", domainerrors.ErrValidation)
		}
		entries = append(entries, &model.RetrievalEntry{
			ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
			IndexGenerationID: generation.ID, DocumentID: chunk.DocumentID,
			DocumentRevisionID: chunk.DocumentRevisionID, ChunkSetID: chunk.ChunkSetID,
			ChunkID: chunk.ID, ChunkRevisionID: revision.ID, State: value.RetrievalEntryStaging,
			SearchContent: searchContent, Content: content, SourceAnchor: chunk.SourceAnchor,
			Metadata: cloneIndexMetadata(chunk.Metadata), CreatedAt: time.Now().UTC(),
		})
		texts = append(texts, searchContent)
	}
	vectors, err := embedIndexTexts(ctx, resolved, texts)
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
		if chunk == nil || revision == nil || chunk.Sequence != index ||
			chunk.WorkspaceID != workspaceID || chunk.KnowledgeBaseID != generation.KnowledgeBaseID ||
			chunk.ChunkSetID != source.ChunkSet.ID || chunk.DocumentID != source.ChunkSet.DocumentID ||
			chunk.DocumentRevisionID != source.ChunkSet.DocumentRevisionID ||
			revision.WorkspaceID != workspaceID || revision.ChunkID != chunk.ID ||
			revision.ChunkSetID != chunk.ChunkSetID || chunk.ActiveRevisionID == nil ||
			*chunk.ActiveRevisionID != revision.ID {
			return fmt.Errorf("%w: IndexSource chunk %d lineage 无效", domainerrors.ErrValidation, index)
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

func embedIndexTexts(
	ctx context.Context,
	resolved *appservice.ResolvedEmbeddingClient,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return make([][]float32, 0), nil
	}
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += resolved.BatchSize {
		end := min(start+resolved.BatchSize, len(texts))
		result, err := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: texts[start:end]})
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Vectors) != end-start {
			return nil, domainerrors.ErrInvalidEmbeddingResponse
		}
		for _, vector := range result.Vectors {
			if len(vector) != resolved.Dimensions || !finiteVector(vector) {
				return nil, domainerrors.ErrDimensionMismatch
			}
			vectors = append(vectors, vector)
		}
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
