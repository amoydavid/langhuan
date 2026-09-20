package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
)

// 本文件覆盖 web 源调度入口（v1.3.0 spec §9.1）的应用层单元行为：
//   - 任务超时按源推导（三模式页数基准 / 硬上限钳制 / 高于全局 30min 默认）；
//   - EnqueueSyncForSource 的幂等复用、入队失败标记与校验分支。

// newWebDispatchService 构造被测 service：30s 单请求 + 200ms 间隔 + 4h 硬上限。
func newWebDispatchService(store *fakeSourceSyncStore, q *fakeSyncQueue) *SourceSyncService {
	return NewSourceSyncService(SourceSyncServiceDeps{
		Store:                  store,
		Queue:                  q,
		WebRequestTimeout:      30 * time.Second,
		WebDefaultRequestDelay: 200 * time.Millisecond,
		WebSyncTaskTimeout:     4 * time.Hour,
	})
}

func newWebDispatchSource(kind value.KbSourceKind, config map[string]any) *model.KbSource {
	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		Kind: kind, Name: "测试源", Config: config,
	})
	if err != nil {
		panic(err)
	}
	return source
}

// TestWebSyncTaskTimeoutForDerivesByMode 表驱动验证超时推导：
// pages × (request_timeout + request_delay) × 2，再与硬上限取 min。
func TestWebSyncTaskTimeoutForDerivesByMode(t *testing.T) {
	perPage := 30*time.Second + 200*time.Millisecond // 30.2s
	tests := []struct {
		name   string
		config map[string]any
		want   time.Duration
	}{
		{
			// crawl：页数基准 = crawl.max_pages；100 × 30.2s × 2 = 6040s。
			name:   "crawl_max_pages",
			config: map[string]any{"mode": "crawl", "crawl": map[string]any{"max_pages": float64(100)}},
			want:   100 * perPage * 2,
		},
		{
			// crawl 缺 max_pages：按缺省 500 基准（与 value 层缺省一致）→ 钳制 4h。
			name:   "crawl_missing_max_pages_clamped",
			config: map[string]any{"mode": "crawl", "crawl": map[string]any{"seed_url": "https://example.com/"}},
			want:   4 * time.Hour,
		},
		{
			// crawl max_pages=10000（天花板量级）→ 钳制 4h。
			name:   "crawl_max_pages_clamped",
			config: map[string]any{"mode": "crawl", "crawl": map[string]any{"max_pages": float64(10000)}},
			want:   4 * time.Hour,
		},
		{
			// crawl max_pages 非数值：按缺省基准 → 钳制 4h。
			name:   "crawl_invalid_max_pages",
			config: map[string]any{"mode": "crawl", "crawl": map[string]any{"max_pages": "big"}},
			want:   4 * time.Hour,
		},
		{
			// urls：页数基准 = len(urls)；3 × 30.2s × 2 = 181.2s。
			name:   "urls_len",
			config: map[string]any{"mode": "urls", "urls": []any{"https://a.com/", "https://b.com/", "https://c.com/"}},
			want:   3 * perPage * 2,
		},
		{
			// urls 空：页数下限 1，避免推导出必然不够用的超时。
			name:   "urls_empty_floors_at_one_page",
			config: map[string]any{"mode": "urls", "urls": []any{}},
			want:   perPage * 2,
		},
		{
			// sitemap：发现前不可知，具名常量 500 基准 → 钳制 4h。
			name:   "sitemap_constant_clamped",
			config: map[string]any{"mode": "sitemap", "sitemap": map[string]any{"url": "https://example.com/sitemap.xml"}},
			want:   4 * time.Hour,
		},
		{
			// 未知 mode：兜底 sitemap 基准。
			name:   "unknown_mode_falls_back",
			config: map[string]any{"mode": "mystery"},
			want:   4 * time.Hour,
		},
		{
			// 空 config：兜底 sitemap 基准。
			name:   "empty_config_falls_back",
			config: map[string]any{},
			want:   4 * time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newWebDispatchService(&fakeSourceSyncStore{}, &fakeSyncQueue{})
			source := newWebDispatchSource(value.KbSourceKindWeb, tt.config)
			if got := svc.webSyncTaskTimeoutFor(source); got != tt.want {
				t.Fatalf("timeout = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWebSyncTaskTimeoutForExceedsGlobalDefault 验证推导值可以高于队列全局默认
// （30 分钟）：中等规模 max_pages 源不被全局默认过早掐断（spec §9.1）。
func TestWebSyncTaskTimeoutForExceedsGlobalDefault(t *testing.T) {
	svc := newWebDispatchService(&fakeSourceSyncStore{}, &fakeSyncQueue{})
	source := newWebDispatchSource(value.KbSourceKindWeb, map[string]any{
		"mode":  "crawl",
		"crawl": map[string]any{"max_pages": float64(100)},
	})
	got := svc.webSyncTaskTimeoutFor(source)
	if got <= 30*time.Minute {
		t.Fatalf("timeout = %v, want > 全局默认 30min", got)
	}
	if got >= svc.webSyncTaskTimeout {
		t.Fatalf("timeout = %v, want < 硬上限 %v", got, svc.webSyncTaskTimeout)
	}
}

// TestNewSourceSyncServiceWebTimeoutDefaults 验证零值依赖兜底默认
// （30s / 200ms / 4h），旧装配路径行为不变。
func TestNewSourceSyncServiceWebTimeoutDefaults(t *testing.T) {
	svc := NewSourceSyncService(SourceSyncServiceDeps{})
	source := newWebDispatchSource(value.KbSourceKindWeb, map[string]any{"mode": "urls", "urls": []any{"https://a.com/"}})
	if got := svc.webSyncTaskTimeoutFor(source); got != (30*time.Second+200*time.Millisecond)*2 {
		t.Fatalf("timeout = %v, want 60.4s（默认单请求 30s + 默认间隔 200ms）", got)
	}
}

// TestEnqueueSyncForSourceEnqueuesDerivedTimeout 验证调度入口全链路：
// 新建 Job + 入队（TaskID 幂等、payload 带 source/connection、Timeout 按源推导）。
func TestEnqueueSyncForSourceEnqueuesDerivedTimeout(t *testing.T) {
	store := newFakeSourceSyncStore(nil)
	q := &fakeSyncQueue{}
	svc := newWebDispatchService(store, q)
	source := newWebDispatchSource(value.KbSourceKindWeb, map[string]any{
		"mode":  "urls",
		"urls":  []any{"https://a.com/", "https://b.com/"},
		"fetch": map[string]any{"engine": "firecrawl", "connection_id": uuid.New().String()},
	})
	store.kbSources[source.ID] = source

	job, err := svc.EnqueueSyncForSource(context.Background(), source.WorkspaceID, source.KnowledgeBaseID, source.ID, SyncOptions{Force: false})
	if err != nil {
		t.Fatalf("EnqueueSyncForSource: %v", err)
	}
	if job == nil || job.KbSourceID != source.ID {
		t.Fatalf("job = %+v, want 源级目标", job)
	}
	if len(q.requests) != 1 {
		t.Fatalf("enqueue requests = %d, want 1", len(q.requests))
	}
	req := q.requests[0]
	if want := queue.SourceSyncTaskIDForSource(source.WorkspaceID, source.KnowledgeBaseID, source.ID); req.TaskID != want {
		t.Fatalf("task id = %q, want %q", req.TaskID, want)
	}
	if want := (2 * (30*time.Second + 200*time.Millisecond) * 2); req.Timeout != want {
		t.Fatalf("timeout = %v, want %v（urls=2 页基准）", req.Timeout, want)
	}
	var payload sourceSyncTaskPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.SourceID != source.ID || payload.KnowledgeBaseID != source.KnowledgeBaseID {
		t.Fatalf("payload = %+v, want 源级 lineage", payload)
	}
	if payload.ConnectionID == uuid.Nil {
		t.Fatal("payload 应携带 fetch.connection_id（worker 续跑用）")
	}
}

// TestEnqueueSyncForSourceReusesActiveJob 验证同源串行：已有 active Job 时复用、不重复入队。
func TestEnqueueSyncForSourceReusesActiveJob(t *testing.T) {
	store := newFakeSourceSyncStore(nil)
	q := &fakeSyncQueue{}
	svc := newWebDispatchService(store, q)
	source := newWebDispatchSource(value.KbSourceKindWeb, map[string]any{"mode": "urls", "urls": []any{}})
	store.kbSources[source.ID] = source
	active := &model.Job{ID: uuid.New(), WorkspaceID: source.WorkspaceID,
		KnowledgeBaseID: source.KnowledgeBaseID, KbSourceID: source.ID,
		Type: model.SourceSyncJobType, Status: value.JobStatusRunning}
	store.sourceActiveSyncJob[source.ID] = active

	job, err := svc.EnqueueSyncForSource(context.Background(), source.WorkspaceID, source.KnowledgeBaseID, source.ID, SyncOptions{Force: false})
	if err != nil {
		t.Fatalf("EnqueueSyncForSource: %v", err)
	}
	if job.ID != active.ID {
		t.Fatalf("job = %s, want 复用 active %s", job.ID, active.ID)
	}
	if len(q.requests) != 0 {
		t.Fatalf("复用路径不应重复入队, got %d requests", len(q.requests))
	}
}

// TestEnqueueSyncForSourceMarksFailedOnEnqueueError 验证入队失败：Job 标记失败 +
// 保留 latch，错误向上返回（源保持到期，下一 Tick 重试派发）。
func TestEnqueueSyncForSourceMarksFailedOnEnqueueError(t *testing.T) {
	store := newFakeSourceSyncStore(nil)
	q := &fakeSyncQueue{err: errors.New("queue down")}
	svc := newWebDispatchService(store, q)
	source := newWebDispatchSource(value.KbSourceKindWeb, map[string]any{"mode": "urls", "urls": []any{}})
	store.kbSources[source.ID] = source
	store.sourceForceLatch[source.ID] = true // 预置 latch，断言不被清除

	if _, err := svc.EnqueueSyncForSource(context.Background(), source.WorkspaceID, source.KnowledgeBaseID, source.ID, SyncOptions{Force: false}); err == nil {
		t.Fatal("EnqueueSyncForSource err = nil, want enqueue error")
	}
	job, ok := func() (*model.Job, bool) {
		for _, j := range store.jobs {
			if j.KbSourceID == source.ID {
				return j, true
			}
		}
		return nil, false
	}()
	if !ok {
		t.Fatal("Job 应已落库")
	}
	if job.Status != value.JobStatusFailed {
		t.Fatalf("job status = %s, want failed", job.Status)
	}
	if !store.sourceForceLatch[source.ID] {
		t.Fatal("入队失败应保留 force latch")
	}
}

// TestEnqueueSyncForSourceValidations 覆盖校验分支：空 lineage / 非依赖 / 非 web 种类 / 源缺失。
func TestEnqueueSyncForSourceValidations(t *testing.T) {
	store := newFakeSourceSyncStore(nil)
	svc := newWebDispatchService(store, &fakeSyncQueue{})
	ctx := context.Background()

	if _, err := svc.EnqueueSyncForSource(ctx, uuid.Nil, uuid.New(), uuid.New(), SyncOptions{}); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("空 lineage err = %v, want ErrValidation", err)
	}
	bare := NewSourceSyncService(SourceSyncServiceDeps{})
	if _, err := bare.EnqueueSyncForSource(ctx, uuid.New(), uuid.New(), uuid.New(), SyncOptions{}); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("依赖缺失 err = %v, want ErrValidation", err)
	}

	nonWeb := newWebDispatchSource(value.KbSourceKindWeb, nil)
	nonWeb.Kind = "mystery"
	store.kbSources[nonWeb.ID] = nonWeb
	if _, err := svc.EnqueueSyncForSource(ctx, nonWeb.WorkspaceID, nonWeb.KnowledgeBaseID, nonWeb.ID, SyncOptions{}); !errors.Is(err, domainerrors.ErrValidation) || !strings.Contains(err.Error(), "不支持 web 调度") {
		t.Fatalf("非 web 源 err = %v, want ErrValidation(不支持 web 调度)", err)
	}

	if _, err := svc.EnqueueSyncForSource(ctx, uuid.New(), uuid.New(), uuid.New(), SyncOptions{}); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("缺失源 err = %v, want ErrNotFound", err)
	}
}
