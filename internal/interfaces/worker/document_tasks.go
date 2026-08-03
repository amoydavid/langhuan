package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/dajee/langhuan/internal/application/dto"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
	"github.com/dajee/langhuan/internal/ports/queue"
)

const (
	TaskDocumentParseStart = "document_parse_start"
	TaskDocumentParsePoll  = "document_parse_poll"
	TaskDocumentIndex      = "document_index"
)

// DocumentTaskPayload carries the complete immutable lineage of one document task.
type DocumentTaskPayload struct {
	WorkspaceID        uuid.UUID `json:"workspace_id"`
	KnowledgeBaseID    uuid.UUID `json:"knowledge_base_id"`
	DocumentID         uuid.UUID `json:"document_id"`
	DocumentRevisionID uuid.UUID `json:"document_revision_id"`
	GenerationID       uuid.UUID `json:"generation_id,omitempty"`
	ChunkSetID         uuid.UUID `json:"chunk_set_id,omitempty"`
	JobID              uuid.UUID `json:"job_id"`
}

// DocumentTaskTx is the application-owned tenant transaction contract used by workers.
type DocumentTaskTx = appservice.DocumentTaskTx

// DocumentTaskStore is the application-owned persistence boundary used by workers.
type DocumentTaskStore = appservice.DocumentTaskStore

type DocumentPipeline interface {
	RunParse(ctx context.Context, workspaceID, revisionID uuid.UUID) error
	RunChunk(ctx context.Context, workspaceID, revisionID, generationID uuid.UUID) (uuid.UUID, error)
	RunIndex(ctx context.Context, workspaceID, generationID, chunkSetID uuid.UUID) ([]*model.RetrievalEntry, error)
}

type DocumentHandlers struct {
	Store    DocumentTaskStore
	Queue    queue.JobQueue
	Pipeline DocumentPipeline
}

func RegisterDocumentHandlers(mux *asynq.ServeMux, handlers DocumentHandlers) {
	mux.HandleFunc(TaskDocumentParseStart, handlers.HandleDocumentParseStart)
	mux.HandleFunc(TaskDocumentParsePoll, handlers.HandleDocumentParsePoll)
	mux.HandleFunc(TaskDocumentIndex, handlers.HandleDocumentIndex)
}

func (h DocumentHandlers) HandleDocumentParseStart(ctx context.Context, task *asynq.Task) error {
	payload, state, err := h.loadTask(ctx, task)
	if err != nil {
		return err
	}
	if state.job.Status == value.JobStatusCompleted {
		return nil
	}
	if err := h.Store.MarkRunning(ctx, payload.WorkspaceID, payload.JobID); err != nil {
		return err
	}
	if state.published {
		return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
	}
	nextType := TaskDocumentParsePoll
	if state.revision.Kind == value.DocumentKindFAQ || state.revision.Status == value.DocumentRevisionReady {
		nextType = TaskDocumentIndex
	}
	if _, err := h.createAndEnqueue(ctx, payload, nextType, uuid.Nil); err != nil {
		return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
	}
	return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
}

func (h DocumentHandlers) HandleDocumentParsePoll(ctx context.Context, task *asynq.Task) error {
	payload, state, err := h.loadTask(ctx, task)
	if err != nil {
		return err
	}
	if state.job.Status == value.JobStatusCompleted {
		return nil
	}
	if err := h.Store.MarkRunning(ctx, payload.WorkspaceID, payload.JobID); err != nil {
		return err
	}
	if state.published {
		return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
	}
	if state.revision.Kind == value.DocumentKindFAQ {
		if state.revision.Status != value.DocumentRevisionReady {
			return h.failPipelineRun(ctx, payload, fmt.Errorf("%w: FAQ Revision 尚未就绪", domainerrors.ErrValidation))
		}
	} else if state.revision.Status != value.DocumentRevisionReady {
		if err := h.Pipeline.RunParse(ctx, payload.WorkspaceID, payload.DocumentRevisionID); err != nil {
			return h.failPipelineRun(ctx, payload, err)
		}
	}
	if _, err := h.createAndEnqueue(ctx, payload, TaskDocumentIndex, uuid.Nil); err != nil {
		return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
	}
	return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
}

