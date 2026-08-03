package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestSessionRowMappingPreservesIdentity(t *testing.T) {
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	s := &model.Session{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		ExpiresAt:  expires,
		CreatedAt:  now,
		LastSeenAt: now,
		UserAgent:  "ua",
		IPAddr:     "127.0.0.1",
		RevokedAt:  nil,
	}

	row := sessionToRow(s)
	got := sessionFromRow(row)

	if got.ID != s.ID || got.UserID != s.UserID {
		t.Fatalf("identity not preserved: %#v", got)
	}
	if got.ExpiresAt != expires || got.UserAgent != s.UserAgent || got.IPAddr != s.IPAddr {
		t.Fatalf("session fields not preserved: %#v", got)
	}
	if got.RevokedAt != nil {
		t.Fatalf("revoked_at = %v, want nil", got.RevokedAt)
	}
}

func TestSessionRepositoryImplementsAuthContract(t *testing.T) {
	var repo *SessionRepository
	var _ interface {
		Create(ctx context.Context, s *model.Session) error
		FindActive(ctx context.Context, id uuid.UUID) (*model.Session, error)
		Delete(ctx context.Context, id uuid.UUID) error
		DeleteAllForUser(ctx context.Context, userID uuid.UUID) error
	} = repo
}
