//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// sourceSyncSeed 复用 knowledgeSchemaSeed，并额外建一条飞书来源连接，
// 返回 connectionID 供 source_sync 任务的 source_connection_id 使用。
type sourceSyncSeed struct {
	knowledgeSchemaSeed
	connectionID uuid.UUID
}

// insertSourceSyncSeed 建立最小可用 fixture：workspace/user/provider/model/kb/root/generation，
// 再插入一条 workspace_source_connections（provider=feishu）。
// 复用 insertKnowledgeSchemaSeed 的事务 fixture 模式（migrate_v018 的列合同）。
func insertSourceSyncSeed(t *testing.T, ctx context.Context, tx *gorm.DB) sourceSyncSeed {
	t.Helper()
	base := insertKnowledgeSchemaSeed(t, ctx, tx)
	connectionID := uuid.New()
	now := time.Now().UTC()
	if err := tx.WithContext(ctx).Exec(
		"INSERT INTO workspace_source_connections "+
			"(id, workspace_id, provider, name, config, credentials_ciphertext, status, created_at, updated_at) "+
			"VALUES (?, ?, 'feishu', ?, ?::jsonb, ?, 'active', ?, ?)",
		connectionID, base.workspaceID, "主公司飞书",
		`{"app_id":"cli_a1b2"}`, []byte("encrypted-secret"),
		now, now,
	).Error; err != nil {
		t.Fatalf("seed workspace_source_connections: %v", err)
	}
	return sourceSyncSeed{knowledgeSchemaSeed: base, connectionID: connectionID}
}

// newSourceSyncJob 构造一个合法的 source_sync Job（仅关联 KB，三者皆 nil）。
func newSourceSyncJob(t *testing.T, workspaceID, kbID, connectionID uuid.UUID, status value.JobStatus) *model.Job {
	t.Helper()
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID:        workspaceID,
		KnowledgeBaseID:    kbID,
		SourceConnectionID: connectionID,
		Type:               model.SourceSyncJobType,
		Status:             status,
	})
	if err != nil {
		t.Fatalf("new source_sync job: %v", err)
	}
	return job
}

// TestSourceSyncStoreCreateSourceSyncJob 验证 source_sync Job（仅 KB）可落库。
func TestSourceSyncStoreCreateSourceSyncJob(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	job := newSourceSyncJob(t, seed.workspaceID, seed.kbID, seed.connectionID, value.JobStatusPending)
	if err := store.CreateSourceSyncJob(ctx, job); err != nil {
		t.Fatalf("CreateSourceSyncJob: %v", err)
	}

	var row JobRow
	if err := database.WithContext(ctx).
		First(&row, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID).Error; err != nil {
		t.Fatalf("read back job: %v", err)
	}
	if row.Type != model.SourceSyncJobType {
		t.Fatalf("job type = %q, want %q", row.Type, model.SourceSyncJobType)
	}
	if row.KnowledgeBaseID != seed.kbID {
		t.Fatalf("job kb = %s, want %s", row.KnowledgeBaseID, seed.kbID)
	}
	if row.DocumentID != nil || row.DocumentRevisionID != nil || row.IndexGenerationID != nil {
		t.Fatalf("source_sync job should have nil document/revision/generation targets, got %#v", row)
	}
	if row.SourceConnectionID == nil || *row.SourceConnectionID != seed.connectionID {
		t.Fatalf("job source_connection_id = %#v, want %s", row.SourceConnectionID, seed.connectionID)
	}
	if row.Status != string(value.JobStatusPending) {
		t.Fatalf("job status = %q, want %q", row.Status, value.JobStatusPending)
	}
}

// TestSourceSyncStoreCreateSourceSyncJobRejectsNonSourceSync 验证非 source_sync 类型
// 在 store 层被拒绝（即便领域层已放宽，store 仍做类型守卫）。
func TestSourceSyncStoreCreateSourceSyncJobRejectsNonSourceSync(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID:        seed.workspaceID,
		KnowledgeBaseID:    seed.kbID,
		DocumentID:         uuid.New(),
		DocumentRevisionID: uuid.New(),
		SourceConnectionID: seed.connectionID,
		Type:               "document_parse_start",
		Status:             value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("new parse job: %v", err)
	}
	if err := store.CreateSourceSyncJob(ctx, job); err == nil {
		t.Fatal("CreateSourceSyncJob accepted non-source_sync job; want validation error")
	}
}

// TestSourceSyncStoreCountActiveByConnection 验证 CountActiveByConnection 只统计
// pending/running 的 source_sync 任务（completed 不计），这是 Meta Scheduler 限流的核心查询。
func TestSourceSyncStoreCountActiveByConnection(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// seed 三个任务：pending + running + completed，均绑定同一 connection。
	jobs := []*model.Job{
		newSourceSyncJob(t, seed.workspaceID, seed.kbID, seed.connectionID, value.JobStatusPending),
		newSourceSyncJob(t, seed.workspaceID, seed.kbID, seed.connectionID, value.JobStatusRunning),
		newSourceSyncJob(t, seed.workspaceID, seed.kbID, seed.connectionID, value.JobStatusCompleted),
	}
	for _, j := range jobs {
		if err := store.CreateSourceSyncJob(ctx, j); err != nil {
			t.Fatalf("seed job %s: %v", j.Status, err)
		}
	}
	// completed 的任务 CreateSourceSyncJob 写入时仍是 pending，这里直接改库状态模拟已完成。
	if err := database.WithContext(ctx).Model(&JobRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, jobs[2].ID).
		Update("status", string(value.JobStatusCompleted)).Error; err != nil {
		t.Fatalf("mark job completed: %v", err)
	}

	count, err := store.CountActiveByConnection(ctx, seed.workspaceID, seed.connectionID)
	if err != nil {
		t.Fatalf("CountActiveByConnection: %v", err)
	}
	if count != 2 {
		t.Fatalf("active count = %d, want 2 (pending + running, completed excluded)", count)
	}

	// 跨 connection 隔离：另一个 connection 应返回 0。
	otherConnection := uuid.New()
	now := time.Now().UTC()
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO workspace_source_connections "+
			"(id, workspace_id, provider, name, config, status, created_at, updated_at) "+
			"VALUES (?, ?, 'feishu', ?, '{}'::jsonb, 'active', ?, ?)",
		otherConnection, seed.workspaceID, "other-app", now, now,
	).Error; err != nil {
		t.Fatalf("seed other connection: %v", err)
	}
	otherCount, err := store.CountActiveByConnection(ctx, seed.workspaceID, otherConnection)
	if err != nil {
		t.Fatalf("CountActiveByConnection other: %v", err)
	}
	if otherCount != 0 {
		t.Fatalf("other connection active count = %d, want 0", otherCount)
	}
}

