package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/ports/queue"
)

// SourceCleanupSchedulerDeps 注入 cleanup scheduler 的全部依赖。
type SourceCleanupSchedulerDeps struct {
	// Store 列出 pending 的 source_cleanup Job。
	Store SourceCleanupStore
	// Queue 把每个 cleanup Job 入队 asynq。
	Queue queue.JobQueue
	// Interval 是 Tick 周期；<=0 时由 NewSourceCleanupScheduler 兜底为 60s。
	Interval time.Duration
	// Logger 输出调度日志（不含正文/凭证）。
	Logger Logger
}

// SourceCleanupScheduler 周期性扫描 pending 的 source_cleanup Job 并入队，
// 防止"DB 已提交 cleanup Job 但首次 asynq 入队失败"导致的永久孤儿。
// 单 goroutine 运行；RequeuePending 启动调用一次，Tick 周期调用。
type SourceCleanupScheduler struct {
	store    SourceCleanupStore
	queue    queue.JobQueue
	interval time.Duration
	logger   Logger
}

// NewSourceCleanupScheduler 构造一个 cleanup scheduler。
func NewSourceCleanupScheduler(deps SourceCleanupSchedulerDeps) *SourceCleanupScheduler {
	interval := deps.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	logger := deps.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	return &SourceCleanupScheduler{
		store:    deps.Store,
		queue:    deps.Queue,
		interval: interval,
		logger:   logger,
	}
}

// RequeuePending 在启动时调用一次：扫描所有 pending cleanup Job 并入队。
// 入队失败的 Job 保留 pending（下一次 Tick 再试），仅记录 warning。
func (s *SourceCleanupScheduler) RequeuePending(ctx context.Context) error {
	return s.dispatch(ctx)
}

// Tick 周期调用：与 RequeuePending 相同，重新派发所有 pending cleanup Job。
func (s *SourceCleanupScheduler) Tick(ctx context.Context) error {
	return s.dispatch(ctx)
}

// dispatch 是 RequeuePending/Tick 的共享实现：列出 pending cleanup Job → 逐个入队。
func (s *SourceCleanupScheduler) dispatch(ctx context.Context) error {
	if s.store == nil || s.queue == nil {
		return fmt.Errorf("%w: SourceCleanupScheduler 依赖未配置", domainerrors.ErrValidation)
	}
	pending, err := s.store.ListPendingSourceCleanupJobs(ctx)
	if err != nil {
		return fmt.Errorf("列出 pending source_cleanup Job 失败: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	for _, job := range pending {
		if err := s.enqueue(ctx, job); err != nil {
			// 入队失败不中断其它 Job；保留 pending，下一次 Tick 再试。
			s.logger.Warn("入队 source_cleanup 失败，保留 pending 等待下次 Tick",
				"workspace_id", job.WorkspaceID.String(),
				"knowledge_base_id", job.KnowledgeBaseID.String(),
				"job_id", job.JobID.String(),
				"error", err.Error(),
			)
			continue
		}
	}
	return nil
}

// enqueue 把单个 cleanup Job 入队 asynq，按 job_id 幂等去重。
func (s *SourceCleanupScheduler) enqueue(ctx context.Context, job DueCleanupJob) error {
	payload, err := json.Marshal(sourceCleanupTaskPayload{
		WorkspaceID: job.WorkspaceID, KnowledgeBaseID: job.KnowledgeBaseID, JobID: job.JobID,
	})
	if err != nil {
		return fmt.Errorf("编码 source_cleanup payload 失败: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: model.SourceCleanupJobType, Payload: payload,
		TaskID: queue.SourceCleanupTaskID(job.WorkspaceID, job.JobID),
	}); err != nil {
		return fmt.Errorf("入队 source_cleanup 任务失败: %w", err)
	}
	return nil
}

// Run 启动周期调度循环，直到 ctx 取消。周期来自 Interval（config）。
func (s *SourceCleanupScheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.logger.Info("启动 source_cleanup scheduler",
		"interval_seconds", int(s.interval.Seconds()),
	)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("source_cleanup scheduler Tick 失败", "error", err.Error())
			}
		}
	}
}
