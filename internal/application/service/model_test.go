package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestModelServiceOnlyCreatesEmbeddingAndProtectsReferencedSemantics(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "openai", "OpenAI", "", "openai", map[string]any{}, []byte("cipher"), actorID)
	if err != nil {
		t.Fatal(err)
	}
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	embeddingFactory := &fakeEmbeddingFactory{}
	service := NewModelService(providers, models, fakeFactoryRegistry{factory: embeddingFactory}, fakeRerankFactoryRegistry{}, testProviderDescriptors(EmbeddingProviderDescriptor(embeddingFactory)))

	if _, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "chat", Type: value.ModelTypeLLM,
		ModelName: "gpt", Dimensions: intPtr(1024), Parameters: json.RawMessage(`{}`),
	}); !errors.Is(err, domainerrors.ErrUnsupportedModelType) {
		t.Fatalf("LLM create error = %v", err)
	}
	created, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "embed", DisplayName: "Embedding", Type: value.ModelTypeEmbedding,
		ModelName: "text-embedding", Dimensions: intPtr(1024), Parameters: json.RawMessage(`{"batch_size":32}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	models.references[created.ID] = 1
	changedName := "text-embedding-v2"
	if _, err := service.UpdateWorkspace(context.Background(), workspaceID, created.ID, UpdateModelInput{ModelName: &changedName}); !errors.Is(err, domainerrors.ErrImmutableModelField) {
		t.Fatalf("referenced model update error = %v", err)
	}
	parameters := json.RawMessage(`{"batch_size":16}`)
	if _, err := service.UpdateWorkspace(context.Background(), workspaceID, created.ID, UpdateModelInput{Parameters: &parameters}); err != nil {
		t.Fatalf("parameter update error = %v", err)
	}
	if err := service.DeleteWorkspace(context.Background(), workspaceID, created.ID); !errors.Is(err, domainerrors.ErrModelInUse) {
		t.Fatalf("delete error = %v", err)
	}
}

func TestModelDTOAvailabilityIncludesProviderStatus(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, _ := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "openai", "OpenAI", "", "openai", map[string]any{}, []byte("cipher"), actorID)
	provider.Status = value.ModelStatusDisabled
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	dimension := 1024
	item, _ := model.NewModel(provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding, "text-embedding", &dimension, map[string]any{}, actorID)
	models.items[item.ID] = item
	embeddingFactory := &fakeEmbeddingFactory{}
	service := NewModelService(providers, models, fakeFactoryRegistry{factory: embeddingFactory}, fakeRerankFactoryRegistry{}, testProviderDescriptors(EmbeddingProviderDescriptor(embeddingFactory)))
	got, err := service.GetWorkspace(context.Background(), workspaceID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Available {
		t.Fatal("model under disabled provider must be unavailable")
	}
}

func TestModelServiceCreatesRerankWithoutDimensions(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "rerank_compatible", "Rerank Compatible", "", "rerank_compatible", map[string]any{}, []byte("cipher"), actorID)
	if err != nil {
		t.Fatal(err)
	}
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	rerankRegistry := fakeRerankFactoryRegistry{factory: &fakeRerankFactory{provider: "rerank_compatible"}}
	service := NewModelService(providers, models, fakeFactoryRegistry{}, rerankRegistry, testProviderDescriptors(RerankProviderDescriptor(rerankRegistry.factory)))

	// Rerank 传 dimensions 被拒绝。
	if _, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "rerank", DisplayName: "Rerank",
		Type: value.ModelTypeRerank, ModelName: "bge-reranker", Dimensions: intPtr(1024),
		Parameters: json.RawMessage(`{}`),
	}); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("rerank with dimensions error = %v", err)
	}

	// Provider 未声明 Embedding capability 时直接拒绝。
	if _, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "embed", Type: value.ModelTypeEmbedding,
		ModelName: "text-embedding", Parameters: json.RawMessage(`{}`),
	}); !errors.Is(err, domainerrors.ErrUnsupportedModelType) {
		t.Fatalf("embedding without dimensions error = %v", err)
	}

	// LLM 继续拒绝。
	if _, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "llm", Type: value.ModelTypeLLM,
		ModelName: "gpt", Parameters: json.RawMessage(`{}`),
	}); !errors.Is(err, domainerrors.ErrUnsupportedModelType) {
		t.Fatalf("llm error = %v", err)
	}

	// Rerank 正常创建且 dimensions 为 nil。
	got, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "bge_reranker", DisplayName: "BGE Reranker",
		Type: value.ModelTypeRerank, ModelName: "BAAI/bge-reranker-v2-m3",
		Parameters: json.RawMessage(`{"max_documents":100,"max_query_chars":4096,"max_document_chars":8192}`),
	})
	if err != nil {
		t.Fatalf("create rerank error = %v", err)
	}
	if got.Type != value.ModelTypeRerank || got.Dimensions != nil {
		t.Fatalf("rerank model = %#v", got)
	}
}

func TestModelServiceRejectsTypeOutsideProviderCapabilities(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "openai", "OpenAI", "", "openai", map[string]any{}, []byte("cipher"), actorID)
	if err != nil {
		t.Fatal(err)
	}
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	descriptors, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key:          "openai",
		Capabilities: []value.ProviderCapability{value.CapabilityEmbedding},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := NewModelService(providers, models, fakeFactoryRegistry{factory: &fakeEmbeddingFactory{}}, fakeRerankFactoryRegistry{factory: &fakeRerankFactory{provider: "openai"}}, descriptors)

	_, err = service.CreateWorkspace(context.Background(), workspaceID, CreateModelInput{
		ProviderID: provider.ID, ActorID: actorID, Name: "rerank", DisplayName: "Rerank",
		Type: value.ModelTypeRerank, ModelName: "bge-reranker", Parameters: json.RawMessage(`{}`),
	})
	if !errors.Is(err, domainerrors.ErrUnsupportedModelType) {
		t.Fatalf("error = %v", err)
	}
}
