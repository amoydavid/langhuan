package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

const indexGenerationBuildJobType = "index_generation_build"

// IndexGenerationModelResolver resolves a selectable immutable model snapshot.
type IndexGenerationModelResolver interface {
	ResolveSelectable(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedModel, error)
}

// IndexGenerationServiceDeps contains generation lifecycle dependencies.
type IndexGenerationServiceDeps struct {
	Store  IndexGenerationStore
	Models IndexGenerationModelResolver
	Queue  queue.JobQueue
}

// IndexGenerationService manages double-buffer generation creation and activation.
type IndexGenerationService struct {
	store  IndexGenerationStore
	models IndexGenerationModelResolver
	queue  queue.JobQueue
}

// CreateIndexGenerationInput selects the next immutable generation configuration.
type CreateIndexGenerationInput struct {
	WorkspaceID, KnowledgeBaseID uuid.UUID
	EmbeddingModelID             uuid.UUID
	ChunkingConfig               *value.ChunkingConfig
	RetrievalConfig              *RetrievalConfig
	ActorRole                    value.WorkspaceRole
}

// ActivateIndexGenerationInput carries the explicit activation confirmation.
type ActivateIndexGenerationInput struct {
	WorkspaceID, KnowledgeBaseID, GenerationID uuid.UUID
	ArchiveManualEdits                         bool
	ActorRole                                  value.WorkspaceRole
}

// NewIndexGenerationService creates the Generation lifecycle use case.
func NewIndexGenerationService(deps IndexGenerationServiceDeps) *IndexGenerationService {
	return &IndexGenerationService{store: deps.Store, models: deps.Models, queue: deps.Queue}
}

// List returns newest generations first inside one Workspace/KB lineage.
func (s *IndexGenerationService) List(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) ([]*dto.IndexGeneration, error) {
	if s.store == nil || workspaceID == uuid.Nil || knowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: Generation list lineage/store 无效", domainerrors.ErrValidation)
	}
	items, err := s.store.List(ctx, workspaceID, knowledgeBaseID)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.IndexGeneration, len(items))
	for index, item := range items {
		result[index] = dto.IndexGenerationFromModel(item)
	}
	return result, nil
}

// Create snapshots active state and queues an inactive Generation build.
func (s *IndexGenerationService) Create(ctx context.Context, input CreateIndexGenerationInput) (*dto.IndexGeneration, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || !input.ActorRole.AtLeast(value.RoleAdmin) {
		if !input.ActorRole.AtLeast(value.RoleAdmin) {
			return nil, domainerrors.ErrForbidden
		}
		return nil, fmt.Errorf("%w: Generation lineage 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil || s.models == nil || s.queue == nil {
		return nil, fmt.Errorf("%w: Generation dependencies 不能为空", domainerrors.ErrValidation)
	}
	var generation *model.IndexGeneration
	var job *model.Job
	err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx IndexGenerationTx) error {
		kb, err := tx.GetKnowledgeBaseForUpdate(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		if kb.WorkspaceID != input.WorkspaceID || kb.ActiveIndexGenerationID == nil {
			return domainerrors.ErrNotFound
		}
		base, err := tx.GetIndexGeneration(txCtx, *kb.ActiveIndexGenerationID)
		if err != nil {
			return err
		}
		modelID := input.EmbeddingModelID
		if modelID == uuid.Nil {
			modelID = base.EmbeddingModelID
		}
		resolved, err := s.models.ResolveSelectable(txCtx, input.WorkspaceID, modelID)
		if err != nil {
			return err
		}
		chunkingConfig, err := generationChunkingConfig(input.ChunkingConfig, base.ChunkingConfig)
		if err != nil {
			return err
		}
		retrievalConfig, err := generationRetrievalConfig(input.RetrievalConfig, base.RetrievalConfig)
		if err != nil {
			return err
		}
		modelHash, configHash, err := generationConfigHashes(resolved, chunkingConfig, retrievalConfig)
		if err != nil {
			return err
		}
		manualCount, disabledCount, err := tx.GetActiveManualEditStats(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		disposition := value.ManualEditNotApplicable
		if !reflect.DeepEqual(chunkingConfig, base.ChunkingConfig) && manualCount > 0 {
			disposition = value.ManualEditPending
		}
		baseID := base.ID
		generation, err = model.NewIndexGeneration(model.NewIndexGenerationInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			BaseGenerationID: &baseID, EmbeddingModelID: resolved.Model.ID,
			ProviderID: resolved.Provider.ID, ModelName: resolved.Model.ModelName,
			EmbeddingDimension: *resolved.Model.Dimensions, ModelConfigHash: modelHash,
			ChunkerVersion: value.StandardChunkerVersion, ChunkingConfig: chunkingConfig,
			RetrievalConfig: retrievalConfig, ConfigHash: configHash,
			SourceContentVersion: kb.ContentVersion, IndexedContentVersion: kb.ContentVersion,
			Status: value.IndexGenerationBuilding, ManualEditDisposition: disposition,
		})
		if err != nil {
			return err
		}
		generation.ManualEditCount = manualCount
		generation.DisabledChunkCount = disabledCount
		job, err = model.NewJob(model.NewJobInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			IndexGenerationID: generation.ID, Type: indexGenerationBuildJobType,
			Status: value.JobStatusPending,
			Payload: map[string]any{
				"workspace_id": input.WorkspaceID.String(), "knowledge_base_id": input.KnowledgeBaseID.String(),
				"index_generation_id": generation.ID.String(),
			},
		})
		if err != nil {
			return err
		}
		return tx.CreateIndexGeneration(txCtx, generation, job)
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"workspace_id": input.WorkspaceID, "knowledge_base_id": input.KnowledgeBaseID,
		"generation_id": generation.ID, "job_id": job.ID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: indexGenerationBuildJobType, Payload: payload,
		TaskID: fmt.Sprintf("%s:%s:%s", indexGenerationBuildJobType, input.WorkspaceID, generation.ID),
	}); err != nil {
		cause := fmt.Errorf("入队 Generation 构建任务失败: %w", err)
		request := IndexGenerationBuildRequest{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			GenerationID: generation.ID, JobID: job.ID, TerminalAttempt: true,
		}
		if recordErr := s.store.RecordFailure(ctx, request, "enqueue_error", cause.Error(), true); recordErr != nil {
			return nil, errors.Join(cause, fmt.Errorf("持久化 Generation 入队失败状态失败: %w", recordErr))
		}
		return nil, cause
	}
	return dto.IndexGenerationFromModel(generation), nil
}

