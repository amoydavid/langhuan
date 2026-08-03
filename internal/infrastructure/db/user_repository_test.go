package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestUserRowMappingPreservesIdentityAndHash(t *testing.T) {
	now := time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC)
	last := now.Add(-time.Hour)
	u := &model.User{
		ID:              uuid.New(),
		Email:           "user@example.com",
		Nickname:        "user",
		PasswordHash:    "$argon2id$encoded",
		IsPlatformAdmin: true,
		LastLoginAt:     &last,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	row := userToRow(u)
	got := userFromRow(row)

	if got.ID != u.ID || got.Email != u.Email || got.Nickname != u.Nickname {
		t.Fatalf("identity not preserved: %#v", got)
	}
	if got.PasswordHash != u.PasswordHash {
		t.Fatalf("password_hash = %q, want %q", got.PasswordHash, u.PasswordHash)
	}
	if got.IsPlatformAdmin != u.IsPlatformAdmin {
		t.Fatalf("is_platform_admin = %v, want %v", got.IsPlatformAdmin, u.IsPlatformAdmin)
	}
	if got.LastLoginAt == nil || !got.LastLoginAt.Equal(last) {
		t.Fatalf("last_login_at = %v, want %v", got.LastLoginAt, last)
	}
}

func TestUserRowMappingHandlesNilLastLogin(t *testing.T) {
	u := &model.User{
		ID:           uuid.New(),
		Email:        "x@example.com",
		Nickname:     "x",
		PasswordHash: "$argon2id$encoded",
	}
	row := userToRow(u)
	if row.LastLoginAt != nil {
		t.Fatalf("row.LastLoginAt = %v, want nil", row.LastLoginAt)
	}
	got := userFromRow(row)
	if got.LastLoginAt != nil {
		t.Fatalf("got.LastLoginAt = %v, want nil", got.LastLoginAt)
	}
}

func TestUserRepositoryImplementsAuthContract(t *testing.T) {
	var repo *UserRepository
	var _ interface {
		Create(ctx context.Context, u *model.User) error
		FindByEmail(ctx context.Context, email string) (*model.User, error)
		FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
		Count(ctx context.Context) (int64, error)
		UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
		ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
		TouchLastLogin(ctx context.Context, id uuid.UUID) error
	} = repo
}
