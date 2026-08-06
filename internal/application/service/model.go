package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// CreateModelInput contains user-editable model fields.
type CreateModelInput struct {
	ProviderID  uuid.UUID
	ActorID     uuid.UUID
	Name        string
	DisplayName string
	Description string
	Type        value.ModelType
	ModelName   string
	Dimensions  *int
	Parameters  json.RawMessage
}

// UpdateModelInput applies PATCH semantics to mutable model fields.
type UpdateModelInput struct {
	DisplayName *string
	Description *string
	ModelName   *string
	Dimensions  *int
	Parameters  *json.RawMessage
	Status      *value.ModelStatus
}

// ModelListFilter 描述管理目录支持的可选筛选条件。
type ModelListFilter struct {
	Type       *value.ModelType
	Status     *value.ModelStatus
	Scope      *value.ModelScope
	ProviderID *uuid.UUID
	Query      string
}

// ModelService manages Embedding 与 Rerank 模型，按 ModelType 路由 capability factory。
type ModelService struct {
	providers        ModelProviderRepository
	models           ModelRepository
	embeddingFactory embeddingport.FactoryRegistry
	rerankFactory    rerankport.FactoryRegistry
	descriptors      *ProviderDescriptorRegistry
}

// NewModelService creates a model application service.
func NewModelService(
	providers ModelProviderRepository,
	models ModelRepository,
	embeddingFactory embeddingport.FactoryRegistry,
	rerankFactory rerankport.FactoryRegistry,
	descriptors *ProviderDescriptorRegistry,
) *ModelService {
	return &ModelService{
		providers:        providers,
		models:           models,
		embeddingFactory: embeddingFactory,
		rerankFactory:    rerankFactory,
		descriptors:      descriptors,
	}
}

// CreateWorkspace creates a model under a Workspace-owned Provider.
func (s *ModelService) CreateWorkspace(ctx context.Context, workspaceID uuid.UUID, input CreateModelInput) (*dto.Model, error) {
	provider, err := s.providers.GetWorkspaceOwned(ctx, workspaceID, input.ProviderID)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, provider, input)
}

// CreatePlatform creates a model under a platform Provider.
func (s *ModelService) CreatePlatform(ctx context.Context, input CreateModelInput) (*dto.Model, error) {
	provider, err := s.providers.GetPlatform(ctx, input.ProviderID)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, provider, input)
}

func (s *ModelService) create(ctx context.Context, provider *model.ModelProvider, input CreateModelInput) (*dto.Model, error) {
	parameters, dimensions, err := s.decodeModelParameters(provider.Provider, input.Type, input.ModelName, input.Dimensions, input.Parameters)
	if err != nil {
		return nil, err
	}
	item, err := model.NewModel(provider.ID, input.Name, input.DisplayName, input.Description, input.Type, input.ModelName, dimensions, parameters, input.ActorID)
	if err != nil {
		return nil, err
	}
	if err := s.models.Create(ctx, item); err != nil {
		return nil, err
	}
	return dto.ModelFromResolved(&model.ResolvedModel{Model: item, Provider: provider}, 0), nil
}

// decodeModelParameters 按 ModelType 路由到对应能力域的 Factory。
// Embedding 要求 dimensions 必填且合法；Rerank 要求 dimensions 为空；LLM 拒绝。
func (s *ModelService) decodeModelParameters(
	providerKey string,
	modelType value.ModelType,
	modelName string,
	dimensions *int,
	parameters json.RawMessage,
) (map[string]any, *int, error) {
	if s.descriptors == nil || !s.descriptors.SupportsModelType(providerKey, modelType) {
		return nil, nil, domainerrors.ErrUnsupportedModelType
	}
	switch modelType {
	case value.ModelTypeEmbedding:
		if dimensions == nil {
			return nil, nil, domainerrors.ErrUnsupportedEmbeddingDimension
		}
		factory, err := s.embeddingFactory.Factory(modelType, providerKey)
		if err != nil {
			return nil, nil, err
		}
		decoded, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: modelName, Dimensions: *dimensions, Parameters: parameters})
		if err != nil {
			return nil, nil, err
		}
		return decoded, dimensions, nil
	case value.ModelTypeRerank:
		if dimensions != nil {
			return nil, nil, fmt.Errorf("%w: Rerank 模型不得设置 dimensions", domainerrors.ErrValidation)
		}
		factory, err := s.rerankFactory.Factory(providerKey)
		if err != nil {
			return nil, nil, err
		}
		decoded, err := factory.DecodeModel(rerankport.ModelDecodeInput{ModelName: modelName, Parameters: parameters})
		if err != nil {
			return nil, nil, err
		}
		return decoded, nil, nil
	default:
		return nil, nil, domainerrors.ErrUnsupportedModelType
	}
}

// ListWorkspace lists models for a visible Provider.
func (s *ModelService) ListWorkspace(ctx context.Context, workspaceID, providerID uuid.UUID) ([]*dto.Model, error) {
	provider, err := s.providers.GetVisible(ctx, workspaceID, providerID)
	if err != nil {
		return nil, err
	}
	items, err := s.models.ListByProviderVisible(ctx, workspaceID, providerID)
	return s.modelList(ctx, provider, items, err)
}

