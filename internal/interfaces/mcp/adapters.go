package mcp

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// knowledgeBaseCreateAdapter 把 service.KnowledgeBaseService.Create 适配为
// MCPKnowledgeBaseService，并保留 MCP 分块配置中显式传入的 false 值。
type knowledgeBaseCreateAdapter struct {
	svc KnowledgeBaseCreator
}

// KnowledgeBaseCreator 是 service.KnowledgeBaseService.Create 的最小端口。
type KnowledgeBaseCreator interface {
	Create(ctx context.Context, input service.CreateKnowledgeBaseInput) (*dto.KnowledgeBase, error)
}

// NewMCPKnowledgeBaseService 构造 MCP 知识库创建适配器。
func NewMCPKnowledgeBaseService(svc KnowledgeBaseCreator) MCPKnowledgeBaseService {
	return &knowledgeBaseCreateAdapter{svc: svc}
}

func (a *knowledgeBaseCreateAdapter) Create(ctx context.Context, in MCPCreateKnowledgeBaseInput) (*dto.KnowledgeBase, error) {
	input := service.CreateKnowledgeBaseInput{
		WorkspaceID: in.WorkspaceID, CallerAPIKeyID: in.CallerAPIKeyID,
		Name: in.Name, Description: in.Description, EmbeddingModelID: in.EmbeddingModelID,
	}
	if in.Strategy != nil || in.EnableParentChild != nil || in.ParentChunkSize != nil || in.ChildChunkSize != nil || in.ChunkSize != nil || in.ChunkOverlap != nil {
		config := value.DefaultChunkingConfig()
		if in.Strategy != nil {
			config.Strategy = *in.Strategy
		}
		if in.EnableParentChild != nil {
			config.EnableParentChild = *in.EnableParentChild
		}
		if in.ParentChunkSize != nil {
			config.ParentChunkSize = *in.ParentChunkSize
		}
		if in.ChildChunkSize != nil {
			config.ChildChunkSize = *in.ChildChunkSize
		}
		if in.ChunkSize != nil {
			config.ChunkSize = *in.ChunkSize
		}
		if in.ChunkOverlap != nil {
			config.ChunkOverlap = *in.ChunkOverlap
		}
		input.ChunkingConfig = &config
	}
	return a.svc.Create(ctx, input)
}

// documentIngestAdapter 把 service.DocumentIngestService.Ingest 适配为
// MCPDocumentIngestService，固定 source_type=mcp。
type documentIngestAdapter struct {
	svc DocumentIngester
}

// DocumentIngester 是 service.DocumentIngestService.Ingest 的最小端口。
type DocumentIngester interface {
	Ingest(ctx context.Context, input service.IngestDocumentInput) (*service.IngestDocumentResult, error)
}

// NewMCPDocumentIngestService 构造 MCP 文档导入适配器。
func NewMCPDocumentIngestService(svc DocumentIngester) MCPDocumentIngestService {
	return &documentIngestAdapter{svc: svc}
}

func (a *documentIngestAdapter) Ingest(ctx context.Context, in MCPIngestInput) (*MCPIngestResult, error) {
	input := service.IngestDocumentInput{
		WorkspaceID: in.WorkspaceID, KnowledgeBaseID: in.KnowledgeBaseID,
		Title: in.Title, FileName: in.FileName, ContentType: in.ContentType,
		SourceType: "mcp", Reader: in.Reader, SizeBytes: in.SizeBytes,
		Dedupe: in.Dedupe, ParentNodeID: in.ParentNodeID, NodeName: in.NodeName,
	}
	result, err := a.svc.Ingest(ctx, input)
	if err != nil {
		return nil, err
	}
	status := "pending"
	if result.Document != nil {
		status = string(result.Document.Status)
	}
	jobID := uuid.Nil
	if result.Job != nil {
		jobID = result.Job.ID
	}
	docID := uuid.Nil
	if result.Document != nil {
		docID = result.Document.ID
	}
	return &MCPIngestResult{DocumentID: docID, JobID: jobID, Status: status, Deduped: result.Deduped}, nil
}

// documentDeleteAdapter 把 service.DocumentService.Delete 适配为
// MCPDocumentDeleteService。
type documentDeleteAdapter struct {
	svc DocumentDeleter
}

// DocumentDeleter 是 service.DocumentService.Delete 的最小端口。
type DocumentDeleter interface {
	Delete(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) error
}

// NewMCPDocumentDeleteService 构造 MCP 文档删除适配器。
func NewMCPDocumentDeleteService(svc DocumentDeleter) MCPDocumentDeleteService {
	return &documentDeleteAdapter{svc: svc}
}

func (a *documentDeleteAdapter) Delete(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) error {
	return a.svc.Delete(ctx, access, documentID)
}

// chunkGetAdapter 把 service.ChunkRevisionService.Get 适配为 MCPChunkGetService。
type chunkGetAdapter struct {
	svc ChunkGetter
}

// ChunkGetter 是 service.ChunkRevisionService.Get 的最小端口。
type ChunkGetter interface {
	Get(ctx context.Context, workspaceID, knowledgeBaseID, chunkID uuid.UUID) (*dto.Chunk, error)
}

// NewMCPChunkGetService 构造 MCP Chunk 获取适配器。
func NewMCPChunkGetService(svc ChunkGetter) MCPChunkGetService {
	return &chunkGetAdapter{svc: svc}
}

func (a *chunkGetAdapter) Get(ctx context.Context, workspaceID, knowledgeBaseID, chunkID uuid.UUID) (*dto.Chunk, error) {
	return a.svc.Get(ctx, workspaceID, knowledgeBaseID, chunkID)
}

// MCPKnowledgeBaseListService 是发现类工具的知识库列表契约。
// *service.KnowledgeBaseService 直接满足。
type MCPKnowledgeBaseListService interface {
	List(ctx context.Context, access value.ResourceAccess) ([]*dto.KnowledgeBase, error)
}

// MCPDocumentListService 是发现类工具的文档列表契约。
// *service.DocumentService 直接满足。
type MCPDocumentListService interface {
	List(ctx context.Context, filter service.DocumentListFilter) ([]*dto.Document, error)
}

// MCPDocumentGetService 是阅读类工具的文档详情契约（含归一化正文）。
// *service.DocumentService 直接满足。
type MCPDocumentGetService interface {
	Get(ctx context.Context, access value.ResourceAccess, id uuid.UUID) (*dto.Document, error)
}
