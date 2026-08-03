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

func TestEmbeddingClientResolverResolvesWorkspaceAndPlatformModels(t *testing.T) {
	for _, scope := range []value.ModelScope{value.ModelScopeWorkspace, value.ModelScopePlatform} {
		t.Run(string(scope), func(t *testing.T) {
			workspaceID := uuid.New()
			models, cipher, registry, item, client := embeddingResolverFixture(t, workspaceID, scope)
			resolver := NewEmbeddingClientResolver(models, cipher, registry)

			resolved, err := resolver.Resolve(context.Background(), workspaceID, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.Client != client || resolved.ModelID != item.ID ||
				resolved.ProviderID != item.ProviderID || resolved.ModelName != item.ModelName ||
				resolved.Dimensions != 1024 || resolved.BatchSize != 32 {
				t.Fatalf("resolved = %#v", resolved)
			}
		})
	}
}

func TestEmbeddingClientResolverRejectsDisabledRecords(t *testing.T) {
	tests := []struct {
		name      string
		disable   func(*model.ResolvedModel)
		wantError error
	}{
		{name: "provider", disable: func(resolved *model.ResolvedModel) {
			resolved.Provider.Status = value.ModelStatusDisabled
		}, wantError: domainerrors.ErrProviderDisabled},
		{name: "model", disable: func(resolved *model.ResolvedModel) {
			resolved.Model.Status = value.ModelStatusDisabled
		}, wantError: domainerrors.ErrModelDisabled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspaceID := uuid.New()
			models, cipher, registry, item, _ := embeddingResolverFixture(t, workspaceID, value.ModelScopeWorkspace)
			resolved, err := models.GetVisible(context.Background(), workspaceID, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			test.disable(resolved)
			models.items[item.ID] = resolved.Model
			models.providers.items[resolved.Provider.ID] = resolved.Provider

			_, err = NewEmbeddingClientResolver(models, cipher, registry).
				Resolve(context.Background(), workspaceID, item.ID)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Resolve error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func TestEmbeddingClientResolverRejectsInvalidCiphertext(t *testing.T) {
	workspaceID := uuid.New()
	models, cipher, registry, item, _ := embeddingResolverFixture(t, workspaceID, value.ModelScopeWorkspace)
	provider := models.providers.items[item.ProviderID]
	provider.CredentialsCiphertext = []byte("invalid")

	_, err := NewEmbeddingClientResolver(models, cipher, registry).
		Resolve(context.Background(), workspaceID, item.ID)
	if !errors.Is(err, domainerrors.ErrCredentialDecryption) {
		t.Fatalf("Resolve error = %v, want ErrCredentialDecryption", err)
	}
}

func TestEmbeddingClientResolverRejectsClientDimensionMismatch(t *testing.T) {
	workspaceID := uuid.New()
	models, cipher, registry, item, client := embeddingResolverFixture(t, workspaceID, value.ModelScopeWorkspace)
	client.dimension = 2048

	_, err := NewEmbeddingClientResolver(models, cipher, registry).
		Resolve(context.Background(), workspaceID, item.ID)
	if !errors.Is(err, domainerrors.ErrDimensionMismatch) {
		t.Fatalf("Resolve error = %v, want ErrDimensionMismatch", err)
	}
}

func embeddingResolverFixture(
	t *testing.T,
	workspaceID uuid.UUID,
	scope value.ModelScope,
) (*fakeModelRepository, fakeCredentialCipher, fakeFactoryRegistry, *model.Model, *recordingEmbeddingClient) {
	t.Helper()
	providers := newFakeModelProviderRepository()
	var providerWorkspaceID *uuid.UUID
	if scope == value.ModelScopeWorkspace {
		providerWorkspaceID = &workspaceID
	}
	provider, err := model.NewModelProvider(
		scope, providerWorkspaceID, "openai", "OpenAI", "", "openai",
		map[string]any{"timeout_seconds": float64(60)}, nil, uuid.New(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cipher := fakeCredentialCipher{}
	provider.CredentialsCiphertext, err = cipher.Encrypt(provider.ID, []byte(`{"api_key":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	dimensions := 1024
	item, err := model.NewModel(
		provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding,
		"text-embedding", &dimensions, map[string]any{"batch_size": float64(32)}, uuid.New(),
	)
	if err != nil {
		t.Fatal(err)
	}
	models.items[item.ID] = item
	client := &recordingEmbeddingClient{dimension: dimensions}
	registry := fakeFactoryRegistry{factory: &fakeEmbeddingFactory{client: client}}
	return models, cipher, registry, item, client
}
