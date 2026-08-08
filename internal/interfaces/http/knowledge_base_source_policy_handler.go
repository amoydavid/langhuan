package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSourcePolicyService 是来源删除策略更新的 HTTP 层契约。
// 生产装配用一个薄适配器包裹 *service.KnowledgeBaseService.UpdateSourceDeletePolicy。
type KnowledgeBaseSourcePolicyService interface {
	UpdateSourceDeletePolicy(ctx context.Context, workspaceID, kbID uuid.UUID, policy value.SourceDeletePolicy) error
}

// knowledgeBaseSourcePolicyHandler 处理来源删除策略的 PATCH 请求。
type knowledgeBaseSourcePolicyHandler struct {
	service KnowledgeBaseSourcePolicyService
}

// sourcePolicyRequest 是更新删除策略的请求体；只接受 on_delete 单字段，
// 不接受整个 source_config（未知字段由 decodeStrictJSON 拒绝 → 400）。
type sourcePolicyRequest struct {
	OnDelete string `json:"on_delete"`
}

// sourcePolicyResponse 返回更新后的策略值，便于前端即时回显。
type sourcePolicyResponse struct {
	OnDelete string `json:"on_delete"`
}

// update 仅更新 source_config.on_delete，保留其余运行期键。
//
// PATCH /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/source-policy
//
// 鉴权：要求 workspace admin/owner（路由层 RequireAdminForSession 强制；
// handler 内再做一次角色校验，与 knowledgeBaseSyncHandler 对齐）。
func (h knowledgeBaseSourcePolicyHandler) update(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if !authCtx.Role.AtLeast(value.RoleAdmin) {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}

	var req sourcePolicyRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	// 严格解析：空值或未知值（purge 等）返回 ErrValidation → 400。
	policy, err := value.ParseSourceDeletePolicy(req.OnDelete)
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "on_delete 必须是 keep 或 remove")
		return
	}

	if err := h.service.UpdateSourceDeletePolicy(c.Request.Context(), authCtx.WorkspaceID, knowledgeBaseID, policy); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, sourcePolicyResponse{OnDelete: policy.String()})
}
