package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

// =====================================================================
// membership handler tests
// =====================================================================

func TestMembershipListReturnsMemberships(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)
	f.mbs.listResult = []*dto.Membership{
		{
			ID: uuid.New(), WorkspaceID: f.wsID, UserID: uuid.New(), Role: value.RoleMember,
			User: &dto.MembershipUserSummary{Email: "member@example.com", Nickname: "成员"},
		},
		{ID: uuid.New(), WorkspaceID: f.wsID, UserID: uuid.New(), Role: value.RoleOwner},
	}

	rec := f.authedRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/members", nil, "")

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []*dto.Membership
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("members = %d, want 2", len(got))
	}
	if got[0].User == nil || got[0].User.Email != "member@example.com" || got[0].User.Nickname != "成员" {
		t.Fatalf("user summary = %#v", got[0].User)
	}
	if strings.Contains(rec.Body.String(), "password_hash") || strings.Contains(rec.Body.String(), "last_login") {
		t.Fatalf("membership response leaked sensitive fields: %s", rec.Body.String())
	}
}

func TestMembershipChangeRoleOwnerSucceeds(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleOwner, false)
	targetUserID := uuid.New()

	rec := f.authedRequest(stdhttp.MethodPatch, "/api/v1/workspaces/acme/members/"+targetUserID.String(), []byte(`{"role":"admin"}`), "application/json")

	if rec.Code != stdhttp.StatusOK && rec.Code != stdhttp.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.mbs.changeInput.TargetUserID != targetUserID {
		t.Fatalf("target = %s, want %s", f.mbs.changeInput.TargetUserID, targetUserID)
	}
	if f.mbs.changeInput.WorkspaceID != f.wsID {
		t.Fatalf("workspace_id = %s, want %s (from AuthContext)", f.mbs.changeInput.WorkspaceID, f.wsID)
	}
	if f.mbs.changeInput.NewRole != value.RoleAdmin {
		t.Fatalf("new role = %v, want %s", f.mbs.changeInput.NewRole, value.RoleAdmin)
	}
	if f.mbs.changeInput.ActorRole != value.RoleOwner {
		t.Fatalf("actor role = %v, want %s", f.mbs.changeInput.ActorRole, value.RoleOwner)
	}
}

func TestMembershipChangeRoleNonOwnerReturns403(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleAdmin, false)
	targetUserID := uuid.New()

	rec := f.authedRequest(stdhttp.MethodPatch, "/api/v1/workspaces/acme/members/"+targetUserID.String(), []byte(`{"role":"member"}`), "application/json")

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403 (only owner)", rec.Code)
	}
}

func TestMembershipRemoveOwnerSucceeds(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleOwner, false)
	targetUserID := uuid.New()

	rec := f.authedRequest(stdhttp.MethodDelete, "/api/v1/workspaces/acme/members/"+targetUserID.String(), nil, "")

	if rec.Code != stdhttp.StatusNoContent && rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !f.mbs.removeCalled {
		t.Fatal("Remove not called")
	}
	if f.mbs.removeInput.TargetUserID != targetUserID {
		t.Fatalf("target = %s, want %s", f.mbs.removeInput.TargetUserID, targetUserID)
	}
	if f.mbs.removeInput.ActorRole != value.RoleOwner {
		t.Fatalf("actor role = %v", f.mbs.removeInput.ActorRole)
	}
}

func TestMembershipListWithoutCookieReturns401(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleMember, false)

	rec := f.plainRequest(stdhttp.MethodGet, "/api/v1/workspaces/acme/members", nil, "")
	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestMembershipChangeRoleInvalidRoleReturns400.
func TestMembershipChangeRoleInvalidRoleReturns400(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleOwner, false)
	targetUserID := uuid.New()

	rec := f.authedRequest(stdhttp.MethodPatch, "/api/v1/workspaces/acme/members/"+targetUserID.String(), []byte(`{"role":"superuser"}`), "application/json")
	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid role", rec.Code)
	}
}
