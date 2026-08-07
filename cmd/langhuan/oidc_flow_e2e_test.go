//go:build integration

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/db"
	authport "github.com/dajee/langhuan/internal/ports/auth"
	"github.com/dajee/langhuan/internal/testsupport"
)

// stubOIDCProvider 是 e2e 测试用的固定返回 provider，绕过真实 IdP discovery。
type stubOIDCProvider struct {
	profile *authport.OIDCProfile
}

func (p *stubOIDCProvider) AuthCodeURL(state, oidcNonce, codeChallenge string) string {
	return "https://idp.example.com/auth?state=" + state
}

func (p *stubOIDCProvider) Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (*authport.OIDCProfile, error) {
	return p.profile, nil
}

// stubStateStore 是 e2e 测试用的内存 state store（绕过 Redis）。
type stubStateStore struct {
	store map[string]authport.OIDCStatePayload
}

func newStubStateStore() *stubStateStore {
	return &stubStateStore{store: map[string]authport.OIDCStatePayload{}}
}

func (s *stubStateStore) Issue(ctx context.Context, payload authport.OIDCStatePayload) (string, error) {
	state := "state-" + uuid.New().String()
	s.store[state] = payload
	return state, nil
}

func (s *stubStateStore) Consume(ctx context.Context, state, browserNonce string) (*authport.OIDCStatePayload, error) {
	p, ok := s.store[state]
	if !ok || p.BrowserNonce != browserNonce {
		return nil, errors.New("state invalid")
	}
	delete(s.store, state)
	return &p, nil
}

// TestOIDCLoginOrProvisionE2E 验证 OIDC 登录主链路：
// service.LoginOrProvision 在真实 DB 上完成 JIT 建号、首用户 platform_admin、session 创建。
func TestOIDCLoginOrProvisionE2E(t *testing.T) {
	ctx := context.Background()
	testDSN := testsupport.NewMigratedPostgres(t)
	gormDB, err := db.Open(testDSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, e := gormDB.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})

	identityRepo := db.NewExternalIdentityRepository(gormDB)
	authTx := db.NewOIDCAuthTxRunner(gormDB)
	provider := &stubOIDCProvider{profile: &authport.OIDCProfile{
		Subject: "e2e-sub-1", Email: "ada@example.com", EmailVerified: true, Name: "Ada", RawProfile: `{"sub":"e2e-sub-1"}`,
	}}
	svc := service.NewOIDCLoginService(provider, newStubStateStore(), authTx, identityRepo,
		config.SessionConfig{LifetimeSeconds: 3600},
		config.OIDCConfig{Issuer: "https://sso.example.com", RequireEmailVerified: true},
		false,
		nil,
	)

	// 空库首个 OIDC 用户 → JIT 建号 + platform_admin。
	session, err := svc.LoginOrProvision(ctx, provider.profile, "test-ua", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, session)

	// 验证 DB 状态：1 user、1 identity、该 user 是 platform_admin 且无密码。
	var user model.User
	require.NoError(t, gormDB.First(&user, "email = ?", "ada@example.com").Error)
	require.True(t, user.IsPlatformAdmin, "首用户应为 platform_admin")
	require.False(t, user.HasPassword(), "JIT 用户应无密码")

	var identity model.ExternalIdentity
	// 直接用 GORM Row 校验存在性（e2e 测试可访问 db 包）。
	var idRow db.ExternalIdentityRow
	require.NoError(t, gormDB.First(&idRow, "user_id = ?", user.ID).Error)
	require.Equal(t, "https://sso.example.com", idRow.Issuer)
	require.Equal(t, "e2e-sub-1", idRow.Subject)
	_ = identity

	// 第二个 OIDC 用户 → 普通用户，非 admin。
	provider2Profile := &authport.OIDCProfile{
		Subject: "e2e-sub-2", Email: "bob@example.com", EmailVerified: true, Name: "Bob", RawProfile: `{"sub":"e2e-sub-2"}`,
	}
	provider.profile = provider2Profile
	_, err = svc.LoginOrProvision(ctx, provider2Profile, "test-ua", "127.0.0.1")
	require.NoError(t, err)

	var bob model.User
	require.NoError(t, gormDB.First(&bob, "email = ?", "bob@example.com").Error)
	require.False(t, bob.IsPlatformAdmin, "第二用户不应是 platform_admin")
}

