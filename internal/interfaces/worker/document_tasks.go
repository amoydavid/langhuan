package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/pipeline"
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

// AsyncParseSupport 是 DocumentPipeline 的可选扩展，支持异步解析完成。
type AsyncParseSupport interface {
	CompleteAsyncParse(
		ctx context.Context,
		workspaceID, revisionID uuid.UUID,
		parsed *parserport.ParsedDocument,
		assetResolver *pipeline.AssetResolver,
	) error
}

// ParserRegistry 按 filetype 查找 parser，供 worker 检测是否为异步 parser。
type ParserRegistry interface {
	Get(fileType string) (parserport.DocumentParser, error)
}

type DocumentHandlers struct {
	Store             DocumentTaskStore
	Queue             queue.JobQueue
	Pipeline          DocumentPipeline
	ParserRegistry    ParserRegistry
	AssetStoreFactory func(workspaceID, knowledgeBaseID, documentID, revisionID uuid.UUID) *pipeline.AssetResolver
	Logger            *slog.Logger
}

// logger 返回注入的 logger，未注入时回退到 slog.Default（测试友好）。
func (h DocumentHandlers) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// lineageAttrs 返回文档任务 lineage 的日志字段，用于追踪单条文档流水线。
func lineageAttrs(payload DocumentTaskPayload) []slog.Attr {
	return []slog.Attr{
		slog.String("workspace_id", payload.WorkspaceID.String()),
		slog.String("document_id", payload.DocumentID.String()),
		slog.String("revision_id", payload.DocumentRevisionID.String()),
		slog.String("job_id", payload.JobID.String()),
	}
}

// jobAttrs 返回仅含 workspace/job 的日志字段（用于无法拿到完整 lineage 的辅助路径）。
func jobAttrs(workspaceID, jobID uuid.UUID) []slog.Attr {
	return []slog.Attr{
		slog.String("workspace_id", workspaceID.String()),
		slog.String("job_id", jobID.String()),
	}
}

func RegisterDocumentHandlers(mux *asynq.ServeMux, handlers DocumentHandlers) {
	mux.HandleFunc(TaskDocumentParseStart, handlers.HandleDocumentParseStart)
	mux.HandleFunc(TaskDocumentParsePoll, handlers.HandleDocumentParsePoll)
	mux.HandleFunc(TaskDocumentIndex, handlers.HandleDocumentIndex)
}

