//go:build integration

package db

import (
	"testing"

	"github.com/google/uuid"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// TestSourceCleanupStoreDeleteSourceDocumentBatchesJobs 验证 remove 策略下，
// 当清理对象超过 SourceCleanupObjectBatchSize 时按批次拆分创建多个 KB-only cleanup Job。
// 这里用少量 key（1 个 revision 产生 1 个 raw key）验证单 Job 路径；
// 批次切分逻辑由 createSourceCleanupJobs 的稳定排序保证（单元覆盖见 source_cleanup_test.go 的常量断言）。
func TestSourceCleanupStoreDeleteSourceDocumentBatchesJobs(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "待删文档batch", "doccnCleanupBatch")
	store := NewSourceSyncDBStore(database)

	objects, jobs, err := store.DeleteSourceDocument(ctx, seed.workspaceID, document.ID, value.SourceDeleteRemove)
	if err != nil {
		t.Fatalf("DeleteSourceDocument: %v", err)
	}
	if len(objects) == 0 {
		t.Fatal("应收集清理对象")
	}
	if len(jobs) != 1 {
		t.Fatalf("单个 revision 应创建 1 个 cleanup Job（batch<=100），got %d", len(jobs))
	}
	// 每个 Job 应为 KB-only（document_id 为 nil）。
	for _, job := range jobs {
		if job.DocumentID != uuid.Nil {
			t.Fatalf("cleanup Job 不应带 document_id，got %s", job.DocumentID)
		}
		if job.Type != model.SourceCleanupJobType {
			t.Fatalf("job type = %q, want %q", job.Type, model.SourceCleanupJobType)
		}
		if job.Status != value.JobStatusPending {
			t.Fatalf("job status = %q, want pending", job.Status)
		}
	}
}

// TestSourceCleanupStoreGetAndMarkRoundtrip 验证 SourceCleanupDBStore 的
// GetSourceCleanupJob（payload 解析）+ MarkSucceeded/Failed + ListPending 全流程。
func TestSourceCleanupStoreGetAndMarkRoundtrip(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "待删文档roundtrip", "doccnCleanupRT")
	syncStore := NewSourceSyncDBStore(database)
	cleanupStore := NewSourceCleanupStore(database)

	objects, jobs, err := syncStore.DeleteSourceDocument(ctx, seed.workspaceID, document.ID, value.SourceDeleteRemove)
	if err != nil {
		t.Fatalf("DeleteSourceDocument: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("want 1 cleanup job, got %d", len(jobs))
	}
	jobID := jobs[0].ID

	// ListPending 应能扫到该 Job。
	pending, err := cleanupStore.ListPendingSourceCleanupJobs(ctx)
	if err != nil {
		t.Fatalf("ListPendingSourceCleanupJobs: %v", err)
	}
	foundPending := false
	for _, p := range pending {
		if p.JobID == jobID {
			foundPending = true
			if p.WorkspaceID != seed.workspaceID || p.KnowledgeBaseID != seed.kbID {
				t.Fatalf("pending lineage = %+v, want ws=%s kb=%s", p, seed.workspaceID, seed.kbID)
			}
		}
	}
	if !foundPending {
		t.Fatal("ListPendingSourceCleanupJobs 未返回刚创建的 cleanup Job")
	}

	// GetSourceCleanupJob 应返回 Job + payload 解析出的对象。
	gotJob, gotObjects, err := cleanupStore.GetSourceCleanupJob(ctx, seed.workspaceID, jobID)
	if err != nil {
		t.Fatalf("GetSourceCleanupJob: %v", err)
	}
	if gotJob == nil || gotJob.ID != jobID {
		t.Fatalf("got job = %+v, want id %s", gotJob, jobID)
	}
	if len(gotObjects) != len(objects) {
		t.Fatalf("payload objects = %d, want %d", len(gotObjects), len(objects))
	}

	// MarkSucceeded 后 ListPending 不应再包含该 Job。
	if err := cleanupStore.MarkSourceCleanupJobSucceeded(ctx, seed.workspaceID, jobID); err != nil {
		t.Fatalf("MarkSourceCleanupJobSucceeded: %v", err)
	}
	pending2, err := cleanupStore.ListPendingSourceCleanupJobs(ctx)
	if err != nil {
		t.Fatalf("ListPendingSourceCleanupJobs after succeed: %v", err)
	}
	for _, p := range pending2 {
		if p.JobID == jobID {
			t.Fatal("succeeded job 不应再出现在 pending 列表")
		}
	}
}

// TestSourceCleanupStoreMarkFailedSetsStatus 验证 MarkSourceCleanupJobFailed 把状态写为 failed。
func TestSourceCleanupStoreMarkFailedSetsStatus(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "待删文档fail", "doccnCleanupFail")
	syncStore := NewSourceSyncDBStore(database)
	cleanupStore := NewSourceCleanupStore(database)

	_, jobs, err := syncStore.DeleteSourceDocument(ctx, seed.workspaceID, document.ID, value.SourceDeleteRemove)
	if err != nil {
		t.Fatalf("DeleteSourceDocument: %v", err)
	}
	jobID := jobs[0].ID

	if err := cleanupStore.MarkSourceCleanupJobFailed(ctx, seed.workspaceID, jobID, "s3 timeout"); err != nil {
		t.Fatalf("MarkSourceCleanupJobFailed: %v", err)
	}
	// 读回验证 status=failed + error_message。
	var row JobRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, jobID).First(&row).Error; err != nil {
		t.Fatalf("read back job: %v", err)
	}
	if row.Status != string(value.JobStatusFailed) {
		t.Fatalf("status = %q, want failed", row.Status)
	}
	if row.ErrorMessage != "s3 timeout" {
		t.Fatalf("error_message = %q, want %q", row.ErrorMessage, "s3 timeout")
	}
}

// TestSourceCleanupStoreGetNotFoundReturnsError 验证不存在的 cleanup Job 返回 ErrNotFound。
func TestSourceCleanupStoreGetNotFoundReturnsError(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	cleanupStore := NewSourceCleanupStore(database)

	if _, _, err := cleanupStore.GetSourceCleanupJob(ctx, uuid.New(), uuid.New()); err == nil {
		t.Fatal("GetSourceCleanupJob 不存在的 job 应返回错误")
	}
}

// TestSourceCleanupStoreRejectsCrossWorkspaceGet 验证 cleanup Job 读取受 Workspace 作用域限制
// （用错误的 workspace_id 读取应失败/返回 not found）。
func TestSourceCleanupStoreRejectsCrossWorkspaceGet(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "待删文档xws", "doccnCleanupXWS")
	syncStore := NewSourceSyncDBStore(database)
	cleanupStore := NewSourceCleanupStore(database)

	_, jobs, err := syncStore.DeleteSourceDocument(ctx, seed.workspaceID, document.ID, value.SourceDeleteRemove)
	if err != nil {
		t.Fatalf("DeleteSourceDocument: %v", err)
	}
	jobID := jobs[0].ID

	// 用其它 workspace 读取应失败。
	otherWS := uuid.New()
	if _, _, err := cleanupStore.GetSourceCleanupJob(ctx, otherWS, jobID); err == nil {
		t.Fatal("跨 workspace 读取 cleanup Job 应失败（RLS/workspace 过滤）")
	}
}

// 确保 appservice 包被引用（避免 unused import 警告，DB 包的 cleanup store 实现其接口）。
var _ appservice.SourceCleanupStore = (*SourceCleanupDBStore)(nil)
