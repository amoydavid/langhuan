package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// =====================================================================
// slugFixtures: wires a router where the user is a member of ws "acme"
// with a given role. The SAME fake services are shared by middleware
// (resolver interfaces) and handlers (handler-side interfaces).
// =====================================================================

type slugFixtures struct {
	router    *gin.Engine
	wsID      uuid.UUID
	userID    uuid.UUID
	sessionID uuid.UUID
	auth      *fakeAuthService
	users     *fakeUserService
	mbs       *fakeMembershipService
	invs      *fakeInvitationService
	wsSvc     *fakeWorkspaceService
}

// newSlugFixtures configures a router where the user is a member of the "acme"
// workspace with the given role. The user's membership is pre-seeded so
// RequireWorkspace resolves slug=acme -> wsID and the role.
func newSlugFixtures(role value.WorkspaceRole, isPlatformAdmin bool) *slugFixtures {
	return newSlugFixturesWithPublicURLs(role, isPlatformAdmin, "")
}

func newSlugFixturesWithPublicURLs(role value.WorkspaceRole, isPlatformAdmin bool, publicURLs string) *slugFixtures {
	deps, auth, users, mbs, invs := newAuthTestDeps()
	if publicURLs != "" {
		builder, err := service.NewPublicURLBuilder(publicURLs)
		if err != nil {
			panic(fmt.Errorf("newSlugFixturesWithPublicURLs: %w", err))
		}
		deps.PublicURLs = builder
	}

	wsID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()

	// SessionAuth: any cookie value resolves to this user (fake matches by session id
	// only when authUser is set, so we configure authUser and the authenticator returns it
	// for the supplied session id).
	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: isPlatformAdmin}
	auth.sessionID = sessionID

	// The membership the middleware should resolve for (wsID, userID).
	mbs.getResult = &dto.Membership{ID: uuid.New(), WorkspaceID: wsID, UserID: userID, Role: role}

	// Workspace fake: GetBySlug resolves "acme" -> wsID, Get resolves wsID -> the workspace.
	wsSvc := &fakeWorkspaceService{items: map[uuid.UUID]*dto.Workspace{
		wsID: {ID: wsID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}},
	}}
	wsSvc.slugIndex = map[string]*dto.Workspace{"acme": {ID: wsID, Name: "Acme", Slug: "acme", Metadata: map[string]any{}}}

	deps.Workspaces = wsSvc

	return &slugFixtures{
		router:    NewRouter(deps),
		wsID:      wsID,
		userID:    userID,
		sessionID: sessionID,
		auth:      auth,
		users:     users,
		mbs:       mbs,
		invs:      invs,
		wsSvc:     wsSvc,
	}
}

// authedSlugRequest issues an authenticated request to a workspace-scoped slug route.
func (f *slugFixtures) authedRequest(method, path string, body string) *httptest.ResponseRecorder {
	var reader = strings.NewReader("")
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// =====================================================================
// getPublic invitation tests
// =====================================================================

func TestGetInvitationPublicReturnsNoHash(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/invitations/some-token", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "token_hash") || strings.Contains(raw, "some-token") {
		t.Fatalf("public invitation must not leak token/hash: %s", raw)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["workspace_name"] == nil || body["workspace_slug"] == nil {
		t.Fatalf("public invitation missing workspace info: %#v", body)
	}
	if body["role"] != string(value.RoleMember) {
		t.Fatalf("role = %v", body["role"])
	}
}

func TestGetInvitationPublicNotFoundReturns404(t *testing.T) {
	deps, _, _, _, invs := newAuthTestDeps()
	invs.publicErr = domainerrors.ErrNotFound
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/invitations/bogus", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInvitationPublicRouteRequiresNoCookie(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/invitations/abc", nil)
	router.ServeHTTP(rec, req)
	if rec.Code == stdhttp.StatusUnauthorized {
		t.Fatalf("public invitation route must not require auth, got %d", rec.Code)
	}
}

