package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ===== web_source_list / web_source_get / web_source_create / web_source_update / web_source_sync / web_source_delete =====
//
// web 内容源（kind=web）按 config.mode 分三种：crawl（种子 URL 爬取）、sitemap、
// urls（固定 URL 清单）。create/update 成功即 best-effort 入队一次同步（入队失败
// 不回滚，可手动 sync 重试）；手动 sync 也是入队即返回。config 为对象透传，
// 由 service 层 ParseWebSourceConfig 全量校验（未知键拒绝）。业务规则全部在
// KbSourceService，本文件只做协议转换；语义与 REST /web-sources 完全对齐。

type webSourceListInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
}

type webSourceGetInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	SourceID        string `json:"source_id" jsonschema:"网站源 ID"`
}

type webSourceCreateInput struct {
	KnowledgeBaseID string         `json:"knowledge_base_id" jsonschema:"目标知识库 ID"`
	Name            string         `json:"name" jsonschema:"源名称（展示用，如“官方文档站”）"`
	Config          map[string]any `json:"config" jsonschema:"源配置对象：必填 mode（crawl/sitemap/urls）；crawl 需 crawl.seed_url，sitemap 需 sitemap.url，urls 需 urls 数组；可选 fetch（engine/connection_id）、extract、sync.cron 等——建议先 web_source_get 参考既有源的 config 形状"`
}

