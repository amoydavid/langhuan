//go:build integration

package db

import (
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceSearchSettingsRepositoryUpsertIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	workspaceID := createWorkspaceRow(t, ctx, tx, "search-settings-ws")
	userID := uuid.New()
	if err := tx.Exec("INSERT INTO users (id, email, nickname, password_hash) VALUES (?, ?, 'Search Admin', 'hash')", userID, userID.String()+"@example.com").Error; err != nil {
		t.Fatalf("user seed: %v", err)
	}
	settingsRepo := NewWorkspaceSearchSettingsRepository(tx)

	disabled := &model.WorkspaceSearchSettings{WorkspaceID: workspaceID, UpdatedBy: userID}
	if err := settingsRepo.Upsert(ctx, disabled); err != nil {
		t.Fatalf("disabled upsert: %v", err)
	}
	got, err := settingsRepo.Get(ctx, workspaceID)
	if err != nil || got.Rerank != nil {
		t.Fatalf("disabled get = %#v err=%v", got, err)
	}

	// Model/provider references are validated by the application service; this
	// integration test focuses on persistence shape and transaction scoping.
	modelID, providerID := uuid.New(), uuid.New()
	enabled := &model.WorkspaceSearchSettings{WorkspaceID: workspaceID, UpdatedBy: userID, Rerank: &model.RerankSnapshot{
		ModelID: modelID, ProviderID: providerID, ModelName: "rerank", ModelConfigHash: "hash", CandidateTopK: 50, FailureMode: value.RerankFailureFallback,
	}}
	// Insert referenced rows before the settings row so PostgreSQL foreign keys
	// are exercised by the real temporary integration database.
	if err := tx.Exec("INSERT INTO model_providers (id, scope, name, display_name, description, provider, config, credentials_ciphertext, status) VALUES (?, 'platform', 'search-settings-provider', 'Search Settings Provider', '', 'rerank_compatible', '{}', '', 'active')", providerID).Error; err != nil {
		t.Fatalf("provider seed: %v", err)
	}
	if err := tx.Exec("INSERT INTO models (id, provider_id, name, display_name, description, type, model_name, parameters, status) VALUES (?, ?, 'search-settings-model', 'Search Settings Model', '', 'rerank', 'rerank', '{\"max_documents\":100,\"max_query_chars\":4096,\"max_document_chars\":8192}', 'active')", modelID, providerID).Error; err != nil {
		t.Fatalf("model seed: %v", err)
	}
	if err := settingsRepo.Upsert(ctx, enabled); err != nil {
		t.Fatalf("enabled upsert: %v", err)
	}
	got, err = settingsRepo.Get(ctx, workspaceID)
	if err != nil || got.Rerank == nil || got.Rerank.ModelID != modelID || got.Rerank.FailureMode != value.RerankFailureFallback {
		t.Fatalf("enabled get = %#v err=%v", got, err)
	}
}
