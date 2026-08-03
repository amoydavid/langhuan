package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewMembershipCreatesMembership(t *testing.T) {
	workspaceID := uuid.New()
	userID := uuid.New()
	m, err := NewMembership(workspaceID, userID, value.RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == uuid.Nil {
		t.Fatal("membership id should be generated")
	}
	if m.WorkspaceID != workspaceID || m.UserID != userID {
		t.Fatal("ids should be propagated")
	}
	if m.Role != value.RoleOwner {
		t.Fatalf("role = %q", m.Role)
	}
	if !m.CreatedAt.Equal(m.UpdatedAt) {
		t.Fatal("created_at and updated_at should match initially")
	}
}

func TestNewMembershipRejectsInvalidRole(t *testing.T) {
	tests := []value.WorkspaceRole{
		"", "guest", "MEMBER", "superuser",
	}
	for _, role := range tests {
		_, err := NewMembership(uuid.New(), uuid.New(), role)
		if err == nil {
			t.Fatalf("role %q should be rejected", role)
		}
	}
	if _, err := NewMembership(uuid.Nil, uuid.New(), value.RoleMember); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("nil workspace id should be validation error, got %v", err)
	}
	if _, err := NewMembership(uuid.New(), uuid.Nil, value.RoleMember); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("nil user id should be validation error, got %v", err)
	}
}
