package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestConnectionTestUsesFixedTextAndAcceptsDisabledRecords(t *testing.T) {
	t.Parallel()
	workspaceID, actorID := uuid.New(), uuid.New()
	providers := newFakeModelProviderRepository()
	provider, _ := model.NewModelProvider(value.ModelScopeWorkspace, &workspaceID, "openai", "OpenAI", "", "openai", map[string]any{"timeout_seconds": float64(60)}, nil, actorID)
	cipher := fakeCredentialCipher{}
	provider.CredentialsCiphertext, _ = cipher.Encrypt(provider.ID, []byte(`{"api_key":"secret"}`))
	provider.Status = value.ModelStatusDisabled
	providers.items[provider.ID] = provider
	models := newFakeModelRepository(providers)
	dimension := 1024
	item, _ := model.NewModel(provider.ID, "embed", "Embedding", "", value.ModelTypeEmbedding, "text-embedding", &dimension, map[string]any{"batch_size": float64(32)}, actorID)
	item.Status = value.ModelStatusDisabled
	models.items[item.ID] = item
	client := &recordingEmbeddingClient{dimension: 1024}
	service := NewModelConnectionTestService(models, cipher, fakeFactoryRegistry{factory: &fakeEmbeddingFactory{client: client}})
	got, err := service.TestWorkspace(context.Background(), workspaceID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Dimensions != 1024 || len(client.input.Texts) != 1 || client.input.Texts[0] != ConnectionTestText {
		t.Fatalf("result = %#v, input = %#v", got, client.input)
	}
}
