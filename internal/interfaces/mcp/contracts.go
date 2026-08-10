package mcp

import (
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
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

// MCPIngestInput 是 MCP 工具层的导入输入。Reader 与 SizeBytes 由解码 Base64 得到。
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
}

// MCPIngestResult 是 MCP 工具层的导入输出。
type MCPIngestResult struct {
	DocumentID uuid.UUID `json:"document_id"`
	JobID      uuid.UUID `json:"job_id"`
	Status     string    `json:"status"`
	Deduped    bool      `json:"deduped"`
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
	case "document_ingest", "document_delete", "document_retry":
		return value.ScopeDocumentsWrite, true
	case "document_status", "chunk_get":
		return value.ScopeDocumentsRead, true
	case "knowledge_search":
		return value.ScopeSearchRead, true
	}
	return "", false
}
