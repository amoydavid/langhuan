//go:build integration

package db

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestAuthMembershipRepositoryIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	mbRepo := NewMembershipRepository(tx)
	workspaceID := createWorkspaceRow(t, ctx, tx, "mb-ws")

	user, err := model.NewUser("mb@example.com", "MB", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	mb, err := model.NewMembership(workspaceID, user.ID, value.RoleOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := mbRepo.Create(ctx, mb); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := mbRepo.Get(ctx, workspaceID, user.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Role != value.RoleOwner {
		t.Fatalf("role = %q, want owner", got.Role)
	}

	if _, err := mbRepo.Get(ctx, workspaceID, uuid.New()); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing membership err = %v, want ErrNotFound", err)
	}

	// 第二个成员
	user2, err := model.NewUser("mb2@example.com", "MB2", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, user2); err != nil {
		t.Fatal(err)
	}
	mb2, err := model.NewMembership(workspaceID, user2.ID, value.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := mbRepo.Create(ctx, mb2); err != nil {
		t.Fatal(err)
	}

	list, err := mbRepo.List(ctx, workspaceID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}

	owners, err := mbRepo.CountOwners(ctx, workspaceID)
	if err != nil {
		t.Fatalf("CountOwners: %v", err)
	}
	if owners != 1 {
		t.Fatalf("owners = %d, want 1", owners)
	}

	if err := mbRepo.ChangeRole(ctx, workspaceID, user2.ID, value.RoleAdmin); err != nil {
		t.Fatalf("ChangeRole: %v", err)
	}
	changed, err := mbRepo.Get(ctx, workspaceID, user2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Role != value.RoleAdmin {
		t.Fatalf("role = %q, want admin", changed.Role)
	}

	if err := mbRepo.Delete(ctx, workspaceID, user2.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list2, err := mbRepo.List(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 1 {
		t.Fatalf("after Delete List len = %d, want 1", len(list2))
	}

	// 重复 (workspace,user) 必须 ErrConflict。
	// 该操作会让 Postgres 将事务标记为 aborted，因此放在测试最后执行，
	// 随后整个测试事务会被回滚清理。
	dup, err := model.NewMembership(workspaceID, user.ID, value.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := mbRepo.Create(ctx, dup); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("duplicate membership err = %v, want ErrConflict", err)
	}
}
