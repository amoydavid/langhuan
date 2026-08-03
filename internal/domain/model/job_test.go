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
