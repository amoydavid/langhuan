//go:build integration

package db

import (
	"testing"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceReadinessRepositoryReadsVisibleModelAndKnowledgeBase(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, database, "readiness")
	repository := NewWorkspaceReadinessRepository(database)
	actor, err := model.NewUser("readiness@example.com", "Readiness", "test-password-hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := NewUserRepository(database).Create(ctx, actor); err != nil {
		t.Fatal(err)
	}

	empty, err := repository.GetWorkspaceReadinessFacts(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read empty readiness: %v", err)
	}
	if empty.HasActiveProvider || empty.HasSelectableEmbeddingModel || empty.KnowledgeBaseCount != 0 || empty.TotalDocuments != 0 {
		t.Fatalf("empty facts = %#v", empty)
	}

	provider, err := model.NewModelProvider(
		value.ModelScopeWorkspace, &workspaceID, "readiness-provider", "Readiness Provider", "",
		"openai", map[string]any{}, []byte("ciphertext"), actor.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewModelProviderRepository(database).Create(ctx, provider); err != nil {
		t.Fatal(err)
	}
	dimensions := 1024
	embeddingModel, err := model.NewModel(
		provider.ID, "readiness-model", "Readiness Model", "", value.ModelTypeEmbedding,
		"text-embedding", &dimensions, map[string]any{}, actor.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewModelRepository(database).Create(ctx, embeddingModel); err != nil {
		t.Fatal(err)
	}
	kbRepository := NewKnowledgeBaseRepository(database)
	created, err := service.NewKnowledgeBaseService(kbRepository, kbRepository).Create(ctx, service.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "产品文档", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	facts, err := repository.GetWorkspaceReadinessFacts(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read populated readiness: %v", err)
	}
	if !facts.HasActiveProvider || !facts.HasSelectableEmbeddingModel || facts.KnowledgeBaseCount != 1 {
		t.Fatalf("facts = %#v", facts)
	}
	if facts.RecommendedKnowledgeBaseID == nil || *facts.RecommendedKnowledgeBaseID != created.ID || facts.RecommendedKnowledgeBaseName != "产品文档" {
		t.Fatalf("recommendation = %#v", facts)
	}
}
