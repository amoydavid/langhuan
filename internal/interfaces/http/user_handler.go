package http

import (
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// userHandler exposes platform-admin user-management endpoints (password reset).
type userHandler struct {
	users UserService
}

type passwordResetRequest struct {
	NewPassword string `json:"new_password"`
}

// resetPassword (platform admin only, enforced by RequirePlatformAdmin) resets a
// target user's password and revokes their sessions in one transaction.
func (h userHandler) resetPassword(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	targetUserID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "user_id 必须是有效 UUID")
		return
	}
	var req passwordResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "new_password 不能为空")
		return
	}
	if err := h.users.ResetPassword(c.Request.Context(), authCtx.UserID, authCtx.IsPlatformAdmin, targetUserID, req.NewPassword); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}
