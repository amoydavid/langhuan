package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SourceConnectionService 是来源连接（feishu/firecrawl/jina/browser）管理的
// handler 合同。DTO 不含 secret，明文 AppSecret 仅作为 create/update 的入参
// 进入 service。
type SourceConnectionService interface {
	Create(ctx context.Context, input service.CreateSourceConnectionInput) (*dto.SourceConnection, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]dto.SourceConnection, error)
	Get(ctx context.Context, workspaceID, id uuid.UUID) (*dto.SourceConnection, error)
	Update(ctx context.Context, input service.UpdateSourceConnectionInput) (*dto.SourceConnection, error)
	Delete(ctx context.Context, workspaceID, id uuid.UUID) error
	// TestConnection 做一次非持久化连通性探测（spec §9.4）。契约取舍（勿与
	// 模型连接测试统一）：探测失败是「结果」→ HTTP 200 + {ok:false, message}；
	// error 仅表达「无法测试」（不存在→404、已停用/feishu 不支持→400）。
	TestConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) (*dto.SourceConnectionTestResult, error)
}

type sourceConnectionHandler struct {
	svc SourceConnectionService
}

// createSourceConnectionRequest 是 POST /source-connections 的请求体。
// AppSecret 仅在此处短暂出现，绝不会被持久化到 DTO 或响应中。
// Config 承载 provider 相关的非敏感配置（firecrawl endpoint 覆盖 / browser
// CDP endpoint_url），键约束由领域构造函数按 provider 校验。
type createSourceConnectionRequest struct {
	Provider  string         `json:"provider"`
	Name      string         `json:"name"`
	AppID     string         `json:"app_id"`
	AppSecret string         `json:"app_secret"`
	Config    map[string]any `json:"config"`
}

// updateSourceConnectionRequest 是 PATCH /source-connections/:connection_id 的请求体。
// 三个字段均为可选；AppSecret 非空时触发凭证轮换。
type updateSourceConnectionRequest struct {
	Name      *string `json:"name"`
	Status    *string `json:"status"`
	AppSecret *string `json:"app_secret"`
}

// create 创建来源连接。仅 workspace admin 可达（路由层强制）。
// provider 矩阵与领域构造函数同口径（spec §3.1）：app_id 仅 feishu 必填且其它
// provider 提供即 400；凭证 feishu/firecrawl/jina 必填、browser 可空（token 可选）。
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
	provider := value.SourceProvider(strings.ToLower(strings.TrimSpace(req.Provider)))
	if err := provider.Validate(); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "provider 必须为 feishu/firecrawl/jina/browser")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 不能为空")
		return
	}
	if provider.RequiresAppID() {
		if strings.TrimSpace(req.AppID) == "" {
			writeError(c, stdhttp.StatusBadRequest, "validation_error", "app_id 不能为空")
			return
		}
	} else if strings.TrimSpace(req.AppID) != "" {
		// 与领域构造函数同一口径：显式拒绝优于静默丢弃。
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "app_id 仅飞书连接需要")
		return
	}
	if provider.RequiresCredentials() && req.AppSecret == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "app_secret 不能为空")
		return
	}
	result, err := h.svc.Create(c.Request.Context(), service.CreateSourceConnectionInput{
		WorkspaceID: authCtx.WorkspaceID, Provider: string(provider), Name: req.Name,
		AppID: req.AppID, AppSecret: req.AppSecret, Config: req.Config,
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

// testConnection 对来源连接做一次非持久化连通性探测（spec §9.4，T13 接入 REST）。
// 仅 workspace admin（Session）可达，与连接管理路由同中间件链。
//
// 响应契约（T12 评审移交 b，显式与模型连接测试不同）：
//   - 探测成功与探测失败都返回 200 + dto.SourceConnectionTestResult；ok=false
//     时失败原因在 message（不含凭证），前端按 ok 字段分支展示；
//   - 「无法测试」才走 error：连接不存在→404（ErrNotFound）、连接已停用→400
//     （ErrValidation，启用后可重试）、feishu 不支持测试→400；
//   - 对照：模型连接测试（POST .../models/:model_id/test）失败即 4xx/5xx error body。
func (h sourceConnectionHandler) testConnection(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	connectionID, ok := parseUUIDParam(c, "connection_id")
	if !ok {
		return
	}
	result, err := h.svc.TestConnection(c.Request.Context(), authCtx.WorkspaceID, connectionID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}
