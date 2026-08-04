package service

import (
	"context"
	"errors"
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// ChunkRevisionIndexRequest identifies one targeted immutable revision indexing task.
type ChunkRevisionIndexRequest struct {
	WorkspaceID, KnowledgeBaseID, GenerationID    uuid.UUID
	DocumentID, DocumentRevisionID, ChunkSetID    uuid.UUID
	ChunkID, BaseRevisionID, NewRevisionID, JobID uuid.UUID
	ExpectedContentVersion                        int64
}

// PublishChunkRevisionInput is the compare-and-swap contract for one Chunk pointer switch.
type PublishChunkRevisionInput struct {
	WorkspaceID, KnowledgeBaseID, GenerationID uuid.UUID
	ChunkID, BaseRevisionID, NewRevisionID     uuid.UUID
	ExpectedContentVersion                     int64
}

// ChunkRevisionIndexSource is the trusted persisted state used for targeted indexing.
type ChunkRevisionIndexSource struct {
	Job           *model.Job
	KnowledgeBase *model.KnowledgeBase
	Generation    *model.IndexGeneration
	Chunk         *model.Chunk
	BaseRevision  *model.ChunkRevision
	NewRevision   *model.ChunkRevision
}

// ChunkRevisionIndexStore owns Workspace-scoped task state and atomic publication.
type ChunkRevisionIndexStore interface {
	Load(context.Context, ChunkRevisionIndexRequest) (*ChunkRevisionIndexSource, error)
	MarkIndexing(context.Context, ChunkRevisionIndexRequest) error
	Publish(context.Context, PublishChunkRevisionInput, *model.RetrievalEntry) error
	MarkSucceeded(context.Context, uuid.UUID, uuid.UUID) error
	MarkFailed(context.Context, ChunkRevisionIndexRequest, string, string) error
}

// ChunkRevisionIndexService embeds and publishes exactly one user ChunkRevision.
type ChunkRevisionIndexService struct {
	store    ChunkRevisionIndexStore
	resolver EmbeddingClientResolver
	index    indexport.RetrievalIndex
}

// NewChunkRevisionIndexService creates the targeted indexing use case.
func NewChunkRevisionIndexService(
	store ChunkRevisionIndexStore,
	resolver EmbeddingClientResolver,
	index indexport.RetrievalIndex,
) *ChunkRevisionIndexService {
	return &ChunkRevisionIndexService{store: store, resolver: resolver, index: index}
}

// Run stages an enabled revision, then atomically switches the active pointer and projection.
func (s *ChunkRevisionIndexService) Run(ctx context.Context, request ChunkRevisionIndexRequest) error {
	if err := validateChunkRevisionIndexRequest(request); err != nil {
		return err
	}
	if s.store == nil || s.resolver == nil || s.index == nil {
		return fmt.Errorf("%w: ChunkRevision index dependencies 不能为空", domainerrors.ErrValidation)
	}
	source, err := s.store.Load(ctx, request)
	if err != nil {
		return s.fail(ctx, request, err)
	}
	if source == nil || source.Job == nil || source.KnowledgeBase == nil || source.Generation == nil ||
		source.Chunk == nil || source.BaseRevision == nil || source.NewRevision == nil {
		return s.fail(ctx, request, fmt.Errorf("%w: ChunkRevision index source 不能为空", domainerrors.ErrValidation))
	}
	if err := validateChunkRevisionIndexLineage(request, source); err != nil {
		return s.fail(ctx, request, err)
	}
	if source.Job.Status == value.JobStatusCompleted {
		return nil
	}
	if source.NewRevision.Status == value.ChunkRevisionReady && source.Chunk.ActiveRevisionID != nil &&
		*source.Chunk.ActiveRevisionID == source.NewRevision.ID {
		return s.store.MarkSucceeded(ctx, request.WorkspaceID, request.JobID)
	}
	if err := validateChunkRevisionIndexCAS(request, source); err != nil {
		return s.fail(ctx, request, err)
	}
	if err := s.store.MarkIndexing(ctx, request); err != nil {
		return err
	}

	var entry *model.RetrievalEntry
	if source.NewRevision.Enabled {
		entry, err = s.stage(ctx, request, source)
		if err != nil {
			return s.fail(ctx, request, err)
		}
	}
	publishInput := PublishChunkRevisionInput{
		WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
		GenerationID: request.GenerationID, ChunkID: request.ChunkID,
		BaseRevisionID: request.BaseRevisionID, NewRevisionID: request.NewRevisionID,
		ExpectedContentVersion: request.ExpectedContentVersion,
	}
	if err := s.store.Publish(ctx, publishInput, entry); err != nil {
		return s.fail(ctx, request, err)
	}
	if err := s.store.MarkSucceeded(ctx, request.WorkspaceID, request.JobID); err != nil {
		return err
	}
	return nil
}

func (s *ChunkRevisionIndexService) stage(
	ctx context.Context,
	request ChunkRevisionIndexRequest,
	source *ChunkRevisionIndexSource,
) (*model.RetrievalEntry, error) {
	resolved, err := s.resolver.Resolve(ctx, request.WorkspaceID, source.Generation.EmbeddingModelID)
	if err != nil {
		return nil, err
	}
	if resolved == nil || resolved.Client == nil || resolved.ProviderID != source.Generation.ProviderID ||
		resolved.ModelName != source.Generation.ModelName || resolved.Dimensions != source.Generation.EmbeddingDimension {
		return nil, domainerrors.ErrDimensionMismatch
	}
	searchContent := strings.TrimSpace(source.NewRevision.EmbeddingContent)
	content := strings.TrimSpace(source.NewRevision.Content)
	if searchContent == "" || content == "" {
		return nil, fmt.Errorf("%w: enabled ChunkRevision content 不能为空", domainerrors.ErrValidation)
	}
	result, err := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: []string{searchContent}})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Vectors) != 1 || len(result.Vectors[0]) != resolved.Dimensions ||
		!finiteChunkRevisionVector(result.Vectors[0]) {
		return nil, domainerrors.ErrInvalidEmbeddingResponse
	}
	ftsConfig, ok := source.Generation.RetrievalConfig["fts_config"].(string)
	ftsConfig = strings.TrimSpace(ftsConfig)
	if !ok || ftsConfig == "" {
		return nil, fmt.Errorf("%w: Generation fts_config 无效", domainerrors.ErrValidation)
	}
	entry := &model.RetrievalEntry{
		ID: id.New(), WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
		IndexGenerationID: request.GenerationID, DocumentID: request.DocumentID,
		DocumentRevisionID: request.DocumentRevisionID, ChunkSetID: request.ChunkSetID,
		ChunkID: request.ChunkID, ChunkRevisionID: request.NewRevisionID,
		State: value.RetrievalEntryStaging, SearchContent: searchContent, Content: content,
		SourceAnchor: source.Chunk.SourceAnchor, Metadata: cloneChunkRevisionMetadata(source.Chunk.Metadata),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.index.StageBatch(ctx, request.WorkspaceID, ftsConfig, resolved.Dimensions, []indexport.StageEntry{{
		Entry: entry, Embedding: result.Vectors[0],
	}}); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *ChunkRevisionIndexService) fail(ctx context.Context, request ChunkRevisionIndexRequest, cause error) error {
	if err := s.store.MarkFailed(ctx, request, chunkRevisionIndexErrorClass(cause), cause.Error()); err != nil {
		return errors.Join(cause, fmt.Errorf("持久化 ChunkRevision 索引失败状态失败: %w", err))
	}
	return cause
}

