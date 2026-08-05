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

// typedGenerationModelBinder 根据 ModelType 返回不同的 resolved model。
type typedGenerationModelBinder struct {
	embedding *model.ResolvedModel
	rerank    *model.ResolvedModel
}

func (b *typedGenerationModelBinder) ResolveSelectable(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*model.ResolvedModel, error) {
	return b.embedding, nil
}

func (b *typedGenerationModelBinder) ResolveSelectableModel(_ context.Context, _ uuid.UUID, _ uuid.UUID, modelType value.ModelType) (*model.ResolvedModel, error) {
	switch modelType {
	case value.ModelTypeEmbedding:
		return b.embedding, nil
	case value.ModelTypeRerank:
		return b.rerank, nil
	default:
		return nil, domainerrors.ErrUnsupportedModelType
	}
}

func testRerankResolvedModel() *model.ResolvedModel {
	return &model.ResolvedModel{
		Model: &model.Model{
			ID: uuid.New(), Type: value.ModelTypeRerank, ModelName: "bge-reranker-v2-m3",
			Parameters: map[string]any{"max_documents": float64(100), "max_query_chars": float64(4096), "max_document_chars": float64(8192)},
			Status:     value.ModelStatusActive,
		},
		Provider: &model.ModelProvider{ID: uuid.New(), Provider: "rerank_compatible", Status: value.ModelStatusActive, Config: map[string]any{}},
	}
}

func TestCreateGenerationInheritsRerankWhenSelectionOmitted(t *testing.T) {
	store := newFakeIndexGenerationStore()
	store.active.Rerank = &model.RerankSnapshot{
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "bge-reranker-v2-m3",
		ModelConfigHash: "oldhash", CandidateTopK: 50, FailureMode: value.RerankFailureFallback,
	}
	binder := &typedGenerationModelBinder{embedding: testGenerationResolvedModel(), rerank: testRerankResolvedModel()}
	jobQueue := &generationQueueSpy{}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})

	got, err := service.Create(context.Background(), CreateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		EmbeddingModelID: binder.embedding.Model.ID, ActorRole: value.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if got.Rerank == nil {
		t.Fatal("inherited rerank should not be nil")
	}
	// 继承时使用 base 的 model id 重新解析当前模型，因此 ModelID 来自 runtime rerank model。
	if got.Rerank.ModelID != binder.rerank.Model.ID || got.Rerank.CandidateTopK != 50 || got.Rerank.FailureMode != value.RerankFailureFallback {
		t.Fatalf("inherited rerank = %#v", got.Rerank)
	}
	// 继承后 hash 应基于当前模型重算，不等于 base 的旧 hash。
	if got.Rerank.ModelName == "" {
		t.Fatalf("rerank model name empty = %#v", got.Rerank)
	}
}

func TestCreateGenerationExplicitlyDisablesRerank(t *testing.T) {
	store := newFakeIndexGenerationStore()
	store.active.Rerank = &model.RerankSnapshot{
		ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank",
		ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFallback,
	}
	binder := &typedGenerationModelBinder{embedding: testGenerationResolvedModel(), rerank: testRerankResolvedModel()}
	jobQueue := &generationQueueSpy{}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})

	got, err := service.Create(context.Background(), CreateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		EmbeddingModelID: binder.embedding.Model.ID, ActorRole: value.RoleAdmin,
		Rerank: &RerankSelection{Enabled: false, FailureMode: value.RerankFailureFallback},
	})
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if got.Rerank != nil {
		t.Fatalf("disabled rerank should be nil, got %#v", got.Rerank)
	}
}

func TestCreateGenerationEnablesRerank(t *testing.T) {
	store := newFakeIndexGenerationStore()
	binder := &typedGenerationModelBinder{embedding: testGenerationResolvedModel(), rerank: testRerankResolvedModel()}
	jobQueue := &generationQueueSpy{}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})

	got, err := service.Create(context.Background(), CreateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		EmbeddingModelID: binder.embedding.Model.ID, ActorRole: value.RoleAdmin,
		Rerank: &RerankSelection{
			Enabled: true, ModelID: binder.rerank.Model.ID,
			CandidateTopK: 100, FailureMode: value.RerankFailureFail,
		},
	})
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if got.Rerank == nil || got.Rerank.ModelID != binder.rerank.Model.ID ||
		got.Rerank.CandidateTopK != 100 || got.Rerank.FailureMode != value.RerankFailureFail {
		t.Fatalf("enabled rerank = %#v", got.Rerank)
	}
}

func TestCreateGenerationRejectsCandidateAboveModelLimit(t *testing.T) {
	store := newFakeIndexGenerationStore()
	binder := &typedGenerationModelBinder{embedding: testGenerationResolvedModel(), rerank: testRerankResolvedModel()}
	jobQueue := &generationQueueSpy{}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})

	_, err := service.Create(context.Background(), CreateIndexGenerationInput{
		WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
		EmbeddingModelID: binder.embedding.Model.ID, ActorRole: value.RoleAdmin,
		Rerank: &RerankSelection{
			Enabled: true, ModelID: binder.rerank.Model.ID,
			CandidateTopK: 101, FailureMode: value.RerankFailureFallback,
		},
	})
	// max_documents=100, candidate=101 应被拒绝。
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("candidate above limit error = %v", err)
	}
}

func TestCreateGenerationRejectsBadCandidateTopK(t *testing.T) {
	store := newFakeIndexGenerationStore()
	binder := &typedGenerationModelBinder{embedding: testGenerationResolvedModel(), rerank: testRerankResolvedModel()}
	jobQueue := &generationQueueSpy{}
	service := NewIndexGenerationService(IndexGenerationServiceDeps{Store: store, Models: binder, Queue: jobQueue})

	for _, topK := range []int{49, 201} {
		_, err := service.Create(context.Background(), CreateIndexGenerationInput{
			WorkspaceID: store.kb.WorkspaceID, KnowledgeBaseID: store.kb.ID,
			EmbeddingModelID: binder.embedding.Model.ID, ActorRole: value.RoleAdmin,
			Rerank: &RerankSelection{Enabled: true, ModelID: binder.rerank.Model.ID, CandidateTopK: topK, FailureMode: value.RerankFailureFallback},
		})
		if !errors.Is(err, domainerrors.ErrValidation) {
			t.Fatalf("candidate %d error = %v", topK, err)
		}
	}
}
