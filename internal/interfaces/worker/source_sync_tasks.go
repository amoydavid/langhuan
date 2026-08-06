package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// TaskSourceSync 是知识库级来源同步任务（飞书全量同步）的 asynq type。
const TaskSourceSync = "source_sync"

// SourceSyncTaskPayload 是 source_sync 任务的队列载荷。
// 携带完整 lineage（workspace/kb/job），不含正文与凭证。
type SourceSyncTaskPayload struct {
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	KnowledgeBaseID uuid.UUID `json:"knowledge_base_id"`
	JobID           uuid.UUID `json:"job_id"`
	ConnectionID    uuid.UUID `json:"connection_id,omitempty"`
}

// SourceSyncRunner 是 worker 适配的应用层用例（由 *SourceSyncService 实现）。
type SourceSyncRunner interface {
	SyncKnowledgeBase(ctx context.Context, workspaceID, kbID uuid.UUID) error
}

// SourceSyncTaskStore 推进 source_sync Job 的状态（按 job ID，不限 job type）。
// 复用 DocumentTaskDBStore 的 MarkRunning/MarkSucceeded/MarkFailed。
type SourceSyncTaskStore interface {
	MarkRunning(ctx context.Context, workspaceID, jobID uuid.UUID) error
	MarkSucceeded(ctx context.Context, workspaceID, jobID uuid.UUID) error
	MarkFailed(ctx context.Context, workspaceID, jobID uuid.UUID, message string) error
}

// SourceSyncHandler 是 source_sync 任务的 worker 适配器：解码 → MarkRunning → 调 Runner → 推进状态。
type SourceSyncHandler struct {
	Runner SourceSyncRunner
	Store  SourceSyncTaskStore
	Logger *slog.Logger
}

// RegisterSourceSyncHandler 注册 source_sync 消费者。
func RegisterSourceSyncHandler(mux *asynq.ServeMux, handler SourceSyncHandler) {
	mux.HandleFunc(TaskSourceSync, handler.Handle)
}

func (h SourceSyncHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// Handle 解码 payload 并校验 lineage → MarkRunning → 调 Runner.SyncKnowledgeBase → 成功 MarkSucceeded，
// 失败 MarkFailed 并按错误类型决定是否 SkipRetry（validation/notfound 类永久错误立即终止，其它可重试）。
func (h SourceSyncHandler) Handle(ctx context.Context, task *asynq.Task) error {
	if h.Runner == nil {
		return fmt.Errorf("source_sync runner 不能为空")
	}
	if task == nil {
		return fmt.Errorf("source_sync task 不能为空")
	}
	var payload SourceSyncTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("解析 source_sync payload 失败: %w", err)
	}
	if payload.WorkspaceID == uuid.Nil || payload.KnowledgeBaseID == uuid.Nil || payload.JobID == uuid.Nil {
		return fmt.Errorf("source_sync payload lineage 不能为空")
	}

	if h.Store != nil {
		if err := h.Store.MarkRunning(ctx, payload.WorkspaceID, payload.JobID); err != nil {
			h.logger().LogAttrs(ctx, slog.LevelError, "标记 source_sync 运行中失败",
				slog.String("workspace_id", payload.WorkspaceID.String()),
				slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
				slog.String("job_id", payload.JobID.String()),
				slog.String("error", err.Error()),
			)
			// 标记失败属持久化层问题，保留可重试语义：直接返回让 asynq 重试。
			return err
		}
	}

	if err := h.Runner.SyncKnowledgeBase(ctx, payload.WorkspaceID, payload.KnowledgeBaseID); err != nil {
		if h.Store != nil {
			if markErr := h.Store.MarkFailed(ctx, payload.WorkspaceID, payload.JobID, err.Error()); markErr != nil {
				h.logger().LogAttrs(ctx, slog.LevelError, "标记 source_sync 失败状态失败",
					slog.String("workspace_id", payload.WorkspaceID.String()),
					slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
					slog.String("job_id", payload.JobID.String()),
					slog.String("error", markErr.Error()),
				)
			}
		}
		h.logger().LogAttrs(ctx, slog.LevelError, "source_sync 同步失败",
			slog.String("workspace_id", payload.WorkspaceID.String()),
			slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
			slog.String("job_id", payload.JobID.String()),
			slog.String("error", err.Error()),
		)
		if isPermanentSourceSyncTaskError(err) {
			return errors.Join(asynq.SkipRetry, err)
		}
		return err
	}

	if h.Store != nil {
		if err := h.Store.MarkSucceeded(ctx, payload.WorkspaceID, payload.JobID); err != nil {
			h.logger().LogAttrs(ctx, slog.LevelError, "标记 source_sync 成功状态失败",
				slog.String("workspace_id", payload.WorkspaceID.String()),
				slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
				slog.String("job_id", payload.JobID.String()),
				slog.String("error", err.Error()),
			)
		}
	}
	h.logger().LogAttrs(ctx, slog.LevelInfo, "source_sync 同步完成",
		slog.String("workspace_id", payload.WorkspaceID.String()),
		slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
		slog.String("job_id", payload.JobID.String()),
	)
	return nil
}

// isPermanentSourceSyncTaskError 判断是否为永久性错误（validation/notfound 类），
// 这类错误不应重试，直接 SkipRetry。
func isPermanentSourceSyncTaskError(err error) bool {
	return errors.Is(err, domainerrors.ErrValidation) ||
		errors.Is(err, domainerrors.ErrNotFound)
}
