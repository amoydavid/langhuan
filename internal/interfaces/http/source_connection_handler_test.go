package http

import (
	"context"
	"encoding/json"
	"net/http"
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

// fakeSourceConnectionService 是 SourceConnectionService 的测试替身。
// 它记录每次调用并返回预设结果；不接触真实加密或 DB。
type fakeSourceConnectionService struct {
	createInput *service.CreateSourceConnectionInput
	createItem  *dto.SourceConnection
	createErr   error

	updateInput *service.UpdateSourceConnectionInput
	updateItem  *dto.SourceConnection
	updateErr   error

	listItems []dto.SourceConnection
	listErr   error

	getItem *dto.SourceConnection
	getErr  error

	deleteWorkspaceID uuid.UUID
	deleteID          uuid.UUID
	deleteErr         error
}

func (f *fakeSourceConnectionService) Create(_ context.Context, input service.CreateSourceConnectionInput) (*dto.SourceConnection, error) {
	clone := input
	f.createInput = &clone
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createItem, nil
}

func (f *fakeSourceConnectionService) List(_ context.Context, _ uuid.UUID) ([]dto.SourceConnection, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listItems == nil {
		return []dto.SourceConnection{}, nil
	}
	return f.listItems, nil
}

func (f *fakeSourceConnectionService) Get(_ context.Context, _, _ uuid.UUID) (*dto.SourceConnection, error) {
	return f.getItem, f.getErr
}

func (f *fakeSourceConnectionService) Update(_ context.Context, input service.UpdateSourceConnectionInput) (*dto.SourceConnection, error) {
	clone := input
	f.updateInput = &clone
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.updateItem, nil
}

func (f *fakeSourceConnectionService) Delete(_ context.Context, workspaceID, id uuid.UUID) error {
	f.deleteWorkspaceID = workspaceID
	f.deleteID = id
	return f.deleteErr
}

// newSourceConnectionFixture 构造一个 Session-only 的工作区 admin 路由环境。
func newSourceConnectionFixture(t *testing.T, role value.WorkspaceRole, svc *fakeSourceConnectionService) (*gin.Engine, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	workspaceID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	authSvc := &fakeAuthService{
		authUser:  &model.User{ID: userID},
		sessionID: sessionID,
	}
	wsSvc := &fakeWorkspaceService{
		items:     map[uuid.UUID]*dto.Workspace{workspaceID: {ID: workspaceID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}}},
		slugIndex: map[string]*dto.Workspace{"acme": {ID: workspaceID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}}},
	}
	mbs := &fakeMembershipService{getResult: &dto.Membership{ID: uuid.New(), WorkspaceID: workspaceID, UserID: userID, Role: role}}
	deps := Dependencies{
		Auth:              authSvc,
		SessionConfig:     config.SessionConfig{CookieName: "langhuan_session"},
		PublicURLs:        mustPublicURLs(t),
		Workspaces:        wsSvc,
		Memberships:       mbs,
		SourceConnections: svc,
	}
	return NewRouter(deps), workspaceID, userID, sessionID
}

func authedSourceConnectionRequest(router *gin.Engine, sessionID uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(&http.Cookie{Name: "langhuan_session", Value: sessionID.String()})
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestSourceConnectionHandlerAdminCreateReturns201WithoutSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	item := &dto.SourceConnection{
		ID: uuid.New(), Provider: "feishu", Name: "主公司飞书", AppID: "cli_a1", Status: "active",
	}
	svc := &fakeSourceConnectionService{createItem: item}
	router, workspaceID, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	body := `{"provider":"feishu","name":"主公司飞书","app_id":"cli_a1","app_secret":"topsecret"}`
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodPost, "/api/v1/workspaces/acme/source-connections", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	// service 收到的是明文 secret（仅入参），workspace 来自 authContext。
	require.NotNil(t, svc.createInput)
	require.Equal(t, workspaceID, svc.createInput.WorkspaceID)
	require.Equal(t, "feishu", svc.createInput.Provider)
	require.Equal(t, "cli_a1", svc.createInput.AppID)
	require.Equal(t, "topsecret", svc.createInput.AppSecret)

	// 响应 DTO 绝不能包含 app_secret 字段。
	require.NotContains(t, rec.Body.String(), "app_secret")
	require.NotContains(t, rec.Body.String(), "secret")
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	_, hasSecret := raw["app_secret"]
	require.False(t, hasSecret, "app_secret 字段不应出现在响应 JSON 中")
}

func TestSourceConnectionHandlerAdminListReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	items := []dto.SourceConnection{
		{ID: uuid.New(), Provider: "feishu", Name: "a", AppID: "cli_a", Status: "active"},
		{ID: uuid.New(), Provider: "feishu", Name: "b", AppID: "cli_b", Status: "active"},
	}
	svc := &fakeSourceConnectionService{listItems: items}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	rec := authedSourceConnectionRequest(router, sessionID, http.MethodGet, "/api/v1/workspaces/acme/source-connections", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var got []dto.SourceConnection
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	require.NotContains(t, rec.Body.String(), "app_secret")
}

func TestSourceConnectionHandlerAdminListEmptyReturnsBrackets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	rec := authedSourceConnectionRequest(router, sessionID, http.MethodGet, "/api/v1/workspaces/acme/source-connections", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "[]", strings.TrimSpace(rec.Body.String()))
}

func TestSourceConnectionHandlerAdminUpdateRotatesSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	item := &dto.SourceConnection{ID: uuid.New(), Provider: "feishu", Name: "renamed", AppID: "cli_a1", Status: "active"}
	svc := &fakeSourceConnectionService{updateItem: item}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	connectionID := uuid.New()
	body := `{"name":"renamed","app_secret":"rotated-secret"}`
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodPatch, "/api/v1/workspaces/acme/source-connections/"+connectionID.String(), body)
	require.Equal(t, http.StatusOK, rec.Code)

	require.NotNil(t, svc.updateInput)
	require.Equal(t, connectionID, svc.updateInput.ID)
	require.NotNil(t, svc.updateInput.Name)
	require.Equal(t, "renamed", *svc.updateInput.Name)
	require.NotNil(t, svc.updateInput.AppSecret)
	require.Equal(t, "rotated-secret", *svc.updateInput.AppSecret)

	require.NotContains(t, rec.Body.String(), "app_secret")
	require.NotContains(t, rec.Body.String(), "secret")
}

func TestSourceConnectionHandlerAdminDeleteReturns204(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{}
	router, workspaceID, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	connectionID := uuid.New()
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodDelete, "/api/v1/workspaces/acme/source-connections/"+connectionID.String(), "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, workspaceID, svc.deleteWorkspaceID)
	require.Equal(t, connectionID, svc.deleteID)
}

func TestSourceConnectionHandlerMemberCannotCreate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{createItem: &dto.SourceConnection{}}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleMember, svc)

	body := `{"provider":"feishu","name":"x","app_id":"cli_a","app_secret":"s"}`
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodPost, "/api/v1/workspaces/acme/source-connections", body)
	require.Equal(t, http.StatusForbidden, rec.Code)
	// member 角色被中间件拦下，service 不应被调用。
	require.Nil(t, svc.createInput)
}

func TestSourceConnectionHandlerCreateRejectsEmptyAppSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{createItem: &dto.SourceConnection{}}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	body := `{"provider":"feishu","name":"x","app_id":"cli_a","app_secret":""}`
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodPost, "/api/v1/workspaces/acme/source-connections", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Nil(t, svc.createInput)
}

func TestSourceConnectionHandlerCreateRejectsUnsupportedProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{createItem: &dto.SourceConnection{}}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	body := `{"provider":"slack","name":"x","app_id":"cli_a","app_secret":"s"}`
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodPost, "/api/v1/workspaces/acme/source-connections", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSourceConnectionHandlerCreateRejectsUnknownField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{createItem: &dto.SourceConnection{}}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	body := `{"provider":"feishu","name":"x","app_id":"cli_a","app_secret":"s","metadata":{}}`
	rec := authedSourceConnectionRequest(router, sessionID, http.MethodPost, "/api/v1/workspaces/acme/source-connections", body)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSourceConnectionHandlerGetReturnsItemWithoutSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	item := &dto.SourceConnection{ID: uuid.New(), Provider: "feishu", Name: "a", AppID: "cli_a", Status: "active"}
	svc := &fakeSourceConnectionService{getItem: item}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	rec := authedSourceConnectionRequest(router, sessionID, http.MethodGet, "/api/v1/workspaces/acme/source-connections/"+item.ID.String(), "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "app_secret")
}

func TestSourceConnectionHandlerRejectsInvalidConnectionID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{}
	router, _, _, sessionID := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	rec := authedSourceConnectionRequest(router, sessionID, http.MethodGet, "/api/v1/workspaces/acme/source-connections/not-a-uuid", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestSourceConnectionRouteRequiresSession 断言凭证管理路由对 Bearer API Key 不可达。
// 只有浏览器 session 能到达 handler；Bearer token 没有 cookie 会被 SessionAuth 挡在 401。
func TestSourceConnectionRouteRequiresSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &fakeSourceConnectionService{}
	router, _, _, _ := newSourceConnectionFixture(t, value.RoleAdmin, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/acme/source-connections", nil)
	req.Header.Set("Authorization", "Bearer lhk_test")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// 没有 session cookie -> 401，API Key 不被接受。
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
