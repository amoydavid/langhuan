package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
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
	// UpdateSyncCursor 写回增量同步游标 source_config.sync_cursor（RFC3339）。
	UpdateSyncCursor(ctx context.Context, workspaceID, kbID uuid.UUID, cursor time.Time) error
}

// SourceSyncTx 是来源同步在单个 Workspace 事务内的最小持久化契约。
type SourceSyncTx interface {
	GetKnowledgeBase(ctx context.Context, kbID uuid.UUID) (*model.KnowledgeBase, error)
	GetFileTreeNodeForUpdate(ctx context.Context, id uuid.UUID) (*model.FileTreeNode, error)
	// CreateFileTreeNode 写入一个 folder 节点（同步目录树）。
	CreateFileTreeNode(ctx context.Context, node *model.FileTreeNode) error
	// ListFileTreeNodes 返回该 KB 下所有 file tree 节点（含 folder/file/root），
	// 供完整 snapshot 的 folder 删除检测使用。
	ListFileTreeNodes(ctx context.Context, kbID uuid.UUID) ([]*model.FileTreeNode, error)
	// DeleteFileTreeNode 删除一个 folder 节点（仅用于完整 snapshot 删除空的失踪 folder）。
	DeleteFileTreeNode(ctx context.Context, id uuid.UUID) error
	// CreateSyncedDocumentNodeRevisionAndJob 原子写入一份同步文档：
	// document row + fileTreeNode row + documentRevision row + job row。
	CreateSyncedDocumentNodeRevisionAndJob(
		ctx context.Context,
		document *model.Document,
		node *model.FileTreeNode,
		revision *model.DocumentRevision,
		job *model.Job,
	) error
	// ListDocumentsByKB 返回该 KB 下所有 external_id 非空的文档（含已软删的），
	// 供增量同步的删除检测计算存活集合差集。
	ListDocumentsByKB(ctx context.Context, kbID uuid.UUID) ([]*model.Document, error)
	// SoftDeleteDocument 软删一个文档（仅当 deleted_at IS NULL 时生效），
	// 用于飞书侧已删除的文档在本地软删。
	SoftDeleteDocument(ctx context.Context, documentID uuid.UUID) error
}

// SyncResult 是单次同步的结果摘要，写入 source_config.sync_last_result。
// UpdateSyncResult 用 jsonb_set 仅替换 sync_last_result 这一个 key，保留其它键。
type SyncResult struct {
	Status            string    `json:"status"` // succeeded|partial|failed
	Complete          bool      `json:"complete"`
	SyncedDocuments   int       `json:"synced_documents"`
	SkippedDocuments  int       `json:"skipped_documents"`
	FailedDocuments   int       `json:"failed_documents"`
	OversizeDocuments int       `json:"oversize_documents"`
	UnsupportedNodes  int       `json:"unsupported_nodes"`
	DeletedDocuments  int       `json:"deleted_documents"`
	CleanupPending    int       `json:"cleanup_pending"`
	FinishedAt        time.Time `json:"finished_at"`
}

// UpdateDocumentRequest 描述一次稳定文档更新（spec 6.3 更新路径）的输入。
// 通过 external_id 复用既有 Document、递增 revision_no、写入新 Revision+Job。
type UpdateDocumentRequest struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	ExternalID      string
	DocumentID      uuid.UUID
	RevisionID      uuid.UUID
	Title           string
	ParentNodeID    uuid.UUID
	RawStorageKey   string
	SHA256          string
	SizeBytes       int64
	ContentType     string
	FileType        string
	Reason          value.DocumentRevisionReason
}

// RetryDocumentRequest 描述一次重试：复用最新未完成/失败的 revision，
// 不创建相同 hash 的新 revision；创建/重置幂等的 parse Job。
type RetryDocumentRequest struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	DocumentID      uuid.UUID
	RevisionID      uuid.UUID
	SHA256          string
	Title           string
	ParentNodeID    uuid.UUID
}

// SyncWriteResult 是一次稳定写入（更新/重试）返回给服务层的结果。
type SyncWriteResult struct {
	DocumentID uuid.UUID
	RevisionID uuid.UUID
	RevisionNo int64
	JobID      uuid.UUID
	RawKey     string
}

// CleanupObject 是删除 remove 策略下需要清理的外部对象 key。
// 调用方（Task 8）在事务提交后据 Store 值归类删除（Task 6 仅负责收集 + 建清理 Job）。
type CleanupObject struct {
	Key   string
	Store string // raw|parser|asset
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

	// ListSourceDocuments 返回该 KB 下所有 external_id 非空的文档（含已软删的），
	// 并聚合最新 source revision 与失败/重试信号，供 diff 计算使用。
	ListSourceDocuments(ctx context.Context, kbID uuid.UUID) ([]LocalDocView, error)
	// UpsertSourceFolder 锁定 workspace/KB/external_id 更新 parent/name；缺失则插入。
	UpsertSourceFolder(ctx context.Context, folder *model.FileTreeNode) error
	// CreateSyncedDocumentRevisionJob 在单个 workspace 事务内：锁定既有 Document
	// （按 workspace/kb/external_id FOR UPDATE），revision_no=max+1，写 Revision+Job，
	// 更新 Document content_hash/status/title，更新 FileTreeNode。
	CreateSyncedDocumentRevisionJob(ctx context.Context, request UpdateDocumentRequest) (*SyncWriteResult, error)
	// RetrySourceRevision 复用最新未完成/失败的 revision（不创建相同 hash 的新 revision），
	// 创建/重置幂等的 parse Job。
	RetrySourceRevision(ctx context.Context, request RetryDocumentRequest) (*SyncWriteResult, error)
	// DeleteSourceDocument 按策略删除文档：keep 仅软删；remove 先收集 raw/parser/asset key、
	// 建立 KB 级清理 Job（按 SourceCleanupObjectBatchSize 拆批，每批一个 Job）、再删除 Document（外键级联）。
	// 返回所有收集到的清理对象 + 在事务内创建的清理 Job（pending 状态），供调用方在提交后入队。
	DeleteSourceDocument(ctx context.Context, documentID uuid.UUID, policy value.SourceDeletePolicy) ([]CleanupObject, []*model.Job, error)
	// RequestSourceSync 在 KB 锁定事务内：latch = old OR requestedForce；
	// 存在 pending/running 的 source_sync Job 则复用（created=false），否则新建（created=true）。
	RequestSourceSync(ctx context.Context, workspaceID, kbID, connectionID uuid.UUID, requestedForce bool) (job *model.Job, created bool, err error)
	// ConsumeForceLatch 原子读取并清空 force latch，返回读到的值。
	ConsumeForceLatch(ctx context.Context, workspaceID, kbID, jobID uuid.UUID) (bool, error)
	// FinalizeSourceSyncJob 在同一个 KB 锁定事务内：标记 Job 终态，读取 latch，
	// 若为 true 则新建并返回下一个 source_sync Job（调用方入队）；否则返回 nil。
	FinalizeSourceSyncJob(ctx context.Context, workspaceID, kbID, jobID uuid.UUID, status value.JobStatus, errorMessage string) (*model.Job, error)
	// FailSourceSyncEnqueue 标记首次入队失败，但保留 force latch 供调度器恢复。
	FailSourceSyncEnqueue(ctx context.Context, workspaceID, kbID, jobID uuid.UUID, message string) error
	// UpdateSyncResult 用 jsonb_set 把 SyncResult 写入 source_config.sync_last_result，
	// 保留其它 root/cursor/cron/latch 键。
	UpdateSyncResult(ctx context.Context, workspaceID, kbID uuid.UUID, result SyncResult) error
}
