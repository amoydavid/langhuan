package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

const (
	defaultKnowledgeBaseJobLimit = 20
	maxKnowledgeBaseJobLimit     = 100
)

// KnowledgeBaseJobFacts is one safe-query source row. Payload and external IDs are deliberately absent.
type KnowledgeBaseJobFacts struct {
	ID                uuid.UUID
	DocumentID        *uuid.UUID
	IndexGenerationID *uuid.UUID
	Type              string
	Status            value.JobStatus
	TargetType        string
	TargetDisplayName string
	TargetCreatedAt   *time.Time
	TargetModelName   string
	Attempts          int
	ErrorClass        string
	ErrorMessage      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// KnowledgeBaseJobFactsFilter is the repository-facing stable seek filter.
type KnowledgeBaseJobFactsFilter struct {
	DocumentID      *uuid.UUID
	Status          value.JobStatus
	BeforeCreatedAt *time.Time
	BeforeID        *uuid.UUID
	Limit           int
}

// JobListFilter is the protocol-neutral workbench Job filter.
type JobListFilter struct {
	DocumentID *uuid.UUID
	Status     value.JobStatus
	Cursor     string
	Limit      int
}

// ListJobs returns a stable seek page with readable actions and targets.
func (s *KnowledgeBaseSummaryService) ListJobs(ctx context.Context, access value.ResourceAccess, knowledgeBaseID uuid.UUID, filter JobListFilter) (*dto.JobSummaryPage, error) {
	workspaceID := access.WorkspaceID
	if s.store == nil {
		return nil, fmt.Errorf("%w: KnowledgeBase Job list 参数无效", domainerrors.ErrValidation)
	}
	if err := validateResourceAccess(access, workspaceID, knowledgeBaseID); err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit == 0 {
		limit = defaultKnowledgeBaseJobLimit
	}
	if limit < 1 || limit > maxKnowledgeBaseJobLimit || (filter.DocumentID != nil && *filter.DocumentID == uuid.Nil) || !validJobSummaryStatus(filter.Status) {
		return nil, fmt.Errorf("%w: KnowledgeBase Job filter 无效", domainerrors.ErrValidation)
	}
	repositoryFilter := KnowledgeBaseJobFactsFilter{DocumentID: filter.DocumentID, Status: filter.Status, Limit: limit + 1}
	if strings.TrimSpace(filter.Cursor) != "" {
		cursor, err := decodeKnowledgeBaseJobCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: Job cursor 无效", domainerrors.ErrValidation)
		}
		repositoryFilter.BeforeCreatedAt = &cursor.CreatedAt
		repositoryFilter.BeforeID = &cursor.ID
	}
	facts, err := s.store.ListKnowledgeBaseJobFacts(ctx, workspaceID, knowledgeBaseID, repositoryFilter)
	if err != nil {
		return nil, err
	}
	pageSize := len(facts)
	if pageSize > limit {
		pageSize = limit
	}
	items := make([]*dto.JobSummary, 0, pageSize)
	for index := 0; index < pageSize; index++ {
		items = append(items, jobSummary(facts[index]))
	}
	var nextCursor *string
	if len(facts) > limit && pageSize > 0 {
		encoded, err := encodeKnowledgeBaseJobCursor(knowledgeBaseJobCursor{CreatedAt: facts[pageSize-1].CreatedAt, ID: facts[pageSize-1].ID})
		if err != nil {
			return nil, fmt.Errorf("编码 Job cursor 失败: %w", err)
		}
		nextCursor = &encoded
	}
	return &dto.JobSummaryPage{Items: items, NextCursor: nextCursor}, nil
}

func jobSummary(facts KnowledgeBaseJobFacts) *dto.JobSummary {
	targetName := facts.TargetDisplayName
	if facts.TargetType == "generation" {
		modelName := strings.TrimSpace(facts.TargetModelName)
		if modelName == "" {
			modelName = "未命名模型"
		}
		if facts.TargetCreatedAt != nil {
			targetName = fmt.Sprintf("%s · %s", facts.TargetCreatedAt.Format("2006-01-02 15:04"), modelName)
		}
	}
	return &dto.JobSummary{
		ID: facts.ID, DocumentID: facts.DocumentID, IndexGenerationID: facts.IndexGenerationID,
		Status: facts.Status, ActionLabel: jobActionLabel(facts.Type), TargetType: facts.TargetType,
		TargetDisplayName: readableResourceName(facts.TargetType, targetName), Attempts: facts.Attempts,
		ErrorMessage: safeJobError(facts), CreatedAt: facts.CreatedAt, UpdatedAt: facts.UpdatedAt,
	}
}

func jobActionLabel(jobType string) string {
	switch jobType {
	case "document_parse_start":
		return "导入文件"
	case "document_index":
		return "更新 FAQ"
	case "chunk_revision_index":
		return "修订分块"
	case "index_generation_build":
		return "构建索引版本"
	default:
		return "处理任务"
	}
}

func safeJobError(facts KnowledgeBaseJobFacts) string {
	if strings.TrimSpace(facts.ErrorClass) == "" && strings.TrimSpace(facts.ErrorMessage) == "" {
		return ""
	}
	switch facts.ErrorClass {
	case "invalid_document", "validation_error":
		return "任务参数无效，请检查相关内容。"
	case "provider_error", "embedding_error", "parse_error":
		return "外部处理服务返回错误，请检查模型或解析配置。"
	case "enqueue_error":
		return "任务暂未成功进入队列，请稍后重新执行相关操作。"
	case "generation_stale":
		return "内容已更新，此索引版本需要重新构建。"
	default:
		return "任务执行失败，请检查相关资源后重试。"
	}
}

func validJobSummaryStatus(status value.JobStatus) bool {
	switch status {
	case "", value.JobStatusPending, value.JobStatusQueued, value.JobStatusRunning,
		value.JobStatusCompleted, value.JobStatusSucceeded, value.JobStatusFailed, value.JobStatusCancelled:
		return true
	default:
		return false
	}
}

type knowledgeBaseJobCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

func encodeKnowledgeBaseJobCursor(cursor knowledgeBaseJobCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeKnowledgeBaseJobCursor(input string) (knowledgeBaseJobCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(input))
	if err != nil {
		return knowledgeBaseJobCursor{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor knowledgeBaseJobCursor
	if err := decoder.Decode(&cursor); err != nil {
		return knowledgeBaseJobCursor{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return knowledgeBaseJobCursor{}, fmt.Errorf("cursor 包含多余内容")
	}
	if cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return knowledgeBaseJobCursor{}, fmt.Errorf("cursor 字段为空")
	}
	return cursor, nil
}
