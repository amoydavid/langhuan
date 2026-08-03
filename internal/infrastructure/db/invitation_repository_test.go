package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestInvitationRowMappingPreservesTokenFields(t *testing.T) {
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	expires := now.Add(24 * time.Hour)
	inv := &model.Invitation{
		ID:           uuid.New(),
		WorkspaceID:  uuid.New(),
		InvitedEmail: "invite@example.com",
		Role:         value.RoleMember,
		TokenHash:    "hash123",
		TokenPrefix:  "tok12345",
		ExpiresAt:    expires,
		CreatedBy:    uuid.New(),
		CreatedAt:    now,
	}

	row := invitationToRow(inv)
	got := invitationFromRow(row)

	if got.ID != inv.ID || got.WorkspaceID != inv.WorkspaceID {
		t.Fatalf("identity not preserved: %#v", got)
	}
	if got.TokenHash != inv.TokenHash || got.TokenPrefix != inv.TokenPrefix {
		t.Fatalf("token fields not preserved: %#v", got)
	}
	if got.InvitedEmail != inv.InvitedEmail || got.Role != inv.Role {
		t.Fatalf("email/role not preserved: %#v", got)
	}
	if got.AcceptedAt != nil {
		t.Fatalf("accepted_at = %v, want nil", got.AcceptedAt)
	}
	if got.AcceptedUserID != uuid.Nil {
		t.Fatalf("accepted_user_id = %s, want nil", got.AcceptedUserID)
	}
}

func TestInvitationRepositoryImplementsAuthContract(t *testing.T) {
	var repo *InvitationRepository
	var _ interface {
		Create(ctx context.Context, inv *model.Invitation) error
		FindByID(ctx context.Context, id uuid.UUID) (*model.Invitation, error)
		FindPendingByTokenHash(ctx context.Context, tokenHash string) (*model.Invitation, error)
		Revoke(ctx context.Context, id uuid.UUID) error
		MarkAccepted(ctx context.Context, id, userID uuid.UUID) error
		AcceptRegistration(ctx context.Context, inv *model.Invitation, user *model.User, membership *model.Membership, session *model.Session) error
	} = repo
}
