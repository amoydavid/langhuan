package asynq

import (
	"context"
	"time"

	hibikenasynq "github.com/hibiken/asynq"

	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *hibikenasynq.Task, opts ...hibikenasynq.Option) (*hibikenasynq.TaskInfo, error)
}

// QueueDefaults 是入队时注入的全局策略默认值（来自 config.queue）。
// 当 JobRequest 未显式覆盖对应字段时使用这些默认值。零值字段表示不注入对应 asynq 选项。
type QueueDefaults struct {
	MaxRetry  int
	Timeout   time.Duration
	Retention time.Duration
}

type Queue struct {
	client   Enqueuer
	defaults QueueDefaults
}

// NewQueue 创建使用库默认策略的队列适配器（向后兼容旧调用方）。
func NewQueue(client Enqueuer) *Queue {
	return &Queue{client: client}
}

// NewQueueWithDefaults 创建注入全局重试/超时/保留策略的队列适配器。
func NewQueueWithDefaults(client Enqueuer, defaults QueueDefaults) *Queue {
	return &Queue{client: client, defaults: defaults}
}

func (q *Queue) Enqueue(ctx context.Context, job queueport.JobRequest) (*queueport.JobHandle, error) {
	task := hibikenasynq.NewTask(job.Type, job.Payload)
	opts := make([]hibikenasynq.Option, 0, 6)
	if job.Queue != "" {
		opts = append(opts, hibikenasynq.Queue(job.Queue))
	}
	if job.TaskID != "" {
		opts = append(opts, hibikenasynq.TaskID(job.TaskID))
	}
	if d := time.Duration(job.Delay); d > 0 {
		opts = append(opts, hibikenasynq.ProcessIn(d))
	}
	// 重试次数：JobRequest 显式覆盖优先，否则用全局默认。
	maxRetry := job.MaxRetry
	if maxRetry == 0 {
		maxRetry = q.defaults.MaxRetry
	}
	if maxRetry > 0 {
		opts = append(opts, hibikenasynq.MaxRetry(maxRetry))
	}
	// 超时：JobRequest 显式覆盖优先，否则用全局默认。
	timeout := job.Timeout
	if timeout == 0 {
		timeout = q.defaults.Timeout
	}
	if timeout > 0 {
		opts = append(opts, hibikenasynq.Timeout(timeout))
	}
	// 保留时长：JobRequest 显式覆盖优先，否则用全局默认。
	retention := job.Retention
	if retention == 0 {
		retention = q.defaults.Retention
	}
	if retention > 0 {
		opts = append(opts, hibikenasynq.Retention(retention))
	}
	info, err := q.client.EnqueueContext(ctx, task, opts...)
	if err != nil {
		return nil, err
	}
	return &queueport.JobHandle{ID: info.ID}, nil
}
