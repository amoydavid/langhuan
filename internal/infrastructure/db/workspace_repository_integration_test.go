//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestWorkspaceSlugGetBySlugIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	repo := NewWorkspaceRepository(tx)
	wsID := createWorkspaceRow(t, ctx, tx, "slug-test-ws")

	got, err := repo.GetBySlug(ctx, "slug-test-ws")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != wsID {
		t.Fatalf("id = %s, want %s", got.ID, wsID)
	}
	if got.Slug != "slug-test-ws" {
		t.Fatalf("slug = %q, want slug-test-ws", got.Slug)
	}

	if _, err := repo.GetBySlug(ctx, "does-not-exist"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing slug err = %v, want ErrNotFound", err)
	}
}

func TestWorkspaceCreateWithOwnerIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	wsRepo := NewWorkspaceRepository(tx)
	userRepo := NewUserRepository(tx)
	mbRepo := NewMembershipRepository(tx)

	owner, err := model.NewUser("cw-owner@example.com", "Owner", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, owner); err != nil {
		t.Fatal(err)
	}

	ws, err := model.NewWorkspace("New WS", "new-ws-slug", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsRepo.CreateWithOwner(ctx, ws, owner.ID); err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	got, err := wsRepo.GetBySlug(ctx, "new-ws-slug")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != ws.ID {
		t.Fatalf("workspace id = %s, want %s", got.ID, ws.ID)
	}

	mb, err := mbRepo.Get(ctx, ws.ID, owner.ID)
	if err != nil {
		t.Fatalf("owner membership not created: %v", err)
	}
	if mb.Role != value.RoleOwner {
		t.Fatalf("owner role = %q, want owner", mb.Role)
	}
}

// fakeRateLimiter is a no-op authport.RateLimiter used by TestV021AuthFlow so
// the end-to-end auth flow is never blocked by cross-test Redis rate-limit
// state. It records nothing and never blocks.
type fakeRateLimiter struct{}

func (fakeRateLimiter) IsBlocked(context.Context, string, int) (bool, error)       { return false, nil }
func (fakeRateLimiter) RecordFailure(context.Context, string, time.Duration) error { return nil }
func (fakeRateLimiter) Reset(context.Context, string) error                        { return nil }
