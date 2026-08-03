package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// =====================================================================
// user handler tests (platform-admin password reset)
// =====================================================================

func TestPostAdminPasswordResetByPlatformAdmin(t *testing.T) {
	deps, auth, users, _, _ := newAuthTestDeps()
	router := NewRouter(deps)
	userID := uuid.New()
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: userID, IsPlatformAdmin: true}
	auth.sessionID = sessionID
	targetUserID := uuid.New()

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"new_password":"brand-new-pw"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/admin/users/"+targetUserID.String()+"/password-reset", body)
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK && rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !users.resetCalled {
		t.Fatal("ResetPassword not called")
	}
	if users.resetTarget != targetUserID {
		t.Fatalf("target = %s, want %s", users.resetTarget, targetUserID)
	}
	if !users.resetAdmin {
		t.Fatal("actorIsPlatformAdmin must be true (enforced by middleware)")
	}
	if users.resetActor != userID {
		t.Fatalf("actor = %s, want %s", users.resetActor, userID)
	}
	if users.resetPassword != "brand-new-pw" {
		t.Fatalf("password = %q", users.resetPassword)
	}
}

func TestPostAdminPasswordResetNonAdminReturns403(t *testing.T) {
	deps, auth, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)
	sessionID := uuid.New()
	auth.authUser = &model.User{ID: uuid.New(), IsPlatformAdmin: false}
	auth.sessionID = sessionID
	targetUserID := uuid.New()

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"new_password":"x"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/admin/users/"+targetUserID.String()+"/password-reset", body)
	req.Header.Set("content-type", "application/json")
	req.AddCookie(&stdhttp.Cookie{Name: testCookieName, Value: sessionID.String()})
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403 (platform admin only)", rec.Code)
	}
}

func TestPostAdminPasswordResetWithoutCookieReturns401(t *testing.T) {
	deps, _, _, _, _ := newAuthTestDeps()
	router := NewRouter(deps)
	targetUserID := uuid.New()

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"new_password":"x"}`)
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/admin/users/"+targetUserID.String()+"/password-reset", body)
	req.Header.Set("content-type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
