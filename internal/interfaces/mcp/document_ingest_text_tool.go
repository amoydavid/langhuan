package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ===== document_ingest_text =====

// documentIngestTextInput 是 document_ingest_text 的输入：Agent 把自己生成的
// Markdown 文本直接写入知识库，无需 Base64 伪装成文件（对应 REST
// POST /knowledge-bases/:id/documents/text）。
type documentIngestTextInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"目标知识库 ID"`
	Title           string `json:"title" jsonschema:"文档标题（同时作为文件名与树节点名）"`
	Content         string `json:"content" jsonschema:"Markdown 正文内容"`
	ParentNodeID    string `json:"parent_node_id,omitempty" jsonschema:"父节点 ID（从 file_tree_list 获取），缺省挂在根目录"`
	NodeName        string `json:"node_name,omitempty" jsonschema:"文件树节点名，缺省使用标题"`
	IdempotencyKey  string `json:"idempotency_key,omitempty" jsonschema:"幂等键（1..128 ASCII）：同 key 重复调用返回首次结果，防止网络重试造成重复文档"`
}

// documentIngestTextOutput 与 document_ingest file 模式同形：text 写入同样异步
// 走解析→分块→索引链路，需用 document_status 轮询。
type documentIngestTextOutput struct {
	DocumentID string `json:"document_id"`
	JobID      string `json:"job_id"`
	Status     string `json:"status"`
	Deduped    bool   `json:"deduped"`
}

func registerDocumentIngestText(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_ingest_text",
		mcp.WithDescription("把一段 Markdown 文本直接写入知识库（无需 Base64 编码），适合 Agent 沉淀自己生成的内容：笔记、总结、会话纪要等。"+
			"异步操作：立即返回任务 ID，用 document_status 轮询直到 ready 才能被检索。"+
			"导入已有文件用 document_ingest；沉淀问答知识用 faq_create。"),
		withRawInputSchema[documentIngestTextInput](),
		withRawOutputSchema[documentIngestTextOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentIngestTextInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return toErrorResult(fmt.Errorf("%w: title 不能为空", domainerrors.ErrValidation)), nil
		}
		if strings.TrimSpace(in.Content) == "" {
			return toErrorResult(fmt.Errorf("%w: content 不能为空", domainerrors.ErrValidation)), nil
		}
		var parentNodeID *uuid.UUID
		if in.ParentNodeID != "" {
			parsed, err := uuid.Parse(in.ParentNodeID)
			if err != nil {
				return toErrorResult(fmt.Errorf("%w: parent_node_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
			}
			parentNodeID = &parsed
		}
		size := int64(len(in.Content))
		if size > deps.InlineLimit {
			return toErrorResult(fmt.Errorf("文本导入超过 %d 字节上限，请改用 REST multipart", deps.InlineLimit)), nil
		}
		// 幂等键仅对 API Key 主体生效（与 REST /documents/text 的 Bearer 语义对齐）。
		var callerID *uuid.UUID
		if auth.IsAPIKey() && auth.PrincipalID != uuid.Nil {
			id := auth.PrincipalID
			callerID = &id
		}
		nodeName := in.NodeName
		if nodeName == "" {
			nodeName = title
		}
		result, err := deps.DocumentIngest.Ingest(ctx, MCPIngestInput{
			WorkspaceID: auth.WorkspaceID, Access: auth.ResourceAccess(),
			KnowledgeBaseID: kbID, FileName: title + ".md", ContentType: "text/markdown",
			Title: title, Reader: strings.NewReader(in.Content), SizeBytes: size,
			ParentNodeID: parentNodeID, NodeName: nodeName,
			IdempotencyKey: in.IdempotencyKey, CallerAPIKeyID: callerID,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(documentIngestTextOutput{
			DocumentID: result.DocumentID.String(), JobID: result.JobID.String(),
			Status: result.Status, Deduped: result.Deduped,
		}), nil
	}))
}
