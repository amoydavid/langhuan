//go:build integration

package db

import (
	"testing"
	"time"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestKnowledgeBaseGenerationCreatesRootAndReadyGenerationAtomically(t *testing.T) {
	t.Parallel()

	ctx, database := openIntegrationTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, database, "kb-generation-"+uuid.NewString())
	providerRepo := NewModelProviderRepository(database)
	modelRepo := NewModelRepository(database)
	provider := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceID, "kb-generation")
	embeddingModel := createModelForTest(t, ctx, modelRepo, provider.ID, "kb-generation", value.ModelStatusActive)
	t.Cleanup(func() {
		database.WithContext(ctx).Delete(&KnowledgeBaseRow{}, "workspace_id = ?", workspaceID)
		database.WithContext(ctx).Delete(&ModelRow{}, "id = ?", embeddingModel.ID)
		database.WithContext(ctx).Delete(&ModelProviderRow{}, "id = ?", provider.ID)
		database.WithContext(ctx).Delete(&WorkspaceRow{}, "id = ?", workspaceID)
	})

	repository := NewKnowledgeBaseRepository(database)
	service := appservice.NewKnowledgeBaseService(repository, repository)
	created, err := service.Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "initial generation", EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	var kb KnowledgeBaseRow
	if err := database.WithContext(ctx).First(&kb, "id = ? AND workspace_id = ?", created.ID, workspaceID).Error; err != nil {
		t.Fatal(err)
	}
	if kb.ActiveIndexGenerationID == nil || kb.FileTreeRootID == uuid.Nil || kb.ContentVersion != 0 {
		t.Fatalf("knowledge base = %#v", kb)
	}
	var root FileTreeNodeRow
	if err := database.WithContext(ctx).First(&root, "id = ? AND workspace_id = ?", kb.FileTreeRootID, workspaceID).Error; err != nil {
		t.Fatal(err)
	}
	if root.NodeType != string(value.FileTreeNodeRoot) || root.ParentID != nil || root.DocumentID != nil {
		t.Fatalf("root = %#v", root)
	}
	var generation IndexGenerationRow
	if err := database.WithContext(ctx).First(&generation, "id = ? AND workspace_id = ?", *kb.ActiveIndexGenerationID, workspaceID).Error; err != nil {
		t.Fatal(err)
	}
	if generation.Status != string(value.IndexGenerationReady) || generation.SourceContentVersion != 0 ||
		generation.IndexedContentVersion != 0 || generation.ReadyAt == nil || generation.EmbeddingModelID != embeddingModel.ID {
		t.Fatalf("generation = %#v", generation)
	}
	if created.EmbeddingModelID != embeddingModel.ID || created.ChunkingConfig.ChunkSize == 0 {
		t.Fatalf("compatibility DTO = %#v", created)
	}

	secondRoot := &FileTreeNodeRow{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: created.ID,
		NodeType: string(value.FileTreeNodeRoot), Name: "", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := database.WithContext(ctx).Create(secondRoot).Error; err == nil {
		t.Fatal("second root insert error = nil, want unique violation")
	}
}
