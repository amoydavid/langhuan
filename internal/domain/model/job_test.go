package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewJobValidatesInput(t *testing.T) {
	valid := NewJobInput{
		DocumentID: uuid.New(),
		Type:       "parse",
		Status:     value.JobStatusQueued,
	}

	tests := []struct {
		name  string
		input NewJobInput
	}{
		{name: "document", input: func() NewJobInput { in := valid; in.DocumentID = uuid.Nil; return in }()},
		{name: "type", input: func() NewJobInput { in := valid; in.Type = ""; return in }()},
		{name: "status", input: func() NewJobInput { in := valid; in.Status = ""; return in }()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewJob(tt.input); !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestNewJobAllowsSourceSyncWithOnlyKnowledgeBase(t *testing.T) {
	job, err := NewJob(NewJobInput{
		WorkspaceID:     uuid.New(),
		KnowledgeBaseID: uuid.New(),
		Type:            "source_sync",
		Status:          value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("expected nil err for source_sync with only knowledge_base_id, got %v", err)
	}
	if job.Type != "source_sync" {
		t.Fatalf("type = %q, want source_sync", job.Type)
	}
}

func TestNewJobStillRejectsNonSourceSyncWithAllNilTargets(t *testing.T) {
	_, err := NewJob(NewJobInput{
		WorkspaceID:     uuid.New(),
		KnowledgeBaseID: uuid.New(),
		Type:            "document_parse_start",
		Status:          value.JobStatusPending,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("expected ErrValidation for non-source_sync with all-nil targets, got %v", err)
	}
}

// TestNewJobAllowsSourceCleanupKBOnly 验证 source_cleanup 任务类型与 source_sync 一样
// 仅关联 KB（document/revision/generation 三者全 nil 合法）。
func TestNewJobAllowsSourceCleanupKBOnly(t *testing.T) {
	workspaceID, kbID := uuid.New(), uuid.New()
	job, err := NewJob(NewJobInput{
		WorkspaceID:     workspaceID,
		KnowledgeBaseID: kbID,
		Type:            SourceCleanupJobType,
		Status:          value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("expected nil err for source_cleanup KB-only, got %v", err)
	}
	if job.Type != SourceCleanupJobType {
		t.Fatalf("type = %q, want %q", job.Type, SourceCleanupJobType)
	}
	if job.DocumentID != uuid.Nil || job.DocumentRevisionID != uuid.Nil || job.IndexGenerationID != uuid.Nil {
		t.Fatalf("source_cleanup job should not carry doc/revision/generation: %#v", job)
	}
}

// TestNewJobRejectsOtherTypesWithAllNilTargets 验证 source_sync/source_cleanup 之外
// 的类型在全 nil 时仍被拒绝。
func TestNewJobRejectsOtherTypesWithAllNilTargets(t *testing.T) {
	for _, jobType := range []string{"parse", "index_build", "reparse"} {
		_, err := NewJob(NewJobInput{
			WorkspaceID:     uuid.New(),
			KnowledgeBaseID: uuid.New(),
			Type:            jobType,
			Status:          value.JobStatusPending,
		})
		if !errors.Is(err, domainerrors.ErrValidation) {
			t.Fatalf("type %q with all-nil targets should be rejected; got err = %v", jobType, err)
		}
	}
}

func TestNewJobDefaultsAttemptsAndPayload(t *testing.T) {
	job, err := NewJob(NewJobInput{
		DocumentID: uuid.New(),
		Type:       "parse",
		Status:     value.JobStatusQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == uuid.Nil {
		t.Fatal("job id should not be nil")
	}
	if job.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0", job.Attempts)
	}
	if job.Payload == nil {
		t.Fatal("payload should default to an empty map")
	}
}
