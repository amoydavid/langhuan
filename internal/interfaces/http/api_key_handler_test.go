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
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// fakeAPIKeyService 是 APIKeyServiceHTTP 的测试替身。
type fakeAPIKeyService struct {
	createResult *service.CreateAPIKeyResult
	createErr    error
	updateItem   dto.WorkspaceAPIKey
	updateErr    error
	updateCalled bool
	updateInput  service.UpdateAPIKeyInput
	getItem      dto.WorkspaceAPIKey
	getErr       error
	listItems    []dto.WorkspaceAPIKey
	revealResult *service.RevealResult
	revealErr    error
	revokeErr    error
	revokeCalled bool
	createCalled bool
	publicURLs   dto.PublicURLs
}

func (f *fakeAPIKeyService) Create(ctx context.Context, input service.CreateAPIKeyInput) (*service.CreateAPIKeyResult, error) {
	f.createCalled = true
	return f.createResult, f.createErr
}
func (f *fakeAPIKeyService) Update(ctx context.Context, input service.UpdateAPIKeyInput) (dto.WorkspaceAPIKey, error) {
	f.updateCalled = true
	f.updateInput = input
	return f.updateItem, f.updateErr
}
func (f *fakeAPIKeyService) Get(ctx context.Context, workspaceID uuid.UUID, role value.WorkspaceRole, keyID uuid.UUID) (dto.WorkspaceAPIKey, error) {
	return f.getItem, f.getErr
}
func (f *fakeAPIKeyService) List(ctx context.Context, workspaceID uuid.UUID, role value.WorkspaceRole) ([]dto.WorkspaceAPIKey, error) {
	return f.listItems, nil
}
func (f *fakeAPIKeyService) Reveal(ctx context.Context, workspaceID uuid.UUID, role value.WorkspaceRole, keyID uuid.UUID) (*service.RevealResult, error) {
	return f.revealResult, f.revealErr
}
func (f *fakeAPIKeyService) Revoke(ctx context.Context, workspaceID, actorID uuid.UUID, role value.WorkspaceRole, keyID uuid.UUID) error {
	f.revokeCalled = true
	return f.revokeErr
}
func (f *fakeAPIKeyService) PublicURLs() dto.PublicURLs { return f.publicURLs }

// newAPIKeyRouterFixture 构造一个仅含 SessionAuth + workspace admin 的 router。
func newAPIKeyRouterFixture(t *testing.T, role value.WorkspaceRole, keys *fakeAPIKeyService) (*gin.Engine, uuid.UUID, uuid.UUID, uuid.UUID) {
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
		Auth:          authSvc,
		SessionConfig: config.SessionConfig{CookieName: "langhuan_session"},
		PublicURLs:    mustPublicURLs(t),
		APIKeys:       keys,
		Workspaces:    wsSvc,
		Memberships:   mbs,
	}
	engine := NewRouter(deps)
	return engine, workspaceID, userID, sessionID
}

func mustPublicURLs(t *testing.T) *service.PublicURLBuilder {
	t.Helper()
	b, err := service.NewPublicURLBuilder("https://langhuan.example.com")
	require.NoError(t, err)
	return b
}

func authedAPIKeyRequest(router *gin.Engine, sessionID uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	reader := strings.NewReader("")
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(&stdhttp.Cookie{Name: "langhuan_session", Value: sessionID.String()})
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAPIKeyHandlerRevealResponseIsNeverCached(t *testing.T) {
	keys := &fakeAPIKeyService{
		publicURLs:   dto.PublicURLs{BaseURL: "https://x.example.com", WebURL: "https://x.example.com/", RESTBaseURL: "https://x.example.com/api/v1", MCPURL: "https://x.example.com/mcp"},
		revealResult: &service.RevealResult{APIKey: "lhk_" + strings.Repeat("x", 43)},
	}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleAdmin, keys)
	keyID := uuid.New()
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodPost, "/api/v1/workspaces/acme/api-keys/"+keyID.String()+"/reveal", "")
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", rec.Header().Get("Pragma"))
}

