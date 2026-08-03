package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// --- fakes ---

type fakeSessionAuthenticator struct {
	sessionID uuid.UUID
	user      *model.User
	err       error
	called    bool
}

func (f *fakeSessionAuthenticator) Authenticate(_ context.Context, sessionID uuid.UUID) (*model.User, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	if sessionID != f.sessionID {
		return nil, domainerrors.ErrUnauthorized
	}
	return f.user, nil
}

type fakeWorkspaceResolver struct {
	slug string
	ws   *dto.Workspace
	err  error
}

func (f *fakeWorkspaceResolver) GetBySlug(_ context.Context, slug string) (*dto.Workspace, error) {
	if f.err != nil {
		return nil, f.err
	}
	if slug != f.slug {
		return nil, domainerrors.ErrNotFound
	}
	return f.ws, nil
}

type fakeMembershipResolver struct {
	workspaceID uuid.UUID
	userID      uuid.UUID
	membership  *dto.Membership
	err         error
}

func (f *fakeMembershipResolver) Get(_ context.Context, workspaceID, userID uuid.UUID) (*dto.Membership, error) {
	if f.err != nil {
		return nil, f.err
	}
	if workspaceID != f.workspaceID || userID != f.userID {
		return nil, domainerrors.ErrNotFound
	}
	return f.membership, nil
}

// --- helpers ---

const testCookieName = "langhuan_session"

// authEchoHandler returns 200 and echoes the AuthContext (if present) as JSON.
func authEchoHandler(c *gin.Context) {
	val, exists := c.Get(authContextKey)
	if !exists {
		c.JSON(stdhttp.StatusOK, gin.H{"auth": nil})
		return
	}
	authCtx, _ := val.(value.AuthContext)
	c.JSON(stdhttp.StatusOK, gin.H{
		"user_id":           authCtx.UserID,
		"is_platform_admin": authCtx.IsPlatformAdmin,
		"workspace_id":      authCtx.WorkspaceID,
		"role":              string(authCtx.Role),
	})
}

func newTestRouter(middlewares ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middlewares...)
	r.GET("/", authEchoHandler)
	return r
}

func doRequest(t *testing.T, router *gin.Engine, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	if cookieValue != "" {
		req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: cookieValue})
	}
	router.ServeHTTP(rec, req)
	return rec
}

func parseAuthBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// --- SessionAuth tests ---

func TestSessionAuthRejectsMissingCookie(t *testing.T) {
	auth := &fakeSessionAuthenticator{err: domainerrors.ErrUnauthorized}
	router := newTestRouter(SessionAuth(auth, testCookieName))

	rec := doRequest(t, router, "")

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
	if auth.called {
		t.Fatal("authenticator should not be called when cookie is missing")
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "unauthorized" {
		t.Fatalf("code = %q, want unauthorized", body.Error.Code)
	}
}

func TestSessionAuthRejectsInvalidCookie(t *testing.T) {
	auth := &fakeSessionAuthenticator{err: domainerrors.ErrUnauthorized}
	router := newTestRouter(SessionAuth(auth, testCookieName))

	rec := doRequest(t, router, "not-a-uuid")

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
	if auth.called {
		t.Fatal("authenticator should not be called when cookie is not a uuid")
	}
}

func TestSessionAuthRejectsExpiredOrRevokedSession(t *testing.T) {
	sessionID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, err: domainerrors.ErrUnauthorized}
	router := newTestRouter(SessionAuth(auth, testCookieName))

	rec := doRequest(t, router, sessionID.String())

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
	if !auth.called {
		t.Fatal("authenticator should be called")
	}
}

func TestSessionAuthMapsInternalErrorTo500(t *testing.T) {
	sessionID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, err: context.DeadlineExceeded}
	router := newTestRouter(SessionAuth(auth, testCookieName))

	rec := doRequest(t, router, sessionID.String())

	if rec.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", body.Error.Code)
	}
}

func TestSessionAuthSetsAuthContextOnValidCookie(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	user := &model.User{ID: userID, IsPlatformAdmin: true}
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: user}
	router := newTestRouter(SessionAuth(auth, testCookieName))

	rec := doRequest(t, router, sessionID.String())

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	m := parseAuthBody(t, rec.Body.Bytes())
	if m["user_id"] != userID.String() {
		t.Fatalf("user_id = %v, want %s", m["user_id"], userID)
	}
	if m["is_platform_admin"] != true {
		t.Fatalf("is_platform_admin = %v, want true", m["is_platform_admin"])
	}
	// workspace-scoped fields should be zero on a session-only context.
	if m["workspace_id"] != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("workspace_id = %v, want zero uuid", m["workspace_id"])
	}
	if m["role"] != "" {
		t.Fatalf("role = %v, want empty", m["role"])
	}
}

