package memory

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	hibikenasynq "github.com/hibiken/asynq"

	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

func newTestMux(t *testing.T, typ string, handler hibikenasynq.HandlerFunc) *hibikenasynq.ServeMux {
	t.Helper()
	mux := hibikenasynq.NewServeMux()
	mux.HandleFunc(typ, handler)
	return mux
}

func TestEnqueueProcessesViaMux(t *testing.T) {
	var got atomic.Int32
	mux := newTestMux(t, "ping", func(ctx context.Context, task *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})
	q := New(mux, Config{Concurrency: 1, Capacity: 4})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "ping", Payload: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	// 等待 worker 处理
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatalf("handler 未被调用，got=%d", got.Load())
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := q.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueTaskIDDedup(t *testing.T) {
	mux := newTestMux(t, "dedup", func(ctx context.Context, task *hibikenasynq.Task) error { return nil })
	q := New(mux, Config{Concurrency: 1, Capacity: 4})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	defer q.Stop(context.Background())

	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "dedup", TaskID: "t1"}); err != nil {
		t.Fatal(err)
	}
	// 第二次相同 TaskID 应被拒（在 inFlight 中）
	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "dedup", TaskID: "t1"}); err == nil {
		t.Fatal("相同 TaskID 在执行中应拒绝重复入队")
	}
}

func TestRetryOnFailure(t *testing.T) {
	var attempts atomic.Int32
	mux := newTestMux(t, "flaky", func(ctx context.Context, task *hibikenasynq.Task) error {
		if attempts.Add(1) < 3 {
			return errors.New("transient")
		}
		return nil
	})
	q := New(mux, Config{Concurrency: 1, Capacity: 4, MaxRetry: 5, MinBackoff: time.Millisecond, MaxBackoff: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "flaky"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && attempts.Load() < 3 {
		time.Sleep(5 * time.Millisecond)
	}
	if attempts.Load() < 3 {
		t.Fatalf("应重试到成功，attempts=%d", attempts.Load())
	}
	q.Stop(context.Background())
}

func TestSkipRetryNotRetried(t *testing.T) {
	var attempts atomic.Int32
	mux := newTestMux(t, "skip", func(ctx context.Context, task *hibikenasynq.Task) error {
		attempts.Add(1)
		return hibikenasynq.SkipRetry
	})
	q := New(mux, Config{Concurrency: 1, Capacity: 4, MinBackoff: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	q.Enqueue(ctx, queueport.JobRequest{Type: "skip"})
	time.Sleep(100 * time.Millisecond)
	q.Stop(context.Background())
	if attempts.Load() != 1 {
		t.Fatalf("SkipRetry 不应重试，attempts=%d", attempts.Load())
	}
}

func TestEnqueueDelayed(t *testing.T) {
	var got atomic.Int32
	mux := newTestMux(t, "late", func(ctx context.Context, task *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})
	q := New(mux, Config{Concurrency: 1, Capacity: 4})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	start := time.Now()
	q.Enqueue(ctx, queueport.JobRequest{Type: "late", Delay: queueport.Delay(80 * time.Millisecond)})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got.Load() != 1 {
		t.Fatal("延迟任务应被处理")
	}
	if elapsed := time.Since(start); elapsed < 70*time.Millisecond {
		t.Fatalf("延迟未生效，elapsed=%v", elapsed)
	}
	q.Stop(context.Background())
}
