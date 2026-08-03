package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// registerTools 注册六个 typed tools。
func registerTools(srv *mcpserver.MCPServer, deps Dependencies) {
	registerKnowledgeBaseCreate(srv, deps)
	registerDocumentIngest(srv, deps)
	registerDocumentStatus(srv, deps)
	registerKnowledgeSearch(srv, deps)
	registerDocumentDelete(srv, deps)
	registerChunkGet(srv, deps)
}

// authFromRequest 从 request.Context() 读取已鉴权的 AuthContext。
func authFromRequest(ctx context.Context) (value.AuthContext, error) {
	auth, ok := value.AuthContextFromContext(ctx)
	if !ok || !auth.IsAPIKey() {
		return value.AuthContext{}, fmt.Errorf("missing api key auth context")
	}
	return auth, nil
}

// toErrorResult 把领域错误转成 MCP CallToolResult（isError=true）。
func toErrorResult(err error) *mcp.CallToolResult {
	mcpErr := mapDomainError(err)
	result, _ := mcp.NewToolResultJSON(mcpErr)
	result.IsError = true
	return result
}

// jsonResult 把结构化输出转成 MCP CallToolResult（structuredContent + JSON text）。
func jsonResult(data any) *mcp.CallToolResult {
	result, err := mcp.NewToolResultJSON(data)
	if err != nil {
		return toErrorResult(fmt.Errorf("生成结果失败: %w", err))
	}
	return result
}

var (
	// documentStatusOutputSchema 是 document_status 工具的输出 schema。
	// 使用宽松定义，允许实际返回包含更多字段。
	documentStatusOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "document": {
      "type": "object",
      "description": "文档信息",
      "properties": {
        "id": {"type": "string", "description": "文档 ID"},
        "title": {"type": "string", "description": "文档标题"},
        "kind": {"type": "string", "description": "文档种类: document / faq"},
        "file_type": {"type": "string", "description": "文件类型"},
        "source_type": {"type": "string", "description": "导入来源"},
        "status": {"type": "string", "description": "文档状态: pending / parsing / chunking / indexing / ready / error / deleted"},
        "size_bytes": {"type": "integer", "description": "文件大小（字节）"},
        "error_message": {"type": "string", "description": "错误信息"},
        "created_at": {"type": "string", "description": "创建时间"}
      },
      "additionalProperties": true
    },
    "job": {
      "type": "object",
      "description": "异步任务信息（仅当通过 job_id 查询时返回）",
      "properties": {
        "id": {"type": "string", "description": "任务 ID"},
        "type": {"type": "string", "description": "任务类型"},
        "status": {"type": "string", "description": "任务状态"},
        "error_message": {"type": "string", "description": "任务错误信息"}
      },
      "additionalProperties": true
    }
  },
  "additionalProperties": true
}`)

	// chunkGetOutputSchema 是 chunk_get 工具的输出 schema。
	// 使用宽松定义，允许实际返回包含更多字段。
	chunkGetOutputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Chunk ID"},
    "sequence": {"type": "integer", "description": "在文档中的序号"},
    "source_content": {"type": "string", "description": "原始内容"},
    "source_anchor": {"type": "object", "description": "来源锚点（页码、段落位置等）"},
    "active_revision": {
      "type": "object",
      "description": "当前活跃修订",
      "properties": {
        "id": {"type": "string", "description": "修订 ID"},
        "revision_no": {"type": "integer", "description": "修订版本号"},
        "content": {"type": "string", "description": "修订后的文本内容"},
        "context_header": {"type": "string", "description": "上下文标题（如章节名）"},
        "enabled": {"type": "boolean", "description": "是否启用（禁用则不参与检索）"},
        "status": {"type": "string", "description": "修订状态"},
        "error_message": {"type": "string", "description": "修订级错误信息"},
        "created_at": {"type": "string", "description": "创建时间"}
      },
      "additionalProperties": true
    }
  },
  "additionalProperties": true
}`)
)

// ===== knowledge_base_create =====

type knowledgeBaseCreateInput struct {
	Name             string `json:"name" jsonschema:"知识库名称"`
	Description      string `json:"description,omitempty" jsonschema:"知识库描述"`
	EmbeddingModelID string `json:"embedding_model_id" jsonschema:"Embedding 模型 ID"`
	ChunkSize        *int   `json:"chunk_size,omitempty" jsonschema:"分块大小，需与 chunk_overlap 成对提供"`
	ChunkOverlap     *int   `json:"chunk_overlap,omitempty" jsonschema:"分块重叠，需与 chunk_size 成对提供"`
}

type chunkingConfigOutput struct {
	ChunkSize    int `json:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap"`
}

type knowledgeBaseCreateOutput struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Description      string               `json:"description"`
	EmbeddingModelID string               `json:"embedding_model_id"`
	ChunkingConfig   chunkingConfigOutput `json:"chunking_config"`
	ContentVersion   int64                `json:"content_version"`
	CreatedAt        string               `json:"created_at"`
}

