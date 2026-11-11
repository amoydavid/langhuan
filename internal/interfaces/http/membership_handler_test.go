package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
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

func TestMembershipSetKBGrantsAdminSucceeds(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleAdmin, false)
	targetUserID := uuid.New()
	kbID := uuid.New()
	f.mbs.kbGrantsErr = nil

	rec := f.authedRequest(stdhttp.MethodPut,
		"/api/v1/workspaces/acme/members/"+targetUserID.String()+"/kb-grants",
		[]byte(`{"kb_scope":"restricted","knowledge_base_ids":["`+kbID.String()+`"]}`), "application/json")

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	input := f.mbs.kbGrantsInput
	if input == nil {
		t.Fatal("SetKBGrants 未被调用")
	}
	if input.TargetUserID != targetUserID || input.WorkspaceID != f.wsID {
		t.Fatalf("input = %#v", input)
	}
	if input.ActorRole != value.RoleAdmin {
		t.Fatalf("actor role = %v, want admin (来自 AuthContext)", input.ActorRole)
	}
	if input.Scope != value.KBScopeRestricted || len(input.KnowledgeBaseIDs) != 1 || input.KnowledgeBaseIDs[0] != kbID {
		t.Fatalf("scope/ids = %v %v", input.Scope, input.KnowledgeBaseIDs)
	}
}

func TestMembershipSetKBGrantsScopeAllIgnoresIDs(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleOwner, false)
	f.mbs.kbGrantsErr = nil

	rec := f.authedRequest(stdhttp.MethodPut,
		"/api/v1/workspaces/acme/members/"+uuid.New().String()+"/kb-grants",
		[]byte(`{"kb_scope":"all","knowledge_base_ids":["`+uuid.New().String()+`"]}`), "application/json")

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if f.mbs.kbGrantsInput.Scope != value.KBScopeAll {
		t.Fatalf("scope = %v, want all", f.mbs.kbGrantsInput.Scope)
	}
}

func TestMembershipSetKBGrantsPermissionMatrix(t *testing.T) {
	tests := []struct {
		name       string
		actorRole  value.WorkspaceRole
		wantStatus int
	}{
		{name: "member 403", actorRole: value.RoleMember, wantStatus: stdhttp.StatusForbidden},
		{name: "admin 200", actorRole: value.RoleAdmin, wantStatus: stdhttp.StatusOK},
		{name: "owner 200", actorRole: value.RoleOwner, wantStatus: stdhttp.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSlugResourceFixtures(t, tt.actorRole, false)
			f.mbs.kbGrantsErr = nil
			rec := f.authedRequest(stdhttp.MethodPut,
				"/api/v1/workspaces/acme/members/"+uuid.New().String()+"/kb-grants",
				[]byte(`{"kb_scope":"all"}`), "application/json")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestMembershipSetKBGrantsValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "非法 scope", body: `{"kb_scope":"deny"}`, wantStatus: stdhttp.StatusBadRequest},
		{name: "坏 JSON", body: `{`, wantStatus: stdhttp.StatusBadRequest},
		{name: "坏 user_id", body: `{"kb_scope":"all"}`, wantStatus: stdhttp.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSlugResourceFixtures(t, value.RoleAdmin, false)
			path := "/api/v1/workspaces/acme/members/" + uuid.New().String() + "/kb-grants"
			if tt.name == "坏 user_id" {
				path = "/api/v1/workspaces/acme/members/not-a-uuid/kb-grants"
			}
			rec := f.authedRequest(stdhttp.MethodPut, path, []byte(tt.body), "application/json")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestMembershipSetKBGrantsServiceErrorMapped(t *testing.T) {
	f := newSlugResourceFixtures(t, value.RoleAdmin, false)
	f.mbs.kbGrantsErr = domainerrors.ErrValidation

	rec := f.authedRequest(stdhttp.MethodPut,
		"/api/v1/workspaces/acme/members/"+uuid.New().String()+"/kb-grants",
		[]byte(`{"kb_scope":"restricted"}`), "application/json")

	if rec.Code != stdhttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (admin 目标等校验失败)", rec.Code)
	}
}
