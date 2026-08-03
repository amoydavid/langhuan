//go:build integration

package db

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

func TestAuthUserRepositoryIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	repo := NewUserRepository(tx)

	user, err := model.NewUser("alice@example.com", "Alice", "$argon2id$v=19$hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create: %v", err)
	}

	gotByEmail, err := repo.FindByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if gotByEmail.ID != user.ID {
		t.Fatalf("FindByEmail id = %s, want %s", gotByEmail.ID, user.ID)
	}

	gotByID, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if gotByID.PasswordHash != user.PasswordHash {
		t.Fatalf("FindByID hash mismatch")
	}

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count < 1 {
		t.Fatalf("Count = %d, want >= 1", count)
	}

	if err := repo.UpdatePassword(ctx, user.ID, "$argon2id$new"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	updated, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordHash != "$argon2id$new" {
		t.Fatalf("password_hash = %q, want updated", updated.PasswordHash)
	}

	if err := repo.TouchLastLogin(ctx, user.ID); err != nil {
		t.Fatalf("TouchLastLogin: %v", err)
	}
	touched, err := repo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if touched.LastLoginAt == nil {
		t.Fatal("LastLoginAt should be set after TouchLastLogin")
	}

	if _, err := repo.FindByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing email err = %v, want ErrNotFound", err)
	}
	if _, err := repo.FindByID(ctx, uuid.New()); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing id err = %v, want ErrNotFound", err)
	}

	// 重复 email 必须映射为 ErrConflict
	dup, err := model.NewUser("alice@example.com", "Dup", "$argon2id$x")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, dup); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("duplicate email err = %v, want ErrConflict", err)
	}
}

func TestUserRepositoryListByIDsIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	repo := NewUserRepository(tx)
	users := make([]*model.User, 0, 3)
	for i, email := range []string{"batch-one@example.com", "batch-two@example.com", "batch-three@example.com"} {
		user, err := model.NewUser(email, "Batch", "$argon2id$h")
		if err != nil {
			t.Fatal(err)
		}
		user.ID = uuid.MustParse(fmt.Sprintf("%08d-0000-0000-0000-000000000001", i+1))
		if err := repo.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
		users = append(users, user)
	}

	got, err := repo.ListByIDs(ctx, []uuid.UUID{users[0].ID, users[2].ID, uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListByIDs()) = %d, want 2", len(got))
	}
	gotByID := map[uuid.UUID]*model.User{}
	for _, user := range got {
		gotByID[user.ID] = user
	}
	if gotByID[users[0].ID].Email != users[0].Email || gotByID[users[2].ID].Email != users[2].Email {
		t.Fatalf("ListByIDs() = %#v", got)
	}

	empty, err := repo.ListByIDs(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListByIDs(nil) = %#v", empty)
	}
}

func TestAuthUserRepositoryResetPasswordIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	sessRepo := NewSessionRepository(tx)

	user, err := model.NewUser("reset@example.com", "Reset", "$argon2id$old")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	// 预置两条活动会话。
	s1, err := model.NewSession(user.ID, time.Hour, "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := model.NewSession(user.ID, time.Hour, "ua2", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.Create(ctx, s1); err != nil {
		t.Fatal(err)
	}
	if err := sessRepo.Create(ctx, s2); err != nil {
		t.Fatal(err)
	}

	// ResetPassword 必须在同一事务内更新密码并删除该用户的全部会话。
	if err := userRepo.ResetPassword(ctx, user.ID, "$argon2id$new"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	updated, err := userRepo.FindByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.PasswordHash != "$argon2id$new" {
		t.Fatalf("password_hash = %q, want updated", updated.PasswordHash)
	}
	if _, err := sessRepo.FindActive(ctx, s1.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after ResetPassword s1 err = %v, want ErrNotFound", err)
	}
	if _, err := sessRepo.FindActive(ctx, s2.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after ResetPassword s2 err = %v, want ErrNotFound", err)
	}

	// 不存在的用户必须映射为 ErrRepositoryNotFound。
	if err := userRepo.ResetPassword(ctx, uuid.New(), "$argon2id$x"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("ResetPassword missing user err = %v, want ErrNotFound", err)
	}
}
