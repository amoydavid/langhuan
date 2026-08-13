// Package dbqueue 实现基于 SQLite 或 PostgreSQL 的持久化任务队列，
// 供无 Redis 部署使用（PG + 队列表 / SQLite standalone）。
//
// 与内存队列（adapters/queue/memory）一样复用 *asynq.ServeMux.ProcessTask 执行
// 已注册的 worker handler，payload 解码与业务幂等逻辑零改动。区别在于任务持久化
// 到 queue_tasks 表（SQLite 或 PostgreSQL），进程重启后 pending/active（crashed）
// 自动恢复重试。
//
// 部署模式：PG+Redis(asynq) / PG+队列表(无Redis) / SQLite全栈(standalone)。
// queue_tasks 表由数据库迁移建好（PG 000024 / SQLite 000005），本包不建表。
package dbqueue

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

// Dialect 标识队列底层数据库方言。
type Dialect int

const (
	// DialectSQLite 对应 SQLite 队列表（? 占位符）。
	DialectSQLite Dialect = iota
	// DialectPostgres 对应 PostgreSQL 队列表（$N 占位符）。
	DialectPostgres
)

// TaskRunner 执行单个任务。asynq.ServeMux 实现该接口。
type TaskRunner interface {
	ProcessTask(ctx context.Context, t *hibikenasynq.Task) error
}

// 编译期断言。
var _ queueport.JobQueue = (*Queue)(nil)

// Config 描述队列运行参数。
type Config struct {
	Concurrency  int
	MaxRetry     int
	MinBackoff   time.Duration
	MaxBackoff   time.Duration
	PollInterval time.Duration
}

