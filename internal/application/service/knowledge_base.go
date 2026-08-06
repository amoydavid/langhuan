package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseService manages KnowledgeBases.
type KnowledgeBaseService struct {
	binder  KnowledgeBaseModelBinder
	store   KnowledgeBaseCreateStore
	updater KnowledgeBaseBasicsUpdater
}

// CreateKnowledgeBaseInput contains KnowledgeBase creation fields.
type CreateKnowledgeBaseInput struct {
	WorkspaceID      uuid.UUID
	Name             string
	Description      string
	EmbeddingModelID uuid.UUID
	ChunkingConfig   *value.ChunkingConfig
	// CallerAPIKeyID 在 API Key 主体创建知识库时携带自身 ID，新知识库会被
	// 原子加入该 key 的绑定集合。Session 主体为 nil。
	CallerAPIKeyID *uuid.UUID
}

// UpdateKnowledgeBaseBasicsInput contains the only mutable basic fields.
type UpdateKnowledgeBaseBasicsInput struct {
	WorkspaceID, KnowledgeBaseID uuid.UUID
	Name, Description            *string
	ActorRole                    value.WorkspaceRole
	Access                       value.ResourceAccess
	IsAPIKey                     bool
}

// KnowledgeBaseBasicsUpdater persists a typed basic-information patch.
type KnowledgeBaseBasicsUpdater interface {
	UpdateBasics(context.Context, UpdateKnowledgeBaseBasicsInput) error
}

// NewKnowledgeBaseService creates a KnowledgeBase service.
func NewKnowledgeBaseService(binder KnowledgeBaseModelBinder, stores ...KnowledgeBaseCreateStore) *KnowledgeBaseService {
	var store KnowledgeBaseCreateStore
	if len(stores) > 0 {
		store = stores[0]
	} else if candidate, ok := binder.(KnowledgeBaseCreateStore); ok {
		store = candidate
	}
	updater, _ := binder.(KnowledgeBaseBasicsUpdater)
	return &KnowledgeBaseService{binder: binder, store: store, updater: updater}
}

// UpdateBasics updates name and/or description without changing content or Generation state.
func (s *KnowledgeBaseService) UpdateBasics(ctx context.Context, input UpdateKnowledgeBaseBasicsInput) (*dto.KnowledgeBase, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: KnowledgeBase lineage 无效", domainerrors.ErrValidation)
	}
	if input.IsAPIKey {
		if input.Access.WorkspaceID != input.WorkspaceID || input.Access.Unrestricted || !input.Access.AllowsKnowledgeBase(input.KnowledgeBaseID) {
			return nil, domainerrors.ErrNotFound
		}
	} else if !input.ActorRole.AtLeast(value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	if input.Name == nil && input.Description == nil {
		return nil, fmt.Errorf("%w: 至少提供 name 或 description", domainerrors.ErrValidation)
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: 知识库名称不能为空", domainerrors.ErrValidation)
		}
		input.Name = &name
	}
	if s.updater == nil {
		return nil, fmt.Errorf("%w: KnowledgeBase basics updater 不能为空", domainerrors.ErrValidation)
	}
	if err := s.updater.UpdateBasics(ctx, input); err != nil {
		return nil, err
	}
	resolved, err := s.binder.GetResolved(ctx, input.WorkspaceID, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	return dto.KnowledgeBaseFromResolved(resolved), nil
}

// Create creates a KnowledgeBase only when its model is currently selectable.
func (s *KnowledgeBaseService) Create(ctx context.Context, input CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	kb, err := model.NewKnowledgeBase(input.WorkspaceID, input.Name, input.Description, input.EmbeddingModelID, input.ChunkingConfig, map[string]any{})
	if err != nil {
		return nil, err
	}
	if s.store != nil {
		resolvedModel, err := s.binder.ResolveSelectable(ctx, input.WorkspaceID, input.EmbeddingModelID)
		if err != nil {
			return nil, err
		}
		root, generation, err := buildInitialKnowledgeBaseState(kb, resolvedModel)
		if err != nil {
			return nil, err
		}
		if err := s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx KnowledgeBaseCreateTx) error {
			// 带 CallerAPIKeyID 时在同一事务内把新知识库原子加入调用 key 的绑定集合。
			return tx.CreateKnowledgeBaseRootGenerationAndBinding(txCtx, kb, root, generation, input.CallerAPIKeyID)
		}); err != nil {
			return nil, err
		}
		return dto.KnowledgeBaseFromResolved(&model.ResolvedKnowledgeBase{
			KnowledgeBase: kb, EmbeddingModel: resolvedModel,
			RetrievalConfig: generation.RetrievalConfig,
		}), nil
	}
	resolvedModel, err := s.binder.Create(ctx, kb)
	if err != nil {
		return nil, err
	}
	return dto.KnowledgeBaseFromResolved(&model.ResolvedKnowledgeBase{KnowledgeBase: kb, EmbeddingModel: resolvedModel}), nil
}

// Get gets a KnowledgeBase with its bound model summary.
func (s *KnowledgeBaseService) Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.KnowledgeBase, error) {
	if access.WorkspaceID == uuid.Nil || id == uuid.Nil {
		return nil, fmt.Errorf("%w: KnowledgeBase access 参数无效", domainerrors.ErrValidation)
	}
	if !access.Unrestricted && !access.AllowsKnowledgeBase(id) {
		return nil, domainerrors.ErrNotFound
	}
	resolved, err := s.binder.GetResolved(ctx, access.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	return dto.KnowledgeBaseFromResolved(resolved), nil
}

// List lists KnowledgeBases with bound model summaries.
func (s *KnowledgeBaseService) List(ctx context.Context, access value.ResourceAccess) ([]*dto.KnowledgeBase, error) {
	if access.WorkspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: KnowledgeBase access workspace 无效", domainerrors.ErrValidation)
	}
	var allowedIDs []uuid.UUID
	if !access.Unrestricted {
		// Keep a restricted empty binding as an explicit empty slice. A nil
		// allowedIDs value means "no SQL restriction" to the repository and
		// must never be produced for an API-key principal.
		allowedIDs = append([]uuid.UUID{}, access.AllowedKnowledgeBaseIDs...)
	}
	items, err := s.binder.ListResolved(ctx, access.WorkspaceID, allowedIDs)
	if err != nil {
		return nil, err
	}
	result := make([]*dto.KnowledgeBase, 0, len(items))
	for _, item := range items {
		result = append(result, dto.KnowledgeBaseFromResolved(item))
	}
	return result, nil
}