// TestSourceSyncStoreCountActiveByConnectionValidation 验证空 UUID 被拒绝。
func TestSourceSyncStoreCountActiveByConnectionValidation(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	store := NewSourceSyncDBStore(database)

	if _, err := store.CountActiveByConnection(ctx, uuid.Nil, uuid.New()); err == nil {
		t.Fatal("CountActiveByConnection accepted nil workspace; want validation error")
	}
	if _, err := store.CountActiveByConnection(ctx, uuid.New(), uuid.Nil); err == nil {
		t.Fatal("CountActiveByConnection accepted nil connection; want validation error")
	}
}

// newSyncedFileAggregate 构造一份飞书同步落库所需的最小聚合：
// Document(file, external_id) + Revision(reason=crawl, markdown) + FileTreeNode(file) + Job(parse_start)。
func newSyncedFileAggregate(
	t *testing.T, workspaceID, kbID, rootID uuid.UUID, title, externalID string,
) (*model.Document, *model.FileTreeNode, *model.DocumentRevision, *model.Job) {
	t.Helper()
	document, err := model.NewDocumentIdentityWithExternal(
		workspaceID, kbID, value.DocumentKindFile, title, model.SourceProviderFeishu, "", externalID, nil,
	)
	if err != nil {
		t.Fatalf("new synced document: %v", err)
	}
	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonCrawl,
		OriginalFilename: title + ".md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: "raw/" + externalID + ".md", SHA256: "abc123",
		SizeBytes: 7, ProcessingVersion: model.CurrentProcessingVersion,
		Status: value.DocumentRevisionPending,
	})
	if err != nil {
		t.Fatalf("new synced revision: %v", err)
	}
	documentID := document.ID
	node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, ParentID: &rootID,
		NodeType: value.FileTreeNodeFile, Name: title,
		DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
	})
	if err != nil {
		t.Fatalf("new synced file node: %v", err)
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if err != nil {
		t.Fatalf("new synced parse job: %v", err)
	}
	return document, node, revision, job
}

// TestSourceSyncTxCreateSyncedDocumentNodeRevisionAndJob 验证单事务原子写入
// document + filenode + revision + job 四条记录，且都能按 lineage 读回。
func TestSourceSyncTxCreateSyncedDocumentNodeRevisionAndJob(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	document, node, revision, job := newSyncedFileAggregate(
		t, seed.workspaceID, seed.kbID, seed.rootID, "飞书文档一", "doccnXYZ123",
	)
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			// 先校验 KB 存在（生产代码也会做），再写四条记录。
			if _, err := tx.GetKnowledgeBase(txCtx, seed.kbID); err != nil {
				return err
			}
			return tx.CreateSyncedDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatalf("CreateSyncedDocumentNodeRevisionAndJob: %v", err)
	}

	// 断言 document 落库且 external_id 正确。
	var docRow DocumentRow
	if err := database.WithContext(ctx).
		First(&docRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatalf("read back document: %v", err)
	}
	if docRow.ExternalID == nil || *docRow.ExternalID != "doccnXYZ123" {
		t.Fatalf("document external_id = %#v, want %q", docRow.ExternalID, "doccnXYZ123")
	}
	if docRow.SourceType != model.SourceProviderFeishu {
		t.Fatalf("document source_type = %q, want %q", docRow.SourceType, model.SourceProviderFeishu)
	}
	if docRow.Kind != string(value.DocumentKindFile) {
		t.Fatalf("document kind = %q, want %q", docRow.Kind, value.DocumentKindFile)
	}

	// 断言 file node 落库。
	var nodeRow FileTreeNodeRow
	if err := database.WithContext(ctx).
		First(&nodeRow, "workspace_id = ? AND id = ?", seed.workspaceID, node.ID).Error; err != nil {
		t.Fatalf("read back file node: %v", err)
	}
	if nodeRow.DocumentID == nil || *nodeRow.DocumentID != document.ID {
		t.Fatalf("file node document_id = %#v, want %s", nodeRow.DocumentID, document.ID)
	}

	// 断言 revision 落库且 reason=crawl。
	var revRow DocumentRevisionRow
	if err := database.WithContext(ctx).
		First(&revRow, "workspace_id = ? AND id = ?", seed.workspaceID, revision.ID).Error; err != nil {
		t.Fatalf("read back revision: %v", err)
	}
	if revRow.RevisionReason != string(value.DocumentRevisionReasonCrawl) {
		t.Fatalf("revision reason = %q, want %q", revRow.RevisionReason, value.DocumentRevisionReasonCrawl)
	}
	if revRow.FileType == nil || *revRow.FileType != "markdown" {
		t.Fatalf("revision file_type = %#v, want markdown", revRow.FileType)
	}

	// 断言 parse job 落库。
	var jobRow JobRow
	if err := database.WithContext(ctx).
		First(&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, job.ID).Error; err != nil {
		t.Fatalf("read back job: %v", err)
	}
	if jobRow.Type != "document_parse_start" {
		t.Fatalf("job type = %q, want document_parse_start", jobRow.Type)
	}
}

