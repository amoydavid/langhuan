package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	modelcatalogport "github.com/dajee/langhuan/internal/ports/modelcatalog"
)

type recordingModelCatalog struct {
	input              modelcatalogport.Input
	credentialSnapshot string
	items              []modelcatalogport.Item
}

func (c *recordingModelCatalog) ListModels(_ context.Context, input modelcatalogport.Input) ([]modelcatalogport.Item, error) {
	c.input = input
	c.credentialSnapshot = string(input.CredentialsJSON)
	return c.items, nil
}

func newCatalogService(t *testing.T, catalog modelcatalogport.Catalog) (*ModelProviderService, *fakeModelProviderRepository, uuid.UUID, uuid.UUID) {
	t.Helper()
	repository := newFakeModelProviderRepository()
	registry, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key: "catalog-provider", Capabilities: []value.ProviderCapability{value.CapabilityEmbedding}, ModelCatalog: catalog,
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{Config: map[string]any{"base_url": "https://example.test"}, CredentialsJSON: []byte("{\"api_key\":\"secret\"}")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := uuid.New()
	service := NewModelProviderService(repository, fakeCredentialCipher{}, NewProviderFactoryResolver(registry))
	provider, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelProviderInput{
		ActorID: uuid.New(), Name: "catalog", DisplayName: "目录 Provider", Provider: "catalog-provider",
		Config: json.RawMessage("{}"), Credentials: json.RawMessage("{\"api_key\":\"secret\"}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, provider.ID, workspaceID
}

func TestModelCatalogServiceDecryptsCredentialsAndReturnsSafeItems(t *testing.T) {
	t.Parallel()
	catalog := &recordingModelCatalog{items: []modelcatalogport.Item{{
		ID: "embed-v1", DisplayName: "Embedding V1", Type: ptrModelType(value.ModelTypeEmbedding),
		Dimensions: ptrModelInt(1024), Parameters: map[string]any{"batch_size": 32}, Available: true,
	}}}
	service, _, providerID, workspaceID := newCatalogService(t, catalog)
	result, err := service.ListModelCatalogWorkspace(context.Background(), workspaceID, providerID, ModelCatalogFilter{Type: ptrModelType(value.ModelTypeEmbedding), Query: "embed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "embed-v1" || result.Items[0].Dimensions == nil || *result.Items[0].Dimensions != 1024 {
		t.Fatalf("catalog result = %#v", result)
	}
	if catalog.credentialSnapshot != "{\"api_key\":\"secret\"}" || catalog.input.Query != "embed" {
		t.Fatalf("catalog input query/type was not forwarded")
	}
	if strings.Contains(string(catalog.input.CredentialsJSON), "secret") {
		t.Fatal("decrypted credentials were not cleared after catalog call")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("catalog response leaked credentials: %s", encoded)
	}
}

func TestModelCatalogServiceRejectsDisabledAndUnavailableProviders(t *testing.T) {
	t.Parallel()
	catalog := &recordingModelCatalog{}
	service, repository, providerID, workspaceID := newCatalogService(t, catalog)
	provider := repository.items[providerID]
	provider.Status = value.ModelStatusDisabled
	repository.items[providerID] = provider
	_, err := service.ListModelCatalogWorkspace(context.Background(), workspaceID, providerID, ModelCatalogFilter{})
	if !errors.Is(err, domainerrors.ErrProviderDisabled) {
		t.Fatalf("disabled error = %v", err)
	}

	registry, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key: "no-catalog", Capabilities: []value.ProviderCapability{value.CapabilityEmbedding},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{Config: map[string]any{}, CredentialsJSON: []byte("{}")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	noCatalogRepo := newFakeModelProviderRepository()
	noCatalogService := NewModelProviderService(noCatalogRepo, fakeCredentialCipher{}, NewProviderFactoryResolver(registry))
	created, err := noCatalogService.CreatePlatform(context.Background(), CreateModelProviderInput{ActorID: uuid.New(), Name: "none", Provider: "no-catalog", Config: json.RawMessage("{}"), Credentials: json.RawMessage("{}")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = noCatalogService.ListModelCatalogPlatform(context.Background(), created.ID, ModelCatalogFilter{})
	if !errors.Is(err, domainerrors.ErrCatalogUnavailable) {
		t.Fatalf("unavailable error = %v", err)
	}
}

func ptrModelType(modelType value.ModelType) *value.ModelType { return &modelType }
func ptrModelInt(number int) *int                             { return &number }
