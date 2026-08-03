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

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestKnowledgeBaseSummaryHandlerReturnsSafeReadableResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeKnowledgeBaseSummaryHTTPService{summary: &dto.KnowledgeBaseSummary{
		KnowledgeBaseID: knowledgeBaseID, KnowledgeBaseName: "产品文档",
		SyncState: dto.KnowledgeBaseSyncFailed,
		RecentJobs: []*dto.JobSummary{{
			ID: uuid.New(), DocumentID: &documentID, Status: value.JobStatusFailed,
			ActionLabel: "导入文件", TargetType: "document", TargetDisplayName: "installation.md",
			ErrorMessage: "任务执行失败，请检查相关资源后重试。", CreatedAt: now, UpdatedAt: now,
		}},
	}}
	handler := knowledgeBaseSummaryHandler{service: fake}
	router := gin.New()
	router.GET("/knowledge-bases/:id/summary", withSummaryAuth(workspaceID, handler.getSummary))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/knowledge-bases/"+knowledgeBaseID.String()+"/summary", nil))

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"target_display_name":"installation.md"`) || strings.Contains(body, "payload") || strings.Contains(body, "external_job_id") {
		t.Fatalf("unsafe or unreadable response: %s", body)
	}
	if fake.workspaceID != workspaceID || fake.knowledgeBaseID != knowledgeBaseID {
		t.Fatalf("lineage = %s/%s", fake.workspaceID, fake.knowledgeBaseID)
	}
}

func TestKnowledgeBaseJobsHandlerParsesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	fake := &fakeKnowledgeBaseSummaryHTTPService{jobs: &dto.JobSummaryPage{Items: []*dto.JobSummary{}}}
	handler := knowledgeBaseSummaryHandler{service: fake}
	router := gin.New()
	router.GET("/knowledge-bases/:id/jobs", withSummaryAuth(workspaceID, handler.listJobs))

	request := httptest.NewRequest(stdhttp.MethodGet, "/knowledge-bases/"+knowledgeBaseID.String()+"/jobs?document_id="+documentID.String()+"&status=running&cursor=opaque&limit=40", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if fake.filter.DocumentID == nil || *fake.filter.DocumentID != documentID || fake.filter.Status != value.JobStatusRunning || fake.filter.Cursor != "opaque" || fake.filter.Limit != 40 {
		t.Fatalf("filter = %#v", fake.filter)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := response["items"]; !ok {
		t.Fatalf("response = %#v", response)
	}
}

func TestKnowledgeBaseJobsHandlerRejectsInvalidQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	handler := knowledgeBaseSummaryHandler{service: &fakeKnowledgeBaseSummaryHTTPService{}}
	tests := []string{
		"document_id=bad",
		"status=unknown",
		"limit=0",
		"limit=101",
		"limit=not-a-number",
	}
	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			router := gin.New()
			router.GET("/knowledge-bases/:id/jobs", withSummaryAuth(workspaceID, handler.listJobs))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/knowledge-bases/"+knowledgeBaseID.String()+"/jobs?"+query, nil))
			if recorder.Code != stdhttp.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func withSummaryAuth(workspaceID uuid.UUID, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, Role: value.RoleMember})
		handler(c)
	}
}

type fakeKnowledgeBaseSummaryHTTPService struct {
	summary                      *dto.KnowledgeBaseSummary
	jobs                         *dto.JobSummaryPage
	err                          error
	workspaceID, knowledgeBaseID uuid.UUID
	filter                       service.JobListFilter
}

func (s *fakeKnowledgeBaseSummaryHTTPService) GetSummary(_ context.Context, workspaceID, knowledgeBaseID uuid.UUID) (*dto.KnowledgeBaseSummary, error) {
	s.workspaceID, s.knowledgeBaseID = workspaceID, knowledgeBaseID
	return s.summary, s.err
}

func (s *fakeKnowledgeBaseSummaryHTTPService) ListJobs(_ context.Context, workspaceID, knowledgeBaseID uuid.UUID, filter service.JobListFilter) (*dto.JobSummaryPage, error) {
	s.workspaceID, s.knowledgeBaseID, s.filter = workspaceID, knowledgeBaseID, filter
	return s.jobs, s.err
}
