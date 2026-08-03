package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// InvitationService is the handler-side invitation service interface.
type InvitationService interface {
	List(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole) ([]*dto.InvitationListItem, error)
	Create(ctx context.Context, input service.CreateInvitationInput) (*dto.Invitation, string, error)
	GetPublic(ctx context.Context, plaintextToken string) (*dto.PublicInvitation, error)
	Accept(ctx context.Context, plaintextToken, email, nickname, password, userAgent, ipAddr string) (*model.Session, error)
	Revoke(ctx context.Context, invitationID, actorUserID uuid.UUID, actorRole value.WorkspaceRole, isPlatformAdmin bool) error
}

// list 返回当前 workspace 的邀请管理列表。
func (h invitationHandler) list(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	invitations, err := h.invitations.List(c.Request.Context(), authCtx.WorkspaceID, authCtx.Role)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if invitations == nil {
		invitations = []*dto.InvitationListItem{}
	}
	c.JSON(stdhttp.StatusOK, invitations)
}

// invitationHandler exposes the public invitation query, the workspace-scoped
// invitation list/create/revoke, and the platform-admin revoke-any endpoint.
type invitationHandler struct {
	invitations InvitationService
	publicURLs  *service.PublicURLBuilder
}

// getPublic returns the public (no-hash, no-plaintext-token) view of a pending
// invitation. Expired/accepted/revoked/unknown invitations all yield 404 so the
// status cannot be enumerated.
func (h invitationHandler) getPublic(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" {
		writeError(c, stdhttp.StatusNotFound, "not_found", "邀请不存在")
		return
	}
	invitation, err := h.invitations.GetPublic(c.Request.Context(), token)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, invitation)
}

type createInvitationRequest struct {
	InvitedEmail string `json:"invited_email"`
	Role         string `json:"role"`
}

// createInvitationResponse mirrors dto.Invitation plus the one-time invite_url.
// The plaintext token appears ONLY in invite_url; it is never persisted or
// returned in any other field.
type createInvitationResponse struct {
	ID           uuid.UUID           `json:"id"`
	InvitedEmail string              `json:"invited_email"`
	Role         value.WorkspaceRole `json:"role"`
	ExpiresAt    time.Time           `json:"expires_at"`
	TokenPrefix  string              `json:"token_prefix"`
	InviteURL    string              `json:"invite_url"`
}

// create (workspace admin+) creates a new invitation. The plaintext token is
// embedded solely in the returned invite_url.
func (h invitationHandler) create(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}

	var req createInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.InvitedEmail) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "invited_email 不能为空")
		return
	}
	role := value.WorkspaceRole(strings.TrimSpace(req.Role))
	if !role.IsValid() {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "role 必须是 owner/admin/member 之一")
		return
	}

	invitation, plaintextToken, err := h.invitations.Create(c.Request.Context(), service.CreateInvitationInput{
		WorkspaceID:  authCtx.WorkspaceID,
		InvitedEmail: req.InvitedEmail,
		Role:         role,
		CreatedBy:    authCtx.UserID,
		ActorRole:    authCtx.Role,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	inviteURL := h.inviteURL(c, plaintextToken)
	c.JSON(stdhttp.StatusCreated, createInvitationResponse{
		ID:           invitation.ID,
		InvitedEmail: invitation.InvitedEmail,
		Role:         invitation.Role,
		ExpiresAt:    invitation.ExpiresAt,
		TokenPrefix:  invitation.TokenPrefix,
		InviteURL:    inviteURL,
	})
}

func (h invitationHandler) inviteURL(c *gin.Context, token string) string {
	path := "/invitations/" + url.PathEscape(token)
	if h.publicURLs != nil {
		return h.publicURLs.Resolve(path)
	}
	// 仅在测试或未配置 PublicURLs 时回退，生产路径始终经过全局 base_url 派生。
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + path
}

// revoke (workspace-scoped, creator admin+) revokes an invitation. Authorization
// (owner any / admin own / platform admin any) is enforced by the service.
func (h invitationHandler) revoke(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	invitationID, err := uuid.Parse(c.Param("invitation_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "invitation_id 必须是有效 UUID")
		return
	}
	if err := h.invitations.Revoke(c.Request.Context(), invitationID, authCtx.UserID, authCtx.Role, authCtx.IsPlatformAdmin); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

// revokeAny (platform admin) revokes any invitation regardless of workspace/creator.
func (h invitationHandler) revokeAny(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	invitationID, err := uuid.Parse(c.Param("invitation_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "invitation_id 必须是有效 UUID")
		return
	}
	if err := h.invitations.Revoke(c.Request.Context(), invitationID, authCtx.UserID, authCtx.Role, authCtx.IsPlatformAdmin); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}