// Activate atomically switches the KB pointer after validating the build snapshot.
func (s *IndexGenerationService) Activate(ctx context.Context, input ActivateIndexGenerationInput) (*dto.IndexGeneration, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.GenerationID == uuid.Nil {
		return nil, fmt.Errorf("%w: Generation activation lineage 不能为空", domainerrors.ErrValidation)
	}
	if !input.ActorRole.AtLeast(value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	if s.store == nil {
		return nil, fmt.Errorf("%w: Generation store 不能为空", domainerrors.ErrValidation)
	}
	var candidate *model.IndexGeneration
	var activationOutcome error
	err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx IndexGenerationTx) error {
		kb, err := tx.GetKnowledgeBaseForUpdate(txCtx, input.KnowledgeBaseID)
		if err != nil {
			return err
		}
		candidate, err = tx.GetIndexGeneration(txCtx, input.GenerationID)
		if err != nil {
			return err
		}
		if candidate.WorkspaceID != input.WorkspaceID || candidate.KnowledgeBaseID != input.KnowledgeBaseID ||
			kb.ActiveIndexGenerationID == nil {
			return domainerrors.ErrNotFound
		}
		if err := candidate.ValidateActivation(*kb.ActiveIndexGenerationID, kb.ContentVersion, input.ArchiveManualEdits); err != nil {
			if errors.Is(err, domainerrors.ErrGenerationStale) {
				candidate.Status = value.IndexGenerationStale
				activationOutcome = err
				return tx.ActivateIndexGeneration(txCtx, kb, candidate, nil)
			}
			return err
		}
		base, err := tx.GetIndexGeneration(txCtx, *kb.ActiveIndexGenerationID)
		if err != nil {
			return err
		}
		if candidate.ManualEditDisposition == value.ManualEditPending {
			candidate.ManualEditDisposition = value.ManualEditArchiveConfirmed
		}
		return tx.ActivateIndexGeneration(txCtx, kb, candidate, base)
	})
	if err != nil {
		return nil, err
	}
	if activationOutcome != nil {
		return nil, activationOutcome
	}
	return dto.IndexGenerationFromModel(candidate), nil
}

func generationChunkingConfig(input *value.ChunkingConfig, fallback map[string]any) (map[string]any, error) {
	if input == nil {
		return cloneGenerationConfig(fallback), nil
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	return map[string]any{"chunk_size": input.ChunkSize, "chunk_overlap": input.ChunkOverlap}, nil
}

func generationRetrievalConfig(input *RetrievalConfig, fallback map[string]any) (map[string]any, error) {
	if input == nil {
		return cloneGenerationConfig(fallback), nil
	}
	if input.FTSConfig == "" ||
		input.VectorTopK < minRetrievalTopK || input.VectorTopK > maxCandidateTopK ||
		input.KeywordTopK < minRetrievalTopK || input.KeywordTopK > maxCandidateTopK ||
		input.FinalTopK < minRetrievalTopK || input.FinalTopK > maxFinalTopK ||
		input.RRFK < 1 {
		return nil, fmt.Errorf("%w: RetrievalConfig 无效", domainerrors.ErrValidation)
	}
	return map[string]any{
		"fts_config": input.FTSConfig, "vector_top_k": input.VectorTopK,
		"keyword_top_k": input.KeywordTopK, "final_top_k": input.FinalTopK, "rrf_k": input.RRFK,
	}, nil
}

func cloneGenerationConfig(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}

func generationConfigHashes(
	resolved *model.ResolvedModel,
	chunkingConfig, retrievalConfig map[string]any,
) (string, string, error) {
	if resolved == nil || resolved.Model == nil || resolved.Provider == nil || resolved.Model.Dimensions == nil {
		return "", "", fmt.Errorf("%w: Generation model snapshot 无效", domainerrors.ErrValidation)
	}
	modelHash, err := CanonicalConfigHash(map[string]any{
		"provider": resolved.Provider.Provider, "provider_config": resolved.Provider.Config,
		"model_name": resolved.Model.ModelName, "dimensions": *resolved.Model.Dimensions,
		"parameters": resolved.Model.Parameters,
	})
	if err != nil {
		return "", "", err
	}
	configHash, err := CanonicalConfigHash(map[string]any{
		"model_config_hash": modelHash, "chunker_version": value.StandardChunkerVersion,
		"chunking_config": chunkingConfig, "retrieval_config": retrievalConfig,
	})
	return modelHash, configHash, err
}