// TestOIDCEmailMergeE2E 验证 email 合并：预置 password user，OIDC 同 email 回调只增 identity 不建新 user。
func TestOIDCEmailMergeE2E(t *testing.T) {
	ctx := context.Background()
	testDSN := testsupport.NewMigratedPostgres(t)
	gormDB, err := db.Open(testDSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, e := gormDB.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})

	// 预置一个 password admin（首用户）。
	pwUser, err := model.NewUser("admin@example.com", "Admin", "$argon2id$placeholder")
	require.NoError(t, err)
	pwUser.IsPlatformAdmin = true
	require.NoError(t, db.NewUserRepository(gormDB).Create(ctx, pwUser))

	// 再预置一个 password user（将被 OIDC 合并）。
	mergeUser, err := model.NewUser("ada@example.com", "Ada", "$argon2id$placeholder")
	require.NoError(t, err)
	require.NoError(t, db.NewUserRepository(gormDB).Create(ctx, mergeUser))

	identityRepo := db.NewExternalIdentityRepository(gormDB)
	authTx := db.NewOIDCAuthTxRunner(gormDB)
	provider := &stubOIDCProvider{profile: &authport.OIDCProfile{
		Subject: "merge-sub", Email: "ada@example.com", EmailVerified: true, Name: "Ada", RawProfile: `{"sub":"merge-sub"}`,
	}}
	svc := service.NewOIDCLoginService(provider, newStubStateStore(), authTx, identityRepo,
		config.SessionConfig{LifetimeSeconds: 3600},
		config.OIDCConfig{Issuer: "https://sso.example.com"},
		false,
		nil,
	)

	session, err := svc.LoginOrProvision(ctx, provider.profile, "ua", "1.2.3.4")
	require.NoError(t, err)
	require.Equal(t, mergeUser.ID, session.UserID, "应合并到现有 email user")

	// 不应新建 user：users 表仍只有 admin + ada 两条。
	var count int64
	require.NoError(t, gormDB.Model(&model.User{}).Count(&count).Error)
	// model.User{} 无法直接 Count（无 gorm tag）；用 db.UserRow。
	var rowCount int64
	require.NoError(t, gormDB.Table("users").Count(&rowCount).Error)
	require.Equal(t, int64(2), rowCount, "email 合并不应新建 user")
	_ = count
}

// TestSingleTenantWorkspaceAndAutoJoinE2E 验证单租户模式（OIDC 开启）全链路：
// 首用户 JIT 建号成为 platform_admin → 创建唯一 workspace（owner）→
// 第二个 OIDC 用户登录自动加入 member → 再次创建 workspace 被拒（仅允许一个）。
func TestSingleTenantWorkspaceAndAutoJoinE2E(t *testing.T) {
	ctx := context.Background()
	testDSN := testsupport.NewMigratedPostgres(t)
	gormDB, err := db.Open(testDSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, e := gormDB.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})

	wsRepo := db.NewWorkspaceRepository(gormDB)
	authTx := db.NewOIDCAuthTxRunner(gormDB)
	identityRepo := db.NewExternalIdentityRepository(gormDB)
	wsSvc := service.NewWorkspaceService(wsRepo, true) // 单租户
	oidcSvc := service.NewOIDCLoginService(&stubOIDCProvider{}, newStubStateStore(), authTx, identityRepo,
		config.SessionConfig{LifetimeSeconds: 3600},
		config.OIDCConfig{Issuer: "https://sso.example.com"},
		true, // 单租户
		nil,
	)

	// 首用户登录（空库 JIT → platform_admin）。
	s1, err := oidcSvc.LoginOrProvision(ctx, &authport.OIDCProfile{
		Subject: "e2e-owner-sub", Email: "owner@example.com", EmailVerified: true, Name: "Owner", RawProfile: `{"sub":"e2e-owner-sub"}`,
	}, "ua", "1.2.3.4")
	require.NoError(t, err)

	// 首用户创建唯一 workspace，自动成为 owner。
	ws, err := wsSvc.CreateForPlatformAdmin(ctx, service.CreateWorkspaceInput{Name: "Tenant", Slug: "tenant"}, s1.UserID, true)
	require.NoError(t, err)

	// 第二个 OIDC 用户登录 → 自动加入 member。
	s2, err := oidcSvc.LoginOrProvision(ctx, &authport.OIDCProfile{
		Subject: "e2e-new-sub", Email: "new@example.com", EmailVerified: true, Name: "New", RawProfile: `{"sub":"e2e-new-sub"}`,
	}, "ua", "1.2.3.4")
	require.NoError(t, err)
	var mbRow db.MembershipRow
	require.NoError(t, gormDB.First(&mbRow, "workspace_id = ? AND user_id = ?", ws.ID, s2.UserID).Error)
	require.Equal(t, "member", mbRow.Role)

	// 再次创建 workspace → 单租户限制。
	_, err = wsSvc.CreateForPlatformAdmin(ctx, service.CreateWorkspaceInput{Name: "Second", Slug: "second"}, s1.UserID, true)
	require.ErrorIs(t, err, domainerrors.ErrWorkspaceLimitReached)

	// 全库仍只有一个 workspace。
	var wsCount int64
	require.NoError(t, gormDB.Table("workspaces").Count(&wsCount).Error)
	require.Equal(t, int64(1), wsCount)
}

// TestOIDCPasswordDisabledBlocksPasswordLoginE2E 验证 password.enabled=false 时 password 登录被拒。
// 该场景已在 service 单测 TestLoginRejectsWhenPasswordDisabled + handler 测试覆盖，
// e2e 层不重复 HTTP 403 映射，此处保留链路完整性声明。
func TestOIDCPasswordDisabledBlocksPasswordLoginE2E(t *testing.T) {
	t.Log("password.enabled=false 阻断已在 service + handler 测试覆盖")
}