// --- RequirePlatformAdmin tests ---

func TestRequirePlatformAdminReturns403ForNonAdmin(t *testing.T) {
	sessionID := uuid.New()
	user := &model.User{ID: uuid.New(), IsPlatformAdmin: false}
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: user}
	router := newTestRouter(
		SessionAuth(auth, testCookieName),
		RequirePlatformAdmin(),
	)

	rec := doRequest(t, router, sessionID.String())

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "forbidden" {
		t.Fatalf("code = %q, want forbidden", body.Error.Code)
	}
}

func TestRequirePlatformAdminAllowsAdmin(t *testing.T) {
	sessionID := uuid.New()
	user := &model.User{ID: uuid.New(), IsPlatformAdmin: true}
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: user}
	router := newTestRouter(
		SessionAuth(auth, testCookieName),
		RequirePlatformAdmin(),
	)

	rec := doRequest(t, router, sessionID.String())

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
}

// --- RequireWorkspace tests ---

func newWorkspaceRouter(auth *fakeSessionAuthenticator, ws *fakeWorkspaceResolver, mb *fakeMembershipResolver) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SessionAuth(auth, testCookieName))
	r.GET("/w/:workspace_slug", RequireWorkspace(ws, mb), authEchoHandler)
	return r
}

func doWorkspaceRequest(t *testing.T, router *gin.Engine, slug, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/w/"+slug, nil)
	if cookieValue != "" {
		req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: cookieValue})
	}
	router.ServeHTTP(rec, req)
	return rec
}

func TestRequireWorkspaceReturns404ForMissingSlug(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "real-slug", ws: &dto.Workspace{ID: uuid.New(), Name: "Real", Slug: "real-slug"}, err: domainerrors.ErrNotFound}
	mb := &fakeMembershipResolver{}
	router := newWorkspaceRouter(auth, ws, mb)

	rec := doWorkspaceRequest(t, router, "missing-slug", sessionID.String())

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", body.Error.Code)
	}
	// No existence leak: workspace name/ID/slug must not appear in the body.
	raw := rec.Body.String()
	if containsAny(raw, "Real", "missing-slug") {
		t.Fatalf("404 body leaks workspace info: %s", raw)
	}
}

func TestRequireWorkspaceReturns404ForNoMembership(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	wsID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: wsID, Name: "Acme Inc", Slug: "acme"}}
	mb := &fakeMembershipResolver{workspaceID: wsID, err: domainerrors.ErrNotFound}
	router := newWorkspaceRouter(auth, ws, mb)

	rec := doWorkspaceRequest(t, router, "acme", sessionID.String())

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("code = %q, want not_found", body.Error.Code)
	}
	// No existence leak.
	raw := rec.Body.String()
	if containsAny(raw, "Acme Inc", wsID.String()) {
		t.Fatalf("404 body leaks workspace info: %s", raw)
	}
}

// TestRequireWorkspaceReturnsIdentical404ForMissingSlugAndNoMembership asserts both
// conditions produce the SAME body, so an attacker cannot distinguish "workspace
// does not exist" from "I'm not a member".
func TestRequireWorkspaceReturnsIdentical404ForMissingSlugAndNoMembership(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	wsID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}

	// missing slug
	wsMissing := &fakeWorkspaceResolver{slug: "x", ws: nil, err: domainerrors.ErrNotFound}
	routerMissing := newWorkspaceRouter(auth, wsMissing, &fakeMembershipResolver{})
	recMissing := doWorkspaceRequest(t, routerMissing, "missing-slug", sessionID.String())

	// no membership
	wsExists := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}}
	mb := &fakeMembershipResolver{workspaceID: wsID, err: domainerrors.ErrNotFound}
	routerNoMember := newWorkspaceRouter(auth, wsExists, mb)
	recNoMember := doWorkspaceRequest(t, routerNoMember, "acme", sessionID.String())

	if recMissing.Code != stdhttp.StatusNotFound || recNoMember.Code != stdhttp.StatusNotFound {
		t.Fatalf("statuses = %d / %d, both want 404", recMissing.Code, recNoMember.Code)
	}
	var missingBody, noMemberBody errorBody
	if err := json.Unmarshal(recMissing.Body.Bytes(), &missingBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recNoMember.Body.Bytes(), &noMemberBody); err != nil {
		t.Fatal(err)
	}
	if missingBody.Error.Code != noMemberBody.Error.Code {
		t.Fatalf("codes differ: %q vs %q", missingBody.Error.Code, noMemberBody.Error.Code)
	}
	if missingBody.Error.Message != noMemberBody.Error.Message {
		t.Fatalf("messages differ: %q vs %q", missingBody.Error.Message, noMemberBody.Error.Message)
	}
}

