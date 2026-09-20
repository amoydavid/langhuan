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
	// KbSourceID 是源级任务目标（kb_sources.id，迁移 000025 的 jobs 第五分支）：
	// 仅 source_sync/source_cleanup 可携带，且不得与 document/revision/generation 同现。
	// 零值表示 KB 级目标（飞书路径语义不变）。
	KbSourceID    uuid.UUID
	Type          string
	Status        value.JobStatus
	ExternalJobID string
	Payload       map[string]any
	ErrorMessage  string
}

type Job struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	IndexGenerationID  uuid.UUID
	SourceConnectionID uuid.UUID
	// KbSourceID 是源级任务目标；零值表示 KB 级目标（飞书任务）。
	KbSourceID    uuid.UUID
	Type          string
	Status        value.JobStatus
	Attempts      int
	ExternalJobID string
	Payload       map[string]any
	ErrorMessage  string
	ErrorClass    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SourceSyncJobType 是知识库级同步任务（仅关联 KB，不带 document/generation）的类型。
const SourceSyncJobType = "source_sync"

// SourceCleanupJobType 是知识库级来源清理任务（仅关联 KB，不带 document/generation）的类型。
const SourceCleanupJobType = "source_cleanup"

func NewJob(input NewJobInput) (*Job, error) {
	jobType := strings.TrimSpace(input.Type)
	if jobType == "" {
		return nil, fmt.Errorf("%w: 任务类型不能为空", domainerrors.ErrValidation)
	}
	if input.DocumentID != uuid.Nil && input.IndexGenerationID != uuid.Nil {
		return nil, fmt.Errorf("%w: Job 不能同时关联 Document 与 Generation", domainerrors.ErrValidation)
	}
	// source_sync / source_cleanup 任务仅关联 KB，允许 document/revision/generation 三者皆 nil。
	if !kbOnlyJobType(jobType) && input.DocumentID == uuid.Nil && input.IndexGenerationID == uuid.Nil {
		return nil, fmt.Errorf("%w: document_id/index_generation_id 必须提供一个", domainerrors.ErrValidation)
	}
	// 源级目标（迁移 000025 第五分支）：仅 source_sync/source_cleanup 可携带，
	// 且与 document/revision/generation 目标互斥。
	if input.KbSourceID != uuid.Nil {
		if !kbOnlyJobType(jobType) {
			return nil, fmt.Errorf("%w: 仅 source_sync/source_cleanup 任务可携带源级目标", domainerrors.ErrValidation)
		}
		if input.DocumentID != uuid.Nil || input.DocumentRevisionID != uuid.Nil || input.IndexGenerationID != uuid.Nil {
			return nil, fmt.Errorf("%w: 源级目标不能与 document/revision/generation 目标同现", domainerrors.ErrValidation)
		}
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
		KbSourceID:         input.KbSourceID,
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

// kbOnlyJobType 报告该任务类型是否仅关联知识库（不带 document/revision/generation）。
// 这类任务允许三者全 nil，用于知识库级来源同步与清理。
func kbOnlyJobType(jobType string) bool {
	return jobType == SourceSyncJobType || jobType == SourceCleanupJobType
}
