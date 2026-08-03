package db

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestDefaultWorkspaceIDIsStable(t *testing.T) {
	if DefaultWorkspaceID == uuid.Nil {
		t.Fatal("DefaultWorkspaceID should not be nil")
	}
	want := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	if DefaultWorkspaceID != want {
		t.Fatalf("DefaultWorkspaceID = %s, want %s", DefaultWorkspaceID, want)
	}
}

func TestWorkspaceRowMappingPreservesIdentityAndMetadata(t *testing.T) {
	now := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	id := uuid.New()
	ws := &model.Workspace{
		ID:        id,
		Name:      "Acme",
		Metadata:  map[string]any{"owner": "test"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	row := workspaceToRow(ws)
	got := workspaceFromRow(row)

	if got.ID != id || got.CreatedAt != now || got.UpdatedAt != now {
		t.Fatalf("identity/time not preserved: %#v", got)
	}
	if got.Name != ws.Name {
		t.Fatalf("name = %q, want %q", got.Name, ws.Name)
	}
	if !reflect.DeepEqual(got.Metadata, ws.Metadata) {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
}

func TestDefaultWorkspaceRowUsesDefaultID(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	ws := &model.Workspace{
		ID:        DefaultWorkspaceID,
		Name:      "Default",
		Metadata:  map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	row := workspaceToRow(ws)
	got := workspaceFromRow(row)

	if got.ID != DefaultWorkspaceID {
		t.Fatalf("id = %s, want %s", got.ID, DefaultWorkspaceID)
	}
}

func TestWorkspaceRowMappingPreservesSlug(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	ws := &model.Workspace{
		ID:        uuid.New(),
		Name:      "Acme",
		Slug:      "acme",
		Metadata:  map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	row := workspaceToRow(ws)
	if row.Slug != "acme" {
		t.Fatalf("row.Slug = %q, want acme", row.Slug)
	}
	got := workspaceFromRow(row)
	if got.Slug != "acme" {
		t.Fatalf("got.Slug = %q, want acme", got.Slug)
	}
}

func TestWorkspaceRepositoryExposesSlugAndOwnerMethods(t *testing.T) {
	var repo *WorkspaceRepository
	var _ interface {
		GetBySlug(ctx context.Context, slug string) (*model.Workspace, error)
		CreateWithOwner(ctx context.Context, ws *model.Workspace, ownerUserID uuid.UUID) error
	} = repo
}
