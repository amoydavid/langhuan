package service

import (
	"context"
	"errors"
	"testing"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/ports/queue"
	"github.com/google/uuid"
)

// --- cleanup scheduler tests ------------------------------------------

// cleanupSchedulerQueue 记录每次入队尝试（即使失败也记录），用于断言 dispatch 行为。
// 与 fakeSyncQueue 不同，它在注入 err 时仍累加 attempts，便于验证"失败不中断后续"。
type cleanupSchedulerQueue struct {
	requests []queue.JobRequest
	attempts int
	err      error
}

func (q *cleanupSchedulerQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	q.attempts++
	if q.err != nil {
		return nil, q.err
	}
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: "queued"}, nil
}

// newCleanupSchedulerHarness 构造一个 scheduler + fakes（store + queue + logger）。
func newCleanupSchedulerHarness() (*SourceCleanupScheduler, *fakeCleanupStore, *cleanupSchedulerQueue) {
	store := &fakeCleanupStore{}
	queue := &cleanupSchedulerQueue{}
	scheduler := NewSourceCleanupScheduler(SourceCleanupSchedulerDeps{
		Store: store, Queue: queue,
		Interval: 50 * time.Millisecond,
		Logger:   &recordingLogger{},
	})
	return scheduler, store, queue
}

// TestCleanupSchedulerRequeuePendingEnqueuesAllPendingJobs 验证 RequeuePending 入队所有 pending cleanup Job。
func TestCleanupSchedulerRequeuePendingEnqueuesAllPendingJobs(t *testing.T) {
	scheduler, store, queue := newCleanupSchedulerHarness()
	ws := uuid.New()
	kb := uuid.New()
	store.pending = []DueCleanupJob{
		{WorkspaceID: ws, KnowledgeBaseID: kb, JobID: uuid.New()},
		{WorkspaceID: ws, KnowledgeBaseID: kb, JobID: uuid.New()},
	}

	if err := scheduler.RequeuePending(context.Background()); err != nil {
		t.Fatalf("RequeuePending err = %v", err)
	}
	if got, want := len(queue.requests), 2; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	// 入队的 TaskID 应按 job_id 区分。
	taskIDs := map[string]bool{}
	for _, req := range queue.requests {
		if req.TaskID == "" {
			t.Fatal("入队请求缺少 TaskID")
		}
		taskIDs[req.TaskID] = true
	}
	if len(taskIDs) != 2 {
		t.Fatalf("task IDs 去重后 = %d, want 2", len(taskIDs))
	}
}

// TestCleanupSchedulerTickEnqueuesPending 验证 Tick 与 RequeuePending 行为一致。
func TestCleanupSchedulerTickEnqueuesPending(t *testing.T) {
	scheduler, store, queue := newCleanupSchedulerHarness()
	ws := uuid.New()
	kb := uuid.New()
	store.pending = []DueCleanupJob{
		{WorkspaceID: ws, KnowledgeBaseID: kb, JobID: uuid.New()},
	}

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if len(queue.requests) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.requests))
	}
}

// TestCleanupSchedulerNoopWhenNoPending 验证无 pending Job 时为空操作。
func TestCleanupSchedulerNoopWhenNoPending(t *testing.T) {
	scheduler, _, queue := newCleanupSchedulerHarness()
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if len(queue.requests) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.requests))
	}
}

// TestCleanupSchedulerEnqueueFailureLeavesPending 验证入队失败不中断后续 + 不返回错误（保留 pending）。
func TestCleanupSchedulerEnqueueFailureLeavesPending(t *testing.T) {
	scheduler, store, q := newCleanupSchedulerHarness()
	ws := uuid.New()
	kb := uuid.New()
	store.pending = []DueCleanupJob{
		{WorkspaceID: ws, KnowledgeBaseID: kb, JobID: uuid.New()},
		{WorkspaceID: ws, KnowledgeBaseID: kb, JobID: uuid.New()},
	}
	q.err = errors.New("redis down")

	// 入队失败不应返回错误（dispatch 内部吞掉，只记 warning）；Job 保留 pending。
	if err := scheduler.RequeuePending(context.Background()); err != nil {
		t.Fatalf("RequeuePending err = %v, want nil (enqueue failures are warnings)", err)
	}
	// 两个 Job 都被尝试入队（attempts 累加，即使每次都失败）。
	if got, want := q.attempts, 2; got != want {
		t.Fatalf("enqueue attempts = %d, want %d (all attempted)", got, want)
	}
	if len(q.requests) != 0 {
		t.Fatalf("no successful enqueues expected on queue error; got %d", len(q.requests))
	}
}

// TestCleanupSchedulerPropagatesListError 验证 ListPendingSourceCleanupJobs 错误向上冒泡。
func TestCleanupSchedulerPropagatesListError(t *testing.T) {
	scheduler, store, queue := newCleanupSchedulerHarness()
	store.listErr = errors.New("db down")
	if err := scheduler.Tick(context.Background()); err == nil {
		t.Fatal("Tick err = nil, want list error")
	}
	if len(queue.requests) != 0 {
		t.Fatalf("no enqueue expected on list error")
	}
}

// TestCleanupSchedulerRejectsMissingDeps 验证依赖缺失返回 ErrValidation。
func TestCleanupSchedulerRejectsMissingDeps(t *testing.T) {
	s := &SourceCleanupScheduler{} // 全部依赖 nil
	if err := s.Tick(context.Background()); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("Tick err = %v, want ErrValidation", err)
	}
}

// TestNewCleanupSchedulerAppliesDefaults 验证构造器兜底默认 interval/logger。
func TestNewCleanupSchedulerAppliesDefaults(t *testing.T) {
	s := NewSourceCleanupScheduler(SourceCleanupSchedulerDeps{
		Store: &fakeCleanupStore{}, Queue: &fakeSyncQueue{},
	})
	if s.interval != 60*time.Second {
		t.Fatalf("default interval = %v, want 60s", s.interval)
	}
	if s.logger == nil {
		t.Fatal("default logger should be noop, not nil")
	}
}
