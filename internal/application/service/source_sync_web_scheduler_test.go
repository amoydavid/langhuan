package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// 本文件覆盖 Meta Scheduler 的 web 内容源调度（v1.3.0 spec §9.1 执行隔离）：
//   - 全局在飞上限（仅统计 web 源任务，飞书任务在场不占配额）；
//   - 分组限流（有 connection 组沿用 per-connection 限额；无连接源自成组额度 1）；
//   - 同源串行跳过（busy 源不占额度、不推进 next_sync_at）；
//   - worker 完成后的同 connection 续跑与 next_sync_at 按 cron 推进。

// fakeWebSchedulerStore 实现 WebSourceSchedulerStore（嵌入飞书 fake 复用限流口径）。
type fakeWebSchedulerStore struct {
	fakeSchedulerStore
	dueWebSources  []DueWebSource
	listDueErr     error
	activeBySource map[uuid.UUID]int
	countBySrcErr  error
	// globalActiveBase 是已存在的 web 源在飞基数；每次成功入队 +1（模拟 DB pending Job）。
	globalActiveBase int
	countGlobalErr   error
	// enq 指向调度器使用的入队 fake，使全局计数随入队增长（如真实 DB 计数）。
	enq *fakeSourceEnqueuer
	// globalCountCalls 记录全局计数调用次数（断言逐次入队前重读）。
	globalCountCalls int
	nextSyncAtCalls  []webNextSyncAtCall
}

type webNextSyncAtCall struct {
	WorkspaceID uuid.UUID
	KBID        uuid.UUID
	SourceID    uuid.UUID
	NextSyncAt  time.Time
}

func (s *fakeWebSchedulerStore) ListDueWebSources(_ context.Context, _ time.Time) ([]DueWebSource, error) {
	if s.listDueErr != nil {
		return nil, s.listDueErr
	}
	return append([]DueWebSource(nil), s.dueWebSources...), nil
}

func (s *fakeWebSchedulerStore) CountActiveBySource(_ context.Context, _, _, sourceID uuid.UUID) (int, error) {
	if s.countBySrcErr != nil {
		return 0, s.countBySrcErr
	}
	return s.activeBySource[sourceID], nil
}

func (s *fakeWebSchedulerStore) CountActiveWebSourceJobs(_ context.Context) (int, error) {
	if s.countGlobalErr != nil {
		return 0, s.countGlobalErr
	}
	s.globalCountCalls++
	count := s.globalActiveBase
	if s.enq != nil {
		count += len(s.enq.calls) - len(s.enq.failed)
	}
	return count, nil
}

func (s *fakeWebSchedulerStore) UpdateWebSourceNextSyncAt(_ context.Context, workspaceID, kbID, sourceID uuid.UUID, nextSyncAt time.Time) error {
	s.nextSyncAtCalls = append(s.nextSyncAtCalls, webNextSyncAtCall{
		WorkspaceID: workspaceID, KBID: kbID, SourceID: sourceID, NextSyncAt: nextSyncAt,
	})
	return nil
}

// fakeSourceEnqueuer 记录 EnqueueSyncForSource 调用，可按源注入错误。
type fakeSourceEnqueuer struct {
	calls   []enqueueSourceCall
	failed  []uuid.UUID
	err     error
	errOnID uuid.UUID
}

type enqueueSourceCall struct {
	WorkspaceID uuid.UUID
	KBID        uuid.UUID
	SourceID    uuid.UUID
	Force       bool
}

func (e *fakeSourceEnqueuer) EnqueueSyncForSource(_ context.Context, workspaceID, kbID, sourceID uuid.UUID, options SyncOptions) (*model.Job, error) {
	e.calls = append(e.calls, enqueueSourceCall{WorkspaceID: workspaceID, KBID: kbID, SourceID: sourceID, Force: options.Force})
	if (e.errOnID != uuid.Nil && e.errOnID == sourceID) || (e.errOnID == uuid.Nil && e.err != nil) {
		e.failed = append(e.failed, sourceID)
		return nil, e.err
	}
	return &model.Job{ID: uuid.New()}, nil
}

