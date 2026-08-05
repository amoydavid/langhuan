package service

import (
	"context"
	"errors"
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// IndexGenerationBuildRequest identifies one full inactive Generation build.
type IndexGenerationBuildRequest struct {
	WorkspaceID, KnowledgeBaseID, GenerationID, JobID uuid.UUID
	TerminalAttempt                                   bool
}

// IndexGenerationBuildDocument selects one active DocumentRevision and its reusable ChunkSet.
type IndexGenerationBuildDocument struct {
	DocumentID, DocumentRevisionID, ChunkSetID uuid.UUID
	Kind                                       value.DocumentKind
}

// IndexGenerationBuildSource is the trusted snapshot loaded for a build task.
type IndexGenerationBuildSource struct {
	Job            *model.Job
	KnowledgeBase  *model.KnowledgeBase
	Generation     *model.IndexGeneration
	BaseGeneration *model.IndexGeneration
	Documents      []IndexGenerationBuildDocument
}

// IndexGenerationBuildStore owns task state and atomic inactive publication/readiness.
type IndexGenerationBuildStore interface {
	Load(context.Context, IndexGenerationBuildRequest) (*IndexGenerationBuildSource, error)
	MarkRunning(context.Context, IndexGenerationBuildRequest) error
	Complete(context.Context, IndexGenerationBuildRequest, []*model.RetrievalEntry, int64, int64) error
	RecordFailure(context.Context, IndexGenerationBuildRequest, string, string, bool) error
}

// IndexGenerationChunker materializes a standard ChunkSet for one revision/config snapshot.
type IndexGenerationChunker interface {
	RunChunk(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (uuid.UUID, error)
}

// IndexGenerationBuildDeps contains full rebuild dependencies.
type IndexGenerationBuildDeps struct {
	Store    IndexGenerationBuildStore
	Chunker  IndexGenerationChunker
	Sources  indexport.SourceRepository
	Resolver EmbeddingClientResolver
	Index    indexport.RetrievalIndex
}

// IndexGenerationBuildService builds a complete inactive retrieval projection.
type IndexGenerationBuildService struct {
	store    IndexGenerationBuildStore
	chunker  IndexGenerationChunker
	sources  indexport.SourceRepository
	resolver EmbeddingClientResolver
	index    indexport.RetrievalIndex
}

// NewIndexGenerationBuildService creates the full-generation build use case.
func NewIndexGenerationBuildService(deps IndexGenerationBuildDeps) *IndexGenerationBuildService {
	return &IndexGenerationBuildService{
		store: deps.Store, chunker: deps.Chunker, sources: deps.Sources,
		resolver: deps.Resolver, index: deps.Index,
	}
}

// Run optionally rechunks File/Web, stages every enabled Chunk, then marks the inactive Generation ready.
func (s *IndexGenerationBuildService) Run(ctx context.Context, request IndexGenerationBuildRequest) error {
	if err := validateGenerationBuildRequest(request); err != nil {
		return err
	}
	if s.store == nil || s.chunker == nil || s.sources == nil || s.resolver == nil || s.index == nil {
		return fmt.Errorf("%w: Generation build dependencies 不能为空", domainerrors.ErrValidation)
	}
	source, err := s.store.Load(ctx, request)
	if err != nil {
		return err
	}
	if err := validateGenerationBuildSource(request, source); err != nil {
		return s.fail(ctx, request, err)
	}
	if source.Generation.Status == value.IndexGenerationReady {
		if source.Job.Status == value.JobStatusCompleted {
			return nil
		}
		return s.store.Complete(
			ctx, request, nil, source.Generation.DocumentCount, source.Generation.ChunkCount,
		)
	}
	if source.Job.Status == value.JobStatusCompleted {
		return s.fail(ctx, request, fmt.Errorf("%w: Generation/Job 完成状态不一致", domainerrors.ErrValidation))
	}
	if source.Generation.Status != value.IndexGenerationBuilding {
		return s.fail(ctx, request, domainerrors.ErrGenerationNotReady)
	}
	if err := s.store.MarkRunning(ctx, request); err != nil {
		return err
	}
	rechunk := !reflect.DeepEqual(source.Generation.ChunkingConfig, source.BaseGeneration.ChunkingConfig)
	readySources := make([]*indexport.Source, 0, len(source.Documents))
	var chunkCount int64
	for _, document := range source.Documents {
		chunkSetID := document.ChunkSetID
		if rechunk && document.Kind != value.DocumentKindFAQ {
			chunkSetID, err = s.chunker.RunChunk(
				ctx, request.WorkspaceID, document.DocumentRevisionID, request.GenerationID,
			)
			if err != nil {
				return s.fail(ctx, request, err)
			}
		}
		if chunkSetID == uuid.Nil {
			return s.fail(ctx, request, fmt.Errorf("%w: active Document 缺少 ready ChunkSet", domainerrors.ErrValidation))
		}
		ready, err := s.sources.GetReadyIndexSource(ctx, request.WorkspaceID, chunkSetID)
		if err != nil {
			return s.fail(ctx, request, err)
		}
		if err := validateGenerationReadySource(request, document, ready); err != nil {
			return s.fail(ctx, request, err)
		}
		readySources = append(readySources, ready)
		chunkCount += int64(len(ready.Chunks))
	}
	entries, staged, err := s.stage(ctx, request, source.Generation, readySources)
	if err != nil {
		return s.fail(ctx, request, err)
	}
	if len(staged) > 0 {
		ftsConfig, ok := source.Generation.RetrievalConfig["fts_config"].(string)
		ftsConfig = strings.TrimSpace(ftsConfig)
		if !ok || ftsConfig == "" {
			return s.fail(ctx, request, fmt.Errorf("%w: Generation fts_config 无效", domainerrors.ErrValidation))
		}
		if err := s.index.StageBatch(
			ctx, request.WorkspaceID, ftsConfig, source.Generation.EmbeddingDimension, staged,
		); err != nil {
			return s.fail(ctx, request, err)
		}
	}
	if err := s.store.Complete(ctx, request, entries, int64(len(source.Documents)), chunkCount); err != nil {
		return s.fail(ctx, request, err)
	}
	return nil
}

func (s *IndexGenerationBuildService) stage(
	ctx context.Context,
	request IndexGenerationBuildRequest,
	generation *model.IndexGeneration,
	sources []*indexport.Source,
) ([]*model.RetrievalEntry, []indexport.StageEntry, error) {
	resolved, err := s.resolver.Resolve(ctx, request.WorkspaceID, generation.EmbeddingModelID)
	if err != nil {
		return nil, nil, err
	}
	if resolved == nil || resolved.Client == nil || resolved.ProviderID != generation.ProviderID ||
		resolved.ModelName != generation.ModelName || resolved.Dimensions != generation.EmbeddingDimension {
		return nil, nil, domainerrors.ErrDimensionMismatch
	}
	entries := make([]*model.RetrievalEntry, 0)
	texts := make([]string, 0)
	for _, source := range sources {
		for index, chunk := range source.Chunks {
			revision := source.Revisions[index]
			role := chunk.Role
			if role == "" {
				role = value.ChunkRoleFlat
			}
			if !role.IsRetrievable() || !revision.Enabled {
				continue
			}
			searchContent, content := strings.TrimSpace(revision.EmbeddingContent), strings.TrimSpace(revision.Content)
			if searchContent == "" || content == "" {
				return nil, nil, fmt.Errorf("%w: enabled ChunkRevision content 不能为空", domainerrors.ErrValidation)
			}
			entries = append(entries, &model.RetrievalEntry{
				ID: id.New(), WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
				IndexGenerationID: request.GenerationID, DocumentID: chunk.DocumentID,
				DocumentRevisionID: chunk.DocumentRevisionID, ChunkSetID: chunk.ChunkSetID,
				ChunkID: chunk.ID, ChunkRevisionID: revision.ID, State: value.RetrievalEntryStaging,
				SearchContent: searchContent, Content: content, SourceAnchor: chunk.SourceAnchor,
				Metadata: cloneGenerationBuildMetadata(chunk.Metadata), CreatedAt: time.Now().UTC(),
			})
			texts = append(texts, searchContent)
		}
	}
	vectors, err := embedGenerationBuildTexts(ctx, resolved, texts)
	if err != nil {
		return nil, nil, err
	}
	staged := make([]indexport.StageEntry, len(entries))
	for index := range entries {
		staged[index] = indexport.StageEntry{Entry: entries[index], Embedding: vectors[index]}
	}
	return entries, staged, nil
}

func embedGenerationBuildTexts(
	ctx context.Context,
	resolved *ResolvedEmbeddingClient,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	batchSize := resolved.BatchSize
	if batchSize < 1 {
		batchSize = 1
	}
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := min(start+batchSize, len(texts))
		result, err := resolved.Client.Embed(ctx, embeddingport.EmbedInput{Texts: texts[start:end]})
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Vectors) != end-start {
			return nil, domainerrors.ErrInvalidEmbeddingResponse
		}
		for _, vector := range result.Vectors {
			if len(vector) != resolved.Dimensions || !finiteChunkRevisionVector(vector) {
				return nil, domainerrors.ErrInvalidEmbeddingResponse
			}
			vectors = append(vectors, vector)
		}
	}
	return vectors, nil
}

