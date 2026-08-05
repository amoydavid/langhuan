package http

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/requestmeta"
)

// requestIDHeader 是请求/响应中使用的 request ID 头。
const requestIDHeader = "X-Request-ID"

// requestIDPattern 限制合法 request ID：长度 1..64，字符集 [A-Za-z0-9._:-]。
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// RequestID 是覆盖 REST 与 /mcp 的 request ID 中间件：
// 接受合法 X-Request-ID，缺失或非法时生成 UUID；始终回写响应头；
// 把 request ID、transport、principal kind 写入 context。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(requestIDHeader)
		requestID := raw
		if !requestIDPattern.MatchString(requestID) {
			requestID = uuid.New().String()
		}
		c.Header(requestIDHeader, requestID)
		meta := requestmeta.Meta{
			RequestID:     requestID,
			Transport:     "rest",
			PrincipalKind: principalKindFromContext(c),
		}
		c.Request = c.Request.WithContext(requestmeta.With(c.Request.Context(), meta))
		c.Next()
	}
}

// principalKindFromContext 根据已认证主体推断 user/api_key。
// 中间件执行顺序：RequestID 先于 auth，因此此处可能尚未认证；后续 handler
// 可在认证后补全 transport/principal_kind，这里仅尽力设置。
func principalKindFromContext(c *gin.Context) string {
	if authCtx, ok := authFromContext(c); ok {
		if authCtx.IsAPIKey() {
			return "api_key"
		}
		if authCtx.UserID != uuid.Nil {
			return "user"
		}
	}
	return ""
}

// MCPTransport 把 requestmeta.transport 覆盖为 mcp，在 /mcp 路由组上注册。
// 它在 RequestID 之后运行，复用同一 request ID，只修正 transport。
func MCPTransport() gin.HandlerFunc {
	return func(c *gin.Context) {
		meta := requestmeta.From(c.Request.Context())
		meta.Transport = "mcp"
		if meta.PrincipalKind == "" {
			meta.PrincipalKind = "api_key"
		}
		c.Request = c.Request.WithContext(requestmeta.With(c.Request.Context(), meta))
		c.Next()
	}
}

// requestIDTransportMarker 用于测试暴露 transport 设置时机。
