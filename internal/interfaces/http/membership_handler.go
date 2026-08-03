package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

// MembershipService is the handler-side membership service interface. It also
// satisfies MembershipResolver (middleware) via Get.
type MembershipService interface {
	List(ctx context.Context, workspaceID uuid.UUID) ([]*dto.Membership, error)
	Get(ctx context.Context, workspaceID, userID uuid.UUID) (*dto.Membership, error)
	ChangeRole(ctx context.Context, workspaceID, targetUserID uuid.UUID, newRole value.WorkspaceRole, actorRole value.WorkspaceRole) (*dto.Membership, error)
	Remove(ctx context.Context, workspaceID, targetUserID uuid.UUID, actorRole value.WorkspaceRole) error
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*dto.Membership, error)
}

// membershipHandler exposes the workspace-scoped member list / role change / remove endpoints.
type membershipHandler struct {
	memberships MembershipService
}

// list returns the workspace's memberships. The workspace id comes from AuthContext
// (resolved by RequireWorkspace from the slug).
func (h membershipHandler) list(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	memberships, err := h.memberships.List(c.Request.Context(), authCtx.WorkspaceID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, memberships)
}

type changeMemberRoleRequest struct {
	Role string `json:"role"`
}

// changeRole (owner only) changes a member's role. Authorization is enforced
// both by the RequireWorkspaceRole(RoleOwner) middleware and defensively by the service.
func (h membershipHandler) changeRole(c *gin.Context) {
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
	var req changeMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	newRole := value.WorkspaceRole(strings.TrimSpace(req.Role))
	if !newRole.IsValid() {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "role 必须是 owner/admin/member 之一")
		return
	}
	updated, err := h.memberships.ChangeRole(c.Request.Context(), authCtx.WorkspaceID, targetUserID, newRole, authCtx.Role)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, updated)
}

// remove (owner only) removes a member from the workspace.
func (h membershipHandler) remove(c *gin.Context) {
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
	if err := h.memberships.Remove(c.Request.Context(), authCtx.WorkspaceID, targetUserID, authCtx.Role); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}
