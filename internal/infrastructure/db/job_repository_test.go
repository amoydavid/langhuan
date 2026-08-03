package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestJobRowMappingPreservesStatusFields(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	job := &model.Job{
		ID:                 uuid.New(),
		WorkspaceID:        uuid.New(),
		KnowledgeBaseID:    uuid.New(),
		DocumentID:         uuid.New(),
		DocumentRevisionID: uuid.New(),
		Type:               "parse",
		Status:             value.JobStatusFailed,
		Attempts:           2,
		ExternalJobID:      "ext-1",
		Payload:            map[string]any{"priority": "high"},
		ErrorMessage:       "failed",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	row := jobToRow(job)
	got := jobFromRow(row)

	if got.ID != job.ID || got.WorkspaceID != job.WorkspaceID || got.DocumentID != job.DocumentID || got.DocumentRevisionID != job.DocumentRevisionID {
		t.Fatalf("identity not preserved: %#v", got)
	}
	if got.Type != job.Type || got.Status != job.Status || got.Attempts != job.Attempts || got.ErrorMessage != job.ErrorMessage {
		t.Fatalf("status fields not preserved: %#v", got)
	}
	if !reflect.DeepEqual(got.Payload, job.Payload) {
		t.Fatalf("payload = %#v", got.Payload)
	}
}
