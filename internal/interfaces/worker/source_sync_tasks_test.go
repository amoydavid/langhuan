package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

func TestSourceSyncHandleForwardsLineageAndMarksRunning(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	payload := SourceSyncTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(),
		ConnectionID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	// RunSourceSyncJob 应以完整 lineage（含 job_id）被调用一次。
	if len(runner.calls) != 1 ||
		runner.calls[0].WorkspaceID != payload.WorkspaceID ||
		runner.calls[0].KnowledgeBaseID != payload.KnowledgeBaseID ||
		runner.calls[0].JobID != payload.JobID {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
	if !store.running[payload.JobID] {
		t.Fatalf("MarkRunning not called for job %s", payload.JobID)
	}
	// 终态由服务的 FinalizeSourceSyncJob 标记，worker 不再调用 MarkSucceeded/MarkFailed。
	if store.succeeded[payload.JobID] {
		t.Fatalf("worker 不应调用 MarkSucceeded（终态由服务 finalize）")
	}
	if store.failed[payload.JobID] != "" {
		t.Fatalf("worker 不应调用 MarkFailed（终态由服务 finalize）; got %q", store.failed[payload.JobID])
	}
}

func TestSourceSyncHandleRetriesOnTransientError(t *testing.T) {
	runner := &sourceSyncRunnerSpy{err: errors.New("network down")}
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	payload := SourceSyncTaskPayload{WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New()}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	err = handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded))
	if err == nil {
		t.Fatal("Handle err = nil, want transient error")
	}
	if !errors.Is(err, runner.err) {
		t.Fatalf("err = %v, want %v", err, runner.err)
	}
	if errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("transient error should not SkipRetry; got %v", err)
	}
	if !store.running[payload.JobID] {
		t.Fatalf("MarkRunning not called")
	}
	if len(runner.calls) != 1 || runner.calls[0].JobID != payload.JobID {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

func TestSourceSyncHandleSkipRetryOnPermanentError(t *testing.T) {
	perm := fmt.Errorf("%w: 知识库未绑定来源连接", domainerrors.ErrValidation)
	runner := &sourceSyncRunnerSpy{err: perm}
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	payload := SourceSyncTaskPayload{WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New()}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	err = handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded))
	if err == nil {
		t.Fatal("Handle err = nil, want permanent error")
	}
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("permanent error should include SkipRetry; got %v", err)
	}
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("permanent error should wrap ErrValidation; got %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0].JobID != payload.JobID {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

func TestSourceSyncHandleRejectsEmptyLineage(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	// Missing JobID.
	encoded, err := json.Marshal(SourceSyncTaskPayload{WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err == nil {
		t.Fatal("Handle err = nil, want lineage error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner should not be called on bad payload; calls = %#v", runner.calls)
	}
	if len(store.running) != 0 {
		t.Fatalf("MarkRunning should not be called on bad payload")
	}
}

func TestSourceSyncHandleHandlesNilStoreGracefully(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	handler := SourceSyncHandler{Runner: runner} // no Store
	payload := SourceSyncTaskPayload{WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New()}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d", len(runner.calls))
	}
}

// TestSourceSyncHandlePartialRunMarkedSucceeded（spec 12.3）验证：source sync 产生
// partial 结果时 RunSourceSyncJob 返回 nil（partial 不算任务失败），worker 因此把通用 Job
// 标记为 succeeded（终态由服务的 FinalizeSourceSyncJob 写入 completed），绝不调用 MarkFailed。
// 只有 fatal error 才会让 worker 走失败/重试路径（覆盖见 TestSourceSyncHandleSkipRetryOnPermanentError）。
func TestSourceSyncHandlePartialRunMarkedSucceeded(t *testing.T) {
	runner := &sourceSyncRunnerSpy{} // err=nil 模拟 partial/成功：service 对 partial 返回 nil
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	payload := SourceSyncTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("partial sync 应返回 nil（标记 succeeded），got err = %v", err)
	}
	if store.failed[payload.JobID] != "" {
		t.Fatalf("partial sync 不应调用 MarkFailed; got %q", store.failed[payload.JobID])
	}
	// worker 不直接 MarkSucceeded：终态由 service FinalizeSourceSyncJob 写 completed。
	// 这里仅断言未标 failed；succeeded map 也不应有 entry（worker 不调用 MarkSucceeded）。
	if store.succeeded[payload.JobID] {
		t.Fatalf("worker 不应调用 MarkSucceeded（终态由服务 finalize）")
	}
	if len(runner.calls) != 1 || runner.calls[0].JobID != payload.JobID {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

// TestSourceSyncHandleDecodesLegacyPayloadWithoutForce（spec 12.3）验证：旧 source_sync
// payload（仅 lineage/job 字段、缺失任何 force 字段）仍可正常解码并以完整 lineage 调 runner。
// force 的真实值由 worker→service 从 DB latch 原子消费（service 层覆盖见
// TestRunSourceSyncJobLatchFalseRunsNonForced / TestRunSourceSyncJobConsumesForceLatchAndRunsForcedSync）。
func TestSourceSyncHandleDecodesLegacyPayloadWithoutForce(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	ws, kb, job := uuid.New(), uuid.New(), uuid.New()
	// 手工构造一个不含 force 键的旧式 payload（与 SourceSyncTaskPayload 当前形状一致，
	// 但显式用 map 强调"无 force 字段"的兼容语义）。
	legacy, err := json.Marshal(map[string]any{
		"workspace_id":      ws,
		"knowledge_base_id": kb,
		"job_id":            job,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, legacy)); err != nil {
		t.Fatalf("legacy payload Handle err = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if runner.calls[0].WorkspaceID != ws || runner.calls[0].KnowledgeBaseID != kb || runner.calls[0].JobID != job {
		t.Fatalf("lineage = %#v, want ws=%s kb=%s job=%s", runner.calls[0], ws, kb, job)
	}
}

// TestSourceSyncHandleDispatchesNextOnSuccess 验证任务成功后若 payload 带 connection_id，
// 则触发 Dispatcher.TryDispatchConnection 续跑同 connection 的到期 KB。
func TestSourceSyncHandleDispatchesNextOnSuccess(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	dispatcher := &sourceSyncDispatcherSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store, Dispatcher: dispatcher}
	connID := uuid.New()
	payload := SourceSyncTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(), ConnectionID: connID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher calls = %d, want 1", len(dispatcher.calls))
	}
	if dispatcher.calls[0].ConnectionID != connID {
		t.Fatalf("dispatch connection = %s, want %s", dispatcher.calls[0].ConnectionID, connID)
	}
	if dispatcher.calls[0].WorkspaceID != payload.WorkspaceID {
		t.Fatalf("dispatch workspace = %s, want %s", dispatcher.calls[0].WorkspaceID, payload.WorkspaceID)
	}
}

// TestSourceSyncHandleDispatchesNextOnPermanentFailure 验证永久失败（SkipRetry）后也触发续跑。
func TestSourceSyncHandleDispatchesNextOnPermanentFailure(t *testing.T) {
	perm := fmt.Errorf("%w: 知识库未绑定来源连接", domainerrors.ErrValidation)
	runner := &sourceSyncRunnerSpy{err: perm}
	store := &sourceSyncTaskStoreSpy{}
	dispatcher := &sourceSyncDispatcherSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store, Dispatcher: dispatcher}
	connID := uuid.New()
	payload := SourceSyncTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New(), ConnectionID: connID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_ = handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded))
	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher calls = %d, want 1 (permanent failure releases slot)", len(dispatcher.calls))
	}
}

