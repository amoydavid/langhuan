package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// DueKnowledgeBase 描述一个到期需要同步的飞书知识库的最小 lineage。
// 由 KnowledgeBaseSyncRepository.ListDueFeishuKBs 返回，供 Meta Scheduler 调度。
type DueKnowledgeBase struct {
	WorkspaceID        uuid.UUID
	ID                 uuid.UUID
	SourceConnectionID uuid.UUID
}

// KnowledgeBaseSyncRepository 是来源同步所需的只读知识库访问端口。
// 复用现有 KnowledgeBaseRepository 的 Get 语义，独立成接口以便测试注入 fake。
type KnowledgeBaseSyncRepository interface {
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*model.KnowledgeBase, error)
	// ListDueFeishuKBs 返回所有 source_type 为飞书且 next_sync_at <= now 的知识库。
	// when 为零值时表示不按 connection 过滤；非零值时仅返回该 connection 下的到期 KB。
	ListDueFeishuKBs(ctx context.Context, now time.Time, connectionID uuid.UUID) ([]DueKnowledgeBase, error)
	// UpdateNextSyncAt 更新某个知识库 source_config.next_sync_at；nextSyncAt 为零值时清除字段。
	UpdateNextSyncAt(ctx context.Context, workspaceID, kbID uuid.UUID, nextSyncAt time.Time) error
}

// SourceSyncTx 是来源同步在单个 Workspace 事务内的最小持久化契约。
type SourceSyncTx interface {
	GetKnowledgeBase(ctx context.Context, kbID uuid.UUID) (*model.KnowledgeBase, error)
	GetFileTreeNodeForUpdate(ctx context.Context, id uuid.UUID) (*model.FileTreeNode, error)
	// CreateFileTreeNode 写入一个 folder 节点（同步目录树）。
	CreateFileTreeNode(ctx context.Context, node *model.FileTreeNode) error
	// CreateSyncedDocumentNodeRevisionAndJob 原子写入一份同步文档：
	// document row + fileTreeNode row + documentRevision row + job row。
	CreateSyncedDocumentNodeRevisionAndJob(
		ctx context.Context,
		document *model.Document,
		node *model.FileTreeNode,
		revision *model.DocumentRevision,
		job *model.Job,
	) error
}

// SourceSyncStore 进入一个 Workspace 级别的来源同步事务。
type SourceSyncStore interface {
	WithinWorkspace(ctx context.Context, workspaceID uuid.UUID, fn func(ctx context.Context, tx SourceSyncTx) error) error
	// FailCreatedSync 在入队失败时把刚创建的 document/revision/job 标记为失败。
	FailCreatedSync(
		ctx context.Context,
		workspaceID, documentID, revisionID, jobID uuid.UUID,
		errorClass, errorMessage string,
	) error
	// CreateSourceSyncJob 持久化一个 source_sync 任务（仅关联 KB，不带 document/generation）。
	CreateSourceSyncJob(ctx context.Context, job *model.Job) error
	// CountActiveByConnection 统计某 connection 下进行中的 source_sync 任务数（pending/running）。
	// 供 Meta Scheduler 按应用限流使用。
	CountActiveByConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) (int, error)
}
