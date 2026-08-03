package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
)

// KnowledgeBaseCreateTx is the minimal atomic write used to create a KnowledgeBase.
type KnowledgeBaseCreateTx interface {
	CreateKnowledgeBaseRootAndGeneration(
		context.Context,
		*model.KnowledgeBase,
		*model.FileTreeNode,
		*model.IndexGeneration,
	) error
	// CreateKnowledgeBaseRootGenerationAndBinding 在创建知识库初始状态的同时，
	// 把新知识库原子加入调用 API Key 的绑定集合；bindAPIKeyID 为 nil 时等价于
	// 不写绑定。
	CreateKnowledgeBaseRootGenerationAndBinding(
		context.Context,
		*model.KnowledgeBase,
		*model.FileTreeNode,
		*model.IndexGeneration,
		*uuid.UUID,
	) error
}

// KnowledgeBaseCreateStore enters a tenant-local KnowledgeBase creation transaction.
type KnowledgeBaseCreateStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, KnowledgeBaseCreateTx) error) error
}

// DocumentIngestTx is the minimal File-ingest persistence contract.
type DocumentIngestTx interface {
	GetKnowledgeBase(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
	FindReusableRevision(
		context.Context,
		uuid.UUID,
		string,
		int,
	) (*model.Document, *model.DocumentRevision, *model.Job, error)
	GetFileTreeNodeForUpdate(context.Context, uuid.UUID) (*model.FileTreeNode, error)
	CreateFileDocumentNodeRevisionAndJob(
		context.Context,
		*model.Document,
		*model.FileTreeNode,
		*model.DocumentRevision,
		*model.Job,
	) error
}

// DocumentIngestStore enters a tenant-local File-ingest transaction.
type DocumentIngestStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, DocumentIngestTx) error) error
	FailCreatedIngest(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		string,
		string,
	) error
}

// FileTreeTx owns tree-local reads, locks and atomic node/Document title writes.
type FileTreeTx interface {
	ListFileTreeNodes(context.Context, uuid.UUID) ([]*model.FileTreeNode, error)
	GetFileTreeNodeForUpdate(context.Context, uuid.UUID) (*model.FileTreeNode, error)
	GetDocumentForUpdate(context.Context, uuid.UUID) (*model.Document, error)
	WouldCreateCycle(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	HasFileTreeChildren(context.Context, uuid.UUID) (bool, error)
	CreateFileTreeNode(context.Context, *model.FileTreeNode) error
	SaveFileTreeNode(context.Context, *model.FileTreeNode, *model.Document) error
	DeleteFileTreeNode(context.Context, uuid.UUID) error
}

// FileTreeStore enters a tenant-local File-tree transaction.
type FileTreeStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, FileTreeTx) error) error
}

// FAQRevisionTx owns one complete FAQ aggregate write under a locked Document.
type FAQRevisionTx interface {
	GetKnowledgeBase(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
	GetDocumentForUpdate(context.Context, uuid.UUID) (*model.Document, error)
	GetFAQRevision(context.Context, uuid.UUID) (*model.FAQRevision, error)
	CreateFAQRevisionAggregate(
		context.Context,
		*model.Document,
		*model.FAQRevision,
		*model.Job,
	) error
}

// FAQRevisionStore enters a tenant-local FAQ creation or replacement transaction.
type FAQRevisionStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, FAQRevisionTx) error) error
}

// DocumentPublishTx owns the pointer switch that makes one built revision searchable.
type DocumentPublishTx interface {
	GetDocumentForUpdate(context.Context, uuid.UUID) (*model.Document, error)
	GetKnowledgeBaseForUpdate(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
	PublishDocument(
		context.Context,
		*model.Document,
		*model.DocumentChunkSet,
		[]*model.Chunk,
		[]*model.ChunkRevision,
		[]*model.RetrievalEntry,
	) error
}

// DocumentPublishStore enters a tenant-local document publication transaction.
type DocumentPublishStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, DocumentPublishTx) error) error
}

// DocumentTaskTx reads trusted Job and Revision lineage before a worker runs external stages.
type DocumentTaskTx interface {
	GetJob(context.Context, uuid.UUID) (*dto.Job, error)
	GetRevision(context.Context, uuid.UUID) (*model.DocumentRevision, error)
	IsRevisionPublished(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}

// DocumentTaskStore owns short Workspace-scoped worker reads and atomic terminal failure writes.
type DocumentTaskStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, DocumentTaskTx) error) error
	MarkRunning(context.Context, uuid.UUID, uuid.UUID) error
	MarkSucceeded(context.Context, uuid.UUID, uuid.UUID) error
	MarkFailed(context.Context, uuid.UUID, uuid.UUID, string) error
	CreateNextForRevision(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		uuid.UUID,
		string,
		map[string]any,
	) (*dto.Job, error)
	FailTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error
}

// ChunkEditTx owns optimistic creation of a user ChunkRevision and its indexing Job.
type ChunkEditTx interface {
	GetKnowledgeBaseForUpdate(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
	GetDocumentForUpdate(context.Context, uuid.UUID) (*model.Document, error)
	GetChunkForUpdate(context.Context, uuid.UUID) (*model.Chunk, error)
	GetChunkRevision(context.Context, uuid.UUID) (*model.ChunkRevision, error)
	NextChunkRevisionNo(context.Context, uuid.UUID) (int64, error)
	CreateChunkRevisionAndJob(context.Context, *model.ChunkRevision, *model.Job) error
}

// ChunkEditStore enters a tenant-local Chunk edit transaction.
type ChunkEditStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, ChunkEditTx) error) error
}

// IndexGenerationTx owns generation creation and active-pointer compare-and-swap.
type IndexGenerationTx interface {
	GetKnowledgeBaseForUpdate(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
	GetIndexGeneration(context.Context, uuid.UUID) (*model.IndexGeneration, error)
	GetActiveManualEditStats(context.Context, uuid.UUID) (int64, int64, error)
	CreateIndexGeneration(context.Context, *model.IndexGeneration, *model.Job) error
	ActivateIndexGeneration(
		context.Context,
		*model.KnowledgeBase,
		*model.IndexGeneration,
		*model.IndexGeneration,
	) error
}

// IndexGenerationStore enters a tenant-local generation lifecycle transaction.
type IndexGenerationStore interface {
	WithinWorkspace(context.Context, uuid.UUID, func(context.Context, IndexGenerationTx) error) error
	List(context.Context, uuid.UUID, uuid.UUID) ([]*model.IndexGeneration, error)
	RecordFailure(context.Context, IndexGenerationBuildRequest, string, string, bool) error
}
