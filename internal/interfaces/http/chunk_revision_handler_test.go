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
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestChunkRevisionCreatePermissionAndFAQMapping(t *testing.T) {
	tests := []struct {
		name       string
		role       value.WorkspaceRole
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{name: "member forbidden", role: value.RoleMember, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "admin accepted", role: value.RoleAdmin, wantStatus: http.StatusAccepted},
		{name: "owner accepted", role: value.RoleOwner, wantStatus: http.StatusAccepted},
		{name: "faq immutable", role: value.RoleAdmin, serviceErr: domainerrors.ErrFAQChunkImmutable, wantStatus: http.StatusConflict, wantCode: "faq_chunk_immutable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			workspaceID, userID, knowledgeBaseID, chunkID, baseID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			fake := &fakeChunkRevisionHTTPService{createResult: &dto.ChunkRevision{ID: uuid.New()}, createErr: test.serviceErr}
			handler := chunkRevisionHandler{service: fake}
			router := gin.New()
			router.POST("/knowledge-bases/:id/chunks/:chunk_id/revisions", func(c *gin.Context) {
				c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: test.role})
				handler.create(c)
			})
			body := `{"base_revision_id":"` + baseID.String() + `","content":"new","context_header":"header","enabled":true}`
			req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+knowledgeBaseID.String()+"/chunks/"+chunkID.String()+"/revisions", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != test.wantStatus || (test.wantCode != "" && !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"`+test.wantCode+`"`))) {
				t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestChunkRevisionRoutesExposeMemberReadsAndAdminWrite(t *testing.T) {
	deps := Dependencies{
		Workspaces: newFakeWorkspaceService(), Memberships: &fakeMembershipService{},
		ChunkRevisions: &fakeChunkRevisionHTTPService{},
	}
	want := map[string]bool{
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/chunks/:chunk_id":            false,
		http.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/chunks/:chunk_id/revisions":  false,
		http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/chunks/:chunk_id/revisions": false,
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

func TestChunkRevisionCreateRequiresEnabledField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, userID, knowledgeBaseID, chunkID, baseID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	fake := &fakeChunkRevisionHTTPService{createResult: &dto.ChunkRevision{ID: uuid.New()}}
	handler := chunkRevisionHandler{service: fake}
	router := gin.New()
	router.POST("/knowledge-bases/:id/chunks/:chunk_id/revisions", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: value.RoleAdmin})
		handler.create(c)
	})
	body := `{"base_revision_id":"` + baseID.String() + `","content":"new","context_header":"header"}`
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+knowledgeBaseID.String()+"/chunks/"+chunkID.String()+"/revisions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || fake.createCalls != 0 {
		t.Fatalf("status/body/calls = %d %s %d", recorder.Code, recorder.Body.String(), fake.createCalls)
	}
}

type fakeChunkRevisionHTTPService struct {
	createResult *dto.ChunkRevision
	createErr    error
	createCalls  int
}

func (*fakeChunkRevisionHTTPService) Get(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*dto.Chunk, error) {
	return nil, nil
}

func (*fakeChunkRevisionHTTPService) List(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]*dto.ChunkRevision, error) {
	return nil, nil
}

func (s *fakeChunkRevisionHTTPService) Create(context.Context, service.CreateChunkRevisionInput) (*dto.ChunkRevision, error) {
	s.createCalls++
	return s.createResult, s.createErr
}
