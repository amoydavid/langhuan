package http

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// OIDCLoginServiceHTTP 是 handler 层依赖的 OIDC 登录服务接口。
// 由 application/service.OIDCLoginService 满足。
type OIDCLoginServiceHTTP interface {
	BeginLogin(ctx context.Context, next string, invitationToken string, actorUserID, sessionID uuid.UUID) (authURL, browserNonce, state string, err error)
	ConsumeAndExchange(ctx context.Context, code, state, browserNonce string) (*authport.OIDCStatePayload, *authport.OIDCProfile, error)
	LoginOrProvision(ctx context.Context, profile *authport.OIDCProfile, userAgent, ipAddr string) (*model.Session, error)
	BindIdentity(ctx context.Context, actorUserID uuid.UUID, profile *authport.OIDCProfile) error
	ListIdentities(ctx context.Context, userID uuid.UUID) ([]*model.ExternalIdentity, error)
}

// OIDCAcceptor 是 AcceptOIDC 接口（InvitationService 满足）。
type OIDCAcceptor interface {
	AcceptOIDC(ctx context.Context, invitationTokenHash string, profile *authport.OIDCProfile, userAgent, ipAddr string) (*model.Session, error)
}

// oidcNonceCookiePrefix 是浏览器 nonce cookie 的前缀（动态后缀 = state）。
const oidcNonceCookiePrefix = "oidc_nonce_"

// 前端路由常量：登录失败/成功后重定向的目标路径，避免硬编码漂移。
const (
	loginRoute       = "/sign-in"
	settingsRoute    = "/settings"
	defaultNextRoute = "/"
)

// externalIdentityDTO 是 /auth/external-identities 返回的非敏感摘要。
type externalIdentityDTO struct {
	Issuer     string `json:"issuer"`
	Email      string `json:"email"`
	LastAuthAt string `json:"last_auth_at"`
}

// oidcHandler 暴露 OIDC 登录/回调/绑定/外部身份查询端点。
type oidcHandler struct {
	svc        OIDCLoginServiceHTTP
	acceptor   OIDCAcceptor    // AcceptOIDC 路径；nil 时该路径不可用
	auth       OIDCSessionAuth // bind 回调重新认证 session
	sessionCfg config.SessionConfig
}

// OIDCSessionAuth 提供 bind 回调时重新认证当前 session 的能力。
type OIDCSessionAuth interface {
	Authenticate(ctx context.Context, sessionID uuid.UUID) (*model.User, error)
}

// newOIDCHandler 构造 oidcHandler。
func newOIDCHandler(svc OIDCLoginServiceHTTP, acceptor OIDCAcceptor, auth OIDCSessionAuth, sessionCfg config.SessionConfig) oidcHandler {
	return oidcHandler{svc: svc, acceptor: acceptor, auth: auth, sessionCfg: sessionCfg}
}

// begin 处理 GET /auth/oidc/login?next=&invitation_token=。
// 普通登录与邀请接受发起；生成 state、设动态 nonce cookie、302 到 IdP。
func (h oidcHandler) begin(c *gin.Context) {
	next := strings.TrimSpace(c.Query("next"))
	invitationToken := strings.TrimSpace(c.Query("invitation_token"))

	// bind 路径用 POST /auth/oidc/bind/start（见 beginBind），这里不处理 bind。
	authURL, browserNonce, state, err := h.svc.BeginLogin(c.Request.Context(), next, invitationToken, uuid.Nil, uuid.Nil)
	if err != nil {
		c.Redirect(http.StatusFound, loginRoute+"?oidc_error="+errorCode(err))
		return
	}
	// 动态 cookie 名（与 state 绑定），允许并发标签页。
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcNonceCookiePrefix+state, browserNonce, h.sessionCfg.LifetimeSeconds, "/", h.sessionCfg.Domain, h.sessionCfg.SecureCookie, true)
	c.Redirect(http.StatusFound, authURL)
}

