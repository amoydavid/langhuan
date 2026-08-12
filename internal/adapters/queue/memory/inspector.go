package memory

import (
	"context"

	appservice "github.com/dajee/langhuan/internal/application/service"
)

// 编译期断言：Inspector 实现 appservice.QueueInspectorPort。
var _ appservice.QueueInspectorPort = (*Inspector)(nil)

// Inspector 暴露内存队列的可见性与死信管理能力（spec §10.2）。
// 与 asynq Inspector 对齐：Snapshots/ListDead/RetryDead/DeleteDead。
type Inspector struct {
	q     *Queue
	queue string // 逻辑队列名（内存模式只有一个 default 队列）
}

// NewInspector 构造内存队列 Inspector。
func NewInspector(q *Queue, queueName string) *Inspector {
	if queueName == "" {
		queueName = "default"
	}
	return &Inspector{q: q, queue: queueName}
}

// Snapshots 返回队列状态快照。
func (i *Inspector) Snapshots(ctx context.Context) ([]appservice.QueueSnapshot, error) {
	s := i.q.Stats()
	return []appservice.QueueSnapshot{{
		Queue:          i.queue,
		Size:           s.Pending + s.Active,
		Pending:        s.Pending,
		Active:         s.Active,
		Scheduled:      0,
		Retry:          0,
		Dead:           s.Dead,
		ProcessedTotal: int(s.Processed),
		FailedTotal:    int(s.Failed),
	}}, nil
}

// ListDead 分页返回死信 metadata（脱敏，不含 payload）。
func (i *Inspector) ListDead(ctx context.Context, queue string, page, pageSize int) ([]appservice.DeadTask, error) {
	entries := i.q.ListDead(page, pageSize)
	out := make([]appservice.DeadTask, 0, len(entries))
	for _, e := range entries {
		out = append(out, appservice.DeadTask{
			ID:           e.id,
			Type:         e.typ,
			LastFailedAt: e.failedAt,
			LastError:    e.lastError,
			Retried:      e.retried,
			MaxRetry:     e.maxRetry,
		})
	}
	return out, nil
}

// RetryDead 把指定死信重新入队。
func (i *Inspector) RetryDead(ctx context.Context, queue, taskID string) error {
	if !i.q.RetryDead(taskID) {
		return nil // 不存在视为已成功（幂等）
	}
	return nil
}

// DeleteDead 删除指定死信。
func (i *Inspector) DeleteDead(ctx context.Context, queue, taskID string) error {
	i.q.DeleteDead(taskID)
	return nil
}
