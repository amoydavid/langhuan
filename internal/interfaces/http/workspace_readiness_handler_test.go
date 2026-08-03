package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceReadinessHandlerReturnsTypedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	service := &fakeWorkspaceReadinessHTTPService{result: &dto.WorkspaceReadiness{
		HasActiveProvider:           true,
		HasSelectableEmbeddingModel: true,
		KnowledgeBaseCount:          2,
		DocumentCounts: dto.WorkspaceReadinessDocumentCounts{
			Total: 20, Ready: 18, Processing: 1, Failed: 1,
		},
		SearchableKnowledgeBaseCount: 2,
		RecommendedAction:            dto.ReadinessResolveFailedDocument,
		RecommendedKnowledgeBaseID:   &knowledgeBaseID,
		RecommendedKnowledgeBaseName: "产品文档",
		RecommendedDocumentID:        &documentID,
		RecommendedDocumentName:      "faq-import.csv",
	}}
	handler := workspaceReadinessHandler{service: service}
	router := gin.New()
	router.GET("/readiness", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, Role: value.RoleMember})
		handler.get(c)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, "/readiness", nil))

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["recommended_action"] != string(dto.ReadinessResolveFailedDocument) ||
		response["recommended_knowledge_base_name"] != "产品文档" ||
		response["recommended_document_name"] != "faq-import.csv" {
		t.Fatalf("response = %#v", response)
	}
	if service.workspaceID != workspaceID {
		t.Fatalf("workspaceID = %s, want %s", service.workspaceID, workspaceID)
	}
}

type fakeWorkspaceReadinessHTTPService struct {
	result      *dto.WorkspaceReadiness
	err         error
	workspaceID uuid.UUID
}

func (s *fakeWorkspaceReadinessHTTPService) Get(_ context.Context, workspaceID uuid.UUID) (*dto.WorkspaceReadiness, error) {
	s.workspaceID = workspaceID
	return s.result, s.err
}
