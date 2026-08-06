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

type workspaceSearchSettingsRepoStub struct {
	settings *model.WorkspaceSearchSettings
}

func (r *workspaceSearchSettingsRepoStub) Get(context.Context, uuid.UUID) (*model.WorkspaceSearchSettings, error) {
	if r.settings == nil {
		return nil, domainerrors.ErrNotFound
	}
	return r.settings.Clone(), nil
}
func (r *workspaceSearchSettingsRepoStub) Upsert(_ context.Context, settings *model.WorkspaceSearchSettings) error {
	r.settings = settings.Clone()
	return nil
}

type workspaceSearchModelResolverStub struct {
	resolved *model.ResolvedModel
	err      error
}

func (r *workspaceSearchModelResolverStub) ResolveSelectable(context.Context, uuid.UUID, uuid.UUID) (*model.ResolvedModel, error) {
	return r.resolved, r.err
}
func (r *workspaceSearchModelResolverStub) ResolveSelectableModel(context.Context, uuid.UUID, uuid.UUID, value.ModelType) (*model.ResolvedModel, error) {
	return r.resolved, r.err
}

func TestWorkspaceSearchSettingsServiceRequiresAdminToUpdate(t *testing.T) {
	t.Parallel()
	service := NewWorkspaceSearchSettingsService(&workspaceSearchSettingsRepoStub{}, &workspaceSearchModelResolverStub{})
	_, err := service.Update(context.Background(), uuid.New(), value.RoleMember, UpdateWorkspaceSearchSettingsInput{RerankEnabled: false})
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("member update err = %v, want forbidden", err)
	}
}

func TestWorkspaceSearchSettingsServiceSnapshotsRerankModel(t *testing.T) {
	t.Parallel()
	workspaceID, modelID, providerID := uuid.New(), uuid.New(), uuid.New()
	resolver := &workspaceSearchModelResolverStub{resolved: &model.ResolvedModel{
		Model:    &model.Model{ID: modelID, ProviderID: providerID, Type: value.ModelTypeRerank, ModelName: "rerank", Parameters: map[string]any{"max_documents": 100, "max_query_chars": 4096, "max_document_chars": 8192}},
		Provider: &model.ModelProvider{ID: providerID, Provider: "rerank_compatible", Status: value.ModelStatusActive, Config: map[string]any{"base_url": "https://example.test"}},
	}}
	repo := &workspaceSearchSettingsRepoStub{}
	service := NewWorkspaceSearchSettingsService(repo, resolver)
	got, err := service.Update(context.Background(), workspaceID, value.RoleAdmin, UpdateWorkspaceSearchSettingsInput{
		RerankEnabled: true, ModelID: modelID, CandidateTopK: 80, FailureMode: value.RerankFailureFail,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rerank == nil || got.Rerank.ModelID != modelID || got.Rerank.ProviderID != providerID || got.Rerank.CandidateTopK != 80 || got.Rerank.FailureMode != value.RerankFailureFail || repo.settings.Rerank.ModelConfigHash == "" {
		t.Fatalf("snapshot = %#v", got)
	}
}

func TestWorkspaceSearchSettingsServiceMissingRowIsDisabled(t *testing.T) {
	t.Parallel()
	workspaceID := uuid.New()
	got, err := NewWorkspaceSearchSettingsService(&workspaceSearchSettingsRepoStub{}, &workspaceSearchModelResolverStub{}).Get(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != workspaceID || got.Rerank != nil {
		t.Fatalf("default = %#v", got)
	}
}
