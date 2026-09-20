//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	queueport "github.com/dajee/langhuan/internal/ports/queue"
)

// 本文件覆盖 web 内容源调度（v1.3.0 spec §9.1）的 DB 层能力：
//   - ListDueWebSources：active + 到期筛选（disabled/未到期/无 next_sync_at/软删/非 web 排除），
//     config 与 fetch.connection_id 解析随行；
//   - CountActiveBySource / CountActiveWebSourceJobs：源级与全局在飞计数
//     （仅统计 kb_source_id 非空的 source_sync，pending/running）；
//   - RequestSourceSyncForSource：事务内 reuse-or-create + force latch 合并 + connection 标注；
//   - FailSourceSyncEnqueueForSource / UpdateWebSourceNextSyncAt：入队失败标记与到期推进。

// setWebSourceRuntime 更新源行的运行态列（status/next_sync_at/deleted_at 按需覆盖）。
func setWebSourceRuntime(t *testing.T, ctx context.Context, tx *gorm.DB, source *model.KbSource, mutate func(map[string]any)) {
	t.Helper()
	updates := map[string]any{}
	mutate(updates)
	if err := tx.WithContext(ctx).Model(&KbSourceRow{}).
		Where("workspace_id = ? AND id = ?", source.WorkspaceID, source.ID).
		Updates(updates).Error; err != nil {
		t.Fatalf("update kb_sources runtime: %v", err)
	}
}

// TestListDueWebSourcesFiltersDueActiveSources 覆盖到期 web 源筛选的全部排除分支。
func TestListDueWebSourcesFiltersDueActiveSources(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, tx)
	store := NewSourceSyncDBStore(tx)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	connID := uuid.New()
	dueSource := insertWebSource(t, ctx, tx, seed, "到期源", map[string]any{
		"mode": "crawl",
		"crawl": map[string]any{
			"seed_url":  "https://example.com/",
			"max_pages": float64(2000),
		},
		"fetch": map[string]any{"engine": "firecrawl", "connection_id": connID.String()},
		"sync":  map[string]any{"cron": "*/10 * * * *"},
	})
	setWebSourceRuntime(t, ctx, tx, dueSource, func(u map[string]any) {
		u["next_sync_at"] = past
	})
	// 停用源：到期但 status != active，不参与调度。
	disabledSource := insertWebSource(t, ctx, tx, seed, "停用源", map[string]any{"mode": "urls", "urls": []any{}})
	setWebSourceRuntime(t, ctx, tx, disabledSource, func(u map[string]any) {
		u["status"] = string(value.KbSourceStatusDisabled)
		u["next_sync_at"] = past
	})
	// 未到期源。
	notDueSource := insertWebSource(t, ctx, tx, seed, "未到期源", map[string]any{"mode": "urls", "urls": []any{}})
	setWebSourceRuntime(t, ctx, tx, notDueSource, func(u map[string]any) {
		u["next_sync_at"] = future
	})
	// 无 next_sync_at（无 cron 仅手动触发）。
	insertWebSource(t, ctx, tx, seed, "无调度源", map[string]any{"mode": "urls", "urls": []any{}})
	// 软删源。
	deletedSource := insertWebSource(t, ctx, tx, seed, "软删源", map[string]any{"mode": "urls", "urls": []any{}})
	setWebSourceRuntime(t, ctx, tx, deletedSource, func(u map[string]any) {
		u["next_sync_at"] = past
		u["deleted_at"] = now
	})

	due, err := store.ListDueWebSources(ctx, now)
	if err != nil {
		t.Fatalf("ListDueWebSources: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due = %d sources, want 1（仅到期 active web 源）", len(due))
	}
	got := due[0]
	if got.ID != dueSource.ID || got.WorkspaceID != seed.workspaceID || got.KnowledgeBaseID != seed.kbID {
		t.Fatalf("due source = %+v, want source %s", got, dueSource.ID)
	}
	if got.ConnectionID != connID {
		t.Fatalf("connection_id = %s, want %s（fetch.connection_id 已解析）", got.ConnectionID, connID)
	}
	if got.Config["mode"] != "crawl" {
		t.Fatalf("config = %v, want 随行原始源配置", got.Config)
	}
	crawl, ok := got.Config["crawl"].(map[string]any)
	if !ok || crawl["max_pages"] != float64(2000) {
		t.Fatalf("config.crawl = %v, want max_pages=2000", got.Config["crawl"])
	}

	// 软删源行即使到期也不可见（GetSyncKbSource 同口径）。
	if _, err := store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, deletedSource.ID); err == nil {
		t.Fatal("软删源应返回 ErrNotFound")
	}
}

