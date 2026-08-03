package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// JobSummary exposes one safe, readable asynchronous activity item.
type JobSummary struct {
	ID                uuid.UUID       `json:"id"`
	DocumentID        *uuid.UUID      `json:"document_id,omitempty"`
	IndexGenerationID *uuid.UUID      `json:"index_generation_id,omitempty"`
	Status            value.JobStatus `json:"status"`
	ActionLabel       string          `json:"action_label"`
	TargetType        string          `json:"target_type"`
	TargetDisplayName string          `json:"target_display_name"`
	Attempts          int             `json:"attempts"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// JobSummaryPage is one stable cursor page ordered by created_at/id descending.
type JobSummaryPage struct {
	Items      []*JobSummary `json:"items"`
	NextCursor *string       `json:"next_cursor"`
}
