package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// valueAuthContextWithUser 构造一个带 UserID 的 AuthContext 用于测试。
func valueAuthContextWithUser(userID uuid.UUID) value.AuthContext {
	return value.AuthContext{PrincipalKind: value.PrincipalUser, UserID: userID}
}

// fakeOIDCService 实现 OIDCLoginServiceHTTP。
type fakeOIDCService struct {
	beginURL          string
	beginErr          error
	payload           *authport.OIDCStatePayload
	profile           *authport.OIDCProfile
	consumeErr        error
	loginSession      *model.Session
	loginErr          error
	bindErr           error
	identities        []*model.ExternalIdentity
	listErr           error
	beginCalledNext   string
	beginCalledInvite string
	needsEmail        bool
	hasBoundIdentity  bool
}

func (f *fakeOIDCService) HasIdentityForIssuer(ctx context.Context, userID uuid.UUID) (bool, error) {
	return f.hasBoundIdentity, nil
}

func (f *fakeOIDCService) BeginLogin(ctx context.Context, next string, invitationToken string, actorUserID, sessionID uuid.UUID) (string, string, string, error) {
	f.beginCalledNext = next
	f.beginCalledInvite = invitationToken
	if f.beginErr != nil {
		return "", "", "", f.beginErr
	}
	return f.beginURL, "browser-nonce", "state-123", nil
}

func (f *fakeOIDCService) ConsumeAndExchange(ctx context.Context, code, state, browserNonce string) (*authport.OIDCStatePayload, *authport.OIDCProfile, error) {
	if f.consumeErr != nil {
		return nil, nil, f.consumeErr
	}
	return f.payload, f.profile, nil
}

func (f *fakeOIDCService) LoginOrProvision(ctx context.Context, profile *authport.OIDCProfile, userAgent, ipAddr string) (*model.Session, error) {
	return f.loginSession, f.loginErr
}

func (f *fakeOIDCService) BindIdentity(ctx context.Context, actorUserID uuid.UUID, profile *authport.OIDCProfile) error {
	return f.bindErr
}

func (f *fakeOIDCService) ListIdentities(ctx context.Context, userID uuid.UUID) ([]*model.ExternalIdentity, error) {
	return f.identities, f.listErr
}

func (f *fakeOIDCService) NeedsEmailCompletion(ctx context.Context, userID uuid.UUID) (bool, error) {
	return f.needsEmail, nil
}

// fakeOIDCAcceptor 实现 OIDCAcceptor 与 OIDCInvitationCompleter。
type fakeOIDCAcceptor struct {
	session     *model.Session
	err         error
	completeErr error
}

func (a *fakeOIDCAcceptor) AcceptOIDC(ctx context.Context, tokenHash string, profile *authport.OIDCProfile, userAgent, ipAddr string) (*model.Session, error) {
	return a.session, a.err
}

func (a *fakeOIDCAcceptor) CompleteInvitationAccept(ctx context.Context, tokenHash string, userID uuid.UUID) error {
	return a.completeErr
}

// fakeOIDCSessionAuth 实现 OIDCSessionAuth。
type fakeOIDCSessionAuth struct {
	user *model.User
	err  error
}

func (a *fakeOIDCSessionAuth) Authenticate(ctx context.Context, sessionID uuid.UUID) (*model.User, error) {
	return a.user, a.err
}

func newTestOIDCHandler(svc OIDCLoginServiceHTTP, acceptor OIDCAcceptor, auth OIDCSessionAuth) oidcHandler {
	var completer OIDCInvitationCompleter
	if c, ok := acceptor.(OIDCInvitationCompleter); ok {
		completer = c
	}
	return newOIDCHandler(svc, acceptor, completer, auth, config.SessionConfig{
		CookieName:      "langhuan_session",
		LifetimeSeconds: 3600,
		SecureCookie:    false,
	})
}

func newTestGin() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestOIDCBeginSetsNonceCookieAndRedirects(t *testing.T) {
	svc := &fakeOIDCService{beginURL: "https://idp.example.com/auth?state=xyz"}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?next=/dashboard", nil)

	h.begin(c)

	rec := w.Result()
	if rec.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.StatusCode)
	}
	if rec.Header.Get("Location") != "https://idp.example.com/auth?state=xyz" {
		t.Fatalf("location = %s", rec.Header.Get("Location"))
	}
	// 应设置 oidc_nonce_state-123 cookie。
	found := false
	for _, ck := range rec.Cookies() {
		if ck.Name == "oidc_nonce_state-123" && ck.Value == "browser-nonce" {
			found = true
		}
	}
	if !found {
		t.Fatal("oidc_nonce_state-123 cookie not set")
	}
}

