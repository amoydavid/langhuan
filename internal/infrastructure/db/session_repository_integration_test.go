//go:build integration

package db

import (
	"errors"
	"testing"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestAuthSessionRepositoryIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	sessRepo := NewSessionRepository(tx)

	user, err := model.NewUser("sess@example.com", "Sess", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	// 活跃会话
	active, err := model.NewSession(user.ID, time.Hour, "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.Create(ctx, active); err != nil {
		t.Fatalf("create active: %v", err)
	}
	gotActive, err := sessRepo.FindActive(ctx, active.ID)
	if err != nil {
		t.Fatalf("FindActive: %v", err)
	}
	if gotActive.ID != active.ID {
		t.Fatalf("FindActive id = %s, want %s", gotActive.ID, active.ID)
	}

	// 过期会话
	expired, err := model.NewSession(user.ID, time.Hour, "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if err := sessRepo.Create(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := sessRepo.FindActive(ctx, expired.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("expired FindActive err = %v, want ErrNotFound", err)
	}

	// 已撤销会话
	revoked, err := model.NewSession(user.ID, time.Hour, "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	revoked.RevokedAt = &now
	if err := sessRepo.Create(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	if _, err := sessRepo.FindActive(ctx, revoked.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("revoked FindActive err = %v, want ErrNotFound", err)
	}

	// Delete 注销单条
	if err := sessRepo.Delete(ctx, active.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := sessRepo.FindActive(ctx, active.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after Delete FindActive err = %v, want ErrNotFound", err)
	}

	// DeleteAllForUser 清空该用户所有会话
	another, err := model.NewSession(user.ID, time.Hour, "ua2", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.Create(ctx, another); err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.DeleteAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	if _, err := sessRepo.FindActive(ctx, another.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after DeleteAllForUser FindActive err = %v, want ErrNotFound", err)
	}
}
