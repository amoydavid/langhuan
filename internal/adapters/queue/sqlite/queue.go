// Package sqlite 实现基于 SQLite 的持久化任务队列，供 standalone / 无 Redis 部署使用。
//
// 与内存队列（adapters/queue/memory）一样复用 *asynq.ServeMux.ProcessTask 执行
// 已注册的 worker handler，payload 解码与业务幂等逻辑零改动。区别在于任务持久化
// 到 SQLite 表 queue_tasks，进程重启后 pending/active（crashed）自动恢复重试。
//
// 部署模式：PG+Redis(asynq) / PG+SQLite队列 / SQLite全栈(standalone)。
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	hibikenasynq "github.com/hibiken/asynq"

	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

// TaskRunner 执行单个任务。asynq.ServeMux 实现该接口。
type TaskRunner interface {
	ProcessTask(ctx context.Context, t *hibikenasynq.Task) error
}

// 编译期断言。
var _ queueport.JobQueue = (*Queue)(nil)

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS queue_tasks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      TEXT    NOT NULL DEFAULT '',
    type         TEXT    NOT NULL,
    payload      BLOB    NOT NULL,
    state        TEXT    NOT NULL DEFAULT 'pending',
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_retry    INTEGER NOT NULL DEFAULT 4,
    timeout_ms   INTEGER NOT NULL DEFAULT 0,
    scheduled_at INTEGER NOT NULL DEFAULT 0,
    enqueued_at  INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT    NOT NULL DEFAULT '',
    dead_at      INTEGER
)`,
	`CREATE INDEX IF NOT EXISTS idx_queue_state_scheduled ON queue_tasks(state, scheduled_at)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_queue_task_id_live ON queue_tasks(task_id) WHERE task_id != '' AND state IN ('pending', 'active')`,
}

// Config 描述 SQLite 队列运行参数。
type Config struct {
	Concurrency  int
	MaxRetry     int
	MinBackoff   time.Duration
	MaxBackoff   time.Duration
	PollInterval time.Duration
}

// Queue 是基于 SQLite 的持久化任务队列。
type Queue struct {
	db           *sql.DB
	mux          TaskRunner
	concurrency  int
	maxRetry     int
	minBackoff   time.Duration
	maxBackoff   time.Duration
	pollInterval time.Duration
	notify       chan struct{}
	stop         chan struct{}
	stopped      atomic.Bool
	wg           sync.WaitGroup
	processed    atomic.Int64
	failed       atomic.Int64
}

// New 构造 SQLite 持久化队列。db 应为与业务共享的 *sql.DB（同一 .db 文件）。
// mux 是注册了所有 worker handler 的 *asynq.ServeMux。
func New(db *sql.DB, mux TaskRunner, cfg Config) (*Queue, error) {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.MaxRetry < 0 {
		cfg.MaxRetry = 4
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff <= cfg.MinBackoff {
		cfg.MaxBackoff = cfg.MinBackoff * 60
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	// modernc 的 Exec 可能只执行多语句 SQL 的第一条，逐条执行确保索引全部创建。
	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("创建 queue_tasks 表/索引失败: %w", err)
		}
	}
	return &Queue{
		db:           db,
		mux:          mux,
		concurrency:  cfg.Concurrency,
		maxRetry:     cfg.MaxRetry,
		minBackoff:   cfg.MinBackoff,
		maxBackoff:   cfg.MaxBackoff,
		pollInterval: cfg.PollInterval,
		notify:       make(chan struct{}, 1),
		stop:         make(chan struct{}),
	}, nil
}

// Enqueue 实现 queueport.JobQueue。
// TaskID 非空时在 pending/active 期间唯一（UNIQUE INDEX 保证幂等去重）。
// Delay > 0 时 scheduled_at 设为未来时间，到期前 worker 不选取。
func (q *Queue) Enqueue(ctx context.Context, job queueport.JobRequest) (*queueport.JobHandle, error) {
	if q.stopped.Load() {
		return nil, fmt.Errorf("队列已停止，拒绝入队")
	}
	scheduled := time.Now()
	if job.Delay > 0 {
		scheduled = scheduled.Add(time.Duration(job.Delay))
	}
	maxRetry := job.MaxRetry
	if maxRetry == 0 {
		maxRetry = q.maxRetry
	}
	_, err := q.db.ExecContext(ctx,
		`INSERT INTO queue_tasks (task_id, type, payload, state, max_retry, timeout_ms, scheduled_at, enqueued_at)
		 VALUES (?, ?, ?, 'pending', ?, ?, ?, ?)`,
		job.TaskID, job.Type, payloadOrDefault(job.Payload), maxRetry, job.Timeout.Milliseconds(),
		scheduled.UnixMilli(), time.Now().UnixMilli(),
	)
	if err != nil {
		// SQLite UNIQUE 约束冲突（SQLITE_CONSTRAINT_UNIQUE = 2067）→ 任务已在队列中
		return nil, fmt.Errorf("任务 %s 已在队列中: %w", job.TaskID, err)
	}
	q.wake()
	return &queueport.JobHandle{ID: job.TaskID}, nil
}

// wake 通知 worker 立即检查（非阻塞）。
func (q *Queue) wake() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Start 恢复崩溃任务并启动 worker goroutines。
func (q *Queue) Start(ctx context.Context) {
	// 重启恢复：上次崩溃时处于 active 的任务重置为 pending 重试。
	if _, err := q.db.ExecContext(ctx,
		`UPDATE queue_tasks SET state='pending', last_error='重启恢复：上次未完成任务' WHERE state='active'`,
	); err != nil {
		// 非致命：worker 仍能处理 pending 任务
		fmt.Printf("警告: 重启恢复 active→pending 失败: %v\n", err)
	}
	for i := 0; i < q.concurrency; i++ {
		q.wg.Add(1)
		go q.runWorker(ctx)
	}
}

func (q *Queue) runWorker(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stop:
			return
		case <-q.notify:
		case <-time.After(q.pollInterval):
		}
		if q.stopped.Load() {
			return
		}
		q.processNext(ctx)
	}
}

func (q *Queue) processNext(ctx context.Context) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	var (
		id        int64
		typ       string
		payload   []byte
		attempts  int
		maxRetry  int
		timeoutMs int
	)
	row := tx.QueryRowContext(ctx,
		`SELECT id, type, payload, attempts, max_retry, timeout_ms
		 FROM queue_tasks
		 WHERE state='pending' AND scheduled_at <= ?
		 ORDER BY enqueued_at LIMIT 1`,
		time.Now().UnixMilli(),
	)
	if err := row.Scan(&id, &typ, &payload, &attempts, &maxRetry, &timeoutMs); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			// 非致命扫描错误日志（不做完整 log 依赖）
		}
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE queue_tasks SET state='active', attempts=attempts+1 WHERE id=?`, id,
	); err != nil {
		return
	}
	if err := tx.Commit(); err != nil {
		return
	}

	// 执行任务（事务外，避免长事务占用连接）
	taskCtx := ctx
	var cancel context.CancelFunc
	if timeoutMs > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
	}
	task := hibikenasynq.NewTask(typ, payload)
	execErr := q.mux.ProcessTask(taskCtx, task)

	if execErr == nil || errors.Is(execErr, hibikenasynq.SkipRetry) {
		_, _ = q.db.ExecContext(ctx, `DELETE FROM queue_tasks WHERE id=?`, id)
		q.processed.Add(1)
		return
	}
	// 失败处理
	newAttempts := attempts + 1
	if newAttempts > maxRetry {
		_, _ = q.db.ExecContext(ctx,
			`UPDATE queue_tasks SET state='dead', last_error=?, dead_at=? WHERE id=?`,
			truncateErr(execErr), time.Now().UnixMilli(), id,
		)
		q.failed.Add(1)
		return
	}
	// 重试：退避后重新 pending
	backoff := q.backoff(newAttempts)
	_, _ = q.db.ExecContext(ctx,
		`UPDATE queue_tasks SET state='pending', scheduled_at=?, last_error=? WHERE id=?`,
		time.Now().Add(backoff).UnixMilli(), truncateErr(execErr), id,
	)
	// 退避到期后通知 worker
	time.AfterFunc(backoff, q.wake)
}

