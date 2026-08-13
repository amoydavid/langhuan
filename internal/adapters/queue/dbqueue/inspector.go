package dbqueue

import (
	"context"
	"time"

	appservice "github.com/dajee/langhuan/internal/application/service"
)

// 编译期断言。
var _ appservice.QueueInspectorPort = (*Inspector)(nil)

// Inspector 暴露 SQLite 队列的可见性与死信管理能力。
type Inspector struct {
	q *Queue
}

// NewInspector 构造 SQLite 队列 Inspector。
func NewInspector(q *Queue) *Inspector {
	return &Inspector{q: q}
}

// Snapshots 返回队列状态快照。
func (i *Inspector) Snapshots(ctx context.Context) ([]appservice.QueueSnapshot, error) {
	pending, active, dead, processed, failed := i.q.Stats()
	return []appservice.QueueSnapshot{{
		Queue:          "default",
		Size:           pending + active,
		Pending:        pending,
		Active:         active,
		Scheduled:      0,
		Retry:          0,
		Dead:           dead,
		ProcessedTotal: int(processed),
		FailedTotal:    int(failed),
	}}, nil
}

// ListDead 分页返回死信 metadata（不含 payload，脱敏）。
func (i *Inspector) ListDead(ctx context.Context, queue string, page, pageSize int) ([]appservice.DeadTask, error) {
	rows := i.q.ListDead(page, pageSize)
	out := make([]appservice.DeadTask, 0, len(rows))
	for _, r := range rows {
		var lastFailed time.Time
		if r.DeadAtMs.Valid {
			lastFailed = time.UnixMilli(r.DeadAtMs.Int64)
		}
		out = append(out, appservice.DeadTask{
			ID:           r.TaskID,
			Type:         r.Type,
			LastFailedAt: lastFailed,
			LastError:    r.LastError,
			Retried:      r.Attempts,
			MaxRetry:     r.MaxRetry,
		})
	}
	return out, nil
}

// RetryDead 把指定 taskID 的死信重新入队。
func (i *Inspector) RetryDead(ctx context.Context, queue, taskID string) error {
	i.q.RetryDeadByTaskID(taskID)
	return nil
}

// DeleteDead 删除指定 taskID 的死信。
func (i *Inspector) DeleteDead(ctx context.Context, queue, taskID string) error {
	i.q.DeleteDeadByTaskID(taskID)
	return nil
}