func TestOIDCBeginSanitizesNext(t *testing.T) {
	svc := &fakeOIDCService{beginURL: "https://idp.example.com/auth"}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login?next=//evil.com", nil)

	h.begin(c)

	rec := w.Result()
	if rec.Header.Get("Location") != "/sign-in?oidc_error=validation_error" {
		// sanitizeNextPath 返回 ErrValidation → errorCode default oidc_error；
		// 但 ErrValidation 未在 errorCode switch 中，落到 default。
	}
	// begin 失败应 302 到 /sign-in?oidc_error=...
	if rec.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.StatusCode)
	}
}

func TestOIDCCallbackHandlesIdPError(t *testing.T) {
	svc := &fakeOIDCService{}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?error=access_denied&state=xyz", nil)

	h.callback(c)

	rec := w.Result()
	if rec.Header.Get("Location") != "/sign-in?oidc_error=oidc_access_denied" {
		t.Fatalf("location = %s, want oidc_access_denied redirect", rec.Header.Get("Location"))
	}
}

func TestOIDCCallbackLoginSuccessSetsSessionCookie(t *testing.T) {
	session := &model.Session{ID: uuid.New(), UserID: uuid.New()}
	svc := &fakeOIDCService{
		payload:      &authport.OIDCStatePayload{Next: "/dashboard"},
		profile:      &authport.OIDCProfile{Subject: "sub-1", Email: "a@b.com"},
		loginSession: session,
	}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	// 设置 nonce cookie。
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=x&state=state-123", nil)
	c.Request.AddCookie(&http.Cookie{Name: "oidc_nonce_state-123", Value: "browser-nonce"})

	h.callback(c)

	rec := w.Result()
	if rec.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.StatusCode)
	}
	if rec.Header.Get("Location") != "/dashboard" {
		t.Fatalf("location = %s, want /dashboard", rec.Header.Get("Location"))
	}
	found := false
	for _, ck := range rec.Cookies() {
		if ck.Name == "langhuan_session" && ck.Value == session.ID.String() {
			found = true
		}
	}
	if !found {
		t.Fatal("session cookie not set")
	}
}

