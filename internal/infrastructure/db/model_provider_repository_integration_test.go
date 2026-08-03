//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestModelProviderRepositoryScopesVisibleRecords(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceA := createWorkspaceRow(t, ctx, tx, "provider-visible-a")
	workspaceB := createWorkspaceRow(t, ctx, tx, "provider-visible-b")
	repo := NewModelProviderRepository(tx)

	platform := createProviderForTest(t, ctx, repo, value.ModelScopePlatform, nil, "platform")
	own := createProviderForTest(t, ctx, repo, value.ModelScopeWorkspace, &workspaceA, "own")
	other := createProviderForTest(t, ctx, repo, value.ModelScopeWorkspace, &workspaceB, "other")

	got, err := repo.ListVisible(ctx, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != own.ID || got[1].ID != platform.ID {
		t.Fatalf("visible providers = %#v", got)
	}
	if _, err := repo.GetVisible(ctx, workspaceA, other.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("other workspace error = %v", err)
	}
	if _, err := repo.GetWorkspaceOwned(ctx, workspaceA, platform.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("platform through workspace mutation error = %v", err)
	}
	if _, err := repo.GetPlatform(ctx, own.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("workspace through platform mutation error = %v", err)
	}
}

func TestModelProviderRepositoryUpdatesSafeFieldsAndCountsModels(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "provider-update")
	providerRepo := NewModelProviderRepository(tx)
	modelRepo := NewModelRepository(tx)
	provider := createProviderForTest(t, ctx, providerRepo, value.ModelScopeWorkspace, &workspaceID, "provider")

	provider.DisplayName = "新名称"
	provider.Description = "updated"
	provider.Config = map[string]any{"timeout_seconds": float64(30)}
	provider.CredentialsCiphertext = []byte{9, 8, 7}
	provider.Status = value.ModelStatusDisabled
	if err := providerRepo.Update(ctx, provider); err != nil {
		t.Fatal(err)
	}
	got, err := providerRepo.GetWorkspaceOwned(ctx, workspaceID, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "新名称" || got.Status != value.ModelStatusDisabled || got.Config["timeout_seconds"] != float64(30) {
		t.Fatalf("updated provider = %#v", got)
	}

	createModelForTest(t, ctx, modelRepo, provider.ID, "embedding", value.ModelStatusActive)
	count, err := providerRepo.CountModels(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("model count = %d, want 1", count)
	}
	if err := providerRepo.Delete(ctx, provider.ID); !errors.Is(err, domainerrors.ErrProviderInUse) {
		t.Fatalf("delete provider error = %v", err)
	}
}

func createProviderForTest(
	t *testing.T,
	ctx context.Context,
	repo *ModelProviderRepository,
	scope value.ModelScope,
	workspaceID *uuid.UUID,
	name string,
) *model.ModelProvider {
	t.Helper()
	provider, err := model.NewModelProvider(scope, workspaceID, name, name, "", "openai", map[string]any{"timeout_seconds": float64(60)}, []byte{1, 2, 3}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	// Integration fixtures use nullable created_by to avoid coupling repository tests to user creation.
	provider.CreatedBy = nil
	if err := repo.Create(ctx, provider); err != nil {
		t.Fatal(err)
	}
	return provider
}
