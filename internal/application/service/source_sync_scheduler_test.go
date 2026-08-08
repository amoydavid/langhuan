package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// --- scheduler fakes ---------------------------------------------------

// fakeSchedulerStore 提供 CountActiveByConnection 的固定返回（限流模拟）。
type fakeSchedulerStore struct {
	countByConn map[uuid.UUID]int
	count       int
	err         error
	// latchedKBs 模拟 latch 恢复扫描返回的 KB（无 active Job）。
	latchedKBs []DueKnowledgeBase
	// latchErr 注入 ListFeishuKBsWithForceLatchAndNoActiveJob 错误。
	latchErr error
}

func (s *fakeSchedulerStore) CountActiveByConnection(_ context.Context, _ uuid.UUID, connectionID uuid.UUID) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.countByConn != nil {
		return s.countByConn[connectionID], nil
	}
	return s.count, nil
}

func (s *fakeSchedulerStore) ListFeishuKBsWithForceLatchAndNoActiveJob(_ context.Context) ([]DueKnowledgeBase, error) {
	if s.latchErr != nil {
		return nil, s.latchErr
	}
	return append([]DueKnowledgeBase(nil), s.latchedKBs...), nil
}

// fakeSyncEnqueuer 记录 EnqueueSync 调用（按 KB id），可注入错误。
type fakeSyncEnqueuer struct {
	calls   []enqueueSyncCall
	jobs    map[uuid.UUID]*model.Job
	err     error
	errOnKB uuid.UUID // 仅对该 KB 返回 err
}

type enqueueSyncCall struct {
	WorkspaceID uuid.UUID
	KBID        uuid.UUID
	Force       bool
}

func (e *fakeSyncEnqueuer) EnqueueSync(_ context.Context, workspaceID, kbID uuid.UUID, options SyncOptions) (*model.Job, error) {
	e.calls = append(e.calls, enqueueSyncCall{WorkspaceID: workspaceID, KBID: kbID, Force: options.Force})
	// errOnKB 命中时对该 KB 返回 err，其它 KB 不受影响（用于"单 KB 失败"场景）。
	if e.errOnKB != uuid.Nil && e.errOnKB == kbID {
		return nil, e.err
	}
	if e.errOnKB == uuid.Nil && e.err != nil {
		return nil, e.err
	}
	if e.jobs == nil {
		e.jobs = map[uuid.UUID]*model.Job{}
	}
	job := &model.Job{ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kbID}
	e.jobs[kbID] = job
	return job, nil
}

// newSchedulerTestKB 构造一个飞书 KB（带可选 cron）写入 fake repo。
func newSchedulerTestKB(workspaceID, connID uuid.UUID, cron string) *model.KnowledgeBase {
	sourceConfig := map[string]any{"root_token": "tok"}
	if cron != "" {
		sourceConfig["cron"] = cron
	}
	return &model.KnowledgeBase{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceType: value.SourceTypeFeishuWiki,
		SourceConfig: sourceConfig, SourceConnectionID: &connID,
	}
}

func newSchedulerHarness(maxConcurrent int) (*SourceSyncScheduler, *fakeKBSyncRepo, *fakeSchedulerStore, *fakeSyncEnqueuer) {
	kbRepo := &fakeKBSyncRepo{items: map[uuid.UUID]*model.KnowledgeBase{}}
	store := &fakeSchedulerStore{}
	enqueuer := &fakeSyncEnqueuer{}
	scheduler := NewSourceSyncScheduler(SourceSyncSchedulerDeps{
		KBRepo:                     kbRepo,
		Store:                      store,
		SyncService:                enqueuer,
		MaxConcurrentPerConnection: maxConcurrent,
		Logger:                     &recordingLogger{},
	})
	return scheduler, kbRepo, store, enqueuer
}

// --- tests -------------------------------------------------------------