func validateChunkRevisionIndexRequest(request ChunkRevisionIndexRequest) error {
	ids := []uuid.UUID{
		request.WorkspaceID, request.KnowledgeBaseID, request.GenerationID, request.DocumentID,
		request.DocumentRevisionID, request.ChunkSetID, request.ChunkID, request.BaseRevisionID,
		request.NewRevisionID, request.JobID,
	}
	for _, id := range ids {
		if id == uuid.Nil {
			return fmt.Errorf("%w: ChunkRevision index lineage 不能为空", domainerrors.ErrValidation)
		}
	}
	if request.ExpectedContentVersion < 0 {
		return fmt.Errorf("%w: expected_content_version 不能为负数", domainerrors.ErrValidation)
	}
	return nil
}

func validateChunkRevisionIndexLineage(request ChunkRevisionIndexRequest, source *ChunkRevisionIndexSource) error {
	if source == nil || source.Job == nil || source.KnowledgeBase == nil || source.Generation == nil || source.Chunk == nil ||
		source.BaseRevision == nil || source.NewRevision == nil {
		return fmt.Errorf("%w: ChunkRevision index source 不能为空", domainerrors.ErrValidation)
	}
	if source.Job.ID != request.JobID || source.Job.WorkspaceID != request.WorkspaceID ||
		source.Job.KnowledgeBaseID != request.KnowledgeBaseID || source.Job.DocumentID != request.DocumentID ||
		source.Job.DocumentRevisionID != request.DocumentRevisionID || source.Job.Type != chunkRevisionIndexJobType ||
		source.KnowledgeBase.ID != request.KnowledgeBaseID ||
		source.KnowledgeBase.WorkspaceID != request.WorkspaceID ||
		source.Generation.ID != request.GenerationID || source.Generation.WorkspaceID != request.WorkspaceID ||
		source.Generation.KnowledgeBaseID != request.KnowledgeBaseID ||
		source.Chunk.ID != request.ChunkID || source.Chunk.WorkspaceID != request.WorkspaceID ||
		source.Chunk.KnowledgeBaseID != request.KnowledgeBaseID || source.Chunk.DocumentID != request.DocumentID ||
		source.Chunk.DocumentRevisionID != request.DocumentRevisionID || source.Chunk.ChunkSetID != request.ChunkSetID ||
		source.BaseRevision.ID != request.BaseRevisionID || source.NewRevision.ID != request.NewRevisionID ||
		source.NewRevision.BaseRevisionID == nil || *source.NewRevision.BaseRevisionID != request.BaseRevisionID ||
		source.NewRevision.ChunkID != request.ChunkID || source.NewRevision.DocumentID != request.DocumentID {
		return fmt.Errorf("%w: ChunkRevision index lineage 不一致", domainerrors.ErrValidation)
	}
	return nil
}

