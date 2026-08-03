package db

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestModelProviderRowRoundTripPreservesScopeJSONAndCiphertext(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	workspaceID, actorID := uuid.New(), uuid.New()
	want := &model.ModelProvider{
		ID:                    uuid.New(),
		Scope:                 value.ModelScopeWorkspace,
		WorkspaceID:           &workspaceID,
		Name:                  "openai-prod",
		DisplayName:           "OpenAI 生产",
		Description:           "desc",
		Provider:              "openai",
		Config:                map[string]any{"timeout_seconds": float64(60)},
		CredentialsCiphertext: []byte{1, 2, 3, 4},
		Status:                value.ModelStatusActive,
		CreatedBy:             &actorID,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	row, err := modelProviderToRow(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := modelProviderFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Scope != want.Scope || got.WorkspaceID == nil || *got.WorkspaceID != workspaceID {
		t.Fatalf("identity = %#v", got)
	}
	if !reflect.DeepEqual(got.Config, want.Config) || !bytes.Equal(got.CredentialsCiphertext, want.CredentialsCiphertext) {
		t.Fatalf("config/ciphertext = %#v / %x", got.Config, got.CredentialsCiphertext)
	}

	row.Config["timeout_seconds"] = float64(1)
	row.CredentialsCiphertext[0] = 9
	if got.Config["timeout_seconds"] != float64(60) || got.CredentialsCiphertext[0] != 1 {
		t.Fatal("domain model aliases mutable row data")
	}
}

func TestModelProviderFromRowRejectsUnknownScopeAndStatus(t *testing.T) {
	t.Parallel()

	base := ModelProviderRow{ID: uuid.New(), Scope: "platform", Name: "provider", Provider: "openai", Config: JSONMap{}, Status: "active"}
	badScope := base
	badScope.Scope = "organization"
	if _, err := modelProviderFromRow(&badScope); err == nil {
		t.Fatal("expected unknown scope error")
	}
	badStatus := base
	badStatus.Status = "deleted"
	if _, err := modelProviderFromRow(&badStatus); err == nil {
		t.Fatal("expected unknown status error")
	}
}