func (s *IndexGenerationBuildService) fail(
	ctx context.Context,
	request IndexGenerationBuildRequest,
	cause error,
) error {
	terminal := request.TerminalAttempt || isPermanentGenerationBuildError(cause)
	if err := s.store.RecordFailure(
		ctx, request, generationBuildErrorClass(cause), cause.Error(), terminal,
	); err != nil {
		return errors.Join(cause, fmt.Errorf("持久化 Generation 构建失败状态失败: %w", err))
	}
	return cause
}

func isPermanentGenerationBuildError(err error) bool {
	return errors.Is(err, domainerrors.ErrValidation) ||
		errors.Is(err, domainerrors.ErrDimensionMismatch) ||
		errors.Is(err, domainerrors.ErrGenerationStale) ||
		errors.Is(err, domainerrors.ErrGenerationNotReady) ||
		errors.Is(err, domainerrors.ErrNotFound)
}

func validateGenerationBuildRequest(request IndexGenerationBuildRequest) error {
	if request.WorkspaceID == uuid.Nil || request.KnowledgeBaseID == uuid.Nil ||
		request.GenerationID == uuid.Nil || request.JobID == uuid.Nil {
		return fmt.Errorf("%w: Generation build lineage 不能为空", domainerrors.ErrValidation)
	}
	return nil
}

