package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
	rerankport "github.com/dajee/langhuan/internal/ports/rerank"
)

type fakeModelProviderRepository struct {
	items map[uuid.UUID]*model.ModelProvider
}

func newFakeModelProviderRepository() *fakeModelProviderRepository {
	return &fakeModelProviderRepository{items: map[uuid.UUID]*model.ModelProvider{}}
}

func (r *fakeModelProviderRepository) Create(_ context.Context, item *model.ModelProvider) error {
	r.items[item.ID] = cloneTestProvider(item)
	return nil
}
func (r *fakeModelProviderRepository) GetWorkspaceOwned(_ context.Context, workspaceID, id uuid.UUID) (*model.ModelProvider, error) {
	item := r.items[id]
	if item == nil || item.Scope != value.ModelScopeWorkspace || item.WorkspaceID == nil || *item.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return cloneTestProvider(item), nil
}
func (r *fakeModelProviderRepository) GetPlatform(_ context.Context, id uuid.UUID) (*model.ModelProvider, error) {
	item := r.items[id]
	if item == nil || item.Scope != value.ModelScopePlatform {
		return nil, domainerrors.ErrNotFound
	}
	return cloneTestProvider(item), nil
}
func (r *fakeModelProviderRepository) GetVisible(ctx context.Context, workspaceID, id uuid.UUID) (*model.ModelProvider, error) {
	if item, err := r.GetPlatform(ctx, id); err == nil {
		return item, nil
	}
	return r.GetWorkspaceOwned(ctx, workspaceID, id)
}
func (r *fakeModelProviderRepository) ListVisible(_ context.Context, workspaceID uuid.UUID) ([]*model.ModelProvider, error) {
	result := make([]*model.ModelProvider, 0)
	for _, item := range r.items {
		if item.Scope == value.ModelScopePlatform || (item.WorkspaceID != nil && *item.WorkspaceID == workspaceID) {
			result = append(result, cloneTestProvider(item))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
func (r *fakeModelProviderRepository) ListPlatform(context.Context) ([]*model.ModelProvider, error) {
	result := make([]*model.ModelProvider, 0)
	for _, item := range r.items {
		if item.Scope == value.ModelScopePlatform {
			result = append(result, cloneTestProvider(item))
		}
	}
	return result, nil
}
func (r *fakeModelProviderRepository) Update(_ context.Context, item *model.ModelProvider) error {
	if r.items[item.ID] == nil {
		return domainerrors.ErrNotFound
	}
	r.items[item.ID] = cloneTestProvider(item)
	return nil
}
func (r *fakeModelProviderRepository) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.items, id)
	return nil
}
func (r *fakeModelProviderRepository) CountModels(_ context.Context, providerID uuid.UUID) (int64, error) {
	return 0, nil
}
func (r *fakeModelProviderRepository) CountGenerationReferences(_ context.Context, _ uuid.UUID) (int64, error) {
	return 0, nil
}

type fakeModelRepository struct {
	items      map[uuid.UUID]*model.Model
	providers  *fakeModelProviderRepository
	references map[uuid.UUID]int64
}

func newFakeModelRepository(providers *fakeModelProviderRepository) *fakeModelRepository {
	return &fakeModelRepository{items: map[uuid.UUID]*model.Model{}, providers: providers, references: map[uuid.UUID]int64{}}
}
func (r *fakeModelRepository) Create(_ context.Context, item *model.Model) error {
	r.items[item.ID] = cloneTestModel(item)
	return nil
}
func (r *fakeModelRepository) resolved(id uuid.UUID) (*model.ResolvedModel, error) {
	item := r.items[id]
	if item == nil || r.providers.items[item.ProviderID] == nil {
		return nil, domainerrors.ErrNotFound
	}
	return &model.ResolvedModel{Model: cloneTestModel(item), Provider: cloneTestProvider(r.providers.items[item.ProviderID])}, nil
}
func (r *fakeModelRepository) GetWorkspaceOwned(_ context.Context, workspaceID, id uuid.UUID) (*model.ResolvedModel, error) {
	resolved, err := r.resolved(id)
	if err != nil || resolved.Provider.Scope != value.ModelScopeWorkspace || resolved.Provider.WorkspaceID == nil || *resolved.Provider.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return resolved, nil
}
func (r *fakeModelRepository) GetPlatform(_ context.Context, id uuid.UUID) (*model.ResolvedModel, error) {
	resolved, err := r.resolved(id)
	if err != nil || resolved.Provider.Scope != value.ModelScopePlatform {
		return nil, domainerrors.ErrNotFound
	}
	return resolved, nil
}
func (r *fakeModelRepository) GetVisible(ctx context.Context, workspaceID, id uuid.UUID) (*model.ResolvedModel, error) {
	if item, err := r.GetPlatform(ctx, id); err == nil {
		return item, nil
	}
	return r.GetWorkspaceOwned(ctx, workspaceID, id)
}
func (r *fakeModelRepository) ListByProviderVisible(_ context.Context, workspaceID, providerID uuid.UUID) ([]*model.Model, error) {
	provider := r.providers.items[providerID]
	if provider == nil || (provider.Scope != value.ModelScopePlatform && (provider.WorkspaceID == nil || *provider.WorkspaceID != workspaceID)) {
		return nil, domainerrors.ErrNotFound
	}
	return r.listProvider(providerID), nil
}
func (r *fakeModelRepository) ListByProviderPlatform(_ context.Context, providerID uuid.UUID) ([]*model.Model, error) {
	provider := r.providers.items[providerID]
	if provider == nil || provider.Scope != value.ModelScopePlatform {
		return nil, domainerrors.ErrNotFound
	}
	return r.listProvider(providerID), nil
}
func (r *fakeModelRepository) listProvider(providerID uuid.UUID) []*model.Model {
	result := make([]*model.Model, 0)
	for _, item := range r.items {
		if item.ProviderID == providerID {
			result = append(result, cloneTestModel(item))
		}
	}
	return result
}
func (r *fakeModelRepository) ListVisible(ctx context.Context, workspaceID uuid.UUID, modelType value.ModelType, activeOnly bool) ([]*model.ResolvedModel, error) {
	result := make([]*model.ResolvedModel, 0)
	for id, item := range r.items {
		resolved, err := r.GetVisible(ctx, workspaceID, id)
		if err == nil && item.Type == modelType && (!activeOnly || (item.Status == value.ModelStatusActive && resolved.Provider.Status == value.ModelStatusActive)) {
			result = append(result, resolved)
		}
	}
	return result, nil
}
func (r *fakeModelRepository) Update(_ context.Context, item *model.Model) error {
	current := r.items[item.ID]
	if current == nil {
		return domainerrors.ErrNotFound
	}
	semanticChanged := current.ModelName != item.ModelName ||
		(current.Dimensions == nil) != (item.Dimensions == nil) ||
		(current.Dimensions != nil && item.Dimensions != nil && *current.Dimensions != *item.Dimensions)
	if semanticChanged && r.references[item.ID] > 0 {
		return domainerrors.ErrImmutableModelField
	}
	r.items[item.ID] = cloneTestModel(item)
	return nil
}
func (r *fakeModelRepository) Delete(_ context.Context, id uuid.UUID) error {
	delete(r.items, id)
	return nil
}
func (r *fakeModelRepository) CountGenerationReferences(_ context.Context, id uuid.UUID) (int64, error) {
	return r.references[id], nil
}

type fakeCredentialCipher struct{}

func (fakeCredentialCipher) Encrypt(providerID uuid.UUID, plaintext []byte) ([]byte, error) {
	return []byte("cipher:" + providerID.String() + ":" + string(plaintext)), nil
}
func (fakeCredentialCipher) Decrypt(providerID uuid.UUID, ciphertext []byte) ([]byte, error) {
	prefix := []byte("cipher:" + providerID.String() + ":")
	if !bytes.HasPrefix(ciphertext, prefix) {
		return nil, domainerrors.ErrCredentialDecryption
	}
	return bytes.Clone(ciphertext[len(prefix):]), nil
}

type fakeEmbeddingFactory struct {
	client *recordingEmbeddingClient
}

func testProviderDescriptors(descriptors ...ProviderDescriptor) *ProviderDescriptorRegistry {
	registry, err := NewProviderDescriptorRegistry(descriptors...)
	if err != nil {
		panic(err)
	}
	return registry
}

func (f *fakeEmbeddingFactory) Provider() string           { return "openai" }
func (f *fakeEmbeddingFactory) CredentialFields() []string { return []string{"api_key"} }
func (f *fakeEmbeddingFactory) DecodeProvider(input embeddingport.ProviderDecodeInput) (map[string]any, []byte, error) {
	var config map[string]any
	if err := json.Unmarshal(input.Config, &config); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", domainerrors.ErrInvalidProviderConfig, err)
	}
	if len(input.Credentials) == 0 {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	return config, bytes.Clone(input.Credentials), nil
}
func (f *fakeEmbeddingFactory) DecodeModel(input embeddingport.ModelDecodeInput) (map[string]any, error) {
	if !value.IsSupportedEmbeddingDimension(input.Dimensions) {
		return nil, domainerrors.ErrUnsupportedEmbeddingDimension
	}
	var parameters map[string]any
	if err := json.Unmarshal(input.Parameters, &parameters); err != nil {
		return nil, err
	}
	return parameters, nil
}
func (f *fakeEmbeddingFactory) NewClient(_ context.Context, _ embeddingport.ClientInput) (embeddingport.EmbeddingClient, error) {
	return f.client, nil
}

type fakeFactoryRegistry struct{ factory embeddingport.Factory }

func (r fakeFactoryRegistry) Factory(modelType value.ModelType, provider string) (embeddingport.Factory, error) {
	if modelType != value.ModelTypeEmbedding {
		return nil, domainerrors.ErrUnsupportedModelType
	}
	if r.factory == nil || provider != r.factory.Provider() {
		return nil, domainerrors.ErrUnsupportedProvider
	}
	return r.factory, nil
}

// fakeRerankFactory 是测试用的 rerank Factory。
type fakeRerankFactory struct {
	provider string
	client   rerankport.Client
}

func (f *fakeRerankFactory) Provider() string           { return f.provider }
func (f *fakeRerankFactory) CredentialFields() []string { return []string{"api_key"} }
func (f *fakeRerankFactory) DecodeProvider(input rerankport.ProviderDecodeInput) (map[string]any, []byte, error) {
	var config map[string]any
	if err := json.Unmarshal(input.Config, &config); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", domainerrors.ErrInvalidProviderConfig, err)
	}
	if len(input.Credentials) == 0 {
		return nil, nil, domainerrors.ErrCredentialsRequired
	}
	return config, bytes.Clone(input.Credentials), nil
}
func (f *fakeRerankFactory) DecodeModel(input rerankport.ModelDecodeInput) (map[string]any, error) {
	if input.ModelName == "" {
		return nil, fmt.Errorf("%w: model_name 不能为空", domainerrors.ErrInvalidProviderConfig)
	}
	var parameters map[string]any
	if err := json.Unmarshal(input.Parameters, &parameters); err != nil {
		return nil, err
	}
	if parameters == nil {
		parameters = map[string]any{}
	}
	// 测试默认补全最小合法值。
	if _, ok := parameters["max_documents"]; !ok {
		parameters["max_documents"] = float64(100)
	}
	if _, ok := parameters["max_query_chars"]; !ok {
		parameters["max_query_chars"] = float64(4096)
	}
	if _, ok := parameters["max_document_chars"]; !ok {
		parameters["max_document_chars"] = float64(8192)
	}
	return parameters, nil
}
func (f *fakeRerankFactory) NewClient(_ context.Context, _ rerankport.ClientInput) (rerankport.Client, error) {
	return f.client, nil
}

