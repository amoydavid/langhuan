package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

const maxModelCatalogItems = 200

// ModelCatalogFilter describes a user-triggered Provider catalog request.
type ModelCatalogFilter struct {
	Type  *value.ModelType
	Query string
}

func (s *ModelProviderService) ListModelCatalogWorkspace(ctx context.Context, workspaceID, providerID uuid.UUID, filter ModelCatalogFilter) (*dto.ModelCatalogResponse, error) {
	provider, err := s.repository.GetVisible(ctx, workspaceID, providerID)
	if err != nil {
		return nil, err
	}
	return s.listModelCatalog(ctx, provider, filter)
}

func (s *ModelProviderService) ListModelCatalogPlatform(ctx context.Context, providerID uuid.UUID, filter ModelCatalogFilter) (*dto.ModelCatalogResponse, error) {
	provider, err := s.repository.GetPlatform(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return s.listModelCatalog(ctx, provider, filter)
}

func (s *ModelProviderService) listModelCatalog(ctx context.Context, provider *model.ModelProvider, filter ModelCatalogFilter) (*dto.ModelCatalogResponse, error) {
	if provider == nil {
		return nil, domainerrors.ErrNotFound
	}
	if provider.Status != value.ModelStatusActive {
		return nil, domainerrors.ErrProviderDisabled
	}
	filter, err := normalizeModelCatalogFilter(filter)
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolver.Resolve(provider.Provider)
	if err != nil {
		return nil, err
	}
	if resolved.ModelCatalog == nil {
		return nil, domainerrors.ErrCatalogUnavailable
	}
	credentials, err := s.cipher.Decrypt(provider.ID, provider.CredentialsCiphertext)
	if err != nil {
		return nil, fmt.Errorf("Provider %s: %w", provider.ID, domainerrors.ErrCredentialDecryption)
	}
	defer clearBytes(credentials)
	items, err := resolved.ModelCatalog.ListModels(ctx, modelcatalogport.Input{
		Scope:           provider.Scope,
		Config:          cloneCatalogConfig(provider.Config),
		CredentialsJSON: credentials,
		ModelType:       filter.Type,
		Query:           filter.Query,
	})
	if err != nil {
		return nil, err
	}
	if len(items) > maxModelCatalogItems {
		items = items[:maxModelCatalogItems]
	}
	result := make([]dto.ModelCatalogItem, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		result = append(result, dto.ModelCatalogItem{
			ID: item.ID, DisplayName: item.DisplayName, Description: item.Description,
			Type: cloneModelTypePointer(item.Type), Dimensions: cloneIntPointer(item.Dimensions),
			Parameters: cloneCatalogConfig(item.Parameters), Available: item.Available,
		})
	}
	return &dto.ModelCatalogResponse{
		Provider: provider.Provider, Items: result, Source: "provider_api", FetchedAt: time.Now().UTC(),
	}, nil
}

func normalizeModelCatalogFilter(filter ModelCatalogFilter) (ModelCatalogFilter, error) {
	if filter.Type != nil && *filter.Type != value.ModelTypeEmbedding && *filter.Type != value.ModelTypeRerank {
		return ModelCatalogFilter{}, domainerrors.ErrUnsupportedModelType
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 100 {
		return ModelCatalogFilter{}, fmt.Errorf("%w: 搜索词不能超过 100 个字符", domainerrors.ErrValidation)
	}
	return filter, nil
}

func cloneCatalogConfig(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneModelTypePointer(source *value.ModelType) *value.ModelType {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}

func cloneIntPointer(source *int) *int {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}
