package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentRetryDBStore 是 DocumentRetryStore 的 Workspace-scoped 实现。
type DocumentRetryDBStore struct {
	db *gorm.DB
}

func NewDocumentRetryStore(database *gorm.DB) *DocumentRetryDBStore {
	return &DocumentRetryDBStore{db: database}
}

func (s *DocumentRetryDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.DocumentRetryTx) error,
) error {
	if fn == nil {
		return fmt.Errorf("%w: DocumentRetry transaction callback 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &documentRetryDBTx{db: tx, workspaceID: workspaceID})
	})
}

type documentRetryDBTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *documentRetryDBTx) GetKnowledgeBase(ctx context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).Where(
		"workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id,
	).First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取重试 KnowledgeBase 失败")
	}
	return knowledgeBaseV2FromRow(&row), nil
}

// GetLatestRevision 返回 document 的最新 revision（按 revision_no DESC）。
func (tx *documentRetryDBTx) GetLatestRevision(ctx context.Context, documentID uuid.UUID) (*model.DocumentRevision, error) {
	var row DocumentRevisionRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND document_id = ?", tx.workspaceID, documentID).
		Order("revision_no DESC, id DESC").
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取最新 DocumentRevision 失败")
	}
	return documentRevisionFromRow(&row)
}

// GetJobRevision 按 job_id 定位其关联 revision 与 KB lineage。
func (tx *documentRetryDBTx) GetJobRevision(ctx context.Context, jobID uuid.UUID) (*appservice.JobRevision, error) {
	var row JobRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, jobID).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取重试 Job 失败")
	}
	return &appservice.JobRevision{
		JobID:           row.ID,
		KnowledgeBaseID: row.KnowledgeBaseID,
		DocumentID:      derefUUID(row.DocumentID),
		RevisionID:      derefUUID(row.DocumentRevisionID),
	}, nil
}

// ResetFailedRevision 复位 failed revision 到 pending，并复位/新建幂等的 parse Job。
// revision 非 failed 时返回 ErrNotRetryable；不存在返回 ErrNotFound。
func (tx *documentRetryDBTx) ResetFailedRevision(ctx context.Context, request appservice.ResetFailedRevisionRequest) (uuid.UUID, error) {
	if request.WorkspaceID == uuid.Nil || request.DocumentID == uuid.Nil || request.RevisionID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: 重试 lineage 不能为空", domainerrors.ErrValidation)
	}

	// 锁定 revision 并校验状态。
	var revRow DocumentRevisionRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND document_id = ? AND id = ?",
			request.WorkspaceID, request.DocumentID, request.RevisionID).
		First(&revRow).Error; err != nil {
		return uuid.Nil, translateDBError(err, "锁定重试 DocumentRevision 失败")
	}
	if revRow.Status != string(value.DocumentRevisionFailed) {
		return uuid.Nil, fmt.Errorf("%w: revision 状态为 %s，仅 failed 可重试",
			domainerrors.ErrNotRetryable, revRow.Status)
	}

	now := time.Now().UTC()
	// 复位 revision：status→pending，清错误，清 completed_at。
	revUpdate := tx.db.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.RevisionID).
		Updates(map[string]any{
			"status":        string(value.DocumentRevisionPending),
			"error_class":   "",
			"error_message": "",
			"completed_at":  nil,
		})
	if revUpdate.Error != nil {
		return uuid.Nil, translateDBError(revUpdate.Error, "重置重试 DocumentRevision 失败")
	}
	if revUpdate.RowsAffected != 1 {
		return uuid.Nil, domainerrors.ErrNotFound
	}

	// 幂等 parse Job：找该 revision 最近的 document_parse_start job。
	var existingJob JobRow
	findErr := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND document_id = ? AND document_revision_id = ? AND type = ?",
			request.WorkspaceID, request.DocumentID, request.RevisionID, "document_parse_start").
		Order("updated_at DESC, id DESC").
		First(&existingJob).Error

	var jobID uuid.UUID
	if findErr == nil {
		// 重置既有 Job 为 pending。
		jobReset := tx.db.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND id = ?", request.WorkspaceID, existingJob.ID).
			Updates(map[string]any{
				"status":        string(value.JobStatusPending),
				"error_class":   "",
				"error_message": "",
				"updated_at":    now,
			})
		if jobReset.Error != nil {
			return uuid.Nil, translateDBError(jobReset.Error, "重置重试 parse Job 失败")
		}
		jobID = existingJob.ID
	} else if errors.Is(findErr, gorm.ErrRecordNotFound) {
		// 新建幂等 parse Job。
		job, err := model.NewJob(model.NewJobInput{
			WorkspaceID:        request.WorkspaceID,
			KnowledgeBaseID:    request.KnowledgeBaseID,
			DocumentID:         request.DocumentID,
			DocumentRevisionID: request.RevisionID,
			Type:               "document_parse_start",
			Status:             value.JobStatusPending,
		})
		if err != nil {
			return uuid.Nil, err
		}
		if err := tx.db.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
			return uuid.Nil, translateDBError(err, "创建重试 parse Job 失败")
		}
		jobID = job.ID
	} else {
		return uuid.Nil, translateDBError(findErr, "查找重试 parse Job 失败")
	}

	// 复位 document 状态为 pending（不复活软删，手动重试 Revive=false）。
	docUpdate := tx.db.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.DocumentID).
		Updates(map[string]any{
			"status":     string(value.DocumentStatusPending),
			"updated_at": now,
		})
	if docUpdate.Error != nil {
		return uuid.Nil, translateDBError(docUpdate.Error, "重置重试 Document 状态失败")
	}
	// document 不存在不算致命（可能已被软删），不强制 RowsAffected 校验。

	return jobID, nil
}

// derefUUID 安全解引用 *uuid.UUID，nil 返回 uuid.Nil。
func derefUUID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}