// Queue 是基于 SQLite/PostgreSQL 的持久化任务队列。
type Queue struct {
	db           *sql.DB
	dialect      Dialect
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

// New 构造持久化队列。db 应为与业务共享的 *sql.DB（PG 或 SQLite 连接）。
// dialect 标识底层方言，决定占位符与领取 SQL。mux 是注册了所有 worker handler
// 的 *asynq.ServeMux。
//
// queue_tasks 表必须已由数据库迁移创建（PG 000024 / SQLite 000005）。
func New(db *sql.DB, dialect Dialect, mux TaskRunner, cfg Config) (*Queue, error) {
	if db == nil {
		return nil, fmt.Errorf("dbqueue: 数据库连接不能为空")
	}
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
	return &Queue{
		db:           db,
		dialect:      dialect,
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
// TaskID 非空时在 pending/active 期间唯一（部分唯一索引保证幂等去重）。
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
		q.sqlInsert(),
		job.TaskID, job.Type, payloadOrDefault(job.Payload), maxRetry, job.Timeout.Milliseconds(),
		scheduled.UnixMilli(), time.Now().UnixMilli(),
	)
	if err != nil {
		// 唯一索引冲突（UNIQUE/PK）→ 任务已在队列中
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
	if _, err := q.db.ExecContext(ctx, q.sqlRecoverActive()); err != nil {
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
	var (
		id        int64
		typ       string
		payload   []byte
		attempts  int
		maxRetry  int
		timeoutMs int
	)
	if q.dialect == DialectPostgres {
		// PG：FOR UPDATE SKIP LOCKED 原子领取（多 worker 并发安全）
		err := q.db.QueryRowContext(ctx, q.sqlClaim(),
			time.Now().UnixMilli(),
		).Scan(&id, &typ, &payload, &attempts, &maxRetry, &timeoutMs)
		if err != nil {
			return // sql.ErrNoRows 或其它错误：无任务可处理
		}
		// 更新为 active（attempts 已在 claim 中 +1）
		q.db.ExecContext(ctx, q.sqlMarkActive(), id)
	} else {
		// SQLite：事务内 SELECT + UPDATE active（单写锁串行）
		tx, err := q.db.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		row := tx.QueryRowContext(ctx, q.sqlClaim(),
			time.Now().UnixMilli(),
		)
		err = row.Scan(&id, &typ, &payload, &attempts, &maxRetry, &timeoutMs)
		if err != nil {
			tx.Rollback()
			return
		}
		if _, err := tx.ExecContext(ctx, q.sqlMarkActive(), id); err != nil {
			tx.Rollback()
			return
		}
		if err := tx.Commit(); err != nil {
			return
		}
	}
	attempts++ // claim 时 attempts 已 +1，此处记录当前执行次数
	q.execute(ctx, id, typ, payload, attempts, maxRetry, timeoutMs)
}

// execute 执行任务并更新状态（事务外，避免长事务占用连接）。
func (q *Queue) execute(ctx context.Context, id int64, typ string, payload []byte, attempts, maxRetry, timeoutMs int) {
	taskCtx := ctx
	var cancel context.CancelFunc
	if timeoutMs > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()
	}
	task := hibikenasynq.NewTask(typ, payload)
	execErr := q.mux.ProcessTask(taskCtx, task)

	if execErr == nil || errors.Is(execErr, hibikenasynq.SkipRetry) {
		_, _ = q.db.ExecContext(ctx, q.sqlDelete(), id)
		q.processed.Add(1)
		return
	}
	if attempts > maxRetry {
		_, _ = q.db.ExecContext(ctx, q.sqlMarkDead(), truncateErr(execErr), time.Now().UnixMilli(), id)
		q.failed.Add(1)
		return
	}
	// 重试：退避后重新 pending
	backoff := q.backoff(attempts)
	_, _ = q.db.ExecContext(ctx, q.sqlReschedule(), time.Now().Add(backoff).UnixMilli(), truncateErr(execErr), id)
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
	q.db.QueryRow(q.sqlCount("pending")).Scan(&pending)
	q.db.QueryRow(q.sqlCount("active")).Scan(&active)
	q.db.QueryRow(q.sqlCount("dead")).Scan(&dead)
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
	rows, err := q.db.Query(q.sqlListDead(), pageSize, offset)
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
	_, err := q.db.Exec(q.sqlRetryDead(), time.Now().UnixMilli(), id)
	if err != nil {
		return false
	}
	q.wake()
	return true
}

// RetryDeadByTaskID 按 task_id 重投死信。
func (q *Queue) RetryDeadByTaskID(taskID string) bool {
	var id int64
	err := q.db.QueryRow(q.sqlSelectDeadIDByTaskID(), taskID).Scan(&id)
	if err != nil {
		return false
	}
	return q.RetryDead(id)
}

// DeleteDead 删除指定 id 的死信。
func (q *Queue) DeleteDead(id int64) bool {
	_, err := q.db.Exec(q.sqlDeleteDead(), id)
	return err == nil
}

// DeleteDeadByTaskID 按 task_id 删除死信。
func (q *Queue) DeleteDeadByTaskID(taskID string) bool {
	_, err := q.db.Exec(q.sqlDeleteDeadByTaskID(), taskID)
	return err == nil
}

// --- 方言 SQL ---

func (q *Queue) ph(index int) string {
	if q.dialect == DialectPostgres {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

func (q *Queue) sqlInsert() string {
	if q.dialect == DialectPostgres {
		return `INSERT INTO queue_tasks (task_id, type, payload, state, max_retry, timeout_ms, scheduled_at, enqueued_at)
		        VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7)`
	}
	return `INSERT INTO queue_tasks (task_id, type, payload, state, max_retry, timeout_ms, scheduled_at, enqueued_at)
	        VALUES (?, ?, ?, 'pending', ?, ?, ?, ?)`
}

func (q *Queue) sqlClaim() string {
	if q.dialect == DialectPostgres {
		return `SELECT id, type, payload, attempts, max_retry, timeout_ms
		        FROM queue_tasks
		        WHERE state='pending' AND scheduled_at <= $1
		        ORDER BY enqueued_at
		        FOR UPDATE SKIP LOCKED LIMIT 1`
	}
	return `SELECT id, type, payload, attempts, max_retry, timeout_ms
	        FROM queue_tasks
	        WHERE state='pending' AND scheduled_at <= ?
	        ORDER BY enqueued_at LIMIT 1`
}

func (q *Queue) sqlMarkActive() string {
	if q.dialect == DialectPostgres {
		return `UPDATE queue_tasks SET state='active', attempts=attempts+1 WHERE id=$1`
	}
	return `UPDATE queue_tasks SET state='active', attempts=attempts+1 WHERE id=?`
}

func (q *Queue) sqlRecoverActive() string {
	if q.dialect == DialectPostgres {
		return `UPDATE queue_tasks SET state='pending', last_error='重启恢复：上次未完成任务' WHERE state='active'`
	}
	return `UPDATE queue_tasks SET state='pending', last_error='重启恢复：上次未完成任务' WHERE state='active'`
}

func (q *Queue) sqlDelete() string {
	if q.dialect == DialectPostgres {
		return `DELETE FROM queue_tasks WHERE id=$1`
	}
	return `DELETE FROM queue_tasks WHERE id=?`
}

func (q *Queue) sqlMarkDead() string {
	if q.dialect == DialectPostgres {
		return `UPDATE queue_tasks SET state='dead', last_error=$1, dead_at=$2 WHERE id=$3`
	}
	return `UPDATE queue_tasks SET state='dead', last_error=?, dead_at=? WHERE id=?`
}

func (q *Queue) sqlReschedule() string {
	if q.dialect == DialectPostgres {
		return `UPDATE queue_tasks SET state='pending', scheduled_at=$1, last_error=$2 WHERE id=$3`
	}
	return `UPDATE queue_tasks SET state='pending', scheduled_at=?, last_error=? WHERE id=?`
}

func (q *Queue) sqlCount(state string) string {
	if q.dialect == DialectPostgres {
		return `SELECT COUNT(*) FROM queue_tasks WHERE state='` + state + `'`
	}
	return `SELECT COUNT(*) FROM queue_tasks WHERE state='` + state + `'`
}

func (q *Queue) sqlListDead() string {
	if q.dialect == DialectPostgres {
		return `SELECT id, task_id, type, payload, last_error, attempts, max_retry, dead_at
		        FROM queue_tasks WHERE state='dead' ORDER BY dead_at DESC LIMIT $1 OFFSET $2`
	}
	return `SELECT id, task_id, type, payload, last_error, attempts, max_retry, dead_at
	        FROM queue_tasks WHERE state='dead' ORDER BY dead_at DESC LIMIT ? OFFSET ?`
}

func (q *Queue) sqlRetryDead() string {
	if q.dialect == DialectPostgres {
		return `UPDATE queue_tasks SET state='pending', attempts=0, scheduled_at=$1, last_error='', dead_at=NULL WHERE id=$2 AND state='dead'`
	}
	return `UPDATE queue_tasks SET state='pending', attempts=0, scheduled_at=?, last_error='', dead_at=NULL WHERE id=? AND state='dead'`
}

func (q *Queue) sqlSelectDeadIDByTaskID() string {
	if q.dialect == DialectPostgres {
		return `SELECT id FROM queue_tasks WHERE task_id=$1 AND state='dead' LIMIT 1`
	}
	return `SELECT id FROM queue_tasks WHERE task_id=? AND state='dead' LIMIT 1`
}

func (q *Queue) sqlDeleteDead() string {
	if q.dialect == DialectPostgres {
		return `DELETE FROM queue_tasks WHERE id=$1 AND state='dead'`
	}
	return `DELETE FROM queue_tasks WHERE id=? AND state='dead'`
}

func (q *Queue) sqlDeleteDeadByTaskID() string {
	if q.dialect == DialectPostgres {
		return `DELETE FROM queue_tasks WHERE task_id=$1 AND state='dead'`
	}
	return `DELETE FROM queue_tasks WHERE task_id=? AND state='dead'`
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
