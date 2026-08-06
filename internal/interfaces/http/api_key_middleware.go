package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// APIKeyAuthenticator 把 Bearer 明文解析为 API Key 主体上下文。由
// service.APIKeyService.Authenticate 实现。格式错误、查无记录、已吊销、已
// 到期统一返回 ErrUnauthorized，不泄漏原因。
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, plaintext string) (value.AuthContext, error)
}

// parseSingleBearer 解析单一严格 Bearer 凭证。
//
// 规则：恰好一个 Authorization header，值必须是 "Bearer <credential>"，
// credential 非空。拒绝多 header、逗号拼接、Basic、空 credential。
func parseSingleBearer(headers []string) (string, error) {
	if len(headers) != 1 {
		return "", errors.New("Authorization 必须恰好出现一次")
	}
	parts := strings.SplitN(headers[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", errors.New("Authorization 必须是 Bearer 凭证")
	}
	credential := strings.TrimSpace(parts[1])
	// 拒绝逗号拼接的多凭证。
	if strings.Contains(credential, ",") || strings.ContainsAny(credential, " \t") {
		return "", errors.New("Authorization 凭证不能包含空白或逗号")
	}
	if credential == "" {
		return "", errors.New("Authorization 凭证不能为空")
	}
	return credential, nil
}

// writeAPIKeyUnauthorized 写 401 并设置 WWW-Authenticate: Bearer，不进入业务 handler。
func writeAPIKeyUnauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", "Bearer")
	writeError(c, stdhttp.StatusUnauthorized, "unauthorized", domainerrors.ErrUnauthorized.Error())
	c.Abort()
}

// SessionOrAPIKeyAuth 在允许两类凭证的路由上提供统一认证。
//
// 凭证优先级：存在任意 Authorization header 时，Bearer 是权威凭证；无效
// Bearer 直接 401，不回退有效 Cookie。仅在没有 Authorization 时才尝试 Session。
// apiKeyAuth 为 nil（未配置程序化访问）时退化为纯 Session 认证。
func SessionOrAPIKeyAuth(sessionAuth SessionAuthenticator, apiKeyAuth APIKeyAuthenticator, cookieName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeaders := c.Request.Header.Values("Authorization")
		if len(authHeaders) > 0 {
			if apiKeyAuth == nil {
				// 未配置程序化访问却带了 Authorization：拒绝。
				writeAPIKeyUnauthorized(c)
				return
			}
			credential, err := parseSingleBearer(authHeaders)
			if err != nil {
				writeAPIKeyUnauthorized(c)
				return
			}
			principal, err := apiKeyAuth.Authenticate(c.Request.Context(), credential)
			if err != nil {
				writeAPIKeyUnauthorized(c)
				return
			}
			c.Set(authContextKey, principal)
			c.Next()
			return
		}
		// 无 Authorization：回退到 Session 认证。
		authenticateBySession(c, sessionAuth, cookieName)
	}
}

// APIKeyOnlyAuth 仅接受 Bearer API Key，不接受 Cookie。用于 /mcp。
func APIKeyOnlyAuth(apiKeyAuth APIKeyAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeaders := c.Request.Header.Values("Authorization")
		if len(authHeaders) == 0 {
			writeAPIKeyUnauthorized(c)
			return
		}
		credential, err := parseSingleBearer(authHeaders)
		if err != nil {
			writeAPIKeyUnauthorized(c)
			return
		}
		principal, err := apiKeyAuth.Authenticate(c.Request.Context(), credential)
		if err != nil {
			writeAPIKeyUnauthorized(c)
			return
		}
		c.Set(authContextKey, principal)
		// 把 AuthContext 注入 request.Context()，使被 gin.WrapH 包装的下游
		// handler（如 MCP Streamable HTTP）能通过 request.Context() 读取主体。
		c.Request = c.Request.WithContext(value.ContextWithAuthContext(c.Request.Context(), principal))
		c.Next()
	}
}

// authenticateBySession 复用 SessionAuth 的 cookie -> session 认证逻辑。
func authenticateBySession(c *gin.Context, authenticator SessionAuthenticator, cookieName string) {
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

// RequireScopeForAPIKey 要求 API Key 主体拥有指定 scope；Session 主体直接通过。
func RequireScopeForAPIKey(required value.APIScope) gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists {
			writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
			c.Abort()
			return
		}
		if authCtx.IsAPIKey() && !authCtx.HasScope(required) {
			writeError(c, stdhttp.StatusForbidden, "insufficient_scope", domainerrors.ErrInsufficientScope.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAdminForSession permits API keys (which are governed by scopes) while
// retaining the existing admin requirement for browser Session principals.
func RequireAdminForSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists {
			writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
			c.Abort()
			return
		}
		if !authCtx.IsAPIKey() && !authCtx.Role.AtLeast(value.RoleAdmin) {
			writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireSessionOnly keeps legacy document-only endpoints out of the
// programmatic contract when they cannot carry KnowledgeBase lineage.
func RequireSessionOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists {
			writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
			c.Abort()
			return
		}
		if authCtx.IsAPIKey() {
			writeError(c, stdhttp.StatusForbidden, "forbidden", domainerrors.ErrForbidden.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireKnowledgeBaseForAPIKey 要求 API Key 主体只能访问绑定的知识库。
// paramKey 是 URL 中知识库 ID 的参数名（如 "id" 或 "knowledge_base_id"）；
// Session 主体直接通过。
func RequireKnowledgeBaseForAPIKey(paramKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authCtx, exists := authFromContext(c)
		if !exists {
			writeError(c, stdhttp.StatusInternalServerError, "internal_error", internalErrorMessage)
			c.Abort()
			return
		}
		if !authCtx.IsAPIKey() {
			c.Next()
			return
		}
		raw := c.Param(paramKey)
		kbID, err := uuid.Parse(raw)
		if err != nil {
			// 路径参数不是有效知识库 ID：统一 404，不泄漏存在性。
			writeError(c, stdhttp.StatusNotFound, "not_found", domainerrors.ErrNotFound.Error())
			c.Abort()
			return
		}
		allowed := false
		for _, id := range authCtx.KnowledgeBaseIDs {
			if id == kbID {
				allowed = true
				break
			}
		}
		if !allowed {
			writeError(c, stdhttp.StatusNotFound, "not_found", domainerrors.ErrNotFound.Error())
			c.Abort()
			return
		}
		c.Next()
	}
}
