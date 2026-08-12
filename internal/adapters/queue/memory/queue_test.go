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

// TestRetryDeadPreservesPayload 验证 H2 修复：RetryDead 重投携带原 payload。
func TestRetryDeadPreservesPayload(t *testing.T) {
	var gotPayload []byte
	var attempts atomic.Int32
	mux := newTestMux(t, "revive", func(ctx context.Context, task *hibikenasynq.Task) error {
		gotPayload = task.Payload()
		attempts.Add(1)
		return nil // 重投后成功
	})
	q := New(mux, Config{Concurrency: 1, Capacity: 4, MaxRetry: 1, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	want := []byte("original-payload")
	// 用一个必然失败一次再成功的 handler 制造死信不易；这里直接构造死信路径：
	// 改用 always-fail handler 进死信，再换 handler 重投。
	failMux := hibikenasynq.NewServeMux()
	_ = failMux // placeholder
	// 入队 + 失败到死信
	q2 := New(mux, Config{Concurrency: 1, Capacity: 4, MaxRetry: 0, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	failHandler := hibikenasynq.HandlerFunc(func(ctx context.Context, task *hibikenasynq.Task) error {
		return errors.New("always fail")
	})
	q2.mux = failHandler
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	q2.Start(ctx2)
	q2.Enqueue(ctx2, queueport.JobRequest{Type: "revive", Payload: want, TaskID: "td"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && q2.Stats().Dead == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if q2.Stats().Dead != 1 {
		t.Fatalf("应进死信，Dead=%d", q2.Stats().Dead)
	}
	// 换回成功 handler，RetryDead 重投
	q2.mux = mux
	if !q2.RetryDead("td") {
		t.Fatal("RetryDead 应找到死信")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && attempts.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if attempts.Load() != 1 {
		t.Fatal("RetryDead 后 handler 应被调用")
	}
	if string(gotPayload) != string(want) {
		t.Fatalf("重投 payload 丢失: got %q want %q", gotPayload, want)
	}
	q.Stop(context.Background())
	q2.Stop(context.Background())
}

// TestRetryBackoffKeepsTaskIDOccupied 验证 M1 修复：retry 退避期 TaskID 仍占用。
func TestRetryBackoffKeepsTaskIDOccupied(t *testing.T) {
	var first atomic.Bool
	mux := newTestMux(t, "race", func(ctx context.Context, task *hibikenasynq.Task) error {
		if !first.CompareAndSwap(false, true) {
			return nil
		}
		return errors.New("transient") // 首次失败，进 backoff
	})
	q := New(mux, Config{Concurrency: 1, Capacity: 4, MaxRetry: 5, MinBackoff: 100 * time.Millisecond, MaxBackoff: 100 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "race", TaskID: "X"}); err != nil {
		t.Fatal(err)
	}
	// 等待首次执行 + 进入 backoff（TaskID 应仍占用）
	time.Sleep(30 * time.Millisecond)
	_, err := q.Enqueue(ctx, queueport.JobRequest{Type: "race", TaskID: "X"})
	if err == nil {
		t.Fatal("retry backoff 期间同 TaskID 应被拒绝（M1 契约）")
	}
	q.Stop(context.Background())
}

// TestDelayPushFailReleasesSlot 验证 M2 修复：Delay push 失败释放 TaskID。
func TestDelayPushFailReleasesSlot(t *testing.T) {
	mux := newTestMux(t, "leak", func(ctx context.Context, task *hibikenasynq.Task) error { return nil })
	q := New(mux, Config{Concurrency: 1, Capacity: 1, MinBackoff: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)
	// 填满 pending（capacity=1）
	q.Enqueue(ctx, queueport.JobRequest{Type: "leak"})
	// 入队一个 Delay 任务，timer 触发时 push 会失败（pending 满）
	q.Enqueue(ctx, queueport.JobRequest{Type: "leak", TaskID: "leak-id", Delay: queueport.Delay(30 * time.Millisecond)})
	time.Sleep(120 * time.Millisecond)
	// leak-id 应已释放（M2），可重新入队
	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "leak", TaskID: "leak-id"}); err != nil {
		t.Fatalf("Delay push 失败后 TaskID 应释放（M2），got: %v", err)
	}
	q.Stop(context.Background())
}
