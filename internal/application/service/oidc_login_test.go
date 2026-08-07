package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// fakeOIDCProvider 实现 authport.OIDCProvider 用于测试。
type fakeOIDCProvider struct {
	authURL string
	profile *authport.OIDCProfile
	exchErr error
}

func (f *fakeOIDCProvider) AuthCodeURL(state, oidcNonce, codeChallenge string) string {
	return f.authURL
}

func (f *fakeOIDCProvider) Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (*authport.OIDCProfile, error) {
	if f.exchErr != nil {
		return nil, f.exchErr
	}
	return f.profile, nil
}

// fakeStateStore 实现 authport.OIDCStateStore 用于测试。
type fakeStateStore struct {
	issued   map[string]authport.OIDCStatePayload
	consumed map[string]bool
}

func newFakeStateStore() *fakeStateStore {
	return &fakeStateStore{issued: map[string]authport.OIDCStatePayload{}, consumed: map[string]bool{}}
}

func (s *fakeStateStore) Issue(ctx context.Context, payload authport.OIDCStatePayload) (string, error) {
	state := "state-" + uuid.New().String()
	s.issued[state] = payload
	return state, nil
}

func (s *fakeStateStore) Consume(ctx context.Context, state, browserNonce string) (*authport.OIDCStatePayload, error) {
	payload, ok := s.issued[state]
	if !ok || s.consumed[state] {
		return nil, errors.New("state invalid")
	}
	s.consumed[state] = true
	if payload.BrowserNonce != browserNonce {
		return nil, errors.New("nonce mismatch")
	}
	return &payload, nil
}

// fakeAuthTx 实现 OIDCAuthTx 与 OIDCAuthTxRunner 用于测试。
type fakeAuthTx struct {
	users         map[uuid.UUID]*model.User
	usersByEmail  map[string]*model.User
	identities    []*model.ExternalIdentity
	sessions      []*model.Session
	memberships   []*model.Membership
	invitations   map[string]*model.Invitation // tokenHash → invitation
	bootstrapLock bool
	bootstrapCnt  int // 记录 AcquireBootstrapLock 调用次数
	failOn        string
}

func newFakeAuthTx() *fakeAuthTx {
	return &fakeAuthTx{
		users:        map[uuid.UUID]*model.User{},
		usersByEmail: map[string]*model.User{},
		invitations:  map[string]*model.Invitation{},
	}
}

func (f *fakeAuthTx) WithinOIDCAuth(ctx context.Context, fn func(tx OIDCAuthTx) error) error {
	return fn(f)
}

func (f *fakeAuthTx) AcquireBootstrapLock(ctx context.Context) error {
	f.bootstrapCnt++
	f.bootstrapLock = true
	return nil
}

func (f *fakeAuthTx) CountUsers(ctx context.Context) (int64, error) {
	return int64(len(f.users)), nil
}

func (f *fakeAuthTx) FindIdentityByIssuerSubject(ctx context.Context, issuer, subject string) (*model.ExternalIdentity, error) {
	for _, id := range f.identities {
		if id.Issuer == issuer && id.Subject == subject {
			return id, nil
		}
	}
	return nil, domainerrors.ErrNotFound
}

