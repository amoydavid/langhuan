package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestIndexGenerationCreateRequiresAdmin(t *testing.T) {
	tests := []struct {
		role       value.WorkspaceRole
		wantStatus int
	}{
		{role: value.RoleMember, wantStatus: http.StatusForbidden},
		{role: value.RoleAdmin, wantStatus: http.StatusAccepted},
		{role: value.RoleOwner, wantStatus: http.StatusAccepted},
	}
	for _, test := range tests {
		gin.SetMode(gin.TestMode)
		workspaceID, userID, kbID := uuid.New(), uuid.New(), uuid.New()
		fake := &generationHTTPServiceFake{created: &dto.IndexGeneration{ID: uuid.New()}}
		handler := indexGenerationHandler{service: fake}
		router := gin.New()
		router.POST("/knowledge-bases/:id/index-generations", func(c *gin.Context) {
			c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: test.role})
			handler.create(c)
		})
		req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+kbID.String()+"/index-generations", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != test.wantStatus {
			t.Fatalf("role=%s status/body=%d %s", test.role, recorder.Code, recorder.Body.String())
		}
	}
}

func TestIndexGenerationCreateTranslatesChunkingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, userID, kbID := uuid.New(), uuid.New(), uuid.New()
	fake := &generationHTTPServiceFake{created: &dto.IndexGeneration{ID: uuid.New()}}
	handler := indexGenerationHandler{service: fake}
	router := gin.New()
	router.POST("/knowledge-bases/:id/index-generations", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: value.RoleAdmin})
		handler.create(c)
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/knowledge-bases/"+kbID.String()+"/index-generations",
		bytes.NewBufferString(`{"chunking_config":{"chunk_size":256,"chunk_overlap":32}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status/body=%d %s", recorder.Code, recorder.Body.String())
	}
	if fake.createInput.ChunkingConfig == nil || fake.createInput.ChunkingConfig.ChunkSize != 256 || fake.createInput.ChunkingConfig.ChunkOverlap != 32 {
		t.Fatalf("chunking config = %#v", fake.createInput.ChunkingConfig)
	}
}

func TestIndexGenerationRoutesExposeMemberListAndAdminMutations(t *testing.T) {
	deps := Dependencies{
		Workspaces: newFakeWorkspaceService(), Memberships: &fakeMembershipService{},
		IndexGenerations: &generationHTTPServiceFake{},
	}
	want := map[string]bool{
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/index-generations":                          false,
		http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/index-generations":                         false,
		http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/index-generations/:generation_id/activate": false,
	}
	for _, route := range NewRouter(deps).Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("missing route %s", route)
		}
	}
}

type generationHTTPServiceFake struct {
	created     *dto.IndexGeneration
	createInput service.CreateIndexGenerationInput
}

func (*generationHTTPServiceFake) List(context.Context, uuid.UUID, uuid.UUID) ([]*dto.IndexGeneration, error) {
	return nil, nil
}

func (s *generationHTTPServiceFake) Create(_ context.Context, input service.CreateIndexGenerationInput) (*dto.IndexGeneration, error) {
	s.createInput = input
	return s.created, nil
}

func (s *generationHTTPServiceFake) Activate(context.Context, service.ActivateIndexGenerationInput) (*dto.IndexGeneration, error) {
	return s.created, nil
}
