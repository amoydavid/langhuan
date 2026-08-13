//go:build integration

package dbqueue

import (
	"context"
	"database/sql"
	"os"
	"sync/atomic"
	"testing"
	"time"

	hibikenasynq "github.com/hibiken/asynq"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dajee/langhuan/internal/infrastructure/migrate"
	queueport "github.com/dajee/langhuan/internal/ports/queue"
	"github.com/dajee/langhuan/internal/testsupport"
)

func TestMain(m *testing.M) {
	os.Exit(testsupport.RunPostgresTestMain(m, migrate.Run))
}

// openTestPGDB 用临时 PG 容器（已迁移到最新 schema，含 queue_tasks 表）并返回连接。
func openTestPGDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testsupport.NewMigratedPostgres(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(5)
	return db
}

func TestPGQueueEnqueueAndProcess(t *testing.T) {
	var got atomic.Int32
	mux := hibikenasynq.NewServeMux()
	mux.HandleFunc("pg-task", func(_ context.Context, _ *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})
	db := openTestPGDB(t)
	defer db.Close()
	q, err := New(db, DialectPostgres, mux, Config{Concurrency: 2, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	for i := 0; i < 5; i++ {
		if _, err := q.Enqueue(ctx, queueport.JobRequest{Type: "pg-task", Payload: []byte("x")}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && got.Load() < 5 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 5 {
		t.Fatalf("应处理 5 个任务，got=%d", got.Load())
	}
	q.Stop(context.Background())
}

func TestPGQueueRestartRecovery(t *testing.T) {
	// 验证 PG 队列表重启恢复（active→pending）
	var got atomic.Int32
	handler := hibikenasynq.HandlerFunc(func(_ context.Context, _ *hibikenasynq.Task) error {
		got.Add(1)
		return nil
	})
	dsn := testsupport.NewMigratedPostgres(t)

	// 第 1 轮：入队但不启动 worker
	db1, _ := sql.Open("pgx", dsn)
	db1.SetMaxOpenConns(1)
	mux1 := hibikenasynq.NewServeMux()
	mux1.HandleFunc("recover", handler)
	q1, _ := New(db1, DialectPostgres, mux1, Config{Concurrency: 1})
	q1.Enqueue(context.Background(), queueport.JobRequest{Type: "recover", Payload: []byte("x")})
	pending, _, _, _, _ := q1.Stats()
	if pending != 1 {
		t.Fatalf("入队后 pending 应为 1，got %d", pending)
	}
	db1.Close()

	// 第 2 轮：reopen + Start → 恢复执行
	db2, _ := sql.Open("pgx", dsn)
	db2.SetMaxOpenConns(2)
	defer db2.Close()
	mux2 := hibikenasynq.NewServeMux()
	mux2.HandleFunc("recover", handler)
	q2, _ := New(db2, DialectPostgres, mux2, Config{Concurrency: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q2.Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && got.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() == 0 {
		t.Fatal("重启后任务应恢复执行")
	}
	q2.Stop(context.Background())
}
