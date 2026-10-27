package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// insightsServiceStub 覆盖四个聚合端点与开关的编排语义。
type insightsServiceStub struct {
	overview  *service.InsightsOverview
	overviewE error

	queries        []service.InsightsZeroRecallQuery
	queriesEnabled bool
	queriesE       error

	sleeping      []dto.InsightsSleepingDocument
	sleepingInsuf bool
	sleepingE     error

	activity  []dto.InsightsActivityEvent
	activityE error

	settingsE error
}

func (s *insightsServiceStub) Overview(_ context.Context, _, _ uuid.UUID, _ int) (*service.InsightsOverview, error) {
	return s.overview, s.overviewE
}

func (s *insightsServiceStub) ZeroRecall(_ context.Context, _, _ uuid.UUID, _, _ int) ([]service.InsightsZeroRecallQuery, bool, error) {
	return s.queries, s.queriesEnabled, s.queriesE
}

func (s *insightsServiceStub) Sleeping(_ context.Context, _, _ uuid.UUID, _ int) ([]dto.InsightsSleepingDocument, bool, error) {
	return s.sleeping, s.sleepingInsuf, s.sleepingE
}

func (s *insightsServiceStub) Activity(_ context.Context, _, _ uuid.UUID, _ int) ([]dto.InsightsActivityEvent, error) {
	return s.activity, s.activityE
}

func (s *insightsServiceStub) UpdateQueryLogEnabled(_ context.Context, _, _ uuid.UUID, _ bool) error {
	return s.settingsE
}

func (s *insightsServiceStub) GetQueryLogEnabled(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *insightsServiceStub) DocumentCitationSummary(_ context.Context, _, _, _ uuid.UUID, _ int) (int64, *time.Time, error) {
	last := time.Date(2026, 10, 25, 9, 14, 0, 0, time.UTC)
	return 23, &last, nil
}

func newInsightsTestRouter(t *testing.T, stub *insightsServiceStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// gin 的 Use 必须先于路由注册：注入 Session admin 主体。
	router.Use(func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			UserID: uuid.New(), WorkspaceID: uuid.Must(uuid.NewRandom()),
			PrincipalKind: value.PrincipalUser, Role: value.RoleAdmin,
		})
		c.Next()
	})
	insights := router.Group("/api/v1/workspaces/:workspace_slug/knowledge-bases/:id/insights")
	h := newInsightsHandler(stub, stub)
	{
		insights.GET("/overview", h.getOverview)
		insights.GET("/queries", h.getQueries)
		insights.GET("/documents", h.getDocuments)
		insights.GET("/activity", h.getActivity)
		insights.PUT("/query-log-settings", h.putQueryLogSettings)
	}
	citation := router.Group("/api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/:document_id")
	ch := newCitationSummaryHandler(stub)
	citation.GET("/citation-summary", ch.get)
	return router
}

func TestInsightsHandlerOverviewReturnsPayload(t *testing.T) {
	stub := &insightsServiceStub{overview: &service.InsightsOverview{
		WindowDays: 14, RunsTotal: 100, RunsEmpty: 5, EmptyRecallRate: 0.05,
		HasPreviousWindow: true, PreviousEmptyRecallRate: 0.07, QueryLogEnabled: false,
	}}
	router := newInsightsTestRouter(t, stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/insights/overview?window_days=14", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusOK, rec.Code)

	var payload service.InsightsOverview
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, 14, payload.WindowDays)
	require.Equal(t, float64(0.05), payload.EmptyRecallRate)
	require.False(t, payload.QueryLogEnabled)
}

func TestInsightsHandlerQueriesOmitPlainTextWhenDisabled(t *testing.T) {
	// 关键隐私断言：query_log_enabled=false 时响应不含 latest_query_text。
	stub := &insightsServiceStub{
		queries: []service.InsightsZeroRecallQuery{{
			QueryHash: "h1", QueryChars: 7, Hits: 12,
			FirstSeenAt: time.Now(), LastSeenAt: time.Now(),
		}},
		queriesEnabled: false,
	}
	router := newInsightsTestRouter(t, stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/insights/queries", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "latest_query_text", "未开启原文记录时响应不得含明文字段")
	require.Contains(t, rec.Body.String(), "\"query_log_enabled\":false")

	// 开启后明文出现（该字段只在本端点可达）。
	stub.queriesEnabled = true
	stub.queries[0].LatestQueryText = "发票抬头怎么改"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "发票抬头怎么改")
}

func TestInsightsHandlerDocumentsInsufficientSample(t *testing.T) {
	stub := &insightsServiceStub{sleeping: nil, sleepingInsuf: true}
	router := newInsightsTestRouter(t, stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/insights/documents", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "\"insufficient_sample\":true")
	require.Contains(t, rec.Body.String(), "\"documents\":null")
}

func TestInsightsHandlerQueryLogSettingsRequiresBoolean(t *testing.T) {
	stub := &insightsServiceStub{}
	router := newInsightsTestRouter(t, stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/insights/query-log-settings", strings.NewReader(`{"enabled":"yes"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusBadRequest, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(stdhttp.MethodPut, "/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/insights/query-log-settings", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"retention_days":30`)
}

func TestCitationSummaryHandlerReturnsCountAndLastCited(t *testing.T) {
	stub := &insightsServiceStub{}
	router := newInsightsTestRouter(t, stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet,
		"/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/documents/"+uuid.NewString()+"/citation-summary?window_days=30", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"citation_count":23`)
	require.Contains(t, rec.Body.String(), `"last_cited_at":"2026-10-25T09:14:00Z"`)
}

func TestInsightsHandlerRejectsInvalidWindow(t *testing.T) {
	stub := &insightsServiceStub{overview: &service.InsightsOverview{}}
	router := newInsightsTestRouter(t, stub)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workspaces/ws/knowledge-bases/"+uuid.NewString()+"/insights/overview?window_days=abc", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, stdhttp.StatusBadRequest, rec.Code)
}
