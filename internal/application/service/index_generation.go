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
	// ResolveSelectableModel 解析指定类型的可选模型快照（embedding 或 rerank）。
	ResolveSelectableModel(context.Context, uuid.UUID, uuid.UUID, value.ModelType) (*model.ResolvedModel, error)
}

// RerankSelection 描述创建 Generation 时的重排显式三态输入。
type RerankSelection struct {
	Enabled       bool                    `json:"enabled"`
	ModelID       uuid.UUID               `json:"model_id"`
	CandidateTopK int                     `json:"candidate_top_k"`
	FailureMode   value.RerankFailureMode `json:"failure_mode"`
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
	// Rerank 为 nil 表示继承 base Generation 的重排选择；显式 enabled=false 关闭；
	// enabled=true 时 model_id/candidate_top_k/failure_mode 全部必填。
	Rerank *RerankSelection
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
		rerankSnapshot, err := s.resolveRerankSelection(txCtx, input.WorkspaceID, input.Rerank, base.Rerank)
		if err != nil {
			return err
		}
		modelHash, configHash, err := generationConfigHashes(resolved, chunkingConfig, retrievalConfig, rerankSnapshot)
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
			Rerank: rerankSnapshot,
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
	return map[string]any{"strategy": string(input.Strategy), "enable_parent_child": input.EnableParentChild, "parent_chunk_size": input.ParentChunkSize, "child_chunk_size": input.ChildChunkSize, "chunk_size": input.ChunkSize, "chunk_overlap": input.ChunkOverlap}, nil
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

// resolveRerankSelection 根据 CreateIndexGenerationInput.Rerank 三态解析最终快照：
// - nil：继承 base Generation 的 Rerank（若 base 启用，则用 base 的 model id 重新解析当前模型并重算 hash）。
// - enabled=false：新 Generation 关闭 Rerank。
// - enabled=true：校验 model_id/candidate_top_k/failure_mode，解析当前模型，candidate_top_k 不得超过模型 max_documents。
func (s *IndexGenerationService) resolveRerankSelection(ctx context.Context, workspaceID uuid.UUID, selection *RerankSelection, baseRerank *model.RerankSnapshot) (*model.RerankSnapshot, error) {
	if selection == nil {
		if baseRerank == nil {
			return nil, nil
		}
		// 继承：用 base 的 model id 重新解析当前模型并重算 hash，避免 base 模型已变更后漂移。
		return s.buildRerankSnapshot(ctx, workspaceID, baseRerank.ModelID, baseRerank.CandidateTopK, baseRerank.FailureMode)
	}
	if !selection.Enabled {
		return nil, nil
	}
	if selection.ModelID == uuid.Nil {
		return nil, fmt.Errorf("%w: enabled Rerank 必须提供 model_id", domainerrors.ErrValidation)
	}
	if err := value.ValidateRerankCandidateTopK(selection.CandidateTopK); err != nil {
		return nil, err
	}
	if !selection.FailureMode.IsValid() {
		return nil, fmt.Errorf("%w: Rerank failure_mode 无效", domainerrors.ErrValidation)
	}
	return s.buildRerankSnapshot(ctx, workspaceID, selection.ModelID, selection.CandidateTopK, selection.FailureMode)
}

func (s *IndexGenerationService) buildRerankSnapshot(ctx context.Context, workspaceID, modelID uuid.UUID, candidateTopK int, failureMode value.RerankFailureMode) (*model.RerankSnapshot, error) {
	resolved, err := s.models.ResolveSelectableModel(ctx, workspaceID, modelID, value.ModelTypeRerank)
	if err != nil {
		return nil, err
	}
	maxDocuments, err := rerankIntParameter(resolved.Model.Parameters, "max_documents")
	if err != nil {
		return nil, err
	}
	if candidateTopK > maxDocuments {
		return nil, fmt.Errorf("%w: candidate_top_k %d 超过模型 max_documents %d", domainerrors.ErrValidation, candidateTopK, maxDocuments)
	}
	configHash, err := rerankModelConfigHash(resolved)
	if err != nil {
		return nil, err
	}
	return &model.RerankSnapshot{
		ModelID:         resolved.Model.ID,
		ProviderID:      resolved.Provider.ID,
		ModelName:       resolved.Model.ModelName,
		ModelConfigHash: configHash,
		CandidateTopK:   candidateTopK,
		FailureMode:     failureMode,
	}, nil
}

func generationConfigHashes(
	resolved *model.ResolvedModel,
	chunkingConfig, retrievalConfig map[string]any,
	rerankSnapshot *model.RerankSnapshot,
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
	configInput := map[string]any{
		"model_config_hash": modelHash, "chunker_version": value.StandardChunkerVersion,
		"chunking_config": chunkingConfig, "retrieval_config": retrievalConfig,
	}
	if rerankSnapshot != nil {
		configInput["rerank"] = map[string]any{
			"model_id": rerankSnapshot.ModelID.String(), "provider_id": rerankSnapshot.ProviderID.String(),
			"model_name": rerankSnapshot.ModelName, "model_config_hash": rerankSnapshot.ModelConfigHash,
			"candidate_top_k": rerankSnapshot.CandidateTopK, "failure_mode": string(rerankSnapshot.FailureMode),
		}
	}
	configHash, err := CanonicalConfigHash(configInput)
	return modelHash, configHash, err
}

// rerankModelConfigHash 计算不含 dimensions 的 Rerank 模型 config hash，
// 与 Embedding 路径保持字段一致性（provider/provider_config/model_name/parameters）。
func rerankModelConfigHash(resolved *model.ResolvedModel) (string, error) {
	if resolved == nil || resolved.Model == nil || resolved.Provider == nil {
		return "", fmt.Errorf("%w: Rerank 模型快照无效", domainerrors.ErrValidation)
	}
	return CanonicalConfigHash(map[string]any{
		"provider": resolved.Provider.Provider, "provider_config": resolved.Provider.Config,
		"model_name": resolved.Model.ModelName, "parameters": resolved.Model.Parameters,
	})
}
