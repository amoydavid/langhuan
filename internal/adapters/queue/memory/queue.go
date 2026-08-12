// Package memory 实现进程内内存队列，供 standalone 无 Redis 模式使用（spec §10.2）。
//
// 复用现有 asynq worker handler：内存 runtime 持有同一个 *asynq.ServeMux，
// worker goroutine 从 pending 取任务，构造 asynq.NewTask(type, payload) 后调用
// mux.ProcessContext 执行。因此所有 worker.RegisterXxxHandler、payload 解码、
// SkipRetry 与业务幂等逻辑无需改动。
//
// 不承诺跨进程持久化：进程退出时未完成任务允许丢失，仅由现有 source cleanup/
// force latch 补偿与用户 retry/reindex 恢复（spec §1.1、§11）。
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	hibikenasynq "github.com/hibiken/asynq"

	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

// TaskRunner 执行单个任务。asynq.ServeMux 实现该接口（ProcessTask，即 asynq.Handler）。
type TaskRunner interface {
	ProcessTask(ctx context.Context, t *hibikenasynq.Task) error
}

// Queue 是进程内内存队列，实现 queueport.JobQueue。
type Queue struct {
	mux         TaskRunner
	pending     chan *item
	concurrency int
	maxRetry    int
	minBackoff  time.Duration
	maxBackoff  time.Duration

	mu       sync.Mutex
	inFlight map[string]struct{} // TaskID 去重（pending/active 期间占用）
	wg       sync.WaitGroup
	stop     chan struct{}
	stopped  bool
}

type item struct {
	typ        string
	payload    []byte
	taskID     string
	attempts   int // 已执行次数（含当前）
	maxRetry   int
	timeout    time.Duration
	enqueuedAt time.Time
}

// Config 描述内存队列运行参数。
type Config struct {
	Concurrency int
	Capacity    int
	MaxRetry    int
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
}

// New 构造内存队列。mux 通常是注册了所有 worker handler 的 *asynq.ServeMux。
func New(mux TaskRunner, cfg Config) *Queue {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.Capacity < cfg.Concurrency {
		cfg.Capacity = cfg.Concurrency
	}
	if cfg.MaxRetry < 0 {
		cfg.MaxRetry = 5
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff <= cfg.MinBackoff {
		cfg.MaxBackoff = cfg.MinBackoff * 60
	}
	return &Queue{
		mux:         mux,
		pending:     make(chan *item, cfg.Capacity),
		concurrency: cfg.Concurrency,
		maxRetry:    cfg.MaxRetry,
		minBackoff:  cfg.MinBackoff,
		maxBackoff:  cfg.MaxBackoff,
		inFlight:    make(map[string]struct{}),
		stop:        make(chan struct{}),
	}
}

// Enqueue 实现 queueport.JobQueue。
// TaskID 在 pending/active 期间唯一占用（与 asynq 显式 TaskID 语义一致）。
// Delay > 0 时由独立 scheduler goroutine 延后投递。
func (q *Queue) Enqueue(ctx context.Context, job queueport.JobRequest) (*queueport.JobHandle, error) {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return nil, fmt.Errorf("内存队列已停止，拒绝入队")
	}
	if job.TaskID != "" {
		if _, exists := q.inFlight[job.TaskID]; exists {
			q.mu.Unlock()
			return nil, fmt.Errorf("任务 %s 已在队列中", job.TaskID)
		}
		q.inFlight[job.TaskID] = struct{}{}
	}
	q.mu.Unlock()

	maxRetry := job.MaxRetry
	if maxRetry == 0 {
		maxRetry = q.maxRetry
	}
	it := &item{
		typ:        job.Type,
		payload:    job.Payload,
		taskID:     job.TaskID,
		maxRetry:   maxRetry,
		timeout:    job.Timeout,
		enqueuedAt: time.Now(),
	}

	if job.Delay > 0 {
		go func() {
			t := time.NewTimer(time.Duration(job.Delay))
			defer t.Stop()
			select {
			case <-t.C:
				q.push(it)
			case <-q.stop:
				q.releaseSlot(it.taskID)
			}
		}()
		return &queueport.JobHandle{ID: job.TaskID}, nil
	}
	if !q.push(it) {
		q.releaseSlot(it.taskID)
		return nil, fmt.Errorf("内存队列已满（capacity），拒绝入队")
	}
	return &queueport.JobHandle{ID: job.TaskID}, nil
}

// push 非阻塞投递；队列满返回 false。
func (q *Queue) push(it *item) bool {
	select {
	case q.pending <- it:
		return true
	default:
		return false
	}
}

// Start 启动 worker goroutines 消费 pending。重复调用 panic。
func (q *Queue) Start(ctx context.Context) {
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
		case it := <-q.pending:
			q.execute(ctx, it)
		}
	}
}

func (q *Queue) execute(ctx context.Context, it *item) {
	defer q.releaseSlot(it.taskID)

	taskCtx := ctx
	var cancel context.CancelFunc
	if it.timeout > 0 {
		taskCtx, cancel = context.WithTimeout(ctx, it.timeout)
		defer cancel()
	}
	it.attempts++
	task := hibikenasynq.NewTask(it.typ, it.payload)
	err := q.mux.ProcessTask(taskCtx, task)
	if err == nil || err == hibikenasynq.SkipRetry {
		return
	}
	// 失败：按指数退避重试
	if it.attempts > it.maxRetry {
		return // 超出重试次数，丢弃（standalone 边界）
	}
	backoff := q.backoff(it.attempts)
	go func() {
		t := time.NewTimer(backoff)
		defer t.Stop()
		select {
		case <-t.C:
			// 重新占用 TaskID 槽位后投递
			q.reacquireAndPush(it)
		case <-q.stop:
		}
	}()
}

func (q *Queue) reacquireAndPush(it *item) {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return
	}
	if it.taskID != "" {
		q.inFlight[it.taskID] = struct{}{}
	}
	q.mu.Unlock()
	if !q.push(it) {
		q.releaseSlot(it.taskID)
	}
}

func (q *Queue) backoff(attempts int) time.Duration {
	d := q.minBackoff << uint(attempts-1)
	if d > q.maxBackoff || d <= 0 {
		d = q.maxBackoff
	}
	return d
}

func (q *Queue) releaseSlot(taskID string) {
	if taskID == "" {
		return
	}
	q.mu.Lock()
	delete(q.inFlight, taskID)
	q.mu.Unlock()
}

// Stop 停止接收与调度，等待运行中任务到 ctx 超时。可多次调用。
func (q *Queue) Stop(ctx context.Context) error {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return nil
	}
	q.stopped = true
	q.mu.Unlock()
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
