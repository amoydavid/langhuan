package main

import (
	"context"

	"github.com/dajee/langhuan/internal/adapters/queue/asynq"
	"github.com/dajee/langhuan/internal/application/service"
)

// inspectorPortAdapter 把 asynq.QueueInspector 适配为 service.QueueInspectorPort，
// 负责 asynq 原生类型 → service 类型的转换，保持 application 层不依赖 adapter 包。
type inspectorPortAdapter struct {
	inspector *asynq.QueueInspector
}

func (a inspectorPortAdapter) Snapshots(ctx context.Context) ([]service.QueueSnapshot, error) {
	snapshots, err := a.inspector.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.QueueSnapshot, len(snapshots))
	for i, s := range snapshots {
		out[i] = service.QueueSnapshot{
			Queue:          s.Queue,
			Size:           s.Size,
			Pending:        s.Pending,
			Active:         s.Active,
			Scheduled:      s.Scheduled,
			Retry:          s.Retry,
			Dead:           s.Archived,
			ProcessedTotal: s.ProcessedTotal,
			FailedTotal:    s.FailedTotal,
		}
	}
	return out, nil
}

func (a inspectorPortAdapter) ListDead(ctx context.Context, queue string, page, pageSize int) ([]service.DeadTask, error) {
	tasks, err := a.inspector.ListDead(ctx, queue, page, pageSize)
	if err != nil {
		return nil, err
	}
	out := make([]service.DeadTask, len(tasks))
	for i, t := range tasks {
		out[i] = service.DeadTask{
			ID:           t.ID,
			Type:         t.Type,
			LastFailedAt: t.LastFailedAt,
			LastError:    t.LastError,
			Retried:      t.Retried,
			MaxRetry:     t.MaxRetry,
		}
	}
	return out, nil
}

func (a inspectorPortAdapter) RetryDead(ctx context.Context, queue, taskID string) error {
	return a.inspector.RetryDead(ctx, queue, taskID)
}

func (a inspectorPortAdapter) DeleteDead(ctx context.Context, queue, taskID string) error {
	return a.inspector.DeleteDead(ctx, queue, taskID)
}
