package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelProviderRepository 定义模型连接应用服务需要的数据访问能力。
type ModelProviderRepository interface {
	Create(context.Context, *model.ModelProvider) error
	GetWorkspaceOwned(context.Context, uuid.UUID, uuid.UUID) (*model.ModelProvider, error)
	GetPlatform(context.Context, uuid.UUID) (*model.ModelProvider, error)
	GetVisible(context.Context, uuid.UUID, uuid.UUID) (*model.ModelProvider, error)
	ListVisible(context.Context, uuid.UUID) ([]*model.ModelProvider, error)
	ListPlatform(context.Context) ([]*model.ModelProvider, error)
	Update(context.Context, *model.ModelProvider) error
	Delete(context.Context, uuid.UUID) error
	CountModels(context.Context, uuid.UUID) (int64, error)
	CountModelsByProvider(context.Context, []uuid.UUID) (map[uuid.UUID]dto.ProviderModelCounts, error)
	CountGenerationReferences(context.Context, uuid.UUID) (int64, error)
}

// ModelRepository 定义具体模型应用服务需要的数据访问能力。
type ModelRepository interface {
	Create(context.Context, *model.Model) error
	GetWorkspaceOwned(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedModel, error)
	GetPlatform(context.Context, uuid.UUID) (*model.ResolvedModel, error)
	GetVisible(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedModel, error)
	ListByProviderVisible(context.Context, uuid.UUID, uuid.UUID) ([]*model.Model, error)
	ListByProviderPlatform(context.Context, uuid.UUID) ([]*model.Model, error)
	ListVisible(context.Context, uuid.UUID, value.ModelType, bool) ([]*model.ResolvedModel, error)
	ListManagedVisible(context.Context, uuid.UUID, ModelListFilter) ([]*model.ResolvedModel, error)
	ListManagedPlatform(context.Context, ModelListFilter) ([]*model.ResolvedModel, error)
	Update(context.Context, *model.Model) error
	Delete(context.Context, uuid.UUID) error
	CountGenerationReferences(context.Context, uuid.UUID) (int64, error)
}