func (h DocumentHandlers) HandleDocumentParseStart(ctx context.Context, task *asynq.Task) error {
	payload, state, err := h.loadTask(ctx, task)
	if err != nil {
		h.logger().LogAttrs(ctx, slog.LevelError, "文档解析任务加载失败",
			append(lineageAttrs(payload), slog.String("error", err.Error()))...)
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

	// 检查是否为异步 parser（如 MinerU PDF）
	if h.ParserRegistry != nil && state.revision.FileType != "" {
		if parser, pErr := h.ParserRegistry.Get(state.revision.FileType); pErr == nil {
			if asyncParser, ok := parser.(parserport.AsyncDocumentParser); ok {
				// 异步路径：调 Start，把 externalJobId 存入 job payload
				start, sErr := asyncParser.Start(ctx, parserport.AsyncParseInput{
					WorkspaceID:     payload.WorkspaceID,
					KnowledgeBaseID: payload.KnowledgeBaseID,
					DocumentID:      payload.DocumentID,
					RevisionID:      payload.DocumentRevisionID,
					JobID:           payload.JobID,
					FileType:        state.revision.FileType,
					Title:           state.revision.OriginalFilename,
					RawStorageKey:   state.revision.RawStorageKey,
					ContentType:     state.revision.ContentType,
				})
				if sErr != nil {
					return h.failPipelineRun(ctx, payload, sErr)
				}
				h.logger().LogAttrs(ctx, slog.LevelInfo, "异步解析任务已提交（MinerU）",
					append(lineageAttrs(payload),
						slog.String("file_type", state.revision.FileType),
						slog.String("external_job_id", start.ExternalJobID))...)
				// 异步路径：用 external_job_id 入队 poll 任务
				if _, err := h.createAndEnqueueWithExternalJob(ctx, payload, TaskDocumentParsePoll, start.ExternalJobID, start.Payload); err != nil {
					return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
				}
				return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
			}
		}
	}

	h.logger().LogAttrs(ctx, slog.LevelDebug, "同步解析路径：提交后续任务",
		append(lineageAttrs(payload), slog.String("next_type", nextType))...)
	if _, err := h.createAndEnqueue(ctx, payload, nextType, uuid.Nil); err != nil {
		return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
	}
	return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
}

func (h DocumentHandlers) HandleDocumentParsePoll(ctx context.Context, task *asynq.Task) error {
	payload, state, err := h.loadTask(ctx, task)
	if err != nil {
		h.logger().LogAttrs(ctx, slog.LevelError, "文档轮询任务加载失败",
			append(lineageAttrs(payload), slog.String("error", err.Error()))...)
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
		// 检查是否为异步 parser poll
		externalJobID, _ := state.job.Payload["external_job_id"].(string)
		if externalJobID != "" && h.ParserRegistry != nil {
			if pErr := h.handleAsyncPoll(ctx, payload, state, externalJobID); pErr != nil {
				return pErr
			}
			return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
		}
		// 同步解析路径
		if err := h.Pipeline.RunParse(ctx, payload.WorkspaceID, payload.DocumentRevisionID); err != nil {
			return h.failPipelineRun(ctx, payload, err)
		}
		h.logger().LogAttrs(ctx, slog.LevelInfo, "同步解析完成，提交索引任务", lineageAttrs(payload)...)
	}
	if _, err := h.createAndEnqueue(ctx, payload, TaskDocumentIndex, uuid.Nil); err != nil {
		return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
	}
	return h.succeedRunningJob(ctx, payload.WorkspaceID, payload.JobID)
}

// handleAsyncPoll 处理异步 parser（MinerU）的 poll 逻辑。
func (h DocumentHandlers) handleAsyncPoll(
	ctx context.Context,
	payload DocumentTaskPayload,
	state loadedDocumentTask,
	externalJobID string,
) error {
	parser, err := h.ParserRegistry.Get(state.revision.FileType)
	if err != nil {
		return h.failPipelineRun(ctx, payload, fmt.Errorf("查找异步 parser 失败: %w", err))
	}
	asyncParser, ok := parser.(parserport.AsyncDocumentParser)
	if !ok {
		return h.failPipelineRun(ctx, payload, fmt.Errorf("parser 不支持异步 poll"))
	}

	result, err := asyncParser.Poll(ctx, parserport.AsyncParsePollInput{
		AsyncParseInput: parserport.AsyncParseInput{
			WorkspaceID:     payload.WorkspaceID,
			KnowledgeBaseID: payload.KnowledgeBaseID,
			DocumentID:      payload.DocumentID,
			RevisionID:      payload.DocumentRevisionID,
			JobID:           payload.JobID,
			FileType:        state.revision.FileType,
			Title:           state.revision.OriginalFilename,
			RawStorageKey:   state.revision.RawStorageKey,
			ContentType:     state.revision.ContentType,
		},
		ExternalJobID: externalJobID,
		Payload:       state.job.Payload,
	})
	if err != nil {
		return h.failPipelineRun(ctx, payload, err)
	}

	switch result.Status {
	case parserport.AsyncRunning:
		// 重新入队 poll，带延迟
		h.logger().LogAttrs(ctx, slog.LevelDebug, "异步解析轮询中（MinerU）",
			append(lineageAttrs(payload),
				slog.String("external_job_id", externalJobID),
				slog.Int("poll_count", pollCountFromPayload(result.Payload)),
				slog.Duration("retry_after", result.RetryAfter))...)
		if _, err := h.createAndEnqueueWithExternalJobDelayed(ctx, payload, TaskDocumentParsePoll, externalJobID, result.Payload, queue.Delay(result.RetryAfter)); err != nil {
			return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
		}
		return nil

	case parserport.AsyncFailed:
		h.logger().LogAttrs(ctx, slog.LevelError, "异步解析失败（MinerU）",
			append(lineageAttrs(payload),
				slog.String("external_job_id", externalJobID),
				slog.String("error_code", result.ErrorCode),
				slog.String("error_message", result.ErrorMessage))...)
		return h.failPipelineRun(ctx, payload, fmt.Errorf("%w: %s: %s",
			parserport.ErrAsyncParseFailed, result.ErrorCode, result.ErrorMessage))

	case parserport.AsyncSucceeded:
		markdownChars, assetCandidates := 0, 0
		if result.Document != nil {
			markdownChars = len(result.Document.Markdown)
			assetCandidates = len(result.Document.AssetCandidates)
		}
		h.logger().LogAttrs(ctx, slog.LevelInfo, "异步解析完成（MinerU）",
			append(lineageAttrs(payload),
				slog.String("external_job_id", externalJobID),
				slog.Int("markdown_chars", markdownChars),
				slog.Int("asset_candidates", assetCandidates))...)
		// 完成异步解析：存储 markdown + manifest + 归档资产
		var assetResolver *pipeline.AssetResolver
		if h.AssetStoreFactory != nil {
			assetResolver = h.AssetStoreFactory(payload.WorkspaceID, payload.KnowledgeBaseID, payload.DocumentID, payload.DocumentRevisionID)
		}
		if asyncPipeline, ok := h.Pipeline.(AsyncParseSupport); ok {
			if err := asyncPipeline.CompleteAsyncParse(ctx, payload.WorkspaceID, payload.DocumentRevisionID, result.Document, assetResolver); err != nil {
				return h.failPipelineRun(ctx, payload, err)
			}
		} else {
			return h.failPipelineRun(ctx, payload, fmt.Errorf("pipeline 不支持异步解析完成"))
		}
		// 入队 index
		if _, err := h.createAndEnqueue(ctx, payload, TaskDocumentIndex, uuid.Nil); err != nil {
			return h.failRunningJob(ctx, payload.WorkspaceID, payload.JobID, err)
		}
		return nil

	default:
		return h.failPipelineRun(ctx, payload, fmt.Errorf("未知异步解析状态: %s", result.Status))
	}
}

// pollCountFromPayload 从 job payload 安全提取 poll_count（DB JSON 数字可能为 float64）。
func pollCountFromPayload(payload map[string]any) int {
	if v, ok := payload["poll_count"]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

func (h DocumentHandlers) HandleDocumentIndex(ctx context.Context, task *asynq.Task) error {
	payload, state, err := h.loadTask(ctx, task)
	if err != nil {
		h.logger().LogAttrs(ctx, slog.LevelError, "文档索引任务加载失败",
			append(lineageAttrs(payload), slog.String("error", err.Error()))...)
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
	entries, err := h.Pipeline.RunIndex(ctx, payload.WorkspaceID, payload.GenerationID, chunkSetID)
	if err != nil {
		return h.failPipelineRun(ctx, payload, err)
	}
	h.logger().LogAttrs(ctx, slog.LevelInfo, "文档索引完成",
		append(lineageAttrs(payload),
			slog.String("chunk_set_id", chunkSetID.String()),
			slog.Int("retrieval_entries", len(entries)))...)
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
	return h.createAndEnqueueWithJobPayload(ctx, source, typ, chunkSetID, nil, 0)
}

// createAndEnqueueWithExternalJob 创建后续任务并携带 external_job_id（异步 parser poll 用）。
func (h DocumentHandlers) createAndEnqueueWithExternalJob(
	ctx context.Context,
	source DocumentTaskPayload,
	typ string,
	externalJobID string,
	extraPayload map[string]any,
) (*dto.Job, error) {
	jobPayload := h.buildJobPayload(source, extraPayload)
	jobPayload["external_job_id"] = externalJobID
	return h.createAndEnqueueWithJobPayloadAndExtra(ctx, source, typ, uuid.Nil, jobPayload, 0)
}

// createAndEnqueueWithExternalJobDelayed 创建带延迟的后续任务。
func (h DocumentHandlers) createAndEnqueueWithExternalJobDelayed(
	ctx context.Context,
	source DocumentTaskPayload,
	typ string,
	externalJobID string,
	extraPayload map[string]any,
	delay queue.Delay,
) (*dto.Job, error) {
	jobPayload := h.buildJobPayload(source, extraPayload)
	jobPayload["external_job_id"] = externalJobID
	return h.createAndEnqueueWithJobPayloadAndExtra(ctx, source, typ, uuid.Nil, jobPayload, delay)
}

func (h DocumentHandlers) buildJobPayload(source DocumentTaskPayload, extra map[string]any) map[string]any {
	jobPayload := map[string]any{
		"workspace_id": source.WorkspaceID.String(), "knowledge_base_id": source.KnowledgeBaseID.String(),
		"document_id": source.DocumentID.String(), "document_revision_id": source.DocumentRevisionID.String(),
		"index_generation_id": source.GenerationID.String(),
	}
	for k, v := range extra {
		jobPayload[k] = v
	}
	return jobPayload
}

func (h DocumentHandlers) createAndEnqueueWithJobPayload(
	ctx context.Context,
	source DocumentTaskPayload,
	typ string,
	chunkSetID uuid.UUID,
	extra map[string]any,
	delay queue.Delay,
) (*dto.Job, error) {
	jobPayload := h.buildJobPayload(source, extra)
	return h.createAndEnqueueWithJobPayloadAndExtra(ctx, source, typ, chunkSetID, jobPayload, delay)
}

func (h DocumentHandlers) createAndEnqueueWithJobPayloadAndExtra(
	ctx context.Context,
	source DocumentTaskPayload,
	typ string,
	chunkSetID uuid.UUID,
	jobPayload map[string]any,
	delay queue.Delay,
) (*dto.Job, error) {
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
	// 异步 poll 任务会多次重入队同一 revision，必须使用唯一 TaskID（jobID 维度），
	// 否则 asynq 报 "task ID conflicts with another task"。
	taskID := queue.DocumentTaskID(typ, source.WorkspaceID, source.DocumentRevisionID, source.GenerationID)
	if typ == TaskDocumentParsePoll {
		taskID = queue.DocumentPollTaskID(source.WorkspaceID, source.DocumentRevisionID, job.ID)
	}
	if _, err := h.Queue.Enqueue(ctx, queue.JobRequest{
		Type: typ, Payload: queuePayload,
		TaskID: taskID,
		Delay:  delay,
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
	h.logger().LogAttrs(ctx, slog.LevelError, "文档流水线失败",
		append(lineageAttrs(payload),
			slog.String("error", cause.Error()),
			slog.String("error_class", documentTaskErrorClass(cause)),
			slog.Bool("permanent", permanent),
			slog.Bool("terminal", terminal),
			slog.Int("retry_count", retryCount))...)
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
		errors.Is(err, parserport.ErrAsyncParseFailed) ||
		errors.Is(err, parserport.ErrMissingParserProvider) ||
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
	case errors.Is(err, parserport.ErrAsyncParseFailed):
		return "async_parse_failed"
	case errors.Is(err, parserport.ErrMissingParserProvider):
		return "missing_parser_provider"
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
	h.logger().LogAttrs(ctx, slog.LevelError, "文档后续任务创建失败",
		append(jobAttrs(workspaceID, jobID), slog.String("error", cause.Error()))...)
	if markErr := h.Store.MarkFailed(ctx, workspaceID, jobID, cause.Error()); markErr != nil {
		return errors.Join(cause, fmt.Errorf("标记后续任务失败也失败: %w", markErr))
	}
	return cause
}

func (h DocumentHandlers) failRunningJob(ctx context.Context, workspaceID, jobID uuid.UUID, cause error) error {
	h.logger().LogAttrs(ctx, slog.LevelError, "文档任务失败",
		append(jobAttrs(workspaceID, jobID), slog.String("error", cause.Error()))...)
	if markErr := h.Store.MarkFailed(ctx, workspaceID, jobID, cause.Error()); markErr != nil {
		return errors.Join(cause, fmt.Errorf("标记任务失败也失败: %w", markErr))
	}
	return cause
}

func (h DocumentHandlers) succeedRunningJob(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	if err := h.Store.MarkSucceeded(ctx, workspaceID, jobID); err != nil {
		return h.failRunningJob(ctx, workspaceID, jobID, err)
	}
	h.logger().LogAttrs(ctx, slog.LevelDebug, "文档任务成功", jobAttrs(workspaceID, jobID)...)
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
