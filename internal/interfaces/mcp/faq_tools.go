package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ===== faq_create / faq_update / faq_get =====
//
// FAQ 是"问题集合 → 单一答案"的文档形态（kind=faq），创建即 ready、恰好一个
// chunk（问题做 embedding、答案做内容），不走解析链路，由 document_index 异步
// 建索引。业务规则全部在 service 层（FAQDocumentService），本文件只做协议转换。

type faqCreateInput struct {
	KnowledgeBaseID string   `json:"knowledge_base_id" jsonschema:"目标知识库 ID"`
	Title           string   `json:"title" jsonschema:"FAQ 标题（通常是主问题的概括）"`
	Questions       []string `json:"questions" jsonschema:"问题列表（至少 1 个，支持同一答案的多种问法变体；规范化后重复的问题会被拒绝）"`
	Answer          string   `json:"answer" jsonschema:"答案（Markdown）"`
}

type faqUpdateInput struct {
	KnowledgeBaseID string   `json:"knowledge_base_id" jsonschema:"目标知识库 ID"`
	DocumentID      string   `json:"document_id" jsonschema:"FAQ 文档 ID"`
	BaseRevisionID  string   `json:"base_revision_id" jsonschema:"基于的修订 ID（乐观锁）：必须是当前活跃修订（从 faq_get 或 document_status 获取），过期则报 revision_conflict"`
	Questions       []string `json:"questions" jsonschema:"问题列表（完整替换语义，至少 1 个）"`
	Answer          string   `json:"answer" jsonschema:"答案（Markdown，完整替换语义）"`
}

type faqGetInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	DocumentID      string `json:"document_id" jsonschema:"FAQ 文档 ID"`
}

// faqOutput 是三个 FAQ 工具共用的输出形状。revision_status 为 indexing 期间检索
// 不可见，ready 后可检索。
type faqOutput struct {
	DocumentID     string   `json:"document_id"`
	Title          string   `json:"title"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	RevisionID     string   `json:"revision_id"`
	RevisionNo     int64    `json:"revision_no"`
	RevisionStatus string   `json:"revision_status"`
	Questions      []string `json:"questions"`
	Answer         string   `json:"answer"`
	JobID          string   `json:"job_id,omitempty"`
}

func faqOutputOf(result *dto.FAQDocument) faqOutput {
	if result == nil || result.Document == nil || result.Revision == nil {
		return faqOutput{Questions: []string{}}
	}
	out := faqOutput{
		DocumentID: result.Document.ID.String(), Title: result.Document.Title,
		Kind: string(result.Document.Kind), Status: string(result.Document.Status),
		RevisionID: result.Revision.ID.String(), RevisionNo: result.Revision.RevisionNo,
		RevisionStatus: string(result.Revision.Status),
		Questions:      result.Questions, Answer: result.Answer,
	}
	if out.Questions == nil {
		out.Questions = []string{}
	}
	if result.Job != nil {
		out.JobID = result.Job.ID.String()
	}
	return out
}

func registerFAQTools(srv *mcpserver.MCPServer, deps Dependencies) {
	registerFAQCreate(srv, deps)
	registerFAQUpdate(srv, deps)
	registerFAQGet(srv, deps)
}

func registerFAQCreate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("faq_create",
		mcp.WithDescription("在知识库中创建一条 FAQ 问答对（一个问题集合 + 一个答案）。"+
			"适合沉淀确定性的问答知识：答案即时生效为文档修订，后台异步建索引，用 document_status 轮询到 ready 即可被检索。"+
			"多个问题变体共享同一答案时把它们都放进 questions，检索命中率更高。"),
		withRawInputSchema[faqCreateInput](),
		withRawOutputSchema[faqOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in faqCreateInput) (*mcp.CallToolResult, error) {
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
		result, err := deps.FAQDocuments.Create(ctx, service.CreateFAQDocumentInput{
			WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID,
			Access: auth.ResourceAccess(), Title: in.Title,
			Questions: in.Questions, Answer: in.Answer,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(faqOutputOf(result)), nil
	}))
}

func registerFAQUpdate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("faq_update",
		mcp.WithDescription("整体更新一条 FAQ（questions 与 answer 均为完整替换语义）。"+
			"必须携带 base_revision_id 做乐观锁：先 faq_get 取当前活跃修订 ID，若期间已被他人更新将返回 revision_conflict，重新获取后再试。"),
		withRawInputSchema[faqUpdateInput](),
		withRawOutputSchema[faqOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in faqUpdateInput) (*mcp.CallToolResult, error) {
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
		docID, err := uuid.Parse(in.DocumentID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: document_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		revID, err := uuid.Parse(in.BaseRevisionID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: base_revision_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		result, err := deps.FAQDocuments.Update(ctx, service.UpdateFAQDocumentInput{
			WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID,
			Access: auth.ResourceAccess(), DocumentID: docID, BaseRevisionID: revID,
			Questions: in.Questions, Answer: in.Answer,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(faqOutputOf(result)), nil
	}))
}

func registerFAQGet(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("faq_get",
		mcp.WithDescription("读取一条 FAQ 的当前问答内容（questions + answer）与活跃修订信息。"+
			"document_get 对 FAQ 文档不返回问答结构，编辑前用本工具取内容与 base_revision_id。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[faqGetInput](),
		withRawOutputSchema[faqOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in faqGetInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		docID, err := uuid.Parse(in.DocumentID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: document_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		result, err := deps.FAQDocuments.Get(ctx, auth.ResourceAccess(), kbID, docID)
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(faqOutputOf(result)), nil
	}))
}