func validateGenerationBuildSource(request IndexGenerationBuildRequest, source *IndexGenerationBuildSource) error {
	if source == nil || source.Job == nil || source.KnowledgeBase == nil || source.Generation == nil ||
		source.BaseGeneration == nil || source.Generation.BaseGenerationID == nil ||
		source.Job.ID != request.JobID || source.Job.WorkspaceID != request.WorkspaceID ||
		source.Job.KnowledgeBaseID != request.KnowledgeBaseID || source.Job.IndexGenerationID != request.GenerationID ||
		source.Job.Type != indexGenerationBuildJobType || source.Generation.ID != request.GenerationID ||
		source.Generation.WorkspaceID != request.WorkspaceID || source.Generation.KnowledgeBaseID != request.KnowledgeBaseID ||
		*source.Generation.BaseGenerationID != source.BaseGeneration.ID ||
		source.KnowledgeBase.ActiveIndexGenerationID == nil ||
		*source.KnowledgeBase.ActiveIndexGenerationID != source.BaseGeneration.ID {
		return fmt.Errorf("%w: Generation build source lineage 无效", domainerrors.ErrValidation)
	}
	return nil
}

func validateGenerationReadySource(
	request IndexGenerationBuildRequest,
	document IndexGenerationBuildDocument,
	source *indexport.Source,
) error {
	if source == nil || source.ChunkSet == nil || source.ChunkSet.WorkspaceID != request.WorkspaceID ||
		source.ChunkSet.KnowledgeBaseID != request.KnowledgeBaseID || source.ChunkSet.DocumentID != document.DocumentID ||
		source.ChunkSet.DocumentRevisionID != document.DocumentRevisionID || source.ChunkSet.Status != value.ChunkSetReady ||
		len(source.Chunks) != len(source.Revisions) || int64(len(source.Chunks)) != source.ChunkSet.ChunkCount {
		return fmt.Errorf("%w: Generation ChunkSet source 无效", domainerrors.ErrValidation)
	}
	return nil
}

func cloneGenerationBuildMetadata(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}

func generationBuildErrorClass(err error) string {
	if errors.Is(err, domainerrors.ErrGenerationStale) {
		return "generation_stale"
	}
	if errors.Is(err, domainerrors.ErrValidation) {
		return "validation_error"
	}
	return "build_error"
}
