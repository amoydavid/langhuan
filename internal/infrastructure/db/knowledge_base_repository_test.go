package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestKnowledgeBaseRowMappingPreservesJSONAndIdentity(t *testing.T) {
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	id := uuid.New()
	workspaceID := uuid.New()
	activeGenerationID := uuid.New()
	kb := &model.KnowledgeBase{
		ID: id, WorkspaceID: workspaceID, Name: "kb", Description: "desc",
		Metadata: map[string]any{"owner": "test"}, ContentVersion: 3,
		ActiveIndexGenerationID: &activeGenerationID, FileTreeRootID: uuid.New(),
		CreatedAt: now, UpdatedAt: now,
	}

	row, err := knowledgeBaseToRow(kb)
	if err != nil {
		t.Fatal(err)
	}
	got, err := knowledgeBaseFromRow(row)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != id || got.CreatedAt != now || got.UpdatedAt != now {
		t.Fatalf("identity/time not preserved: %#v", got)
	}
	if got.WorkspaceID != workspaceID {
		t.Fatalf("workspace_id = %s, want %s", got.WorkspaceID, workspaceID)
	}
	if got.ContentVersion != 3 || got.ActiveIndexGenerationID == nil || *got.ActiveIndexGenerationID != activeGenerationID || got.FileTreeRootID != kb.FileTreeRootID {
		t.Fatalf("control fields = %#v", got)
	}
	if !reflect.DeepEqual(got.Metadata, kb.Metadata) {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
}
