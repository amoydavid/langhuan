package asynq

import (
	"context"
	"testing"

	hibikenasynq "github.com/hibiken/asynq"

	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

type fakeEnqueuer struct {
	task *hibikenasynq.Task
	opts []hibikenasynq.Option
}

func (f *fakeEnqueuer) EnqueueContext(_ context.Context, task *hibikenasynq.Task, opts ...hibikenasynq.Option) (*hibikenasynq.TaskInfo, error) {
	f.task = task
	f.opts = opts
	return &hibikenasynq.TaskInfo{ID: "queued-id"}, nil
}

func TestQueueEnqueuePassesTaskAndOptions(t *testing.T) {
	fake := &fakeEnqueuer{}
	adapter := NewQueue(fake)

	handle, err := adapter.Enqueue(context.Background(), queueport.JobRequest{
		Type:    "system_smoke",
		Payload: []byte(`{"ok":true}`),
		Queue:   "critical",
		TaskID:  "task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.ID != "queued-id" {
		t.Fatalf("handle id = %q", handle.ID)
	}
	if fake.task.Type() != "system_smoke" || string(fake.task.Payload()) != `{"ok":true}` {
		t.Fatalf("task = %s %s", fake.task.Type(), fake.task.Payload())
	}
	if len(fake.opts) != 2 {
		t.Fatalf("opts len = %d", len(fake.opts))
	}
	if fake.opts[0].Type() != hibikenasynq.QueueOpt || fake.opts[0].Value() != "critical" {
		t.Fatalf("queue opt = %#v", fake.opts[0])
	}
	if fake.opts[1].Type() != hibikenasynq.TaskIDOpt || fake.opts[1].Value() != "task-1" {
		t.Fatalf("task id opt = %#v", fake.opts[1])
	}
}