// TestSourceSyncTxSoftDeleteDocument 验证软删后 deleted_at 非空且 status=deleted。
func TestSourceSyncTxSoftDeleteDocument(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	document, node, revision, job := newSyncedFileAggregate(
		t, seed.workspaceID, seed.kbID, seed.rootID, "待删飞书文档", "doccnDeleteMe",
	)
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.CreateSyncedDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatalf("seed synced document: %v", err)
	}

	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.SoftDeleteDocument(txCtx, document.ID)
		},
	); err != nil {
		t.Fatalf("SoftDeleteDocument: %v", err)
	}

	var docRow DocumentRow
	if err := database.WithContext(ctx).
		First(&docRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatalf("read back document: %v", err)
	}
	if docRow.DeletedAt == nil {
		t.Fatal("deleted_at is nil after SoftDeleteDocument; want non-null")
	}
	if docRow.Status != string(value.DocumentStatusDeleted) {
		t.Fatalf("document status = %q, want %q", docRow.Status, value.DocumentStatusDeleted)
	}

	// 幂等：再次软删不应报错且不改变 deleted_at。
	firstDeletedAt := *docRow.DeletedAt
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.SoftDeleteDocument(txCtx, document.ID)
		},
	); err != nil {
		t.Fatalf("idempotent SoftDeleteDocument: %v", err)
	}
	if err := database.WithContext(ctx).
		First(&docRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatalf("re-read document: %v", err)
	}
	if docRow.DeletedAt == nil || !docRow.DeletedAt.Equal(firstDeletedAt) {
		t.Fatalf("deleted_at changed on idempotent soft delete; first=%s second=%v", firstDeletedAt, docRow.DeletedAt)
	}
}

// TestSourceSyncTxListAndDeleteFileTreeNode 验证 ListFileTreeNodes 返回该 KB 的节点，
// 且 DeleteFileTreeNode 能按 external_id 清理单个 folder（来源同步删除闸门用）。
func TestSourceSyncTxListAndDeleteFileTreeNode(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// 插入一个带 external_id 的 folder 节点（父 = root）。
	rootID := seed.rootID
	folder, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
		ParentID: &rootID, NodeType: value.FileTreeNodeFolder,
		Name: "同步目录", ExternalID: "folderExt1",
	})
	if err != nil {
		t.Fatalf("new folder node: %v", err)
	}
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.CreateFileTreeNode(txCtx, folder)
		},
	); err != nil {
		t.Fatalf("CreateFileTreeNode: %v", err)
	}

	// ListFileTreeNodes 应包含 root + folder。
	var listed []*model.FileTreeNode
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			var err error
			listed, err = tx.ListFileTreeNodes(txCtx, seed.kbID)
			return err
		},
	); err != nil {
		t.Fatalf("ListFileTreeNodes: %v", err)
	}
	foundFolder := false
	for _, n := range listed {
		if n.ID == folder.ID {
			foundFolder = true
		}
	}
	if !foundFolder {
		t.Fatalf("ListFileTreeNodes 未返回插入的 folder; got %d nodes", len(listed))
	}

	// DeleteFileTreeNode 应删除该 folder。
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.DeleteFileTreeNode(txCtx, folder.ID)
		},
	); err != nil {
		t.Fatalf("DeleteFileTreeNode: %v", err)
	}

	var count int64
	if err := database.WithContext(ctx).Model(&FileTreeNodeRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, folder.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count folder after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("DeleteFileTreeNode 后 folder 仍存在; count=%d", count)
	}
}

// TestSourceSyncStoreUpdateSyncCursor 验证 source_config.sync_cursor 正确更新。
func TestSourceSyncStoreUpdateSyncCursor(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// 先给 KB 设置 source_type=feishu_wiki + source_config（验证 jsonb_set 在已有 jsonb 上工作）。
	if err := database.WithContext(ctx).Exec(
		"UPDATE knowledge_bases SET source_type = 'feishu_wiki', "+
			"source_config = '{\"root_token\":\"wikcnRoot\",\"sync_cursor\":\"\"}'::jsonb "+
			"WHERE workspace_id = ? AND id = ?",
		seed.workspaceID, seed.kbID,
	).Error; err != nil {
		t.Fatalf("set kb source_config: %v", err)
	}

	cursor := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	if err := store.UpdateSyncCursor(ctx, seed.workspaceID, seed.kbID, cursor); err != nil {
		t.Fatalf("UpdateSyncCursor: %v", err)
	}

	var stored string
	if err := database.WithContext(ctx).
		Raw("SELECT source_config->>'sync_cursor' FROM knowledge_bases WHERE workspace_id = ? AND id = ?",
			seed.workspaceID, seed.kbID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read back sync_cursor: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, stored)
	if err != nil {
		t.Fatalf("sync_cursor %q not RFC3339: %v", stored, err)
	}
	// jsonb_set 写回的是 timestamptz，读出为 RFC3339；允许微秒级差异。
	if delta := parsed.Sub(cursor); delta > time.Second || delta < -time.Second {
		t.Fatalf("sync_cursor = %s, want ~%s (delta %s)", stored, cursor.Format(time.RFC3339), delta)
	}

	// 其它字段应保留（jsonb_set 只改 sync_cursor）。
	var rootToken string
	if err := database.WithContext(ctx).
		Raw("SELECT source_config->>'root_token' FROM knowledge_bases WHERE workspace_id = ? AND id = ?",
			seed.workspaceID, seed.kbID).
		Scan(&rootToken).Error; err != nil {
		t.Fatalf("read back root_token: %v", err)
	}
	if rootToken != "wikcnRoot" {
		t.Fatalf("root_token = %q, want wikcnRoot (jsonb_set should preserve other keys)", rootToken)
	}
}

