//go:build integration

package db

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestExternalIdentitiesMigrationIntegration(t *testing.T) {
	ctx, gormDB := openIntegrationTestDB(t)
	var exists bool
	if err := gormDB.WithContext(ctx).Raw("SELECT to_regclass('public.external_identities') IS NOT NULL").Scan(&exists).Error; err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("external_identities 表未创建")
	}
}

func TestOIDCAuthTxJITFirstUserIsPlatformAdminIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	runner := NewOIDCAuthTxRunner(tx)

	var createdUserID uuid.UUID
	err := runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		if err := txx.AcquireBootstrapLock(ctx); err != nil {
			return err
		}
		count, err := txx.CountUsers(ctx)
		if err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("空库 count = %d, want 0", count)
		}
		user, err := model.NewProvisionalUser("ada@example.com", "Ada")
		if err != nil {
			return err
		}
		user.IsPlatformAdmin = count == 0
		if err := txx.CreateUser(ctx, user); err != nil {
			return err
		}
		identity, err := model.NewExternalIdentity(user.ID, "https://sso.example.com", "sub-1", "ada@example.com", true, `{"sub":"sub-1"}`)
		if err != nil {
			return err
		}
		if err := txx.CreateIdentity(ctx, identity); err != nil {
			return err
		}
		createdUserID = user.ID
		return nil
	})
	if err != nil {
		t.Fatalf("WithinOIDCAuth error: %v", err)
	}
	if createdUserID == uuid.Nil {
		t.Fatal("user should be created")
	}
}

func TestOIDCAuthTxJITUserWithoutEmailIntegration(t *testing.T) {
	// IdP 不返回 email 时，JIT 建号应允许无 email 用户，users.email 落库为 NULL。
	ctx, tx := newAuthTestDB(t)
	runner := NewOIDCAuthTxRunner(tx)

	var createdUserID uuid.UUID
	err := runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		if err := txx.AcquireBootstrapLock(ctx); err != nil {
			return err
		}
		user, err := model.NewProvisionalUser("", "NoEmail")
		if err != nil {
			return err
		}
		user.IsPlatformAdmin = true
		if err := txx.CreateUser(ctx, user); err != nil {
			return err
		}
		identity, err := model.NewExternalIdentity(user.ID, "https://sso.example.com", "sub-noemail", "", false, `{"sub":"sub-noemail"}`)
		if err != nil {
			return err
		}
		if err := txx.CreateIdentity(ctx, identity); err != nil {
			return err
		}
		createdUserID = user.ID
		return nil
	})
	if err != nil {
		t.Fatalf("WithinOIDCAuth error: %v", err)
	}

	// users.email 应为 NULL（不是空串）。
	var email *string
	if err := tx.Raw("SELECT email FROM users WHERE id = ?", createdUserID).Scan(&email).Error; err != nil {
		t.Fatal(err)
	}
	if email != nil {
		t.Fatalf("users.email = %q, want NULL for no-email OIDC user", *email)
	}

	// external_identities.email 也应 NULL。
	var idEmail *string
	if err := tx.Raw("SELECT email FROM external_identities WHERE user_id = ?", createdUserID).Scan(&idEmail).Error; err != nil {
		t.Fatal(err)
	}
	if idEmail != nil {
		t.Fatalf("external_identities.email = %q, want NULL", *idEmail)
	}
}

func TestOIDCAuthTxUniqueIssuerSubjectIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	runner := NewOIDCAuthTxRunner(tx)

	user, _ := model.NewProvisionalUser("ada@example.com", "Ada")
	err := runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		if err := txx.CreateUser(ctx, user); err != nil {
			return err
		}
		identity, _ := model.NewExternalIdentity(user.ID, "https://sso.example.com", "sub-1", "ada@example.com", true, "{}")
		return txx.CreateIdentity(ctx, identity)
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	err = runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		identity, _ := model.NewExternalIdentity(user.ID, "https://sso.example.com", "sub-1", "other@example.com", true, "{}")
		return txx.CreateIdentity(ctx, identity)
	})
	if err == nil {
		t.Fatal("duplicate (issuer, subject) should fail")
	}
}