func (h DocumentHandlers) HandleDocumentIndex(ctx context.Context, task *asynq.Task) error {
	payload, state, err := h.loadTask(ctx, task)
	if err != nil {
		return err
	}
	if state.job.Status == value.JobStatusCompleted {
		return nil
	}
	if err := h.Store.MarkRunning(ctx, payload.WorkspaceID, payload.JobID); err != nil {
		return err
	}
	if state.published {
		return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
	}
	chunkSetID := payload.ChunkSetID
	if chunkSetID == uuid.Nil {
		chunkSetID, err = h.Pipeline.RunChunk(
			ctx, payload.WorkspaceID, payload.DocumentRevisionID, payload.GenerationID,
		)
		if err != nil {
			return h.failPipelineRun(ctx, payload, err)
		}
	}
	if _, err := h.Pipeline.RunIndex(ctx, payload.WorkspaceID, payload.GenerationID, chunkSetID); err != nil {
		return h.failPipelineRun(ctx, payload, err)
	}
	return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
}

type loadedDocumentTask struct {
	job       *dto.Job
	revision  *model.DocumentRevision
	published bool
}

func (h DocumentHandlers) loadTask(
	ctx context.Context,
	task *asynq.Task,
) (DocumentTaskPayload, loadedDocumentTask, error) {
	payload, err := decodeDocumentTaskPayload(task)
	if err != nil {
		return payload, loadedDocumentTask{}, err
	}
	if h.Store == nil {
		return payload, loadedDocumentTask{}, fmt.Errorf("文档任务 Store 不能为空")
	}
	var state loadedDocumentTask
	err = h.Store.WithinWorkspace(ctx, payload.WorkspaceID, func(txCtx context.Context, tx DocumentTaskTx) error {
		job, err := tx.GetJob(txCtx, payload.JobID)
		if err != nil {
			return err
		}
		revision, err := tx.GetRevision(txCtx, payload.DocumentRevisionID)
		if err != nil {
			return err
		}
		if err := validateDocumentTaskLineage(task.Type(), payload, job, revision); err != nil {
			return err
		}
		published, err := tx.IsRevisionPublished(txCtx, payload.GenerationID, payload.DocumentRevisionID)
		if err != nil {
			return err
		}
		state = loadedDocumentTask{job: job, revision: revision, published: published}
		return nil
	})
	return payload, state, err
}

func validateDocumentTaskLineage(
	taskType string,
	payload DocumentTaskPayload,
	job *dto.Job,
	revision *model.DocumentRevision,
) error {
	if job == nil || revision == nil {
		return fmt.Errorf("%w: 文档任务 lineage 记录为空", domainerrors.ErrValidation)
	}
	if job.ID != payload.JobID || job.WorkspaceID != payload.WorkspaceID ||
		job.KnowledgeBaseID != payload.KnowledgeBaseID || job.DocumentID != payload.DocumentID ||
		job.DocumentRevisionID != payload.DocumentRevisionID || job.Type != taskType {
		return fmt.Errorf("%w: Job 与任务 payload lineage 不一致", domainerrors.ErrValidation)
	}
	if revision.ID != payload.DocumentRevisionID || revision.WorkspaceID != payload.WorkspaceID ||
		revision.KnowledgeBaseID != payload.KnowledgeBaseID || revision.DocumentID != payload.DocumentID {
		return fmt.Errorf("%w: DocumentRevision 与任务 payload lineage 不一致", domainerrors.ErrValidation)
	}
	if job.IndexGenerationID != uuid.Nil && job.IndexGenerationID != payload.GenerationID {
		return fmt.Errorf("%w: Job 与任务 Generation lineage 不一致", domainerrors.ErrValidation)
	}
	generationID, err := generationIDFromJobPayload(job.Payload)
	if err != nil || generationID != payload.GenerationID {
		return fmt.Errorf("%w: Job payload 与任务 Generation lineage 不一致", domainerrors.ErrValidation)
	}
	return nil
}

func generationIDFromJobPayload(payload map[string]any) (uuid.UUID, error) {
	raw, ok := payload["index_generation_id"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return uuid.Nil, fmt.Errorf("index_generation_id 缺失")
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("index_generation_id 无效")
	}
	return id, nil
}

func (h DocumentHandlers) createAndEnqueue(
	ctx context.Context,
	source DocumentTaskPayload,
	typ string,
	chunkSetID uuid.UUID,
) (*dto.Job, error) {
	jobPayload := map[string]any{
		"workspace_id": source.WorkspaceID.String(), "knowledge_base_id": source.KnowledgeBaseID.String(),
		"document_id": source.DocumentID.String(), "document_revision_id": source.DocumentRevisionID.String(),
		"index_generation_id": source.GenerationID.String(),
	}
	job, err := h.Store.CreateNextForRevision(
		ctx, source.WorkspaceID, source.KnowledgeBaseID, source.DocumentID,
		source.DocumentRevisionID, source.GenerationID, typ, jobPayload,
	)
	if err != nil {
		return nil, err
	}
	queuePayload, err := json.Marshal(DocumentTaskPayload{
		WorkspaceID: source.WorkspaceID, KnowledgeBaseID: source.KnowledgeBaseID,
		DocumentID: source.DocumentID, DocumentRevisionID: source.DocumentRevisionID,
		GenerationID: source.GenerationID, ChunkSetID: chunkSetID, JobID: job.ID,
	})
	if err != nil {
		return nil, h.failCreatedJob(ctx, source.WorkspaceID, job.ID, err)
	}
	if _, err := h.Queue.Enqueue(ctx, queue.JobRequest{
		Type: typ, Payload: queuePayload,
		TaskID: queue.DocumentTaskID(typ, source.WorkspaceID, source.DocumentRevisionID, source.GenerationID),
	}); err != nil {
		return nil, h.failCreatedJob(ctx, source.WorkspaceID, job.ID, err)
	}
	return job, nil
}

