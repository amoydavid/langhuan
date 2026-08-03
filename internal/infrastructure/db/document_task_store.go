package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/dto"
	appservice "github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentTaskDBStore validates worker lineage and persists terminal state in Workspace transactions.
type DocumentTaskDBStore struct {
	db *gorm.DB
}

// NewDocumentTaskStore creates a Workspace-scoped document task store.
func NewDocumentTaskStore(database *gorm.DB) *DocumentTaskDBStore {
	return &DocumentTaskDBStore{db: database}
}

// WithinWorkspace reads one task through a transaction-local tenant context.
func (s *DocumentTaskDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, appservice.DocumentTaskTx) error,
) error {
	if fn == nil {
		return fmt.Errorf("%w: DocumentTask transaction callback 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &documentTaskDBTx{db: tx, workspaceID: workspaceID})
	})
}

// MarkRunning records one trusted task attempt inside its Workspace boundary.
func (s *DocumentTaskDBStore) MarkRunning(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	return s.updateJob(ctx, workspaceID, jobID, map[string]any{
		"status": string(value.JobStatusRunning), "attempts": gorm.Expr("attempts + 1"),
		"error_class": "", "error_message": "", "updated_at": time.Now().UTC(),
	}, "标记文档任务运行中失败")
}

// MarkSucceeded completes one trusted task inside its Workspace boundary.
func (s *DocumentTaskDBStore) MarkSucceeded(ctx context.Context, workspaceID, jobID uuid.UUID) error {
	return s.updateJob(ctx, workspaceID, jobID, map[string]any{
		"status": string(value.JobStatusCompleted), "error_class": "", "error_message": "",
		"updated_at": time.Now().UTC(),
	}, "标记文档任务成功失败")
}

// MarkFailed records a retryable task failure without changing the Revision state.
func (s *DocumentTaskDBStore) MarkFailed(
	ctx context.Context,
	workspaceID, jobID uuid.UUID,
	message string,
) error {
	return s.updateJob(ctx, workspaceID, jobID, map[string]any{
		"status": string(value.JobStatusFailed), "error_message": message, "updated_at": time.Now().UTC(),
	}, "标记文档任务失败")
}

// CreateNextForRevision persists the next stage with document lineage but no generation target column.
func (s *DocumentTaskDBStore) CreateNextForRevision(
	ctx context.Context,
	workspaceID, knowledgeBaseID, documentID, revisionID, generationID uuid.UUID,
	typ string,
	payload map[string]any,
) (*dto.Job, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["index_generation_id"] = generationID.String()
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		DocumentID: documentID, DocumentRevisionID: revisionID,
		Type: typ, Status: value.JobStatusPending, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	err = NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
			return translateDBError(err, "创建后续文档任务失败")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dto.JobFromModel(job), nil
}

func (s *DocumentTaskDBStore) updateJob(
	ctx context.Context,
	workspaceID, jobID uuid.UUID,
	updates map[string]any,
	description string,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, jobID).
			Updates(updates)
		if result.Error != nil {
			return translateDBError(result.Error, description)
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

// FailTask atomically marks both the Job and its immutable DocumentRevision failed.
func (s *DocumentTaskDBStore) FailTask(
	ctx context.Context,
	workspaceID, jobID, revisionID uuid.UUID,
	errorClass, message string,
) error {
	if workspaceID == uuid.Nil || jobID == uuid.Nil || revisionID == uuid.Nil ||
		strings.TrimSpace(errorClass) == "" || strings.TrimSpace(message) == "" {
		return fmt.Errorf("%w: DocumentTask failure lineage/message 无效", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		jobResult := tx.WithContext(ctx).Model(&JobRow{}).
			Where(
				"workspace_id = ? AND id = ? AND document_revision_id = ?",
				workspaceID, jobID, revisionID,
			).
			Updates(map[string]any{
				"status": string(value.JobStatusFailed), "error_class": errorClass,
				"error_message": message, "updated_at": now,
			})
		if jobResult.Error != nil {
			return translateDBError(jobResult.Error, "标记文档任务失败")
		}
		if jobResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		revisionResult := tx.WithContext(ctx).Model(&DocumentRevisionRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, revisionID).
			Updates(map[string]any{
				"status": string(value.DocumentRevisionFailed), "error_class": errorClass,
				"error_message": message, "completed_at": now,
			})
		if revisionResult.Error != nil {
			return translateDBError(revisionResult.Error, "标记 DocumentRevision 失败")
		}
		if revisionResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

type documentTaskDBTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *documentTaskDBTx) GetJob(ctx context.Context, id uuid.UUID) (*dto.Job, error) {
	var row JobRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取文档任务 Job 失败")
	}
	return dto.JobFromModel(jobV2FromRow(&row)), nil
}

func (tx *documentTaskDBTx) GetRevision(ctx context.Context, id uuid.UUID) (*model.DocumentRevision, error) {
	var row DocumentRevisionRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, id).
		First(&row).Error; err != nil {
		return nil, translateDBError(err, "读取文档任务 DocumentRevision 失败")
	}
	return documentRevisionFromRow(&row)
}

func (tx *documentTaskDBTx) IsRevisionPublished(
	ctx context.Context,
	generationID, revisionID uuid.UUID,
) (bool, error) {
	var count int64
	err := tx.db.WithContext(ctx).Table("documents AS d").
		Joins(
			"JOIN knowledge_bases AS kb ON kb.workspace_id = d.workspace_id AND kb.id = d.knowledge_base_id",
		).
		Where(
			"d.workspace_id = ? AND d.active_revision_id = ? AND kb.active_index_generation_id = ? "+
				"AND d.deleted_at IS NULL AND kb.deleted_at IS NULL",
			tx.workspaceID, revisionID, generationID,
		).
		Count(&count).Error
	if err != nil {
		return false, translateDBError(err, "检查 DocumentRevision 发布状态失败")
	}
	return count > 0, nil
}
