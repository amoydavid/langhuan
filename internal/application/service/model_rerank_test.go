package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

// recordingRerankClient 记录调用输入并返回固定结果。
type recordingRerankClient struct {
	input rerankport.RerankInput
}

func (c *recordingRerankClient) Rerank(_ context.Context, input rerankport.RerankInput) (*rerankport.RerankResult, error) {
	c.input = input
	// 按输入顺序返回有限分数，连接测试只校验数量/唯一/有限。
	items := make([]rerankport.RerankItem, 0, len(input.Documents))
	for i, document := range input.Documents {
		items = append(items, rerankport.RerankItem{DocumentID: document.ID, Score: float64(len(input.Documents)-i) / 10})
	}
	return &rerankport.RerankResult{Items: items}, nil
}

func TestConnectionTestRerankReturnsResultCount(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "rerank_compatible", "Rerank Compatible", "", "rerank_compatible", map[string]any{}, nil, actorID)
	if err != nil {
		t.Fatal(err)
	}
	cipher := fakeCredentialCipher{}
	provider.CredentialsCiphertext, _ = cipher.Encrypt(provider.ID, []byte(`{"api_key":"secret"}`))
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	rerankModel, err := model.NewModel(provider.ID, "bge_reranker", "BGE Reranker", "", value.ModelTypeRerank, "bge-reranker-v2-m3", nil, map[string]any{
		"max_documents":      float64(100),
		"max_query_chars":    float64(4096),
		"max_document_chars": float64(8192),
	}, actorID)
	if err != nil {
		t.Fatal(err)
	}
	models.items[rerankModel.ID] = rerankModel

	recordingClient := &recordingRerankClient{}
	rerankRegistry := fakeRerankFactoryRegistry{factory: &fakeRerankFactory{provider: "rerank_compatible", client: recordingClient}}
	service := NewModelConnectionTestService(models, cipher, fakeFactoryRegistry{}, rerankRegistry)

	got, err := service.TestWorkspace(context.Background(), workspaceID, rerankModel.ID)
	if err != nil {
		t.Fatalf("rerank test error = %v", err)
	}
	if !got.OK || got.Type != value.ModelTypeRerank || got.Dimensions != nil || got.ResultCount == nil || *got.ResultCount != 2 {
		t.Fatalf("rerank test result = %#v", got)
	}
	// 验证发送了固定测试 query 与 2 个文档。
	if recordingClient.input.Query != RerankConnectionTestQuery || len(recordingClient.input.Documents) != 2 {
		t.Fatalf("rerank call input = %#v", recordingClient.input)
	}
}

func TestConnectionTestRoutesEmbeddingModelToEmbeddingTest(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, _ := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "openai", "OpenAI", "", "openai", map[string]any{}, nil, actorID)
	cipher := fakeCredentialCipher{}
	provider.CredentialsCiphertext, _ = cipher.Encrypt(provider.ID, []byte(`{"api_key":"secret"}`))
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	dimension := 1024
	embeddingModel, _ := model.NewModel(provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding, "text-embedding", &dimension, map[string]any{"batch_size": float64(32)}, actorID)
	models.items[embeddingModel.ID] = embeddingModel

	rerankRegistry := fakeRerankFactoryRegistry{}
	service := NewModelConnectionTestService(models, cipher, fakeFactoryRegistry{factory: &fakeEmbeddingFactory{client: &recordingEmbeddingClient{dimension: 1024}}}, rerankRegistry)

	got, err := service.TestWorkspace(context.Background(), workspaceID, embeddingModel.ID)
	if err != nil {
		t.Fatalf("embedding test should succeed, got error = %v", err)
	}
	if got.Type != value.ModelTypeEmbedding || got.ResultCount != nil {
		t.Fatalf("embedding test result = %#v", got)
	}
}

func TestRerankResolverRejectsEmbeddingModel(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, _ := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "openai", "OpenAI", "", "openai", map[string]any{}, nil, actorID)
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	dimension := 1024
	embeddingModel, _ := model.NewModel(provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding, "text-embedding", &dimension, map[string]any{}, actorID)
	models.items[embeddingModel.ID] = embeddingModel

	rerankRegistry := fakeRerankFactoryRegistry{}
	resolver := NewRerankClientResolver(models, fakeCredentialCipher{}, rerankRegistry)
	_, err := resolver.Resolve(context.Background(), workspaceID, embeddingModel.ID)
	if !errors.Is(err, domainerrors.ErrUnsupportedModelType) {
		t.Fatalf("embedding via rerank resolver error = %v", err)
	}
}