func TestAPIKeyHandlerCreateReturnsSecretAndItem(t *testing.T) {
	item := dto.WorkspaceAPIKey{ID: uuid.New(), Name: "Agent", TokenPrefix: "lhk_a1b2c3d4"}
	keys := &fakeAPIKeyService{
		publicURLs:   dto.PublicURLs{BaseURL: "https://x.example.com"},
		createResult: &service.CreateAPIKeyResult{APIKey: "lhk_" + strings.Repeat("x", 43), Item: item},
	}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleAdmin, keys)
	body := `{"name":"Agent","knowledge_base_ids":["00000000-0000-4000-8000-000000000001"],"scopes":["search:read"]}`
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodPost, "/api/v1/workspaces/acme/api-keys", body)
	require.Equal(t, stdhttp.StatusCreated, rec.Code)
	require.Contains(t, rec.Body.String(), "lhk_")
	require.True(t, keys.createCalled)
}

func TestAPIKeyHandlerMemberCannotManageKeys(t *testing.T) {
	keys := &fakeAPIKeyService{publicURLs: dto.PublicURLs{BaseURL: "https://x.example.com"}}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleMember, keys)
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodGet, "/api/v1/workspaces/acme/api-keys", "")
	// member 不满足 admin 角色。
	require.True(t, rec.Code == stdhttp.StatusForbidden || rec.Code == stdhttp.StatusNotFound, "member status = %d", rec.Code)
}

func TestAPIKeyHandlerRevokeReturns204(t *testing.T) {
	keys := &fakeAPIKeyService{publicURLs: dto.PublicURLs{BaseURL: "https://x.example.com"}}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleAdmin, keys)
	keyID := uuid.New()
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodDelete, "/api/v1/workspaces/acme/api-keys/"+keyID.String(), "")
	require.Equal(t, stdhttp.StatusNoContent, rec.Code)
	require.True(t, keys.revokeCalled)
}

func TestAPIKeyHandlerRevealMapsSecretUnavailable(t *testing.T) {
	keys := &fakeAPIKeyService{publicURLs: dto.PublicURLs{BaseURL: "https://x.example.com"}, revealErr: domainerrors.ErrAPIKeySecretUnavailable}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleAdmin, keys)
	keyID := uuid.New()
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodPost, "/api/v1/workspaces/acme/api-keys/"+keyID.String()+"/reveal", "")
	require.Equal(t, stdhttp.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "api_key_secret_unavailable")
	// 不泄漏密码学原因。
	require.NotContains(t, rec.Body.String(), "nonce")
	require.NotContains(t, rec.Body.String(), "cipher")
}

func TestAPIKeyHandlerUpdateReturnsItem(t *testing.T) {
	item := dto.WorkspaceAPIKey{ID: uuid.New(), Name: "Updated", TokenPrefix: "lhk_a1b2c3d4"}
	keys := &fakeAPIKeyService{
		publicURLs: dto.PublicURLs{BaseURL: "https://x.example.com"},
		updateItem: item,
	}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleAdmin, keys)
	keyID := uuid.New()
	body := `{"name":"Updated","knowledge_base_ids":["00000000-0000-4000-8000-000000000001"],"scopes":["search:read"],"expiration":{"type":"never"}}`
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodPatch, "/api/v1/workspaces/acme/api-keys/"+keyID.String(), body)
	require.Equal(t, stdhttp.StatusOK, rec.Code)
	require.True(t, keys.updateCalled)
	require.Equal(t, keyID, keys.updateInput.KeyID)
	require.Equal(t, "Updated", keys.updateInput.Name)
}

func TestAPIKeyHandlerUpdateMapsImmutable(t *testing.T) {
	keys := &fakeAPIKeyService{
		publicURLs: dto.PublicURLs{BaseURL: "https://x.example.com"},
		updateErr:  domainerrors.ErrAPIKeyImmutable,
	}
	router, _, _, sessionID := newAPIKeyRouterFixture(t, value.RoleAdmin, keys)
	keyID := uuid.New()
	body := `{"name":"x","knowledge_base_ids":["00000000-0000-4000-8000-000000000001"],"scopes":["search:read"]}`
	rec := authedAPIKeyRequest(router, sessionID, stdhttp.MethodPatch, "/api/v1/workspaces/acme/api-keys/"+keyID.String(), body)
	require.Equal(t, stdhttp.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "api_key_immutable")
}