// beginBind 处理 POST /auth/oidc/bind/start（SessionAuth）。
// 已登录用户发起 OIDC 绑定；从 AuthContext 取 actor/session 写入 state。
func (h oidcHandler) beginBind(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	sessionID, err := sessionIDFromCookie(c, h.sessionCfg.CookieName)
	if err != nil {
		writeError(c, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	next := strings.TrimSpace(c.Query("next"))
	if next == "" {
		next = settingsRoute
	}
	authURL, browserNonce, state, err := h.svc.BeginLogin(c.Request.Context(), next, "", authCtx.UserID, sessionID)
	if err != nil {
		c.Redirect(http.StatusFound, settingsRoute+"?oidc_error="+errorCode(err))
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcNonceCookiePrefix+state, browserNonce, h.sessionCfg.LifetimeSeconds, "/", h.sessionCfg.Domain, h.sessionCfg.SecureCookie, true)
	c.Redirect(http.StatusFound, authURL)
}

// callback 处理 GET /auth/oidc/callback?code=&state= 或 ?error=&state=。
// 按 state payload 分派到登录/邀请接受/绑定。
func (h oidcHandler) callback(c *gin.Context) {
	// IdP 错误（用户拒绝/IdP 拒绝）。
	if errMsg := strings.TrimSpace(c.Query("error")); errMsg != "" {
		state := c.Query("state")
		if cookieName := oidcNonceCookiePrefix + state; cookieName != oidcNonceCookiePrefix {
			h.clearNonceCookie(c, state)
		}
		code := "oidc_provider_error"
		if errMsg == "access_denied" {
			code = "oidc_access_denied"
		}
		c.Redirect(http.StatusFound, loginRoute+"?oidc_error="+code)
		return
	}

	code := c.Query("code")
	state := c.Query("state")
	nonceCookie := nonceCookieForState(c, state)

	payload, profile, err := h.svc.ConsumeAndExchange(c.Request.Context(), code, state, nonceCookie)
	h.clearNonceCookie(c, state)
	if err != nil {
		c.Redirect(http.StatusFound, loginRoute+"?oidc_error="+errorCode(err))
		return
	}

	next := payload.Next
	if next == "" {
		next = defaultNextRoute
	}
	userAgent := c.Request.UserAgent()
	ipAddr := clientIP(c)

	// 分派。
	if payload.InvitationTokenHash != "" {
		if h.acceptor == nil {
			c.Redirect(http.StatusFound, loginRoute+"?oidc_error=oidc_unavailable")
			return
		}
		session, err := h.acceptor.AcceptOIDC(c.Request.Context(), payload.InvitationTokenHash, profile, userAgent, ipAddr)
		if err != nil {
			c.Redirect(http.StatusFound, loginRoute+"?oidc_error="+errorCode(err))
			return
		}
		h.setSessionCookie(c, session.ID.String())
		c.Redirect(http.StatusFound, next)
		return
	}

	if payload.BindActorID != uuid.Nil {
		// 绑定：重新认证当前 session，确认 actor/session 一致。
		if payload.BindSessionID == uuid.Nil {
			c.Redirect(http.StatusFound, settingsRoute+"?oidc_error=unauthorized")
			return
		}
		currentSessionID, serr := sessionIDFromCookie(c, h.sessionCfg.CookieName)
		if serr != nil || currentSessionID != payload.BindSessionID {
			c.Redirect(http.StatusFound, settingsRoute+"?oidc_error=unauthorized")
			return
		}
		user, aerr := h.auth.Authenticate(c.Request.Context(), currentSessionID)
		if aerr != nil || user == nil || user.ID != payload.BindActorID {
			c.Redirect(http.StatusFound, settingsRoute+"?oidc_error=unauthorized")
			return
		}
		if err := h.svc.BindIdentity(c.Request.Context(), payload.BindActorID, profile); err != nil {
			c.Redirect(http.StatusFound, settingsRoute+"?oidc_error="+errorCode(err))
			return
		}
		c.Redirect(http.StatusFound, next)
		return
	}

	// 常规登录/JIT/合并。
	session, err := h.svc.LoginOrProvision(c.Request.Context(), profile, userAgent, ipAddr)
	if err != nil {
		c.Redirect(http.StatusFound, loginRoute+"?oidc_error="+errorCode(err))
		return
	}
	h.setSessionCookie(c, session.ID.String())
	c.Redirect(http.StatusFound, next)
}

// listIdentities 处理 GET /auth/external-identities（SessionAuth）。
// 返回当前 user 的外部身份非敏感摘要（不含 subject/raw_profile）。
func (h oidcHandler) listIdentities(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	identities, err := h.svc.ListIdentities(c.Request.Context(), authCtx.UserID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	dto := make([]externalIdentityDTO, 0, len(identities))
	for _, id := range identities {
		dto = append(dto, externalIdentityDTO{
			Issuer:     id.Issuer,
			Email:      id.Email,
			LastAuthAt: id.LastAuthAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"identities": dto})
}

// setSessionCookie 写入 session cookie（复用 auth_handler 的逻辑风格）。
func (h oidcHandler) setSessionCookie(c *gin.Context, sessionID string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.sessionCfg.CookieName, sessionID, h.sessionCfg.LifetimeSeconds, "/", h.sessionCfg.Domain, h.sessionCfg.SecureCookie, true)
}

// clearNonceCookie 清除指定 state 的 nonce cookie。
func (h oidcHandler) clearNonceCookie(c *gin.Context, state string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcNonceCookiePrefix+state, "", -1, "/", h.sessionCfg.Domain, h.sessionCfg.SecureCookie, true)
}

// nonceCookieForState 读取指定 state 的 nonce cookie。
func nonceCookieForState(c *gin.Context, state string) string {
	cookieValue, _ := c.Cookie(oidcNonceCookiePrefix + state)
	return cookieValue
}

// sessionIDFromCookie 从 session cookie 解析 sessionID。
func sessionIDFromCookie(c *gin.Context, cookieName string) (uuid.UUID, error) {
	cookieValue, _ := c.Cookie(cookieName)
	if cookieValue == "" {
		return uuid.Nil, domainerrors.ErrUnauthorized
	}
	return uuid.Parse(cookieValue)
}

// errorCode 把领域错误映射为稳定的 oidc_error code。
func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domainerrors.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, domainerrors.ErrForbidden):
		return "forbidden"
	case errors.Is(err, domainerrors.ErrConflict):
		return "conflict"
	case errors.Is(err, domainerrors.ErrPasswordLoginDisabled):
		return "password_login_disabled"
	case errors.Is(err, domainerrors.ErrPasswordRegistrationDisabled):
		return "password_registration_disabled"
	default:
		return "oidc_error"
	}
}
