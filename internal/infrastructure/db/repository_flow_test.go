//go:build integration

package db

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	authadapter "github.com/dajee/langhuan/internal/adapters/auth"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/testsupport"
)

func TestV021AuthFlow(t *testing.T) {
	ctx := context.Background()
	databaseURL := testsupport.NewMigratedPostgres(t)
	gormDB, err := Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}

	// Per-invocation unique namespace so concurrent runs never collide.
	runID := uuid.New().String()
	adminEmail := "v021-admin-" + runID + "@example.com"
	inviteeEmail := "v021-invitee-" + runID + "@example.com"
	slug := "v021-acme-" + runID[:8]

	// Repos constructed against the shared (committing) connection.
	userRepo := NewUserRepository(gormDB)
	sessRepo := NewSessionRepository(gormDB)
	mbRepo := NewMembershipRepository(gormDB)
	invRepo := NewInvitationRepository(gormDB)
	wsRepo := NewWorkspaceRepository(gormDB)
	kbRepo := NewKnowledgeBaseRepository(gormDB)

	// Low-cost Argon2 hasher for test speed; a fake limiter avoids Redis state.
	hasher := authadapter.NewArgon2Hasher(128, 1, 1)
	authCfg := config.AuthConfig{
		Session:    config.SessionConfig{CookieName: "langhuan_session", LifetimeSeconds: 3600},
		Password:   config.PasswordConfig{Argon2MemoryKiB: 128, Argon2Iterations: 1, Argon2Parallelism: 1, Enabled: true},
		RateLimit:  config.RateLimitConfig{LoginMaxAttempts: 5, LoginWindowSeconds: 900},
		Invitation: config.InvitationConfig{LifetimeSeconds: 3600},
	}
	userSvc := service.NewUserService(userRepo, hasher, true)
	authSvc := service.NewAuthService(userRepo, sessRepo, hasher, fakeRateLimiter{}, authCfg)
	invSvc := service.NewInvitationService(invRepo, wsRepo, userRepo, hasher, authCfg)
	mbSvc := service.NewMembershipService(mbRepo, userRepo)
	wsSvc := service.NewWorkspaceService(wsRepo, false)
	kbSvc := service.NewKnowledgeBaseService(kbRepo)

	// Track created workspace + user ids for cleanup. ORDER MATTERS: delete the
	// workspace FIRST (cascades memberships + invitations via workspace_id FK),
	// which removes the workspace_invitations.created_by / accepted_user_id
	// references (those FKs are NO ACTION, not CASCADE — deleting a user before
	// its invitations would fail the constraint). Only then delete the users,
	// which cascades their sessions via sessions.user_id ON DELETE CASCADE.
	var createdWorkspaceID uuid.UUID
	var createdUserIDs []uuid.UUID
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if createdWorkspaceID != uuid.Nil {
			_ = gormDB.WithContext(cleanupCtx).Where("id = ?", createdWorkspaceID).Delete(&WorkspaceRow{}).Error
		}
		for _, id := range createdUserIDs {
			_ = gormDB.WithContext(cleanupCtx).Where("id = ?", id).Delete(&UserRow{}).Error
		}
	})

	// 1. Seed the platform admin. RegisterFirstUser requires a globally empty
	// user table (it refuses once any user exists), which cannot be assumed on
	// the shared dev DB. The first-user gate itself is covered exhaustively in
	// internal/application/service/user_test.go, so here we create the admin
	// directly via the repo (IsPlatformAdmin=true) and exercise the full
	// multi-tenant flow that this test is concerned with: workspace creation,
	// invitation, acceptance, login, KB access, logout, and password reset.
	adminHash, err := hasher.Hash("pass123")
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	admin, err := model.NewUser(adminEmail, "Admin", adminHash)
	if err != nil {
		t.Fatal(err)
	}
	admin.IsPlatformAdmin = true
	if err := userRepo.Create(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	createdUserIDs = append(createdUserIDs, admin.ID)

	// 2. Create workspace as the platform admin -> owner membership.
	ws, err := wsSvc.CreateForPlatformAdmin(ctx, service.CreateWorkspaceInput{
		Name: "Acme",
		Slug: slug,
	}, admin.ID, true)
	if err != nil {
		t.Fatalf("CreateForPlatformAdmin: %v", err)
	}
	createdWorkspaceID = ws.ID
	ownerMB, err := mbSvc.Get(ctx, ws.ID, admin.ID)
	if err != nil {
		t.Fatalf("owner membership not created: %v", err)
	}
	if ownerMB.Role != value.RoleOwner {
		t.Fatalf("admin role = %q, want owner", ownerMB.Role)
	}

	// 3. Create invitation as the workspace owner.
	_, plaintextToken, err := invSvc.Create(ctx, service.CreateInvitationInput{
		WorkspaceID:  ws.ID,
		InvitedEmail: inviteeEmail,
		Role:         value.RoleMember,
		CreatedBy:    admin.ID,
		ActorRole:    value.RoleOwner,
	})
	if err != nil {
		t.Fatalf("Create invitation: %v", err)
	}

	// 4. Accept invitation -> invitee user + membership + session.
	acceptSession, err := invSvc.Accept(ctx, plaintextToken, inviteeEmail, "Member", "pass456", "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("Accept invitation: %v", err)
	}
	invitee, err := userRepo.FindByEmail(ctx, inviteeEmail)
	if err != nil {
		t.Fatalf("invitee not persisted: %v", err)
	}
	createdUserIDs = append(createdUserIDs, invitee.ID)
	inviteeMB, err := mbSvc.Get(ctx, ws.ID, invitee.ID)
	if err != nil {
		t.Fatalf("invitee membership not created: %v", err)
	}
	if inviteeMB.Role != value.RoleMember {
		t.Fatalf("invitee role = %q, want member", inviteeMB.Role)
	}

	// 5. Login as the invitee -> new active session.
	loginSession, err := authSvc.Login(ctx, inviteeEmail, "pass456", "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := sessRepo.FindActive(ctx, loginSession.ID); err != nil {
		t.Fatalf("login session not active: %v", err)
	}

	// 6. KB access: the invitee (a member) can create and read a KB in the
	// workspace; cross-workspace reads are not found.
	providerRepo := NewModelProviderRepository(gormDB)
	modelRepo := NewModelRepository(gormDB)
	provider, err := model.NewModelProvider(value.ModelScopeWorkspace, &ws.ID, "flow-openai", "Flow OpenAI", "", "openai", map[string]any{}, []byte("cipher"), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := providerRepo.Create(ctx, provider); err != nil {
		t.Fatal(err)
	}
	dimensions := 1024
	embeddingModel, err := model.NewModel(provider.ID, "flow-embed", "Flow Embedding", "", value.ModelTypeEmbedding, "flow-embed", &dimensions, map[string]any{}, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := modelRepo.Create(ctx, embeddingModel); err != nil {
		t.Fatal(err)
	}
	createdKB, err := kbSvc.Create(ctx, service.CreateKnowledgeBaseInput{
		WorkspaceID: ws.ID, Name: "docs", Description: "desc",
		EmbeddingModelID: embeddingModel.ID,
	})
	if err != nil {
		t.Fatalf("create kb: %v", err)
	}
	if _, err := kbRepo.Get(ctx, ws.ID, createdKB.ID); err != nil {
		t.Fatalf("read kb in workspace: %v", err)
	}
	if _, err := kbRepo.Get(ctx, uuid.New(), createdKB.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("cross-workspace kb err = %v, want ErrNotFound", err)
	}

	// 7. Other slug not found: a non-existent workspace slug maps to
	// ErrNotFound at the service/repo layer (the HTTP layer renders this 404).
	if _, err := wsSvc.GetBySlug(ctx, "does-not-exist-"+runID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("missing slug err = %v, want ErrNotFound", err)
	}
	// Non-member lookup: the invitee is NOT a member of a foreign workspace.
	if _, err := mbSvc.Get(ctx, uuid.New(), invitee.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("non-member membership err = %v, want ErrNotFound", err)
	}

	// 8. Logout deletes the session; the old session id is no longer active.
	if err := authSvc.Logout(ctx, loginSession.ID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := sessRepo.FindActive(ctx, loginSession.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after logout FindActive err = %v, want ErrNotFound", err)
	}

	// 9. Password reset (actor is platform admin): revokes ALL of the invitee's
	// sessions (including the accept-session) and allows login with the new
	// password; the old password no longer works.
	if err := userSvc.ResetPassword(ctx, admin.ID, true, invitee.ID, "newpass789"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if _, err := sessRepo.FindActive(ctx, acceptSession.ID); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("after reset old session err = %v, want ErrNotFound", err)
	}
	// Login with the new password succeeds.
	if _, err := authSvc.Login(ctx, inviteeEmail, "newpass789", "ua", "127.0.0.1"); err != nil {
		t.Fatalf("Login with new password: %v", err)
	}
	// Login with the old password fails (unauthorized).
	if _, err := authSvc.Login(ctx, inviteeEmail, "pass456", "ua", "127.0.0.1"); !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("Login with old password err = %v, want ErrUnauthorized", err)
	}
}
