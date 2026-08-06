package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// KnowledgeBaseSyncRepository 是来源同步所需的只读知识库访问端口。
// 复用现有 KnowledgeBaseRepository 的 Get 语义，独立成接口以便测试注入 fake。
type KnowledgeBaseSyncRepository interface {
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*model.KnowledgeBase, error)
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
}
