package asynq

import (
	"context"
	"testing"
	"time"

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

// optValue 提取指定类型选项的值，不存在返回 (nil,false)。
func optValue(opts []hibikenasynq.Option, typ hibikenasynq.OptionType) (any, bool) {
	for _, o := range opts {
		if o.Type() == typ {
			return o.Value(), true
		}
	}
	return nil, false
}

func TestQueueEnqueuePassesTaskAndOptions(t *testing.T) {
	fake := &fakeEnqueuer{}
	// 无 defaults 的 Queue：向后兼容，不注入 MaxRetry/Timeout/Retention。
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

func TestQueueEnqueueWithGlobalDefaults(t *testing.T) {
	fake := &fakeEnqueuer{}
	adapter := NewQueueWithDefaults(fake, QueueDefaults{
		MaxRetry:    4,
		MaxRetrySet: true,
		Timeout:     30 * time.Minute,
		Retention:   24 * time.Hour,
	})

	if _, err := adapter.Enqueue(context.Background(), queueport.JobRequest{
		Type:   "document_index",
		TaskID: "task-1",
	}); err != nil {
		t.Fatal(err)
	}

	if v, ok := optValue(fake.opts, hibikenasynq.MaxRetryOpt); !ok || v != 4 {
		t.Fatalf("MaxRetry opt = %v (ok=%v), want 4", v, ok)
	}
	if v, ok := optValue(fake.opts, hibikenasynq.TimeoutOpt); !ok || v != 30*time.Minute {
		t.Fatalf("Timeout opt = %v (ok=%v), want 30m", v, ok)
	}
	if v, ok := optValue(fake.opts, hibikenasynq.RetentionOpt); !ok || v != 24*time.Hour {
		t.Fatalf("Retention opt = %v (ok=%v), want 24h", v, ok)
	}
}

func TestQueueEnqueueJobRequestOverridesDefaults(t *testing.T) {
	fake := &fakeEnqueuer{}
	adapter := NewQueueWithDefaults(fake, QueueDefaults{
		MaxRetry:    4,
		MaxRetrySet: true,
		Timeout:     30 * time.Minute,
		Retention:   24 * time.Hour,
	})

	if _, err := adapter.Enqueue(context.Background(), queueport.JobRequest{
		Type:      "document_index",
		TaskID:    "task-1",
		MaxRetry:  1,
		Timeout:   5 * time.Minute,
		Retention: 2 * time.Hour,
	}); err != nil {
		t.Fatal(err)
	}

	// JobRequest 显式值覆盖全局默认。
	if v, ok := optValue(fake.opts, hibikenasynq.MaxRetryOpt); !ok || v != 1 {
		t.Fatalf("MaxRetry opt = %v (ok=%v), want 1 (override)", v, ok)
	}
	if v, ok := optValue(fake.opts, hibikenasynq.TimeoutOpt); !ok || v != 5*time.Minute {
		t.Fatalf("Timeout opt = %v (ok=%v), want 5m (override)", v, ok)
	}
	if v, ok := optValue(fake.opts, hibikenasynq.RetentionOpt); !ok || v != 2*time.Hour {
		t.Fatalf("Retention opt = %v (ok=%v), want 2h (override)", v, ok)
	}
}

// TestQueueEnqueueZeroMaxRetryExplicit 验证 max_attempts=1（MaxRetry=0）时
// 显式注入 MaxRetry(0)，而不是回落到 asynq 库默认 25 次重试。
func TestQueueEnqueueZeroMaxRetryExplicit(t *testing.T) {
	fake := &fakeEnqueuer{}
	adapter := NewQueueWithDefaults(fake, QueueDefaults{
		MaxRetry:    0,
		MaxRetrySet: true,
	})

	if _, err := adapter.Enqueue(context.Background(), queueport.JobRequest{
		Type: "document_index",
	}); err != nil {
		t.Fatal(err)
	}
	if v, ok := optValue(fake.opts, hibikenasynq.MaxRetryOpt); !ok || v != 0 {
		t.Fatalf("MaxRetry opt = %v (ok=%v), want 0 (explicit no-retry)", v, ok)
	}
}

func TestQueueEnqueueNoRetryWhenDefaultsZero(t *testing.T) {
	fake := &fakeEnqueuer{}
	// 空 defaults 不注入 MaxRetry/Timeout/Retention（等价于 NewQueue 行为）。
	adapter := NewQueueWithDefaults(fake, QueueDefaults{})

	if _, err := adapter.Enqueue(context.Background(), queueport.JobRequest{
		Type: "document_index",
	}); err != nil {
		t.Fatal(err)
	}

	if _, ok := optValue(fake.opts, hibikenasynq.MaxRetryOpt); ok {
		t.Fatal("MaxRetry opt should not be set when defaults zero")
	}
	if _, ok := optValue(fake.opts, hibikenasynq.TimeoutOpt); ok {
		t.Fatal("Timeout opt should not be set when defaults zero")
	}
	if _, ok := optValue(fake.opts, hibikenasynq.RetentionOpt); ok {
		t.Fatal("Retention opt should not be set when defaults zero")
	}
}
