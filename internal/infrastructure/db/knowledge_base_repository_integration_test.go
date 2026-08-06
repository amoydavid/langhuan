//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestKnowledgeBaseBinderRejectsCrossWorkspaceInsideTransaction(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceA := createWorkspaceRow(t, ctx, tx, "kb-bind-a")
	workspaceB := createWorkspaceRow(t, ctx, tx, "kb-bind-b")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	providerB := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceB, "provider-b")
	modelB := createModelForTest(t, ctx, modelRepo, providerB.ID, "model-b", value.ModelStatusActive)
	repository := NewKnowledgeBaseRepository(tx)
	service := appservice.NewKnowledgeBaseService(repository, repository)
	_, err := service.Create(ctx, appservice.CreateKnowledgeBaseInput{WorkspaceID: workspaceA, Name: "cross-workspace", EmbeddingModelID: modelB.ID})
	if !errors.Is(err, domainerrors.ErrModelNotVisible) {
		t.Fatalf("Create() error = %v", err)
	}
	var count int64
	if err := tx.WithContext(ctx).Model(&KnowledgeBaseRow{}).Where("workspace_id = ?", workspaceA).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed binding left %d knowledge base rows", count)
	}
}

func TestKnowledgeBaseBinderCreatesUpdatesAndLoadsModelSummary(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "kb-bind-visible")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	platform := createProviderForTest(t, ctx, providerRepo, value.ModelScopePlatform, nil, "platform")
	sharedModel := createModelForTest(t, ctx, modelRepo, platform.ID, "shared", value.ModelStatusActive)
	repository := NewKnowledgeBaseRepository(tx)
	service := appservice.NewKnowledgeBaseService(repository, repository)
	created, err := service.Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "resolved", Description: "desc", EmbeddingModelID: sharedModel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.EmbeddingModel.ID != sharedModel.ID || created.EmbeddingModel.Provider != platform.Provider {
		t.Fatalf("created model = %#v", created)
	}
	loaded, err := repository.GetResolved(ctx, workspaceID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.KnowledgeBase.EmbeddingModelID != sharedModel.ID || loaded.EmbeddingModel.Provider.DisplayName != platform.DisplayName {
		t.Fatalf("loaded = %#v", loaded)
	}
	if loaded.RetrievalConfig["fts_config"] != "zhparser" || loaded.RetrievalConfig["final_top_k"] != float64(10) {
		t.Fatalf("active retrieval config = %#v", loaded.RetrievalConfig)
	}
	listed, err := repository.ListResolved(ctx, workspaceID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].EmbeddingModel.Model.ID != sharedModel.ID {
		t.Fatalf("listed = %#v", listed)
	}
}

func TestKnowledgeBaseBinderRejectsDisabledProviderAndModel(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "kb-bind-disabled")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	provider := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceID, "disabled-provider")
	item := createModelForTest(t, ctx, modelRepo, provider.ID, "disabled-model", value.ModelStatusDisabled)
	repository := NewKnowledgeBaseRepository(tx)
	service := appservice.NewKnowledgeBaseService(repository, repository)
	input := appservice.CreateKnowledgeBaseInput{WorkspaceID: workspaceID, Name: "disabled", EmbeddingModelID: item.ID}
	if _, err := service.Create(ctx, input); !errors.Is(err, domainerrors.ErrModelDisabled) {
		t.Fatalf("disabled model error = %v", err)
	}
	item.Status = value.ModelStatusActive
	if err := modelRepo.Update(ctx, item); err != nil {
		t.Fatal(err)
	}
	provider.Status = value.ModelStatusDisabled
	if err := providerRepo.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, input); !errors.Is(err, domainerrors.ErrProviderDisabled) {
		t.Fatalf("disabled provider error = %v", err)
	}
}

func TestKnowledgeBaseRepositoryListScopesAndOrders(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceA := createWorkspaceRow(t, ctx, tx, "kb-list-a")
	workspaceB := createWorkspaceRow(t, ctx, tx, "kb-list-b")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	providerA := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceA, "list-a")
	providerB := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceB, "list-b")
	modelA := createModelForTest(t, ctx, modelRepo, providerA.ID, "list-a", value.ModelStatusActive)
	modelB := createModelForTest(t, ctx, modelRepo, providerB.ID, "list-b", value.ModelStatusActive)
	repository := NewKnowledgeBaseRepository(tx)
	createdAt := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	createKB := func(workspaceID, modelID uuid.UUID, name string, timestamp time.Time) uuid.UUID {
		t.Helper()
		service := appservice.NewKnowledgeBaseService(repository, repository)
		created, err := service.Create(ctx, appservice.CreateKnowledgeBaseInput{WorkspaceID: workspaceID, Name: name, EmbeddingModelID: modelID})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.WithContext(ctx).Model(&KnowledgeBaseRow{}).Where("id = ?", created.ID).Updates(map[string]any{"created_at": timestamp, "updated_at": timestamp}).Error; err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	id1 := createKB(workspaceA, modelA.ID, "one", createdAt)
	id2 := createKB(workspaceA, modelA.ID, "two", createdAt.Add(time.Minute))
	createKB(workspaceB, modelB.ID, "other", createdAt)
	got, err := repository.List(ctx, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != id2 || got[1].ID != id1 {
		t.Fatalf("List() = %#v", got)
	}
}

func TestKnowledgeBaseRepositoryPropagatesCancelledContext(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := NewKnowledgeBaseRepository(tx).ListResolved(cancelled, uuid.New(), nil); err == nil {
		t.Fatal("expected cancelled query")
	}
}

func TestKnowledgeBaseRepositoryUpdatesOnlyScopedBasics(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	repository := NewKnowledgeBaseRepository(database)
	name, description := "新的知识库", "新的描述"
	if err := repository.UpdateBasics(ctx, appservice.UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		Name: &name, Description: &description, ActorRole: value.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	var updated KnowledgeBaseRow
	if err := database.WithContext(ctx).First(&updated, "workspace_id = ? AND id = ?", seed.workspaceID, seed.kbID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.Description != description || updated.ActiveIndexGenerationID == nil || *updated.ActiveIndexGenerationID != seed.generationID || updated.ContentVersion != 0 {
		t.Fatalf("updated = %#v", updated)
	}
	otherWorkspaceID := createWorkspaceRow(t, ctx, database, "kb-update-other-"+uuid.NewString())
	if err := repository.UpdateBasics(ctx, appservice.UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: otherWorkspaceID, KnowledgeBaseID: seed.kbID, Name: &name, ActorRole: value.RoleOwner,
	}); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace update error = %v, want not found", err)
	}
	duplicate, err := appservice.NewKnowledgeBaseService(repository, repository).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: seed.workspaceID, Name: "名称冲突", EmbeddingModelID: seed.modelID,
	})
	if err != nil {
		t.Fatal(err)
	}
	conflictingName := duplicate.Name
	if err := repository.UpdateBasics(ctx, appservice.UpdateKnowledgeBaseBasicsInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, Name: &conflictingName, ActorRole: value.RoleAdmin,
	}); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("duplicate-name update error = %v, want conflict", err)
	}
}