func (f *fakeAuthTx) FindUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (f *fakeAuthTx) FindUserByEmail(ctx context.Context, email string) (*model.User, error) {
	if u, ok := f.usersByEmail[strings.ToLower(strings.TrimSpace(email))]; ok {
		return u, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (f *fakeAuthTx) CreateUser(ctx context.Context, user *model.User) error {
	if f.failOn == "create_user" {
		return errors.New("create user failed")
	}
	f.users[user.ID] = user
	f.usersByEmail[user.Email] = user
	return nil
}

func (f *fakeAuthTx) CreateIdentity(ctx context.Context, identity *model.ExternalIdentity) error {
	f.identities = append(f.identities, identity)
	return nil
}

func (f *fakeAuthTx) UpdateIdentityAuth(ctx context.Context, identity *model.ExternalIdentity, rawProfile string) error {
	identity.RawProfile = rawProfile
	return nil
}

func (f *fakeAuthTx) CreateSession(ctx context.Context, session *model.Session) error {
	f.sessions = append(f.sessions, session)
	return nil
}

func (f *fakeAuthTx) TouchLastLogin(ctx context.Context, userID uuid.UUID) error { return nil }

func (f *fakeAuthTx) FindActiveSession(ctx context.Context, sessionID uuid.UUID) (*model.Session, error) {
	for _, s := range f.sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return nil, domainerrors.ErrNotFound
}

func (f *fakeAuthTx) FindPendingInvitationForUpdate(ctx context.Context, tokenHash string) (*model.Invitation, error) {
	if inv, ok := f.invitations[tokenHash]; ok {
		return inv, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (f *fakeAuthTx) CreateMembership(ctx context.Context, membership *model.Membership) error {
	f.memberships = append(f.memberships, membership)
	return nil
}

func (f *fakeAuthTx) MarkInvitationAccepted(ctx context.Context, invitationID, userID uuid.UUID) error {
	for _, inv := range f.invitations {
		if inv.ID == invitationID {
			inv.AcceptedAt = ptrTimeNow()
			inv.AcceptedUserID = userID
			return nil
		}
	}
	return domainerrors.ErrConflict
}

// fakeIdentityReader 实现 ExternalIdentityReader。
type fakeIdentityReader struct {
	list []*model.ExternalIdentity
}

func (r *fakeIdentityReader) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*model.ExternalIdentity, error) {
	var out []*model.ExternalIdentity
	for _, id := range r.list {
		if id.UserID == userID {
			out = append(out, id)
		}
	}
	return out, nil
}

func ptrTimeNow() *time.Time {
	now := time.Now()
	return &now
}

func newTestOIDCLoginService(t *testing.T, issuer string, requireVerified bool) (*OIDCLoginService, *fakeOIDCProvider, *fakeStateStore, *fakeAuthTx, *fakeIdentityReader) {
	t.Helper()
	prov := &fakeOIDCProvider{authURL: "https://idp.example.com/auth"}
	store := newFakeStateStore()
	tx := newFakeAuthTx()
	reader := &fakeIdentityReader{}
	svc := NewOIDCLoginService(prov, store, tx, reader,
		config.SessionConfig{LifetimeSeconds: 3600},
		config.OIDCConfig{Issuer: issuer, RequireEmailVerified: requireVerified},
		nil,
	)
	return svc, prov, store, tx, reader
}

func validProfile(sub, email string, verified bool) *authport.OIDCProfile {
	return &authport.OIDCProfile{
		Subject:       sub,
		Email:         email,
		EmailVerified: verified,
		Name:          "Test User",
		RawProfile:    `{"sub":"` + sub + `"}`,
	}
}

func TestLoginOrProvisionReusesExistingIdentity(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	// 预置 user + identity。
	user, _ := model.NewUser("ada@example.com", "Ada", "$argon2id$h")
	tx.users[user.ID] = user
	tx.usersByEmail[user.Email] = user
	identity, _ := model.NewExternalIdentity(user.ID, "https://sso.example.com", "sub-1", "ada@example.com", true, "{}")
	tx.identities = append(tx.identities, identity)

	session, err := svc.LoginOrProvision(ctx, validProfile("sub-1", "ada@example.com", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("LoginOrProvision error: %v", err)
	}
	if session.UserID != user.ID {
		t.Fatalf("session user = %v, want %v", session.UserID, user.ID)
	}
	if len(tx.users) != 1 {
		t.Fatalf("should not create new user, got %d users", len(tx.users))
	}
}

func TestLoginOrProvisionMergesByEmail(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	// 预置 password user（无 identity）。
	user, _ := model.NewUser("ada@example.com", "Ada", "$argon2id$h")
	tx.users[user.ID] = user
	tx.usersByEmail[user.Email] = user

	session, err := svc.LoginOrProvision(ctx, validProfile("sub-new", "ada@example.com", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("LoginOrProvision error: %v", err)
	}
	if session.UserID != user.ID {
		t.Fatalf("should merge to existing user")
	}
	if len(tx.users) != 1 {
		t.Fatalf("should not create new user on email merge, got %d", len(tx.users))
	}
	// 应新建 identity 指向该 user。
	if len(tx.identities) != 1 || tx.identities[0].UserID != user.ID {
		t.Fatalf("should attach identity to existing user, got %+v", tx.identities)
	}
}

func TestLoginOrProvisionJITFirstUserBecomesPlatformAdmin(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()

	session, err := svc.LoginOrProvision(ctx, validProfile("sub-1", "ada@example.com", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("LoginOrProvision error: %v", err)
	}
	if len(tx.users) != 1 {
		t.Fatalf("should create 1 user, got %d", len(tx.users))
	}
	for _, u := range tx.users {
		if !u.IsPlatformAdmin {
			t.Fatal("first JIT user should be platform_admin")
		}
		if u.HasPassword() {
			t.Fatal("JIT user should be passwordless")
		}
	}
	if tx.bootstrapCnt != 1 {
		t.Fatalf("should acquire bootstrap lock once, got %d", tx.bootstrapCnt)
	}
	if session == nil {
		t.Fatal("session should be created")
	}
}

func TestLoginOrProvisionJITSubsequentUserNotAdmin(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	// 预置一个 admin。
	admin, _ := model.NewUser("admin@example.com", "Admin", "$argon2id$h")
	admin.IsPlatformAdmin = true
	tx.users[admin.ID] = admin
	tx.usersByEmail[admin.Email] = admin

	_, err := svc.LoginOrProvision(ctx, validProfile("sub-2", "bob@example.com", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("LoginOrProvision error: %v", err)
	}
	if len(tx.users) != 2 {
		t.Fatalf("should have 2 users, got %d", len(tx.users))
	}
	for _, u := range tx.users {
		if u.Email == "bob@example.com" && u.IsPlatformAdmin {
			t.Fatal("second JIT user should NOT be platform_admin")
		}
	}
}

func TestLoginOrProvisionRejectsMissingSub(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	profile := validProfile("", "ada@example.com", true)
	_, err := svc.LoginOrProvision(ctx, profile, "ua", "1.2.3.4")
	if err == nil {
		t.Fatal("should reject missing sub")
	}
	if len(tx.users) != 0 {
		t.Fatal("no user should be created on rejection")
	}
}

func TestLoginOrProvisionRejectsUnverifiedEmailWhenRequired(t *testing.T) {
	svc, _, _, _, _ := newTestOIDCLoginService(t, "https://sso.example.com", true)
	ctx := context.Background()
	_, err := svc.LoginOrProvision(ctx, validProfile("sub-1", "ada@example.com", false), "ua", "1.2.3.4")
	if err == nil {
		t.Fatal("should reject unverified email when require_email_verified=true")
	}
}

func TestLoginOrProvisionAllowsMissingEmail(t *testing.T) {
	// IdP 出于隐私可能不返回 email；应允许 JIT 建无 email 用户。
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	session, err := svc.LoginOrProvision(ctx, validProfile("sub-1", "", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("LoginOrProvision should allow missing email: %v", err)
	}
	if session == nil {
		t.Fatal("session should be created")
	}
	if len(tx.users) != 1 {
		t.Fatalf("should create 1 user, got %d", len(tx.users))
	}
	for _, u := range tx.users {
		if u.Email != "" {
			t.Fatalf("user email = %q, want empty (no email from IdP)", u.Email)
		}
		if u.IsPlatformAdmin != true {
			t.Fatal("first user should be platform_admin")
		}
	}
}

func TestLoginOrProvisionMissingEmailDoesNotMerge(t *testing.T) {
	// 无 email 的 OIDC 用户不应与任何现有用户"合并"（无 email 无法匹配），
	// 必须 JIT 新建独立用户。
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	existing, _ := model.NewUser("ada@example.com", "Ada", "$argon2id$h")
	tx.users[existing.ID] = existing
	tx.usersByEmail[existing.Email] = existing

	session, err := svc.LoginOrProvision(ctx, validProfile("sub-1", "", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if session.UserID == existing.ID {
		t.Fatal("missing-email OIDC user must not merge into existing user")
	}
	if len(tx.users) != 2 {
		t.Fatalf("should create new user (not merge), got %d users", len(tx.users))
	}
}

func TestBindIdentitySuccess(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	actor, _ := model.NewUser("actor@example.com", "Actor", "$argon2id$h")
	tx.users[actor.ID] = actor
	tx.usersByEmail[actor.Email] = actor

	err := svc.BindIdentity(ctx, actor.ID, validProfile("sub-bind", "actor@example.com", true))
	if err != nil {
		t.Fatalf("BindIdentity error: %v", err)
	}
	if len(tx.identities) != 1 || tx.identities[0].UserID != actor.ID {
		t.Fatalf("identity should attach to actor")
	}
}

func TestBindIdentityConflictWhenSSOBoundToOther(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	other, _ := model.NewUser("other@example.com", "Other", "$argon2id$h")
	tx.users[other.ID] = other
	tx.usersByEmail[other.Email] = other
	identity, _ := model.NewExternalIdentity(other.ID, "https://sso.example.com", "sub-taken", "other@example.com", true, "{}")
	tx.identities = append(tx.identities, identity)

	actor, _ := model.NewUser("actor@example.com", "Actor", "$argon2id$h")
	tx.users[actor.ID] = actor
	tx.usersByEmail[actor.Email] = actor

	err := svc.BindIdentity(ctx, actor.ID, validProfile("sub-taken", "other@example.com", true))
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestBindIdentityIdempotentWhenBoundToSelf(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	actor, _ := model.NewUser("actor@example.com", "Actor", "$argon2id$h")
	tx.users[actor.ID] = actor
	tx.usersByEmail[actor.Email] = actor
	identity, _ := model.NewExternalIdentity(actor.ID, "https://sso.example.com", "sub-self", "actor@example.com", true, "{}")
	tx.identities = append(tx.identities, identity)

	err := svc.BindIdentity(ctx, actor.ID, validProfile("sub-self", "actor@example.com", true))
	if err != nil {
		t.Fatalf("idempotent bind should succeed, got %v", err)
	}
	if len(tx.identities) != 1 {
		t.Fatalf("should not create duplicate identity")
	}
}

func TestBeginLoginSanitizesNextPath(t *testing.T) {
	svc, _, _, _, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()

	for _, bad := range []string{"//evil.com", "https://evil.com/", "\\evil"} {
		_, _, _, err := svc.BeginLogin(ctx, bad, "", uuid.Nil, uuid.Nil)
		if err == nil {
			t.Fatalf("next %q should be rejected", bad)
		}
	}
	// 合法路径。
	_, _, _, err := svc.BeginLogin(ctx, "/dashboard", "", uuid.Nil, uuid.Nil)
	if err != nil {
		t.Fatalf("valid next rejected: %v", err)
	}
	// 空路径默认 /。
	_, _, _, err = svc.BeginLogin(ctx, "", "", uuid.Nil, uuid.Nil)
	if err != nil {
		t.Fatalf("empty next rejected: %v", err)
	}
}

func TestBeginLoginStoresInvitationTokenHash(t *testing.T) {
	svc, _, store, _, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	_, _, state, err := svc.BeginLogin(ctx, "/", "plaintext-invite-token", uuid.Nil, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := store.issued[state]
	if payload.InvitationTokenHash == "" || strings.Contains(payload.InvitationTokenHash, "plaintext") {
		t.Fatalf("invitation token should be stored as hash, got %q", payload.InvitationTokenHash)
	}
	if payload.InvitationTokenHash != sha256HexString("plaintext-invite-token") {
		t.Fatalf("invitation token hash mismatch")
	}
}

func TestConsumeAndExchangeValidatesProfile(t *testing.T) {
	// state store 预置一个 payload，profile 缺 sub。
	prov := &fakeOIDCProvider{authURL: "x", profile: &authport.OIDCProfile{Email: "a@b.com"}}
	store := newFakeStateStore()
	tx := newFakeAuthTx()
	svc := NewOIDCLoginService(prov, store, tx, &fakeIdentityReader{},
		config.SessionConfig{LifetimeSeconds: 3600},
		config.OIDCConfig{Issuer: "https://sso.example.com"},
		nil,
	)

	// 手动 Issue 一个 payload 拿到 state。
	state, _ := store.Issue(context.Background(), authport.OIDCStatePayload{BrowserNonce: "bn", OIDCNonce: "on", PKCEVerifier: "pv"})

	_, _, err := svc.ConsumeAndExchange(context.Background(), "code", state, "bn")
	if err == nil {
		t.Fatal("should reject profile missing sub")
	}
}

func TestListIdentities(t *testing.T) {
	svc, _, _, _, reader := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()
	uid := uuid.New()
	reader.list = []*model.ExternalIdentity{
		{UserID: uid, Issuer: "https://sso.example.com", Subject: "s1"},
		{UserID: uuid.New(), Issuer: "https://sso.example.com", Subject: "s2"},
	}
	got, err := svc.ListIdentities(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].UserID != uid {
		t.Fatalf("should return only identities for uid, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// InvitationService.AcceptOIDC（复用 fakeAuthTx）
// ---------------------------------------------------------------------------

func newTestInvitationServiceForOIDC(t *testing.T, passwordEnabled bool) (*InvitationService, *fakeAuthTx) {
	t.Helper()
	invRepo := newFakeInvitationRepository()
	wsRepo := newFakeWorkspaceRepository()
	userRepo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	cfg := testAuthConfig()
	cfg.Password.Enabled = passwordEnabled
	cfg.OIDC.Issuer = "https://sso.example.com"
	svc := NewInvitationService(invRepo, wsRepo, userRepo, hasher, cfg)
	tx := newFakeAuthTx()
	svc.WithOIDCAuthTx(tx)
	return svc, tx
}

func seedPendingInvitationInTx(tx *fakeAuthTx, email string, role value.WorkspaceRole) string {
	inv := &model.Invitation{
		ID:           uuid.New(),
		WorkspaceID:  uuid.New(),
		InvitedEmail: strings.ToLower(strings.TrimSpace(email)),
		Role:         role,
	}
	tokenHash := sha256HexString("invite-token-" + email)
	tx.invitations[tokenHash] = inv
	return tokenHash
}

func TestAcceptOIDCEmailMismatchRejects(t *testing.T) {
	svc, tx := newTestInvitationServiceForOIDC(t, false)
	tokenHash := seedPendingInvitationInTx(tx, "invited@example.com", value.RoleMember)
	ctx := context.Background()

	_, err := svc.AcceptOIDC(ctx, tokenHash, validProfile("sub-1", "other@example.com", true), "ua", "1.2.3.4")
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for email mismatch, got %v", err)
	}
}

func TestAcceptOIDCInvalidTokenHashRejects(t *testing.T) {
	svc, _ := newTestInvitationServiceForOIDC(t, false)
	ctx := context.Background()
	_, err := svc.AcceptOIDC(ctx, "not-a-hash", validProfile("sub-1", "a@b.com", true), "ua", "1.2.3.4")
	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for invalid hash, got %v", err)
	}
}

func TestAcceptOIDCSuccessCreatesUserMembershipSession(t *testing.T) {
	svc, tx := newTestInvitationServiceForOIDC(t, false)
	tokenHash := seedPendingInvitationInTx(tx, "invited@example.com", value.RoleMember)
	ctx := context.Background()

	session, err := svc.AcceptOIDC(ctx, tokenHash, validProfile("sub-1", "invited@example.com", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("AcceptOIDC error: %v", err)
	}
	if session == nil {
		t.Fatal("session should be created")
	}
	if len(tx.users) != 1 {
		t.Fatalf("should create 1 user, got %d", len(tx.users))
	}
	if len(tx.memberships) != 1 {
		t.Fatalf("should create 1 membership, got %d", len(tx.memberships))
	}
	if len(tx.identities) != 1 {
		t.Fatalf("should create 1 identity, got %d", len(tx.identities))
	}
	// invitation 应标记已接受。
	for _, inv := range tx.invitations {
		if inv.AcceptedAt == nil {
			t.Fatal("invitation should be marked accepted")
		}
	}
}

func TestAcceptOIDCInvitationNotFoundRejects(t *testing.T) {
	svc, _ := newTestInvitationServiceForOIDC(t, false)
	ctx := context.Background()
	// 合法格式但不存在。
	tokenHash := sha256HexString("nonexistent-token")
	_, err := svc.AcceptOIDC(ctx, tokenHash, validProfile("sub-1", "invited@example.com", true), "ua", "1.2.3.4")
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAcceptPasswordDisabledRejects(t *testing.T) {
	// password.enabled=false 时，password Accept 路径应拒绝。
	invRepo := newFakeInvitationRepository()
	wsRepo := newFakeWorkspaceRepository()
	userRepo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	cfg := testAuthConfig()
	cfg.Password.Enabled = false
	svc := NewInvitationService(invRepo, wsRepo, userRepo, hasher, cfg)

	_, err := svc.Accept(context.Background(), "any-token", "a@b.com", "nick", "pw", "ua", "1.2.3.4")
	if !errors.Is(err, domainerrors.ErrPasswordRegistrationDisabled) {
		t.Fatalf("expected ErrPasswordRegistrationDisabled, got %v", err)
	}
}

func TestAcceptOIDCWithoutEmailDoesNotCreateMembership(t *testing.T) {
	// IdP 不返回 email 时，接受邀请只建 user+identity+session，
	// 不建 membership、不标记 accepted（待补齐 email 后 CompleteInvitationAccept）。
	svc, tx := newTestInvitationServiceForOIDC(t, false)
	tokenHash := seedPendingInvitationInTx(tx, "invited@example.com", value.RoleMember)
	ctx := context.Background()

	session, err := svc.AcceptOIDC(ctx, tokenHash, validProfile("sub-1", "", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("AcceptOIDC error: %v", err)
	}
	if session == nil {
		t.Fatal("session should be created")
	}
	if len(tx.users) != 1 {
		t.Fatalf("should create 1 user, got %d", len(tx.users))
	}
	if len(tx.memberships) != 0 {
		t.Fatalf("no membership should be created without email, got %d", len(tx.memberships))
	}
	for _, inv := range tx.invitations {
		if inv.AcceptedAt != nil {
			t.Fatal("invitation should NOT be marked accepted without email")
		}
	}
}

func TestCompleteInvitationAcceptSuccess(t *testing.T) {
	// 用户先无 email 接受邀请（不建 membership），补齐 email 匹配邀请后完成接受。
	svc, tx := newTestInvitationServiceForOIDC(t, false)
	tokenHash := seedPendingInvitationInTx(tx, "invited@example.com", value.RoleMember)
	ctx := context.Background()

	session, err := svc.AcceptOIDC(ctx, tokenHash, validProfile("sub-1", "", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("AcceptOIDC error: %v", err)
	}
	if len(tx.memberships) != 0 {
		t.Fatalf("precondition: no membership yet, got %d", len(tx.memberships))
	}

	// 补齐 email。
	user := tx.users[session.UserID]
	user.Email = "invited@example.com"

	if err := svc.CompleteInvitationAccept(ctx, tokenHash, session.UserID); err != nil {
		t.Fatalf("CompleteInvitationAccept error: %v", err)
	}
	if len(tx.memberships) != 1 {
		t.Fatalf("should create 1 membership after email completion, got %d", len(tx.memberships))
	}
	for _, inv := range tx.invitations {
		if inv.AcceptedAt == nil {
			t.Fatal("invitation should be marked accepted")
		}
	}
}

func TestCompleteInvitationAcceptEmailMismatch(t *testing.T) {
	svc, tx := newTestInvitationServiceForOIDC(t, false)
	tokenHash := seedPendingInvitationInTx(tx, "invited@example.com", value.RoleMember)
	ctx := context.Background()

	session, err := svc.AcceptOIDC(ctx, tokenHash, validProfile("sub-1", "", true), "ua", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	// 补齐一个与邀请不匹配的 email。
	tx.users[session.UserID].Email = "someone-else@example.com"

	err = svc.CompleteInvitationAccept(ctx, tokenHash, session.UserID)
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for email mismatch, got %v", err)
	}
	if len(tx.memberships) != 0 {
		t.Fatalf("no membership on email mismatch, got %d", len(tx.memberships))
	}
}

func TestNeedsEmailCompletion(t *testing.T) {
	svc, _, _, tx, _ := newTestOIDCLoginService(t, "https://sso.example.com", false)
	ctx := context.Background()

	withEmail, _ := model.NewProvisionalUser("a@b.com", "A")
	tx.users[withEmail.ID] = withEmail
	withoutEmail, _ := model.NewProvisionalUser("", "B")
	tx.users[withoutEmail.ID] = withoutEmail

	needs, err := svc.NeedsEmailCompletion(ctx, withEmail.ID)
	if err != nil || needs {
		t.Fatalf("user with email: needs=%v err=%v, want needs=false", needs, err)
	}
	needs, err = svc.NeedsEmailCompletion(ctx, withoutEmail.ID)
	if err != nil || !needs {
		t.Fatalf("user without email: needs=%v err=%v, want needs=true", needs, err)
	}
}

// 编译期引用 value 包避免未使用告警（invitation 测试复用）。
var _ = value.RoleAdmin
