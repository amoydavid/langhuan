package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
)

type fakeReplayService struct {
	input service.ReplaySearchInput
	run   dto.SearchRunSummary
	err   error
	called bool
}

func (f *fakeReplayService) Replay(_ context.Context, input service.ReplaySearchInput) (*dto.SearchResponse, error) {
	f.called = true
	f.input = input
	if f.err != nil {
		return &dto.SearchResponse{Run: f.run}, f.err
	}
	return &dto.SearchResponse{Run: f.run, Results: []*dto.SearchResult{}}, nil
}

func TestReplayHandlerRejectsAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeReplayService{}
	handler := searchHandler{replaySvc: fake}
	router := gin.New()
	router.POST("/search-runs/:search_id/replay", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			WorkspaceID: uuid.New(), UserID: uuid.New(), Role: value.RoleAdmin,
			PrincipalKind: value.PrincipalAPIKey,
		})
		handler.replayHandler(c)
	})
	searchID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/search-runs/"+searchID.String()+"/replay", bytes.NewBufferString(`{"query":"退款政策"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.False(t, fake.called)
}

func TestReplayHandlerAllowsAdminAndForwardsInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID := uuid.New()
	searchID := uuid.New()
	fake := &fakeReplayService{run: dto.SearchRunSummary{SearchID: uuid.New()}}
	handler := searchHandler{replaySvc: fake}
	router := gin.New()
	router.POST("/search-runs/:search_id/replay", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			WorkspaceID: workspaceID, UserID: uuid.New(), Role: value.RoleAdmin,
			PrincipalKind: value.PrincipalUser,
		})
		handler.replayHandler(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/search-runs/"+searchID.String()+"/replay", bytes.NewBufferString(`{"query":"退款政策"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, fake.called)
	require.Equal(t, workspaceID, fake.input.WorkspaceID)
	require.Equal(t, searchID, fake.input.SearchRunID)
	require.Equal(t, "退款政策", fake.input.Query)
	require.Equal(t, value.RoleAdmin, fake.input.ActorRole)
}

func TestReplayHandlerMapsQueryMismatchTo409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeReplayService{err: domainerrors.ErrSearchQueryMismatch}
	handler := searchHandler{replaySvc: fake}
	router := gin.New()
	router.POST("/search-runs/:search_id/replay", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			WorkspaceID: uuid.New(), UserID: uuid.New(), Role: value.RoleOwner,
			PrincipalKind: value.PrincipalUser,
		})
		handler.replayHandler(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/search-runs/"+uuid.New().String()+"/replay", bytes.NewBufferString(`{"query":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "search_query_mismatch")
}

func TestReplayHandlerMapsGenerationNotAvailableTo409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeReplayService{err: domainerrors.ErrGenerationNotAvailable}
	handler := searchHandler{replaySvc: fake}
	router := gin.New()
	router.POST("/search-runs/:search_id/replay", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{
			WorkspaceID: uuid.New(), UserID: uuid.New(), Role: value.RoleOwner,
			PrincipalKind: value.PrincipalUser,
		})
		handler.replayHandler(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/search-runs/"+uuid.New().String()+"/replay", bytes.NewBufferString(`{"query":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "generation_not_available")
}