// TestSchedulerTickEnqueuesDueKBsUpToConcurrencyCap 验证 Tick 把到期 KB 入队，
// 且入队数量受 maxConcurrentPerConnection - activeCount 限制。
func TestSchedulerTickEnqueuesDueKBsUpToConcurrencyCap(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb1 := newSchedulerTestKB(workspaceID, connID, "")
	kb2 := newSchedulerTestKB(workspaceID, connID, "")
	kb3 := newSchedulerTestKB(workspaceID, connID, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2) // cap=2
	kbRepo.items[kb1.ID] = kb1
	kbRepo.items[kb2.ID] = kb2
	kbRepo.items[kb3.ID] = kb3
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb1.ID, SourceConnectionID: connID},
		{WorkspaceID: workspaceID, ID: kb2.ID, SourceConnectionID: connID},
		{WorkspaceID: workspaceID, ID: kb3.ID, SourceConnectionID: connID},
	}
	store.count = 0 // 无进行中任务，可用额度 = 2

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 2; got != want {
		t.Fatalf("enqueued = %d, want %d (capped by concurrency)", got, want)
	}
	// 入队的 KB 应为前两个（列表顺序）。
	if enqueuer.calls[0].KBID != kb1.ID || enqueuer.calls[1].KBID != kb2.ID {
		t.Fatalf("enqueued KBs = %+v, want %s then %s", enqueuer.calls, kb1.ID, kb2.ID)
	}
	// kb3 未入队（额度耗尽）。
	for _, call := range enqueuer.calls {
		if call.KBID == kb3.ID {
			t.Fatalf("kb3 should not be enqueued when cap is reached")
		}
	}
	// 无 cron 时应清除 next_sync_at（推进调用次数 == 入队数）。
	if got, want := len(kbRepo.nextSyncAtCalls), 2; got != want {
		t.Fatalf("next_sync_at calls = %d, want %d", got, want)
	}
	for _, call := range kbRepo.nextSyncAtCalls {
		if !call.NextSyncAt.IsZero() {
			t.Fatalf("next_sync_at should be zero (cleared) for no-cron KB; got %v", call.NextSyncAt)
		}
	}
}

// TestSchedulerTickHonorsActiveCountForThrottling 验证已存在进行中任务时，额度相应减少。
func TestSchedulerTickHonorsActiveCountForThrottling(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb1 := newSchedulerTestKB(workspaceID, connID, "")
	kb2 := newSchedulerTestKB(workspaceID, connID, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2)
	kbRepo.items[kb1.ID] = kb1
	kbRepo.items[kb2.ID] = kb2
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb1.ID, SourceConnectionID: connID},
		{WorkspaceID: workspaceID, ID: kb2.ID, SourceConnectionID: connID},
	}
	store.count = 1 // 已有 1 个进行中，可用额度 = 2-1 = 1

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d (active=1 reduces available to 1)", got, want)
	}
}

// TestSchedulerTickEnqueuesNothingWhenCapExhausted 验证额度满时全部跳过。
func TestSchedulerTickEnqueuesNothingWhenCapExhausted(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb1 := newSchedulerTestKB(workspaceID, connID, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2)
	kbRepo.items[kb1.ID] = kb1
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb1.ID, SourceConnectionID: connID},
	}
	store.count = 2 // 已达上限

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 0; got != want {
		t.Fatalf("enqueued = %d, want %d (cap exhausted)", got, want)
	}
}

// TestSchedulerTickAdvancesNextSyncAtViaCron 验证有 cron 的 KB 入队后按 cron.Next 推进 next_sync_at。
func TestSchedulerTickAdvancesNextSyncAtViaCron(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb := newSchedulerTestKB(workspaceID, connID, "*/5 * * * *") // 每 5 分钟
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2)
	kbRepo.items[kb.ID] = kb
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb.ID, SourceConnectionID: connID},
	}
	store.count = 0

	before := time.Now().UTC()
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	if len(kbRepo.nextSyncAtCalls) != 1 {
		t.Fatalf("next_sync_at calls = %d, want 1", len(kbRepo.nextSyncAtCalls))
	}
	next := kbRepo.nextSyncAtCalls[0].NextSyncAt
	if next.IsZero() {
		t.Fatal("next_sync_at should be advanced, not cleared, for cron KB")
	}
	// cron 下一次应在 now 之后、且不超过 5 分钟。
	if !next.After(before) {
		t.Fatalf("next_sync_at = %v, should be after %v", next, before)
	}
	if diff := next.Sub(before); diff <= 0 || diff > 5*time.Minute+time.Second {
		t.Fatalf("next_sync_at diff = %v, should be within 5 min", diff)
	}
}

// TestSchedulerTickClearsNextSyncAtForInvalidCron 验证无效 cron 时清除 next_sync_at 避免死循环。
func TestSchedulerTickClearsNextSyncAtForInvalidCron(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb := newSchedulerTestKB(workspaceID, connID, "not-a-cron")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2)
	kbRepo.items[kb.ID] = kb
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb.ID, SourceConnectionID: connID},
	}
	store.count = 0

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueuer.calls))
	}
	if len(kbRepo.nextSyncAtCalls) != 1 {
		t.Fatalf("next_sync_at calls = %d, want 1", len(kbRepo.nextSyncAtCalls))
	}
	if !kbRepo.nextSyncAtCalls[0].NextSyncAt.IsZero() {
		t.Fatalf("invalid cron should clear next_sync_at; got %v", kbRepo.nextSyncAtCalls[0].NextSyncAt)
	}
}

