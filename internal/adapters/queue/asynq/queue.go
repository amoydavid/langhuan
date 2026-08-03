package asynq

import (
	"context"

	hibikenasynq "github.com/hibiken/asynq"

	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *hibikenasynq.Task, opts ...hibikenasynq.Option) (*hibikenasynq.TaskInfo, error)
}

type Queue struct {
	client Enqueuer
}

func NewQueue(client Enqueuer) *Queue {
	return &Queue{client: client}
}

func (q *Queue) Enqueue(ctx context.Context, job queueport.JobRequest) (*queueport.JobHandle, error) {
	task := hibikenasynq.NewTask(job.Type, job.Payload)
	opts := make([]hibikenasynq.Option, 0, 2)
	if job.Queue != "" {
		opts = append(opts, hibikenasynq.Queue(job.Queue))
	}
	if job.TaskID != "" {
		opts = append(opts, hibikenasynq.TaskID(job.TaskID))
	}
	info, err := q.client.EnqueueContext(ctx, task, opts...)
	if err != nil {
		return nil, err
	}
	return &queueport.JobHandle{ID: info.ID}, nil
}
