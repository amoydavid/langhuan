package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

type NewJobInput struct {
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	IndexGenerationID  uuid.UUID
	Type               string
	Status             value.JobStatus
	ExternalJobID      string
	Payload            map[string]any
	ErrorMessage       string
}

type Job struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	IndexGenerationID  uuid.UUID
	Type               string
	Status             value.JobStatus
	Attempts           int
	ExternalJobID      string
	Payload            map[string]any
	ErrorMessage       string
	ErrorClass         string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func NewJob(input NewJobInput) (*Job, error) {
	if input.DocumentID == uuid.Nil && input.IndexGenerationID == uuid.Nil {
		return nil, fmt.Errorf("%w: document_id/index_generation_id 必须提供一个", domainerrors.ErrValidation)
	}
	if input.DocumentID != uuid.Nil && input.IndexGenerationID != uuid.Nil {
		return nil, fmt.Errorf("%w: Job 不能同时关联 Document 与 Generation", domainerrors.ErrValidation)
	}
	jobType := strings.TrimSpace(input.Type)
	if jobType == "" {
		return nil, fmt.Errorf("%w: 任务类型不能为空", domainerrors.ErrValidation)
	}
	if input.Status == "" {
		return nil, fmt.Errorf("%w: 任务状态不能为空", domainerrors.ErrValidation)
	}

	payload := input.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	now := time.Now().UTC()
	return &Job{
		ID:                 uuid.New(),
		WorkspaceID:        input.WorkspaceID,
		KnowledgeBaseID:    input.KnowledgeBaseID,
		DocumentID:         input.DocumentID,
		DocumentRevisionID: input.DocumentRevisionID,
		IndexGenerationID:  input.IndexGenerationID,
		Type:               jobType,
		Status:             input.Status,
		Attempts:           0,
		ExternalJobID:      input.ExternalJobID,
		Payload:            payload,
		ErrorMessage:       input.ErrorMessage,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}