// TestCountActiveBySourceAndGlobally 覆盖源级与全局在飞计数的口径：
// 仅 kb_source_id 非空的 source_sync Job（pending/running）计入；飞书 KB 级
// source_sync、源级 source_cleanup、终态 Job 均不计入；全局计数跨 workspace。
func TestCountActiveBySourceAndGlobally(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, tx)
	seed2 := insertKnowledgeSchemaSeed(t, ctx, tx)
	store := NewSourceSyncDBStore(tx)

	source := insertWebSource(t, ctx, tx, seed, "计数源", map[string]any{"mode": "urls", "urls": []any{}})
	otherSource := insertWebSource(t, ctx, tx, seed, "其它源", map[string]any{"mode": "urls", "urls": []any{}})

	// 目标源：pending + running → 计 2。
	insertWebSyncJob(t, ctx, tx, seed, source.ID, value.JobStatusPending)
	insertWebSyncJob(t, ctx, tx, seed, source.ID, value.JobStatusRunning)
	// 终态 Job 不计。
	insertWebSyncJob(t, ctx, tx, seed, source.ID, value.JobStatusCompleted)
	// 其它源不计入目标源。
	insertWebSyncJob(t, ctx, tx, seed, otherSource.ID, value.JobStatusPending)
	// KB 级（飞书）source_sync：kb_source_id 为空，两个口径都不计。
	kbJob, err := model.NewJob(model.NewJobInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		Type: model.SourceSyncJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("NewJob(KB级): %v", err)
	}
	if err := tx.WithContext(ctx).Create(jobV2ToRow(kbJob)).Error; err != nil {
		t.Fatalf("insert KB-level job: %v", err)
	}

	count, err := store.CountActiveBySource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatalf("CountActiveBySource: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountActiveBySource = %d, want 2", count)
	}
	if _, err := store.CountActiveBySource(ctx, seed.workspaceID, seed.kbID, uuid.Nil); err == nil {
		t.Fatal("空 lineage 应返回校验错误")
	}

	// 另一 workspace 的源级 source_sync 也计入全局。
	insertWebSyncJob(t, ctx, tx, seed2, func() uuid.UUID {
		src := insertWebSource(t, ctx, tx, seed2, "ws2 源", map[string]any{"mode": "urls", "urls": []any{}})
		return src.ID
	}(), value.JobStatusPending)

	global, err := store.CountActiveWebSourceJobs(ctx)
	if err != nil {
		t.Fatalf("CountActiveWebSourceJobs: %v", err)
	}
	if global != 4 { // 目标源 2 + 其它源 1 + ws2 1（KB 级与终态不计）
		t.Fatalf("CountActiveWebSourceJobs = %d, want 4", global)
	}
}

