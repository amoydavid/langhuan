package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
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
	SourceConnectionID uuid.UUID
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
	SourceConnectionID uuid.UUID
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

// SourceSyncJobType 是知识库级同步任务（仅关联 KB，不带 document/generation）的类型。
const SourceSyncJobType = "source_sync"

func NewJob(input NewJobInput) (*Job, error) {
	jobType := strings.TrimSpace(input.Type)
	if jobType == "" {
		return nil, fmt.Errorf("%w: 任务类型不能为空", domainerrors.ErrValidation)
	}
	if input.DocumentID != uuid.Nil && input.IndexGenerationID != uuid.Nil {
		return nil, fmt.Errorf("%w: Job 不能同时关联 Document 与 Generation", domainerrors.ErrValidation)
	}
	// source_sync 任务仅关联 KB，允许 document/revision/generation 三者皆 nil。
	if jobType != SourceSyncJobType && input.DocumentID == uuid.Nil && input.IndexGenerationID == uuid.Nil {
		return nil, fmt.Errorf("%w: document_id/index_generation_id 必须提供一个", domainerrors.ErrValidation)
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
		ID:                 id.New(),
		WorkspaceID:        input.WorkspaceID,
		KnowledgeBaseID:    input.KnowledgeBaseID,
		DocumentID:         input.DocumentID,
		DocumentRevisionID: input.DocumentRevisionID,
		IndexGenerationID:  input.IndexGenerationID,
		SourceConnectionID: input.SourceConnectionID,
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
