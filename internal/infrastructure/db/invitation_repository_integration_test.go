//go:build integration

package db

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestAuthInvitationRepositoryIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	invRepo := NewInvitationRepository(tx)
	workspaceID := createWorkspaceRow(t, ctx, tx, "inv-ws")

	creator, err := model.NewUser("creator@example.com", "Creator", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, creator); err != nil {
		t.Fatal(err)
	}

	inv, err := model.NewInvitation(workspaceID, "invite@example.com", value.RoleMember, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = "hash-pending-123"
	inv.TokenPrefix = "hash12"
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatalf("Create: %v", err)
	}

	pending, err := invRepo.FindPendingByTokenHash(ctx, "hash-pending-123")
	if err != nil {
		t.Fatalf("FindPendingByTokenHash: %v", err)
	}
	if pending.ID != inv.ID {
		t.Fatalf("FindPendingByTokenHash id = %s, want %s", pending.ID, inv.ID)
	}

	// 不存在的 token
	if _, err := invRepo.FindPendingByTokenHash(ctx, "nonexistent-hash"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing invitation err = %v, want ErrNotFound", err)
	}

	// 撤销后不再 pending
	if err := invRepo.Revoke(ctx, inv.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := invRepo.FindPendingByTokenHash(ctx, "hash-pending-123"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("revoked FindPendingByTokenHash err = %v, want ErrNotFound", err)
	}

	// 重复 (workspace, email) 必须 ErrConflict
	inv2, err := model.NewInvitation(workspaceID, "invite@example.com", value.RoleAdmin, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	inv2.TokenHash = "hash-2"
	inv2.TokenPrefix = "h2"
	if err := invRepo.Create(ctx, inv2); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("duplicate invitation err = %v, want ErrConflict", err)
	}
}

func TestInvitationRepositoryListByWorkspaceIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	repo := NewInvitationRepository(tx)
	workspaceA := createWorkspaceRow(t, ctx, tx, "inv-list-a")
	workspaceB := createWorkspaceRow(t, ctx, tx, "inv-list-b")
	creator, err := model.NewUser("inv-list-creator@example.com", "Creator", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, creator); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)

	createInvitation := func(id string, workspaceID uuid.UUID, email string) uuid.UUID {
		t.Helper()
		invitation, err := model.NewInvitation(workspaceID, email, value.RoleMember, creator.ID)
		if err != nil {
			t.Fatal(err)
		}
		invitation.ID = uuid.MustParse(id)
		invitation.TokenHash = "hash-" + id
		invitation.TokenPrefix = id[:8]
		invitation.CreatedAt = createdAt
		if err := repo.Create(ctx, invitation); err != nil {
			t.Fatal(err)
		}
		return invitation.ID
	}
	id1 := createInvitation("10000000-0000-0000-0000-000000000001", workspaceA, "one@example.com")
	id2 := createInvitation("20000000-0000-0000-0000-000000000002", workspaceA, "two@example.com")
	createInvitation("30000000-0000-0000-0000-000000000003", workspaceB, "other@example.com")

	got, err := repo.ListByWorkspace(ctx, workspaceA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != id2 || got[1].ID != id1 {
		t.Fatalf("ListByWorkspace() = %#v", got)
	}
}

func TestAuthInvitationAcceptRegistrationIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	invRepo := NewInvitationRepository(tx)
	mbRepo := NewMembershipRepository(tx)
	sessRepo := NewSessionRepository(tx)
	workspaceID := createWorkspaceRow(t, ctx, tx, "accept-ws")

	creator, err := model.NewUser("accept-creator@example.com", "Creator", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, creator); err != nil {
		t.Fatal(err)
	}

	inv, err := model.NewInvitation(workspaceID, "newuser@example.com", value.RoleMember, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = "accept-hash"
	inv.TokenPrefix = "acc"
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}

	newUser, err := model.NewUser("newuser@example.com", "New", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := model.NewMembership(workspaceID, newUser.ID, value.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := model.NewSession(newUser.ID, time.Hour, "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	if err := invRepo.AcceptRegistration(ctx, inv, newUser, mb, sess); err != nil {
		t.Fatalf("AcceptRegistration: %v", err)
	}

	// 四个对象都应落库
	if _, err := userRepo.FindByID(ctx, newUser.ID); err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
	if _, err := mbRepo.Get(ctx, workspaceID, newUser.ID); err != nil {
		t.Fatalf("membership not persisted: %v", err)
	}
	if _, err := sessRepo.FindActive(ctx, sess.ID); err != nil {
		t.Fatalf("session not persisted: %v", err)
	}

	// 邀请应标记为已接受
	got, err := invRepo.FindPendingByTokenHash(ctx, "accept-hash")
	if err == nil {
		t.Fatalf("FindPendingByTokenHash returned invitation after accept: %#v", got)
	}
	if !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after accept FindPendingByTokenHash err = %v, want ErrNotFound", err)
	}

	// 直接读取邀请行确认 accepted_at 与 accepted_user_id 已写
	var row InvitationRow
	if err := tx.WithContext(ctx).First(&row, "id = ?", inv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AcceptedAt == nil {
		t.Fatal("invitation accepted_at should be set")
	}
	if row.AcceptedUserID == nil || *row.AcceptedUserID != newUser.ID {
		t.Fatalf("accepted_user_id = %v, want %s", row.AcceptedUserID, newUser.ID)
	}
}

func TestAuthInvitationAcceptRegistrationRejectsReacceptIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	invRepo := NewInvitationRepository(tx)
	workspaceID := createWorkspaceRow(t, ctx, tx, "reaccept-ws")

	creator, err := model.NewUser("reaccept-creator@example.com", "Creator", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, creator); err != nil {
		t.Fatal(err)
	}

	inv, err := model.NewInvitation(workspaceID, "reaccept@example.com", value.RoleMember, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = "reaccept-hash"
	inv.TokenPrefix = "re"
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}

	buildObjects := func() (*model.User, *model.Membership, *model.Session) {
		u, err := model.NewUser("reaccept@example.com", "RA", "$argon2id$h")
		if err != nil {
			t.Fatal(err)
		}
		mb, err := model.NewMembership(workspaceID, u.ID, value.RoleMember)
		if err != nil {
			t.Fatal(err)
		}
		s, err := model.NewSession(u.ID, time.Hour, "ua", "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		return u, mb, s
	}

	// 第一次接受成功
	user, mb, sess := buildObjects()
	if err := invRepo.AcceptRegistration(ctx, inv, user, mb, sess); err != nil {
		t.Fatalf("first AcceptRegistration: %v", err)
	}

	// 第二次接受同一条邀请：数据库 WHERE pending 拦截，返回 ErrConflict。
	// 注意：第二个 user 的 email 与第一个相同，会在事务内先因唯一约束失败，
	// 但即便构造不同 email，最终 invitation 更新的 RowsAffected==0 也会返回 ErrConflict。
	user2, mb2, sess2 := buildObjects()
	user2.Email = "reaccept2@example.com"
	mb2.UserID = user2.ID
	sess2.UserID = user2.ID
	err = invRepo.AcceptRegistration(ctx, inv, user2, mb2, sess2)
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("second AcceptRegistration err = %v, want ErrConflict", err)
	}

	// 邀请仍只被第一个 user 接受
	var row InvitationRow
	if err := tx.WithContext(ctx).First(&row, "id = ?", inv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AcceptedUserID == nil || *row.AcceptedUserID != user.ID {
		t.Fatalf("accepted_user_id = %v, want %s", row.AcceptedUserID, user.ID)
	}
}

func TestAuthInvitationMarkAcceptedIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	userRepo := NewUserRepository(tx)
	invRepo := NewInvitationRepository(tx)
	workspaceID := createWorkspaceRow(t, ctx, tx, "markacc-ws")

	creator, err := model.NewUser("markacc-creator@example.com", "Creator", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, creator); err != nil {
		t.Fatal(err)
	}

	inv, err := model.NewInvitation(workspaceID, "mark@example.com", value.RoleMember, creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = "markacc-hash"
	inv.TokenPrefix = "mk"
	if err := invRepo.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}

	accepter, err := model.NewUser("mark@example.com", "Mark", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, accepter); err != nil {
		t.Fatal(err)
	}

	if err := invRepo.MarkAccepted(ctx, inv.ID, accepter.ID); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	if _, err := invRepo.FindPendingByTokenHash(ctx, "markacc-hash"); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after MarkAccepted FindPendingByTokenHash err = %v, want ErrNotFound", err)
	}

	// 重复接受同一条邀请必须被拒绝为 ErrConflict（数据库 WHERE pending 保证原子单次接受）。
	otherUser, err := model.NewUser("other@example.com", "Other", "$argon2id$h")
	if err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, otherUser); err != nil {
		t.Fatal(err)
	}
	if err := invRepo.MarkAccepted(ctx, inv.ID, otherUser.ID); !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("re-accept err = %v, want ErrConflict", err)
	}
	// 原接受者保持不变
	var row InvitationRow
	if err := tx.WithContext(ctx).First(&row, "id = ?", inv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AcceptedUserID == nil || *row.AcceptedUserID != accepter.ID {
		t.Fatalf("accepted_user_id = %v, want %s (re-accept must not overwrite)", row.AcceptedUserID, accepter.ID)
	}
}