// TestRequestSourceSyncForSourceReuseOrCreate 覆盖源级请求同步的事务语义：
// 新建（created=true，行上标注 fetch.connection_id 与 latch 合并）、
// 进行中复用（created=false）与入队失败标记。
func TestRequestSourceSyncForSourceReuseOrCreate(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, tx)
	store := NewSourceSyncDBStore(tx)
	connID := uuid.New()
	source := insertWebSource(t, ctx, tx, seed, "请求源", map[string]any{
		"mode":  "urls",
		"urls":  []any{},
		"fetch": map[string]any{"engine": "jina", "connection_id": connID.String()},
	})

	// 首次请求：新建 + force=false 保持 latch 关闭。
	job, created, err := store.RequestSourceSyncForSource(ctx, seed.workspaceID, seed.kbID, source.ID, false)
	if err != nil {
		t.Fatalf("RequestSourceSyncForSource: %v", err)
	}
	if !created {
		t.Fatal("首次请求应创建新 Job")
	}
	if job.KbSourceID != source.ID {
		t.Fatalf("job.KbSourceID = %s, want %s", job.KbSourceID, source.ID)
	}
	if job.SourceConnectionID != connID {
		t.Fatalf("job.SourceConnectionID = %s, want %s（connection 标注）", job.SourceConnectionID, connID)
	}
	var row JobRow
	if err := tx.WithContext(ctx).First(&row, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID).Error; err != nil {
		t.Fatalf("load job row: %v", err)
	}
	if row.SourceConnectionID == nil || *row.SourceConnectionID != connID {
		t.Fatalf("job row source_connection_id = %v, want %s", row.SourceConnectionID, connID)
	}

	// 二次请求（无 force）：复用进行中 Job。
	job2, created2, err := store.RequestSourceSyncForSource(ctx, seed.workspaceID, seed.kbID, source.ID, false)
	if err != nil {
		t.Fatalf("reuse RequestSourceSyncForSource: %v", err)
	}
	if created2 || job2.ID != job.ID {
		t.Fatalf("进行中应复用: created=%v job2=%s job=%s", created2, job2.ID, job.ID)
	}

	// force 请求置位 latch（进行中 Job 仍复用）。
	if _, _, err := store.RequestSourceSyncForSource(ctx, seed.workspaceID, seed.kbID, source.ID, true); err != nil {
		t.Fatalf("force RequestSourceSyncForSource: %v", err)
	}
	consumed, err := store.ConsumeSourceForceLatch(ctx, seed.workspaceID, seed.kbID, source.ID, job.ID)
	if err != nil {
		t.Fatalf("ConsumeSourceForceLatch: %v", err)
	}
	if !consumed {
		t.Fatal("force 请求应置位源级 latch")
	}

	// 入队失败：pending Job 标记 failed 且保留 latch（重试派发可感知）。
	pendingJob := insertWebSyncJob(t, ctx, tx, seed, source.ID, value.JobStatusPending)
	if _, created, err := store.RequestSourceSyncForSource(ctx, seed.workspaceID, seed.kbID, source.ID, true); err != nil || created {
		// force 请求：pending Job 在场 → 复用 + 置位 latch。
		t.Fatalf("force 请求应复用: created=%v err=%v", created, err)
	}
	if err := store.FailSourceSyncEnqueueForSource(ctx, seed.workspaceID, seed.kbID, source.ID, pendingJob.ID, "queue down"); err != nil {
		t.Fatalf("FailSourceSyncEnqueueForSource: %v", err)
	}
	var failedRow JobRow
	if err := tx.WithContext(ctx).First(&failedRow, "workspace_id = ? AND id = ?", seed.workspaceID, pendingJob.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failedRow.Status != string(value.JobStatusFailed) || failedRow.ErrorClass != "enqueue_failed" {
		t.Fatalf("failed job = %s/%s, want failed/enqueue_failed", failedRow.Status, failedRow.ErrorClass)
	}
	consumed, err = store.ConsumeSourceForceLatch(ctx, seed.workspaceID, seed.kbID, source.ID, pendingJob.ID)
	if err != nil {
		t.Fatalf("ConsumeSourceForceLatch after fail: %v", err)
	}
	if !consumed {
		t.Fatal("入队失败应保留 force latch 供调度器重试")
	}

	// 对已终态 Job 标记入队失败：幂等拒绝（RowsAffected=0 → ErrNotFound）。
	if err := store.FailSourceSyncEnqueueForSource(ctx, seed.workspaceID, seed.kbID, source.ID, pendingJob.ID, "again"); err == nil {
		t.Fatal("对已终态 Job 标记入队失败应报错")
	}
}