// TestSchedulerTickGroupsByWorkspaceAndConnection 验证不同 (workspace, connection) 组独立限流。
func TestSchedulerTickGroupsByWorkspaceAndConnection(t *testing.T) {
	ws1, ws2 := uuid.New(), uuid.New()
	conn1, conn2 := uuid.New(), uuid.New()
	kbA := newSchedulerTestKB(ws1, conn1, "")
	kbB := newSchedulerTestKB(ws1, conn2, "")
	kbC := newSchedulerTestKB(ws2, conn1, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(1) // 每组 cap=1
	kbRepo.items[kbA.ID] = kbA
	kbRepo.items[kbB.ID] = kbB
	kbRepo.items[kbC.ID] = kbC
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: ws1, ID: kbA.ID, SourceConnectionID: conn1},
		{WorkspaceID: ws1, ID: kbB.ID, SourceConnectionID: conn2},
		{WorkspaceID: ws2, ID: kbC.ID, SourceConnectionID: conn1},
	}
	// 按 connection 分别计数：每组 active=0，可用=1。
	store.countByConn = map[uuid.UUID]int{conn1: 0, conn2: 0}

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	// 三组各入队 1 个（每组额度独立）。
	if got, want := len(enqueuer.calls), 3; got != want {
		t.Fatalf("enqueued = %d, want %d (independent caps per group)", got, want)
	}
}

// TestSchedulerTickDoesNothingWhenNoDueKBs 验证无到期 KB 时为空操作。
func TestSchedulerTickDoesNothingWhenNoDueKBs(t *testing.T) {
	scheduler, _, store, enqueuer := newSchedulerHarness(2)
	store.count = 0
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(enqueuer.calls))
	}
}

// TestSchedulerTickPropagatesListDueError 验证 ListDueFeishuKBs 错误向上冒泡。
func TestSchedulerTickPropagatesListDueError(t *testing.T) {
	listErr := errors.New("db down")
	scheduler, kbRepo, _, enqueuer := newSchedulerHarness(2)
	kbRepo.dueErr = listErr
	if err := scheduler.Tick(context.Background()); err == nil {
		t.Fatal("Tick err = nil, want list error")
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("no enqueue expected on list error")
	}
}

// TestSchedulerTryDispatchConnectionFiltersByConnection 验证 TryDispatchConnection 仅入队该 connection 的到期 KB。
func TestSchedulerTryDispatchConnectionFiltersByConnection(t *testing.T) {
	workspaceID := uuid.New()
	conn1, conn2 := uuid.New(), uuid.New()
	kbA := newSchedulerTestKB(workspaceID, conn1, "")
	kbB := newSchedulerTestKB(workspaceID, conn2, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(5)
	kbRepo.items[kbA.ID] = kbA
	kbRepo.items[kbB.ID] = kbB
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kbA.ID, SourceConnectionID: conn1},
		{WorkspaceID: workspaceID, ID: kbB.ID, SourceConnectionID: conn2},
	}
	store.count = 0

	if err := scheduler.TryDispatchConnection(context.Background(), workspaceID, conn1); err != nil {
		t.Fatalf("TryDispatchConnection err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d (only conn1)", got, want)
	}
	if enqueuer.calls[0].KBID != kbA.ID {
		t.Fatalf("enqueued KB = %s, want %s", enqueuer.calls[0].KBID, kbA.ID)
	}
}

// TestSchedulerTryDispatchConnectionIgnoresNilConnection 验证 nil connection 为空操作。
func TestSchedulerTryDispatchConnectionIgnoresNilConnection(t *testing.T) {
	scheduler, _, _, enqueuer := newSchedulerHarness(2)
	if err := scheduler.TryDispatchConnection(context.Background(), uuid.New(), uuid.Nil); err != nil {
		t.Fatalf("TryDispatchConnection err = %v", err)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("enqueued = %d, want 0 for nil connection", len(enqueuer.calls))
	}
}

