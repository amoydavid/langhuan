package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

type fakeWorkspaceSearchSettingsHTTPService struct {
	item        *dto.WorkspaceSearchSettings
	updateErr   error
	updateRole  value.WorkspaceRole
	updateInput service.UpdateWorkspaceSearchSettingsInput
}

func (s *fakeWorkspaceSearchSettingsHTTPService) Get(context.Context, uuid.UUID) (*dto.WorkspaceSearchSettings, error) {
	return s.item, nil
}
func (s *fakeWorkspaceSearchSettingsHTTPService) Update(_ context.Context, _ uuid.UUID, role value.WorkspaceRole, input service.UpdateWorkspaceSearchSettingsInput) (*dto.WorkspaceSearchSettings, error) {
	s.updateRole, s.updateInput = role, input
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.item, nil
}

func TestWorkspaceSearchSettingsHandlerUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, userID := uuid.New(), uuid.New()
	settings := &fakeWorkspaceSearchSettingsHTTPService{item: &dto.WorkspaceSearchSettings{WorkspaceID: workspaceID}}
	handler := workspaceSearchSettingsHandler{service: settings}
	router := gin.New()
	router.PUT("/settings", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: value.RoleAdmin})
		handler.update(c)
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPut, "/settings", strings.NewReader(`{"rerank":{"enabled":false}}`))
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(recorder, req)
	require.Equal(t, stdhttp.StatusOK, recorder.Code)
	require.Equal(t, value.RoleAdmin, settings.updateRole)
	require.Equal(t, userID, settings.updateInput.ActorID)
	require.False(t, settings.updateInput.RerankEnabled)
}

func TestWorkspaceSearchSettingsRouteRestrictsWriteToAdmin(t *testing.T) {
	for _, test := range []struct {
		name string
		role value.WorkspaceRole
		code int
	}{
		{name: "admin", role: value.RoleAdmin, code: stdhttp.StatusOK},
		{name: "owner", role: value.RoleOwner, code: stdhttp.StatusOK},
		{name: "member", role: value.RoleMember, code: stdhttp.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspaceID, userID, sessionID := uuid.New(), uuid.New(), uuid.New()
			settings := &fakeWorkspaceSearchSettingsHTTPService{item: &dto.WorkspaceSearchSettings{WorkspaceID: workspaceID}}
			router := NewRouter(Dependencies{
				Auth:          &fakeAuthService{authUser: &model.User{ID: userID}, sessionID: sessionID},
				SessionConfig: config.SessionConfig{CookieName: "langhuan_session"},
				Workspaces: &fakeWorkspaceService{
					items:     map[uuid.UUID]*dto.Workspace{workspaceID: {ID: workspaceID, Slug: "acme"}},
					slugIndex: map[string]*dto.Workspace{"acme": {ID: workspaceID, Slug: "acme"}},
				},
				Memberships:             &fakeMembershipService{getResult: &dto.Membership{WorkspaceID: workspaceID, UserID: userID, Role: test.role}},
				WorkspaceSearchSettings: settings,
			})
			req := httptest.NewRequest(stdhttp.MethodPut, "/api/v1/workspaces/acme/search-settings", strings.NewReader(`{"rerank":{"enabled":false}}`))
			req.Header.Set("content-type", "application/json")
			req.AddCookie(&stdhttp.Cookie{Name: "langhuan_session", Value: sessionID.String()})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			require.Equal(t, test.code, recorder.Code)
		})
	}
}