// =====================================================================
// create invitation tests
// =====================================================================

func TestInvitationListAdminAndOwnerReturnsItems(t *testing.T) {
	for _, role := range []value.WorkspaceRole{value.RoleAdmin, value.RoleOwner} {
		t.Run(string(role), func(t *testing.T) {
			f := newSlugFixtures(role, false)
			f.invs.listResult = []*dto.InvitationListItem{{
				ID:           uuid.New(),
				WorkspaceID:  f.wsID,
				InvitedEmail: "invitee@example.com",
				Role:         value.RoleMember,
				TokenPrefix:  "prefix12",
				Status:       dto.InvitationStatusPending,
			}}

			rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/invitations", "")
			if rec.Code != stdhttp.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var got []*dto.InvitationListItem
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Status != dto.InvitationStatusPending {
				t.Fatalf("invitations = %#v", got)
			}
			if f.invs.listInput.workspaceID != f.wsID || f.invs.listInput.actorRole != role {
				t.Fatalf("List input = %#v", f.invs.listInput)
			}
			if strings.Contains(rec.Body.String(), "token_hash") || strings.Contains(rec.Body.String(), "secret-token") {
				t.Fatalf("list leaked token hash: %s", rec.Body.String())
			}
		})
	}
}

func TestInvitationListEmptyReturnsArray(t *testing.T) {
	f := newSlugFixtures(value.RoleAdmin, false)
	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/invitations", "")
	if rec.Code != stdhttp.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestInvitationListMemberReturns403(t *testing.T) {
	f := newSlugFixtures(value.RoleMember, false)
	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/invitations", "")
	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
}

func TestInvitationListWithoutCookieReturns401(t *testing.T) {
	f := newSlugFixtures(value.RoleAdmin, false)
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/invitations", nil)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

func TestInvitationCreateAdminReturnsInviteURLWithPlaintextToken(t *testing.T) {
	f := newSlugFixtures(value.RoleAdmin, false)

	rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/invitations", `{"invited_email":"x@y.com","role":"member"}`)

	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] == nil {
		t.Fatal("id missing")
	}
	if body["invited_email"] != "x@y.com" {
		t.Fatalf("invited_email = %v", body["invited_email"])
	}
	if body["role"] != "member" {
		t.Fatalf("role = %v", body["role"])
	}
	if body["expires_at"] == nil {
		t.Fatal("expires_at missing")
	}
	if body["token_prefix"] == nil {
		t.Fatal("token_prefix missing")
	}
	inviteURL, _ := body["invite_url"].(string)
	if inviteURL == "" {
		t.Fatal("invite_url missing")
	}
	if !strings.HasPrefix(inviteURL, "http://") {
		t.Fatalf("plain HTTP request must produce an http invite_url: %s", inviteURL)
	}
	if !strings.Contains(inviteURL, "example.com") {
		t.Fatalf("invite_url must use request host: %s", inviteURL)
	}
	if !strings.HasSuffix(inviteURL, "/invitations/"+f.invs.createToken) {
		t.Fatalf("invite_url must end with /invitations/<plaintext token>: %s", inviteURL)
	}
	// The plaintext token must ONLY appear in invite_url (nowhere else in the body).
	rawBody := rec.Body.String()
	plainCount := strings.Count(rawBody, f.invs.createToken)
	if plainCount != 1 {
		t.Fatalf("plaintext token must appear exactly once (in invite_url); found %d", plainCount)
	}
	// Service receives the actor context.
	if f.invs.createInput.CreatedBy != f.userID {
		t.Fatalf("CreatedBy = %s, want %s", f.invs.createInput.CreatedBy, f.userID)
	}
	if f.invs.createInput.WorkspaceID != f.wsID {
		t.Fatalf("WorkspaceID = %s, want %s", f.invs.createInput.WorkspaceID, f.wsID)
	}
	if f.invs.createInput.ActorRole != value.RoleAdmin {
		t.Fatalf("ActorRole = %v, want %s", f.invs.createInput.ActorRole, value.RoleAdmin)
	}
}

func TestInvitationURLUsesConfiguredPublicURLs(t *testing.T) {
	f := newSlugFixturesWithPublicURLs(value.RoleAdmin, false, "https://public.example.com/console")
	rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/invitations", `{"invited_email":"x@y.com","role":"member"}`)

	var body createInvitationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := "https://public.example.com/console/invitations/" + f.invs.createToken
	if body.InviteURL != want {
		t.Fatalf("invite_url = %q, want %q", body.InviteURL, want)
	}
}

func TestInvitationURLUsesTLSRequestScheme(t *testing.T) {
	f := newSlugFixtures(value.RoleAdmin, false)
	req := httptest.NewRequest(stdhttp.MethodPost, "https://secure.example.com/api/v1/workspaces/acme/invitations", strings.NewReader(`{"invited_email":"x@y.com","role":"member"}`))
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	var body createInvitationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.InviteURL, "https://secure.example.com/invitations/") {
		t.Fatalf("invite_url = %q", body.InviteURL)
	}
}