// TestSourceSyncStoreUpdateSyncCursorValidation 验证空 lineage / 零值 cursor 被拒绝。
func TestSourceSyncStoreUpdateSyncCursorValidation(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	store := NewSourceSyncDBStore(database)
	cursor := time.Now().UTC()

	if err := store.UpdateSyncCursor(ctx, uuid.Nil, uuid.New(), cursor); err == nil {
		t.Fatal("UpdateSyncCursor accepted nil workspace; want validation error")
	}
	if err := store.UpdateSyncCursor(ctx, uuid.New(), uuid.Nil, cursor); err == nil {
		t.Fatal("UpdateSyncCursor accepted nil kb; want validation error")
	}
	if err := store.UpdateSyncCursor(ctx, uuid.New(), uuid.New(), time.Time{}); err == nil {
		t.Fatal("UpdateSyncCursor accepted zero cursor; want validation error")
	}
}

// TestSourceSyncStoreUpdateSyncCursorNotFound 验证更新不存在的 KB 返回 ErrNotFound。
func TestSourceSyncStoreUpdateSyncCursorNotFound(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	missingKB := uuid.New()
	err := store.UpdateSyncCursor(ctx, seed.workspaceID, missingKB, time.Now().UTC())
	if err == nil {
		t.Fatal("UpdateSyncCursor on missing KB returned nil; want ErrNotFound")
	}
}

// seedSyncedDocument 落库一份飞书同步文档（external_id + reason=crawl revision），
// 返回 (document, revision, node, job)，供更新/重试/删除测试复用。
func seedSyncedDocument(
	t *testing.T, ctx context.Context, database *gorm.DB, seed sourceSyncSeed, title, externalID string,
) (*model.Document, *model.DocumentRevision, *model.FileTreeNode, *model.Job) {
	t.Helper()
	document, node, revision, job := newSyncedFileAggregate(t, seed.workspaceID, seed.kbID, seed.rootID, title, externalID)
	store := NewSourceSyncDBStore(database)
	if err := store.WithinWorkspace(
		ctx, seed.workspaceID,
		func(txCtx context.Context, tx appservice.SourceSyncTx) error {
			return tx.CreateSyncedDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
		},
	); err != nil {
		t.Fatalf("seed synced document: %v", err)
	}
	return document, revision, node, job
}

// ---- Task 6: 稳定 Document 更新路径 ----

// TestSourceSyncStoreReusesDocumentAndIncrementsRevision 验证 CreateSyncedDocumentRevisionJob
// 复用既有 Document（同一 DocumentID），revision_no=max+1，更新 content_hash/status/title。
func TestSourceSyncStoreReusesDocumentAndIncrementsRevision(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "飞书文档一", "doccnUpdate1")
	store := NewSourceSyncDBStore(database)

	newRevisionID := uuid.New()
	result, err := store.CreateSyncedDocumentRevisionJob(ctx, appservice.UpdateDocumentRequest{
		WorkspaceID:     seed.workspaceID,
		KnowledgeBaseID: seed.kbID,
		ExternalID:      "doccnUpdate1",
		DocumentID:      document.ID,
		RevisionID:      newRevisionID,
		Title:           "飞书文档一（已更新）",
		ParentNodeID:    seed.rootID,
		RawStorageKey:   "raw/doccnUpdate1-v2.md",
		SHA256:          "newhash-v2",
		SizeBytes:       11,
		ContentType:     "text/markdown",
		FileType:        "markdown",
		Reason:          value.DocumentRevisionReasonCrawl,
	})
	if err != nil {
		t.Fatalf("CreateSyncedDocumentRevisionJob: %v", err)
	}
	if result.DocumentID != document.ID {
		t.Fatalf("result DocumentID = %s, want 复用 %s", result.DocumentID, document.ID)
	}
	if result.RevisionID != newRevisionID {
		t.Fatalf("result RevisionID = %s, want %s", result.RevisionID, newRevisionID)
	}
	if result.RevisionNo != 2 {
		t.Fatalf("result RevisionNo = %d, want 2", result.RevisionNo)
	}

	// 断言 Document content_hash 更新、status=pending、未软删，active_revision_id 未切换。
	var docRow DocumentRow
	if err := database.WithContext(ctx).
		First(&docRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatalf("read back document: %v", err)
	}
	if docRow.ContentHash == nil || *docRow.ContentHash != "newhash-v2" {
		t.Fatalf("document content_hash = %#v, want newhash-v2", docRow.ContentHash)
	}
	if docRow.Status != string(value.DocumentStatusPending) {
		t.Fatalf("document status = %q, want pending", docRow.Status)
	}
	if docRow.DeletedAt != nil {
		t.Fatalf("document deleted_at should be nil after update, got %v", docRow.DeletedAt)
	}
	if docRow.ActiveRevisionID != nil {
		t.Fatalf("active_revision_id should NOT switch in source tx, got %v", docRow.ActiveRevisionID)
	}

	// 断言新 revision 落库 revision_no=2。
	var revRow DocumentRevisionRow
	if err := database.WithContext(ctx).
		First(&revRow, "workspace_id = ? AND id = ?", seed.workspaceID, newRevisionID).Error; err != nil {
		t.Fatalf("read back new revision: %v", err)
	}
	if revRow.RevisionNo != 2 {
		t.Fatalf("revision_no = %d, want 2", revRow.RevisionNo)
	}
	if revRow.RevisionReason != string(value.DocumentRevisionReasonCrawl) {
		t.Fatalf("revision reason = %q, want crawl", revRow.RevisionReason)
	}

	// 断言 parse Job 落库。
	var jobRow JobRow
	if err := database.WithContext(ctx).
		First(&jobRow, "workspace_id = ? AND id = ?", seed.workspaceID, result.JobID).Error; err != nil {
		t.Fatalf("read back parse job: %v", err)
	}
	if jobRow.Type != "document_parse_start" {
		t.Fatalf("job type = %q, want document_parse_start", jobRow.Type)
	}
}

