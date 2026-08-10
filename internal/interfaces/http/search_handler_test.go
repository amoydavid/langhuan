package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestSearchHandlerAllowsMemberAndForwardsWorkspaceScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, userID, knowledgeBaseID := uuid.New(), uuid.New(), uuid.New()
	fake := &searchHTTPServiceFake{results: []*dto.SearchResult{{ChunkID: uuid.New(), Content: "answer"}}, searchID: uuid.New()}
	handler := searchHandler{service: fake}
	router := gin.New()
	router.POST("/knowledge-bases/:id/search", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: value.RoleMember})
		handler.search(c)
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/knowledge-bases/"+knowledgeBaseID.String()+"/search",
		bytes.NewBufferString(`{"query":"如何退款","final_top_k":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || len(fake.inputs) != 1 {
		t.Fatalf("status/body=%d %s inputs=%#v", recorder.Code, recorder.Body.String(), fake.inputs)
	}
	if fake.inputs[0].WorkspaceID != workspaceID || fake.inputs[0].KnowledgeBaseID != knowledgeBaseID ||
		fake.inputs[0].Query != "如何退款" || fake.inputs[0].FinalTopK == nil || *fake.inputs[0].FinalTopK != 5 {
		t.Fatalf("input = %#v", fake.inputs[0])
	}
}

func TestSearchRouteIsWorkspaceMemberScoped(t *testing.T) {
	deps := Dependencies{
		Workspaces: newFakeWorkspaceService(), Memberships: &fakeMembershipService{},
		Search: &searchHTTPServiceFake{},
	}
	want := http.MethodPost + " /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/search"
	for _, route := range NewRouter(deps).Routes() {
		if route.Method+" "+route.Path == want {
			return
		}
	}
	t.Fatalf("missing route %s", want)
}

type searchHTTPServiceFake struct {
	inputs   []service.SearchInput
	results  []*dto.SearchResult
	searchID uuid.UUID
	status   value.RetrievalStatus
	genIDs   []uuid.UUID
}

func (s *searchHTTPServiceFake) Search(_ context.Context, input service.SearchInput) (*dto.SearchResponse, error) {
	s.inputs = append(s.inputs, input)
	searchID := s.searchID
	if searchID == uuid.Nil {
		searchID = uuid.New()
	}
	status := s.status
	if status == "" {
		status = value.RetrievalStatusAvailable
	}
	snaps := make([]dto.GenerationSnapshot, 0, len(s.genIDs))
	for _, gid := range s.genIDs {
		snaps = append(snaps, dto.GenerationSnapshot{GenerationID: gid})
	}
	return &dto.SearchResponse{
		Run:     dto.SearchRunSummary{SearchID: searchID, RetrievalStatus: status, GenerationSnapshots: snaps},
		Results: s.results,
	}, nil
}

func TestSingleSearchKeepsArrayBodyAndAddsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, userID, knowledgeBaseID := uuid.New(), uuid.New(), uuid.New()
	searchID := uuid.New()
	genID := uuid.New()
	fake := &searchHTTPServiceFake{
		results:  []*dto.SearchResult{{ChunkID: uuid.New(), Content: "answer"}},
		searchID: searchID,
		genIDs:   []uuid.UUID{genID},
		status:   value.RetrievalStatusAvailable,
	}
	handler := searchHandler{service: fake}
	router := gin.New()
	router.POST("/knowledge-bases/:id/search", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: value.RoleMember})
		handler.search(c)
	})
	body := `{"query":"如何退款"}`
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+knowledgeBaseID.String()+"/search", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// Body 继续为数组。
	if !strings.Contains(recorder.Body.String(), "[") {
		t.Fatalf("body should be array: %s", recorder.Body.String())
	}
	// Headers。
	if got := recorder.Header().Get("X-Search-ID"); got != searchID.String() {
		t.Fatalf("X-Search-ID = %q want %q", got, searchID.String())
	}
	if got := recorder.Header().Get("X-Retrieval-Status"); got != "available" {
		t.Fatalf("X-Retrieval-Status = %q", got)
	}
	if got := recorder.Header().Get("X-Generation-IDs"); got != genID.String() {
		t.Fatalf("X-Generation-IDs = %q want %q", got, genID.String())
	}
}