func TestRerankResolverBuildsVisibleActiveClient(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "rerank_compatible", "Rerank Compatible", "", "rerank_compatible", map[string]any{}, nil, actorID)
	if err != nil {
		t.Fatal(err)
	}
	cipher := fakeCredentialCipher{}
	provider.CredentialsCiphertext, _ = cipher.Encrypt(provider.ID, []byte(`{"api_key":"secret"}`))
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	rerankModel, err := model.NewModel(provider.ID, "bge_reranker", "BGE Reranker", "", value.ModelTypeRerank, "bge-reranker-v2-m3", nil, map[string]any{
		"max_documents":      float64(100),
		"max_query_chars":    float64(4096),
		"max_document_chars": float64(8192),
	}, actorID)
	if err != nil {
		t.Fatal(err)
	}
	models.items[rerankModel.ID] = rerankModel

	recordingClient := &recordingRerankClient{}
	rerankRegistry := fakeRerankFactoryRegistry{factory: &fakeRerankFactory{provider: "rerank_compatible", client: recordingClient}}
	resolver := NewRerankClientResolver(models, cipher, rerankRegistry)

	got, err := resolver.Resolve(context.Background(), workspaceID, rerankModel.ID)
	if err != nil {
		t.Fatalf("resolve error = %v", err)
	}
	if got.Client == nil || got.ModelID != rerankModel.ID || got.MaxDocuments != 100 || got.MaxQueryChars != 4096 || got.MaxDocumentChars != 8192 {
		t.Fatalf("resolved client = %#v", got)
	}
}

func TestModelServiceListSelectableFiltersByType(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, _ := model.NewModelProvider(value.ModelScopePlatform, nil, "openai", "OpenAI", "", "openai", map[string]any{}, nil, actorID)
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	dimension := 1024
	embeddingModel, _ := model.NewModel(provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding, "text-embedding", &dimension, map[string]any{}, actorID)
	rerankModel, _ := model.NewModel(provider.ID, "rerank", "Rerank", "", value.ModelTypeRerank, "bge-reranker", nil, map[string]any{}, actorID)
	models.items[embeddingModel.ID] = embeddingModel
	models.items[rerankModel.ID] = rerankModel

	rerankRegistry := fakeRerankFactoryRegistry{}
	service := NewModelService(providers, models, fakeFactoryRegistry{factory: &fakeEmbeddingFactory{}}, rerankRegistry)

	rerankOnly, err := service.ListSelectableWorkspace(context.Background(), workspaceID, value.ModelTypeRerank, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rerankOnly) != 1 || rerankOnly[0].Type != value.ModelTypeRerank {
		t.Fatalf("rerank selectable = %#v", rerankOnly)
	}
	embeddingOnly, err := service.ListSelectableWorkspace(context.Background(), workspaceID, value.ModelTypeEmbedding, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(embeddingOnly) != 1 || embeddingOnly[0].Type != value.ModelTypeEmbedding {
		t.Fatalf("embedding selectable = %#v", embeddingOnly)
	}
}

func TestProviderFactoryResolverExposesCapabilities(t *testing.T) {
	t.Parallel()
	embeddingReg := fakeFactoryRegistry{factory: &fakeEmbeddingFactory{}}
	rerankReg := fakeRerankFactoryRegistry{factory: &fakeRerankFactory{provider: "rerank_compatible"}}
	resolver := NewProviderFactoryResolver(embeddingReg, rerankReg, nil, "openai", "rerank_compatible")

	options := resolver.ProviderOptions()
	if len(options) != 2 {
		t.Fatalf("options = %#v", options)
	}
	// 按 key 排序：openai 在前，rerank_compatible 在后。
	if options[0].Key != "openai" || len(options[0].Capabilities) != 1 || options[0].Capabilities[0] != value.CapabilityEmbedding {
		t.Fatalf("openai option = %#v", options[0])
	}
	if options[1].Key != "rerank_compatible" || len(options[1].Capabilities) != 1 || options[1].Capabilities[0] != value.CapabilityRerank {
		t.Fatalf("rerank option = %#v", options[1])
	}
}
