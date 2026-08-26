// discovery_tools.go 实现 MCP 发现/阅读类只读工具：
// knowledge_base_list / document_list / document_get。
// agent 被丢进 workspace 后先靠它们建立环境认知（有哪些库、哪些文档、
// 文档讲什么），再走 search → chunk_get 的检索钻取路径。
package mcp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

const (
	mcpDocumentListDefaultPageSize = 50
	mcpDocumentListMaxPageSize     = 200
	mcpDocumentGetDefaultMaxChars  = 50000
	mcpDocumentGetMaxMaxChars      = 200000
	mcpOutlineMaxEntries           = 200
)

// ===== knowledge_base_list =====

type knowledgeBaseListOutputItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
}

type knowledgeBaseListOutput struct {
	KnowledgeBases []knowledgeBaseListOutputItem `json:"knowledge_bases"`
}

func registerKnowledgeBaseListTool(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("knowledge_base_list",
		mcp.WithDescription("列出当前调用方有权访问的全部知识库（API Key 场景为其绑定的知识库集合）。"+
			"在检索前先调用它了解环境里有哪些知识库，再用 knowledge_base_ids 定向检索。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[struct{}](),
		withRawOutputSchema[knowledgeBaseListOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		knowledgeBases, err := deps.KnowledgeBaseList.List(ctx, auth.ResourceAccess())
		if err != nil {
			return toErrorResult(err), nil
		}
		items := make([]knowledgeBaseListOutputItem, 0, len(knowledgeBases))
		for _, kb := range knowledgeBases {
			items = append(items, knowledgeBaseListOutputItem{
				ID: kb.ID.String(), Name: kb.Name, Description: kb.Description,
				UpdatedAt: kb.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
		return jsonResult(knowledgeBaseListOutput{KnowledgeBases: items}), nil
	}))
}

// ===== document_list =====

type documentListInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	Page            *int   `json:"page,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize        *int   `json:"page_size,omitempty" jsonschema:"每页数量，默认 50，最大 200"`
}

type documentListOutputItem struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

type documentListOutput struct {
	Documents []documentListOutputItem `json:"documents"`
	Page      int                      `json:"page"`
	PageSize  int                      `json:"page_size"`
	HasMore   bool                     `json:"has_more"`
}

func registerDocumentListTool(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_list",
		mcp.WithDescription("分页列出指定知识库内的全部文档（标题、类型、处理状态）。"+
			"用于浏览知识库内容概貌；细读某篇文档用 document_get，检索用 knowledge_search。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[documentListInput](),
		withRawOutputSchema[documentListOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentListInput) (*mcp.CallToolResult, error) {
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
		page := 1
		if in.Page != nil && *in.Page > 0 {
			page = *in.Page
		}
		pageSize := mcpDocumentListDefaultPageSize
		if in.PageSize != nil && *in.PageSize > 0 {
			pageSize = *in.PageSize
		}
		if pageSize > mcpDocumentListMaxPageSize {
			pageSize = mcpDocumentListMaxPageSize
		}
		documents, err := deps.DocumentList.List(ctx, service.DocumentListFilter{WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID, Access: auth.ResourceAccess()})
		if err != nil {
			return toErrorResult(err), nil
		}
		start := (page - 1) * pageSize
		if start > len(documents) {
			start = len(documents)
		}
		end := start + pageSize
		if end > len(documents) {
			end = len(documents)
		}
		items := make([]documentListOutputItem, 0, end-start)
		for _, doc := range documents[start:end] {
			items = append(items, documentListOutputItem{
				ID: doc.ID.String(), Title: doc.Title, Kind: string(doc.Kind),
				Status: string(doc.Status), ErrorMessage: doc.ErrorMessage,
				UpdatedAt: doc.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
		return jsonResult(documentListOutput{
			Documents: items, Page: page, PageSize: pageSize, HasMore: end < len(documents),
		}), nil
	}))
}

// ===== document_get =====

type documentGetInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	DocumentID      string `json:"document_id" jsonschema:"文档 ID"`
	MaxChars        *int   `json:"max_chars,omitempty" jsonschema:"返回正文的最大字符数，默认 50000，最大 200000；超出截断并标记 truncated"`
}

type documentOutlineEntry struct {
	Path []string `json:"path"`
	Line int      `json:"line"`
}

type documentGetOutput struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Kind      string                 `json:"kind"`
	Status    string                 `json:"status"`
	UpdatedAt string                 `json:"updated_at"`
	Truncated bool                   `json:"truncated"`
	Content   string                 `json:"content"`
	Outline   []documentOutlineEntry `json:"outline"`
}

func registerDocumentGetTool(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("document_get",
		mcp.WithDescription("获取一篇文档的完整归一化正文（Markdown）与章节大纲（outline 为主线标题路径与行号；无标题结构的文档 outline 为空）。"+
			"正文默认截断到 50000 字符；细读某个片段所在位置时配合 knowledge_search 的锚点使用。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[documentGetInput](),
		withRawOutputSchema[documentGetOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in documentGetInput) (*mcp.CallToolResult, error) {
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
		documentID, err := uuid.Parse(in.DocumentID)
		if err != nil {
			return toErrorResult(fmt.Errorf("document_id 必须是有效 UUID")), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		document, err := deps.DocumentGet.Get(ctx, auth.ResourceAccess(), documentID)
		if err != nil {
			return toErrorResult(err), nil
		}
		if document.KnowledgeBaseID != kbID {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		maxChars := mcpDocumentGetDefaultMaxChars
		if in.MaxChars != nil && *in.MaxChars > 0 {
			maxChars = *in.MaxChars
		}
		if maxChars > mcpDocumentGetMaxMaxChars {
			maxChars = mcpDocumentGetMaxMaxChars
		}
		content := document.NormalizedMarkdown
		truncated := utf8.RuneCountInString(content) > maxChars
		if truncated {
			content = string([]rune(content)[:maxChars])
		}
		return jsonResult(documentGetOutput{
			ID: document.ID.String(), Title: document.Title, Kind: string(document.Kind),
			Status: string(document.Status), UpdatedAt: document.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Truncated: truncated, Content: content, Outline: documentOutlineOf(document.NormalizedMarkdown),
		}), nil
	}))
}

// documentOutlineOf 从归一化 Markdown 的标题行生成章节大纲（信息等价于
// ParseManifest 的 heading 块；行号从 1 起；代码围栏内的 # 行不视为标题）。
// 最多保留前 200 个条目。
func documentOutlineOf(markdown string) []documentOutlineEntry {
	entries := make([]documentOutlineEntry, 0)
	stack := make([]string, 0, 8)
	line := 1
	inFence := false
	for _, text := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimLeft(text, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			line++
			continue
		}
		if !inFence {
			if level, title, ok := markdownHeadingOf(trimmed); ok {
				for len(stack) >= level {
					stack = stack[:len(stack)-1]
				}
				stack = append(stack, title)
				entries = append(entries, documentOutlineEntry{Path: append([]string(nil), stack...), Line: line})
				if len(entries) >= mcpOutlineMaxEntries {
					break
				}
			}
		}
		line++
	}
	return entries
}

// markdownHeadingOf 解析 ATX 标题行（#~######），返回层级与标题文本。
func markdownHeadingOf(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' && level < 6 {
		level++
	}
	if level == 0 || level >= len(line) || (line[level] != ' ' && line[level] != '\t') {
		return 0, "", false
	}
	title := strings.TrimSpace(line[level:])
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}
