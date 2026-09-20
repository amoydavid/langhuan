//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件验证 standalone（SQLite）模式下 web 内容源建源端到端可用（v1.3.0 plan T10）：
// SQLite 快照迁移（kb_sources 表 + provider CHECK 四值 + documents/jobs 源级列）之上，
// 源 CRUD service + 源级 source_sync Job 落库 + 队列入队全链路无方言缺口。

func TestSQLiteStandaloneWebSourceFlow(t *testing.T) {
	database := openSQLiteTestDB(t)
	ctx := context.Background()

	// 最小 KB 夹具（openSQLiteTestDB 已关 FK，规避 file_tree_root 循环外键）。
	wsID, kbID := uuid.New(), uuid.New()
	if err := database.Exec(
		"INSERT INTO workspaces (id, name, slug) VALUES (?, 'ws', ?)",
		wsID, "ws-"+wsID.String()[:8],
	).Error; err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := database.Exec(
		"INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES (?, ?, 'kb', ?)",
		kbID, wsID, uuid.New(),
	).Error; err != nil {
		t.Fatalf("insert knowledge_base: %v", err)
	}

	testQueue := &recordingQueue{}
	syncService := appservice.NewSourceSyncService(appservice.SourceSyncServiceDeps{
		KnowledgeBaseRepository: NewKnowledgeBaseRepository(database),
		Store:                   NewSourceSyncDBStore(database),
		Queue:                   testQueue,
	})
	service := appservice.NewKbSourceService(appservice.KbSourceServiceDeps{
		KBs:             NewKnowledgeBaseRepository(database),
		Store:           NewKbSourceRepository(database),
		SyncEnqueuer:    syncService,
		Limits:          value.WebSourceLimits{MaxPagesCeiling: 100, MaxDepthCeiling: 5},
		MaxSourcesPerKB: 10,
	})

	// 1. 建源（含首次同步入队）。
	source, job, err := service.CreateWebSource(ctx, appservice.CreateWebSourceInput{
		WorkspaceID: wsID, KnowledgeBaseID: kbID, Name: "standalone 文档站",
		Config: map[string]any{
			"mode": "urls",
			"urls": []any{"https://example.com/guide"},
			"crawl": map[string]any{
				"respect_robots": false,
			},
			"sync": map[string]any{"cron": "0 */6 * * *"},
		},
	})
	if err != nil {
		t.Fatalf("CreateWebSource: %v", err)
	}
	if job == nil {
		t.Fatal("首次同步 Job 为 nil")
	}

	// 源行经真实 store 可读（SQLite jsonb→TEXT 往返无方言缺口）。
	if _, err := NewSourceSyncDBStore(database).GetSyncKbSource(ctx, wsID, kbID, source.ID); err != nil {
		t.Fatalf("GetSyncKbSource: %v", err)
	}
	// 源级 source_sync Job 落库为第五分支 target（jobs_target_check 的 SQLite 等价 CHECK）。
	var jobStatus string
	var jobSourceID uuid.UUID
	if err := database.Raw(
		"SELECT status, kb_source_id FROM jobs WHERE id = ?", job.ID,
	).Row().Scan(&jobStatus, &jobSourceID); err != nil {
		t.Fatalf("query job: %v", err)
	}
	if jobStatus != string(value.JobStatusPending) || jobSourceID != source.ID {
		t.Fatalf("job = %s/%s, want pending/%s", jobStatus, jobSourceID, source.ID)
	}
	if got := testQueue.countByType("source_sync"); got != 1 {
		t.Fatalf("source_sync 入队数 = %d, want 1", got)
	}

	// 2. 手动重跑：同源单 active —— 复用进行中 Job，不重复入队。
	reused, err := service.SyncWebSource(ctx, wsID, kbID, source.ID, true)
	if err != nil {
		t.Fatalf("SyncWebSource: %v", err)
	}
	if reused.ID != job.ID {
		t.Fatalf("reuse job = %s, want %s", reused.ID, job.ID)
	}
	if got := testQueue.countByType("source_sync"); got != 1 {
		t.Fatalf("reuse 后入队数 = %d, want 仍为 1", got)
	}

	// 3. 名称唯一（SQLite 表达式唯一索引）。
	if _, _, err := service.CreateWebSource(ctx, appservice.CreateWebSourceInput{
		WorkspaceID: wsID, KnowledgeBaseID: kbID, Name: "standalone 文档站",
		Config: map[string]any{"mode": "urls", "urls": []any{"https://example.com/b"}},
	}); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("同名创建 error = %v, want ErrConflict", err)
	}

	// 4. 启停 + keep 删除分支。
	if err := service.SetWebSourceStatus(ctx, wsID, kbID, source.ID, value.KbSourceStatusDisabled); err != nil {
		t.Fatalf("SetWebSourceStatus: %v", err)
	}
	// 在飞守卫（评审 Minor-2）：首次同步 Job 仍 pending 时删除被拒绝；
	// 模拟 worker 终态化后重试成功。
	if err := service.DeleteWebSource(ctx, wsID, kbID, source.ID, "keep"); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("pending 任务存在时删除 error = %v, want ErrConflict", err)
	}
	if err := database.Exec(
		"UPDATE jobs SET status = ? WHERE id = ?", string(value.JobStatusCompleted), job.ID,
	).Error; err != nil {
		t.Fatalf("finalize job: %v", err)
	}
	if err := service.DeleteWebSource(ctx, wsID, kbID, source.ID, "keep"); err != nil {
		t.Fatalf("DeleteWebSource(keep): %v", err)
	}
	if _, err := NewKbSourceRepository(database).Get(ctx, wsID, kbID, source.ID); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("删除后 Get error = %v, want ErrNotFound", err)
	}
}
