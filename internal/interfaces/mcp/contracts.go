package mcp

import (
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// MCPKnowledgeBaseService 是 MCP knowledge_base_create 工具所需的知识库创建端口。
type MCPKnowledgeBaseService interface {
	Create(ctx context.Context, input MCPCreateKnowledgeBaseInput) (*dto.KnowledgeBase, error)
}

// MCPCreateKnowledgeBaseInput 是 MCP 工具层的知识库创建输入。
type MCPCreateKnowledgeBaseInput struct {
	WorkspaceID       uuid.UUID
	CallerAPIKeyID    *uuid.UUID
	Name              string
	Description       string
	EmbeddingModelID  uuid.UUID
	Strategy          *value.ChunkingStrategy
	EnableParentChild *bool
	ParentChunkSize   *int
	ChildChunkSize    *int
	ChunkSize         *int
	ChunkOverlap      *int
}

// MCPDocumentIngestService 是 MCP document_ingest 工具所需的导入端口。
type MCPDocumentIngestService interface {
	Ingest(ctx context.Context, input MCPIngestInput) (*MCPIngestResult, error)
}

// MCPIngestInput 是 MCP 工具层的导入输入。file 模式 Reader 与 SizeBytes 由解码
// Base64 得到；text 模式由明文内容构造。IdempotencyKey/CallerAPIKeyID 仅 text 模式
// 使用（与 REST /documents/text 的 Bearer 幂等键对齐），file 模式留空、行为不变。
type MCPIngestInput struct {
	WorkspaceID     uuid.UUID
	Access          value.ResourceAccess
	KnowledgeBaseID uuid.UUID
	FileName        string
	ContentType     string
	Title           string
	Reader          io.Reader
	SizeBytes       int64
	Dedupe          bool
	ParentNodeID    *uuid.UUID
	NodeName        string
	IdempotencyKey  string
	CallerAPIKeyID  *uuid.UUID
}

// MCPIngestResult 是 MCP 工具层的导入输出。
type MCPIngestResult struct {
	DocumentID uuid.UUID `json:"document_id"`
	JobID      uuid.UUID `json:"job_id"`
	Status     string    `json:"status"`
	Deduped    bool      `json:"deduped"`
}

// MCPWebSourceService 是 document_ingest url 入口（v1.3.0 spec §9.3）与
// web_source_* 工具组（v1.5.0）共用的 web 源端口：url 模式把 URL 并入目标 KB
// 恰有的一个 urls 模式 active 网站源并触发同步；CRUD 六方法与 REST
// /knowledge-bases/:id/web-sources 同语义。生产实现是 *service.KbSourceService。
type MCPWebSourceService interface {
	AddURLToWebSource(ctx context.Context, workspaceID, kbID uuid.UUID, rawURL string) (*model.KbSource, *model.Job, error)
	CreateWebSource(ctx context.Context, input service.CreateWebSourceInput) (*model.KbSource, *model.Job, error)
	ListWebSources(ctx context.Context, access value.ResourceAccess, kbID uuid.UUID) ([]service.WebSourceView, error)
	GetWebSource(ctx context.Context, access value.ResourceAccess, kbID, sourceID uuid.UUID) (*service.WebSourceView, error)
	UpdateWebSource(ctx context.Context, input service.UpdateWebSourceInput) (*model.KbSource, *model.Job, error)
	SyncWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, force bool) (*model.Job, error)
	DeleteWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, pages string) error
}

// MCPFAQService 是 faq_create/faq_update/faq_get 工具所需的 FAQ 端口。
// 生产实现 *service.FAQDocumentService 直接满足。
type MCPFAQService interface {
	Create(ctx context.Context, input service.CreateFAQDocumentInput) (*dto.FAQDocument, error)
	Update(ctx context.Context, input service.UpdateFAQDocumentInput) (*dto.FAQDocument, error)
	Get(ctx context.Context, access value.ResourceAccess, knowledgeBaseID, documentID uuid.UUID) (*dto.FAQDocument, error)
}

// MCPFileTreeService 是 file_tree_* 工具组所需的文件树端口。
// 生产实现 *service.FileTreeService 直接满足。
type MCPFileTreeService interface {
	List(ctx context.Context, access value.ResourceAccess, knowledgeBaseID uuid.UUID) (*dto.FileTree, error)
	CreateFolder(ctx context.Context, input service.CreateFileTreeFolderInput) (*dto.FileTreeNode, error)
	Update(ctx context.Context, input service.UpdateFileTreeNodeInput) error
	Delete(ctx context.Context, access value.ResourceAccess, knowledgeBaseID, nodeID uuid.UUID) error
}

// MCPDocumentDeleteService 是 MCP document_delete 工具所需的删除端口。
type MCPDocumentDeleteService interface {
	Delete(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) error
}

// MCPChunkGetService 是 MCP chunk_get 工具所需的端口。
type MCPChunkGetService interface {
	Get(ctx context.Context, workspaceID, knowledgeBaseID, chunkID uuid.UUID) (*dto.Chunk, error)
}

// MCPDocumentRetryService 是 MCP document_retry 工具所需的重试端口。
type MCPDocumentRetryService interface {
	RetryDocument(ctx context.Context, access value.ResourceAccess, documentID uuid.UUID) (*service.RetryResult, error)
}

// toolScopeRequirement 返回每个工具所需的 scope；未注册的工具返回 (_, false)。
func toolScopeRequirement(name string) (value.APIScope, bool) {
	switch name {
	case "knowledge_base_create":
		return value.ScopeKnowledgeBasesWrite, true
	case "document_ingest", "document_delete", "document_retry",
		"document_ingest_text", "faq_create", "faq_update",
		"file_tree_folder_create", "file_tree_node_update", "file_tree_node_delete":
		return value.ScopeDocumentsWrite, true
	case "document_status", "chunk_get",
		"faq_get", "file_tree_list":
		return value.ScopeDocumentsRead, true
	case "knowledge_search":
		return value.ScopeSearchRead, true
	case "knowledge_base_list":
		return value.ScopeKnowledgeBasesRead, true
	case "document_list", "document_get":
		return value.ScopeDocumentsRead, true
	case "web_source_list", "web_source_get":
		return value.ScopeKnowledgeBasesRead, true
	case "web_source_create", "web_source_update", "web_source_sync", "web_source_delete":
		return value.ScopeKnowledgeBasesWrite, true
	}
	return "", false
}
