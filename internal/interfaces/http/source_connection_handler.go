package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
)

// sourceProviderFeishu 是当前唯一支持的来源 provider。
// 与 model.SourceProviderFeishu 对齐；这里保持本地常量避免 handler 反向依赖领域层。
const sourceProviderFeishu = "feishu"

// SourceConnectionService 是飞书（及未来其它来源）应用连接管理的 handler 合同。
// DTO 不含 secret，明文 AppSecret 仅作为 create/update 的入参进入 service。
type SourceConnectionService interface {
	Create(ctx context.Context, input service.CreateSourceConnectionInput) (*dto.SourceConnection, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]dto.SourceConnection, error)
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*dto.SourceConnection, error)
	Update(ctx context.Context, input service.UpdateSourceConnectionInput) (*dto.SourceConnection, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
}

type sourceConnectionHandler struct {
	svc SourceConnectionService
}

// createSourceConnectionRequest 是 POST /source-connections 的请求体。
// AppSecret 仅在此处短暂出现，绝不会被持久化到 DTO 或响应中。
type createSourceConnectionRequest struct {
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

// updateSourceConnectionRequest 是 PATCH /source-connections/:connection_id 的请求体。
// 三个字段均为可选；AppSecret 非空时触发凭证轮换。
type updateSourceConnectionRequest struct {
	Name      *string `json:"name"`
	Status    *string `json:"status"`
	AppSecret *string `json:"app_secret"`
}

// create 创建来源连接。仅 workspace admin 可达（路由层强制）。
func (h sourceConnectionHandler) create(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	var req createSourceConnectionRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider != sourceProviderFeishu {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "provider 必须为 feishu")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	if strings.TrimSpace(req.AppID) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "app_id 不能为空")
		return
	}
	if req.AppSecret == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "app_secret 不能为空")
		return
	}
	result, err := h.svc.Create(c.Request.Context(), service.CreateSourceConnectionInput{
		WorkspaceID: authCtx.WorkspaceID, Provider: provider, Name: req.Name,
		AppID: req.AppID, AppSecret: req.AppSecret,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, result)
}

// list 列出当前工作区下的全部来源连接（不含 secret）。
func (h sourceConnectionHandler) list(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	items, err := h.svc.List(c.Request.Context(), authCtx.WorkspaceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if items == nil {
		items = make([]dto.SourceConnection, 0)
	}
	c.JSON(stdhttp.StatusOK, items)
}

// get 读取单条来源连接（不含 secret）。
func (h sourceConnectionHandler) get(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	connectionID, ok := parseUUIDParam(c, "connection_id")
	if !ok {
		return
	}
	result, err := h.svc.Get(c.Request.Context(), authCtx.WorkspaceID, connectionID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

// update 更新连接的非敏感字段，或在 AppSecret 非空时轮换凭证。
func (h sourceConnectionHandler) update(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	connectionID, ok := parseUUIDParam(c, "connection_id")
	if !ok {
		return
	}
	var req updateSourceConnectionRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if req.Name == nil && req.Status == nil && req.AppSecret == nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "至少提供 name、status 或 app_secret")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	result, err := h.svc.Update(c.Request.Context(), service.UpdateSourceConnectionInput{
		WorkspaceID: authCtx.WorkspaceID, ID: connectionID,
		Name: req.Name, Status: req.Status, AppSecret: req.AppSecret,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

// delete 软删来源连接。
func (h sourceConnectionHandler) delete(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	connectionID, ok := parseUUIDParam(c, "connection_id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), authCtx.WorkspaceID, connectionID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}
