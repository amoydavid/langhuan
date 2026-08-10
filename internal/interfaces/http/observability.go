package http

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	metricspkg "github.com/dajee/langhuan/internal/infrastructure/metrics"
)

// readyz 返回依赖就绪探活 handler。
// 任一依赖（DB/Redis/队列）失败返回 503；全成功返回 200。
func readyz(checker ReadinessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		report := checker.Check(ctx)
		status := http.StatusOK
		if !report.Ready {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, report)
	}
}

// MetricsMiddleware 采集 HTTP 请求计数与耗时。
// route 用 gin 的路由模板（已注册路由时 c.FullPath() 非空），未匹配路由用 "unknown"。
func MetricsMiddleware(m *metricspkg.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		m.ObserveHTTPRequest(c.Request.Method, route, strconv.Itoa(c.Writer.Status()), time.Since(start))
	}
}
