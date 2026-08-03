package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestMembershipRowMappingPreservesRole(t *testing.T) {
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	m := &model.Membership{
		ID:          uuid.New(),
		WorkspaceID: uuid.New(),
		UserID:      uuid.New(),
		Role:        value.RoleOwner,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	row := membershipToRow(m)
	got := membershipFromRow(row)

	if got.ID != m.ID || got.WorkspaceID != m.WorkspaceID || got.UserID != m.UserID {
		t.Fatalf("identity not preserved: %#v", got)
	}
	if got.Role != m.Role {
		t.Fatalf("role = %q, want %q", got.Role, m.Role)
	}
}

func TestMembershipRepositoryImplementsAuthContract(t *testing.T) {
	var repo *MembershipRepository
	var _ interface {
		Create(ctx context.Context, m *model.Membership) error
		Get(ctx context.Context, workspaceID, userID uuid.UUID) (*model.Membership, error)
		List(ctx context.Context, workspaceID uuid.UUID) ([]*model.Membership, error)
		ChangeRole(ctx context.Context, workspaceID, userID uuid.UUID, role value.WorkspaceRole) error
		Delete(ctx context.Context, workspaceID, userID uuid.UUID) error
		CountOwners(ctx context.Context, workspaceID uuid.UUID) (int64, error)
	} = repo
}
