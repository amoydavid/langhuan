package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// TaskSourceCleanup 是知识库级来源对象清理任务的 asynq type。
const TaskSourceCleanup = "source_cleanup"

// SourceCleanupTaskPayload 是 source_cleanup 任务的队列载荷。
// 携带 lineage（workspace/kb/job），object keys 由 worker 从 DB 的 Job.Payload 读取，
// 避免队列消息膨胀；payload 与 service.sourceCleanupTaskPayload 保持一致。
type SourceCleanupTaskPayload struct {
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	KnowledgeBaseID uuid.UUID `json:"knowledge_base_id"`
	JobID           uuid.UUID `json:"job_id"`
}

// SourceCleanupRunner 是 worker 适配的应用层用例（由 *service.SourceCleanupService 实现）。
type SourceCleanupRunner interface {
	Run(ctx context.Context, workspaceID, kbID, jobID uuid.UUID) error
}

// SourceCleanupTaskStore 推进 source_cleanup Job 的状态（按 job ID，type=source_cleanup）。
// 复用 DocumentTaskDBStore 的 MarkRunning；Succeeded/Failed 由 SourceCleanupDBStore 限定 type。
type SourceCleanupTaskStore interface {
	MarkRunning(ctx context.Context, workspaceID, jobID uuid.UUID) error
}

// SourceCleanupHandler 是 source_cleanup 任务的 worker 适配器：
// 解码 payload → MarkRunning → 调 Runner.Run → 成功/失败状态由 Runner 内部推进。
type SourceCleanupHandler struct {
	Runner SourceCleanupRunner
	Store  SourceCleanupTaskStore
	Logger *slog.Logger
}

// RegisterSourceCleanupHandler 注册 source_cleanup 消费者。
func RegisterSourceCleanupHandler(mux *asynq.ServeMux, handler SourceCleanupHandler) {
	mux.HandleFunc(TaskSourceCleanup, handler.Handle)
}

func (h SourceCleanupHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// Handle 解码 payload 并校验 lineage → MarkRunning → 调 Runner.Run。
// 成功/失败的 Job 终态由 Runner.Run 内部通过 SourceCleanupStore 推进
// （因为只有 Run 读取了 object keys 才能判定全成功/部分失败）；
// MarkRunning 失败或 Runner 返回错误时直接返回，让 asynq 按重试策略重试。
func (h SourceCleanupHandler) Handle(ctx context.Context, task *asynq.Task) error {
	if h.Runner == nil {
		return fmt.Errorf("source_cleanup runner 不能为空")
	}
	if task == nil {
		return fmt.Errorf("source_cleanup task 不能为空")
	}
	var payload SourceCleanupTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		// payload 损坏属永久错误，立即终止。
		return errors.Join(asynq.SkipRetry, fmt.Errorf("解析 source_cleanup payload 失败: %w", err))
	}
	if payload.WorkspaceID == uuid.Nil || payload.KnowledgeBaseID == uuid.Nil || payload.JobID == uuid.Nil {
		return errors.Join(asynq.SkipRetry, fmt.Errorf("source_cleanup payload lineage 不能为空"))
	}

	if h.Store != nil {
		if err := h.Store.MarkRunning(ctx, payload.WorkspaceID, payload.JobID); err != nil {
			h.logger().LogAttrs(ctx, slog.LevelError, "标记 source_cleanup 运行中失败",
				slog.String("workspace_id", payload.WorkspaceID.String()),
				slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
				slog.String("job_id", payload.JobID.String()),
				slog.String("error", err.Error()),
			)
			// 标记失败属持久化层问题，保留可重试语义。
			return err
		}
	}

	if err := h.Runner.Run(ctx, payload.WorkspaceID, payload.KnowledgeBaseID, payload.JobID); err != nil {
		h.logger().LogAttrs(ctx, slog.LevelError, "source_cleanup 清理失败",
			slog.String("workspace_id", payload.WorkspaceID.String()),
			slog.String("knowledge_base_id", payload.KnowledgeBaseID.String()),
			slog.String("job_id", payload.JobID.String()),
			slog.String("error", err.Error()),
		)
		// Job failed 状态已由 Runner 内部推进；返回错误让 asynq 重试（对象清理可重入）。
		return err
	}
	return nil
}
