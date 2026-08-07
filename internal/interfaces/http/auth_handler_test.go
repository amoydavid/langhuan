package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// =====================================================================
// Handler-side service fakes (also satisfy middleware DI interfaces).
// =====================================================================

// fakeAuthService implements AuthService (handler-side) for tests.
// It ALSO satisfies SessionAuthenticator (middleware) via Authenticate.
type fakeAuthService struct {
	loginSession *model.Session
	loginErr     error

	logoutErr    error
	logoutCalled bool
	logoutID     uuid.UUID

	authUser *model.User
	authErr  error
	// sessionID, when non-zero, is the only cookie value Authenticate accepts.
	// When zero, any cookie value is accepted (convenient for /me tests).
	sessionID uuid.UUID
}

func (s *fakeAuthService) Login(_ context.Context, email, password, userAgent, ipAddr string) (*model.Session, error) {
	if s.loginErr != nil {
		return nil, s.loginErr
	}
	if s.loginSession == nil {
		s.loginSession = &model.Session{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}
	}
	s.loginSession.UserAgent = userAgent
	s.loginSession.IPAddr = ipAddr
	_ = email
	_ = password
	return s.loginSession, nil
}

func (s *fakeAuthService) Logout(_ context.Context, sessionID uuid.UUID) error {
	s.logoutCalled = true
	s.logoutID = sessionID
	return s.logoutErr
}

func (s *fakeAuthService) Authenticate(_ context.Context, sessionID uuid.UUID) (*model.User, error) {
	if s.authErr != nil {
		return nil, s.authErr
	}
	if s.sessionID != uuid.Nil && sessionID != s.sessionID {
		return nil, domainerrors.ErrUnauthorized
	}
	if s.authUser == nil {
		return &model.User{ID: uuid.New()}, nil
	}
	return s.authUser, nil
}

// fakeUserService implements UserService (handler-side) for tests.
type fakeUserService struct {
	initialized    bool
	initializedErr error

	registeredUser *dto.AuthenticatedUser
	registerErr    error
	registerCalled bool
	registerEmail  string
	registerNick   string
	registerPass   string
	registerCount  int

	resetErr      error
	resetCalled   bool
	resetTarget   uuid.UUID
	resetActor    uuid.UUID
	resetAdmin    bool
	resetPassword string

	changePasswordCalled bool
	changeUserID         uuid.UUID
	changeOldPassword    string
	changeNewPassword    string
	changePasswordErr    error

	byIDUser *dto.AuthenticatedUser
	byIDErr  error
}

func (s *fakeUserService) IsInitialized(_ context.Context) (bool, error) {
	return s.initialized, s.initializedErr
}

func (s *fakeUserService) RegisterFirstUser(_ context.Context, email, nickname, password string) (*dto.AuthenticatedUser, error) {
	s.registerCalled = true
	s.registerEmail = email
	s.registerNick = nickname
	s.registerPass = password
	s.registerCount++
	if s.registerErr != nil {
		return nil, s.registerErr
	}
	if s.registeredUser == nil {
		s.registeredUser = &dto.AuthenticatedUser{ID: uuid.New(), Email: email, Nickname: nickname, IsPlatformAdmin: true}
	}
	return s.registeredUser, nil
}

func (s *fakeUserService) ResetPassword(_ context.Context, actorUserID uuid.UUID, actorIsPlatformAdmin bool, targetUserID uuid.UUID, newPassword string) error {
	s.resetCalled = true
	s.resetActor = actorUserID
	s.resetAdmin = actorIsPlatformAdmin
	s.resetTarget = targetUserID
	s.resetPassword = newPassword
	return s.resetErr
}

func (s *fakeUserService) ChangePassword(_ context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	s.changePasswordCalled = true
	s.changeUserID = userID
	s.changeOldPassword = oldPassword
	s.changeNewPassword = newPassword
	return s.changePasswordErr
}

