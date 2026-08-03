package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type Job struct {
	ID                 uuid.UUID       `json:"id"`
	WorkspaceID        uuid.UUID       `json:"workspace_id"`
	KnowledgeBaseID    uuid.UUID       `json:"knowledge_base_id"`
	DocumentID         uuid.UUID       `json:"document_id"`
	DocumentRevisionID uuid.UUID       `json:"document_revision_id"`
	IndexGenerationID  uuid.UUID       `json:"index_generation_id"`
	Type               string          `json:"type"`
	Status             value.JobStatus `json:"status"`
	Attempts           int             `json:"attempts"`
	ExternalJobID      string          `json:"external_job_id"`
	Payload            map[string]any  `json:"payload"`
	ErrorMessage       string          `json:"error_message"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func JobFromModel(job *model.Job) *Job {
	if job == nil {
		return nil
	}
	return &Job{
		ID:                 job.ID,
		WorkspaceID:        job.WorkspaceID,
		KnowledgeBaseID:    job.KnowledgeBaseID,
		DocumentID:         job.DocumentID,
		DocumentRevisionID: job.DocumentRevisionID,
		IndexGenerationID:  job.IndexGenerationID,
		Type:               job.Type,
		Status:             job.Status,
		Attempts:           job.Attempts,
		ExternalJobID:      job.ExternalJobID,
		Payload:            job.Payload,
		ErrorMessage:       job.ErrorMessage,
		CreatedAt:          job.CreatedAt,
		UpdatedAt:          job.UpdatedAt,
	}
}
