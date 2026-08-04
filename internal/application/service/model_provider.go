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

// CreateModelProviderInput contains user-editable Provider fields.
type CreateModelProviderInput struct {
	Scope       value.ModelScope
	WorkspaceID *uuid.UUID
	ActorID     uuid.UUID
	Name        string
	DisplayName string
	Description string
	Provider    string
	Config      json.RawMessage
	Credentials json.RawMessage
}

// UpdateModelProviderInput applies PATCH semantics; nil fields are preserved.
type UpdateModelProviderInput struct {
	DisplayName *string
	Description *string
	Config      *json.RawMessage
	Credentials *json.RawMessage
	Status      *value.ModelStatus
}

// ModelProviderService manages model connections without exposing credentials.
// 它通过 ProviderFactoryResolver 同时支持 embedding 和 parser 能力域的 Provider。
type ModelProviderService struct {
	repository ModelProviderRepository
	cipher     embeddingport.CredentialCipher
	resolver   *ProviderFactoryResolver
}

// NewModelProviderService creates a Provider application service.
func NewModelProviderService(repository ModelProviderRepository, cipher embeddingport.CredentialCipher, resolver *ProviderFactoryResolver) *ModelProviderService {
	return &ModelProviderService{repository: repository, cipher: cipher, resolver: resolver}
}

// CreateWorkspace creates a Provider owned by one Workspace.
func (s *ModelProviderService) CreateWorkspace(ctx context.Context, workspaceID uuid.UUID, input CreateModelProviderInput) (*dto.ModelProvider, error) {
	input.Scope, input.WorkspaceID = value.ModelScopeWorkspace, &workspaceID
	return s.create(ctx, input)
}

// CreatePlatform creates a platform-shared Provider.
func (s *ModelProviderService) CreatePlatform(ctx context.Context, input CreateModelProviderInput) (*dto.ModelProvider, error) {
	input.Scope, input.WorkspaceID = value.ModelScopePlatform, nil
	return s.create(ctx, input)
}

func (s *ModelProviderService) create(ctx context.Context, input CreateModelProviderInput) (*dto.ModelProvider, error) {
	factory, err := s.resolver.Resolve(input.Provider)
	if err != nil {
		return nil, err
	}
	decoded, err := factory.DecodeProvider(input.Scope, input.Config, input.Credentials)
	if err != nil {
		return nil, err
	}
	provider, err := model.NewModelProvider(input.Scope, input.WorkspaceID, input.Name, input.DisplayName, input.Description, factory.ProviderName, decoded.Config, nil, input.ActorID)
	if err != nil {
		return nil, err
	}
	provider.CredentialsCiphertext, err = s.cipher.Encrypt(provider.ID, decoded.CredentialsJSON)
	clearBytes(decoded.CredentialsJSON)
	if err != nil {
		return nil, fmt.Errorf("加密 Provider 凭证失败: %w", err)
	}
	if err := s.repository.Create(ctx, provider); err != nil {
		return nil, err
	}
	return dto.ModelProviderFromModel(provider, factory.CredentialFields), nil
}

// ListWorkspace lists platform-shared and Workspace-owned Providers.
func (s *ModelProviderService) ListWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*dto.ModelProvider, error) {
	items, err := s.repository.ListVisible(ctx, workspaceID)
	return s.providerList(items, err)
}

// ListPlatform lists only platform-shared Providers.
func (s *ModelProviderService) ListPlatform(ctx context.Context) ([]*dto.ModelProvider, error) {
	items, err := s.repository.ListPlatform(ctx)
	return s.providerList(items, err)
}

func (s *ModelProviderService) providerList(items []*model.ModelProvider, err error) ([]*dto.ModelProvider, error) {
	if err != nil {
		return nil, err
	}
	result := make([]*dto.ModelProvider, 0, len(items))
	for _, item := range items {
		factory, factoryErr := s.resolver.Resolve(item.Provider)
		if factoryErr != nil {
			return nil, factoryErr
		}
		result = append(result, dto.ModelProviderFromModel(item, factory.CredentialFields))
	}
	return result, nil
}

// GetWorkspace gets a visible Workspace or platform-shared Provider.
func (s *ModelProviderService) GetWorkspace(ctx context.Context, workspaceID, providerID uuid.UUID) (*dto.ModelProvider, error) {
	provider, err := s.repository.GetVisible(ctx, workspaceID, providerID)
	return s.providerDTO(provider, err)
}

// GetPlatform gets one platform Provider.
func (s *ModelProviderService) GetPlatform(ctx context.Context, providerID uuid.UUID) (*dto.ModelProvider, error) {
	provider, err := s.repository.GetPlatform(ctx, providerID)
	return s.providerDTO(provider, err)
}

func (s *ModelProviderService) providerDTO(provider *model.ModelProvider, err error) (*dto.ModelProvider, error) {
	if err != nil {
		return nil, err
	}
	factory, err := s.resolver.Resolve(provider.Provider)
	if err != nil {
		return nil, err
	}
	return dto.ModelProviderFromModel(provider, factory.CredentialFields), nil
}

