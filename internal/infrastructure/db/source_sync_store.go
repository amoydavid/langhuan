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

// SourceSyncDBStore 把来源同步的写入绑定到单个 Workspace 事务。
type SourceSyncDBStore struct {
	db *gorm.DB
}

// NewSourceSyncDBStore 创建来源同步 store。
func NewSourceSyncDBStore(database *gorm.DB) *SourceSyncDBStore {
	return &SourceSyncDBStore{db: database}
}

func (s *SourceSyncDBStore) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(context.Context, service.SourceSyncTx) error,
) error {
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		return fn(ctx, &sourceSyncTx{db: tx, workspaceID: workspaceID})
	})
}

// CreateSourceSyncJob 持久化一个 source_sync 任务（仅关联 KB）。
func (s *SourceSyncDBStore) CreateSourceSyncJob(ctx context.Context, job *model.Job) error {
	if job == nil {
		return fmt.Errorf("%w: source_sync Job 不能为空", domainerrors.ErrValidation)
	}
	if job.Type != model.SourceSyncJobType {
		return fmt.Errorf("%w: CreateSourceSyncJob 仅接受 source_sync 任务", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, job.WorkspaceID, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
			return translateDBError(err, "创建 source_sync Job 失败")
		}
		return nil
	})
}

// FailCreatedSync 把刚创建的同步 Document/Revision/Job 标记为失败（入队失败兜底）。
func (s *SourceSyncDBStore) FailCreatedSync(
	ctx context.Context,
	workspaceID, documentID, revisionID, jobID uuid.UUID,
	errorClass, message string,
) error {
	if workspaceID == uuid.Nil || documentID == uuid.Nil || revisionID == uuid.Nil || jobID == uuid.Nil ||
		strings.TrimSpace(errorClass) == "" || strings.TrimSpace(message) == "" {
		return fmt.Errorf("%w: 来源同步失败 lineage/message 无效", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		documentResult := tx.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", workspaceID, documentID).
			Updates(map[string]any{
				"status":     string(value.DocumentStatusFailed),
				"updated_at": now,
			})
		if documentResult.Error != nil {
			return translateDBError(documentResult.Error, "标记同步 Document 入队失败")
		}
		if documentResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		revisionResult := tx.WithContext(ctx).Model(&DocumentRevisionRow{}).
			Where("workspace_id = ? AND document_id = ? AND id = ?", workspaceID, documentID, revisionID).
			Updates(map[string]any{
				"status":        string(value.DocumentRevisionFailed),
				"error_class":   errorClass,
				"error_message": message,
				"completed_at":  now,
			})
		if revisionResult.Error != nil {
			return translateDBError(revisionResult.Error, "标记同步 DocumentRevision 入队失败")
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
				"status":        string(value.JobStatusFailed),
				"error_class":   errorClass,
				"error_message": message,
				"updated_at":    now,
			})
		if jobResult.Error != nil {
			return translateDBError(jobResult.Error, "标记同步 parse Job 入队失败")
		}
		if jobResult.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}

type sourceSyncTx struct {
	db          *gorm.DB
	workspaceID uuid.UUID
}

func (tx *sourceSyncTx) GetKnowledgeBase(ctx context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	var row KnowledgeBaseRow
	if err := tx.db.WithContext(ctx).
		First(&row, "workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, id).Error; err != nil {
		return nil, translateDBError(err, "读取知识库失败")
	}
	return knowledgeBaseV2FromRow(&row), nil
}

func (tx *sourceSyncTx) GetFileTreeNodeForUpdate(ctx context.Context, id uuid.UUID) (*model.FileTreeNode, error) {
	var row FileTreeNodeRow
	if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "workspace_id = ? AND id = ?", tx.workspaceID, id).Error; err != nil {
		return nil, translateDBError(err, "锁定文件树节点失败")
	}
	return fileTreeNodeFromRow(&row), nil
}

func (tx *sourceSyncTx) CreateFileTreeNode(ctx context.Context, node *model.FileTreeNode) error {
	if node.WorkspaceID != tx.workspaceID {
		return fmt.Errorf("%w: 来源同步 folder 节点 Workspace lineage 不一致", domainerrors.ErrValidation)
	}
	if err := tx.db.WithContext(ctx).Create(fileTreeNodeToRow(node)).Error; err != nil {
		mapped := translateDBError(err, "创建同步 folder 节点失败")
		if errors.Is(mapped, domainerrors.ErrConflict) {
			return domainerrors.ErrFileTreeNameConflict
		}
		return mapped
	}
	return nil
}

// CreateSyncedDocumentNodeRevisionAndJob 在单事务内原子写入
// document + fileTreeNode + documentRevision + job 四条记录。
func (tx *sourceSyncTx) CreateSyncedDocumentNodeRevisionAndJob(
	ctx context.Context,
	document *model.Document,
	node *model.FileTreeNode,
	revision *model.DocumentRevision,
	job *model.Job,
) error {
	if document.WorkspaceID != tx.workspaceID || node.WorkspaceID != tx.workspaceID ||
		revision.WorkspaceID != tx.workspaceID || job.WorkspaceID != tx.workspaceID {
		return fmt.Errorf("%w: 来源同步 Workspace lineage 不一致", domainerrors.ErrValidation)
	}
	revisionRow, err := documentRevisionToRow(revision)
	if err != nil {
		return err
	}
	if err := tx.db.WithContext(ctx).Create(documentV2ToRow(document)).Error; err != nil {
		return translateDBError(err, "创建同步 Document 失败")
	}
	if err := tx.db.WithContext(ctx).Create(fileTreeNodeToRow(node)).Error; err != nil {
		mapped := translateDBError(err, "创建同步 file 节点失败")
		if errors.Is(mapped, domainerrors.ErrConflict) {
			return domainerrors.ErrFileTreeNameConflict
		}
		return mapped
	}
	if err := tx.db.WithContext(ctx).Create(revisionRow).Error; err != nil {
		return translateDBError(err, "创建同步 DocumentRevision 失败")
	}
	if err := tx.db.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
		return translateDBError(err, "创建同步 parse Job 失败")
	}
	return nil
}
