//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestModelRepositoryListVisibleJoinsProviderScopeAndStatus(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceA := createWorkspaceRow(t, ctx, tx, "model-visible-a")
	workspaceB := createWorkspaceRow(t, ctx, tx, "model-visible-b")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)

	platform := createProviderForTest(t, ctx, providerRepo, value.ModelScopePlatform, nil, "platform")
	own := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceA, "own")
	other := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceB, "other")
	sharedModel := createModelForTest(t, ctx, modelRepo, platform.ID, "shared", value.ModelStatusActive)
	ownModel := createModelForTest(t, ctx, modelRepo, own.ID, "own", value.ModelStatusActive)
	createModelForTest(t, ctx, modelRepo, other.ID, "hidden", value.ModelStatusActive)
	disabledModel := createModelForTest(t, ctx, modelRepo, own.ID, "disabled", value.ModelStatusDisabled)

	active, err := modelRepo.ListVisible(ctx, workspaceA, value.ModelTypeEmbedding, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || active[0].Model.ID != ownModel.ID || active[1].Model.ID != sharedModel.ID {
		t.Fatalf("active visible models = %#v", active)
	}
	all, err := modelRepo.ListVisible(ctx, workspaceA, value.ModelTypeEmbedding, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("all visible model count = %d, want 3", len(all))
	}
	if _, err := modelRepo.GetVisible(ctx, workspaceA, disabledModel.ID); err != nil {
		t.Fatalf("disabled model should remain readable: %v", err)
	}
}

func TestModelRepositoryEnforcesOwnedReadsUpdatesAndReferenceDelete(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "model-update")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	provider := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceID, "provider")
	item := createModelForTest(t, ctx, modelRepo, provider.ID, "model", value.ModelStatusActive)

	item.DisplayName = "新模型名"
	item.Description = "updated"
	item.Parameters = map[string]any{"batch_size": float64(16)}
	item.Status = value.ModelStatusDisabled
	if err := modelRepo.Update(ctx, item); err != nil {
		t.Fatal(err)
	}
	resolved, err := modelRepo.GetWorkspaceOwned(ctx, workspaceID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Model.DisplayName != "新模型名" || resolved.Model.Status != value.ModelStatusDisabled {
		t.Fatalf("updated model = %#v", resolved.Model)
	}
	if _, err := modelRepo.GetPlatform(ctx, item.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("platform lookup error = %v", err)
	}

	item.Status = value.ModelStatusActive
	if err := modelRepo.Update(ctx, item); err != nil {
		t.Fatal(err)
	}
	kbRepo := NewKnowledgeBaseRepository(tx)
	if _, err := appservice.NewKnowledgeBaseService(kbRepo, kbRepo).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "uses-model", EmbeddingModelID: item.ID,
	}); err != nil {
		t.Fatal(err)
	}
	count, err := modelRepo.CountKnowledgeBaseReferences(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reference count = %d, want 1", count)
	}
	if err := modelRepo.Delete(ctx, item.ID); !errors.Is(err, domainerrors.ErrModelInUse) {
		t.Fatalf("delete referenced model error = %v", err)
	}
}

func TestModelRepositoryRejectsSemanticUpdateRacingWithKnowledgeBaseBinding(t *testing.T) {
	ctx, database := openIntegrationTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, database, "model-racing-binding")
	providerRepo := NewModelProviderRepository(database)
	modelRepo := NewModelRepository(database)
	provider := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceID, "race-provider")
	item := createModelForTest(t, ctx, modelRepo, provider.ID, "race-model", value.ModelStatusActive)
	t.Cleanup(func() {
		database.WithContext(ctx).Where("embedding_model_id = ?", item.ID).Delete(&IndexGenerationRow{})
		database.WithContext(ctx).Where("workspace_id = ?", workspaceID).Delete(&KnowledgeBaseRow{})
		database.WithContext(ctx).Delete(&ModelRow{}, "id = ?", item.ID)
		database.WithContext(ctx).Delete(&ModelProviderRow{}, "id = ?", provider.ID)
		database.WithContext(ctx).Delete(&WorkspaceRow{}, "id = ?", workspaceID)
	})

	binding := database.Begin()
	if binding.Error != nil {
		t.Fatal(binding.Error)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			_ = binding.Rollback()
		}
	})
	var locked ModelRow
	if err := binding.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&locked, "id = ?", item.ID).Error; err != nil {
		t.Fatal(err)
	}
	kbRepo := NewKnowledgeBaseRepository(binding)
	if _, err := appservice.NewKnowledgeBaseService(kbRepo, kbRepo).Create(ctx, appservice.CreateKnowledgeBaseInput{
		WorkspaceID: workspaceID, Name: "racing-binding", EmbeddingModelID: item.ID,
	}); err != nil {
		t.Fatal(err)
	}

	candidate := *item
	candidate.ModelName = "changed-after-reference-check"
	candidate.UpdatedAt = time.Now().UTC()
	updated := make(chan error, 1)
	go func() { updated <- modelRepo.Update(ctx, &candidate) }()
	select {
	case err := <-updated:
		t.Fatalf("semantic update did not wait for binding transaction: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := binding.Commit().Error; err != nil {
		t.Fatal(err)
	}
	committed = true
	if err := <-updated; !errors.Is(err, domainerrors.ErrImmutableModelField) {
		t.Fatalf("racing semantic update error = %v", err)
	}
	stored, err := modelRepo.GetWorkspaceOwned(ctx, workspaceID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Model.ModelName != item.ModelName {
		t.Fatalf("model name changed to %q", stored.Model.ModelName)
	}
}

func TestMigration004AllowsFutureProviderKeyButRejectsBadDimensions(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "future-provider")
	provider := ModelProviderRow{
		ID: uuid.New(), Scope: "workspace", WorkspaceID: &workspaceID,
		Name: "future", DisplayName: "future", Provider: "future_vendor",
		Config: JSONMap{}, Status: "active",
	}
	if err := tx.WithContext(ctx).Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	dimension := 1536
	bad := ModelRow{
		ID: uuid.New(), ProviderID: provider.ID, Name: "bad", DisplayName: "bad",
		Type: "embedding", ModelName: "bad", Dimensions: &dimension,
		Parameters: JSONMap{}, Status: "active",
	}
	if err := tx.WithContext(ctx).Create(&bad).Error; err == nil {
		t.Fatal("expected dimensions CHECK violation")
	}
}

func TestModelRepositoriesPropagateCancelledContext(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := NewModelProviderRepository(tx).ListPlatform(cancelled); err == nil {
		t.Fatal("expected cancelled provider query")
	}
	if _, err := NewModelRepository(tx).ListVisible(cancelled, uuid.New(), value.ModelTypeEmbedding, false); err == nil {
		t.Fatal("expected cancelled model query")
	}
}

func createModelForTest(
	t *testing.T,
	ctx context.Context,
	repo *ModelRepository,
	providerID uuid.UUID,
	name string,
	status value.ModelStatus,
) *model.Model {
	t.Helper()
	dimension := 1024
	item, err := model.NewModel(providerID, name, name, "", value.ModelTypeEmbedding, name, &dimension, map[string]any{"batch_size": float64(32)}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	item.CreatedBy = nil
	item.Status = status
	if err := repo.Create(ctx, item); err != nil {
		t.Fatal(err)
	}
	return item
}