func (s *fakeUserService) GetByID(_ context.Context, userID uuid.UUID) (*dto.AuthenticatedUser, error) {
	if s.byIDErr != nil {
		return nil, s.byIDErr
	}
	if s.byIDUser == nil {
		return &dto.AuthenticatedUser{ID: userID, Email: "me@example.com", Nickname: "me", IsPlatformAdmin: false}, nil
	}
	return s.byIDUser, nil
}

// fakeInvitationService implements InvitationService (handler-side) for tests.
type fakeInvitationService struct {
	listResult []*dto.InvitationListItem
	listErr    error
	listInput  struct {
		workspaceID uuid.UUID
		actorRole   value.WorkspaceRole
	}

	createInv   *dto.Invitation
	createToken string
	createErr   error
	createInput service.CreateInvitationInput

	publicInv *dto.PublicInvitation
	publicErr error

	acceptSession *model.Session
	acceptErr     error
	acceptCalled  bool

	revokeErr    error
	revokeCalled bool
	revokeInput  revokeArgs
}

func (s *fakeInvitationService) List(_ context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole) ([]*dto.InvitationListItem, error) {
	s.listInput.workspaceID = workspaceID
	s.listInput.actorRole = actorRole
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.listResult == nil {
		return []*dto.InvitationListItem{}, nil
	}
	return s.listResult, nil
}

// revokeArgs is a test-facing mirror of the arguments to Revoke.
type revokeArgs struct {
	InvitationID    uuid.UUID
	ActorUserID     uuid.UUID
	ActorRole       value.WorkspaceRole
	IsPlatformAdmin bool
}

func (s *fakeInvitationService) Create(_ context.Context, input service.CreateInvitationInput) (*dto.Invitation, string, error) {
	s.createInput = input
	if s.createErr != nil {
		return nil, "", s.createErr
	}
	if s.createInv == nil {
		s.createInv = &dto.Invitation{
			ID:           uuid.New(),
			WorkspaceID:  input.WorkspaceID,
			InvitedEmail: input.InvitedEmail,
			Role:         input.Role,
			TokenPrefix:  "prefix12",
			ExpiresAt:    time.Now().Add(24 * time.Hour),
			CreatedAt:    time.Now(),
		}
	}
	if s.createToken == "" {
		s.createToken = "plaintext-token-value"
	}
	return s.createInv, s.createToken, nil
}

func (s *fakeInvitationService) GetPublic(_ context.Context, plaintextToken string) (*dto.PublicInvitation, error) {
	_ = plaintextToken
	if s.publicErr != nil {
		return nil, s.publicErr
	}
	if s.publicInv == nil {
		s.publicInv = &dto.PublicInvitation{
			WorkspaceID:   uuid.New(),
			WorkspaceName: "Acme",
			WorkspaceSlug: "acme",
			InvitedEmail:  "invitee@example.com",
			Role:          value.RoleMember,
			ExpiresAt:     time.Now().Add(24 * time.Hour),
		}
	}
	return s.publicInv, nil
}

func (s *fakeInvitationService) Accept(_ context.Context, plaintextToken, email, nickname, password, userAgent, ipAddr string) (*model.Session, error) {
	s.acceptCalled = true
	_ = plaintextToken
	_ = email
	_ = nickname
	_ = password
	_ = userAgent
	_ = ipAddr
	if s.acceptErr != nil {
		return nil, s.acceptErr
	}
	if s.acceptSession == nil {
		s.acceptSession = &model.Session{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}
	}
	return s.acceptSession, nil
}

func (s *fakeInvitationService) Revoke(_ context.Context, invitationID, actorUserID uuid.UUID, actorRole value.WorkspaceRole, isPlatformAdmin bool) error {
	s.revokeCalled = true
	s.revokeInput = revokeArgs{invitationID, actorUserID, actorRole, isPlatformAdmin}
	return s.revokeErr
}