// TestSourceSyncStoreCreateRevisionRejectsMismatchedExternalID 验证 external_id 对应 Document
// 与请求 DocumentID 不一致时返回 Conflict（绝不静默复用错误文档）。
func TestSourceSyncStoreCreateRevisionRejectsMismatchedExternalID(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "飞书冲突文档", "doccnConflict")
	store := NewSourceSyncDBStore(database)

	// 用错误的 DocumentID + 正确 external_id 调用，应拒绝。
	_, err := store.CreateSyncedDocumentRevisionJob(ctx, appservice.UpdateDocumentRequest{
		WorkspaceID:     seed.workspaceID,
		KnowledgeBaseID: seed.kbID,
		ExternalID:      "doccnConflict",
		DocumentID:      uuid.New(), // 与 document.ID 不同
		RevisionID:      uuid.New(),
		Title:           "飞书冲突文档",
		ParentNodeID:    seed.rootID,
		RawStorageKey:   "raw/x.md",
		SHA256:          "hash",
		SizeBytes:       1,
		FileType:        "markdown",
		Reason:          value.DocumentRevisionReasonCrawl,
	})
	if err == nil {
		t.Fatal("CreateSyncedDocumentRevisionJob accepted mismatched DocumentID; want Conflict")
	}
	_ = document
}

// ---- force latch（spec 8.2）----

// TestConsumeAndFinalizeForceLatchIsAtomic 验证 latch 的请求/消费/完成闭环：
// RequestSourceSync(force=true) 新建 Job；ConsumeForceLatch 消费 latch；
// 再次 RequestSourceSync 复用进行中 Job；FinalizeSourceSyncJob 在 latch 被重新置位后返回下一个 Job。
func TestConsumeAndFinalizeForceLatchIsAtomic(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	current, created, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, seed.connectionID, true)
	if err != nil {
		t.Fatalf("RequestSourceSync: %v", err)
	}
	if !created {
		t.Fatal("首次 RequestSourceSync 应新建 Job，got created=false")
	}

	// 消费 latch => true。
	force, err := store.ConsumeForceLatch(ctx, seed.workspaceID, seed.kbID, current.ID)
	if err != nil {
		t.Fatalf("ConsumeForceLatch: %v", err)
	}
	if !force {
		t.Fatal("ConsumeForceLatch 返回 false，want true（latch 已置位）")
	}

	// 再次消费 => false（已被清空）。
	force2, err := store.ConsumeForceLatch(ctx, seed.workspaceID, seed.kbID, current.ID)
	if err != nil {
		t.Fatalf("ConsumeForceLatch second: %v", err)
	}
	if force2 {
		t.Fatal("第二次 ConsumeForceLatch 应返回 false")
	}

	// 再次 force=true：应复用进行中的 Job（created=false）并重新置位 latch。
	same, created, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, seed.connectionID, true)
	if err != nil {
		t.Fatalf("RequestSourceSync reuse: %v", err)
	}
	if created {
		t.Fatal("复用进行中 Job 应 created=false")
	}
	if same.ID != current.ID {
		t.Fatalf("复用 Job id = %s, want %s", same.ID, current.ID)
	}

	// FinalizeSourceSyncJob(succeeded)：latch 仍为 true => 返回新建的下一个 Job。
	next, err := store.FinalizeSourceSyncJob(ctx, seed.workspaceID, seed.kbID, current.ID, value.JobStatusCompleted, "")
	if err != nil {
		t.Fatalf("FinalizeSourceSyncJob: %v", err)
	}
	if next == nil {
		t.Fatal("FinalizeSourceSyncJob 返回 nil，want 新建的下一个 Job（latch 为 true）")
	}
	if next.ID == current.ID {
		t.Fatal("下一个 Job 不应等于已完成的 Job")
	}

	// 原 Job 应为 succeeded 终态。
	var curRow JobRow
	if err := database.WithContext(ctx).
		First(&curRow, "workspace_id = ? AND id = ?", seed.workspaceID, current.ID).Error; err != nil {
		t.Fatalf("read back current job: %v", err)
	}
	if curRow.Status != string(value.JobStatusCompleted) {
		t.Fatalf("current job status = %q, want succeeded", curRow.Status)
	}
}

// TestRequestSourceSyncForceFalseDoesNotSetLatch 验证 requestedForce=false 不置位 latch。
func TestRequestSourceSyncForceFalseDoesNotSetLatch(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	job, created, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, seed.connectionID, false)
	if err != nil {
		t.Fatalf("RequestSourceSync: %v", err)
	}
	if !created {
		t.Fatal("首次应新建 Job")
	}
	force, err := store.ConsumeForceLatch(ctx, seed.workspaceID, seed.kbID, job.ID)
	if err != nil {
		t.Fatalf("ConsumeForceLatch: %v", err)
	}
	if force {
		t.Fatal("force=false 时 latch 应保持 false")
	}
	// finalize 时 latch=false => 不应产生下一个 Job。
	next, err := store.FinalizeSourceSyncJob(ctx, seed.workspaceID, seed.kbID, job.ID, value.JobStatusCompleted, "")
	if err != nil {
		t.Fatalf("FinalizeSourceSyncJob: %v", err)
	}
	if next != nil {
		t.Fatalf("latch=false 时不应创建下一个 Job，got %s", next.ID)
	}
}

