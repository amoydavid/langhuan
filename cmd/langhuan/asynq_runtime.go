package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	hibikenasynq "github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"

	"github.com/dajee/langhuan/internal/adapters/queue/asynq"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/logger"
)

// runRetrievalCleanupLoop 周期性清理过期的 retrieval 投影，随 ctx 取消退出。
func runRetrievalCleanupLoop(ctx context.Context, cleanup *service.RetrievalCleanupService, interval time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	log.Info("启动 retrieval 投影定时清理", "interval", interval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			result, err := cleanup.CleanupGlobal(cctx)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					log.Warn("retrieval 投影清理失败", "error", err.Error())
				}
				continue
			}
			if result.DeletedEntries > 0 || result.DeletedGenerations > 0 {
				log.Info("retrieval 投影清理完成",
					"deleted_entries", result.DeletedEntries,
					"deleted_generations", result.DeletedGenerations)
			}
		}
	}
}

// newLogger 根据配置创建 logger，按 log.redact 决定是否启用敏感字段脱敏。
func newLogger(cfg *config.Config) *slog.Logger {
	opts := make([]logger.Option, 0, 1)
	if cfg.Log.Redact {
		opts = append(opts, logger.WithRedact())
	}
	return logger.NewWithOpts(cfg.Log.Level, opts...)
}

// asynqServerConfig 根据 config.queue 构造 asynq Server 配置。
// 覆盖库默认值（MaxRetry=25、无超时），注入可治理的并发、退避、超时与日志。
func asynqServerConfig(cfg config.QueueConfig, logger *slog.Logger) hibikenasynq.Config {
	return hibikenasynq.Config{
		Queues:         map[string]int{"default": 1},
		Concurrency:    cfg.Concurrency,
		RetryDelayFunc: retryDelayFunc(cfg.MinBackoff(), cfg.MaxBackoff()),
		Logger:         asynqSlogAdapter{logger: logger},
		LogLevel:       hibikenasynq.InfoLevel,
		ErrorHandler:   asynqErrorHandler{logger: logger},
	}
}

// retryDelayFunc 返回指数退避策略：min * 2^n，封顶 max。
// n 是已重试次数（从 0 开始），第 n 次失败后等待 min*2^n 再重试。
func retryDelayFunc(minBackoff, maxBackoff time.Duration) hibikenasynq.RetryDelayFunc {
	return func(n int, _ error, _ *hibikenasynq.Task) time.Duration {
		if n <= 0 {
			return minBackoff
		}
		// 防止 shift 溢出：n 超过 30 时直接用 max。
		if n > 30 {
			return maxBackoff
		}
		delay := time.Duration(int64(minBackoff) << uint(n))
		if delay <= 0 || delay > maxBackoff {
			return maxBackoff
		}
		return delay
	}
}

// queueDefaults 从 config 派生入队侧的全局策略。
// MaxRetrySet 恒为 true：applyDefaults 保证 MaxAttempts 有默认值，
// 因此 MaxRetry()=0（max_attempts=1）也是显式配置，必须注入。
func queueDefaults(cfg config.QueueConfig) asynq.QueueDefaults {
	return asynq.QueueDefaults{
		MaxRetry:    cfg.MaxRetry(),
		MaxRetrySet: true,
		Timeout:     cfg.TaskTimeout(),
		Retention:   cfg.Retention(),
	}
}

// otelTaskMiddleware 为每个 asynq 任务开 OTel 根 span（span name=task.<type>）。
// traces 未启用时全局 tracer 为 noop，零开销。
func otelTaskMiddleware() hibikenasynq.MiddlewareFunc {
	return func(next hibikenasynq.Handler) hibikenasynq.Handler {
		return hibikenasynq.HandlerFunc(func(ctx context.Context, task *hibikenasynq.Task) error {
			ctx, span := otel.Tracer("langhuan.worker").Start(ctx, "task."+task.Type())
			defer span.End()
			return next.ProcessTask(ctx, task)
		})
	}
}

// asynqSlogAdapter 把 asynq 的日志接口适配到 slog，
// 避免 asynq 用默认 logger 输出非结构化日志。
type asynqSlogAdapter struct {
	logger *slog.Logger
}

func (a asynqSlogAdapter) Debug(args ...any) { a.log(slog.LevelDebug, args) }
func (a asynqSlogAdapter) Info(args ...any)  { a.log(slog.LevelInfo, args) }
func (a asynqSlogAdapter) Warn(args ...any)  { a.log(slog.LevelWarn, args) }
func (a asynqSlogAdapter) Error(args ...any) { a.log(slog.LevelError, args) }
func (a asynqSlogAdapter) Fatal(args ...any) { a.log(slog.LevelError, args) }

func (a asynqSlogAdapter) log(level slog.Level, args []any) {
	if a.logger == nil {
		return
	}
	a.logger.Log(context.Background(), level, fmt.Sprint(args...))
}

// asynqErrorHandler 是 asynq 的全局错误处理器。
// 注意：asynq 在任务**每次失败**时都会调用 ErrorHandler（不限于终态），
// 因此这里用 Warn 级别 + "asynq.task.failed" 事件名，避免把非终态重试误标为
// terminal 并刷 Error 日志。
// 业务侧终态落库（failPipelineRun）与 asynq 层兜底互不冲突：ErrorHandler 只打日志。
type asynqErrorHandler struct {
	logger *slog.Logger
}

func (h asynqErrorHandler) HandleError(_ context.Context, task *hibikenasynq.Task, err error) {
	if h.logger == nil {
		return
	}
	// asynq 的 *Task 不携带 ID 字段（ID 在 TaskInfo 上，此处不可得），
	// 这里只记录 task type；业务侧的准确 job/document lineage 由 worker handler
	// 内的 failPipelineRun 落库，ErrorHandler 仅作 asynq 层兜底。
	h.logger.Warn("asynq.task.failed",
		"task_type", task.Type(),
		"error", err.Error(),
	)
}
