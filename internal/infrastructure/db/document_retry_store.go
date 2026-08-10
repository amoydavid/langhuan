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
	if request.GenerationID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: 重试 generation 不能为空", domainerrors.ErrValidation)
	}

	// 锁序：先锁 Document 再锁 Revision（与 source_sync RetrySourceRevision 一致，
	// 避免两路复位逻辑在同一文档上 AB-BA 死锁）。
	// deleted_at IS NULL：软删文档不可重试，早失败返回 ErrNotFound。
	var docRow DocumentRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", request.WorkspaceID, request.DocumentID).
		First(&docRow).Error; err != nil {
		return uuid.Nil, translateDBError(err, "锁定重试 Document 失败")
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
		// 重置既有 Job 为 pending，并刷新 payload 的 index_generation_id 为当前 active
		// generation（reindex 后重试必须指向新 generation，否则 worker 校验失败）。
		jobReset := tx.db.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND id = ?", request.WorkspaceID, existingJob.ID).
			Updates(map[string]any{
				"status":        string(value.JobStatusPending),
				"error_class":   "",
				"error_message": "",
				"updated_at":    now,
				"payload":       gorm.Expr("jsonb_set(COALESCE(payload, '{}'::jsonb), '{index_generation_id}', to_jsonb(?::text), true)", request.GenerationID.String()),
			})
		if jobReset.Error != nil {
			return uuid.Nil, translateDBError(jobReset.Error, "重置重试 parse Job 失败")
		}
		jobID = existingJob.ID
	} else if errors.Is(findErr, gorm.ErrRecordNotFound) {
		// 新建幂等 parse Job，payload 携带 index_generation_id 供 worker 校验。
		job, err := model.NewJob(model.NewJobInput{
			WorkspaceID:        request.WorkspaceID,
			KnowledgeBaseID:    request.KnowledgeBaseID,
			DocumentID:         request.DocumentID,
			DocumentRevisionID: request.RevisionID,
			Type:               "document_parse_start",
			Status:             value.JobStatusPending,
			Payload: map[string]any{
				"index_generation_id": request.GenerationID.String(),
			},
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
	// 软删（deleted_at 非空）文档不可重试——重试其 revision 没有意义，
	// 返回 ErrNotFound 让调用方明确感知，而非静默 202。
	docUpdate := tx.db.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", request.WorkspaceID, request.DocumentID).
		Updates(map[string]any{
			"status":     string(value.DocumentStatusPending),
			"updated_at": now,
		})
	if docUpdate.Error != nil {
		return uuid.Nil, translateDBError(docUpdate.Error, "重置重试 Document 状态失败")
	}
	if docUpdate.RowsAffected != 1 {
		return uuid.Nil, domainerrors.ErrNotFound
	}

	return jobID, nil
}

// FailReset 把已复位（pending）的 revision 与 job 标回 failed，供入队失败补偿。
// 幂等：目标已非 pending 时仍按成功处理（状态已推进，无需回滚）。
func (tx *documentRetryDBTx) FailReset(
	ctx context.Context,
	request appservice.ResetFailedRevisionRequest,
	jobID uuid.UUID,
	message string,
) error {
	if request.WorkspaceID == uuid.Nil || request.DocumentID == uuid.Nil ||
		request.RevisionID == uuid.Nil || jobID == uuid.Nil {
		return fmt.Errorf("%w: FailReset lineage 不能为空", domainerrors.ErrValidation)
	}
	now := time.Now().UTC()
	// job 标回 failed。
	jobUpdate := tx.db.WithContext(ctx).Model(&JobRow{}).
		Where("workspace_id = ? AND id = ? AND document_revision_id = ?",
			request.WorkspaceID, jobID, request.RevisionID).
		Updates(map[string]any{
			"status":        string(value.JobStatusFailed),
			"error_class":   "enqueue_error",
			"error_message": message,
			"updated_at":    now,
		})
	if jobUpdate.Error != nil {
		return translateDBError(jobUpdate.Error, "补偿标记重试 Job 失败")
	}
	// revision 标回 failed。
	revUpdate := tx.db.WithContext(ctx).Model(&DocumentRevisionRow{}).
		Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.RevisionID).
		Updates(map[string]any{
			"status":        string(value.DocumentRevisionFailed),
			"error_class":   "enqueue_error",
			"error_message": message,
			"completed_at":  now,
		})
	if revUpdate.Error != nil {
		return translateDBError(revUpdate.Error, "补偿标记重试 DocumentRevision 失败")
	}
	return nil
}

// derefUUID 安全解引用 *uuid.UUID，nil 返回 uuid.Nil。
func derefUUID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}