func TestRequireWorkspaceSetsWorkspaceIDAndRoleOnSuccess(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	wsID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: wsID, Name: "Acme", Slug: "acme"}}
	mb := &fakeMembershipResolver{
		workspaceID: wsID,
		userID:      userID,
		membership:  &dto.Membership{ID: uuid.New(), WorkspaceID: wsID, UserID: userID, Role: value.RoleAdmin},
	}
	router := newWorkspaceRouter(auth, ws, mb)

	rec := doWorkspaceRequest(t, router, "acme", sessionID.String())

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	m := parseAuthBody(t, rec.Body.Bytes())
	if m["workspace_id"] != wsID.String() {
		t.Fatalf("workspace_id = %v, want %s", m["workspace_id"], wsID)
	}
	if m["role"] != string(value.RoleAdmin) {
		t.Fatalf("role = %v, want %s", m["role"], value.RoleAdmin)
	}
}

func TestRequireWorkspaceMapsInternalErrorTo500(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: uuid.New()}, err: context.DeadlineExceeded}
	router := newWorkspaceRouter(auth, ws, &fakeMembershipResolver{})

	rec := doWorkspaceRequest(t, router, "acme", sessionID.String())

	if rec.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", rec.Code, rec.Body.String())
	}
}

// --- RequireWorkspaceRole tests ---

func newRoleRouter(auth *fakeSessionAuthenticator, ws *fakeWorkspaceResolver, mb *fakeMembershipResolver, minRole value.WorkspaceRole) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SessionAuth(auth, testCookieName))
	r.GET("/w/:workspace_slug", RequireWorkspace(ws, mb), RequireWorkspaceRole(minRole), authEchoHandler)
	return r
}

func TestRequireWorkspaceRoleReturns403ForInsufficientRole(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	wsID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: wsID, Slug: "acme"}}
	mb := &fakeMembershipResolver{
		workspaceID: wsID,
		userID:      userID,
		membership:  &dto.Membership{WorkspaceID: wsID, UserID: userID, Role: value.RoleMember},
	}
	router := newRoleRouter(auth, ws, mb, value.RoleAdmin)

	rec := doWorkspaceRequest(t, router, "acme", sessionID.String())

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "forbidden" {
		t.Fatalf("code = %q, want forbidden", body.Error.Code)
	}
}

func TestRequireWorkspaceRoleAllowsSufficientRole(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	wsID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: wsID, Slug: "acme"}}
	mb := &fakeMembershipResolver{
		workspaceID: wsID,
		userID:      userID,
		membership:  &dto.Membership{WorkspaceID: wsID, UserID: userID, Role: value.RoleOwner},
	}
	router := newRoleRouter(auth, ws, mb, value.RoleAdmin)

	rec := doWorkspaceRequest(t, router, "acme", sessionID.String())

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRequireWorkspaceRoleAllowsEqualRole(t *testing.T) {
	sessionID := uuid.New()
	userID := uuid.New()
	wsID := uuid.New()
	auth := &fakeSessionAuthenticator{sessionID: sessionID, user: &model.User{ID: userID}}
	ws := &fakeWorkspaceResolver{slug: "acme", ws: &dto.Workspace{ID: wsID, Slug: "acme"}}
	mb := &fakeMembershipResolver{
		workspaceID: wsID,
		userID:      userID,
		membership:  &dto.Membership{WorkspaceID: wsID, UserID: userID, Role: value.RoleAdmin},
	}
	router := newRoleRouter(auth, ws, mb, value.RoleAdmin)

	rec := doWorkspaceRequest(t, router, "acme", sessionID.String())

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200 (equal role should pass), body = %s", rec.Code, rec.Body.String())
	}
}

// containsAny reports whether s contains any of the given non-empty substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub == "" {
			continue
		}
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
