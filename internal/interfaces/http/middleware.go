package http

import (
	"context"
	"errors"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// authContextKey is the gin context key under which SessionAuth stores the
// resolved value.AuthContext for downstream handlers and middleware.
const authContextKey = "auth"

// WorkspaceSlugParam is the URL path parameter carrying the workspace slug on
// workspace-scoped routes (e.g. /api/v1/workspaces/:workspace_slug/...).
const WorkspaceSlugParam = "workspace_slug"

// SessionAuthenticator resolves a session id to the authenticated user.
// It is satisfied by service.AuthService. Invalid/expired/revoked sessions
// must surface as ErrUnauthorized; genuine internal failures as other errors.
type SessionAuthenticator interface {
	Authenticate(ctx context.Context, sessionID uuid.UUID) (*model.User, error)
}

// WorkspaceResolver looks up a workspace by slug. A missing workspace must
// surface as ErrNotFound. Satisfied by service.WorkspaceService.
type WorkspaceResolver interface {
	GetBySlug(ctx context.Context, slug string) (*dto.Workspace, error)
}

// MembershipResolver looks up a user's membership in a workspace. No membership
// must surface as ErrNotFound. Satisfied by service.MembershipService.
type MembershipResolver interface {
	Get(ctx context.Context, workspaceID, userID uuid.UUID) (*dto.Membership, error)
}

// authFromContext returns the AuthContext set by SessionAuth. The boolean is
// false when SessionAuth did not run (a misconfigured route chain).
func authFromContext(c *gin.Context) (value.AuthContext, bool) {
	val, exists := c.Get(authContextKey)
	if !exists {
		return value.AuthContext{}, false
	}
	authCtx, ok := val.(value.AuthContext)
	return authCtx, ok
}

// SessionAuth builds middleware that authenticates the request via the session
// cookie. The session id is read from the configured cookie (never the URL),
// parsed as a UUID, and resolved through the authenticator. On success it stores
// a session-scoped value.AuthContext (UserID + IsPlatformAdmin; workspace fields
// left zero) on the gin context.
//
// Missing cookie, non-UUID cookie, and ErrUnauthorized from the authenticator
// all yield 401. Any other (internal) error yields 500.
func SessionAuth(authenticator SessionAuthenticator, cookieName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookieValue, err := c.Cookie(cookieName)
		if err != nil || cookieValue == "" {
			writeError(c, stdhttp.StatusUnauthorized, "unauthorized", domainerrors.ErrUnauthorized.Error())
			c.Abort()
			return
		}

		sessionID, err := uuid.Parse(cookieValue)
		if err != nil {
			writeError(c, stdhttp.StatusUnauthorized, "unauthorized", domainerrors.ErrUnauthorized.Error())
			c.Abort()
			return
		}

		user, err := authenticator.Authenticate(c.Request.Context(), sessionID)
		if err != nil {
			if errors.Is(err, domainerrors.ErrUnauthorized) {
				writeError(c, stdhttp.StatusUnauthorized, "unauthorized", domainerrors.ErrUnauthorized.Error())
				c.Abort()
				return
			}
			writeInternalError(c, err)
			c.Abort()
			return
		}

		c.Set(authContextKey, value.AuthContext{
			PrincipalKind:   value.PrincipalUser,
			PrincipalID:     user.ID,
			UserID:          user.ID,
			IsPlatformAdmin: user.IsPlatformAdmin,
		})
		c.Next()
	}
}

// RequirePlatformAdmin builds middleware that allows only platform admins
// through. SessionAuth must run before it.
func RequirePlatformAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists {
			// Route misconfiguration: SessionAuth did not run in the chain.
			writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
			c.Abort()
			return
		}
		if !authCtx.IsPlatformAdmin {
			writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireWorkspace builds middleware that resolves the :workspace_slug path
// parameter to a workspace the caller belongs to.
//
// Cross-workspace access is uniformly mapped to 404 (NOT 403): a missing
// workspace and a missing membership produce the SAME not_found body so the
// response cannot leak whether a given workspace exists. The workspace name/id
// is never included in the 404 body. On success the AuthContext is augmented
// with WorkspaceID and Role.
//
// 对 API Key 主体：不查 membership，只把 slug 解析出的 WorkspaceID 与 key 自身
// WorkspaceID 比较；不匹配统一 404。SessionAuth 或 SessionOrAPIKeyAuth 须先执行。
func RequireWorkspace(workspaceResolver WorkspaceResolver, membershipResolver MembershipResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists {
			writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
			c.Abort()
			return
		}

		slug := c.Param(WorkspaceSlugParam)

		ws, err := workspaceResolver.GetBySlug(c.Request.Context(), slug)
		if err != nil {
			if errors.Is(err, domainerrors.ErrNotFound) {
				// Missing workspace: uniform not_found, no existence leak.
				writeError(c, stdhttp.StatusNotFound, "not_found", domainerrors.ErrNotFound.Error())
				c.Abort()
				return
			}
			writeServiceError(c, err)
			c.Abort()
			return
		}

		if authCtx.IsAPIKey() {
			// API Key 主体：WorkspaceID 在 Authenticate 时已写入，只比较不查 membership。
			if authCtx.WorkspaceID != ws.ID {
				writeError(c, stdhttp.StatusNotFound, "not_found", domainerrors.ErrNotFound.Error())
				c.Abort()
				return
			}
			c.Set(authContextKey, authCtx)
			c.Next()
			return
		}

		membership, err := membershipResolver.Get(c.Request.Context(), ws.ID, authCtx.UserID)
		if err != nil {
			if errors.Is(err, domainerrors.ErrNotFound) {
				// No membership: SAME not_found body as missing workspace, no leak.
				writeError(c, stdhttp.StatusNotFound, "not_found", domainerrors.ErrNotFound.Error())
				c.Abort()
				return
			}
			writeServiceError(c, err)
			c.Abort()
			return
		}

		authCtx.WorkspaceID = ws.ID
		authCtx.Role = membership.Role
		c.Set(authContextKey, authCtx)
		c.Next()
	}
}

// RequireWorkspaceRole builds middleware that enforces a minimum workspace role
// for Session 主体。API Key 主体不受 role 限制（由 scope middleware 强制约束），
// 直接通过。RequireWorkspace (and therefore an auth middleware) must run first.
func RequireWorkspaceRole(minRole value.WorkspaceRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists || authCtx.WorkspaceID == uuid.Nil {
			// Either the chain is misconfigured or the route is not workspace-scoped.
			writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
			c.Abort()
			return
		}
		if authCtx.IsAPIKey() {
			// API Key 不继承创建者角色；最小角色由 scope/KB middleware 强制。
			c.Next()
			return
		}
		if !authCtx.Role.AtLeast(minRole) {
			writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}
