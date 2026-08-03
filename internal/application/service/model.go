package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
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
	Dimensions  int
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

// ModelService manages concrete Embedding models.
type ModelService struct {
	providers ModelProviderRepository
	models    ModelRepository
	registry  embeddingport.FactoryRegistry
}

// NewModelService creates a model application service.
func NewModelService(providers ModelProviderRepository, models ModelRepository, registry embeddingport.FactoryRegistry) *ModelService {
	return &ModelService{providers: providers, models: models, registry: registry}
}

// CreateWorkspace creates a model under a Workspace-owned Provider.
func (s *ModelService) CreateWorkspace(ctx context.Context, workspaceID uuid.UUID, input CreateModelInput) (*dto.Model, error) {
	if input.Type != value.ModelTypeEmbedding {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	provider, err := s.providers.GetWorkspaceOwned(ctx, workspaceID, input.ProviderID)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, provider, input)
}

// CreatePlatform creates a model under a platform Provider.
func (s *ModelService) CreatePlatform(ctx context.Context, input CreateModelInput) (*dto.Model, error) {
	if input.Type != value.ModelTypeEmbedding {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	provider, err := s.providers.GetPlatform(ctx, input.ProviderID)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, provider, input)
}

func (s *ModelService) create(ctx context.Context, provider *model.ModelProvider, input CreateModelInput) (*dto.Model, error) {
	factory, err := s.registry.Factory(input.Type, provider.Provider)
	if err != nil {
		return nil, err
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: input.ModelName, Dimensions: input.Dimensions, Parameters: input.Parameters})
	if err != nil {
		return nil, err
	}
	dimensions := input.Dimensions
	item, err := model.NewModel(provider.ID, input.Name, input.DisplayName, input.Description, input.Type, input.ModelName, &dimensions, parameters, input.ActorID)
	if err != nil {
		return nil, err
	}
	if err := s.models.Create(ctx, item); err != nil {
		return nil, err
	}
	return dto.ModelFromResolved(&model.ResolvedModel{Model: item, Provider: provider}, 0), nil
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

func (s *ModelService) modelList(ctx context.Context, provider *model.ModelProvider, items []*model.Model, err error) ([]*dto.Model, error) {
	if err != nil {
		return nil, err
	}
	result := make([]*dto.Model, 0, len(items))
	for _, item := range items {
		count, countErr := s.models.CountKnowledgeBaseReferences(ctx, item.ID)
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
	count, err := s.models.CountKnowledgeBaseReferences(ctx, resolved.Model.ID)
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
	if item.Type != value.ModelTypeEmbedding || item.Dimensions == nil {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	modelName, dimensions := item.ModelName, *item.Dimensions
	if input.ModelName != nil {
		modelName = *input.ModelName
	}
	if input.Dimensions != nil {
		dimensions = *input.Dimensions
	}
	parametersRaw, err := chooseJSON(input.Parameters, item.Parameters)
	if err != nil {
		return nil, err
	}
	factory, err := s.registry.Factory(item.Type, provider.Provider)
	if err != nil {
		return nil, err
	}
	parameters, err := factory.DecodeModel(embeddingport.ModelDecodeInput{ModelName: modelName, Dimensions: dimensions, Parameters: parametersRaw})
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
	candidate, err := model.NewModel(item.ProviderID, item.Name, displayName, description, item.Type, modelName, &dimensions, parameters, actorIDOrNew(item.CreatedBy))
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
	count, err := s.models.CountKnowledgeBaseReferences(ctx, item.ID)
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
	count, err := s.models.CountKnowledgeBaseReferences(ctx, modelID)
	if err != nil {
		return err
	}
	if count > 0 {
		return domainerrors.ErrModelInUse
	}
	return s.models.Delete(ctx, modelID)
}