// newWebSchedulerHarness 构造带 web 调度依赖的 scheduler。
func newWebSchedulerHarness(maxPerConnection, maxWebSyncs int) (*SourceSyncScheduler, *fakeKBSyncRepo, *fakeWebSchedulerStore, *fakeSourceEnqueuer) {
	kbRepo := &fakeKBSyncRepo{items: map[uuid.UUID]*model.KnowledgeBase{}}
	enqueuer := &fakeSourceEnqueuer{}
	webStore := &fakeWebSchedulerStore{
		fakeSchedulerStore: fakeSchedulerStore{},
		activeBySource:     map[uuid.UUID]int{},
		enq:                enqueuer,
	}
	scheduler := NewSourceSyncScheduler(SourceSyncSchedulerDeps{
		KBRepo:                     kbRepo,
		Store:                      &webStore.fakeSchedulerStore,
		SyncService:                &fakeSyncEnqueuer{},
		WebStore:                   webStore,
		SourceService:              enqueuer,
		MaxConcurrentPerConnection: maxPerConnection,
		MaxConcurrentWebSyncs:      maxWebSyncs,
		Logger:                     &recordingLogger{},
	})
	return scheduler, kbRepo, webStore, enqueuer
}

// newDueWebSource 构造一个到期 web 源 fixture。
func newDueWebSource(workspaceID, kbID, connID uuid.UUID, cron string) DueWebSource {
	config := map[string]any{"mode": "urls", "urls": []any{}}
	if cron != "" {
		config["sync"] = map[string]any{"cron": cron}
	}
	if connID != uuid.Nil {
		config["fetch"] = map[string]any{"engine": "firecrawl", "connection_id": connID.String()}
	}
	return DueWebSource{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, ID: uuid.New(),
		ConnectionID: connID, Config: config,
	}
}

// TestSchedulerWebGlobalCapOnlyCountsWebJobs 验证全局在飞上限：
// 上限内才入队、达到上限停止；飞书任务在场（connection 计数高企）不占 web 配额。
func TestSchedulerWebGlobalCapOnlyCountsWebJobs(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	src1 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src2 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src3 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 2) // 全局上限 2
	store.dueWebSources = []DueWebSource{src1, src2, src3}
	// 飞书侧任务满负荷：不占用 web 配额。
	store.count = 8
	store.globalActiveBase = 1 // 已有 1 个 web 源在飞 → 本轮可再入队 1 个

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d（全局基数 1 + 上限 2）", got, want)
	}
	if enqueuer.calls[0].SourceID != src1.ID {
		t.Fatalf("enqueued source = %s, want %s（列表顺序）", enqueuer.calls[0].SourceID, src1.ID)
	}
	// 达到上限后停止：src2/src3 不入队，也不推进 next_sync_at（保持到期）。
	if got := len(store.nextSyncAtCalls); got != 1 {
		t.Fatalf("next_sync_at calls = %d, want 1（仅入队源推进）", got)
	}
	if store.nextSyncAtCalls[0].SourceID != src1.ID {
		t.Fatalf("advanced source = %s, want %s", store.nextSyncAtCalls[0].SourceID, src1.ID)
	}
}

// TestSchedulerWebGlobalCapReReadsPerEnqueue 验证入队前逐次重读全局计数，
// 基数为 0 时可连续入队至上限（每次入队后的 pending Job 计入下一次判断）。
func TestSchedulerWebGlobalCapReReadsPerEnqueue(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	src1 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src2 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 2)
	store.dueWebSources = []DueWebSource{src1, src2}

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 2; got != want {
		t.Fatalf("enqueued = %d, want %d（基数 0，逐次入队 2 个）", got, want)
	}
	if got, want := store.globalCountCalls, 2; got != want {
		// 每个源入队前各读一次（0 → 入队、1 → 入队），无第三源不再读。
		t.Fatalf("global count calls = %d, want %d", got, want)
	}
}

// TestSchedulerWebGlobalCapShortCircuitsRemainingGroups 验证全局触顶后直接结束
// 本轮：触顶组之后的剩余组不再逐组重读必然触顶的全局计数（触顶即停本轮，
// 剩余源不入队、不推进 next_sync_at，对外行为与逐组触顶返回一致）。
func TestSchedulerWebGlobalCapShortCircuitsRemainingGroups(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	// 三个无连接源各自成组：组 1 入队 1 个后全局触顶，组 2/3 不应再被扫描。
	src1 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src2 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src3 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 2)
	store.dueWebSources = []DueWebSource{src1, src2, src3}
	store.globalActiveBase = 1 // 已有 1 个 web 源在飞 → 入队 1 个后即触顶

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d（入队 1 个后触顶）", got, want)
	}
	// src1 入队 1 次；src2 所在组入队前重读触顶计数即结束，src3 所在组不再发起计数。
	if got, want := store.globalCountCalls, 2; got != want {
		t.Fatalf("global count calls = %d, want %d（触顶后短路，不再扫描剩余组）", got, want)
	}
	if len(store.nextSyncAtCalls) != 1 || store.nextSyncAtCalls[0].SourceID != src1.ID {
		t.Fatalf("next_sync_at calls = %+v, want 仅 src1", store.nextSyncAtCalls)
	}
}

