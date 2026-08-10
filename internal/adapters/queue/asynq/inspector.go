package asynq

import (
	"context"
	"fmt"
	"time"

	hibikenasynq "github.com/hibiken/asynq"
)

// asynqInspector 是 asynq.Inspector 的子集接口，便于单测 mock。
type asynqInspector interface {
	Queues() ([]string, error)
	GetQueueInfo(queue string) (*hibikenasynq.QueueInfo, error)
	ListArchivedTasks(queue string, opts ...hibikenasynq.ListOption) ([]*hibikenasynq.TaskInfo, error)
	RunTask(queue, id string) error
	DeleteTask(queue, id string) error
	Close() error
}

// QueueSnapshot 是某一队列的计数快照，供 /readyz、队列监控端点与 metrics 使用。
type QueueSnapshot struct {
	Queue                       string
	Size, Pending, Active       int
	Scheduled, Retry, Archived  int
	ProcessedTotal, FailedTotal int
}

// DeadTask 是死信（asynq archived）任务的脱敏视图。
// 不返回 Payload 正文，避免泄漏文档片段或文件路径。
type DeadTask struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	LastFailedAt time.Time `json:"last_failed_at"`
	LastError    string    `json:"last_error"`
	Retried      int       `json:"retried"`
	MaxRetry     int       `json:"max_retry"`
}

// QueueInspector 封装 asynq Inspector，提供队列可见性与死信管理。
type QueueInspector struct {
	insp asynqInspector
}

// NewQueueInspector 创建队列可见性封装。
func NewQueueInspector(insp asynqInspector) *QueueInspector {
	return &QueueInspector{insp: insp}
}

// NewQueueInspectorFromRedis 用 Redis 连接参数构造真实 Inspector。
// 调用方负责 Close（通过 io.Closer 或显式 Close）。
func NewQueueInspectorFromRedis(opt hibikenasynq.RedisConnOpt) (*QueueInspector, error) {
	insp := hibikenasynq.NewInspector(opt)
	return &QueueInspector{insp: insp}, nil
}

// Close 释放底层 Inspector 连接。
func (q *QueueInspector) Close() error {
	if q.insp == nil {
		return nil
	}
	return q.insp.Close()
}

// Snapshots 返回所有队列的计数快照。
func (q *QueueInspector) Snapshots(_ context.Context) ([]QueueSnapshot, error) {
	queues, err := q.insp.Queues()
	if err != nil {
		return nil, fmt.Errorf("列举队列失败: %w", err)
	}
	snapshots := make([]QueueSnapshot, 0, len(queues))
	for _, queue := range queues {
		info, err := q.insp.GetQueueInfo(queue)
		if err != nil {
			return nil, fmt.Errorf("读取队列 %s 信息失败: %w", queue, err)
		}
		snapshots = append(snapshots, QueueSnapshot{
			Queue:          info.Queue,
			Size:           info.Size,
			Pending:        info.Pending,
			Active:         info.Active,
			Scheduled:      info.Scheduled,
			Retry:          info.Retry,
			Archived:       info.Archived,
			ProcessedTotal: info.ProcessedTotal,
			FailedTotal:    info.FailedTotal,
		})
	}
	return snapshots, nil
}

// TotalPending 返回所有队列的 pending 总数，供 /readyz 积压判断。
func (q *QueueInspector) TotalPending(_ context.Context) (int, error) {
	snapshots, err := q.Snapshots(context.Background())
	if err != nil {
		return 0, err
	}
	total := 0
	for _, s := range snapshots {
		total += s.Pending
	}
	return total, nil
}

// ListDead 列出某队列的死信（archived）任务，分页，脱敏。
func (q *QueueInspector) ListDead(_ context.Context, queue string, page, pageSize int) ([]DeadTask, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	tasks, err := q.insp.ListArchivedTasks(queue, hibikenasynq.PageSize(pageSize), hibikenasynq.Page(page))
	if err != nil {
		return nil, fmt.Errorf("列出死信任务失败: %w", err)
	}
	result := make([]DeadTask, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, DeadTask{
			ID:           t.ID,
			Type:         t.Type,
			LastFailedAt: t.LastFailedAt,
			LastError:    t.LastErr,
			Retried:      t.Retried,
			MaxRetry:     t.MaxRetry,
		})
	}
	return result, nil
}

// RetryDead 把一个死信任务重新放回待处理队列。
func (q *QueueInspector) RetryDead(_ context.Context, queue, taskID string) error {
	if err := q.insp.RunTask(queue, taskID); err != nil {
		return fmt.Errorf("重试死信任务失败: %w", err)
	}
	return nil
}

// DeleteDead 永久删除一个死信任务。
func (q *QueueInspector) DeleteDead(_ context.Context, queue, taskID string) error {
	if err := q.insp.DeleteTask(queue, taskID); err != nil {
		return fmt.Errorf("删除死信任务失败: %w", err)
	}
	return nil
}
