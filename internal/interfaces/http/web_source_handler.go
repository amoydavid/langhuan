package http

import (
	"bytes"
	"context"
	"io"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// WebSourceService 是 web 内容源管理（v1.3.0 spec §9.2）的 handler 合同。
// 生产实现是 *service.KbSourceService：键白名单、不可变键拒绝、config 全量
// 重校验、每库源数钳制、同步入队（best-effort）均在 service 层完成，本 handler
// 只做协议转换。Create/Update 返回的 *model.Job 是入队的同步任务（可为 nil）。
type WebSourceService interface {
	CreateWebSource(ctx context.Context, input service.CreateWebSourceInput) (*model.KbSource, *model.Job, error)
	ListWebSources(ctx context.Context, workspaceID, kbID uuid.UUID) ([]service.WebSourceView, error)
	GetWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID) (*service.WebSourceView, error)
	UpdateWebSource(ctx context.Context, input service.UpdateWebSourceInput) (*model.KbSource, *model.Job, error)
	SyncWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, force bool) (*model.Job, error)
	DeleteWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, pages string) error
}

type webSourceHandler struct {
	svc WebSourceService
}

// createWebSourceRequest 是 POST .../web-sources 的请求体。config 经 service 层
// value.ParseWebSourceConfig 做 typed 全量校验（含部署天花板），handler 不预校验。
type createWebSourceRequest struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config"`
}

// updateWebSourceRequest 是 PATCH .../web-sources/:source_id 的请求体，三键均可选。
// config 的顶层键白名单与不可变键（mode/crawl.seed_url/sitemap.url/fetch.engine/
// fetch.connection_id）拒绝在 service 层执行。
type updateWebSourceRequest struct {
	Name   *string        `json:"name"`
	Status *string        `json:"status"`
	Config map[string]any `json:"config"`
}

// webSourceSyncRequest 是 POST .../web-sources/:source_id/sync 的可选请求体；
// 空 body 视为 Force=false（与知识库手动同步同形）。
type webSourceSyncRequest struct {
	Force bool `json:"force"`
}

// webSourceViewResponse 是源列表/详情响应：源投影 + 页面计数（spec §9.2）。
type webSourceViewResponse struct {
	Source          *dto.KbSource `json:"source"`
	PageCount       int           `json:"page_count"`
	FailedPageCount int           `json:"failed_page_count"`
}

// webSourceMutationResponse 是创建/PATCH 响应：源 + 本次入队的同步 Job ID。
// job_id 可省略：入队是 best-effort（未配置入队器或入队失败时为 nil，调度器
// 稍后恢复派发），客户端不得假设必有 job_id。
type webSourceMutationResponse struct {
	Source *dto.KbSource `json:"source"`
	JobID  *uuid.UUID    `json:"job_id,omitempty"`
}

// webSourceSyncResponse 是手动同步响应：202 + job_id（与知识库手动同步同形）。
type webSourceSyncResponse struct {
	JobID *uuid.UUID `json:"job_id"`
}

// create 创建 web 内容源。写操作仅 workspace admin/owner（Session）或携带
// knowledge-bases:write scope 的 API Key（路由层强制）。
// 状态码：成功 201（响应体含 source 与可省略的 job_id）。
func (h webSourceHandler) create(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	var req createWebSourceRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	source, job, err := h.svc.CreateWebSource(c.Request.Context(), service.CreateWebSourceInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: kbID,
		Name: req.Name, Config: req.Config,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, webSourceMutationResponse{
		Source: dto.KbSourceFromModel(source), JobID: jobIDPtr(job),
	})
}

// list 列出 KB 下全部未软删源（含页面计数）。
// 读操作对 member 开放；Bearer 需 knowledge-bases:read scope + KB 绑定。
func (h webSourceHandler) list(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	views, err := h.svc.ListWebSources(c.Request.Context(), authCtx.WorkspaceID, kbID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	result := make([]webSourceViewResponse, 0, len(views))
	for _, view := range views {
		result = append(result, toWebSourceViewResponse(view))
	}
	c.JSON(stdhttp.StatusOK, result)
}

// get 读取单个源详情（含页面计数）。
func (h webSourceHandler) get(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDParam(c, "source_id")
	if !ok {
		return
	}
	view, err := h.svc.GetWebSource(c.Request.Context(), authCtx.WorkspaceID, kbID, sourceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, toWebSourceViewResponse(*view))
}

// update 按 PATCH 语义更新源（name/status/config 键；PATCH 成功即入队同步）。
// 状态码：成功 200（响应体含 source 与可省略的 job_id）。
func (h webSourceHandler) update(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDParam(c, "source_id")
	if !ok {
		return
	}
	var req updateWebSourceRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if req.Name == nil && req.Status == nil && req.Config == nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "至少提供 name、status 或 config 之一")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	input := service.UpdateWebSourceInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: kbID, SourceID: sourceID,
		Name: req.Name, Config: req.Config,
	}
	if req.Status != nil {
		status := value.KbSourceStatus(strings.ToLower(strings.TrimSpace(*req.Status)))
		if err := status.Validate(); err != nil {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "status 必须为 active/disabled")
			return
		}
		input.Status = &status
	}
	source, job, err := h.svc.UpdateWebSource(c.Request.Context(), input)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, webSourceMutationResponse{
		Source: dto.KbSourceFromModel(source), JobID: jobIDPtr(job),
	})
}

// sync 手动触发一次源同步（{"force":bool} 可选 body，force=全量重爬）。
// 同源单 active 合并在 service/入队层保证。状态码：202 Accepted + job_id。
func (h webSourceHandler) sync(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDParam(c, "source_id")
	if !ok {
		return
	}
	force := false
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "读取请求体失败")
		return
	}
	if len(bytes.TrimSpace(bodyBytes)) > 0 {
		var req webSourceSyncRequest
		if err := decodeStrictJSONReader(bytes.NewReader(bodyBytes), &req); err != nil {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求体格式错误")
			return
		}
		force = req.Force
	}
	job, err := h.svc.SyncWebSource(c.Request.Context(), authCtx.WorkspaceID, kbID, sourceID, force)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, webSourceSyncResponse{JobID: jobIDPtr(job)})
}

// delete 删除源并按 ?pages=keep|remove 处置页面（缺省 keep）。
// 状态码：成功 204；页面处置策略非法由 service 报 ErrValidation 映射 400。
func (h webSourceHandler) delete(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDParam(c, "source_id")
	if !ok {
		return
	}
	if err := h.svc.DeleteWebSource(c.Request.Context(), authCtx.WorkspaceID, kbID, sourceID, c.Query("pages")); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

// toWebSourceViewResponse 把 service 读投影转成响应体。
func toWebSourceViewResponse(view service.WebSourceView) webSourceViewResponse {
	return webSourceViewResponse{
		Source:          dto.KbSourceFromModel(view.Source),
		PageCount:       view.PageCount,
		FailedPageCount: view.FailedPageCount,
	}
}

// jobIDPtr 把可空的 Job 转成可省略的 job_id 指针（job 为 nil 时响应不含该键）。
func jobIDPtr(job *model.Job) *uuid.UUID {
	if job == nil {
		return nil
	}
	id := job.ID
	return &id
}