func (h DocumentHandlers) failPipelineRun(ctx context.Context, payload DocumentTaskPayload, cause error) error {
	permanent := isPermanentDocumentTaskError(cause)
	retryCount, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	terminal := permanent || (retryOK && maxOK && retryCount >= maxRetry)
	if terminal {
		if err := h.Store.FailTask(
			ctx, payload.WorkspaceID, payload.JobID, payload.DocumentRevisionID,
			documentTaskErrorClass(cause), cause.Error(),
		); err != nil {
			return errors.Join(cause, fmt.Errorf("持久化文档任务失败状态失败: %w", err))
		}
		if permanent {
			return errors.Join(asynq.SkipRetry, cause)
		}
		return cause
	}
	if err := h.Store.MarkFailed(ctx, payload.WorkspaceID, payload.JobID, cause.Error()); err != nil {
		return errors.Join(cause, fmt.Errorf("标记任务失败也失败: %w", err))
	}
	return cause
}

func isPermanentDocumentTaskError(err error) bool {
	return errors.Is(err, parserport.ErrUnsupportedFileType) ||
		errors.Is(err, parserport.ErrInvalidEncoding) ||
		errors.Is(err, parserport.ErrInvalidDocument) ||
		errors.Is(err, parserport.ErrEmptyDocument) ||
		errors.Is(err, parserport.ErrParseLimitExceeded) ||
		errors.Is(err, domainerrors.ErrValidation)
}

func documentTaskErrorClass(err error) string {
	switch {
	case errors.Is(err, parserport.ErrUnsupportedFileType):
		return "unsupported_file_type"
	case errors.Is(err, parserport.ErrInvalidEncoding):
		return "invalid_encoding"
	case errors.Is(err, parserport.ErrInvalidDocument):
		return "invalid_document"
	case errors.Is(err, parserport.ErrEmptyDocument):
		return "empty_document"
	case errors.Is(err, parserport.ErrParseLimitExceeded):
		return "parse_limit_exceeded"
	case errors.Is(err, domainerrors.ErrValidation):
		return "validation_error"
	default:
		return "pipeline_error"
	}
}

func (h DocumentHandlers) failCreatedJob(
	ctx context.Context,
	workspaceID, jobID uuid.UUID,
	cause error,
) error {
	if markErr := h.Store.MarkFailed(ctx, workspaceID, jobID, cause.Error()); markErr != nil {
		return errors.Join(cause, fmt.Errorf("标记后续任务失败也失败: %w", markErr))
	}
	return cause
}

func (h DocumentHandlers) failRunningJob(ctx context.Context, workspaceID, jobID uuid.UUID, cause error) error {
	if markErr := h.Store.MarkFailed(ctx, workspaceID, jobID, cause.Error()); markErr != nil {
		return errors.Join(cause, fmt.Errorf("标记任务失败也失败: %w", markErr))
	}
	return cause
}

func (h DocumentHandlers) succeedRunningJob(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	if err := h.Store.MarkSucceeded(ctx, workspaceID, jobID); err != nil {
		return h.failRunningJob(ctx, workspaceID, jobID, err)
	}
	return nil
}

func decodeDocumentTaskPayload(task *asynq.Task) (DocumentTaskPayload, error) {
	var payload DocumentTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return payload, fmt.Errorf("解析文档任务 payload 失败: %w", err)
	}
	checks := []struct {
		name string
		id   uuid.UUID
	}{
		{name: "workspace_id", id: payload.WorkspaceID},
		{name: "knowledge_base_id", id: payload.KnowledgeBaseID},
		{name: "document_id", id: payload.DocumentID},
		{name: "document_revision_id", id: payload.DocumentRevisionID},
		{name: "generation_id", id: payload.GenerationID},
		{name: "job_id", id: payload.JobID},
	}
	for _, check := range checks {
		if check.id == uuid.Nil {
			return payload, fmt.Errorf("文档任务 %s 不能为空", check.name)
		}
	}
	return payload, nil
}