// fakeMembershipService implements MembershipService (handler-side) AND
// MembershipResolver (middleware) for tests.
type fakeMembershipService struct {
	listResult []*dto.Membership
	listErr    error

	getResult *dto.Membership
	getErr    error

	changeResult *dto.Membership
	changeErr    error
	changeInput  changeRoleArgs

	removeErr    error
	removeCalled bool
	removeInput  removeArgs

	forUserResult []*dto.Membership
	forUserErr    error
}

type changeRoleArgs struct {
	WorkspaceID  uuid.UUID
	TargetUserID uuid.UUID
	NewRole      value.WorkspaceRole
	ActorRole    value.WorkspaceRole
}

type removeArgs struct {
	WorkspaceID  uuid.UUID
	TargetUserID uuid.UUID
	ActorRole    value.WorkspaceRole
}

func (s *fakeMembershipService) List(_ context.Context, workspaceID uuid.UUID) ([]*dto.Membership, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listResult, nil
}

func (s *fakeMembershipService) Get(_ context.Context, workspaceID, userID uuid.UUID) (*dto.Membership, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.getResult == nil {
		return nil, domainerrors.ErrNotFound
	}
	if s.getResult.WorkspaceID != workspaceID || s.getResult.UserID != userID {
		return nil, domainerrors.ErrNotFound
	}
	return s.getResult, nil
}

func (s *fakeMembershipService) ChangeRole(_ context.Context, workspaceID, targetUserID uuid.UUID, newRole value.WorkspaceRole, actorRole value.WorkspaceRole) (*dto.Membership, error) {
	s.changeInput = changeRoleArgs{workspaceID, targetUserID, newRole, actorRole}
	if s.changeErr != nil {
		return nil, s.changeErr
	}
	if s.changeResult == nil {
		s.changeResult = &dto.Membership{ID: uuid.New(), WorkspaceID: workspaceID, UserID: targetUserID, Role: newRole}
	}
	return s.changeResult, nil
}

func (s *fakeMembershipService) Remove(_ context.Context, workspaceID, targetUserID uuid.UUID, actorRole value.WorkspaceRole) error {
	s.removeCalled = true
	s.removeInput = removeArgs{workspaceID, targetUserID, actorRole}
	return s.removeErr
}

func (s *fakeMembershipService) ListForUser(_ context.Context, userID uuid.UUID) ([]*dto.Membership, error) {
	if s.forUserErr != nil {
		return nil, s.forUserErr
	}
	return s.forUserResult, nil
}

// =====================================================================
// Shared test setup helpers.
// =====================================================================

func newAuthTestDeps() (Dependencies, *fakeAuthService, *fakeUserService, *fakeMembershipService, *fakeInvitationService) {
	auth := &fakeAuthService{}
	users := &fakeUserService{}
	mbs := &fakeMembershipService{}
	invs := &fakeInvitationService{}
	deps := Dependencies{
		Auth:        auth,
		Users:       users,
		Memberships: mbs,
		Invitations: invs,
		SessionConfig: config.SessionConfig{
			CookieName:      testCookieName,
			LifetimeSeconds: 3600,
			SecureCookie:    true,
			Domain:          "app.test",
		},
	}
	return deps, auth, users, mbs, invs
}

func TestBootstrapStatusIsPublicAndReturnsOnlyInitialized(t *testing.T) {
	tests := []struct {
		name        string
		initialized bool
	}{
		{name: "not initialized", initialized: false},
		{name: "initialized", initialized: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, _, users, _, _ := newAuthTestDeps()
			users.initialized = tt.initialized
			router := NewRouter(deps)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/auth/bootstrap-status", nil))

			if rec.Code != stdhttp.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["initialized"] != tt.initialized {
				t.Fatalf("initialized = %v, want %v", body["initialized"], tt.initialized)
			}
			// 新增的 auth mode 字段（OIDC 默认关闭、password 默认开启）。
			if _, ok := body["oidc_enabled"]; !ok {
				t.Fatalf("body should include oidc_enabled: %#v", body)
			}
			if _, ok := body["password_enabled"]; !ok {
				t.Fatalf("body should include password_enabled: %#v", body)
			}
		})
	}
}