func registerKnowledgeBaseCreate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("knowledge_base_create",
		mcp.WithDescription("创建知识库；新知识库会原子加入调用 API Key 的范围。"),
		mcp.WithInputSchema[knowledgeBaseCreateInput](),
		mcp.WithOutputSchema[knowledgeBaseCreateOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in knowledgeBaseCreateInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		modelID, err := uuid.Parse(in.EmbeddingModelID)
		if err != nil {
			return toErrorResult(fmt.Errorf("embedding_model_id 必须是有效 UUID")), nil
		}
		if (in.ChunkSize == nil) != (in.ChunkOverlap == nil) {
			return toErrorResult(fmt.Errorf("chunk_size 与 chunk_overlap 必须同时提供或同时省略")), nil
		}
		apiKeyID := auth.PrincipalID
		kb, err := deps.KnowledgeBases.Create(ctx, MCPCreateKnowledgeBaseInput{
			WorkspaceID: auth.WorkspaceID, CallerAPIKeyID: &apiKeyID,
			Name: in.Name, Description: in.Description, EmbeddingModelID: modelID,
			ChunkSize: in.ChunkSize, ChunkOverlap: in.ChunkOverlap,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(knowledgeBaseCreateOutput{
			ID: kb.ID.String(), Name: kb.Name, Description: kb.Description,
			EmbeddingModelID: kb.EmbeddingModelID.String(),
			ChunkingConfig: chunkingConfigOutput{
				ChunkSize: kb.ChunkingConfig.ChunkSize, ChunkOverlap: kb.ChunkingConfig.ChunkOverlap,
			}, ContentVersion: kb.ContentVersion,
			CreatedAt: kb.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}), nil
	}))
}

// ===== document_ingest =====

type documentIngestInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"目标知识库 ID"`
	FileName        string `json:"file_name" jsonschema:"文件名，用于推断文件类型"`
	ContentBase64   string `json:"content_base64" jsonschema:"RFC 4648 standard/raw standard Base64 编码的文件内容"`
	ContentType     string `json:"content_type,omitempty" jsonschema:"MIME 类型（如 text/plain, application/pdf），未提供时根据文件名推断"`
	Title           string `json:"title,omitempty" jsonschema:"文档标题，未提供时使用文件名"`
	Dedupe          bool   `json:"dedupe,omitempty" jsonschema:"是否根据 SHA256 去重，已存在同内容文档时跳过并返回已有文档 ID"`
	ParentNodeID    string `json:"parent_node_id,omitempty" jsonschema:"父节点 ID，用于构建文档树"`
	NodeName        string `json:"node_name,omitempty" jsonschema:"节点名称，用于构建文档树"`
}

type documentIngestOutput struct {
	DocumentID string `json:"document_id"`
	JobID      string `json:"job_id"`
	Status     string `json:"status"`
	Deduped    bool   `json:"deduped"`
}

func registerDocumentIngest(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_ingest",
		mcp.WithDescription("通过 Base64 内联导入文档；不等待异步索引完成。"),
		mcp.WithInputSchema[documentIngestInput](),
		mcp.WithOutputSchema[documentIngestOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentIngestInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("knowledge_base_id 必须是有效 UUID")), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		decoded, err := decodeBase64Content(in.ContentBase64)
		if err != nil {
			return toErrorResult(fmt.Errorf("content_base64 解码失败")), nil
		}
		if int64(len(decoded)) > deps.InlineLimit {
			return toErrorResult(fmt.Errorf("内联导入超过 %d 字节上限，请改用 REST multipart", deps.InlineLimit)), nil
		}
		result, err := deps.DocumentIngest.Ingest(ctx, MCPIngestInput{
			WorkspaceID: auth.WorkspaceID, Access: auth.ResourceAccess(),
			KnowledgeBaseID: kbID, FileName: in.FileName, ContentType: in.ContentType,
			Title: in.Title, Reader: strings.NewReader(string(decoded)), SizeBytes: int64(len(decoded)),
			Dedupe: in.Dedupe, NodeName: in.NodeName,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(documentIngestOutput{
			DocumentID: result.DocumentID.String(), JobID: result.JobID.String(),
			Status: result.Status, Deduped: result.Deduped,
		}), nil
	}))
}

// decodeBase64Content 解码 standard 或 raw standard Base64，拒绝 URL/data URI。
func decodeBase64Content(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("content_base64 为空")
	}
	if strings.HasPrefix(trimmed, "http") || strings.HasPrefix(trimmed, "data:") {
		return nil, fmt.Errorf("不接受 URL 或 data URI")
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("content_base64 不是合法 Base64")
}

// ===== document_status =====

type documentStatusInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	DocumentID      string `json:"document_id" jsonschema:"文档 ID"`
	JobID           string `json:"job_id,omitempty" jsonschema:"异步任务 ID，提供时返回关联的 Job 状态"`
}

func registerDocumentStatus(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_status",
		mcp.WithDescription("查询文档与可选 Job 的安全状态。"),
		mcp.WithInputSchema[documentStatusInput](),
		mcp.WithRawOutputSchema(documentStatusOutputSchema),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentStatusInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("knowledge_base_id 必须是有效 UUID")), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		docID, err := uuid.Parse(in.DocumentID)
		if err != nil {
			return toErrorResult(fmt.Errorf("document_id 必须是有效 UUID")), nil
		}
		input := service.ProgrammaticDocumentStatusInput{Access: auth.ResourceAccess(), DocumentID: docID}
		if in.JobID != "" {
			jobID, err := uuid.Parse(in.JobID)
			if err != nil {
				return toErrorResult(fmt.Errorf("job_id 必须是有效 UUID")), nil
			}
			input.JobID = jobID
		}
		result, err := deps.DocumentStatus.Get(ctx, input)
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(result), nil
	}))
}

// （document_status 工具直接使用 service.ProgrammaticDocumentStatusInput）

// ===== knowledge_search =====

type knowledgeSearchInput struct {
	KnowledgeBaseIDs []string `json:"knowledge_base_ids" jsonschema:"知识库 ID 列表（1 到 20 个），只检索调用方有权访问的知识库"`
	Query            string   `json:"query" jsonschema:"检索查询文本"`
	VectorTopK       *int     `json:"vector_top_k,omitempty" jsonschema:"向量检索候选数，默认使用服务端配置"`
	KeywordTopK      *int     `json:"keyword_top_k,omitempty" jsonschema:"关键词检索候选数，默认使用服务端配置"`
	FinalTopK        *int     `json:"final_top_k,omitempty" jsonschema:"最终返回的结果数，默认使用服务端配置"`
}

type knowledgeSearchOutput struct {
	SearchedKnowledgeBaseIDs []string            `json:"searched_knowledge_base_ids"`
	Results                  []*dto.SearchResult `json:"results"`
}

func registerKnowledgeSearch(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("knowledge_search",
		mcp.WithDescription("跨多个绑定知识库执行混合检索，按 Embedding 模型快照分组复用 query embedding。"),
		mcp.WithInputSchema[knowledgeSearchInput](),
		mcp.WithOutputSchema[knowledgeSearchOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in knowledgeSearchInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeSearchRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbIDs := make([]uuid.UUID, 0, len(in.KnowledgeBaseIDs))
		for _, raw := range in.KnowledgeBaseIDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				return toErrorResult(fmt.Errorf("knowledge_base_ids 含非法 UUID")), nil
			}
			kbIDs = append(kbIDs, id)
		}
		results, err := deps.MultiSearch.Search(ctx, service.MultiKnowledgeSearchInput{
			WorkspaceID: auth.WorkspaceID, Access: auth.ResourceAccess(),
			KnowledgeBaseIDs: kbIDs, Query: in.Query,
			VectorTopK: in.VectorTopK, KeywordTopK: in.KeywordTopK, FinalTopK: in.FinalTopK,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		searched := make([]string, 0, len(kbIDs))
		for _, id := range kbIDs {
			searched = append(searched, id.String())
		}
		if results == nil {
			results = []*dto.SearchResult{}
		}
		return jsonResult(knowledgeSearchOutput{SearchedKnowledgeBaseIDs: searched, Results: results}), nil
	}))
}

// ===== document_delete =====

type documentDeleteInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	DocumentID      string `json:"document_id" jsonschema:"要删除的文档 ID"`
}

type documentDeleteOutput struct {
	DocumentID string `json:"document_id"`
	Deleted    bool   `json:"deleted"`
}

func registerDocumentDelete(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_delete",
		mcp.WithDescription("软删除文档；文档立即退出检索，事实与原始对象按现有保留合同处理。"),
		mcp.WithInputSchema[documentDeleteInput](),
		mcp.WithOutputSchema[documentDeleteOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentDeleteInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("knowledge_base_id 必须是有效 UUID")), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		docID, err := uuid.Parse(in.DocumentID)
		if err != nil {
			return toErrorResult(fmt.Errorf("document_id 必须是有效 UUID")), nil
		}
		if err := deps.DocumentDelete.Delete(ctx, auth.ResourceAccess(), docID); err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(documentDeleteOutput{DocumentID: docID.String(), Deleted: true}), nil
	}))
}

// ===== chunk_get =====

type chunkGetInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	ChunkID         string `json:"chunk_id" jsonschema:"Chunk ID"`
}

func registerChunkGet(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("chunk_get",
		mcp.WithDescription("获取单个 Chunk 的内容、来源锚点与活跃修订。"),
		mcp.WithInputSchema[chunkGetInput](),
		mcp.WithRawOutputSchema(chunkGetOutputSchema),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in chunkGetInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("knowledge_base_id 必须是有效 UUID")), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		chunkID, err := uuid.Parse(in.ChunkID)
		if err != nil {
			return toErrorResult(fmt.Errorf("chunk_id 必须是有效 UUID")), nil
		}
		chunk, err := deps.ChunkGet.Get(ctx, auth.WorkspaceID, kbID, chunkID)
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(chunk), nil
	}))
}
