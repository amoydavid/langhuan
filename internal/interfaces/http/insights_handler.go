package http

import (
	"context"
	stdhttp "net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
)

// InsightsHTTPService 是健康面板聚合契约（Session owner/admin；API Key 不可达）。
type InsightsHTTPService interface {
	Overview(ctx context.Context, workspaceID, kbID uuid.UUID, windowDays int) (*service.InsightsOverview, error)
	ZeroRecall(ctx context.Context, workspaceID, kbID uuid.UUID, windowDays, limit int) ([]service.InsightsZeroRecallQuery, bool, error)
	Sleeping(ctx context.Context, workspaceID, kbID uuid.UUID, limit int) ([]dto.InsightsSleepingDocument, bool, error)
	Activity(ctx context.Context, workspaceID, kbID uuid.UUID, limit int) ([]dto.InsightsActivityEvent, error)
}

// QueryLogSettingsHTTPService 是查询原文开关契约（spec §7 PUT query-log-settings）。
type QueryLogSettingsHTTPService interface {
	UpdateQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID, enabled bool) error
	GetQueryLogEnabled(ctx context.Context, workspaceID, kbID uuid.UUID) (bool, error)
}

// CitationSummaryHTTPService 是文档引用摘要契约（prog 组，删除预警用）。
type CitationSummaryHTTPService interface {
	DocumentCitationSummary(ctx context.Context, workspaceID, kbID, documentID uuid.UUID, windowDays int) (count int64, lastCitedAt *time.Time, err error)
}

type insightsHandler struct {
	insights InsightsHTTPService
	settings QueryLogSettingsHTTPService
	// queryLogRetentionDays 来自 config insights.query_log_retention_days，
	// 用于 settings 响应与确认文案——不得硬编码（与实际清理保留期脱钩会误导用户）。
	queryLogRetentionDays int
}

func newInsightsHandler(insights InsightsHTTPService, settings QueryLogSettingsHTTPService, queryLogRetentionDays int) *insightsHandler {
	if queryLogRetentionDays <= 0 {
		queryLogRetentionDays = 30
	}
	return &insightsHandler{insights: insights, settings: settings, queryLogRetentionDays: queryLogRetentionDays}
}

type queryLogSettingsRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

type queryLogSettingsResponse struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
}

// getOverview 处理 GET /knowledge-bases/:id/insights/overview?window_days=14。
func (h insightsHandler) getOverview(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	windowDays, ok := parseInsightsWindowParam(c, 14)
	if !ok {
		return
	}
	overview, err := h.insights.Overview(c.Request.Context(), authCtx.WorkspaceID, kbID, windowDays)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, overview)
}

// getQueries 处理 GET /knowledge-bases/:id/insights/queries?window_days=14&limit=10。
func (h insightsHandler) getQueries(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	windowDays, ok := parseInsightsWindowParam(c, 14)
	if !ok {
		return
	}
	limit, ok := parseInsightsLimitParam(c, 10)
	if !ok {
		return
	}
	queries, enabled, err := h.insights.ZeroRecall(c.Request.Context(), authCtx.WorkspaceID, kbID, windowDays, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"query_log_enabled": enabled,
		"window_days":       windowDays,
		"queries":           queries,
	})
}

// getDocuments 处理 GET /knowledge-bases/:id/insights/documents?limit=10。
func (h insightsHandler) getDocuments(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	limit, ok := parseInsightsLimitParam(c, 10)
	if !ok {
		return
	}
	documents, insufficient, err := h.insights.Sleeping(c.Request.Context(), authCtx.WorkspaceID, kbID, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"insufficient_sample": insufficient,
		"documents":           documents,
	})
}

// getActivity 处理 GET /knowledge-bases/:id/insights/activity?limit=20。
func (h insightsHandler) getActivity(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	limit, ok := parseInsightsLimitParam(c, 20)
	if !ok {
		return
	}
	events, err := h.insights.Activity(c.Request.Context(), authCtx.WorkspaceID, kbID, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{"events": events})
}

// putQueryLogSettings 处理 PUT /knowledge-bases/:id/insights/query-log-settings。
func (h insightsHandler) putQueryLogSettings(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	var request queryLogSettingsRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "enabled 必须为布尔值")
		return
	}
	if err := h.settings.UpdateQueryLogEnabled(c.Request.Context(), authCtx.WorkspaceID, kbID, *request.Enabled); err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, queryLogSettingsResponse{Enabled: *request.Enabled, RetentionDays: h.queryLogRetentionDays})
}

// citationSummaryGet 处理 GET /knowledge-bases/:id/documents/:document_id/citation-summary?window_days=30。
type citationSummaryHandler struct {
	service CitationSummaryHTTPService
}

func newCitationSummaryHandler(svc CitationSummaryHTTPService) *citationSummaryHandler {
	return &citationSummaryHandler{service: svc}
}

func (h citationSummaryHandler) get(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	kbID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	documentID, ok := parseUUIDParam(c, "document_id")
	if !ok {
		return
	}
	windowDays, ok := parseInsightsWindowParam(c, 30)
	if !ok {
		return
	}
	count, lastCitedAt, err := h.service.DocumentCitationSummary(c.Request.Context(), authCtx.WorkspaceID, kbID, documentID, windowDays)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response := gin.H{"window_days": windowDays, "citation_count": count}
	if lastCitedAt != nil {
		response["last_cited_at"] = lastCitedAt.UTC().Format(time.RFC3339)
	}
	c.JSON(stdhttp.StatusOK, response)
}

// parseInsightsWindowParam 解析 window_days 查询参数；缺省用 fallback，非法返回 false。
func parseInsightsWindowParam(c *gin.Context, fallback int) (int, bool) {
	raw := c.Query("window_days")
	if raw == "" {
		return fallback, true
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "window_days 必须为整数")
		return 0, false
	}
	return days, true
}

// parseInsightsLimitParam 解析 limit 查询参数；缺省用 fallback，非法返回 false。
func parseInsightsLimitParam(c *gin.Context, fallback int) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return fallback, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "limit 必须为整数")
		return 0, false
	}
	return limit, true
}