// TestSchedulerWebConnectionGroupQuota 验证有 connection 的源按 connection 组
// 沿用 maxConcurrentPerConnection 限额（含既有飞书任务计入组额度）。
func TestSchedulerWebConnectionGroupQuota(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	connID := uuid.New()
	src1 := newDueWebSource(workspaceID, kbID, connID, "")
	src2 := newDueWebSource(workspaceID, kbID, connID, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 10)
	store.dueWebSources = []DueWebSource{src1, src2}
	store.countByConn = map[uuid.UUID]int{connID: 1} // 该 connection 已有 1 个进行中任务

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d（组额度 2-1）", got, want)
	}
}

// TestSchedulerWebNoConnectionSourcesEachOwnGroup 验证无连接源各自成组、额度 1：
// 即使 per-connection 限额为 1，多个无连接源也各自入队。
func TestSchedulerWebNoConnectionSourcesEachOwnGroup(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	src1 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src2 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(1, 10) // per-conn=1 不影响无连接组
	store.dueWebSources = []DueWebSource{src1, src2}

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 2; got != want {
		t.Fatalf("enqueued = %d, want %d（无连接源各自成组）", got, want)
	}
}

// TestSchedulerWebSkipsBusySourceWithoutQuota 验证同源串行：
// busy 源跳过——不占组额度、不推进 next_sync_at（留待任务完成后的续跑）。
func TestSchedulerWebSkipsBusySourceWithoutQuota(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	connID := uuid.New()
	busy := newDueWebSource(workspaceID, kbID, connID, "")
	idle := newDueWebSource(workspaceID, kbID, connID, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(1, 10) // 组额度 1
	store.dueWebSources = []DueWebSource{busy, idle}
	store.activeBySource[busy.ID] = 1 // busy 源已有 active 任务

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	// busy 源跳过且不占额度 → idle 仍可入队。
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d（busy 跳过，idle 入队）", got, want)
	}
	if enqueuer.calls[0].SourceID != idle.ID {
		t.Fatalf("enqueued source = %s, want %s", enqueuer.calls[0].SourceID, idle.ID)
	}
	for _, call := range store.nextSyncAtCalls {
		if call.SourceID == busy.ID {
			t.Fatal("busy 源不应推进 next_sync_at（保持到期，等续跑派发）")
		}
	}
}

// TestSchedulerWebTryDispatchConnectionContinuation 验证 worker 完成后的同 connection
// 续跑：仅派发该 connection 的到期 web 源。
func TestSchedulerWebTryDispatchConnectionContinuation(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	conn1, conn2 := uuid.New(), uuid.New()
	srcConn1 := newDueWebSource(workspaceID, kbID, conn1, "")
	srcConn2 := newDueWebSource(workspaceID, kbID, conn2, "")
	srcNoConn := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 10)
	store.dueWebSources = []DueWebSource{srcConn1, srcConn2, srcNoConn}

	if err := scheduler.TryDispatchConnection(context.Background(), workspaceID, conn1); err != nil {
		t.Fatalf("TryDispatchConnection err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d（仅 conn1）", got, want)
	}
	if enqueuer.calls[0].SourceID != srcConn1.ID {
		t.Fatalf("enqueued source = %s, want %s", enqueuer.calls[0].SourceID, srcConn1.ID)
	}
}