// TestFinalizeSourceSyncJobIsIdempotentOnRetry（spec 8.2 / AGENTS 5.5）验证：
// 对一个已终结的 Job 再次调用 FinalizeSourceSyncJob 不应重复转换状态，
// 也不应在 latch 被重新置位时为同一个已终结 Job 再创建下一个 Job
// （避免 asynq 重试已完成的任务时产生重复后续 Job）。
func TestFinalizeSourceSyncJobIsIdempotentOnRetry(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	current, created, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, seed.connectionID, false)
	if err != nil {
		t.Fatalf("RequestSourceSync: %v", err)
	}
	if !created {
		t.Fatal("首次 RequestSourceSync 应创建新 Job")
	}

	// 第一次 finalize：succeeded，latch=false => 无下一个 Job。
	next, err := store.FinalizeSourceSyncJob(ctx, seed.workspaceID, seed.kbID, current.ID, value.JobStatusCompleted, "")
	if err != nil {
		t.Fatalf("第一次 FinalizeSourceSyncJob: %v", err)
	}
	if next != nil {
		t.Fatalf("latch=false 时不应创建下一个 Job，got %s", next.ID)
	}

	// 模拟 asynq 重试：在 Job 已终结后，用户又请求 force（latch=true），
	// 然后对同一个已终结 Job 再次 finalize（worker 重试到达）。
	if _, _, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, seed.connectionID, true); err != nil {
		t.Fatalf("第二次 RequestSourceSync(force=true): %v", err)
	}
	retryNext, err := store.FinalizeSourceSyncJob(ctx, seed.workspaceID, seed.kbID, current.ID, value.JobStatusFailed, "retry")
	if err != nil {
		t.Fatalf("重试 FinalizeSourceSyncJob 不应报错: %v", err)
	}
	if retryNext != nil {
		t.Fatalf("已终结 Job 再次 finalize 不应创建后续 Job，got %s", retryNext.ID)
	}
}

// TestListFeishuKBsWithForceLatchAndNoActiveJob 验证 spec 8.2 latch 恢复扫描：
// 返回 latch=true 且无 active source_sync Job 的飞书 KB；存在 active Job 时不返回；
// latch=false 时不返回。
func TestListFeishuKBsWithForceLatchAndNoActiveJob(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// 给 KB 设置飞书类型 + latch=true + source_connection_id。
	if err := database.WithContext(ctx).Exec(
		"UPDATE knowledge_bases SET source_type = 'feishu_wiki', "+
			"source_config = '{\"root_token\":\"wikcnRoot\",\"sync_requested_force\":true}'::jsonb, "+
			"source_connection_id = ? WHERE workspace_id = ? AND id = ?",
		seed.connectionID, seed.workspaceID, seed.kbID,
	).Error; err != nil {
		t.Fatalf("set kb source_config: %v", err)
	}

	// 初始：latch=true，无 active Job => 应被列出。
	got, err := store.ListFeishuKBsWithForceLatchAndNoActiveJob(ctx)
	if err != nil {
		t.Fatalf("ListFeishuKBsWithForceLatchAndNoActiveJob: %v", err)
	}
	found := false
	for _, kb := range got {
		if kb.ID == seed.kbID {
			found = true
			if kb.WorkspaceID != seed.workspaceID || kb.SourceConnectionID != seed.connectionID {
				t.Fatalf("lineage = %+v, want ws=%s conn=%s", kb, seed.workspaceID, seed.connectionID)
			}
		}
	}
	if !found {
		t.Fatal("latch=true 且无 active Job 的 KB 应被列出")
	}

	// 创建一个 pending source_sync Job => 不应再被列出（有 active Job）。
	if _, _, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, seed.connectionID, false); err != nil {
		t.Fatalf("RequestSourceSync: %v", err)
	}
	got2, err := store.ListFeishuKBsWithForceLatchAndNoActiveJob(ctx)
	if err != nil {
		t.Fatalf("ListFeishuKBsWithForceLatchAndNoActiveJob second: %v", err)
	}
	for _, kb := range got2 {
		if kb.ID == seed.kbID {
			t.Fatal("存在 active Job 时 KB 不应被列出")
		}
	}

	// 清掉 latch（ConsumeForceLatch）并终结 Job => 无 active Job 且 latch=false，不应被列出。
	allJobs := listSourceSyncJobs(t, ctx, database, seed.workspaceID, seed.kbID)
	if len(allJobs) == 0 {
		t.Fatal("expected at least one source_sync job")
	}
	if _, err := store.ConsumeForceLatch(ctx, seed.workspaceID, seed.kbID, allJobs[0].ID); err != nil {
		t.Fatalf("ConsumeForceLatch: %v", err)
	}
	if _, err := store.FinalizeSourceSyncJob(ctx, seed.workspaceID, seed.kbID, allJobs[0].ID, value.JobStatusCompleted, ""); err != nil {
		t.Fatalf("FinalizeSourceSyncJob: %v", err)
	}
	got3, err := store.ListFeishuKBsWithForceLatchAndNoActiveJob(ctx)
	if err != nil {
		t.Fatalf("ListFeishuKBsWithForceLatchAndNoActiveJob third: %v", err)
	}
	for _, kb := range got3 {
		if kb.ID == seed.kbID {
			t.Fatal("latch=false 且无 active Job 时 KB 不应被列出")
		}
	}
}

// listSourceSyncJobs 读取某 KB 下所有 source_sync Job（按创建时间）。
func listSourceSyncJobs(t *testing.T, ctx context.Context, tx *gorm.DB, workspaceID, kbID uuid.UUID) []JobRow {
	t.Helper()
	var rows []JobRow
	if err := tx.WithContext(ctx).
		Where("workspace_id = ? AND knowledge_base_id = ? AND type = ?", workspaceID, kbID, model.SourceSyncJobType).
		Order("created_at, id").
		Find(&rows).Error; err != nil {
		t.Fatalf("list source_sync jobs: %v", err)
	}
	return rows
}

// ---- UpdateSyncResult（spec：保留其它 jsonb key）----