type webSourceUpdateInput struct {
	KnowledgeBaseID string         `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	SourceID        string         `json:"source_id" jsonschema:"网站源 ID"`
	Name            *string        `json:"name,omitempty" jsonschema:"新名称"`
	Status          *string        `json:"status,omitempty" jsonschema:"新状态：active / disabled"`
	Config          map[string]any `json:"config,omitempty" jsonschema:"partial config 合并更新（urls 为整体替换语义）；mode、crawl.seed_url、sitemap.url、fetch.engine、fetch.connection_id 创建后不可变"`
}

type webSourceSyncInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	SourceID        string `json:"source_id" jsonschema:"网站源 ID"`
	Force           bool   `json:"force,omitempty" jsonschema:"true 时忽略增量游标全量重爬（默认 false 增量）"`
}

type webSourceDeleteInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	SourceID        string `json:"source_id" jsonschema:"网站源 ID"`
	Pages           string `json:"pages,omitempty" jsonschema:"已抓取页面的处置：keep（默认，仅停用源）/ remove（连带软删页面文档）"`
}

type webSourceListItem struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Status          string         `json:"status"`
	Mode            string         `json:"mode"`
	PageCount       int            `json:"page_count"`
	FailedPageCount int            `json:"failed_page_count"`
	NextSyncAt      string         `json:"next_sync_at,omitempty"`
	SyncLastResult  map[string]any `json:"sync_last_result,omitempty"`
}

type webSourceListOutput struct {
	Sources []webSourceListItem `json:"sources"`
}

type webSourceDetail struct {
	webSourceListItem
	Config    map[string]any `json:"config"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type webSourceMutationOutput struct {
	SourceID string `json:"source_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Mode     string `json:"mode"`
	// JobID 是创建/更新后自动入队的首次同步任务（best-effort，可能省略——
	// 省略时同步未入队，可用 web_source_sync 手动触发）。
	JobID string `json:"job_id,omitempty"`
}

type webSourceSyncOutput struct {
	JobID string `json:"job_id"`
}

type webSourceDeleteOutput struct {
	SourceID string `json:"source_id"`
	Deleted  bool   `json:"deleted"`
}

// webSourceModeOf 从源 config 投影提取模式（crawl/sitemap/urls）；缺失返回空串。
func webSourceModeOf(config map[string]any) string {
	mode, _ := config["mode"].(string)
	return mode
}

func formatMCPTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func webSourceListItemOf(source *model.KbSource, pageCount, failedPageCount int) webSourceListItem {
	return webSourceListItem{
		ID: source.ID.String(), Name: source.Name, Status: string(source.Status),
		Mode:      webSourceModeOf(source.Config),
		PageCount: pageCount, FailedPageCount: failedPageCount,
		NextSyncAt:     formatMCPTime(source.NextSyncAt),
		SyncLastResult: source.SyncLastResult,
	}
}

func registerWebSourceTools(srv *mcpserver.MCPServer, deps Dependencies) {
	registerWebSourceList(srv, deps)
	registerWebSourceGet(srv, deps)
	registerWebSourceCreate(srv, deps)
	registerWebSourceUpdate(srv, deps)
	registerWebSourceSync(srv, deps)
	registerWebSourceDelete(srv, deps)
}

func registerWebSourceList(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("web_source_list",
		mcp.WithDescription("列出知识库的全部 web 内容源（模式、状态、页面数、下次同步时间）。"+
			"往 urls 模式源加单个网页的快捷方式是 document_ingest 的 url 模式。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[webSourceListInput](),
		withRawOutputSchema[webSourceListOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in webSourceListInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		views, err := deps.WebSources.ListWebSources(ctx, auth.ResourceAccess(), kbID)
		if err != nil {
			return toErrorResult(err), nil
		}
		items := make([]webSourceListItem, 0, len(views))
		for _, view := range views {
			if view.Source == nil {
				continue
			}
			items = append(items, webSourceListItemOf(view.Source, view.PageCount, view.FailedPageCount))
		}
		return jsonResult(webSourceListOutput{Sources: items}), nil
	}))
}

func registerWebSourceGet(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("web_source_get",
		mcp.WithDescription("获取一个 web 内容源的完整详情（含 config 原文）。"+
			"创建新源前建议先查看既有源的 config 形状作为参考。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[webSourceGetInput](),
		withRawOutputSchema[webSourceDetail](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in webSourceGetInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		sourceID, err := uuid.Parse(in.SourceID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: source_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		view, err := deps.WebSources.GetWebSource(ctx, auth.ResourceAccess(), kbID, sourceID)
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(webSourceDetail{
			webSourceListItem: webSourceListItemOf(view.Source, view.PageCount, view.FailedPageCount),
			Config:            view.Source.Config,
			CreatedAt:         formatMCPTime(&view.Source.CreatedAt),
			UpdatedAt:         formatMCPTime(&view.Source.UpdatedAt),
		}), nil
	}))
}

func registerWebSourceCreate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("web_source_create",
		mcp.WithDescription("创建一个 web 内容源并自动触发首次同步（异步，返回 job_id，用 document_status 或 web_source_get 观察抓取结果）。"+
			"config.mode 三选一：crawl（从 seed_url 按深度/数量爬取）、sitemap（按站点地图抓取）、urls（固定 URL 清单，后续可用 document_ingest 的 url 模式追加）。"+
			"config 结构与 REST /web-sources 完全一致，非法键会被拒绝并提示原因。"),
		withRawInputSchema[webSourceCreateInput](),
		withRawOutputSchema[webSourceMutationOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in webSourceCreateInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		source, job, err := deps.WebSources.CreateWebSource(ctx, service.CreateWebSourceInput{
			WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID,
			Name: in.Name, Config: in.Config,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(webSourceMutationOf(source, job)), nil
	}))
}

func registerWebSourceUpdate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("web_source_update",
		mcp.WithDescription("更新 web 内容源的名称/启停状态/配置（partial config 合并语义，urls 整体替换），成功后自动入队一次同步。"+
			"mode、crawl.seed_url、sitemap.url、fetch.engine、fetch.connection_id 创建后不可变，尝试修改会被拒绝。"),
		withRawInputSchema[webSourceUpdateInput](),
		withRawOutputSchema[webSourceMutationOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in webSourceUpdateInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		sourceID, err := uuid.Parse(in.SourceID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: source_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if in.Name == nil && in.Status == nil && in.Config == nil {
			return toErrorResult(fmt.Errorf("%w: name/status/config 至少提供一个", domainerrors.ErrValidation)), nil
		}
		var status *value.KbSourceStatus
		if in.Status != nil {
			parsed := value.KbSourceStatus(*in.Status)
			if parsed != value.KbSourceStatusActive && parsed != value.KbSourceStatusDisabled {
				return toErrorResult(fmt.Errorf("%w: status 只能是 active 或 disabled", domainerrors.ErrValidation)), nil
			}
			status = &parsed
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		source, job, err := deps.WebSources.UpdateWebSource(ctx, service.UpdateWebSourceInput{
			WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID, SourceID: sourceID,
			Name: in.Name, Status: status, Config: in.Config,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(webSourceMutationOf(source, job)), nil
	}))
}

func registerWebSourceSync(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("web_source_sync",
		mcp.WithDescription("手动触发一次 web 内容源同步（异步入队，立即返回 job_id）。"+
			"默认增量（只抓有变化的页面）；force=true 忽略游标全量重爬。同步进度和结果可通过 web_source_get 的 sync_last_result 或 document_status 观察。"),
		withRawInputSchema[webSourceSyncInput](),
		withRawOutputSchema[webSourceSyncOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in webSourceSyncInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		sourceID, err := uuid.Parse(in.SourceID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: source_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		job, err := deps.WebSources.SyncWebSource(ctx, auth.WorkspaceID, kbID, sourceID, in.Force)
		if err != nil {
			return toErrorResult(err), nil
		}
		jobID := ""
		if job != nil {
			jobID = job.ID.String()
		}
		return jsonResult(webSourceSyncOutput{JobID: jobID}), nil
	}))
}

func registerWebSourceDelete(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("web_source_delete",
		mcp.WithDescription("软删除一个 web 内容源。pages=keep（默认）保留已抓取的页面文档（仅停用源，文档仍可检索）；"+
			"pages=remove 连带软删该源抓取的全部页面文档（不可逆，删除前请确认）。"),
		mcp.WithDestructiveHintAnnotation(true),
		withRawInputSchema[webSourceDeleteInput](),
		withRawOutputSchema[webSourceDeleteOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in webSourceDeleteInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeKnowledgeBasesWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		sourceID, err := uuid.Parse(in.SourceID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: source_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		pages := in.Pages
		if pages == "" {
			pages = "keep"
		}
		if pages != "keep" && pages != "remove" {
			return toErrorResult(fmt.Errorf("%w: pages 只能是 keep 或 remove", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		if err := deps.WebSources.DeleteWebSource(ctx, auth.WorkspaceID, kbID, sourceID, pages); err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(webSourceDeleteOutput{SourceID: sourceID.String(), Deleted: true}), nil
	}))
}

// webSourceMutationOf 组装 create/update 的共用输出（job 为 nil 时省略 job_id）。
func webSourceMutationOf(source *model.KbSource, job *model.Job) webSourceMutationOutput {
	out := webSourceMutationOutput{
		SourceID: source.ID.String(), Name: source.Name,
		Status: string(source.Status), Mode: webSourceModeOf(source.Config),
	}
	if job != nil {
		out.JobID = job.ID.String()
	}
	return out
}
