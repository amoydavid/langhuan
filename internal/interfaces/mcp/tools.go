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

// registerTools 注册七个 typed tools。
func registerTools(srv *mcpserver.MCPServer, deps Dependencies) {
	registerKnowledgeBaseCreate(srv, deps)
	registerDocumentIngest(srv, deps)
	registerDocumentStatus(srv, deps)
	registerKnowledgeSearch(srv, deps)
	registerDocumentDelete(srv, deps)
	registerDocumentRetry(srv, deps)
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
	Name              string                  `json:"name" jsonschema:"知识库名称"`
	Description       string                  `json:"description,omitempty" jsonschema:"知识库描述"`
	EmbeddingModelID  string                  `json:"embedding_model_id" jsonschema:"Embedding 模型 ID"`
	Strategy          *value.ChunkingStrategy `json:"strategy,omitempty" jsonschema:"分块策略：auto、heading、heuristic 或 recursive"`
	EnableParentChild *bool                   `json:"enable_parent_child,omitempty" jsonschema:"是否启用父子分块；显式 false 使用扁平分块"`
	ParentChunkSize   *int                    `json:"parent_chunk_size,omitempty" jsonschema:"父块大小，用于返回完整上下文"`
	ChildChunkSize    *int                    `json:"child_chunk_size,omitempty" jsonschema:"子块大小，用于召回"`
	ChunkSize         *int                    `json:"chunk_size,omitempty" jsonschema:"扁平分块大小，需与 chunk_overlap 成对提供"`
	ChunkOverlap      *int                    `json:"chunk_overlap,omitempty" jsonschema:"扁平分块重叠，需与 chunk_size 成对提供"`
}