// TestUpdateSyncResultPreservesOtherKeys 验证 jsonb_set 只改 sync_last_result，保留 latch/cursor/root。
func TestUpdateSyncResultPreservesOtherKeys(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// 给 KB 设置 source_config（含 root_token + latch + cursor）。
	if err := database.WithContext(ctx).Exec(
		"UPDATE knowledge_bases SET source_type = 'feishu_wiki', "+
			"source_config = '{\"root_token\":\"wikcnRoot\",\"sync_requested_force\":true,\"sync_cursor\":\"2026-01-01T00:00:00Z\"}'::jsonb "+
			"WHERE workspace_id = ? AND id = ?",
		seed.workspaceID, seed.kbID,
	).Error; err != nil {
		t.Fatalf("set kb source_config: %v", err)
	}

	result := appservice.SyncResult{
		Status:           "succeeded",
		Complete:         true,
		SyncedDocuments:  3,
		SkippedDocuments: 1,
		FinishedAt:       time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
	}
	if err := store.UpdateSyncResult(ctx, seed.workspaceID, seed.kbID, result); err != nil {
		t.Fatalf("UpdateSyncResult: %v", err)
	}

	// sync_last_result 应可读回。
	var lastResult string
	if err := database.WithContext(ctx).
		Raw("SELECT source_config->>'sync_last_result' FROM knowledge_bases WHERE workspace_id = ? AND id = ?",
			seed.workspaceID, seed.kbID).
		Scan(&lastResult).Error; err != nil {
		t.Fatalf("read back sync_last_result: %v", err)
	}
	if !strings.Contains(lastResult, "\"synced_documents\": 3") {
		t.Fatalf("sync_last_result = %s, want 含 synced_documents: 3", lastResult)
	}

	// 其它 key 应保留。
	var rootToken, latch, cursor string
	database.WithContext(ctx).
		Raw("SELECT source_config->>'root_token' FROM knowledge_bases WHERE id = ?", seed.kbID).Scan(&rootToken)
	database.WithContext(ctx).
		Raw("SELECT source_config->>'sync_requested_force' FROM knowledge_bases WHERE id = ?", seed.kbID).Scan(&latch)
	database.WithContext(ctx).
		Raw("SELECT source_config->>'sync_cursor' FROM knowledge_bases WHERE id = ?", seed.kbID).Scan(&cursor)
	if rootToken != "wikcnRoot" {
		t.Fatalf("root_token = %q, want wikcnRoot（jsonb_set 应保留其它 key）", rootToken)
	}
	if latch != "true" {
		t.Fatalf("sync_requested_force = %q, want true（不应被 UpdateSyncResult 清空）", latch)
	}
	if cursor == "" {
		t.Fatal("sync_cursor 被错误清除")
	}
}

// ---- ListSourceDocuments + RetryRequired ----

// TestListSourceDocumentsRetryRequiredForFailedDoc 验证：
//   - status=failed 的文档 RetryRequired=true；
//   - 最新 source revision 未 ready（pending）的文档 RetryRequired=true；
//   - 已 ready 且无失败 Job 的文档 RetryRequired=false。
func TestListSourceDocumentsRetryRequiredForFailedDoc(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// 文档 A：文档 status=failed。
	docA, _, _, _ := seedSyncedDocument(t, ctx, database, seed, "失败文档A", "doccnFailA")
	if err := database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, docA.ID).
		Update("status", string(value.DocumentStatusFailed)).Error; err != nil {
		t.Fatalf("mark docA failed: %v", err)
	}

	// 文档 B：revision 仍 pending（未完成）。
	seedSyncedDocument(t, ctx, database, seed, "未完成文档B", "doccnPendingB")

	// 文档 C：revision 置 ready + parse job 置 succeeded（RetryRequired=false）。
	_, revC, _, jobC := seedSyncedDocument(t, ctx, database, seed, "就绪文档C", "doccnReadyC")
	now := time.Now().UTC()
	if err := database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, revC.ID).
		Updates(map[string]any{"status": string(value.DocumentRevisionReady), "completed_at": now}).Error; err != nil {
		t.Fatalf("mark revC ready: %v", err)
	}
	if err := database.WithContext(ctx).Model(&JobRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, jobC.ID).
		Update("status", string(value.JobStatusCompleted)).Error; err != nil {
		t.Fatalf("mark jobC succeeded: %v", err)
	}

	views, err := store.ListSourceDocuments(ctx, seed.kbID)
	if err != nil {
		t.Fatalf("ListSourceDocuments: %v", err)
	}
	byExternal := make(map[string]appservice.LocalDocView, len(views))
	for _, v := range views {
		byExternal[v.ExternalID] = v
	}

	if a, ok := byExternal["doccnFailA"]; !ok {
		t.Fatal("缺少 doccnFailA")
	} else if !a.RetryRequired {
		t.Fatal("status=failed 的文档应 RetryRequired=true")
	}

	if b, ok := byExternal["doccnPendingB"]; !ok {
		t.Fatal("缺少 doccnPendingB")
	} else if !b.RetryRequired {
		t.Fatal("revision 未 ready 的文档应 RetryRequired=true")
	}

	if c, ok := byExternal["doccnReadyC"]; !ok {
		t.Fatal("缺少 doccnReadyC")
	} else if c.RetryRequired {
		t.Fatal("revision ready 且 job succeeded 的文档应 RetryRequired=false")
	} else if c.RevisionNo != 1 {
		t.Fatalf("doccnReadyC RevisionNo = %d, want 1", c.RevisionNo)
	}
}

// ---- DeleteSourceDocument（keep / remove）----

// TestDeleteSourceDocumentKeepSoftDeletes 验证 keep 策略软删、保留对象，返回空清理列表。
func TestDeleteSourceDocumentKeepSoftDeletes(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, rev, _, _ := seedSyncedDocument(t, ctx, database, seed, "待删文档keep", "doccnDelKeep")
	store := NewSourceSyncDBStore(database)

	objects, jobs, err := store.DeleteSourceDocument(ctx, seed.workspaceID, document.ID, value.SourceDeleteKeep)
	if err != nil {
		t.Fatalf("DeleteSourceDocument keep: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("keep 策略应返回空清理列表，got %d", len(objects))
	}
	if len(jobs) != 0 {
		t.Fatalf("keep 策略应返回空清理 Job 列表，got %d", len(jobs))
	}

	var docRow DocumentRow
	if err := database.WithContext(ctx).
		First(&docRow, "workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Error; err != nil {
		t.Fatalf("read back document: %v", err)
	}
	if docRow.DeletedAt == nil {
		t.Fatal("keep 策略应软删（deleted_at 非空）")
	}
	if docRow.Status != string(value.DocumentStatusDeleted) {
		t.Fatalf("status = %q, want deleted", docRow.Status)
	}
	// revision 应仍存在。
	var revCount int64
	database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, rev.ID).Count(&revCount)
	if revCount != 1 {
		t.Fatalf("keep 策略应保留 revision，got count=%d", revCount)
	}
}

