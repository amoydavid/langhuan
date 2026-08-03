package http

import (
	"context"
	"net"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// AuthService is the handler-side auth service interface. It is structurally
// satisfied by service.AuthService and also satisfies SessionAuthenticator
// (middleware) via Authenticate.
type AuthService interface {
	Login(ctx context.Context, email, password, userAgent, ipAddr string) (*model.Session, error)
	Logout(ctx context.Context, sessionID uuid.UUID) error
	Authenticate(ctx context.Context, sessionID uuid.UUID) (*model.User, error)
}

// UserService is the handler-side user service interface.
type UserService interface {
	IsInitialized(ctx context.Context) (bool, error)
	RegisterFirstUser(ctx context.Context, email, nickname, password string) (*dto.AuthenticatedUser, error)
	ResetPassword(ctx context.Context, actorUserID uuid.UUID, actorIsPlatformAdmin bool, targetUserID uuid.UUID, newPassword string) error
	GetByID(ctx context.Context, userID uuid.UUID) (*dto.AuthenticatedUser, error)
}

// authHandler exposes the /auth register/login/logout/me endpoints.
// It only depends on services + AuthContext; it never touches DB/Redis directly.
type authHandler struct {
	auth        AuthService
	users       UserService
	invitations InvitationService
	memberships MembershipService
	workspaces  WorkspaceService
	sessionCfg  config.SessionConfig
}

// bootstrapStatus reports whether first-user setup has already completed.
func (h authHandler) bootstrapStatus(c *gin.Context) {
	initialized, err := h.users.IsInitialized(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"initialized": initialized})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	Email           string `json:"email"`
	Nickname        string `json:"nickname"`
	Password        string `json:"password"`
	InvitationToken string `json:"invitation_token"`
}

// login authenticates the user and sets a hardened session cookie.
// The cookie is HttpOnly + Secure(prod) + SameSite=Lax + scoped Domain + max-age=lifetime.
// The session id NEVER appears in the URL or the response body.
func (h authHandler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "email 与 password 不能为空")
		return
	}

	session, err := h.auth.Login(c.Request.Context(), req.Email, req.Password, c.Request.UserAgent(), clientIP(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	h.setSessionCookie(c, session.ID.String())
	c.JSON(stdhttp.StatusOK, gin.H{"user_id": session.UserID})
}

// register handles BOTH first-user registration (no token) and invitation
// acceptance (with invitation_token). First-user registration does NOT create
// a session (per spec); invitation acceptance creates a session and sets the
// cookie.
func (h authHandler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return
	}
	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Nickname) == "" || req.Password == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "email、nickname 与 password 不能为空")
		return
	}

	if strings.TrimSpace(req.InvitationToken) != "" {
		// Invitation acceptance: creates user + membership + session in one transaction.
		session, err := h.invitations.Accept(
			c.Request.Context(),
			req.InvitationToken,
			req.Email,
			req.Nickname,
			req.Password,
			c.Request.UserAgent(),
			clientIP(c),
		)
		if err != nil {
			writeServiceError(c, err)
			return
		}
		h.setSessionCookie(c, session.ID.String())
		// Invitees are never platform admins.
		c.JSON(stdhttp.StatusCreated, dto.AuthenticatedUser{
			ID:              session.UserID,
			Email:           req.Email,
			Nickname:        req.Nickname,
			IsPlatformAdmin: false,
		})
		return
	}

	// First-user registration: no session is created.
	user, err := h.users.RegisterFirstUser(c.Request.Context(), req.Email, req.Nickname, req.Password)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, user)
}

// logout deletes the session referenced by the cookie and clears the cookie.
// Missing/invalid cookie yields 401 (SessionAuth middleware enforces this).
func (h authHandler) logout(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	// SessionAuth already validated the cookie; re-read it to obtain the session id.
	cookieValue, _ := c.Cookie(h.sessionCfg.CookieName)
	sessionID, err := uuid.Parse(cookieValue)
	if err != nil {
		writeError(c, stdhttp.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	_ = authCtx
	if err := h.auth.Logout(c.Request.Context(), sessionID); err != nil {
		writeServiceError(c, err)
		return
	}
	h.clearSessionCookie(c)
	c.Status(stdhttp.StatusNoContent)
}

// meResponse is the body returned by GET /api/v1/auth/me.
type meResponse struct {
	User       *dto.AuthenticatedUser `json:"user"`
	Workspaces []workspaceSummary     `json:"workspaces"`
}

type workspaceSummary struct {
	WorkspaceID uuid.UUID           `json:"workspace_id"`
	Slug        string              `json:"slug"`
	Name        string              `json:"name"`
	Role        value.WorkspaceRole `json:"role"`
}

// me returns the authenticated user's profile plus a summary of the workspaces
// they belong to (id/slug/name/role).
func (h authHandler) me(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	user, err := h.users.GetByID(c.Request.Context(), authCtx.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	resp := meResponse{User: user}
	if h.memberships != nil {
		memberships, err := h.memberships.ListForUser(c.Request.Context(), authCtx.UserID)
		if err != nil {
			writeServiceError(c, err)
			return
		}
		summaries := make([]workspaceSummary, 0, len(memberships))
		for _, m := range memberships {
			summary := workspaceSummary{
				WorkspaceID: m.WorkspaceID,
				Role:        m.Role,
			}
			// Best-effort enrichment of slug/name per membership.
			if h.workspaces != nil {
				if ws, werr := h.workspaces.Get(c.Request.Context(), m.WorkspaceID); werr == nil && ws != nil {
					summary.Slug = ws.Slug
					summary.Name = ws.Name
				}
			}
			summaries = append(summaries, summary)
		}
		resp.Workspaces = summaries
	}
	c.JSON(stdhttp.StatusOK, resp)
}

// setSessionCookie writes the hardened session cookie.
func (h authHandler) setSessionCookie(c *gin.Context, sessionID string) {
	lifetime := time.Duration(h.sessionCfg.LifetimeSeconds) * time.Second
	if lifetime <= 0 {
		lifetime = 0
	}
	c.SetSameSite(stdhttp.SameSiteLaxMode)
	c.SetCookie(
		h.sessionCfg.CookieName,
		sessionID,
		int(lifetime.Seconds()),
		"/",
		h.sessionCfg.Domain,
		h.sessionCfg.SecureCookie,
		true, // HttpOnly
	)
}

// clearSessionCookie expires the session cookie immediately.
func (h authHandler) clearSessionCookie(c *gin.Context) {
	c.SetSameSite(stdhttp.SameSiteLaxMode)
	c.SetCookie(
		h.sessionCfg.CookieName,
		"",
		-1,
		"/",
		h.sessionCfg.Domain,
		h.sessionCfg.SecureCookie,
		true, // HttpOnly
	)
}

// clientIP extracts the client IP from RemoteAddr, stripping the port via
// net.SplitHostPort. Falls back to the raw RemoteAddr when parsing fails.
func clientIP(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}