// TestSourceSyncHandleDoesNotDispatchWithoutConnectionID 验证无 connection_id 时不触发续跑。
func TestSourceSyncHandleDoesNotDispatchWithoutConnectionID(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	dispatcher := &sourceSyncDispatcherSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store, Dispatcher: dispatcher}
	payload := SourceSyncTaskPayload{WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), JobID: uuid.New()}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("dispatcher calls = %d, want 0 (no connection_id)", len(dispatcher.calls))
	}
}

func TestIsPermanentSourceSyncTaskError(t *testing.T) {
	if !isPermanentSourceSyncTaskError(fmt.Errorf("%w: x", domainerrors.ErrValidation)) {
		t.Fatal("ErrValidation should be permanent")
	}
	if !isPermanentSourceSyncTaskError(fmt.Errorf("%w: x", domainerrors.ErrNotFound)) {
		t.Fatal("ErrNotFound should be permanent")
	}
	// 停用连接（T13 复审）：哨兵本身，以及经同步编排真实错误链
	// （selector 哨兵 → 「解析来源连接凭证失败: %w」包装）双层包装后，
	// 都必须判为永久错误——停用是用户可自助解决的配置状态，重试无意义。
	if !isPermanentSourceSyncTaskError(service.ErrSourceConnectionDisabled) {
		t.Fatal("ErrSourceConnectionDisabled should be permanent")
	}
	stopped := fmt.Errorf("%w: %s", service.ErrSourceConnectionDisabled, uuid.New())
	wrapped := fmt.Errorf("解析来源连接凭证失败: %w", stopped)
	if !isPermanentSourceSyncTaskError(wrapped) {
		t.Fatalf("wrapped disabled-connection chain should be permanent; err = %v", wrapped)
	}
	if isPermanentSourceSyncTaskError(errors.New("transient")) {
		t.Fatal("generic error should not be permanent")
	}
}

func TestRegisterSourceSyncHandlerWiresTaskType(t *testing.T) {
	mux := asynq.NewServeMux()
	RegisterSourceSyncHandler(mux, SourceSyncHandler{Runner: &sourceSyncRunnerSpy{}})
	// ServeMux 暴露 ProcessTask；这里只断言注册不 panic。
}

// --- fakes -------------------------------------------------------------