// TestDeleteSourceDocumentRemoveCollectsKeysAndCascades 验证 remove 策略：
// 收集 raw key、建立 KB 级 source_cleanup Job、硬删 Document（级联清掉 revision）。
func TestDeleteSourceDocumentRemoveCollectsKeysAndCascades(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	document, rev, _, _ := seedSyncedDocument(t, ctx, database, seed, "待删文档remove", "doccnDelRemove")
	store := NewSourceSyncDBStore(database)

	objects, jobs, err := store.DeleteSourceDocument(ctx, seed.workspaceID, document.ID, value.SourceDeleteRemove)
	if err != nil {
		t.Fatalf("DeleteSourceDocument remove: %v", err)
	}
	if len(objects) == 0 {
		t.Fatal("remove 策略应收集清理对象")
	}
	if len(jobs) == 0 {
		t.Fatal("remove 策略应创建至少一个 source_cleanup Job")
	}
	// 应包含该 revision 的 raw key。
	wantRaw := "raw/doccnDelRemove.md"
	foundRaw := false
	for _, o := range objects {
		if o.Key == wantRaw && o.Store == "raw" {
			foundRaw = true
		}
	}
	if !foundRaw {
		t.Fatalf("清理对象未包含 raw key %q, got %+v", wantRaw, objects)
	}

	// Document 应被硬删。
	var docCount int64
	database.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, document.ID).Count(&docCount)
	if docCount != 0 {
		t.Fatalf("remove 策略应硬删 Document，仍剩 %d 行", docCount)
	}
	// revision 应被 FK 级联删除。
	var revCount int64
	database.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", seed.workspaceID, rev.ID).Count(&revCount)
	if revCount != 0 {
		t.Fatalf("revision 应被级联删除，仍剩 %d 行", revCount)
	}

	// 应建立 KB 级 source_cleanup Job（无 document_id），payload 含 key 列表。
	var cleanupJob JobRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND knowledge_base_id = ? AND type = ?",
			seed.workspaceID, seed.kbID, model.SourceCleanupJobType).
		First(&cleanupJob).Error; err != nil {
		t.Fatalf("read back source_cleanup job: %v", err)
	}
	if cleanupJob.DocumentID != nil {
		t.Fatalf("source_cleanup Job 应无 document_id，got %v", cleanupJob.DocumentID)
	}
	payloadObjs, _ := cleanupJob.Payload["objects"].([]any)
	if len(payloadObjs) == 0 {
		t.Fatalf("source_cleanup Job payload.objects 为空，应包含清理 key")
	}
}

// ---- UpsertSourceFolder ----

// TestUpsertSourceFolderInsertsThenUpdates 验证先插入、再按 external_id upsert 更新。
func TestUpsertSourceFolderInsertsThenUpdates(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertSourceSyncSeed(t, ctx, database)
	store := NewSourceSyncDBStore(database)

	// 第一次：插入 folder。
	parentID := seed.rootID
	folder1, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, ParentID: &parentID,
		NodeType: value.FileTreeNodeFolder, Name: "飞书文件夹",
		ExternalID: "foldercnRoot1",
	})
	if err != nil {
		t.Fatalf("new folder1: %v", err)
	}
	if err := store.UpsertSourceFolder(ctx, folder1); err != nil {
		t.Fatalf("UpsertSourceFolder insert: %v", err)
	}

	// 第二次：同 external_id、不同 parent/name => upsert 更新既有行。
	otherParent := uuid.New()
	// 先插入一个二级父 folder 供 reparent。
	secondParent, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, ParentID: &parentID,
		NodeType: value.FileTreeNodeFolder, Name: "父文件夹2",
		ExternalID: "foldercnParent2",
	})
	if err != nil {
		t.Fatalf("new second parent: %v", err)
	}
	if err := store.UpsertSourceFolder(ctx, secondParent); err != nil {
		t.Fatalf("UpsertSourceFolder second parent: %v", err)
	}
	otherParent = secondParent.ID

	folder2, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
		WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, ParentID: &otherParent,
		NodeType: value.FileTreeNodeFolder, Name: "飞书文件夹（改名）",
		ExternalID: "foldercnRoot1", // 同 external_id
	})
	if err != nil {
		t.Fatalf("new folder2: %v", err)
	}
	if err := store.UpsertSourceFolder(ctx, folder2); err != nil {
		t.Fatalf("UpsertSourceFolder update: %v", err)
	}

	// 应只有一行 external_id=foldercnRoot1，且 parent/name 已更新为新值。
	var rows []FileTreeNodeRow
	if err := database.WithContext(ctx).
		Where("workspace_id = ? AND knowledge_base_id = ? AND external_id = ?",
			seed.workspaceID, seed.kbID, "foldercnRoot1").
		Find(&rows).Error; err != nil {
		t.Fatalf("read back folder: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应有 1 行 folder，got %d", len(rows))
	}
	if rows[0].Name != "飞书文件夹（改名）" {
		t.Fatalf("folder name = %q, want 改名后的值", rows[0].Name)
	}
	if rows[0].ParentID == nil || *rows[0].ParentID != otherParent {
		t.Fatalf("folder parent_id = %#v, want %s", rows[0].ParentID, otherParent)
	}
}