type fakeRerankFactoryRegistry struct{ factory *fakeRerankFactory }

func (r fakeRerankFactoryRegistry) Factory(provider string) (rerankport.Factory, error) {
	if r.factory == nil || provider != r.factory.Provider() {
		return nil, domainerrors.ErrUnsupportedProvider
	}
	return r.factory, nil
}

type recordingEmbeddingClient struct {
	dimension int
	input     embeddingport.EmbedInput
}

func (c *recordingEmbeddingClient) Embed(_ context.Context, input embeddingport.EmbedInput) (*embeddingport.EmbedResult, error) {
	c.input = input
	return &embeddingport.EmbedResult{Vectors: [][]float32{make([]float32, c.dimension)}}, nil
}
func (c *recordingEmbeddingClient) Dimension() int { return c.dimension }

func cloneTestProvider(input *model.ModelProvider) *model.ModelProvider {
	cloned := *input
	cloned.Config = make(map[string]any, len(input.Config))
	for key, value := range input.Config {
		cloned.Config[key] = value
	}
	cloned.CredentialsCiphertext = bytes.Clone(input.CredentialsCiphertext)
	if input.WorkspaceID != nil {
		workspaceID := *input.WorkspaceID
		cloned.WorkspaceID = &workspaceID
	}
	return &cloned
}

func cloneTestModel(input *model.Model) *model.Model {
	cloned := *input
	cloned.Parameters = make(map[string]any, len(input.Parameters))
	for key, value := range input.Parameters {
		cloned.Parameters[key] = value
	}
	if input.Dimensions != nil {
		dimensions := *input.Dimensions
		cloned.Dimensions = &dimensions
	}
	return &cloned
}

// intPtr 返回 int 值的指针，便于构造 *int 类型的测试输入。
func intPtr(value int) *int { return &value }