// UpdateWorkspace updates only a Provider owned by the given Workspace.
func (s *ModelProviderService) UpdateWorkspace(ctx context.Context, workspaceID, providerID uuid.UUID, input UpdateModelProviderInput) (*dto.ModelProvider, error) {
	provider, err := s.repository.GetWorkspaceOwned(ctx, workspaceID, providerID)
	if err != nil {
		return nil, err
	}
	return s.update(ctx, provider, input)
}

// UpdatePlatform updates only a platform Provider.
func (s *ModelProviderService) UpdatePlatform(ctx context.Context, providerID uuid.UUID, input UpdateModelProviderInput) (*dto.ModelProvider, error) {
	provider, err := s.repository.GetPlatform(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return s.update(ctx, provider, input)
}

func (s *ModelProviderService) update(ctx context.Context, provider *model.ModelProvider, input UpdateModelProviderInput) (*dto.ModelProvider, error) {
	factory, err := s.resolver.Resolve(provider.Provider)
	if err != nil {
		return nil, err
	}
	if input.DisplayName != nil {
		provider.DisplayName = *input.DisplayName
	}
	if input.Description != nil {
		provider.Description = *input.Description
	}
	if input.Status != nil {
		if !input.Status.IsValid() {
			return nil, fmt.Errorf("%w: Provider status 无效", domainerrors.ErrValidation)
		}
		provider.Status = *input.Status
	}
	if input.Config != nil || input.Credentials != nil {
		configRaw, err := chooseJSON(input.Config, provider.Config)
		if err != nil {
			return nil, err
		}
		credentialsRaw := json.RawMessage(nil)
		if input.Credentials != nil {
			credentialsRaw = append(json.RawMessage(nil), (*input.Credentials)...)
			defer clearBytes(credentialsRaw)
		} else {
			credentialsRaw, err = s.cipher.Decrypt(provider.ID, provider.CredentialsCiphertext)
			if err != nil {
				return nil, fmt.Errorf("Provider %s: %w", provider.ID, domainerrors.ErrCredentialDecryption)
			}
			defer clearBytes(credentialsRaw)
		}
		decoded, err := factory.DecodeProvider(provider.Scope, configRaw, credentialsRaw)
		if err != nil {
			return nil, err
		}
		defer clearBytes(decoded.CredentialsJSON)
		provider.Config = decoded.Config
		if input.Credentials != nil {
			provider.CredentialsCiphertext, err = s.cipher.Encrypt(provider.ID, decoded.CredentialsJSON)
			if err != nil {
				return nil, fmt.Errorf("加密 Provider 凭证失败: %w", err)
			}
		}
	}
	validated, err := model.NewModelProvider(provider.Scope, provider.WorkspaceID, provider.Name, provider.DisplayName, provider.Description, provider.Provider, provider.Config, provider.CredentialsCiphertext, actorIDOrNew(provider.CreatedBy))
	if err != nil {
		return nil, err
	}
	provider.DisplayName, provider.Description = validated.DisplayName, validated.Description
	provider.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, provider); err != nil {
		return nil, err
	}
	return dto.ModelProviderFromModel(provider, factory.CredentialFields), nil
}

// DeleteWorkspace deletes an unreferenced Workspace Provider.
func (s *ModelProviderService) DeleteWorkspace(ctx context.Context, workspaceID, providerID uuid.UUID) error {
	provider, err := s.repository.GetWorkspaceOwned(ctx, workspaceID, providerID)
	if err != nil {
		return err
	}
	return s.delete(ctx, provider.ID)
}

// DeletePlatform deletes an unreferenced platform Provider.
func (s *ModelProviderService) DeletePlatform(ctx context.Context, providerID uuid.UUID) error {
	provider, err := s.repository.GetPlatform(ctx, providerID)
	if err != nil {
		return err
	}
	return s.delete(ctx, provider.ID)
}

func (s *ModelProviderService) delete(ctx context.Context, providerID uuid.UUID) error {
	count, err := s.repository.CountModels(ctx, providerID)
	if err != nil {
		return err
	}
	if count > 0 {
		return domainerrors.ErrProviderInUse
	}
	return s.repository.Delete(ctx, providerID)
}

func chooseJSON(input *json.RawMessage, fallback any) (json.RawMessage, error) {
	if input != nil {
		return append(json.RawMessage(nil), (*input)...), nil
	}
	raw, err := json.Marshal(fallback)
	if err != nil {
		return nil, fmt.Errorf("%w: 编码当前 Provider 配置失败", domainerrors.ErrInvalidProviderConfig)
	}
	return raw, nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func actorIDOrNew(value *uuid.UUID) uuid.UUID {
	if value != nil && *value != uuid.Nil {
		return *value
	}
	return uuid.New()
}
