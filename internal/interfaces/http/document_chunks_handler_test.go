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
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentChunksHandlerParsesLineageFilterAndPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID, documentID := uuid.New(), uuid.New(), uuid.New()
	enabled := false
	fake := &fakeDocumentChunksHTTPService{page: &dto.DocumentChunkPage{
		GenerationID: uuid.New(), DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(),
		Items: []*dto.Chunk{},
	}}
	handler := documentChunksHandler{service: fake}
	router := gin.New()
	router.GET("/knowledge-bases/:id/documents/:document_id/chunks", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, Role: value.RoleMember})
		handler.list(c)
	})
	path := "/knowledge-bases/" + knowledgeBaseID.String() + "/documents/" + documentID.String() + "/chunks?enabled=false&cursor=opaque&limit=17"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, path, nil))

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if fake.input.WorkspaceID != workspaceID || fake.input.KnowledgeBaseID != knowledgeBaseID ||
		fake.input.DocumentID != documentID || fake.input.Enabled == nil || *fake.input.Enabled != enabled ||
		fake.input.Cursor != "opaque" || fake.input.Limit != 17 {
		t.Fatalf("input = %#v", fake.input)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("response = %s, want items array", recorder.Body.String())
	}
}

func TestDocumentChunksHandlerRejectsInvalidQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeDocumentChunksHTTPService{}
	handler := documentChunksHandler{service: fake}
	router := gin.New()
	router.GET("/knowledge-bases/:id/documents/:document_id/chunks", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: uuid.New(), Role: value.RoleMember})
		handler.list(c)
	})
	for _, query := range []string{"enabled=maybe", "limit=0", "limit=201"} {
		path := "/knowledge-bases/" + uuid.NewString() + "/documents/" + uuid.NewString() + "/chunks?" + query
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, path, nil))
		if recorder.Code != stdhttp.StatusBadRequest || fake.calls != 0 {
			t.Fatalf("query %q status/body/calls = %d %s %d", query, recorder.Code, recorder.Body.String(), fake.calls)
		}
	}
}

func TestDocumentChunksRouteIsRegisteredForMembers(t *testing.T) {
	deps := Dependencies{
		Workspaces: newFakeWorkspaceService(), Memberships: &fakeMembershipService{},
		DocumentChunks: &fakeDocumentChunksHTTPService{},
	}
	want := stdhttp.MethodGet + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/:document_id/chunks"
	for _, route := range NewRouter(deps).Routes() {
		if route.Method+" "+route.Path == want {
			return
		}
	}
	t.Fatalf("missing route %s", want)
}

type fakeDocumentChunksHTTPService struct {
	page  *dto.DocumentChunkPage
	err   error
	input service.DocumentChunksInput
	calls int
}

func (s *fakeDocumentChunksHTTPService) List(_ context.Context, input service.DocumentChunksInput) (*dto.DocumentChunkPage, error) {
	s.calls++
	s.input = input
	return s.page, s.err
}
