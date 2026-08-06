package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type fakeKnowledgeBaseRepository struct {
	items             map[uuid.UUID]*model.KnowledgeBase
	models            map[uuid.UUID]*model.ResolvedModel
	bindErr           error
	createdRoot       *model.FileTreeNode
	createdGeneration *model.IndexGeneration
	updateInput       UpdateKnowledgeBaseBasicsInput
	boundAPIKeyID     *uuid.UUID
	failBinding       bool
}

func (r *fakeKnowledgeBaseRepository) UpdateBasics(_ context.Context, input UpdateKnowledgeBaseBasicsInput) error {
	r.updateInput = input
	kb := r.items[input.KnowledgeBaseID]
	if kb == nil || kb.WorkspaceID != input.WorkspaceID {
		return domainerrors.ErrNotFound
	}
	if input.Name != nil {
		kb.Name = *input.Name
	}
	if input.Description != nil {
		kb.Description = *input.Description
	}
	return nil
}

func newFakeKnowledgeBaseRepository() *fakeKnowledgeBaseRepository {
	return &fakeKnowledgeBaseRepository{items: map[uuid.UUID]*model.KnowledgeBase{}, models: map[uuid.UUID]*model.ResolvedModel{}}
}

func (r *fakeKnowledgeBaseRepository) Create(_ context.Context, kb *model.KnowledgeBase) (*model.ResolvedModel, error) {
	if r.bindErr != nil {
		return nil, r.bindErr
	}
	resolved := r.models[kb.EmbeddingModelID]
	if resolved == nil {
		return nil, domainerrors.ErrModelNotVisible
	}
	r.items[kb.ID] = kb
	return resolved, nil
}

func (r *fakeKnowledgeBaseRepository) ResolveSelectable(_ context.Context, workspaceID, modelID uuid.UUID) (*model.ResolvedModel, error) {
	if r.bindErr != nil {
		return nil, r.bindErr
	}
	resolved := r.models[modelID]
	if resolved == nil || (resolved.Provider.WorkspaceID != nil && *resolved.Provider.WorkspaceID != workspaceID) {
		return nil, domainerrors.ErrModelNotVisible
	}
	return resolved, nil
}

func (r *fakeKnowledgeBaseRepository) WithinWorkspace(ctx context.Context, _ uuid.UUID, fn func(context.Context, KnowledgeBaseCreateTx) error) error {
	return fn(ctx, r)
}

func (r *fakeKnowledgeBaseRepository) CreateKnowledgeBaseRootAndGeneration(
	_ context.Context,
	kb *model.KnowledgeBase,
	root *model.FileTreeNode,
	generation *model.IndexGeneration,
) error {
	r.items[kb.ID] = kb
	r.createdRoot = root
	r.createdGeneration = generation
	return nil
}

// CreateKnowledgeBaseRootGenerationAndBinding 在测试替身中记录绑定意图，并模拟
// 绑定失败回滚（failBinding 时拒绝写知识库）。
func (r *fakeKnowledgeBaseRepository) CreateKnowledgeBaseRootGenerationAndBinding(
	_ context.Context,
	kb *model.KnowledgeBase,
	root *model.FileTreeNode,
	generation *model.IndexGeneration,
	bindAPIKeyID *uuid.UUID,
) error {
	if r.failBinding {
		return domainerrors.ErrConflict
	}
	r.items[kb.ID] = kb
	r.createdRoot = root
	r.createdGeneration = generation
	r.boundAPIKeyID = bindAPIKeyID
	return nil
}

func (r *fakeKnowledgeBaseRepository) Get(_ context.Context, workspaceID, id uuid.UUID) (*model.KnowledgeBase, error) {
	kb := r.items[id]
	if kb == nil || kb.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return kb, nil
}

func (r *fakeKnowledgeBaseRepository) GetResolved(ctx context.Context, workspaceID, id uuid.UUID) (*model.ResolvedKnowledgeBase, error) {
	kb, err := r.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return &model.ResolvedKnowledgeBase{KnowledgeBase: kb, EmbeddingModel: r.models[kb.EmbeddingModelID]}, nil
}

func (r *fakeKnowledgeBaseRepository) ListResolved(_ context.Context, workspaceID uuid.UUID, allowedIDs []uuid.UUID) ([]*model.ResolvedKnowledgeBase, error) {
	result := make([]*model.ResolvedKnowledgeBase, 0)
	for _, kb := range r.items {
		allowed := allowedIDs == nil
		for _, allowedID := range allowedIDs {
			if allowedID == kb.ID {
				allowed = true
				break
			}
		}
		if kb.WorkspaceID == workspaceID && allowed {
			result = append(result, &model.ResolvedKnowledgeBase{KnowledgeBase: kb, EmbeddingModel: r.models[kb.EmbeddingModelID]})
		}
	}
	return result, nil
}

