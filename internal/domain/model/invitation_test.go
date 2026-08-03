package model

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewInvitationCreatesPendingInvitation(t *testing.T) {
	workspaceID := uuid.New()
	createdBy := uuid.New()
	now := time.Now().UTC()
	inv, err := NewInvitation(workspaceID, "Alice@Example.com", value.RoleMember, createdBy)
	if err != nil {
		t.Fatal(err)
	}
	if inv.ID == uuid.Nil {
		t.Fatal("invitation id should be generated")
	}
	if inv.WorkspaceID != workspaceID {
		t.Fatal("workspace id should be propagated")
	}
	if inv.InvitedEmail != "alice@example.com" {
		t.Fatalf("invited_email should be normalized to lowercase, got %q", inv.InvitedEmail)
	}
	if inv.Role != value.RoleMember {
		t.Fatalf("role = %q", inv.Role)
	}
	if inv.CreatedBy != createdBy {
		t.Fatal("created_by should be propagated")
	}
	// TokenHash/TokenPrefix are set by the application layer (Task 3/5); the model
	// constructor only validates identity + email + role, so they remain empty here.
	if !inv.ExpiresAt.After(now) {
		t.Fatalf("expires_at = %s should be after now", inv.ExpiresAt)
	}
	if !inv.IsPending(now) {
		t.Fatal("fresh invitation should be pending")
	}
}

func TestNewInvitationRejectsInvalidInput(t *testing.T) {
	validEmail := "alice@example.com"
	tests := []struct {
		name        string
		workspaceID uuid.UUID
		email       string
		role        value.WorkspaceRole
	}{
		{name: "nil workspace", workspaceID: uuid.Nil, email: validEmail, role: value.RoleMember},
		{name: "empty email", workspaceID: uuid.New(), email: "  ", role: value.RoleMember},
		{name: "invalid email", workspaceID: uuid.New(), email: "nope", role: value.RoleMember},
		{name: "invalid role", workspaceID: uuid.New(), email: validEmail, role: "wizard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewInvitation(tt.workspaceID, tt.email, tt.role, uuid.New())
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestInvitationIsPending(t *testing.T) {
	workspaceID := uuid.New()
	createdBy := uuid.New()

	inv, err := NewInvitation(workspaceID, "alice@example.com", value.RoleMember, createdBy)
	if err != nil {
		t.Fatal(err)
	}

	// Before expiry, no accept, no revoke -> pending.
	if !inv.IsPending(time.Now().UTC()) {
		t.Fatal("should be pending before expiry")
	}

	// After expiry -> not pending.
	if inv.IsPending(inv.ExpiresAt.Add(time.Second)) {
		t.Fatal("should not be pending after expiry")
	}

	// Accepted -> not pending.
	acceptedAt := time.Now().UTC()
	inv.AcceptedAt = &acceptedAt
	if inv.IsPending(time.Now().UTC()) {
		t.Fatal("accepted invitation should not be pending")
	}
	inv.AcceptedAt = nil

	// Revoked -> not pending.
	revokedAt := time.Now().UTC()
	inv.RevokedAt = &revokedAt
	if inv.IsPending(time.Now().UTC()) {
		t.Fatal("revoked invitation should not be pending")
	}
}
