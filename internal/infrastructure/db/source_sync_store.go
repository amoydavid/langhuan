package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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

// ListFeishuKBsWithForceLatchAndNoActiveJob 列出所有 force latch 已置位
// (source_config.sync_requested_force = true) 且当前没有 pending/running
// source_sync Job 的飞书知识库（spec 8.2 latch 恢复）。
// 这些 KB 的同步因入队失败或 worker 异常退出而滞留，由 Meta Scheduler 恢复派发。
func (s *SourceSyncDBStore) ListFeishuKBsWithForceLatchAndNoActiveJob(ctx context.Context) ([]service.DueKnowledgeBase, error) {
	type dueRow struct {
		WorkspaceID        uuid.UUID `gorm:"column:workspace_id"`
		ID                 uuid.UUID `gorm:"column:id"`
		SourceConnectionID uuid.UUID `gorm:"column:source_connection_id"`
	}
	var rows []dueRow
	activeSubquery := s.db.WithContext(ctx).Table("jobs").
		Select("1").
		Where("jobs.workspace_id = knowledge_bases.workspace_id").
		Where("jobs.knowledge_base_id = knowledge_bases.id").
		Where("jobs.type = ?", model.SourceSyncJobType).
		Where("jobs.status IN ?", []string{string(value.JobStatusPending), string(value.JobStatusRunning)})
	forceLatchQuery := s.db.WithContext(ctx).Table("knowledge_bases").
		Select("workspace_id, id, source_connection_id").
		Where("deleted_at IS NULL").
		Where("source_type IN ?", []string{string(value.SourceTypeFeishuDrive), string(value.SourceTypeFeishuWiki)}).
		Where("source_connection_id IS NOT NULL")
	// force latch 存为 JSON 布尔：PG 用 ->>::boolean，SQLite 用 json_extract（返回 1/0 整数）。
	if s.db.Dialector.Name() == "sqlite" {
		forceLatchQuery = forceLatchQuery.Where("json_extract(source_config, '$.sync_requested_force') = 1")
	} else {
		forceLatchQuery = forceLatchQuery.Where("(source_config->>'sync_requested_force')::boolean = true")
	}
	err := forceLatchQuery.
		Where("NOT EXISTS (?)", activeSubquery).
		Order("workspace_id, id").
		Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "列出 latch 恢复 KB 失败")
	}
	result := make([]service.DueKnowledgeBase, 0, len(rows))
	for _, row := range rows {
		result = append(result, service.DueKnowledgeBase{
			WorkspaceID: row.WorkspaceID, ID: row.ID, SourceConnectionID: row.SourceConnectionID,
		})
	}
	return result, nil
}

