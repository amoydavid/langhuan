package value

import "testing"

func TestWorkspaceRoleOrdering(t *testing.T) {
	tests := []struct {
		name string
		a    WorkspaceRole
		b    WorkspaceRole
		want bool // a.AtLeast(b)
	}{
		{name: "owner >= admin", a: RoleOwner, b: RoleAdmin, want: true},
		{name: "owner >= member", a: RoleOwner, b: RoleMember, want: true},
		{name: "admin >= member", a: RoleAdmin, b: RoleMember, want: true},
		{name: "admin >= admin", a: RoleAdmin, b: RoleAdmin, want: true},
		{name: "member < admin", a: RoleMember, b: RoleAdmin, want: false},
		{name: "member < owner", a: RoleMember, b: RoleOwner, want: false},
		{name: "admin < owner", a: RoleAdmin, b: RoleOwner, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.AtLeast(tt.b); got != tt.want {
				t.Fatalf("%s.AtLeast(%s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestWorkspaceRoleRank(t *testing.T) {
	ranks := map[WorkspaceRole]int{
		RoleMember: 1,
		RoleAdmin:  2,
		RoleOwner:  3,
	}
	for role, want := range ranks {
		if got := role.Rank(); got != want {
			t.Fatalf("%s.Rank() = %d, want %d", role, got, want)
		}
	}
	if !(RoleMember.Rank() < RoleAdmin.Rank() && RoleAdmin.Rank() < RoleOwner.Rank()) {
		t.Fatal("expected member < admin < owner")
	}
}

func TestWorkspaceRoleIsValid(t *testing.T) {
	valid := []WorkspaceRole{RoleMember, RoleAdmin, RoleOwner, "member", "admin", "owner"}
	for _, r := range valid {
		if !r.IsValid() {
			t.Fatalf("%q should be valid", r)
		}
	}
	invalid := []WorkspaceRole{"", "superuser", "MEMBER", "guest"}
	for _, r := range invalid {
		if r.IsValid() {
			t.Fatalf("%q should be invalid", r)
		}
	}
}