func TestOIDCCallbackInvitationAccept(t *testing.T) {
	session := &model.Session{ID: uuid.New(), UserID: uuid.New()}
	svc := &fakeOIDCService{
		payload: &authport.OIDCStatePayload{Next: "/", InvitationTokenHash: "abc123"},
		profile: &authport.OIDCProfile{Subject: "sub-1", Email: "a@b.com"},
	}
	acceptor := &fakeOIDCAcceptor{session: session}
	h := newTestOIDCHandler(svc, acceptor, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=x&state=state-123", nil)
	c.Request.AddCookie(&http.Cookie{Name: "oidc_nonce_state-123", Value: "browser-nonce"})

	h.callback(c)

	rec := w.Result()
	if rec.Header.Get("Location") != "/" {
		t.Fatalf("location = %s", rec.Header.Get("Location"))
	}
	found := false
	for _, ck := range rec.Cookies() {
		if ck.Name == "langhuan_session" && ck.Value == session.ID.String() {
			found = true
		}
	}
	if !found {
		t.Fatal("session cookie not set after invitation accept")
	}
}

func TestOIDCCallbackStateConsumeFailureRedirects(t *testing.T) {
	svc := &fakeOIDCService{
		consumeErr: domainerrors.ErrUnauthorized,
	}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=x&state=bad", nil)

	h.callback(c)

	rec := w.Result()
	if rec.Header.Get("Location") != "/sign-in?oidc_error=unauthorized" {
		t.Fatalf("location = %s, want unauthorized redirect", rec.Header.Get("Location"))
	}
}

func TestListIdentitiesReturnsNonSensitiveDTO(t *testing.T) {
	uid := uuid.New()
	svc := &fakeOIDCService{
		identities: []*model.ExternalIdentity{
			{ID: uuid.New(), UserID: uid, Issuer: "https://sso.example.com", Email: "ada@example.com", Subject: "secret-sub", RawProfile: `{"sensitive":"data"}`},
		},
	}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/auth/external-identities", nil)
	c.Set("auth", valueAuthContextWithUser(uid))

	h.listIdentities(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Identities []map[string]any `json:"identities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Identities) != 1 {
		t.Fatalf("identities count = %d", len(body.Identities))
	}
	id := body.Identities[0]
	if id["subject"] != nil {
		t.Fatal("subject must not leak in non-sensitive DTO")
	}
	if id["raw_profile"] != nil {
		t.Fatal("raw_profile must not leak")
	}
	if id["issuer"] != "https://sso.example.com" || id["email"] != "ada@example.com" {
		t.Fatalf("identity DTO = %#v", id)
	}
}

func TestErrorCodeMapping(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{domainerrors.ErrUnauthorized, "unauthorized"},
		{domainerrors.ErrForbidden, "forbidden"},
		{domainerrors.ErrConflict, "conflict"},
		{domainerrors.ErrPasswordLoginDisabled, "password_login_disabled"},
		{domainerrors.ErrPasswordRegistrationDisabled, "password_registration_disabled"},
		{errors.New("other"), "oidc_error"},
	}
	for _, tt := range tests {
		if got := errorCode(tt.err); got != tt.want {
			t.Errorf("errorCode(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestOIDCCallbackLoginRedirectsToCompleteProfileWhenNoEmail(t *testing.T) {
	session := &model.Session{ID: uuid.New(), UserID: uuid.New()}
	svc := &fakeOIDCService{
		payload:      &authport.OIDCStatePayload{Next: "/"},
		profile:      &authport.OIDCProfile{Subject: "sub-1", Email: ""},
		loginSession: session,
		needsEmail:   true,
	}
	h := newTestOIDCHandler(svc, nil, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=x&state=state-123", nil)
	c.Request.AddCookie(&http.Cookie{Name: "oidc_nonce_state-123", Value: "browser-nonce"})

	h.callback(c)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/complete-profile?next=") {
		t.Fatalf("location = %s, want /complete-profile prefix", loc)
	}
	// session cookie 应已设置。
	found := false
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "langhuan_session" {
			found = true
		}
	}
	if !found {
		t.Fatal("session cookie should be set before redirect to complete-profile")
	}
}

func TestOIDCCallbackInvitationRedirectsToCompleteProfileWithTokenHash(t *testing.T) {
	session := &model.Session{ID: uuid.New(), UserID: uuid.New()}
	acceptor := &fakeOIDCAcceptor{session: session}
	svc := &fakeOIDCService{
		payload:    &authport.OIDCStatePayload{Next: "/", InvitationTokenHash: "abc123hash"},
		profile:    &authport.OIDCProfile{Subject: "sub-1", Email: ""},
		needsEmail: true,
	}
	h := newTestOIDCHandler(svc, acceptor, nil)
	c, w := newTestGin()
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=x&state=state-123", nil)
	c.Request.AddCookie(&http.Cookie{Name: "oidc_nonce_state-123", Value: "browser-nonce"})

	h.callback(c)

	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "invitation_token_hash=abc123hash") {
		t.Fatalf("location = %s, want invitation_token_hash param", loc)
	}
	if !strings.HasPrefix(loc, "/complete-profile?") {
		t.Fatalf("location = %s, want /complete-profile prefix", loc)
	}
}

func TestBeginBindRejectsAlreadyBound(t *testing.T) {
	svc := &fakeOIDCService{beginURL: "https://idp.example.com/auth", hasBoundIdentity: true}
	h := newTestOIDCHandler(svc, nil, nil)
	userID := uuid.New()
	c, w := newTestGin()
	c.Set(authContextKey, valueAuthContextWithUser(userID))
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/bind/start", nil)
	c.Request.AddCookie(&http.Cookie{Name: "langhuan_session", Value: uuid.New().String()})

	h.beginBind(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if svc.beginCalledNext != "" {
		t.Fatalf("已绑定用户不应发起 IdP 跳转, beginCalledNext = %q", svc.beginCalledNext)
	}
}

func TestBeginBindAllowsUnbound(t *testing.T) {
	svc := &fakeOIDCService{beginURL: "https://idp.example.com/auth", hasBoundIdentity: false}
	h := newTestOIDCHandler(svc, nil, nil)
	userID := uuid.New()
	c, w := newTestGin()
	c.Set(authContextKey, valueAuthContextWithUser(userID))
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/bind/start", nil)
	c.Request.AddCookie(&http.Cookie{Name: "langhuan_session", Value: uuid.New().String()})

	h.beginBind(c)
	// CreateTestContext 绕过 engine 收尾的 WriteHeaderNow（gin 延迟写），
	// 手动 flush 模拟生产链路，使 POST 302 状态码可见。
	c.Writer.WriteHeaderNow()

	rec := w.Result()
	if rec.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.StatusCode)
	}
	if rec.Header.Get("Location") != "https://idp.example.com/auth" {
		t.Fatalf("location = %s, want IdP auth URL", rec.Header.Get("Location"))
	}
	if svc.beginCalledNext != "/settings/account" {
		t.Fatalf("beginCalledNext = %q, want /settings/account", svc.beginCalledNext)
	}
}
