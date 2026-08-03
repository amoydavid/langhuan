package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

func TestParseSingleBearerRejects(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
	}{
		{"multiple headers", []string{"Bearer a", "Bearer b"}},
		{"comma joined", []string{"Bearer a,Bearer b"}},
		{"basic", []string{"Basic abc"}},
		{"empty credential", []string{"Bearer "}},
		{"no header", []string{}},
		{"space inside credential", []string{"Bearer a b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSingleBearer(tc.headers)
			require.Error(t, err, tc.name)
		})
	}
}

func TestParseSingleBearerAccepts(t *testing.T) {
	cred, err := parseSingleBearer([]string{"Bearer lhk_example"})
	require.NoError(t, err)
	require.Equal(t, "lhk_example", cred)
}

// fakeAPIKeyAuthenticator 是 APIKeyAuthenticator 的测试替身。
type fakeAPIKeyAuthenticator struct {
	principal value.AuthContext
	err       error
	called    bool
}

func (f *fakeAPIKeyAuthenticator) Authenticate(ctx context.Context, plaintext string) (value.AuthContext, error) {
	f.called = true
	return f.principal, f.err
}

func newSessionOrAPIKeyRouter(t *testing.T, sessionAuth *fakeAuthService, apiKeyAuth *fakeAPIKeyAuthenticator) (*gin.Engine, uuid.UUID) {
	t.Helper()
	workspaceID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	if sessionAuth == nil {
		sessionAuth = &fakeAuthService{authUser: &model.User{ID: userID}, sessionID: sessionID}
	}
	wsSvc := &fakeWorkspaceService{
		items:     map[uuid.UUID]*dto.Workspace{workspaceID: {ID: workspaceID, Slug: "acme", Metadata: map[string]any{}}},
		slugIndex: map[string]*dto.Workspace{"acme": {ID: workspaceID, Slug: "acme", Metadata: map[string]any{}}},
	}
	mbs := &fakeMembershipService{getResult: &dto.Membership{ID: uuid.New(), WorkspaceID: workspaceID, UserID: userID, Role: value.RoleAdmin}}
	deps := Dependencies{
		Auth:           sessionAuth,
		SessionConfig:  config.SessionConfig{CookieName: "langhuan_session"},
		PublicURLs:     mustPublicURLs(t),
		Workspaces:     wsSvc,
		Memberships:    mbs,
		APIKeyAuth:     apiKeyAuth,
		KnowledgeBases: &fakeKnowledgeBaseService{},
	}
	return NewRouter(deps), workspaceID
}

func TestSessionOrAPIKeyAuthInvalidBearerNeverFallsBackToCookie(t *testing.T) {
	apiKeyAuth := &fakeAPIKeyAuthenticator{err: domainerrors.ErrUnauthorized}
	sessionAuth := &fakeAuthService{authUser: &model.User{ID: uuid.New()}, sessionID: uuid.New()}
	router, _ := newSessionOrAPIKeyRouter(t, sessionAuth, apiKeyAuth)

	// POST /knowledge-bases 由 progGroup 注册，走 SessionOrAPIKeyAuth。
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", nil)
	req.Header.Add("Authorization", "Bearer invalid")
	req.AddCookie(&stdhttp.Cookie{Name: "langhuan_session", Value: sessionAuth.sessionID.String()})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, stdhttp.StatusUnauthorized, rec.Code)
	require.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
	// 无效 Bearer 不应回退到 Session：APIKeyAuth 被调用并返回 401。
	require.True(t, apiKeyAuth.called)
}

func TestSessionOrAPIKeyAuthUsesBearerWhenPresent(t *testing.T) {
	principal := value.NewAPIKeyAuthContext(uuid.New(), uuid.New(), []value.APIScope{value.ScopeKnowledgeBasesWrite}, []uuid.UUID{uuid.New()})
	apiKeyAuth := &fakeAPIKeyAuthenticator{principal: principal}
	router, _ := newSessionOrAPIKeyRouter(t, nil, apiKeyAuth)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", nil)
	req.Header.Add("Authorization", "Bearer lhk_valid")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.True(t, apiKeyAuth.called)
	// Bearer 成功后不应 401（进入 handler 后可能 4xx，但不是鉴权失败）。
	require.NotEqual(t, stdhttp.StatusUnauthorized, rec.Code)
}

func TestSessionOrAPIKeyAuthFallsBackToSessionWithoutAuthorization(t *testing.T) {
	apiKeyAuth := &fakeAPIKeyAuthenticator{}
	sessionAuth := &fakeAuthService{authUser: &model.User{ID: uuid.New()}, sessionID: uuid.New()}
	router, _ := newSessionOrAPIKeyRouter(t, sessionAuth, apiKeyAuth)

	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/knowledge-bases", nil)
	req.AddCookie(&stdhttp.Cookie{Name: "langhuan_session", Value: sessionAuth.sessionID.String()})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.False(t, apiKeyAuth.called, "无 Authorization 时不应调用 API Key 鉴权")
	require.NotEqual(t, stdhttp.StatusUnauthorized, rec.Code)
}

func TestAPIKeyOnlyAuthRejectsCookieWithoutBearer(t *testing.T) {
	apiKeyAuth := &fakeAPIKeyAuthenticator{principal: value.NewAPIKeyAuthContext(uuid.New(), uuid.New(), nil, []uuid.UUID{uuid.New()})}
	deps := Dependencies{
		SessionConfig: config.SessionConfig{CookieName: "langhuan_session"},
		PublicURLs:    mustPublicURLs(t),
		APIKeyAuth:    apiKeyAuth,
		MCPHandler:    stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(stdhttp.StatusOK) }),
	}
	router := NewRouter(deps)

	// 仅 Cookie，无 Authorization。
	req := httptest.NewRequest(stdhttp.MethodPost, "/mcp", nil)
	req.AddCookie(&stdhttp.Cookie{Name: "langhuan_session", Value: uuid.NewString()})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, stdhttp.StatusUnauthorized, rec.Code)
	require.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
	require.False(t, apiKeyAuth.called)
}

func TestAPIKeyOnlyAuthAcceptsValidBearer(t *testing.T) {
	apiKeyAuth := &fakeAPIKeyAuthenticator{principal: value.NewAPIKeyAuthContext(uuid.New(), uuid.New(), nil, []uuid.UUID{uuid.New()})}
	deps := Dependencies{
		SessionConfig: config.SessionConfig{CookieName: "langhuan_session"},
		PublicURLs:    mustPublicURLs(t),
		APIKeyAuth:    apiKeyAuth,
		MCPHandler:    stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) { w.WriteHeader(stdhttp.StatusOK) }),
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(stdhttp.MethodPost, "/mcp", nil)
	req.Header.Add("Authorization", "Bearer lhk_valid")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.True(t, apiKeyAuth.called)
}
