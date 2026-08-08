package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// --- cleanup worker fakes ---------------------------------------------

// sourceCleanupRunnerSpy 记录 Run 调用，可注入错误。
type sourceCleanupRunnerSpy struct {
	calls []sourceCleanupRunCall
	err   error
}

type sourceCleanupRunCall struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	JobID           uuid.UUID
}

func (r *sourceCleanupRunnerSpy) Run(_ context.Context, workspaceID, kbID, jobID uuid.UUID) error {
	r.calls = append(r.calls, sourceCleanupRunCall{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, JobID: jobID,
	})
	return r.err
}

// sourceCleanupTaskStoreSpy 记录 MarkRunning 调用，可注入错误。
type sourceCleanupTaskStoreSpy struct {
	running    map[uuid.UUID]bool
	runningErr error
}

func (s *sourceCleanupTaskStoreSpy) MarkRunning(_ context.Context, _ uuid.UUID, jobID uuid.UUID) error {
	if s.runningErr != nil {
		return s.runningErr
	}
	if s.running == nil {
		s.running = map[uuid.UUID]bool{}
	}
	s.running[jobID] = true
	return nil
}

// --- cleanup worker tests ---------------------------------------------

// TestSourceCleanupHandleForwardsLineageAndMarksRunning 验证 Handle 解码 payload、调 Runner.Run、MarkRunning。
func TestSourceCleanupHandleForwardsLineageAndMarksRunning(t *testing.T) {
	runner := &sourceCleanupRunnerSpy{}
	store := &sourceCleanupTaskStoreSpy{}
	handler := SourceCleanupHandler{Runner: runner, Store: store}
	payload := SourceCleanupTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceCleanup, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if runner.calls[0].JobID != payload.JobID || runner.calls[0].WorkspaceID != payload.WorkspaceID ||
		runner.calls[0].KnowledgeBaseID != payload.KnowledgeBaseID {
		t.Fatalf("runner call lineage = %+v, want %+v", runner.calls[0], payload)
	}
	if !store.running[payload.JobID] {
		t.Fatalf("MarkRunning not called for job %s", payload.JobID)
	}
}

// TestSourceCleanupHandleReturnsErrorOnRunnerFailure 验证 Runner 失败时返回错误（让 asynq 重试）。
func TestSourceCleanupHandleReturnsErrorOnRunnerFailure(t *testing.T) {
	runner := &sourceCleanupRunnerSpy{err: errors.New("cleanup failed")}
	store := &sourceCleanupTaskStoreSpy{}
	handler := SourceCleanupHandler{Runner: runner, Store: store}
	payload := SourceCleanupTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	err = handler.Handle(context.Background(), asynq.NewTask(TaskSourceCleanup, encoded))
	if err == nil {
		t.Fatal("Handle err = nil, want runner error")
	}
	if !errors.Is(err, runner.err) {
		t.Fatalf("err = %v, want %v", err, runner.err)
	}
	if !store.running[payload.JobID] {
		t.Fatal("MarkRunning 应在 Runner 之前调用")
	}
}

// TestSourceCleanupHandleRejectsEmptyLineage 验证 lineage 缺失返回 SkipRetry 永久错误。
func TestSourceCleanupHandleRejectsEmptyLineage(t *testing.T) {
	runner := &sourceCleanupRunnerSpy{}
	store := &sourceCleanupTaskStoreSpy{}
	handler := SourceCleanupHandler{Runner: runner, Store: store}
	// 缺 JobID。
	encoded, err := json.Marshal(SourceCleanupTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = handler.Handle(context.Background(), asynq.NewTask(TaskSourceCleanup, encoded))
	if err == nil {
		t.Fatal("Handle err = nil, want lineage error")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("lineage error should SkipRetry; got %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner should not be called on bad payload; calls = %#v", runner.calls)
	}
}

// TestSourceCleanupHandleSkipRetryOnCorruptPayload 验证 payload 解析失败返回 SkipRetry。
func TestSourceCleanupHandleSkipRetryOnCorruptPayload(t *testing.T) {
	runner := &sourceCleanupRunnerSpy{}
	handler := SourceCleanupHandler{Runner: runner}
	err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceCleanup, []byte("not-json")))
	if err == nil {
		t.Fatal("Handle err = nil, want parse error")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("corrupt payload should SkipRetry; got %v", err)
	}
}

// TestSourceCleanupHandleHandlesNilStoreGracefully 验证无 Store 时仍正常执行。
func TestSourceCleanupHandleHandlesNilStoreGracefully(t *testing.T) {
	runner := &sourceCleanupRunnerSpy{}
	handler := SourceCleanupHandler{Runner: runner} // no Store
	payload := SourceCleanupTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceCleanup, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
}

// TestSourceCleanupHandleReturnsMarkRunningError 验证 MarkRunning 失败时返回错误（可重试）。
func TestSourceCleanupHandleReturnsMarkRunningError(t *testing.T) {
	runner := &sourceCleanupRunnerSpy{}
	store := &sourceCleanupTaskStoreSpy{runningErr: errors.New("db down")}
	handler := SourceCleanupHandler{Runner: runner, Store: store}
	payload := SourceCleanupTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(),
	}
	encoded, _ := json.Marshal(payload)
	err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceCleanup, encoded))
	if err == nil {
		t.Fatal("Handle err = nil, want MarkRunning error")
	}
	if !errors.Is(err, store.runningErr) {
		t.Fatalf("err = %v, want %v", err, store.runningErr)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner should not be called when MarkRunning fails; calls = %d", len(runner.calls))
	}
}

// TestRegisterSourceCleanupHandlerWiresTaskType 验证注册不 panic。
func TestRegisterSourceCleanupHandlerWiresTaskType(t *testing.T) {
	mux := asynq.NewServeMux()
	RegisterSourceCleanupHandler(mux, SourceCleanupHandler{Runner: &sourceCleanupRunnerSpy{}})
}