type sourceSyncRunnerSpy struct {
	calls       []sourceSyncCall
	sourceCalls []sourceSyncCall
	err         error
}

type sourceSyncCall struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	JobID           uuid.UUID
	SourceID        uuid.UUID
}

func (s *sourceSyncRunnerSpy) RunSourceSyncJob(_ context.Context, workspaceID, kbID, jobID uuid.UUID) error {
	s.calls = append(s.calls, sourceSyncCall{WorkspaceID: workspaceID, KnowledgeBaseID: kbID, JobID: jobID})
	return s.err
}

func (s *sourceSyncRunnerSpy) RunSourceSyncJobForSource(_ context.Context, workspaceID, kbID, sourceID, jobID uuid.UUID) error {
	s.sourceCalls = append(s.sourceCalls, sourceSyncCall{WorkspaceID: workspaceID, KnowledgeBaseID: kbID, SourceID: sourceID, JobID: jobID})
	return s.err
}

// TestSourceSyncHandleDispatchesSourceTargetToSourceRunner 验证 payload 携带 source_id 时
// 分派到源级编排入口（web 内容源），不进入 KB 级入口。
func TestSourceSyncHandleDispatchesSourceTargetToSourceRunner(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store}
	payload := SourceSyncTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		SourceID: uuid.New(), JobID: uuid.New(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("KB 级 runner 不应被调用，calls = %#v", runner.calls)
	}
	if len(runner.sourceCalls) != 1 ||
		runner.sourceCalls[0].WorkspaceID != payload.WorkspaceID ||
		runner.sourceCalls[0].KnowledgeBaseID != payload.KnowledgeBaseID ||
		runner.sourceCalls[0].SourceID != payload.SourceID ||
		runner.sourceCalls[0].JobID != payload.JobID {
		t.Fatalf("source runner calls = %#v", runner.sourceCalls)
	}
}

// TestSourceSyncHandleSourceTargetDispatchesNextOnConnection 验证源级任务（web 内容源，
// payload 带 source_id + connection_id）完成后同样触发同 connection 续跑——
// 有连接的 web 源完成后立即派发同 connection 的下一个到期项（spec §9.1）。
func TestSourceSyncHandleSourceTargetDispatchesNextOnConnection(t *testing.T) {
	runner := &sourceSyncRunnerSpy{}
	store := &sourceSyncTaskStoreSpy{}
	dispatcher := &sourceSyncDispatcherSpy{}
	handler := SourceSyncHandler{Runner: runner, Store: store, Dispatcher: dispatcher}
	connID := uuid.New()
	payload := SourceSyncTaskPayload{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		SourceID: uuid.New(), JobID: uuid.New(), ConnectionID: connID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), asynq.NewTask(TaskSourceSync, encoded)); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if len(runner.sourceCalls) != 1 {
		t.Fatalf("source runner calls = %d, want 1", len(runner.sourceCalls))
	}
	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher calls = %d, want 1（源级任务完成后续跑）", len(dispatcher.calls))
	}
	if dispatcher.calls[0].ConnectionID != connID || dispatcher.calls[0].WorkspaceID != payload.WorkspaceID {
		t.Fatalf("dispatch = %#v, want conn %s", dispatcher.calls[0], connID)
	}
}

type sourceSyncTaskStoreSpy struct {
	running   map[uuid.UUID]bool
	succeeded map[uuid.UUID]bool
	failed    map[uuid.UUID]string
}

func (s *sourceSyncTaskStoreSpy) MarkRunning(_ context.Context, _ uuid.UUID, jobID uuid.UUID) error {
	s.ensure()
	s.running[jobID] = true
	return nil
}

func (s *sourceSyncTaskStoreSpy) MarkSucceeded(_ context.Context, _ uuid.UUID, jobID uuid.UUID) error {
	s.ensure()
	s.succeeded[jobID] = true
	return nil
}

func (s *sourceSyncTaskStoreSpy) MarkFailed(_ context.Context, _ uuid.UUID, jobID uuid.UUID, message string) error {
	s.ensure()
	s.failed[jobID] = message
	return nil
}

func (s *sourceSyncTaskStoreSpy) ensure() {
	if s.running == nil {
		s.running = map[uuid.UUID]bool{}
	}
	if s.succeeded == nil {
		s.succeeded = map[uuid.UUID]bool{}
	}
	if s.failed == nil {
		s.failed = map[uuid.UUID]string{}
	}
}

// sourceSyncDispatcherSpy 记录 TryDispatchConnection 调用。
type sourceSyncDispatcherSpy struct {
	calls []sourceSyncDispatchCall
	err   error
}

type sourceSyncDispatchCall struct {
	WorkspaceID  uuid.UUID
	ConnectionID uuid.UUID
}

func (d *sourceSyncDispatcherSpy) TryDispatchConnection(_ context.Context, workspaceID, connectionID uuid.UUID) error {
	d.calls = append(d.calls, sourceSyncDispatchCall{WorkspaceID: workspaceID, ConnectionID: connectionID})
	return d.err
}
