package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentIngestDBStore binds File-ingest repositories to one Workspace transaction.
type DocumentIngestDBStore struct {
	db *gorm.DB
}

// NewDocumentIngestDBStore creates the v2 File-ingest store.
func NewDocumentIngestDBStore(database *gorm.DB) *DocumentIngestDBStore {
	return &DocumentIngestDBStore{db: database}
}

func (s *DocumentIngestDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, service.DocumentIngestTx) error,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &documentIngestTx{db: tx, workspaceID: workspaceID})
	})
}

// FailCreatedIngest atomically marks a newly created File Document, Revision and Job failed.
func (s *DocumentIngestDBStore) FailCreatedIngest(
	ctx context.Context,
	workspaceID, documentID, revisionID, jobID uuid.UUID,
	errorClass, message string,
) error {
	if workspaceID == uuid.Nil || documentID == uuid.Nil || revisionID == uuid.Nil || jobID == uuid.Nil ||
		strings.TrimSpace(errorClass) == "" || strings.TrimSpace(message) == "" {
		return fmt.Errorf("%w: File ingest failure lineage/message 无效", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		documentResult := tx.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, documentID).
			Updates(map[string]any{
				"status": string(value.DocumentStatusFailed), "updated_at": now,
			})
		if documentResult.Error != nil {
			return translateDBError(documentResult.Error, "标记 File Document 入队失败")
		}
		if documentResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		revisionResult := tx.WithContext(ctx).Model(&DocumentRevisionRow{}).
			Where("workspace_id = ? AND document_id = ? AND id = ?", workspaceID, documentID, revisionID).
			Updates(map[string]any{
				"status": string(value.DocumentRevisionFailed), "error_class": errorClass,
				"error_message": message, "completed_at": now,
			})
		if revisionResult.Error != nil {
			return translateDBError(revisionResult.Error, "标记 File DocumentRevision 入队失败")
		}
		if revisionResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		jobResult := tx.WithContext(ctx).Model(&JobRow{}).
			Where(
				"workspace_id = ? AND document_id = ? AND document_revision_id = ? AND id = ?",
				workspaceID, documentID, revisionID, jobID,
			).
			Updates(map[string]any{
				"status": string(value.JobStatusFailed), "error_class": errorClass,
				"error_message": message, "updated_at": now,
			})
		if jobResult.Error != nil {
			return translateDBError(jobResult.Error, "标记 File parse Job 入队失败")
		}
		if jobResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

type documentIngestTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *documentIngestTx) GetKnowledgeBase(ctx context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).First(&row, "workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).Error; err != nil {
		return nil, translateDBError(err, "读取知识库失败")
	}
	return knowledgeBaseV2FromRow(&row), nil
}

func (tx *documentIngestTx) FindReusableRevision(
	ctx context.Context,
	knowledgeBaseID uuid.UUID,
	hash string,
	processingVersion int,
) (*model.Document, *model.DocumentRevision, *model.Job, error) {
	var documentRow DocumentRow
	query := tx.db.WithContext(ctx).Table("documents").Select("documents.*").
		Joins("JOIN document_revisions ON document_revisions.id = documents.active_revision_id AND document_revisions.workspace_id = documents.workspace_id").
		Where("documents.workspace_id = ? AND documents.knowledge_base_id = ?", tx.workspaceID, knowledgeBaseID).
		Where("documents.kind = ? AND documents.deleted_at IS NULL", string(value.DocumentKindFile)).
		Where("document_revisions.sha256 = ? AND document_revisions.processing_version = ?", hash, processingVersion).
		Where("document_revisions.status = ?", string(value.DocumentRevisionReady)).
		Order("documents.updated_at DESC, documents.id DESC")
	if err := query.First(&documentRow).Error; err != nil {
		return nil, nil, nil, translateDBError(err, "查找可复用文档版本失败")
	}
	if documentRow.ActiveRevisionID == nil {
		return nil, nil, nil, domainerrors.ErrNotFound
	}
	var revisionRow DocumentRevisionRow
	if err := tx.db.WithContext(ctx).First(&revisionRow,
		"workspace_id = ? AND knowledge_base_id = ? AND document_id = ? AND id = ?",
		tx.workspaceID, knowledgeBaseID, documentRow.ID, *documentRow.ActiveRevisionID,
	).Error; err != nil {
		return nil, nil, nil, translateDBError(err, "读取可复用文档版本失败")
	}
	var jobRow JobRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND document_revision_id = ?", tx.workspaceID, revisionRow.ID).
		Order("created_at DESC, id DESC").First(&jobRow).Error; err != nil {
		return nil, nil, nil, translateDBError(err, "读取可复用文档任务失败")
	}
	revision, err := documentRevisionFromRow(&revisionRow)
	if err != nil {
		return nil, nil, nil, err
	}
	return documentV2FromRow(&documentRow), revision, jobV2FromRow(&jobRow), nil
}

func (tx *documentIngestTx) GetFileTreeNodeForUpdate(ctx context.Context, id uuid.UUID) (*model.FileTreeNode, error) {
	var row FileTreeNodeRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "workspace_id = ? AND id = ?", tx.workspaceID, id).Error; err != nil {
		return nil, translateDBError(err, "锁定文件树节点失败")
	}
	return fileTreeNodeFromRow(&row), nil
}

func (tx *documentIngestTx) CreateFileDocumentNodeRevisionAndJob(
	ctx context.Context,
	document *model.Document,
	node *model.FileTreeNode,
	revision *model.DocumentRevision,
	job *model.Job,
) error {
	if document.WorkspaceID != tx.workspaceID || node.WorkspaceID != tx.workspaceID ||
		revision.WorkspaceID != tx.workspaceID || job.WorkspaceID != tx.workspaceID {
		return fmt.Errorf("%w: File ingest Workspace lineage 不一致", domainerrors.ErrValidation)
	}
	revisionRow, err := documentRevisionToRow(revision)
	if err != nil {
		return err
	}
	if err := tx.db.WithContext(ctx).Create(documentV2ToRow(document)).Error; err != nil {
		return translateDBError(err, "创建 File Document 失败")
	}
	if err := tx.db.WithContext(ctx).Create(fileTreeNodeToRow(node)).Error; err != nil {
		mapped := translateDBError(err, "创建 File 节点失败")
		if errors.Is(mapped, domainerrors.ErrConflict) {
			return domainerrors.ErrFileTreeNameConflict
		}
		return mapped
	}
	if err := tx.db.WithContext(ctx).Create(revisionRow).Error; err != nil {
		return translateDBError(err, "创建 File DocumentRevision 失败")
	}
	if err := tx.db.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		return translateDBError(err, "创建 File parse Job 失败")
	}
	return nil
}