// ListPlatform lists models under a platform Provider.
func (s *ModelService) ListPlatform(ctx context.Context, providerID uuid.UUID) ([]*dto.Model, error) {
	provider, err := s.providers.GetPlatform(ctx, providerID)
	if err != nil {
		return nil, err
	}
	items, err := s.models.ListByProviderPlatform(ctx, providerID)
	return s.modelList(ctx, provider, items, err)
}

// ListSelectableWorkspace 返回当前 Workspace 可见的、指定类型的模型，供 Generation 选择。
func (s *ModelService) ListSelectableWorkspace(ctx context.Context, workspaceID uuid.UUID, modelType value.ModelType, activeOnly bool) ([]*dto.Model, error) {
	if modelType != value.ModelTypeEmbedding && modelType != value.ModelTypeRerank {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	items, err := s.models.ListVisible(ctx, workspaceID, modelType, activeOnly)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.Model, 0, len(items))
	for _, resolved := range items {
		count, countErr := s.models.CountGenerationReferences(ctx, resolved.Model.ID)
		if countErr != nil {
			return nil, countErr
		}
		result = append(result, dto.ModelFromResolved(resolved, count))
	}
	return result, nil
}

// ListSelectablePlatform 返回平台共享的、指定类型的模型。
func (s *ModelService) ListSelectablePlatform(ctx context.Context, modelType value.ModelType, activeOnly bool) ([]*dto.Model, error) {
	// 平台可选项复用 workspace 视图（平台 Provider 对所有 workspace 可见），
	// 使用零值 workspace 不合适；这里直接读取 platform-resolved models。
	return s.ListSelectableWorkspace(ctx, uuid.Nil, modelType, activeOnly)
}

// ListWorkspaceModels 返回 Workspace 可见的管理型模型目录。
func (s *ModelService) ListWorkspaceModels(ctx context.Context, workspaceID uuid.UUID, filter ModelListFilter) ([]*dto.Model, error) {
	filter, err := normalizeModelListFilter(filter)
	if err != nil {
		return nil, err
	}
	items, err := s.models.ListManagedVisible(ctx, workspaceID, filter)
	return s.resolvedModelList(ctx, items, err)
}

// ListPlatformModels 返回平台共享模型的管理目录。
func (s *ModelService) ListPlatformModels(ctx context.Context, filter ModelListFilter) ([]*dto.Model, error) {
	filter, err := normalizeModelListFilter(filter)
	if err != nil {
		return nil, err
	}
	items, err := s.models.ListManagedPlatform(ctx, filter)
	return s.resolvedModelList(ctx, items, err)
}

// ListSelectableForAPIKey exposes the deliberately narrow model contract used
// by programmatic management clients. The service re-validates the exact
// server-side filter instead of trusting query parameters or handler routing.
func (s *ModelService) ListSelectableForAPIKey(ctx context.Context, workspaceID uuid.UUID, filter ModelListFilter) ([]*dto.Model, error) {
	if workspaceID == uuid.Nil || filter.Type == nil || *filter.Type != value.ModelTypeEmbedding ||
		filter.Status == nil || *filter.Status != value.ModelStatusActive ||
		filter.Scope == nil || *filter.Scope != value.ModelScopePlatform ||
		strings.TrimSpace(filter.Query) != "" || filter.ProviderID != nil {
		return nil, fmt.Errorf("%w: Bearer 模型过滤条件必须为 embedding/active/platform", domainerrors.ErrValidation)
	}
	return s.ListPlatformModels(ctx, ModelListFilter{Type: filter.Type, Status: filter.Status, Scope: filter.Scope})
}