func (q *Queue) backoff(attempts int) time.Duration {
	d := q.minBackoff << uint(attempts-1)
	if d > q.maxBackoff || d <= 0 {
		d = q.maxBackoff
	}
	return d
}

// Stop 停止 worker，等待运行中任务完成或 ctx 超时。可多次调用。
func (q *Queue) Stop(ctx context.Context) error {
	if !q.stopped.CompareAndSwap(false, true) {
		return nil
	}
	close(q.stop)
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- Inspector 辅助方法 ---

// Stats 返回队列计数快照。
func (q *Queue) Stats() (pending, active, dead int, processed, failed int64) {
	processed = q.processed.Load()
	failed = q.failed.Load()
	q.db.QueryRow(`SELECT COUNT(*) FROM queue_tasks WHERE state='pending'`).Scan(&pending)
	q.db.QueryRow(`SELECT COUNT(*) FROM queue_tasks WHERE state='active'`).Scan(&active)
	q.db.QueryRow(`SELECT COUNT(*) FROM queue_tasks WHERE state='dead'`).Scan(&dead)
	return
}

// ListDeadRow 是死信行（含 payload 供 RetryDead）。
type ListDeadRow struct {
	ID        int64
	TaskID    string
	Type      string
	Payload   []byte
	LastError string
	Attempts  int
	MaxRetry  int
	DeadAtMs  sql.NullInt64 // Unix milliseconds
}

// ListDead 返回死信分页。
func (q *Queue) ListDead(page, pageSize int) []ListDeadRow {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	rows, err := q.db.Query(
		`SELECT id, task_id, type, payload, last_error, attempts, max_retry, dead_at
		 FROM queue_tasks WHERE state='dead' ORDER BY dead_at DESC LIMIT ? OFFSET ?`,
		pageSize, offset,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ListDeadRow
	for rows.Next() {
		var r ListDeadRow
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Type, &r.Payload, &r.LastError, &r.Attempts, &r.MaxRetry, &r.DeadAtMs); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

// RetryDead 把指定 id 的死信重投（重置 attempts + 携带原 payload）。
func (q *Queue) RetryDead(id int64) bool {
	_, err := q.db.Exec(
		`UPDATE queue_tasks SET state='pending', attempts=0, scheduled_at=?, last_error='', dead_at=NULL WHERE id=? AND state='dead'`,
		time.Now().UnixMilli(), id,
	)
	if err != nil {
		return false
	}
	q.wake()
	return true
}

// RetryDeadByTaskID 按 task_id 重投死信。
func (q *Queue) RetryDeadByTaskID(taskID string) bool {
	var id int64
	err := q.db.QueryRow(`SELECT id FROM queue_tasks WHERE task_id=? AND state='dead' LIMIT 1`, taskID).Scan(&id)
	if err != nil {
		return false
	}
	return q.RetryDead(id)
}

// DeleteDead 删除指定 id 的死信。
func (q *Queue) DeleteDead(id int64) bool {
	_, err := q.db.Exec(`DELETE FROM queue_tasks WHERE id=? AND state='dead'`, id)
	return err == nil
}

// DeleteDeadByTaskID 按 task_id 删除死信。
func (q *Queue) DeleteDeadByTaskID(taskID string) bool {
	_, err := q.db.Exec(`DELETE FROM queue_tasks WHERE task_id=? AND state='dead'`, taskID)
	return err == nil
}

func truncateErr(err error) string {
	s := err.Error()
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func payloadOrDefault(p []byte) []byte {
	if p == nil {
		return []byte{}
	}
	return p
}
