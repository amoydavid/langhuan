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

// CountActiveByConnection 统计某 connection 下进行中的 source_sync 任务数（pending/running）。
// 供 Meta Scheduler 按应用限流使用。workspaceID/connectionID 为空时返回校验错误。
func (s *SourceSyncDBStore) CountActiveByConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) (int, error) {
	if workspaceID == uuid.Nil || connectionID == uuid.Nil {
		return 0, fmt.Errorf("%w: CountActiveByConnection workspace/connection 不能为空", domainerrors.ErrValidation)
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&JobRow{}).
		Where("workspace_id = ? AND source_connection_id = ? AND type = ? AND status IN ?",
			workspaceID, connectionID, model.SourceSyncJobType,
			[]string{string(value.JobStatusPending), string(value.JobStatusRunning)},
		).Count(&count).Error
	if err != nil {
		return 0, translateDBError(err, "统计 connection 进行中任务失败")
	}
	return int(count), nil
}

// UpdateSyncCursor 写回增量同步游标 source_config.sync_cursor（RFC3339）。
// 参照 UpdateNextSyncAt 的 jsonb_set 模式。
func (s *SourceSyncDBStore) UpdateSyncCursor(ctx context.Context, workspaceID, kbID uuid.UUID, cursor time.Time) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || cursor.IsZero() {
		return fmt.Errorf("%w: UpdateSyncCursor workspace/kb/cursor 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		execSQL := "UPDATE knowledge_bases SET source_config = jsonb_set(source_config, '{sync_cursor}', to_jsonb(?::timestamptz)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
		args := []any{cursor.UTC(), now, workspaceID, kbID}
		result := tx.WithContext(ctx).Exec(execSQL, args...)
		if result.Error != nil {
			return translateDBError(result.Error, "更新知识库 sync_cursor 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
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

// ListDocumentsByKB 返回该 KB 下所有 external_id 非空的文档（含已软删的），
// 供增量同步的删除检测计算存活集合差集。
func (tx *sourceSyncTx) ListDocumentsByKB(ctx context.Context, kbID uuid.UUID) ([]*model.Document, error) {
	var rows []DocumentRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND knowledge_base_id = ? AND external_id IS NOT NULL AND external_id <> ''",
			tx.workspaceID, kbID).
		Find(&rows).Error; err != nil {
		return nil, translateDBError(err, "读取 KB external 文档失败")
	}
	docs := make([]*model.Document, 0, len(rows))
	for i := range rows {
		docs = append(docs, documentV2FromRow(&rows[i]))
	}
	return docs, nil
}

// SoftDeleteDocument 软删一个文档（仅当 deleted_at IS NULL 时生效）。
func (tx *sourceSyncTx) SoftDeleteDocument(ctx context.Context, documentID uuid.UUID) error {
	now := time.Now().UTC()
	result := tx.db.WithContext(ctx).Model(&DocumentRow{}).
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", tx.workspaceID, documentID).
		Updates(map[string]any{
			"status":     string(value.DocumentStatusDeleted),
			"deleted_at": now,
			"updated_at": now,
		})
	if result.Error != nil {
		return translateDBError(result.Error, "软删同步文档失败")
	}
	return nil
}