// UpdateSyncCursor 写回增量同步游标 source_config.sync_cursor（RFC3339）。
// 参照 UpdateNextSyncAt 的 jsonb_set 模式。
func (s *SourceSyncDBStore) UpdateSyncCursor(ctx context.Context, workspaceID, kbID uuid.UUID, cursor time.Time) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || cursor.IsZero() {
		return fmt.Errorf("%w: UpdateSyncCursor workspace/kb/cursor 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var execSQL string
		var args []any
		if tx.Dialector.Name() == "sqlite" {
			execSQL = "UPDATE knowledge_bases SET source_config = json_set(source_config, '$.sync_cursor', ?), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{cursor.UTC().Format(time.RFC3339Nano), now, workspaceID, kbID}
		} else {
			execSQL = "UPDATE knowledge_bases SET source_config = jsonb_set(source_config, '{sync_cursor}', to_jsonb(?::timestamptz)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
			args = []any{cursor.UTC(), now, workspaceID, kbID}
		}
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

// ListFileTreeNodes 返回该 KB 下所有 file tree 节点（含 folder/file/root），
// 供完整 snapshot 的 folder 删除检测使用。
func (tx *sourceSyncTx) ListFileTreeNodes(ctx context.Context, kbID uuid.UUID) ([]*model.FileTreeNode, error) {
	var rows []FileTreeNodeRow
	if err := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND knowledge_base_id = ?", tx.workspaceID, kbID).
		Find(&rows).Error; err != nil {
		return nil, translateDBError(err, "读取 KB 文件树节点失败")
	}
	nodes := make([]*model.FileTreeNode, 0, len(rows))
	for i := range rows {
		nodes = append(nodes, fileTreeNodeFromRow(&rows[i]))
	}
	return nodes, nil
}

// DeleteFileTreeNode 删除一个 file tree 节点（仅用于完整 snapshot 删除空的失踪 folder）。
func (tx *sourceSyncTx) DeleteFileTreeNode(ctx context.Context, id uuid.UUID) error {
	result := tx.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", tx.workspaceID, id).
		Delete(&FileTreeNodeRow{})
	if result.Error != nil {
		return translateDBError(result.Error, "删除同步 folder 节点失败")
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

// ---- Task 6: 稳定 Document/Folder upsert、latch 与结果 ----
//
// 以下方法都在 SourceSyncDBStore 上实现，各自开启一个 workspace 事务（符合 spec 6.3/8.2/9.2 的
// 事务边界要求：latch 操作必须在同一个 KB 锁定事务内完成，避免并发 force 请求丢失）。
// 所有查询显式带 workspace_id + knowledge_base_id，遵守 Workspace 隔离铁律。

// sourceDocRow 是 ListSourceDocuments 的 join 投影：文档 + 最新 source revision 的序号/状态。
type sourceDocRow struct {
	DocumentID       uuid.UUID  `gorm:"column:document_id"`
	ExternalID       *string    `gorm:"column:external_id"`
	ContentHash      *string    `gorm:"column:content_hash"`
	Status           string     `gorm:"column:status"`
	ActiveRevisionID *uuid.UUID `gorm:"column:active_revision_id"`
	RevisionNo       *int64     `gorm:"column:revision_no"`
	RevisionStatus   *string    `gorm:"column:revision_status"`
	RevisionID       *uuid.UUID `gorm:"column:revision_id"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

// ListSourceDocuments 返回该 KB 下所有 external_id 非空的文档（含已软删的），
// 聚合最新 source revision（reason=crawl）的状态，并在 Go 中计算 RetryRequired。
//
// RetryRequired 为 true 的条件（任一）：文档 status=failed；最新 source revision 状态
// 不是 ready；该 revision 的 parse Job 处于 failed。
func (s *SourceSyncDBStore) ListSourceDocuments(ctx context.Context, kbID uuid.UUID) ([]service.LocalDocView, error) {
	if kbID == uuid.Nil {
		return nil, fmt.Errorf("%w: ListSourceDocuments kb 不能为空", domainerrors.ErrValidation)
	}
	// 先解析 KB 的 workspace_id（ListSourceDocuments 不带 workspaceID 参数，从 KB 反查）。
	var kb KnowledgeBaseRow
	if err := s.db.WithContext(ctx).
		Select("workspace_id").
		First(&kb, "id = ? AND deleted_at IS NULL", kbID).Error; err != nil {
		return nil, translateDBError(err, "读取知识库 workspace 失败")
	}
	workspaceID := kb.WorkspaceID

	var rows []sourceDocRow
	// 对每个文档 LEFT JOIN 其最新（按 revision_no 倒序）的 source revision（reason=crawl）。
	// 用 DISTINCT ON 取每个 document 的最新 revision；非来源同步文档（无 crawl revision）也保留。
	err := s.db.WithContext(ctx).Raw(`
SELECT d.id AS document_id, d.external_id, d.content_hash, d.status,
       d.active_revision_id, d.deleted_at,
       r.id AS revision_id, r.revision_no, r.status AS revision_status
FROM documents d
LEFT JOIN LATERAL (
    SELECT id, revision_no, status
    FROM document_revisions
    WHERE workspace_id = d.workspace_id
      AND document_id = d.id
      AND revision_reason = ?
    ORDER BY revision_no DESC
    LIMIT 1
) r ON true
WHERE d.workspace_id = ? AND d.knowledge_base_id = ?
  AND d.external_id IS NOT NULL AND d.external_id <> ''
ORDER BY d.id
`, string(value.DocumentRevisionReasonCrawl), workspaceID, kbID).Scan(&rows).Error
	if err != nil {
		return nil, translateDBError(err, "读取来源同步文档投影失败")
	}

	if len(rows) == 0 {
		return []service.LocalDocView{}, nil
	}

	// 收集需要检查 parse Job 失败状态的 revision id。
	revisionIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.RevisionID != nil {
			revisionIDs = append(revisionIDs, *r.RevisionID)
		}
	}
	failedJobRevisions := make(map[uuid.UUID]bool)
	if len(revisionIDs) > 0 {
		type jobFlag struct {
			DocumentRevisionID *uuid.UUID `gorm:"column:document_revision_id"`
		}
		var flags []jobFlag
		err = s.db.WithContext(ctx).Raw(`
SELECT DISTINCT document_revision_id
FROM jobs
WHERE workspace_id = ?
  AND document_revision_id IN ?
  AND status = ?
`, workspaceID, revisionIDs, string(value.JobStatusFailed)).Scan(&flags).Error
		if err != nil {
			return nil, translateDBError(err, "读取来源同步 parse Job 失败状态失败")
		}
		for _, f := range flags {
			if f.DocumentRevisionID != nil {
				failedJobRevisions[*f.DocumentRevisionID] = true
			}
		}
	}

	views := make([]service.LocalDocView, 0, len(rows))
	for _, r := range rows {
		view := service.LocalDocView{
			DocumentID:       r.DocumentID,
			ExternalID:       dereferenceString(r.ExternalID),
			ContentHash:      dereferenceString(r.ContentHash),
			Status:           value.DocumentStatus(r.Status),
			ActiveRevisionID: r.ActiveRevisionID,
			LatestRevisionID: r.RevisionID,
			DeletedAt:        r.DeletedAt,
		}
		if r.RevisionID != nil {
			view.RevisionNo = dereferenceInt64(r.RevisionNo)
		}
		view.RetryRequired = computeRetryRequired(r, failedJobRevisions)
		views = append(views, view)
	}
	return views, nil
}

// computeRetryRequired 在 Go 中根据文档状态、最新 revision 状态、parse Job 失败标志计算重试需求。
func computeRetryRequired(r sourceDocRow, failedJobRevisions map[uuid.UUID]bool) bool {
	if r.Status == string(value.DocumentStatusFailed) {
		return true
	}
	if r.RevisionID == nil {
		// 没有来源 revision（理论不应出现，因为 external_id 非空），保守返回 false。
		return false
	}
	if r.RevisionStatus == nil {
		return true
	}
	revStatus := value.DocumentRevisionStatus(*r.RevisionStatus)
	if revStatus != value.DocumentRevisionReady {
		// 最新 source revision 未完成（pending/parsing/failed）=> 需要重试。
		return true
	}
	return failedJobRevisions[*r.RevisionID]
}

func dereferenceInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// UpsertSourceFolder 锁定 workspace/KB/external_id 的 folder 节点并更新 parent/name；
// 不存在则插入。external_id 为空时直接返回校验错误（root 节点不参与 upsert）。
func (s *SourceSyncDBStore) UpsertSourceFolder(ctx context.Context, folder *model.FileTreeNode) error {
	if folder == nil {
		return fmt.Errorf("%w: UpsertSourceFolder folder 不能为空", domainerrors.ErrValidation)
	}
	if folder.WorkspaceID == uuid.Nil || folder.KnowledgeBaseID == uuid.Nil {
		return fmt.Errorf("%w: UpsertSourceFolder lineage 不能为空", domainerrors.ErrValidation)
	}
	externalID := strings.TrimSpace(folder.ExternalID)
	if externalID == "" {
		return fmt.Errorf("%w: UpsertSourceFolder external_id 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, folder.WorkspaceID, func(tx *gorm.DB) error {
		// 锁定既有 folder 节点（FOR UPDATE）。external_id 有唯一部分索引，至多一行。
		var existing FileTreeNodeRow
		err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND external_id = ?",
				folder.WorkspaceID, folder.KnowledgeBaseID, externalID).
			First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return translateDBError(err, "锁定同步 folder 失败")
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 插入新 folder 节点。
			if err := tx.WithContext(ctx).Create(fileTreeNodeToRow(folder)).Error; err != nil {
				mapped := translateDBError(err, "创建同步 folder 失败")
				if errors.Is(mapped, domainerrors.ErrConflict) {
					return domainerrors.ErrFileTreeNameConflict
				}
				return mapped
			}
			return nil
		}
		// 更新 parent/name/external_id（保留既有 id 与 document 关联）。
		now := time.Now().UTC()
		result := tx.WithContext(ctx).Model(&FileTreeNodeRow{}).
			Where("workspace_id = ? AND id = ?", folder.WorkspaceID, existing.ID).
			Updates(map[string]any{
				"parent_id":  folder.ParentID,
				"name":       folder.Name,
				"updated_at": now,
			})
		if result.Error != nil {
			mapped := translateDBError(result.Error, "更新同步 folder 失败")
			if errors.Is(mapped, domainerrors.ErrConflict) {
				return domainerrors.ErrFileTreeNameConflict
			}
			return mapped
		}
		return nil
	})
}

// CreateSyncedDocumentRevisionJob 是 spec 6.3 的更新路径：在单个 workspace 事务内锁定
// 既有 Document（按 workspace/kb/external_id FOR UPDATE），revision_no=max+1，
// 创建 Revision+Job，更新 Document content_hash/status/title，更新 FileTreeNode。
// 请求中的 RevisionID 是服务层预分配的稳定 id；Job id 由本方法生成。
func (s *SourceSyncDBStore) CreateSyncedDocumentRevisionJob(
	ctx context.Context, request service.UpdateDocumentRequest,
) (*service.SyncWriteResult, error) {
	if request.WorkspaceID == uuid.Nil || request.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: 更新同步文档 lineage 不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(request.ExternalID) == "" || request.DocumentID == uuid.Nil || request.RevisionID == uuid.Nil {
		return nil, fmt.Errorf("%w: 更新同步文档 external/document/revision 不能为空", domainerrors.ErrValidation)
	}
	if request.Reason == "" {
		return nil, fmt.Errorf("%w: 更新同步文档 revision reason 不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(request.Title) == "" || strings.TrimSpace(request.RawStorageKey) == "" {
		return nil, fmt.Errorf("%w: 更新同步文档 title/raw_key 不能为空", domainerrors.ErrValidation)
	}

	var result *service.SyncWriteResult
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		// 锁定既有 Document（FOR UPDATE）。
		var docRow DocumentRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND external_id = ?",
				request.WorkspaceID, request.KnowledgeBaseID, request.ExternalID).
			First(&docRow).Error; err != nil {
			return translateDBError(err, "锁定同步 Document 失败")
		}
		if docRow.ID != request.DocumentID {
			return fmt.Errorf("%w: 更新同步文档 external_id 对应 Document 与请求 DocumentID 不一致", domainerrors.ErrConflict)
		}

		// revision_no = max(document_revisions.revision_no) + 1。
		var maxNo int64
		if err := tx.WithContext(ctx).Model(&DocumentRevisionRow{}).
			Where("workspace_id = ? AND document_id = ?", request.WorkspaceID, request.DocumentID).
			Select("COALESCE(MAX(revision_no), 0)").Scan(&maxNo).Error; err != nil {
			return translateDBError(err, "读取最大 revision_no 失败")
		}
		revisionNo := maxNo + 1

		// 构造领域 Revision（带预分配 id）。
		revision, err := model.NewDocumentRevisionWithID(request.RevisionID, model.NewDocumentRevisionInput{
			WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
			DocumentID: request.DocumentID, Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
			RevisionNo: revisionNo, Reason: request.Reason,
			OriginalFilename: request.Title, FileType: request.FileType, ContentType: request.ContentType,
			RawStorageKey: request.RawStorageKey, SHA256: request.SHA256, SizeBytes: request.SizeBytes,
			ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
		})
		if err != nil {
			return err
		}
		revisionRow, err := documentRevisionToRow(revision)
		if err != nil {
			return err
		}

		// 构造幂等的 parse Job（document_parse_start）。
		job, err := model.NewJob(model.NewJobInput{
			WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
			DocumentID: request.DocumentID, DocumentRevisionID: request.RevisionID,
			Type: "document_parse_start", Status: value.JobStatusPending,
		})
		if err != nil {
			return err
		}

		// 写入 Revision + Job。
		if err := tx.WithContext(ctx).Create(revisionRow).Error; err != nil {
			return translateDBError(err, "创建更新 DocumentRevision 失败")
		}
		if err := tx.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
			return translateDBError(err, "创建更新 parse Job 失败")
		}

		// 更新 Document content_hash/status/title（active_revision_id 不在此切换，pipeline 发布时改）。
		now := time.Now().UTC()
		docUpdate := tx.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.DocumentID).
			Updates(map[string]any{
				"title":        request.Title,
				"content_hash": nullableString(request.SHA256),
				"status":       string(value.DocumentStatusPending),
				"updated_at":   now,
				"deleted_at":   nil, // 远端重新出现的已软删文档：复活。
			})
		if docUpdate.Error != nil {
			return translateDBError(docUpdate.Error, "更新同步 Document 失败")
		}
		if docUpdate.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}

		// 更新 FileTreeNode（按 document_id 定位 file 节点）的 parent/name。
		nodeUpdate := tx.WithContext(ctx).Model(&FileTreeNodeRow{}).
			Where("workspace_id = ? AND document_id = ?", request.WorkspaceID, request.DocumentID).
			Updates(map[string]any{
				"parent_id":  nullableUUID(request.ParentNodeID),
				"name":       request.Title,
				"updated_at": now,
			})
		if nodeUpdate.Error != nil {
			mapped := translateDBError(nodeUpdate.Error, "更新同步 FileTreeNode 失败")
			if errors.Is(mapped, domainerrors.ErrConflict) {
				return domainerrors.ErrFileTreeNameConflict
			}
			return mapped
		}

		result = &service.SyncWriteResult{
			DocumentID: request.DocumentID,
			RevisionID: request.RevisionID,
			RevisionNo: revisionNo,
			JobID:      job.ID,
			RawKey:     request.RawStorageKey,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RetrySourceRevision 复用最新未完成/失败的 revision（不创建相同 hash 的新 revision），
// 把该 revision 重置为 pending 并清空错误，创建/重置幂等的 parse Job。
// 服务层在调用前应保证 request.RevisionID 是该 Document 的最新 source revision。
func (s *SourceSyncDBStore) RetrySourceRevision(
	ctx context.Context, request service.RetryDocumentRequest,
) (*service.SyncWriteResult, error) {
	if request.WorkspaceID == uuid.Nil || request.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: 重试同步文档 lineage 不能为空", domainerrors.ErrValidation)
	}
	if request.DocumentID == uuid.Nil || request.RevisionID == uuid.Nil {
		return nil, fmt.Errorf("%w: 重试同步文档 document/revision 不能为空", domainerrors.ErrValidation)
	}

	var result *service.SyncWriteResult
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, request.WorkspaceID, func(tx *gorm.DB) error {
		// 锁定 Document（FOR UPDATE）。
		var docRow DocumentRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.DocumentID).
			First(&docRow).Error; err != nil {
			return translateDBError(err, "锁定重试 Document 失败")
		}

		// 锁定目标 revision 并读取 revision_no（不新建 revision）。
		var revRow DocumentRevisionRow
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("workspace_id = ? AND document_id = ? AND id = ?",
				request.WorkspaceID, request.DocumentID, request.RevisionID).
			First(&revRow).Error; err != nil {
			return translateDBError(err, "锁定重试 DocumentRevision 失败")
		}

		// 若传入了新的 SHA256，刷新 revision 的 hash 与原始键（幂等重抓后内容可能变化）。
		now := time.Now().UTC()
		revisionUpdates := map[string]any{
			"status":        string(value.DocumentRevisionPending),
			"error_class":   "",
			"error_message": "",
			"completed_at":  nil,
		}
		if strings.TrimSpace(request.SHA256) != "" {
			revisionUpdates["sha256"] = nullableString(request.SHA256)
		}
		revUpdate := tx.WithContext(ctx).Model(&DocumentRevisionRow{}).
			Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.RevisionID).
			Updates(revisionUpdates)
		if revUpdate.Error != nil {
			return translateDBError(revUpdate.Error, "重置重试 DocumentRevision 失败")
		}
		if revUpdate.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}

		// 幂等 parse Job：把该 revision 既有未终态的 parse Job 重置；若无则新建。
		var existingJob JobRow
		findErr := tx.WithContext(ctx).
			Where("workspace_id = ? AND document_id = ? AND document_revision_id = ? AND type = ?",
				request.WorkspaceID, request.DocumentID, request.RevisionID, "document_parse_start").
			Order("updated_at DESC, id DESC").
			First(&existingJob).Error
		var jobID uuid.UUID
		if findErr == nil {
			// 重置既有 Job 为 pending。
			jobReset := tx.WithContext(ctx).Model(&JobRow{}).
				Where("workspace_id = ? AND id = ?", request.WorkspaceID, existingJob.ID).
				Updates(map[string]any{
					"status":        string(value.JobStatusPending),
					"error_class":   "",
					"error_message": "",
					"updated_at":    now,
				})
			if jobReset.Error != nil {
				return translateDBError(jobReset.Error, "重置重试 parse Job 失败")
			}
			jobID = existingJob.ID
		} else if errors.Is(findErr, gorm.ErrRecordNotFound) {
			// 新建幂等 parse Job。
			job, err := model.NewJob(model.NewJobInput{
				WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
				DocumentID: request.DocumentID, DocumentRevisionID: request.RevisionID,
				Type: "document_parse_start", Status: value.JobStatusPending,
			})
			if err != nil {
				return err
			}
			if err := tx.WithContext(ctx).Create(jobV2ToRow(job)).Error; err != nil {
				return translateDBError(err, "创建重试 parse Job 失败")
			}
			jobID = job.ID
		} else {
			return translateDBError(findErr, "查找重试 parse Job 失败")
		}

		// 复活已软删文档，并把状态置回 pending 以重新解析。
		docUpdate := tx.WithContext(ctx).Model(&DocumentRow{}).
			Where("workspace_id = ? AND id = ?", request.WorkspaceID, request.DocumentID).
			Updates(map[string]any{
				"status":     string(value.DocumentStatusPending),
				"title":      request.Title,
				"updated_at": now,
				"deleted_at": nil,
			})
		if docUpdate.Error != nil {
			return translateDBError(docUpdate.Error, "重置重试 Document 状态失败")
		}
		if docUpdate.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}

		// 更新 FileTreeNode（按 document_id 定位 file 节点）的 parent/name（spec 6.3 步骤 6，
		// 与 CreateSyncedDocumentRevisionJob 保持一致）。
		nodeUpdate := tx.WithContext(ctx).Model(&FileTreeNodeRow{}).
			Where("workspace_id = ? AND document_id = ?", request.WorkspaceID, request.DocumentID).
			Updates(map[string]any{
				"parent_id":  nullableUUID(request.ParentNodeID),
				"name":       request.Title,
				"updated_at": now,
			})
		if nodeUpdate.Error != nil {
			mapped := translateDBError(nodeUpdate.Error, "更新重试 FileTreeNode 失败")
			if errors.Is(mapped, domainerrors.ErrConflict) {
				return domainerrors.ErrFileTreeNameConflict
			}
			return mapped
		}

		result = &service.SyncWriteResult{
			DocumentID: request.DocumentID,
			RevisionID: request.RevisionID,
			RevisionNo: revRow.RevisionNo,
			JobID:      jobID,
			RawKey:     dereferenceString(revRow.RawStorageKey),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteSourceDocument 按策略删除文档（spec 9.2）。
//   - keep：软删（status=deleted, deleted_at=now），保留原始对象。
//   - remove：先收集 raw/parser/asset key，按 service.SourceCleanupObjectBatchSize 拆批
//     建立 KB 级 source_cleanup Job（每批 payload 携带该批 key 列表），
//     再硬删 Document（FK 级联清理 revisions/assets/file_tree/jobs）。
//
// 返回所有收集到的清理对象 + 在事务内创建的清理 Job（pending），供调用方在提交后入队。
// keep 策略返回空对象切片与空 Job 切片。
func (s *SourceSyncDBStore) DeleteSourceDocument(
	ctx context.Context, workspaceID, documentID uuid.UUID, policy value.SourceDeletePolicy,
) ([]service.CleanupObject, []*model.Job, error) {
	if workspaceID == uuid.Nil || documentID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: DeleteSourceDocument workspace/document 不能为空", domainerrors.ErrValidation)
	}
	if !policy.IsValid() {
		return nil, nil, fmt.Errorf("%w: DeleteSourceDocument policy 非法", domainerrors.ErrValidation)
	}

	var collected []service.CleanupObject
	var createdJobs []*model.Job
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 读取 Document（显式 workspace 隔离，AGENTS 5.6）。
		var docRow DocumentRow
		if err := tx.WithContext(ctx).
			First(&docRow, "workspace_id = ? AND id = ?", workspaceID, documentID).Error; err != nil {
			return translateDBError(err, "读取待删除 Document 失败")
		}
		if err := tx.WithContext(ctx).Exec(
			"SELECT set_config('app.workspace_id', ?, true)", docRow.WorkspaceID.String(),
		).Error; err != nil {
			return fmt.Errorf("设置 Workspace 数据库上下文失败: %w", err)
		}
		wctx := tx.WithContext(ctx)

		if policy == value.SourceDeleteKeep {
			now := time.Now().UTC()
			result := wctx.Model(&DocumentRow{}).
				Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", docRow.WorkspaceID, documentID).
				Updates(map[string]any{
					"status":     string(value.DocumentStatusDeleted),
					"deleted_at": now,
					"updated_at": now,
				})
			if result.Error != nil {
				return translateDBError(result.Error, "软删同步 Document 失败")
			}
			return nil
		}

		// remove：先收集所有需要清理的 key。
		objects, err := collectDocumentCleanupObjects(wctx, docRow.WorkspaceID, documentID)
		if err != nil {
			return err
		}
		collected = objects

		// 按 service.SourceCleanupObjectBatchSize 拆批建立 KB 级 source_cleanup Job，
		// 每个 Job 的 payload 只携带一个批次的对象 key（无 document FK）。
		jobs, err := createSourceCleanupJobs(wctx, docRow.WorkspaceID, docRow.KnowledgeBaseID, objects)
		if err != nil {
			return err
		}
		createdJobs = jobs

		// 硬删 Document（FK 级联清理 revisions/assets/file_tree/parse jobs）。
		result := wctx.Where("workspace_id = ? AND id = ?", docRow.WorkspaceID, documentID).
			Delete(&DocumentRow{})
		if result.Error != nil {
			return translateDBError(result.Error, "硬删同步 Document 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return collected, createdJobs, nil
}

// createSourceCleanupJobs 按稳定 key 顺序拆批创建 KB 级 source_cleanup Job。
// 空对象列表返回空切片（不创建任何 Job）。每个 Job 的 payload 形如
// {"objects": [{"key": "...", "store": "raw|parser|asset"}, ...]}，仅含该批 key。
func createSourceCleanupJobs(tx *gorm.DB, workspaceID, kbID uuid.UUID, objects []service.CleanupObject) ([]*model.Job, error) {
	if len(objects) == 0 {
		return nil, nil
	}
	// 按 key 稳定排序，保证批次切分可复现（避免随机顺序导致重复 Job）。
	sorted := make([]service.CleanupObject, len(objects))
	copy(sorted, objects)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	batchSize := service.SourceCleanupObjectBatchSize
	if batchSize <= 0 {
		batchSize = len(sorted)
	}
	jobs := make([]*model.Job, 0, (len(sorted)+batchSize-1)/batchSize)
	for start := 0; start < len(sorted); start += batchSize {
		end := start + batchSize
		if end > len(sorted) {
			end = len(sorted)
		}
		batch := sorted[start:end]
		payload := map[string]any{
			"objects": cleanupObjectsToPayload(batch),
		}
		job, err := model.NewJob(model.NewJobInput{
			WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
			Type: model.SourceCleanupJobType, Status: value.JobStatusPending, Payload: payload,
		})
		if err != nil {
			return nil, err
		}
		if err := tx.Create(jobV2ToRow(job)).Error; err != nil {
			return nil, translateDBError(err, "创建 source_cleanup Job 失败")
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// collectDocumentCleanupObjects 收集 Document 的 raw/parser/asset key（在硬删前调用）。
func collectDocumentCleanupObjects(tx *gorm.DB, workspaceID, documentID uuid.UUID) ([]service.CleanupObject, error) {
	var objects []service.CleanupObject
	// raw + parser key 来自 document_revisions。
	var revKeys []struct {
		RawStorageKey        *string `gorm:"column:raw_storage_key"`
		ParserRawMarkdownKey *string `gorm:"column:parser_raw_markdown_key"`
	}
	if err := tx.Model(&DocumentRevisionRow{}).
		Select("raw_storage_key, parser_raw_markdown_key").
		Where("workspace_id = ? AND document_id = ?", workspaceID, documentID).
		Scan(&revKeys).Error; err != nil {
		return nil, translateDBError(err, "收集 raw/parser 清理 key 失败")
	}
	for _, r := range revKeys {
		if key := dereferenceString(r.RawStorageKey); key != "" {
			objects = append(objects, service.CleanupObject{Key: key, Store: "raw"})
		}
		if key := dereferenceString(r.ParserRawMarkdownKey); key != "" {
			objects = append(objects, service.CleanupObject{Key: key, Store: "parser"})
		}
	}
	// asset key 来自 document_assets。
	var assetKeys []string
	if err := tx.Model(&DocumentAssetRow{}).
		Select("storage_key").
		Where("workspace_id = ? AND document_id = ?", workspaceID, documentID).
		Scan(&assetKeys).Error; err != nil {
		return nil, translateDBError(err, "收集 asset 清理 key 失败")
	}
	for _, key := range assetKeys {
		if strings.TrimSpace(key) != "" {
			objects = append(objects, service.CleanupObject{Key: key, Store: "asset"})
		}
	}
	return objects, nil
}

// cleanupObjectsToPayload 把 CleanupObject 列表序列化为 Job payload 中的结构。
func cleanupObjectsToPayload(objects []service.CleanupObject) []map[string]any {
	result := make([]map[string]any, 0, len(objects))
	for _, o := range objects {
		result = append(result, map[string]any{"key": o.Key, "store": o.Store})
	}
	return result
}

// ---- force latch（spec 8.2）----
//
// latch 存储在 source_config.sync_requested_force（boolean JSONB key，默认 false）。
// 所有 latch 操作都在同一个 KB 锁定事务内完成：SELECT knowledge_bases ... FOR UPDATE，
// 再用 jsonb_set 读写 latch，避免并发 force 请求在 "检查 latch" 与 "标记完成" 之间丢失。

// lockKBForUpdate 锁定 KB 行（FOR UPDATE），返回当前 source_config。KB 不存在或已软删返回 ErrNotFound。
func lockKBForUpdate(ctx context.Context, tx *gorm.DB, workspaceID, kbID uuid.UUID) (JSONMap, error) {
	var row KnowledgeBaseRow
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("source_config").
		Where("workspace_id = ? AND id = ? AND deleted_at IS NULL", workspaceID, kbID).
		First(&row).Error
	if err != nil {
		return nil, translateDBError(err, "锁定知识库失败")
	}
	if row.SourceConfig == nil {
		return JSONMap{}, nil
	}
	return row.SourceConfig, nil
}

// readForceLatch 从 source_config 读取 sync_requested_force，缺失/非布尔视为 false。
func readForceLatch(config JSONMap) bool {
	v, _ := config["sync_requested_force"].(bool)
	return v
}

// setForceLatchTx 用 jsonb_set/json_set 把 sync_requested_force 写为 requestedForce。
func setForceLatchTx(ctx context.Context, tx *gorm.DB, workspaceID, kbID uuid.UUID, requestedForce bool, now time.Time) error {
	var execSQL string
	if tx.Dialector.Name() == "sqlite" {
		// 用 json(?) 把 "true"/"false" 解析为 JSON 布尔，确保回读时 JSONMap 得到 bool
		// （否则绑定整数会被存为 1/0，readForceLatch 的 .(bool) 断言失败）。
		execSQL = "UPDATE knowledge_bases SET source_config = json_set(source_config, '$.sync_requested_force', json(?)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
	} else {
		execSQL = "UPDATE knowledge_bases SET source_config = jsonb_set(source_config, '{sync_requested_force}', to_jsonb(?::boolean)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
	}
	result := tx.WithContext(ctx).Exec(execSQL, strconv.FormatBool(requestedForce), now, workspaceID, kbID)
	if result.Error != nil {
		return translateDBError(result.Error, "更新 force latch 失败")
	}
	if result.RowsAffected != 1 {
		return domainerrors.ErrNotFound
	}
	return nil
}

// RequestSourceSync 在 KB 锁定事务内：latch = old OR requestedForce；
// 存在 pending/running 的 source_sync Job 则复用（created=false），否则新建（created=true）。
func (s *SourceSyncDBStore) RequestSourceSync(
	ctx context.Context, workspaceID, kbID, connectionID uuid.UUID, requestedForce bool,
) (*model.Job, bool, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return nil, false, fmt.Errorf("%w: RequestSourceSync lineage 不能为空", domainerrors.ErrValidation)
	}
	var (
		job     *model.Job
		created bool
	)
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		config, err := lockKBForUpdate(ctx, tx, workspaceID, kbID)
		if err != nil {
			return err
		}
		// latch = old OR requestedForce。
		newLatch := readForceLatch(config) || requestedForce
		if err := setForceLatchTx(ctx, tx, workspaceID, kbID, newLatch, time.Now().UTC()); err != nil {
			return err
		}

		// 复用进行中的 source_sync Job（pending/running）。
		var existing JobRow
		findErr := tx.WithContext(ctx).
			Where("workspace_id = ? AND knowledge_base_id = ? AND type = ? AND status IN ?",
				workspaceID, kbID, model.SourceSyncJobType,
				[]string{string(value.JobStatusPending), string(value.JobStatusRunning)}).
			Order("created_at DESC, id DESC").
			First(&existing).Error
		if findErr == nil {
			job = jobV2FromRow(&existing)
			created = false
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return translateDBError(findErr, "查找进行中 source_sync Job 失败")
		}

		// 新建 source_sync Job。
		newJob, err := model.NewJob(model.NewJobInput{
			WorkspaceID: workspaceID, KnowledgeBaseID: kbID, SourceConnectionID: connectionID,
			Type: model.SourceSyncJobType, Status: value.JobStatusPending,
		})
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(jobV2ToRow(newJob)).Error; err != nil {
			return translateDBError(err, "创建 source_sync Job 失败")
		}
		job = newJob
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return job, created, nil
}

// ConsumeForceLatch 原子读取并清空 force latch，返回读到的值。
// jobID 用于 lineage 校验（保证 latch 属于该 Job 所属的 KB）。
func (s *SourceSyncDBStore) ConsumeForceLatch(
	ctx context.Context, workspaceID, kbID, jobID uuid.UUID,
) (bool, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || jobID == uuid.Nil {
		return false, fmt.Errorf("%w: ConsumeForceLatch lineage/job 不能为空", domainerrors.ErrValidation)
	}
	var consumed bool
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		config, err := lockKBForUpdate(ctx, tx, workspaceID, kbID)
		if err != nil {
			return err
		}
		consumed = readForceLatch(config)
		// 清空 latch。
		return setForceLatchTx(ctx, tx, workspaceID, kbID, false, time.Now().UTC())
	})
	if err != nil {
		return false, err
	}
	return consumed, nil
}

// FinalizeSourceSyncJob 在同一个 KB 锁定事务内：标记 Job 终态，
// 读取 latch；若为 true 则新建并返回下一个 source_sync Job（调用方入队）。
func (s *SourceSyncDBStore) FinalizeSourceSyncJob(
	ctx context.Context, workspaceID, kbID, jobID uuid.UUID, status value.JobStatus, errorMessage string,
) (*model.Job, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || jobID == uuid.Nil {
		return nil, fmt.Errorf("%w: FinalizeSourceSyncJob lineage/job 不能为空", domainerrors.ErrValidation)
	}
	if status == "" {
		return nil, fmt.Errorf("%w: FinalizeSourceSyncJob status 不能为空", domainerrors.ErrValidation)
	}
	var next *model.Job
	err := NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		// 先锁定 KB（保证 latch 检查与 Job 完成在同一事务）。
		if _, err := lockKBForUpdate(ctx, tx, workspaceID, kbID); err != nil {
			return err
		}

		// 标记当前 Job 终态（幂等：仅当 Job 仍处于 pending/running 时才转换，
		// 避免 asynq 重试已终结的 Job 时重复执行同步并创建重复的后续 Job）。
		now := time.Now().UTC()
		updates := map[string]any{
			"status":     string(status),
			"updated_at": now,
		}
		if strings.TrimSpace(errorMessage) != "" {
			updates["error_message"] = errorMessage
		}
		result := tx.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND id = ? AND type = ? AND status IN ?",
				workspaceID, kbID, jobID, model.SourceSyncJobType,
				[]string{string(value.JobStatusPending), string(value.JobStatusRunning)}).
			Updates(updates)
		if result.Error != nil {
			return translateDBError(result.Error, "标记 source_sync Job 终态失败")
		}
		if result.RowsAffected == 0 {
			// Job 已是终态（completed/failed）或不存在：视为已完成，不再创建后续 Job。
			next = nil
			return nil
		}

		// 读取 latch（KB 行仍被本事务锁定）。
		var row KnowledgeBaseRow
		if err := tx.WithContext(ctx).
			Select("source_config").
			Where("workspace_id = ? AND id = ?", workspaceID, kbID).
			First(&row).Error; err != nil {
			return translateDBError(err, "读取 source_sync latch 失败")
		}
		config := row.SourceConfig
		if config == nil {
			config = JSONMap{}
		}
		if !readForceLatch(config) {
			return nil
		}
		// latch 为 true：清空并新建下一个 source_sync Job。
		if err := setForceLatchTx(ctx, tx, workspaceID, kbID, false, now); err != nil {
			return err
		}
		newJob, err := model.NewJob(model.NewJobInput{
			WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
			Type: model.SourceSyncJobType, Status: value.JobStatusPending,
		})
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Create(jobV2ToRow(newJob)).Error; err != nil {
			return translateDBError(err, "创建下一个 source_sync Job 失败")
		}
		next = newJob
		return nil
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

// FailSourceSyncEnqueue 标记首次入队失败，但保留 force latch 供调度器恢复。
func (s *SourceSyncDBStore) FailSourceSyncEnqueue(
	ctx context.Context, workspaceID, kbID, jobID uuid.UUID, message string,
) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || jobID == uuid.Nil {
		return fmt.Errorf("%w: FailSourceSyncEnqueue lineage/job 不能为空", domainerrors.ErrValidation)
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("%w: FailSourceSyncEnqueue message 不能为空", domainerrors.ErrValidation)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.WithContext(ctx).Model(&JobRow{}).
			Where("workspace_id = ? AND knowledge_base_id = ? AND id = ? AND type = ?",
				workspaceID, kbID, jobID, model.SourceSyncJobType).
			Updates(map[string]any{
				"status":        string(value.JobStatusFailed),
				"error_class":   "enqueue_failed",
				"error_message": message,
				"updated_at":    now,
			})
		if result.Error != nil {
			return translateDBError(result.Error, "标记 source_sync 入队失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		// 不动 latch：调度器恢复时仍能感知到未消费的 force 请求。
		return nil
	})
}

// UpdateSyncResult 用 jsonb_set 把 SyncResult 写入 source_config.sync_last_result，
// 保留其它 root/cursor/cron/latch 键。
func (s *SourceSyncDBStore) UpdateSyncResult(
	ctx context.Context, workspaceID, kbID uuid.UUID, result service.SyncResult,
) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return fmt.Errorf("%w: UpdateSyncResult lineage 不能为空", domainerrors.ErrValidation)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("编码 SyncResult 失败: %w", err)
	}
	return NewWorkspaceTxRunner(s.db).WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var execSQL string
		if tx.Dialector.Name() == "sqlite" {
			// json(?) 把 JSON 文本解析为 JSON 对象后写入，回读时 JSONMap 正常解码。
			execSQL = "UPDATE knowledge_bases SET source_config = json_set(COALESCE(source_config, '{}'), '$.sync_last_result', json(?)), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
		} else {
			// jsonb_set(source_config, '{sync_last_result}', ?::jsonb)：第二个参数直接传入 JSON 文本，
			// 由 PG 解析为 jsonb；new_value_for_null_missing=true 保证 source_config 为 NULL 时也能写入。
			execSQL = "UPDATE knowledge_bases SET source_config = jsonb_set(COALESCE(source_config, '{}'::jsonb), '{sync_last_result}', ?::jsonb, true), updated_at = ? WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL"
		}
		result := tx.WithContext(ctx).Exec(execSQL, string(payload), now, workspaceID, kbID)
		if result.Error != nil {
			return translateDBError(result.Error, "更新 sync_last_result 失败")
		}
		if result.RowsAffected != 1 {
			return domainerrors.ErrNotFound
		}
		return nil
	})
}