func TestInvitationURLIgnoresUntrustedForwardedHeaders(t *testing.T) {
	f := newSlugFixtures(value.RoleAdmin, false)
	req := httptest.NewRequest(stdhttp.MethodPost, "http://internal.example.com/api/v1/workspaces/acme/invitations", strings.NewReader(`{"invited_email":"x@y.com","role":"member"}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example.com")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: f.sessionID.String()})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	var body createInvitationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.InviteURL, "http://internal.example.com/invitations/") {
		t.Fatalf("invite_url = %q", body.InviteURL)
	}
}

func TestInvitationCreateMemberReturns403(t *testing.T) {
	f := newSlugFixtures(value.RoleMember, false)

	rec := f.authedRequest(stdhttp.MethodPost, "/api/v1/workspaces/acme/invitations", `{"invited_email":"x@y.com","role":"member"}`)

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rec.Code, rec.Body.String())
	}
}

func TestInvitationRevokeAdminCreatorRevokesOwn(t *testing.T) {
	f := newSlugFixtures(value.RoleAdmin, false)
	invitationID := uuid.New()

	rec := f.authedRequest(stdhttp.MethodDelete, "/api/v1/workspaces/acme/invitations/"+invitationID.String(), "")

	if rec.Code != stdhttp.StatusNoContent && rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !f.invs.revokeCalled {
		t.Fatal("Revoke not called")
	}
	if f.invs.revokeInput.InvitationID != invitationID {
		t.Fatalf("invitation id = %s, want %s", f.invs.revokeInput.InvitationID, invitationID)
	}
	if f.invs.revokeInput.ActorUserID != f.userID {
		t.Fatalf("actor = %s, want %s", f.invs.revokeInput.ActorUserID, f.userID)
	}
	if f.invs.revokeInput.ActorRole != value.RoleAdmin {
		t.Fatalf("actor role = %v", f.invs.revokeInput.ActorRole)
	}
	if f.invs.revokeInput.IsPlatformAdmin {
		t.Fatal("should not be platform admin")
	}
}

func TestInvitationRevokeAnyByPlatformAdmin(t *testing.T) {
	deps, auth, _, _, invs := newAuthTestDeps()
	router := NewRouter(deps)
	invitationID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: true}
	auth.sessionID = sessionID

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodDelete, "/api/v1/invitations/"+invitationID.String(), nil)
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusNoContent && rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !invs.revokeCalled {
		t.Fatal("Revoke not called")
	}
	if !invs.revokeInput.IsPlatformAdmin {
		t.Fatal("platform admin revoke must pass isPlatformAdmin=true")
	}
}

var _ = dto.Workspace{}
