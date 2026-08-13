package dbqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	hibikenasynq "github.com/hibiken/asynq"
	_ "modernc.org/sqlite"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/queue.db?cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	// 跑 SQLite 迁移建 queue_tasks 表（与生产一致，表不再由队列自建）
	if err := migrate.Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn}); err != nil {
		t.Fatalf("SQLite 迁移失败: %v", err)
	}
	return db
}

func newTestMux(t *testing.T, typ string, h hibikenasynq.HandlerFunc) *hibikenasynq.ServeMux {
	t.Helper()
	mux := hibikenasynq.NewServeMux()
	mux.HandleFunc(typ, h)
	return mux
}

func waitFor(deadline time.Time, cond func() bool) bool {
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestEnqueueProcessesViaMux(t *testing.T) {
	var got atomic.Int32
	mux := newTestMux(t, "ping", func(_ context.Context, task *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})
	db := openTestDB(t)
	defer db.Close()
	q, err := New(db, DialectSQLite, mux, Config{Concurrency: 1, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "ping", Payload: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if !waitFor(time.Now().Add(2*time.Second), func() bool { return got.Load() == 1 }) {
		t.Fatalf("handler 未被调用，got=%d", got.Load())
	}
	q.Stop(context.Background())
}

func TestTaskIDDedup(t *testing.T) {
	mux := newTestMux(t, "d", func(_ context.Context, _ *hibikenasynq.Task) error { return nil })
	db := openTestDB(t)
	defer db.Close()
	q, _ := New(db, DialectSQLite, mux, Config{Concurrency: 1})
	// 不 Start worker，保证第一次入队的任务仍在 pending（UNIQUE 约束有效）
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "d", Payload: []byte("x"), TaskID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "d", Payload: []byte("x"), TaskID: "t1"}); err == nil {
		t.Fatal("相同 TaskID 应拒绝重复入队")
	}
}

func TestRetryOnFailure(t *testing.T) {
	var attempts atomic.Int32
	mux := newTestMux(t, "flaky", func(_ context.Context, _ *hibikenasynq.Task) error {
		if attempts.Add(1) < 3 {
			return errors.New("transient")
		}
		return nil
	})
	db := openTestDB(t)
	defer db.Close()
	q, _ := New(db, DialectSQLite, mux, Config{Concurrency: 1, MaxRetry: 5, MinBackoff: time.Millisecond, MaxBackoff: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	q.Enqueue(ctx, queueport.JobRequest{Type: "flaky", Payload: []byte("x")})
	if !waitFor(time.Now().Add(3*time.Second), func() bool { return attempts.Load() >= 3 }) {
		t.Fatalf("应重试到成功，attempts=%d", attempts.Load())
	}
	q.Stop(context.Background())
}

func TestDeadAfterMaxRetry(t *testing.T) {
	var attempts atomic.Int32
	mux := newTestMux(t, "dead", func(_ context.Context, _ *hibikenasynq.Task) error {
		attempts.Add(1)
		return errors.New("always fail")
	})
	db := openTestDB(t)
	defer db.Close()
	q, _ := New(db, DialectSQLite, mux, Config{Concurrency: 1, MaxRetry: 1, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	q.Enqueue(ctx, queueport.JobRequest{Type: "dead", Payload: []byte("x")})
	if !waitFor(time.Now().Add(2*time.Second), func() bool {
		_, _, dead, _, _ := q.Stats()
		return dead >= 1
	}) {
		_, _, dead, _, _ := q.Stats()
		t.Fatalf("超重试应进死信，dead=%d attempts=%d", dead, attempts.Load())
	}
	q.Stop(context.Background())
}

func TestRestartRecovery(t *testing.T) {
	// 核心测试：入队后不启动 worker → close DB → reopen → new Queue → Start → 任务恢复执行
	dsn := fmt.Sprintf("file:%s/restart.db?cache=shared", t.TempDir())

	var got atomic.Int32
	handler := hibikenasynq.HandlerFunc(func(_ context.Context, _ *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})

	// 第 1 轮：入队但不启动 worker（先迁移建表）
	if err := migrate.Run(context.Background(), config.DatabaseConfig{Driver: "sqlite", DSN: dsn}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	db1, _ := sql.Open("sqlite", dsn)
	db1.SetMaxOpenConns(1)
	mux1 := hibikenasynq.NewServeMux()
	mux1.HandleFunc("recover", handler)
	q1, err := New(db1, DialectSQLite, mux1, Config{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 入队但不 Start（模拟进程崩溃前任务未处理）
	if _, err := q1.Enqueue(context.Background(), queueport.JobRequest{Type: "recover", Payload: []byte("persist-me")}); err != nil {
		t.Fatal(err)
	}
	// 确认任务在 DB
	pending, _, _, _, _ := q1.Stats()
	if pending != 1 {
		t.Fatalf("入队后 pending 应为 1，got %d", pending)
	}
	db1.Close() // 模拟进程退出

	// 第 2 轮：reopen DB + 新 Queue + Start → 任务应恢复执行
	db2, _ := sql.Open("sqlite", dsn)
	db2.SetMaxOpenConns(1)
	defer db2.Close()
	mux2 := hibikenasynq.NewServeMux()
	mux2.HandleFunc("recover", handler)
	q2, err := New(db2, DialectSQLite, mux2, Config{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q2.Start(ctx) // 重启恢复

	if !waitFor(time.Now().Add(3*time.Second), func() bool { return got.Load() == 1 }) {
		t.Fatalf("重启后任务应恢复执行，got=%d", got.Load())
	}
	q2.Stop(context.Background())
}

func TestDelayExecution(t *testing.T) {
	var got atomic.Int32
	mux := newTestMux(t, "late", func(_ context.Context, _ *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})
	db := openTestDB(t)
	defer db.Close()
	q, _ := New(db, DialectSQLite, mux, Config{Concurrency: 1, PollInterval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	start := time.Now()
	q.Enqueue(ctx, queueport.JobRequest{Type: "late", Payload: []byte("x"), Delay: queueport.Delay(100 * time.Millisecond)})
	if !waitFor(time.Now().Add(2*time.Second), func() bool { return got.Load() == 1 }) {
		t.Fatal("延迟任务应被执行")
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("延迟未生效，elapsed=%v", elapsed)
	}
	q.Stop(context.Background())
}

func TestInspectorSnapshotsAndDead(t *testing.T) {
	mux := newTestMux(t, "x", func(_ context.Context, _ *hibikenasynq.Task) error {
		return errors.New("fail")
	})
	db := openTestDB(t)
	defer db.Close()
	q, _ := New(db, DialectSQLite, mux, Config{Concurrency: 1, MaxRetry: 0, MinBackoff: time.Millisecond})
	insp := NewInspector(q)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	q.Enqueue(ctx, queueport.JobRequest{Type: "x", Payload: []byte("x"), TaskID: "td"})
	if !waitFor(time.Now().Add(2*time.Second), func() bool {
		_, _, dead, _, _ := q.Stats()
		return dead >= 1
	}) {
		t.Fatal("应进死信")
	}

	// Snapshots
	snaps, err := insp.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].Dead < 1 {
		t.Fatalf("Snapshots dead=%v", snaps)
	}

	// ListDead
	deads, err := insp.ListDead(ctx, "default", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(deads) != 1 || deads[0].ID != "td" {
		t.Fatalf("ListDead = %+v", deads)
	}

	// RetryDead
	if err := insp.RetryDead(ctx, "default", "td"); err != nil {
		t.Fatal(err)
	}
	// 等待重投后再次进死信（MaxRetry=0，必然再次失败）
	if !waitFor(time.Now().Add(2*time.Second), func() bool {
		_, _, dead, _, _ := q.Stats()
		return dead >= 1
	}) {
		t.Fatal("RetryDead 后应重新处理并再次进死信")
	}

	// DeleteDead
	if err := insp.DeleteDead(ctx, "default", "td"); err != nil {
		t.Fatal(err)
	}
	_, _, dead, _, _ := q.Stats()
	if dead != 0 {
		t.Fatalf("DeleteDead 后 dead 应为 0，got %d", dead)
	}
	q.Stop(context.Background())
}
