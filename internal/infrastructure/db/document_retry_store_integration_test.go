//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// markRevisionFailed 把 revision 和 document 标记为 failed，模拟导入失败后的状态。
func markRevisionFailed(t *testing.T, ctx context.Context, database *gorm.DB, seed knowledgeSchemaSeed, documentID, revisionID uuid.UUID) {
	t.Helper()
	if err := database.WithContext(ctx).Exec(
		"UPDATE document_revisions SET status = 'failed', error_class = 'parse_error', error_message = 'boom' WHERE id = ?",
		revisionID,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Exec(
		"UPDATE documents SET status = 'failed' WHERE id = ?", documentID,
	).Error; err != nil {
		t.Fatal(err)
	}
}

func TestDocumentRetryStoreResetFailedRevision(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "retry.md"); err != nil {
		t.Fatal(err)
	}
	markRevisionFailed(t, ctx, database, seed, documentID, revisionID)

	store := NewDocumentRetryStore(database)
	var jobID uuid.UUID
	err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentRetryTx) error {
		id, err := tx.ResetFailedRevision(txCtx, appservice.ResetFailedRevisionRequest{
			WorkspaceID:     seed.workspaceID,
			KnowledgeBaseID: seed.kbID,
			DocumentID:      documentID,
			RevisionID:      revisionID,
			GenerationID:    seed.generationID,
		})
		jobID = id
		return err
	})
	if err != nil {
		t.Fatalf("ResetFailedRevision error = %v", err)
	}
	if jobID == uuid.Nil {
		t.Fatal("jobID = nil")
	}

	// revision 应复位为 pending，错误清空。
	var revRow DocumentRevisionRow
	if err := database.WithContext(ctx).First(&revRow, "id = ?", revisionID).Error; err != nil {
		t.Fatal(err)
	}
	if revRow.Status != string(value.DocumentRevisionPending) {
		t.Fatalf("revision status = %s, want pending", revRow.Status)
	}
	if revRow.ErrorClass != "" || revRow.ErrorMessage != "" {
		t.Fatalf("revision error not cleared = %s/%s", revRow.ErrorClass, revRow.ErrorMessage)
	}

	// document 应复位为 pending。
	var docRow DocumentRow
	if err := database.WithContext(ctx).First(&docRow, "id = ?", documentID).Error; err != nil {
		t.Fatal(err)
	}
	if docRow.Status != string(value.DocumentStatusPending) {
		t.Fatalf("document status = %s, want pending", docRow.Status)
	}

	// job 应为 pending。
	var jobRow JobRow
	if err := database.WithContext(ctx).First(&jobRow, "id = ?", jobID).Error; err != nil {
		t.Fatal(err)
	}
	if jobRow.Status != string(value.JobStatusPending) || jobRow.Type != "document_parse_start" {
		t.Fatalf("job = %#v", jobRow)
	}
}

func TestDocumentRetryStoreRejectsNonFailedRevision(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "ok.md"); err != nil {
		t.Fatal(err)
	}
	// revision 保持 pending（未失败）。

	store := NewDocumentRetryStore(database)
	err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentRetryTx) error {
		_, err := tx.ResetFailedRevision(txCtx, appservice.ResetFailedRevisionRequest{
			WorkspaceID:     seed.workspaceID,
			KnowledgeBaseID: seed.kbID,
			DocumentID:      documentID,
			RevisionID:      revisionID,
			GenerationID:    seed.generationID,
		})
		return err
	})
	if !errors.Is(err, domainerrors.ErrNotRetryable) {
		t.Fatalf("error = %v, want ErrNotRetryable", err)
	}
}

func TestDocumentRetryStoreResetIsIdempotentForJob(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "idempotent.md"); err != nil {
		t.Fatal(err)
	}
	markRevisionFailed(t, ctx, database, seed, documentID, revisionID)

	store := NewDocumentRetryStore(database)

	// 第一次复位：应新建 parse job。
	var jobID1 uuid.UUID
	err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentRetryTx) error {
		id, err := tx.ResetFailedRevision(txCtx, appservice.ResetFailedRevisionRequest{
			WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			DocumentID: documentID, RevisionID: revisionID, GenerationID: seed.generationID,
		})
		jobID1 = id
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// 再次标记 failed，第二次复位应复用同一个 job（而非新建）。
	markRevisionFailed(t, ctx, database, seed, documentID, revisionID)
	var jobID2 uuid.UUID
	err = store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentRetryTx) error {
		id, err := tx.ResetFailedRevision(txCtx, appservice.ResetFailedRevisionRequest{
			WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID,
			DocumentID: documentID, RevisionID: revisionID, GenerationID: seed.generationID,
		})
		jobID2 = id
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if jobID1 != jobID2 {
		t.Fatalf("second reset created new job %s instead of reusing %s", jobID2, jobID1)
	}

	// 该 revision 下应只有一个 parse job。
	var count int64
	database.WithContext(ctx).Model(&JobRow{}).
		Where("document_revision_id = ? AND type = ?", revisionID, "document_parse_start").
		Count(&count)
	if count != 1 {
		t.Fatalf("parse job count = %d, want 1", count)
	}
}

func TestDocumentRetryStoreGetLatestRevision(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID := uuid.New()
	rev1, rev2 := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, rev1, "v1.md"); err != nil {
		t.Fatal(err)
	}
	// 手动插入 revision_no=2 的第二个 revision。
	if err := database.WithContext(ctx).Exec(
		"INSERT INTO document_revisions "+
			"(id, workspace_id, knowledge_base_id, document_id, kind, revision_no, revision_reason, "+
			"original_filename, file_type, raw_storage_key, processing_version, status) "+
			"VALUES (?, ?, ?, ?, 'file', 2, 'ingest', ?, 'markdown', ?, 1, 'failed')",
		rev2, seed.workspaceID, seed.kbID, documentID, "v2.md", "raw/"+rev2.String(),
	).Error; err != nil {
		t.Fatal(err)
	}

	store := NewDocumentRetryStore(database)
	var latest *appservice.JobRevision
	_ = latest
	err := store.WithinWorkspace(ctx, seed.workspaceID, func(txCtx context.Context, tx appservice.DocumentRetryTx) error {
		rev, err := tx.GetLatestRevision(txCtx, documentID)
		if err != nil {
			return err
		}
		if rev.ID != rev2 {
			t.Fatalf("latest revision = %s, want %s (revision_no=2)", rev.ID, rev2)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocumentRetryStoreGetLatestRevisionCrossWorkspace404(t *testing.T) {
	ctx, database := newAuthTestDB(t)
	seed := insertKnowledgeSchemaSeed(t, ctx, database)
	documentID, revisionID := uuid.New(), uuid.New()
	if err := insertFileDocumentRevision(ctx, database, seed, documentID, revisionID, "isolated.md"); err != nil {
		t.Fatal(err)
	}

	// 用另一个 workspace 查询，应 404。
	otherWorkspace := uuid.New()
	store := NewDocumentRetryStore(database)
	err := store.WithinWorkspace(ctx, otherWorkspace, func(txCtx context.Context, tx appservice.DocumentRetryTx) error {
		_, err := tx.GetLatestRevision(txCtx, documentID)
		return err
	})
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("cross-workspace error = %v, want ErrNotFound", err)
	}
}