func TestKnowledgeBaseCreateRequiresSelectableEmbeddingModel(t *testing.T) {
	t.Parallel()
	repository := newFakeKnowledgeBaseRepository()
	repository.bindErr = domainerrors.ErrModelDisabled
	service := NewKnowledgeBaseService(repository)
	_, err := service.Create(context.Background(), CreateKnowledgeBaseInput{WorkspaceID: uuid.New(), Name: "kb", EmbeddingModelID: uuid.New()})
	if !errors.Is(err, domainerrors.ErrModelDisabled) {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateKnowledgeBaseCreatesActiveEmptyGeneration(t *testing.T) {
	t.Parallel()

	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	service := NewKnowledgeBaseService(repository, repository)

	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "kb", Description: "desc", EmbeddingModelID: resolved.Model.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	kb := repository.items[created.ID]
	root := repository.createdRoot
	generation := repository.createdGeneration
	if kb == nil || kb.ActiveIndexGenerationID == nil || kb.FileTreeRootID == uuid.Nil {
		t.Fatalf("knowledge base pointers = %#v", kb)
	}
	if root == nil || root.ID != kb.FileTreeRootID || root.NodeType != value.FileTreeNodeRoot || root.ParentID != nil || root.DocumentID != nil {
		t.Fatalf("root = %#v", root)
	}
	if generation == nil || generation.ID != *kb.ActiveIndexGenerationID || generation.Status != value.IndexGenerationReady || generation.ChunkerVersion != value.StandardChunkerVersion {
		t.Fatalf("generation = %#v", generation)
	}
	if generation.ChunkingConfig["strategy"] != "auto" || generation.ChunkingConfig["enable_parent_child"] != true ||
		generation.ChunkingConfig["parent_chunk_size"] != 4096 || generation.ChunkingConfig["child_chunk_size"] != 384 {
		t.Fatalf("generation chunking config = %#v", generation.ChunkingConfig)
	}
	if created.ActiveIndexGenerationID == nil || *created.ActiveIndexGenerationID != generation.ID || created.FileTreeRootID != root.ID {
		t.Fatalf("created DTO pointers = %#v", created)
	}
	if created.RetrievalConfig["fts_config"] != "zhparser" || created.RetrievalConfig["final_top_k"] != 10 {
		t.Fatalf("created DTO retrieval config = %#v", created.RetrievalConfig)
	}
	if generation.SourceContentVersion != 0 || generation.IndexedContentVersion != 0 || generation.DocumentCount != 0 || generation.ChunkCount != 0 || generation.IndexedCount != 0 {
		t.Fatalf("initial generation counters = %#v", generation)
	}
}

func TestKnowledgeBaseServiceCreatesAndReturnsModelSummary(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	first := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[first.Model.ID] = first
	service := NewKnowledgeBaseService(repository)
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{WorkspaceID: workspaceID, Name: "kb", Description: "desc", EmbeddingModelID: first.Model.ID})
	if err != nil {
		t.Fatal(err)
	}
	if created.EmbeddingModelID != first.Model.ID || created.EmbeddingModel.Dimensions != 1024 || !created.EmbeddingModel.Available {
		t.Fatalf("created = %#v", created)
	}
}

// TestKnowledgeBaseServiceCreateAddsBindingForAPIKey 验证 API Key 主体创建知识库时，
// 新知识库与绑定在同一事务内原子提交；绑定失败时整体回滚。
func TestKnowledgeBaseServiceCreateAddsBindingForAPIKey(t *testing.T) {
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	service := NewKnowledgeBaseService(repository)

	keyID := uuid.New()
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "新库", EmbeddingModelID: resolved.Model.ID, CallerAPIKeyID: &keyID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.boundAPIKeyID == nil || *repository.boundAPIKeyID != keyID {
		t.Fatalf("bound API key = %v, want %s", repository.boundAPIKeyID, keyID)
	}
	if repository.items[created.ID] == nil {
		t.Fatal("知识库未持久化")
	}

	// 绑定失败时整体回滚：知识库不应落库。
	repository.failBinding = true
	_, err = service.Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "回滚库", EmbeddingModelID: resolved.Model.ID, CallerAPIKeyID: &keyID,
	})
	if err == nil {
		t.Fatal("绑定失败应回滚整个创建")
	}
}