func TestBootstrapAdvisoryLockSerializesConcurrentFirstUsers(t *testing.T) {
	// 两个并发事务在空库建首用户，advisory lock 应保证只有一个成为 platform_admin。
	ctx, gormDB := openIntegrationTestDB(t)

	results := make([]bool, 2)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			err := gormDB.Transaction(func(tx *gorm.DB) error {
				if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended('langhuan:auth-bootstrap', 0))").Error; err != nil {
					return err
				}
				var count int64
				tx.WithContext(ctx).Model(&UserRow{}).Count(&count)
				user, _ := model.NewProvisionalUser("user"+uuid.New().String()+"@example.com", "User")
				user.IsPlatformAdmin = count == 0
				if err := tx.WithContext(ctx).Create(userToRow(user)).Error; err != nil {
					return err
				}
				mu.Lock()
				results[idx] = user.IsPlatformAdmin
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("txn %d error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	adminCount := 0
	for _, isAdmin := range results {
		if isAdmin {
			adminCount++
		}
	}
	if adminCount != 1 {
		t.Fatalf("expected exactly 1 bootstrap platform_admin, got %d", adminCount)
	}
}

func TestBootstrapConcurrentJITAndInvitation(t *testing.T) {
	// 预置一个 platform_admin creator（已是首用户），JIT 与邀请接受并发建号时
	// count>0，两者都不应成为 platform_admin（验证已初始化库下无 admin 提升）。
	// 空库 bootstrap 唯一性由 TestBootstrapAdvisoryLockSerializesConcurrentFirstUsers 覆盖。
	ctx, gormDB := openIntegrationTestDB(t)
	runner := NewOIDCAuthTxRunner(gormDB)

	wsID := uuid.New()
	if err := gormDB.Exec(`INSERT INTO workspaces (id, slug, name) VALUES (?, 'conc-ws', 'Concurrent')`, wsID).Error; err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256HexForTest("conc-invite")
	now := time.Now().UTC()
	invID := uuid.New()
	creatorID := uuid.New()
	if err := gormDB.Create(&UserRow{ID: creatorID, Email: nullableString("conc-creator@example.com"), Nickname: "Creator", PasswordHash: "h", IsPlatformAdmin: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gormDB.Exec(`INSERT INTO workspace_invitations (id, workspace_id, invited_email, role, token_hash, token_prefix, expires_at, created_by)
		VALUES (?, ?, 'conc-invited@example.com', 'member', ?, 'ci_', ?, ?)`,
		invID, wsID, tokenHash, now.Add(time.Hour), creatorID).Error; err != nil {
		t.Fatal(err)
	}

	var g errgroup.Group
	jitAdmin := make([]bool, 0)
	inviteAdmin := make([]bool, 0)
	var mu sync.Mutex

	g.Go(func() error {
		return runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
			if err := txx.AcquireBootstrapLock(ctx); err != nil {
				return err
			}
			count, err := txx.CountUsers(ctx)
			if err != nil {
				return err
			}
			user, _ := model.NewProvisionalUser("jit"+uuid.New().String()+"@example.com", "JIT")
			user.IsPlatformAdmin = count == 0
			if err := txx.CreateUser(ctx, user); err != nil {
				return err
			}
			mu.Lock()
			jitAdmin = append(jitAdmin, user.IsPlatformAdmin)
			mu.Unlock()
			return nil
		})
	})

	g.Go(func() error {
		return runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
			inv, err := txx.FindPendingInvitationForUpdate(ctx, tokenHash)
			if err != nil {
				return err
			}
			if err := txx.AcquireBootstrapLock(ctx); err != nil {
				return err
			}
			count, err := txx.CountUsers(ctx)
			if err != nil {
				return err
			}
			user, _ := model.NewProvisionalUser(inv.InvitedEmail, "Invited")
			user.IsPlatformAdmin = count == 0
			if err := txx.CreateUser(ctx, user); err != nil {
				return err
			}
			membership, _ := model.NewMembership(inv.WorkspaceID, user.ID, inv.Role)
			if err := txx.CreateMembership(ctx, membership); err != nil {
				return err
			}
			if err := txx.MarkInvitationAccepted(ctx, inv.ID, user.ID); err != nil {
				return err
			}
			mu.Lock()
			inviteAdmin = append(inviteAdmin, user.IsPlatformAdmin)
			mu.Unlock()
			return nil
		})
	})

	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent error: %v", err)
	}

	// 已初始化库（creator 已存在），JIT 与邀请接受都不应成为 platform_admin。
	totalAdmin := 0
	for _, b := range jitAdmin {
		if b {
			totalAdmin++
		}
	}
	for _, b := range inviteAdmin {
		if b {
			totalAdmin++
		}
	}
	if totalAdmin != 0 {
		t.Fatalf("expected 0 platform_admin in initialized DB, got %d", totalAdmin)
	}
}