func validateChunkRevisionIndexCAS(request ChunkRevisionIndexRequest, source *ChunkRevisionIndexSource) error {
	if source.Chunk.ActiveRevisionID == nil || *source.Chunk.ActiveRevisionID != request.BaseRevisionID {
		return domainerrors.ErrRevisionConflict
	}
	if source.KnowledgeBase.ActiveIndexGenerationID == nil ||
		*source.KnowledgeBase.ActiveIndexGenerationID != request.GenerationID ||
		source.KnowledgeBase.ContentVersion != request.ExpectedContentVersion ||
		source.Generation.IndexedContentVersion != request.ExpectedContentVersion ||
		(source.Generation.Status != value.IndexGenerationReady && source.Generation.Status != value.IndexGenerationBuilding) {
		return domainerrors.ErrGenerationStale
	}
	return nil
}

func finiteChunkRevisionVector(vector []float32) bool {
	for _, component := range vector {
		if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
			return false
		}
	}
	return true
}

func cloneChunkRevisionMetadata(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func chunkRevisionIndexErrorClass(err error) string {
	switch {
	case errors.Is(err, domainerrors.ErrRevisionConflict):
		return "revision_conflict"
	case errors.Is(err, domainerrors.ErrGenerationStale):
		return "generation_stale"
	case errors.Is(err, domainerrors.ErrValidation):
		return "validation_error"
	default:
		return "index_error"
	}
}