func normalizeModelListFilter(filter ModelListFilter) (ModelListFilter, error) {
	if filter.Type != nil && *filter.Type != value.ModelTypeEmbedding && *filter.Type != value.ModelTypeRerank {
		return ModelListFilter{}, domainerrors.ErrUnsupportedModelType
	}
	if filter.Status != nil && !filter.Status.IsValid() {
		return ModelListFilter{}, fmt.Errorf("%w: 模型 status 无效", domainerrors.ErrValidation)
	}
	if filter.Scope != nil && *filter.Scope != value.ModelScopeWorkspace && *filter.Scope != value.ModelScopePlatform {
		return ModelListFilter{}, fmt.Errorf("%w: 模型 scope 无效", domainerrors.ErrValidation)
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 100 {
		return ModelListFilter{}, fmt.Errorf("%w: 搜索词不能超过 100 个字符", domainerrors.ErrValidation)
	}
	return filter, nil
}

func (s *ModelService) resolvedModelList(ctx context.Context, items []*model.ResolvedModel, err error) ([]*dto.Model, error) {
	if err != nil {
		return nil, err
	}
	result := make([]*dto.Model, 0, len(items))
	for _, item := range items {
		count, countErr := s.models.CountGenerationReferences(ctx, item.Model.ID)
		if countErr != nil {
			return nil, countErr
		}
		result = append(result, dto.ModelFromResolved(item, count))
	}
	return result, nil
}

func (s *ModelService) modelList(ctx context.Context, provider *model.ModelProvider, items []*model.Model, err error) ([]*dto.Model, error) {
	if err != nil {
		return nil, err
	}
	result := make([]*dto.Model, 0, len(items))
	for _, item := range items {
		count, countErr := s.models.CountGenerationReferences(ctx, item.ID)
		if countErr != nil {
			return nil, countErr
		}
		result = append(result, dto.ModelFromResolved(&model.ResolvedModel{Model: item, Provider: provider}, count))
	}
	return result, nil
}

// GetWorkspace gets a visible Workspace or platform model.
func (s *ModelService) GetWorkspace(ctx context.Context, workspaceID, modelID uuid.UUID) (*dto.Model, error) {
	resolved, err := s.models.GetVisible(ctx, workspaceID, modelID)
	return s.modelDTO(ctx, resolved, err)
}

// GetPlatform gets one platform model.
func (s *ModelService) GetPlatform(ctx context.Context, modelID uuid.UUID) (*dto.Model, error) {
	resolved, err := s.models.GetPlatform(ctx, modelID)
	return s.modelDTO(ctx, resolved, err)
}

func (s *ModelService) modelDTO(ctx context.Context, resolved *model.ResolvedModel, err error) (*dto.Model, error) {
	if err != nil {
		return nil, err
	}
	count, err := s.models.CountGenerationReferences(ctx, resolved.Model.ID)
	if err != nil {
		return nil, err
	}
	return dto.ModelFromResolved(resolved, count), nil
}

// UpdateWorkspace updates a model under a Workspace-owned Provider.
func (s *ModelService) UpdateWorkspace(ctx context.Context, workspaceID, modelID uuid.UUID, input UpdateModelInput) (*dto.Model, error) {
	resolved, err := s.models.GetWorkspaceOwned(ctx, workspaceID, modelID)
	if err != nil {
		return nil, err
	}
	return s.update(ctx, resolved, input)
}

// UpdatePlatform updates a platform model.
func (s *ModelService) UpdatePlatform(ctx context.Context, modelID uuid.UUID, input UpdateModelInput) (*dto.Model, error) {
	resolved, err := s.models.GetPlatform(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return s.update(ctx, resolved, input)
}

func (s *ModelService) update(ctx context.Context, resolved *model.ResolvedModel, input UpdateModelInput) (*dto.Model, error) {
	item, provider := resolved.Model, resolved.Provider
	modelName := item.ModelName
	if input.ModelName != nil {
		modelName = *input.ModelName
	}
	dimensions := item.Dimensions
	if input.Dimensions != nil {
		dimensions = input.Dimensions
	}
	parametersRaw, err := chooseJSON(input.Parameters, item.Parameters)
	if err != nil {
		return nil, err
	}
	decodedParameters, decodedDimensions, err := s.decodeModelParameters(provider.Provider, item.Type, modelName, dimensions, parametersRaw)
	if err != nil {
		return nil, err
	}
	displayName, description := item.DisplayName, item.Description
	if input.DisplayName != nil {
		displayName = *input.DisplayName
	}
	if input.Description != nil {
		description = *input.Description
	}
	candidate, err := model.NewModel(item.ProviderID, item.Name, displayName, description, item.Type, modelName, decodedDimensions, decodedParameters, actorIDOrNew(item.CreatedBy))
	if err != nil {
		return nil, err
	}
	item.DisplayName, item.Description = candidate.DisplayName, candidate.Description
	item.ModelName, item.Dimensions, item.Parameters = candidate.ModelName, candidate.Dimensions, candidate.Parameters
	if input.Status != nil {
		if !input.Status.IsValid() {
			return nil, fmt.Errorf("%w: Model status 无效", domainerrors.ErrValidation)
		}
		item.Status = *input.Status
	}
	item.UpdatedAt = time.Now().UTC()
	if err := s.models.Update(ctx, item); err != nil {
		return nil, err
	}
	count, err := s.models.CountGenerationReferences(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	return dto.ModelFromResolved(resolved, count), nil
}

// DeleteWorkspace deletes an unreferenced Workspace model.
func (s *ModelService) DeleteWorkspace(ctx context.Context, workspaceID, modelID uuid.UUID) error {
	resolved, err := s.models.GetWorkspaceOwned(ctx, workspaceID, modelID)
	if err != nil {
		return err
	}
	return s.delete(ctx, resolved.Model.ID)
}

// DeletePlatform deletes an unreferenced platform model.
func (s *ModelService) DeletePlatform(ctx context.Context, modelID uuid.UUID) error {
	resolved, err := s.models.GetPlatform(ctx, modelID)
	if err != nil {
		return err
	}
	return s.delete(ctx, resolved.Model.ID)
}

func (s *ModelService) delete(ctx context.Context, modelID uuid.UUID) error {
	count, err := s.models.CountGenerationReferences(ctx, modelID)
	if err != nil {
		return err
	}
	if count > 0 {
		return domainerrors.ErrModelInUse
	}
	return s.models.Delete(ctx, modelID)
}