func TestKnowledgeBaseServiceGetRejectsCrossWorkspace(t *testing.T) {
	t.Parallel()
	repository := newFakeKnowledgeBaseRepository()
	workspaceID := uuid.New()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	service := NewKnowledgeBaseService(repository)
	created, err := service.Create(context.Background(), CreateKnowledgeBaseInput{WorkspaceID: workspaceID, Name: "kb", EmbeddingModelID: resolved.Model.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), value.ResourceAccess{WorkspaceID: uuid.New(), Unrestricted: true}, created.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestKnowledgeBaseServiceListAndGetRestrictAPIKeyToBoundKnowledgeBases(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	modelA := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	modelB := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[modelA.Model.ID] = modelA
	repository.models[modelB.Model.ID] = modelB
	kbA := &model.KnowledgeBase{ID: uuid.New(), WorkspaceID: workspaceID, Name: "A", EmbeddingModelID: modelA.Model.ID}
	kbB := &model.KnowledgeBase{ID: uuid.New(), WorkspaceID: workspaceID, Name: "B", EmbeddingModelID: modelB.Model.ID}
	repository.items[kbA.ID] = kbA
	repository.items[kbB.ID] = kbB
	svc := NewKnowledgeBaseService(repository)
	access := value.NewAPIKeyAuthContext(uuid.New(), workspaceID, []value.APIScope{value.ScopeKnowledgeBasesRead}, []uuid.UUID{kbB.ID})

	items, err := svc.List(context.Background(), access.ResourceAccess())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != kbB.ID {
		t.Fatalf("restricted list = %#v, want only %s", items, kbB.ID)
	}
	if _, err := svc.Get(context.Background(), access.ResourceAccess(), kbA.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("unbound get error = %v, want not found", err)
	}
}

func TestKnowledgeBaseServiceListWithEmptyRestrictedBindingReturnsNoKnowledgeBases(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	modelItem := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[modelItem.Model.ID] = modelItem
	kb := &model.KnowledgeBase{ID: uuid.New(), WorkspaceID: workspaceID, Name: "不可见", EmbeddingModelID: modelItem.Model.ID}
	repository.items[kb.ID] = kb

	service := NewKnowledgeBaseService(repository)
	access := value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{}}
	items, err := service.List(context.Background(), access)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("empty restricted list = %#v, want no knowledge bases", items)
	}
}

func TestUpdateKnowledgeBaseBasicsRequiresAdminAndTypedFields(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	repository := newFakeKnowledgeBaseRepository()
	resolved := fakeResolvedEmbeddingModel(t, value.ModelScopePlatform, nil, value.ModelStatusActive, value.ModelStatusActive)
	repository.models[resolved.Model.ID] = resolved
	created, err := NewKnowledgeBaseService(repository).Create(context.Background(), CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "原名称", Description: "原描述", EmbeddingModelID: resolved.Model.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewKnowledgeBaseService(repository)

	name := " 新名称 "
	updated, err := service.UpdateBasics(context.Background(), UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: created.ID, Name: &name, ActorRole: value.RoleAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "新名称" || updated.Description != "原描述" {
		t.Fatalf("updated = %#v", updated)
	}
	if repository.updateInput.Name == nil || *repository.updateInput.Name != "新名称" || repository.updateInput.Description != nil {
		t.Fatalf("repository input = %#v", repository.updateInput)
	}
	apiName := "API 名称"
	apiAccess := value.NewAPIKeyAuthContext(uuid.New(), workspaceID, []value.APIScope{value.ScopeKnowledgeBasesWrite}, []uuid.UUID{created.ID}).ResourceAccess()
	if _, err := service.UpdateBasics(context.Background(), UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: created.ID, Name: &apiName,
		Access: apiAccess, IsAPIKey: true,
	}); err != nil {
		t.Fatalf("bound API key patch error = %v", err)
	}
	unboundAccess := value.NewAPIKeyAuthContext(uuid.New(), workspaceID, []value.APIScope{value.ScopeKnowledgeBasesWrite}, []uuid.UUID{uuid.New()}).ResourceAccess()
	if _, err := service.UpdateBasics(context.Background(), UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: created.ID, Name: &apiName,
		Access: unboundAccess, IsAPIKey: true,
	}); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("unbound API key patch error = %v, want not found", err)
	}

	for _, input := range []UpdateKnowledgeBaseBasicsInput{
		{WorkspaceID: workspaceID, KnowledgeBaseID: created.ID, Name: &name, ActorRole: value.RoleMember},
		{WorkspaceID: workspaceID, KnowledgeBaseID: created.ID, ActorRole: value.RoleAdmin},
		func() UpdateKnowledgeBaseBasicsInput {
			blank := "  "
			return UpdateKnowledgeBaseBasicsInput{WorkspaceID: workspaceID, KnowledgeBaseID: created.ID, Name: &blank, ActorRole: value.RoleOwner}
		}(),
	} {
		_, err := service.UpdateBasics(context.Background(), input)
		if input.ActorRole == value.RoleMember {
			if !errors.Is(err, domainerrors.ErrForbidden) {
				t.Fatalf("member error = %v, want forbidden", err)
			}
		} else if !errors.Is(err, domainerrors.ErrValidation) {
			t.Fatalf("input = %#v error = %v, want validation", input, err)
		}
	}
}

func fakeResolvedEmbeddingModel(t *testing.T, scope value.ModelScope, workspaceID *uuid.UUID, providerStatus, modelStatus value.ModelStatus) *model.ResolvedModel {
	t.Helper()
	provider, err := model.NewModelProvider(scope, workspaceID, "openai", "OpenAI", "", "openai", map[string]any{}, []byte("cipher"), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	provider.Status = providerStatus
	dimensions := 1024
	item, err := model.NewModel(provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding, "text-embedding", &dimensions, map[string]any{}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	item.Status = modelStatus
	return &model.ResolvedModel{Model: item, Provider: provider}
}
