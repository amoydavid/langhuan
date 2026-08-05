package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	embeddingport "github.com/dajee/langhuan/internal/ports/embedding"
)

func TestModelProviderServicePreservesOrReplacesCredentialsExplicitly(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	repository := newFakeModelProviderRepository()
	factory := &fakeEmbeddingFactory{}
	service := NewModelProviderService(repository, fakeCredentialCipher{}, resolverFromFakeRegistry(factory))
	created, err := service.CreateWorkspace(context.Background(), workspaceID, CreateModelProviderInput{
		ActorID: actorID, Name: " openai-prod ", DisplayName: "OpenAI 生产", Provider: "openai",
		Config: json.RawMessage(`{"timeout_seconds":60}`), Credentials: json.RawMessage(`{"api_key":"top-secret"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret") || !created.CredentialsConfigured {
		t.Fatalf("unsafe provider DTO = %s", encoded)
	}
	before := string(repository.items[created.ID].CredentialsCiphertext)
	newConfig := json.RawMessage(`{"timeout_seconds":30}`)
	updated, err := service.UpdateWorkspace(context.Background(), workspaceID, created.ID, UpdateModelProviderInput{
		DisplayName: ptrForModelTest("新名称"), Config: &newConfig,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "新名称" || string(repository.items[created.ID].CredentialsCiphertext) != before {
		t.Fatal("PATCH without credentials did not preserve ciphertext")
	}
	replacement := json.RawMessage(`{"api_key":"new-secret"}`)
	if _, err := service.UpdateWorkspace(context.Background(), workspaceID, created.ID, UpdateModelProviderInput{Credentials: &replacement}); err != nil {
		t.Fatal(err)
	}
	if string(repository.items[created.ID].CredentialsCiphertext) == before {
		t.Fatal("explicit credentials replacement kept old ciphertext")
	}
}

func TestModelProviderServiceEnforcesScopeAndDeleteReference(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	repository := newFakeModelProviderRepository()
	service := NewModelProviderService(repository, fakeCredentialCipher{}, resolverFromFakeRegistry(&fakeEmbeddingFactory{}))
	created, err := service.CreatePlatform(context.Background(), CreateModelProviderInput{
		ActorID: actorID, Name: "shared", Provider: "openai", Config: json.RawMessage(`{}`), Credentials: json.RawMessage(`{"api_key":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateWorkspace(context.Background(), workspaceID, created.ID, UpdateModelProviderInput{DisplayName: ptrForModelTest("越权")}); !isNotFound(err) {
		t.Fatalf("workspace mutation scope error = %v", err)
	}
	repositoryWithModels := &providerRepositoryWithCount{fakeModelProviderRepository: repository, count: 1}
	service = NewModelProviderService(repositoryWithModels, fakeCredentialCipher{}, resolverFromFakeRegistry(&fakeEmbeddingFactory{}))
	if err := service.DeletePlatform(context.Background(), created.ID); err != domainerrors.ErrProviderInUse {
		t.Fatalf("delete error = %v", err)
	}
}

type providerRepositoryWithCount struct {
	*fakeModelProviderRepository
	count int64
}

func (r *providerRepositoryWithCount) CountModels(context.Context, uuid.UUID) (int64, error) {
	return r.count, nil
}

func ptrForModelTest(value string) *string { return &value }

func isNotFound(err error) bool { return errors.Is(err, domainerrors.ErrNotFound) }

// resolverFromFakeRegistry 把测试用的 fakeFactoryRegistry 包装成 ProviderFactoryResolver。
func resolverFromFakeRegistry(factory embeddingport.Factory) *ProviderFactoryResolver {
	return NewProviderFactoryResolver(fakeFactoryRegistry{factory: factory}, nil, nil)
}