// TestSchedulerTickContinuesOnEnqueueError 验证单个 KB 入队失败不中断后续入队。
func TestSchedulerTickContinuesOnEnqueueError(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb1 := newSchedulerTestKB(workspaceID, connID, "")
	kb2 := newSchedulerTestKB(workspaceID, connID, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(3)
	kbRepo.items[kb1.ID] = kb1
	kbRepo.items[kb2.ID] = kb2
	kbRepo.dueList = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb1.ID, SourceConnectionID: connID},
		{WorkspaceID: workspaceID, ID: kb2.ID, SourceConnectionID: connID},
	}
	store.count = 0
	enqueuer.errOnKB = kb1.ID
	enqueuer.err = domainerrors.ErrValidation // 仅 errOnKB 命中时返回

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if len(enqueuer.calls) != 2 {
		t.Fatalf("enqueue attempts = %d, want 2 (both attempted)", len(enqueuer.calls))
	}
	// kb1 入队失败，不应推进其 next_sync_at；kb2 成功应推进。
	if len(kbRepo.nextSyncAtCalls) != 1 || kbRepo.nextSyncAtCalls[0].KBID != kb2.ID {
		t.Fatalf("next_sync_at calls = %+v, want only kb2", kbRepo.nextSyncAtCalls)
	}
}

// TestSchedulerTickRecoversLatchedKBsWithNoActiveJob 验证 spec 8.2 的 latch 恢复：
// Tick 在常规到期扫描后，额外把 force latch=true 且无 active Job 的 KB 重新派发
// （EnqueueSync 以 Force=false；RequestSourceSync 保留 latch 并因无 active Job 而新建）。
func TestSchedulerTickRecoversLatchedKBsWithNoActiveJob(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	latchedKB := newSchedulerTestKB(workspaceID, connID, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2)
	kbRepo.items[latchedKB.ID] = latchedKB
	// 无到期 KB（dueList 空），但有 1 个 latch 滞留 KB。
	kbRepo.dueList = nil
	store.count = 0
	store.latchedKBs = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: latchedKB.ID, SourceConnectionID: connID},
	}

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got := len(enqueuer.calls); got != 1 {
		t.Fatalf("enqueued = %d, want 1 (latch recovery)", got)
	}
	if enqueuer.calls[0].KBID != latchedKB.ID {
		t.Fatalf("recovered KB = %s, want %s", enqueuer.calls[0].KBID, latchedKB.ID)
	}
	// latch 恢复派发固定传 Force=false。
	if enqueuer.calls[0].Force {
		t.Fatalf("latch recovery 应以 Force=false 派发; call = %#v", enqueuer.calls[0])
	}
}

// TestSchedulerTickLatchRecoveryContinuesOnError 验证单个 latch KB 恢复失败不中断其它。
func TestSchedulerTickLatchRecoveryContinuesOnError(t *testing.T) {
	workspaceID := uuid.New()
	connID := uuid.New()
	kb1 := newSchedulerTestKB(workspaceID, connID, "")
	kb2 := newSchedulerTestKB(workspaceID, connID, "")
	scheduler, kbRepo, store, enqueuer := newSchedulerHarness(2)
	kbRepo.items[kb1.ID] = kb1
	kbRepo.items[kb2.ID] = kb2
	kbRepo.dueList = nil
	store.count = 0
	store.latchedKBs = []DueKnowledgeBase{
		{WorkspaceID: workspaceID, ID: kb1.ID, SourceConnectionID: connID},
		{WorkspaceID: workspaceID, ID: kb2.ID, SourceConnectionID: connID},
	}
	enqueuer.errOnKB = kb1.ID
	enqueuer.err = domainerrors.ErrValidation

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got := len(enqueuer.calls); got != 2 {
		t.Fatalf("enqueue attempts = %d, want 2 (both attempted)", got)
	}
}

// TestSchedulerTickLatchRecoveryListErrorPropagates 验证 latch 列表查询错误向上冒泡。
func TestSchedulerTickLatchRecoveryListErrorPropagates(t *testing.T) {
	scheduler, kbRepo, store, _ := newSchedulerHarness(2)
	kbRepo.dueList = nil
	store.latchErr = errors.New("db down")

	if err := scheduler.Tick(context.Background()); err == nil {
		t.Fatal("Tick err = nil, want latch list error")
	}
}

// TestNewSourceSyncSchedulerAppliesDefaults 验证构造器兜底默认值。
func TestNewSourceSyncSchedulerAppliesDefaults(t *testing.T) {
	s := NewSourceSyncScheduler(SourceSyncSchedulerDeps{})
	if s.maxConcurrentPerConnection != 2 {
		t.Fatalf("default maxConcurrent = %d, want 2", s.maxConcurrentPerConnection)
	}
	if s.interval != 60*time.Second {
		t.Fatalf("default interval = %v, want 60s", s.interval)
	}
	if s.logger == nil {
		t.Fatal("default logger should be noop, not nil")
	}
}

// TestSchedulerTickRejectsMissingDeps 验证依赖缺失时返回校验错误。
func TestSchedulerTickRejectsMissingDeps(t *testing.T) {
	s := &SourceSyncScheduler{} // 全部依赖 nil
	if err := s.Tick(context.Background()); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}