func TestOIDCAuthTxMarkInvitationAcceptedConflictIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	runner := NewOIDCAuthTxRunner(tx)

	wsID := createWorkspaceRow(t, ctx, tx, "oidc-invite-ws")
	tokenHash := sha256HexForTest("invite-token")
	now := time.Now().UTC()
	invID := uuid.New()
	creatorID := uuid.New()
	if err := tx.Create(&UserRow{ID: creatorID, Email: nullableString("creator@example.com"), Nickname: "Creator", PasswordHash: "h"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`INSERT INTO workspace_invitations (id, workspace_id, invited_email, role, token_hash, token_prefix, expires_at, created_by)
		VALUES (?, ?, 'invited@example.com', 'member', ?, 'inv_', ?, ?)`,
		invID, wsID, tokenHash, now.Add(time.Hour), creatorID).Error; err != nil {
		t.Fatal(err)
	}

	userID := uuid.New()
	if err := tx.Create(&UserRow{ID: userID, Email: nullableString("accepter@example.com"), Nickname: "Accepter", PasswordHash: "h"}).Error; err != nil {
		t.Fatal(err)
	}
	err := runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		return txx.MarkInvitationAccepted(ctx, invID, userID)
	})
	if err != nil {
		t.Fatalf("first MarkInvitationAccepted: %v", err)
	}

	err = runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		return txx.MarkInvitationAccepted(ctx, invID, uuid.New())
	})
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("expected ErrConflict on second accept, got %v", err)
	}
}

func TestOIDCAuthTxFindPendingInvitationForUpdateIntegration(t *testing.T) {
	ctx, tx := newAuthTestDB(t)
	runner := NewOIDCAuthTxRunner(tx)
	wsID := createWorkspaceRow(t, ctx, tx, "oidc-pending-ws")
	tokenHash := sha256HexForTest("pending-token")
	now := time.Now().UTC()
	invID := uuid.New()
	creatorID := uuid.New()
	if err := tx.Create(&UserRow{ID: creatorID, Email: nullableString("creator2@example.com"), Nickname: "Creator2", PasswordHash: "h"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`INSERT INTO workspace_invitations (id, workspace_id, invited_email, role, token_hash, token_prefix, expires_at, created_by)
		VALUES (?, ?, 'pending@example.com', 'member', ?, 'pen_', ?, ?)`,
		invID, wsID, tokenHash, now.Add(time.Hour), creatorID).Error; err != nil {
		t.Fatal(err)
	}

	var found *model.Invitation
	err := runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		inv, err := txx.FindPendingInvitationForUpdate(ctx, tokenHash)
		if err != nil {
			return err
		}
		found = inv
		return nil
	})
	if err != nil {
		t.Fatalf("FindPendingInvitationForUpdate: %v", err)
	}
	if found == nil || found.InvitedEmail != "pending@example.com" || found.Role != value.RoleMember {
		t.Fatalf("unexpected invitation: %+v", found)
	}

	err = runner.WithinOIDCAuth(ctx, func(txx service.OIDCAuthTx) error {
		_, err := txx.FindPendingInvitationForUpdate(ctx, sha256HexForTest("nonexistent"))
		return err
	})
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