type chunkingConfigOutput struct {
	Strategy          value.ChunkingStrategy `json:"strategy"`
	EnableParentChild bool                   `json:"enable_parent_child"`
	ParentChunkSize   int                    `json:"parent_chunk_size"`
	ChildChunkSize    int                    `json:"child_chunk_size"`
	ChunkSize         int                    `json:"chunk_size"`
	ChunkOverlap      int                    `json:"chunk_overlap"`
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
		mcp.WithDescription("创建一个新的知识库，用于组织和管理同类文档。创建后即可用 document_ingest 向其中导入文件，再用 knowledge_search 检索。需指定名称和 Embedding 模型。"),
		withRawInputSchema[knowledgeBaseCreateInput](),
		withRawOutputSchema[knowledgeBaseCreateOutput](),
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
			Strategy: in.Strategy, EnableParentChild: in.EnableParentChild,
			ParentChunkSize: in.ParentChunkSize, ChildChunkSize: in.ChildChunkSize,
			ChunkSize: in.ChunkSize, ChunkOverlap: in.ChunkOverlap,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(knowledgeBaseCreateOutput{
			ID: kb.ID.String(), Name: kb.Name, Description: kb.Description,
			EmbeddingModelID: kb.EmbeddingModelID.String(),
			ChunkingConfig: chunkingConfigOutput{
				Strategy: kb.ChunkingConfig.Strategy, EnableParentChild: kb.ChunkingConfig.EnableParentChild,
				ParentChunkSize: kb.ChunkingConfig.ParentChunkSize, ChildChunkSize: kb.ChunkingConfig.ChildChunkSize,
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
		mcp.WithDescription("将文件导入到知识库以供后续检索。支持 PDF、Word(docx)、Markdown、纯文本、CSV、Excel(xlsx) 等格式，文件内容以 Base64 编码传入。导入是异步的：调用后立即返回文档 ID 和任务 ID，解析与索引在后台进行——需用 document_status 轮询直到状态变为 ready 才能被检索到。"),
		withRawInputSchema[documentIngestInput](),
		withRawOutputSchema[documentIngestOutput](),
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
		mcp.WithDescription("查询文档的导入处理进度。document_ingest 之后调用此工具轮询，直到状态为 ready（可被检索）或 error。状态取值：pending / parsing / chunking / indexing / ready / error。也返回文档的活跃修订信息，但不返回文档原文。"),
		withRawInputSchema[documentStatusInput](),
		withRawOutputSchemaFrom(documentStatusOutputSchema),
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
	KnowledgeBaseIDs []string `json:"knowledge_base_ids,omitempty" jsonschema:"知识库 ID 列表，只检索调用方有权访问的知识库；未指定时检索当前 API Key 绑定的全部知识库"`
	Query            string   `json:"query" jsonschema:"检索查询文本"`
	VectorTopK       *int     `json:"vector_top_k,omitempty" jsonschema:"向量检索候选数，默认使用服务端配置"`
	KeywordTopK      *int     `json:"keyword_top_k,omitempty" jsonschema:"关键词检索候选数，默认使用服务端配置"`
	FinalTopK        *int     `json:"final_top_k,omitempty" jsonschema:"最终返回的结果数，默认使用服务端配置"`
}

type knowledgeSearchOutput struct {
	SearchID                 string              `json:"search_id"`
	RequestedScope           string              `json:"requested_scope"`
	EffectiveScope           string              `json:"effective_scope"`
	RetrievalStatus          string              `json:"retrieval_status"`
	GenerationIDs            []string            `json:"generation_ids"`
	SearchedKnowledgeBaseIDs []string            `json:"searched_knowledge_base_ids"`
	Results                  []*dto.SearchResult `json:"results"`
}

func registerKnowledgeSearch(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("knowledge_search",
		mcp.WithDescription("知识库检索工具。当需要基于用户的问题从知识库中查找相关资料、回答事实性问题时调用：返回最相关的文档片段（含内容、来源、相关性评分）。同时使用向量语义匹配和关键词匹配，可能根据当前索引配置对结果执行重排（ranking_stage 标识实际排序阶段，fallback 时仍返回 RRF 顺序结果）。knowledge_base_ids 留空只代表当前 API Key 绑定的全部知识库，不代表 Workspace 全量知识库。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[knowledgeSearchInput](),
		withRawOutputSchema[knowledgeSearchOutput](),
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
		// 未指定 knowledge_base_ids 时，默认检索当前 API Key 绑定的全部知识库（api_key_bound_all）。
		// MCP 入口仅接受 API Key 主体，其 KnowledgeBaseIDs 即绑定的知识库集合。
		requestedScope := value.SearchScopeSelected
		if len(kbIDs) == 0 {
			if len(auth.KnowledgeBaseIDs) == 0 {
				return toErrorResult(fmt.Errorf("%w: 未指定 knowledge_base_ids 且当前 API Key 未绑定任何知识库", domainerrors.ErrValidation)), nil
			}
			kbIDs = append(kbIDs, auth.KnowledgeBaseIDs...)
			requestedScope = value.SearchScopeAPIKeyBoundAll
		}
		response, err := deps.MultiSearch.Search(ctx, service.MultiKnowledgeSearchInput{
			WorkspaceID: auth.WorkspaceID, Access: auth.ResourceAccess(),
			KnowledgeBaseIDs: kbIDs, Query: in.Query,
			VectorTopK: in.VectorTopK, KeywordTopK: in.KeywordTopK, FinalTopK: in.FinalTopK,
			RequestedScope: requestedScope,
		})
		if err != nil {
			return toSearchErrorResult(err, response), nil
		}
		searched := make([]string, 0, len(kbIDs))
		for _, id := range kbIDs {
			searched = append(searched, id.String())
		}
		results := []*dto.SearchResult{}
		out := knowledgeSearchOutput{SearchedKnowledgeBaseIDs: searched}
		if response != nil {
			results = response.Results
			out.SearchID = response.Run.SearchID.String()
			out.RequestedScope = string(response.Run.RequestedScope)
			out.EffectiveScope = string(response.Run.EffectiveScope)
			out.RetrievalStatus = string(response.Run.RetrievalStatus)
			genIDs := make([]string, 0, len(response.Run.GenerationSnapshots))
			for _, snap := range response.Run.GenerationSnapshots {
				genIDs = append(genIDs, snap.GenerationID.String())
			}
			out.GenerationIDs = genIDs
		}
		out.Results = results
		return jsonResult(out), nil
	}))
}

// toSearchErrorResult 在 SearchRun 创建后发生错误时，把 search_id 和 failure_class
// 放入 isError=true 的稳定错误对象；response 为空（创建前失败）时退化为 toErrorResult。
func toSearchErrorResult(err error, response *dto.SearchResponse) *mcp.CallToolResult {
	if response == nil {
		return toErrorResult(err)
	}
	mcpErr := mapDomainError(err)
	extra := map[string]any{
		"search_id": response.Run.SearchID.String(),
	}
	if response.Run.FailureClass != "" {
		extra["failure_class"] = response.Run.FailureClass
	}
	mcpErr.Error.Details = extra
	data, marshalErr := json.Marshal(mcpErr)
	if marshalErr != nil {
		return toErrorResult(err)
	}
	result := mcp.NewToolResultText(string(data))
	result.IsError = true
	return result
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

// ===== document_retry =====

type documentRetryInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	DocumentID      string `json:"document_id" jsonschema:"要重试的文档 ID"`
}

type documentRetryOutput struct {
	DocumentID string `json:"document_id"`
	RevisionID string `json:"revision_id"`
	JobID      string `json:"job_id"`
}

func registerDocumentRetry(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_retry",
		mcp.WithDescription("重试一个导入失败的文档。将其最新修订重置为待解析并重新入队，完整重跑解析→分块→索引链路。仅 failed 状态可重试；重复调用幂等。"),
		withRawInputSchema[documentRetryInput](),
		withRawOutputSchema[documentRetryOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentRetryInput) (*mcp.CallToolResult, error) {
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
		result, err := deps.DocumentRetry.RetryDocument(ctx, auth.ResourceAccess(), docID)
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(documentRetryOutput{
			DocumentID: result.DocumentID.String(),
			RevisionID: result.RevisionID.String(),
			JobID:      result.JobID.String(),
		}), nil
	}))
}

func registerDocumentDelete(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_delete",
		mcp.WithDescription("从知识库中删除指定文档。删除后该文档不再参与检索。重复删除同一文档是安全的（幂等）。"),
		withRawInputSchema[documentDeleteInput](),
		withRawOutputSchema[documentDeleteOutput](),
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
		mcp.WithDescription("按 ID 获取单个文档片段（chunk）的完整内容、来源锚点（页码/位置）和活跃修订。通常在 knowledge_search 拿到结果后，需要查看某个片段的更详细信息时调用。"),
		withRawInputSchema[chunkGetInput](),
		withRawOutputSchemaFrom(chunkGetOutputSchema),
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
