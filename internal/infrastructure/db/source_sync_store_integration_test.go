//go:build integration

package db

import (
	"context"
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
