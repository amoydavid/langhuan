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

// APIKeyServiceHTTP 是 handler 侧的 API Key 服务接口，由 service.APIKeyService 实现。
type APIKeyServiceHTTP interface {
	Create(ctx context.Context, input service.CreateAPIKeyInput) (*service.CreateAPIKeyResult, error)
	Get(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole, keyID uuid.UUID) (dto.WorkspaceAPIKey, error)
	List(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole) ([]dto.WorkspaceAPIKey, error)
	Reveal(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole, keyID uuid.UUID) (*service.RevealResult, error)
	Revoke(ctx context.Context, workspaceID, actorID uuid.UUID, actorRole value.WorkspaceRole, keyID uuid.UUID) error
	PublicURLs() dto.PublicURLs
}

type apiKeyHandler struct {
	keys APIKeyServiceHTTP
}

// createAPIKeyRequest 是创建 API Key 的请求体。expiration 省略时默认 90 天。
type createAPIKeyRequest struct {
	Name             string                    `json:"name"`
	KnowledgeBaseIDs []uuid.UUID               `json:"knowledge_base_ids"`
	Scopes           []value.APIScope          `json:"scopes"`
	Expiration       *service.APIKeyExpiration `json:"expiration"`
}

// list (admin+) 列出 Workspace 内全部 API Key 的安全视图。
func (h apiKeyHandler) list(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	items, err := h.keys.List(c.Request.Context(), authCtx.WorkspaceID, authCtx.Role)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if items == nil {
		items = []dto.WorkspaceAPIKey{}
	}
	urls := h.keys.PublicURLs()
	c.JSON(stdhttp.StatusOK, dto.WorkspaceAPIKeyListEnvelope{
		Items: items, BaseURL: urls.BaseURL, RESTBaseURL: urls.RESTBaseURL, MCPURL: urls.MCPURL,
	})
}

// create (admin+) 创建 API Key，返回一次性明文与安全 item。
func (h apiKeyHandler) create(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	var req createAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	expiration := service.APIKeyExpiration{}
	if req.Expiration != nil {
		expiration = *req.Expiration
	}
	result, err := h.keys.Create(c.Request.Context(), service.CreateAPIKeyInput{
		WorkspaceID:      authCtx.WorkspaceID,
		ActorID:          authCtx.UserID,
		ActorRole:        authCtx.Role,
		Name:             req.Name,
		KnowledgeBaseIDs: req.KnowledgeBaseIDs,
		Scopes:           req.Scopes,
		Expiration:       expiration,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, struct {
		APIKey      string              `json:"api_key"`
		Item        dto.WorkspaceAPIKey `json:"item"`
		BaseURL     string              `json:"base_url"`
		RESTBaseURL string              `json:"rest_base_url"`
		MCPURL      string              `json:"mcp_url"`
	}{
		APIKey: result.APIKey, Item: result.Item,
		BaseURL: result.URLs.BaseURL, RESTBaseURL: result.URLs.RESTBaseURL, MCPURL: result.URLs.MCPURL,
	})
}

// get (admin+) 返回单条 API Key 的安全视图。
func (h apiKeyHandler) get(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	keyID, err := uuid.Parse(c.Param("api_key_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "api_key_id 必须是有效 UUID")
		return
	}
	item, err := h.keys.Get(c.Request.Context(), authCtx.WorkspaceID, authCtx.Role, keyID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	urls := h.keys.PublicURLs()
	c.JSON(stdhttp.StatusOK, dto.WorkspaceAPIKeyDetailEnvelope{
		Item: item, BaseURL: urls.BaseURL, RESTBaseURL: urls.RESTBaseURL, MCPURL: urls.MCPURL,
	})
}

// reveal (admin+, Session-only) 重新获取 API Key 明文。
// 响应固定带 no-store/no-cache，不进入任何持久缓存。
func (h apiKeyHandler) reveal(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	// API Key 凭证不能 reveal 自己：reveal 是 Session-only 操作。
	if authCtx.IsAPIKey() {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "reveal 只允许 Session owner/admin")
		return
	}
	keyID, err := uuid.Parse(c.Param("api_key_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "api_key_id 必须是有效 UUID")
		return
	}
	result, err := h.keys.Reveal(c.Request.Context(), authCtx.WorkspaceID, authCtx.Role, keyID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(stdhttp.StatusOK, dto.WorkspaceAPIKeySecretEnvelope{
		APIKey: result.APIKey, BaseURL: result.URLs.BaseURL, RESTBaseURL: result.URLs.RESTBaseURL, MCPURL: result.URLs.MCPURL,
	})
}

// revoke (admin+) 吊销 API Key，幂等返回 204。
func (h apiKeyHandler) revoke(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	keyID, err := uuid.Parse(c.Param("api_key_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "api_key_id 必须是有效 UUID")
		return
	}
	if err := h.keys.Revoke(c.Request.Context(), authCtx.WorkspaceID, authCtx.UserID, authCtx.Role, keyID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

// strings import guard for future trimming helpers.
var _ = strings.TrimSpace