func TestBootstrapStatusMapsCountError(t *testing.T) {
	deps, _, users, _, _ := newAuthTestDeps()
	users.initializedErr = errors.New("count failed")
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/auth/bootstrap-status", nil))

	if rec.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "count failed") {
		t.Fatalf("response leaked internal error: %s", rec.Body.String())
	}
}

// sessionCookieHeader returns the Set-Cookie header string for the named cookie,
// or "" if not set.
func sessionCookieHeader(t *testing.T, rec *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.String()
		}
	}
	return ""
}

// =====================================================================
// auth handler tests
// =====================================================================

func TestPostAuthLoginSetsSecureCookie(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)
	userID := uuid.New()
	auth.loginSession = &model.Session{ID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(3600 * time.Second)}

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"email":"a@b.com","password":"secret"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req.RemoteAddr = "1.2.3.4:5678"
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookieHeader := sessionCookieHeader(t, rec, testCookieName)
	if cookieHeader == "" {
		t.Fatal("expected a Set-Cookie header for the session cookie")
	}
	if !strings.Contains(cookieHeader, "HttpOnly") {
		t.Fatalf("cookie must be HttpOnly: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, "Secure") {
		t.Fatalf("cookie must be Secure: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, "SameSite=Lax") {
		t.Fatalf("cookie must be SameSite=Lax: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, "Domain=app.test") {
		t.Fatalf("cookie must set Domain: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, fmt.Sprintf("Max-Age=%d", deps.SessionConfig.LifetimeSeconds)) {
		t.Fatalf("cookie must set Max-Age to lifetime: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, auth.loginSession.ID.String()) {
		t.Fatalf("cookie value should contain the session id: %s", cookieHeader)
	}
	// IP/UA must be derived and passed through to the service.
	if auth.loginSession.IPAddr != "1.2.3.4" {
		t.Fatalf("ip = %q, want 1.2.3.4 (host part of RemoteAddr)", auth.loginSession.IPAddr)
	}
	if auth.loginSession.UserAgent != "TestAgent/1.0" {
		t.Fatalf("user agent = %q, want TestAgent/1.0", auth.loginSession.UserAgent)
	}
}

func TestPostAuthLoginWrongCredentialsReturns401(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	auth.loginErr = domainerrors.ErrUnauthorized
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"email":"a@b.com","password":"wrong"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if sessionCookieHeader(t, rec, testCookieName) != "" {
		t.Fatal("login failure must NOT set a session cookie")
	}
}

func TestPostAuthLoginRateLimitedReturns429(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	auth.loginErr = domainerrors.ErrRateLimited
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"email":"a@b.com","password":"wrong"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPostAuthLoginInvalidJSONReturns400(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(`{bad`))
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPostAuthLogoutClearsCookieAndReturns204(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: uuid.New()}
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}
	if !auth.logoutCalled || auth.logoutID != sessionID {
		t.Fatalf("logout not called with session id: called=%v id=%v", auth.logoutCalled, auth.logoutID)
	}
	cookieHeader := sessionCookieHeader(t, rec, testCookieName)
	if cookieHeader == "" {
		t.Fatal("logout must clear the session cookie")
	}
	if !strings.Contains(cookieHeader, "Max-Age=0") && !strings.Contains(cookieHeader, "Max-Age=-1") {
		t.Fatalf("logout cookie must clear (Max-Age<=0): %s", cookieHeader)
	}
}

func TestPostAuthLogoutWithoutCookieReturns401(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/logout", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGetAuthMeReturnsUserAndWorkspaceSummaries(t *testing.T) {
	deps, auth, users, mbs, _ := newAuthTestDeps()
	userID := uuid.New()
	wsID := uuid.New()
	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: false}
	users.byIDUser = &dto.AuthenticatedUser{ID: userID, Email: "me@example.com", Nickname: "me", IsPlatformAdmin: false}
	mbs.forUserResult = []*dto.Membership{
		{ID: uuid.New(), WorkspaceID: wsID, UserID: userID, Role: value.RoleAdmin},
	}
	// Wire Workspaces so the handler can fetch slug/name per membership.
	wsSvc := &fakeWorkspaceService{items: map[uuid.UUID]*dto.Workspace{}}
	wsSvc.items[wsID] = &dto.Workspace{ID: wsID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}}
	deps.Workspaces = wsSvc

	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: uuid.NewString()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		User       *dto.AuthenticatedUser `json:"user"`
		Workspaces []map[string]any       `json:"workspaces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.User == nil || resp.User.ID != userID {
		t.Fatalf("user = %#v", resp.User)
	}
	if len(resp.Workspaces) != 1 {
		t.Fatalf("workspaces = %#v, want 1", resp.Workspaces)
	}
	ws := resp.Workspaces[0]
	if ws["workspace_id"] != wsID.String() {
		t.Fatalf("workspace_id = %v, want %s", ws["workspace_id"], wsID)
	}
	if ws["slug"] != "acme" {
		t.Fatalf("slug = %v, want acme", ws["slug"])
	}
	if ws["name"] != "Acme" {
		t.Fatalf("name = %v, want Acme", ws["name"])
	}
	if ws["role"] != string(value.RoleAdmin) {
		t.Fatalf("role = %v, want %s", ws["role"], value.RoleAdmin)
	}
}

func TestPostAuthRegisterFirstUserNoCookie(t *testing.T) {
	deps, _, users, _, _ := newAuthTestDeps()
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"email":"first@example.com","nickname":"first","password":"secret"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !users.registerCalled {
		t.Fatal("RegisterFirstUser should be called for first-user registration")
	}
	if sessionCookieHeader(t, rec, testCookieName) != "" {
		t.Fatal("first-user registration must not set a session cookie")
	}
	var got dto.AuthenticatedUser
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.IsPlatformAdmin {
		t.Fatal("first user must be platform admin")
	}
}

func TestPostAuthRegisterWithInvitationTokenSetsCookie(t *testing.T) {
	deps, _, _, _, invs := newAuthTestDeps()
	router := NewRouter(deps)
	invs.acceptSession = &model.Session{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)}

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"email":"x@y.com","nickname":"nick","password":"pw","invitation_token":"tok"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/register", body)
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !invs.acceptCalled {
		t.Fatal("Invitations.Accept should be called when invitation_token present")
	}
	cookieHeader := sessionCookieHeader(t, rec, testCookieName)
	if cookieHeader == "" {
		t.Fatal("invitation accept must set a session cookie")
	}
	if !strings.Contains(cookieHeader, "HttpOnly") {
		t.Fatalf("cookie must be HttpOnly: %s", cookieHeader)
	}
}

// keep imports referenced
var _ = fmt.Sprintf

func TestChangePasswordEndpoint(t *testing.T) {
	deps, auth, users, _, _ := newAuthTestDeps()
	userID := uuid.New()
	auth.authUser = &model.User{ID: userID}
	router := NewRouter(deps)

	body := strings.NewReader(`{"old_password":"old","new_password":"new"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/change-password", body)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: uuid.New().String()})
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !users.changePasswordCalled {
		t.Fatal("ChangePassword was not called")
	}
	if users.changeUserID != userID {
		t.Fatalf("changeUserID = %v, want %v", users.changeUserID, userID)
	}
	if users.changeOldPassword != "old" || users.changeNewPassword != "new" {
		t.Fatalf("passwords = old=%q new=%q", users.changeOldPassword, users.changeNewPassword)
	}
}