// TestSchedulerWebAdvancesNextSyncAtByCron 验证入队后按 sync.cron 推进 next_sync_at；
// 无 cron 的源清除 next_sync_at（仅手动触发）。
func TestSchedulerWebAdvancesNextSyncAtByCron(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	withCron := newDueWebSource(workspaceID, kbID, uuid.Nil, "*/5 * * * *")
	withoutCron := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, _ := newWebSchedulerHarness(2, 10)
	store.dueWebSources = []DueWebSource{withCron, withoutCron}

	before := time.Now().UTC()
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got := len(store.nextSyncAtCalls); got != 2 {
		t.Fatalf("next_sync_at calls = %d, want 2", got)
	}
	bySource := map[uuid.UUID]time.Time{}
	for _, call := range store.nextSyncAtCalls {
		bySource[call.SourceID] = call.NextSyncAt
	}
	cronNext, ok := bySource[withCron.ID]
	if !ok || cronNext.IsZero() || !cronNext.After(before) || cronNext.Sub(before) > 5*time.Minute+time.Second {
		t.Fatalf("cron 源 next_sync_at = %v, want now 后 5 分钟内", cronNext)
	}
	if got := bySource[withoutCron.ID]; !got.IsZero() {
		t.Fatalf("无 cron 源 next_sync_at = %v, want 零值（清除）", got)
	}
}

// TestSchedulerWebTickClearsNextSyncAtForInvalidCron 验证 web 源 sync.cron 非法时
// 清除 next_sync_at 避免死循环（与飞书 TestSchedulerTickClearsNextSyncAtForInvalidCron 对齐）。
func TestSchedulerWebTickClearsNextSyncAtForInvalidCron(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	invalidCron := newDueWebSource(workspaceID, kbID, uuid.Nil, "not-a-cron")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 10)
	store.dueWebSources = []DueWebSource{invalidCron}

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 1; got != want {
		t.Fatalf("enqueued = %d, want %d", got, want)
	}
	if got := len(store.nextSyncAtCalls); got != 1 {
		t.Fatalf("next_sync_at calls = %d, want 1", got)
	}
	if !store.nextSyncAtCalls[0].NextSyncAt.IsZero() {
		t.Fatalf("invalid cron should clear next_sync_at; got %v", store.nextSyncAtCalls[0].NextSyncAt)
	}
}

// TestSchedulerWebListErrorPropagates 验证 ListDueWebSources 错误向上冒泡。
func TestSchedulerWebListErrorPropagates(t *testing.T) {
	scheduler, _, store, _ := newWebSchedulerHarness(2, 10)
	store.listDueErr = errors.New("db down")
	if err := scheduler.Tick(context.Background()); err == nil {
		t.Fatal("Tick err = nil, want list error")
	}
}

// TestSchedulerWebDisabledWithoutDeps 验证未装配 web 依赖时跳过 web 调度（旧行为不变）。
func TestSchedulerWebDisabledWithoutDeps(t *testing.T) {
	scheduler, kbRepo, _, enqueuer := newSchedulerHarness(2)
	kbRepo.dueList = nil
	// 无 WebStore/SourceService：Tick 不因 web 依赖缺失而失败。
	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if len(enqueuer.calls) != 0 {
		t.Fatalf("feishu enqueued = %d, want 0", len(enqueuer.calls))
	}
}

// TestNewSourceSyncSchedulerWebCapDefault 验证全局在飞上限的构造器兜底默认（2）。
func TestNewSourceSyncSchedulerWebCapDefault(t *testing.T) {
	s := NewSourceSyncScheduler(SourceSyncSchedulerDeps{})
	if s.maxConcurrentWebSyncs != 2 {
		t.Fatalf("default maxConcurrentWebSyncs = %d, want 2", s.maxConcurrentWebSyncs)
	}
}

// TestSchedulerWebEnqueueErrorContinues 验证单个 web 源入队失败不中断同组后续源。
func TestSchedulerWebEnqueueErrorContinues(t *testing.T) {
	workspaceID := uuid.New()
	kbID := uuid.New()
	src1 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	src2 := newDueWebSource(workspaceID, kbID, uuid.Nil, "")
	scheduler, _, store, enqueuer := newWebSchedulerHarness(2, 10)
	store.dueWebSources = []DueWebSource{src1, src2}
	enqueuer.errOnID = src1.ID
	enqueuer.err = domainerrors.ErrValidation

	if err := scheduler.Tick(context.Background()); err != nil {
		t.Fatalf("Tick err = %v", err)
	}
	if got, want := len(enqueuer.calls), 2; got != want {
		t.Fatalf("enqueue attempts = %d, want %d（失败不中断）", got, want)
	}
	for _, call := range store.nextSyncAtCalls {
		if call.SourceID == src1.ID {
			t.Fatal("入队失败的源不应推进 next_sync_at")
		}
	}
}