// TestUpdateWebSourceNextSyncAt 覆盖 next_sync_at 推进与清除（零值置 NULL）。
func TestUpdateWebSourceNextSyncAt(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, tx)
	store := NewSourceSyncDBStore(tx)
	source := insertWebSource(t, ctx, tx, seed, "推进源", map[string]any{"mode": "urls", "urls": []any{}})

	next := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
	if err := store.UpdateWebSourceNextSyncAt(ctx, seed.workspaceID, seed.kbID, source.ID, next); err != nil {
		t.Fatalf("UpdateWebSourceNextSyncAt: %v", err)
	}
	got, err := store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatalf("GetSyncKbSource: %v", err)
	}
	if got.NextSyncAt == nil || !got.NextSyncAt.Equal(next) {
		t.Fatalf("next_sync_at = %v, want %v", got.NextSyncAt, next)
	}

	// 到期扫描此刻可见（next_sync_at <= now）。
	due, err := store.ListDueWebSources(ctx, next.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListDueWebSources: %v", err)
	}
	if len(due) != 1 || due[0].ID != source.ID {
		t.Fatalf("due = %+v, want 刚推进的源", due)
	}

	// 零值清除 → 不再到期。
	if err := store.UpdateWebSourceNextSyncAt(ctx, seed.workspaceID, seed.kbID, source.ID, time.Time{}); err != nil {
		t.Fatalf("clear UpdateWebSourceNextSyncAt: %v", err)
	}
	got, err = store.GetSyncKbSource(ctx, seed.workspaceID, seed.kbID, source.ID)
	if err != nil {
		t.Fatalf("GetSyncKbSource after clear: %v", err)
	}
	if got.NextSyncAt != nil {
		t.Fatalf("next_sync_at = %v, want NULL", got.NextSyncAt)
	}
	due, err = store.ListDueWebSources(ctx, next.Add(time.Hour))
	if err != nil {
		t.Fatalf("ListDueWebSources after clear: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("清除后不应到期: %+v", due)
	}

	// 不存在的源：ErrNotFound。
	err = store.UpdateWebSourceNextSyncAt(ctx, seed.workspaceID, seed.kbID, uuid.New(), next)
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("缺失源 err = %v, want ErrNotFound", err)
	}
}

// TestEnqueueSyncForSourceEndToEnd 从调度入口全链路验证：
// EnqueueSyncForSource → Job 落库 + asynq 请求（Timeout 按源推导、payload 带 source/connection）。
func TestEnqueueSyncForSourceEndToEnd(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, tx)
	source := insertWebSource(t, ctx, tx, seed, "入口源", map[string]any{
		"mode": "crawl",
		"crawl": map[string]any{
			"seed_url":         "https://example.com/",
			"max_pages":        float64(10000),
			"request_delay_ms": float64(200),
		},
		"fetch": map[string]any{"engine": "http"},
		"sync":  map[string]any{"cron": "*/5 * * * *"},
	})
	queue := &recordingQueue{}
	svc := appservice.NewSourceSyncService(appservice.SourceSyncServiceDeps{
		KnowledgeBaseRepository: NewKnowledgeBaseRepository(tx),
		Store:                   NewSourceSyncDBStore(tx),
		Queue:                   queue,
		WebRequestTimeout:       30 * time.Second,
		WebDefaultRequestDelay:  200 * time.Millisecond,
		WebSyncTaskTimeout:      4 * time.Hour,
	})

	job, err := svc.EnqueueSyncForSource(ctx, seed.workspaceID, seed.kbID, source.ID, appservice.SyncOptions{Force: false})
	if err != nil {
		t.Fatalf("EnqueueSyncForSource: %v", err)
	}
	if len(queue.requests) != 1 {
		t.Fatalf("enqueue requests = %d, want 1", len(queue.requests))
	}
	req := queue.requests[0]
	if req.TaskID != queueport.SourceSyncTaskIDForSource(seed.workspaceID, seed.kbID, source.ID) {
		t.Fatalf("task id = %q", req.TaskID)
	}
	// max_pages=10000：10000 × 30.2s × 2 ≈ 168h → 钳制到硬上限 4h。
	if req.Timeout != 4*time.Hour {
		t.Fatalf("timeout = %v, want 4h（硬上限钳制，高于全局 30min 默认）", req.Timeout)
	}
	var row JobRow
	if err := tx.WithContext(ctx).First(&row, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.KbSourceID == nil || *row.KbSourceID != source.ID {
		t.Fatalf("job row kb_source_id = %v, want %s", row.KbSourceID, source.ID)
	}
}
