package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// ---------------------------------------------------------------------------
// Fakes for InvitationRepository
// ---------------------------------------------------------------------------

type fakeInvitationRepository struct {
	mu          sync.Mutex
	byID        map[uuid.UUID]*model.Invitation
	byHash      map[string]*model.Invitation
	createErr   error
	acceptErr   error
	revokeErr   error
	acceptCalls int
	acceptLast  *acceptArgs
	revokeCalls int
}

type acceptArgs struct {
	invitation *model.Invitation
	user       *model.User
	membership *model.Membership
	session    *model.Session
}

func newFakeInvitationRepository() *fakeInvitationRepository {
	return &fakeInvitationRepository{
		byID:   make(map[uuid.UUID]*model.Invitation),
		byHash: make(map[string]*model.Invitation),
	}
}

func (r *fakeInvitationRepository) Create(_ context.Context, invitation *model.Invitation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	// 模拟 (workspace_id, invited_email) 唯一约束：同一 workspace 已存在该 email 的待处理邀请则冲突。
	for _, existing := range r.byID {
		if existing.WorkspaceID == invitation.WorkspaceID && existing.InvitedEmail == invitation.InvitedEmail {
			return domainerrors.ErrConflict
		}
	}
	store := *invitation
	r.byID[invitation.ID] = &store
	if invitation.TokenHash != "" {
		r.byHash[invitation.TokenHash] = &store
	}
	return nil
}

func (r *fakeInvitationRepository) FindByID(_ context.Context, id uuid.UUID) (*model.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inv, ok := r.byID[id]; ok {
		out := *inv
		return &out, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (r *fakeInvitationRepository) ListByWorkspace(_ context.Context, workspaceID uuid.UUID) ([]*model.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*model.Invitation, 0)
	for _, invitation := range r.byID {
		if invitation.WorkspaceID == workspaceID {
			copy := *invitation
			result = append(result, &copy)
		}
	}
	return result, nil
}

// FindPendingByTokenHash 仅返回仍待处理（未接受/未撤销/未过期）的邀请。
func (r *fakeInvitationRepository) FindPendingByTokenHash(_ context.Context, tokenHash string) (*model.Invitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inv, ok := r.byHash[tokenHash]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	now := time.Now().UTC()
	if !inv.IsPending(now) {
		return nil, domainerrors.ErrNotFound
	}
	out := *inv
	return &out, nil
}

func (r *fakeInvitationRepository) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revokeErr != nil {
		return r.revokeErr
	}
	r.revokeCalls++
	inv, ok := r.byID[id]
	if !ok || inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return domainerrors.ErrNotFound
	}
	now := time.Now().UTC()
	inv.RevokedAt = &now
	return nil
}

// MarkAccepted 将仍待处理的邀请标记为已接受；终态邀请返回 ErrConflict（不可重复接受）。
func (r *fakeInvitationRepository) MarkAccepted(_ context.Context, id, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	inv, ok := r.byID[id]
	if !ok || inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return domainerrors.ErrConflict
	}
	now := time.Now().UTC()
	inv.AcceptedAt = &now
	inv.AcceptedUserID = userID
	return nil
}

// AcceptRegistration 模拟事务：校验邀请仍待处理，记录 user/membership/session 并标记已接受。
// 已接受的邀请返回 ErrConflict（不可复用）。
func (r *fakeInvitationRepository) AcceptRegistration(_ context.Context, invitation *model.Invitation, user *model.User, membership *model.Membership, session *model.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acceptCalls++
	r.acceptLast = &acceptArgs{invitation, user, membership, session}
	if r.acceptErr != nil {
		return r.acceptErr
	}
	inv, ok := r.byID[invitation.ID]
	if !ok || inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return domainerrors.ErrConflict
	}
	now := time.Now().UTC()
	inv.AcceptedAt = &now
	inv.AcceptedUserID = user.ID
	return nil
}

// seedInvitation 写入一条邀请（不经过 Create，便于设置 token/hash/状态）。
func seedInvitation(repo *fakeInvitationRepository, inv *model.Invitation) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	store := *inv
	repo.byID[inv.ID] = &store
	if inv.TokenHash != "" {
		repo.byHash[inv.TokenHash] = &store
	}
}

// newTestInvitationService 构造 InvitationService 并注入全部 fake 依赖。
func newTestInvitationService() (*InvitationService, *fakeInvitationRepository, *fakeWorkspaceRepository, *fakeUserRepository, *fakePasswordHasher) {
	invRepo := newFakeInvitationRepository()
	wsRepo := newFakeWorkspaceRepository()
	userRepo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	svc := NewInvitationService(invRepo, wsRepo, userRepo, hasher, testAuthConfig())
	return svc, invRepo, wsRepo, userRepo, hasher
}

// hashToken 复刻服务层的 token hash 算法（sha256 hex），供测试构造/校验。
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// InvitationService.Create
// ---------------------------------------------------------------------------

func TestInvitationListComputesStatusAndOrdersPendingFirst(t *testing.T) {
	svc, repo, _, _, _ := newTestInvitationService()
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	workspaceID := uuid.New()
	creatorID := uuid.New()

	newInvitation := func(id uuid.UUID, createdAt time.Time) *model.Invitation {
		t.Helper()
		invitation, err := model.NewInvitation(workspaceID, id.String()+"@example.com", value.RoleMember, creatorID)
		if err != nil {
			t.Fatal(err)
		}
		invitation.ID = id
		invitation.CreatedAt = createdAt
		invitation.ExpiresAt = now.Add(time.Hour)
		invitation.TokenHash = "secret-" + id.String()
		invitation.TokenPrefix = id.String()[:8]
		return invitation
	}
	pending := newInvitation(uuid.MustParse("10000000-0000-0000-0000-000000000001"), now.Add(-4*time.Hour))
	expired := newInvitation(uuid.MustParse("20000000-0000-0000-0000-000000000002"), now.Add(-3*time.Hour))
	expired.ExpiresAt = now.Add(-time.Minute)
	revoked := newInvitation(uuid.MustParse("30000000-0000-0000-0000-000000000003"), now.Add(-2*time.Hour))
	revokedAt := now.Add(-time.Minute)
	revoked.RevokedAt = &revokedAt
	revoked.ExpiresAt = now.Add(-time.Minute)
	accepted := newInvitation(uuid.MustParse("40000000-0000-0000-0000-000000000004"), now.Add(-time.Hour))
	acceptedAt := now.Add(-30 * time.Minute)
	accepted.AcceptedAt = &acceptedAt
	accepted.RevokedAt = &revokedAt
	accepted.ExpiresAt = now.Add(-time.Minute)
	for _, invitation := range []*model.Invitation{pending, expired, revoked, accepted} {
		seedInvitation(repo, invitation)
	}

	got, err := svc.List(context.Background(), workspaceID, value.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []uuid.UUID{pending.ID, accepted.ID, revoked.ID, expired.ID}
	wantStatuses := []dto.InvitationStatus{
		dto.InvitationStatusPending,
		dto.InvitationStatusAccepted,
		dto.InvitationStatusRevoked,
		dto.InvitationStatusExpired,
	}
	for i := range wantIDs {
		if got[i].ID != wantIDs[i] || got[i].Status != wantStatuses[i] {
			t.Fatalf("List()[%d] = %#v", i, got[i])
		}
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "token_hash") || strings.Contains(string(raw), "secret-") {
		t.Fatalf("management list leaked token hash: %s", raw)
	}
}

func TestInvitationListRequiresAdmin(t *testing.T) {
	svc, _, _, _, _ := newTestInvitationService()
	_, err := svc.List(context.Background(), uuid.New(), value.RoleMember)
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("List() error = %v, want ErrForbidden", err)
	}
}

func TestInvitationCreateRequiresAdmin(t *testing.T) {
	svc, _, _, _, _ := newTestInvitationService()
	_, _, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID:  uuid.New(),
		InvitedEmail: "new@example.com",
		Role:         value.RoleMember,
		CreatedBy:    uuid.New(),
		ActorRole:    value.RoleMember,
	})
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestInvitationCreateAdminCannotInviteOwner(t *testing.T) {
	svc, _, _, _, _ := newTestInvitationService()
	_, _, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID:  uuid.New(),
		InvitedEmail: "new@example.com",
		Role:         value.RoleOwner,
		CreatedBy:    uuid.New(),
		ActorRole:    value.RoleAdmin,
	})
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden (admin cannot invite owner)", err)
	}
}

func TestInvitationCreateOwnerCanInviteOwner(t *testing.T) {
	svc, repo, _, _, _ := newTestInvitationService()
	ws := uuid.New()
	dto, token, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID:  ws,
		InvitedEmail: "new@example.com",
		Role:         value.RoleOwner,
		CreatedBy:    uuid.New(),
		ActorRole:    value.RoleOwner,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if dto.Role != value.RoleOwner {
		t.Fatalf("role = %v, want owner", dto.Role)
	}
	if token == "" {
		t.Fatal("plaintext token must be returned")
	}
	// 仓储内有一条记录。
	if len(repo.byID) != 1 {
		t.Fatalf("expected 1 invitation stored, got %d", len(repo.byID))
	}
}

func TestInvitationCreateRejectsDuplicatePendingOrMember(t *testing.T) {
	svc, repo, _, _, _ := newTestInvitationService()
	ws := uuid.New()
	if _, _, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID: ws, InvitedEmail: "dup@example.com", Role: value.RoleMember,
		CreatedBy: uuid.New(), ActorRole: value.RoleOwner,
	}); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}
	// 第二次：同 workspace + email（重复 pending）→ 冲突。
	_, _, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID: ws, InvitedEmail: "dup@example.com", Role: value.RoleMember,
		CreatedBy: uuid.New(), ActorRole: value.RoleOwner,
	})
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(repo.byID) != 1 {
		t.Fatalf("expected only 1 invitation after conflict, got %d", len(repo.byID))
	}
}

func TestInvitationCreateGeneratesHashAndPrefix(t *testing.T) {
	svc, repo, _, _, _ := newTestInvitationService()
	ws := uuid.New()
	dto, token, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID: ws, InvitedEmail: "new@example.com", Role: value.RoleAdmin,
		CreatedBy: uuid.New(), ActorRole: value.RoleOwner,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	// 明文 token base64url 解码为 32 字节。
	decoded, derr := base64.RawURLEncoding.DecodeString(token)
	if derr != nil {
		t.Fatalf("token is not valid base64url: %v", derr)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded token length = %d, want 32", len(decoded))
	}

	// token_prefix 为明文 token 前 8 字符。
	if dto.TokenPrefix != token[:8] {
		t.Fatalf("token_prefix = %q, want first 8 of token %q", dto.TokenPrefix, token[:8])
	}

	// 存储的 token_hash == sha256(token) hex（64 字符）。
	stored := repo.byID[dto.ID]
	if len(stored.TokenHash) != 64 {
		t.Fatalf("stored token_hash length = %d, want 64", len(stored.TokenHash))
	}
	if stored.TokenHash != hashToken(token) {
		t.Fatalf("stored token_hash = %q, want sha256(token) hex = %q", stored.TokenHash, hashToken(token))
	}

	// expires_at 在未来。
	if !dto.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("expires_at = %v, must be in the future", dto.ExpiresAt)
	}
}

func TestInvitationCreateNormalizesEmail(t *testing.T) {
	svc, repo, _, _, _ := newTestInvitationService()
	dto, _, err := svc.Create(context.Background(), CreateInvitationInput{
		WorkspaceID: uuid.New(), InvitedEmail: "  NEW@Example.COM  ", Role: value.RoleMember,
		CreatedBy: uuid.New(), ActorRole: value.RoleOwner,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if dto.InvitedEmail != "new@example.com" {
		t.Fatalf("invited_email = %q, want normalized", dto.InvitedEmail)
	}
	stored := repo.byID[dto.ID]
	if stored.InvitedEmail != "new@example.com" {
		t.Fatalf("stored invited_email = %q, want normalized", stored.InvitedEmail)
	}
}

// ---------------------------------------------------------------------------
// InvitationService.GetPublic
// ---------------------------------------------------------------------------

func TestInvitationGetPublicRejectsInvalidStates(t *testing.T) {
	_, _, wsRepo, _, _ := newTestInvitationService()
	ws, _ := model.NewWorkspace("Acme", "acme", nil)
	_ = wsRepo.Create(context.Background(), ws)

	// 构造一个 token 与 hash，然后复刻不同状态。
	plaintext := "tok-state-test"
	hash := hashToken(plaintext)

	cases := []struct {
		name   string
		mutate func(inv *model.Invitation)
	}{
		{"accepted", func(inv *model.Invitation) { t := time.Now().UTC(); inv.AcceptedAt = &t }},
		{"revoked", func(inv *model.Invitation) { t := time.Now().UTC(); inv.RevokedAt = &t }},
		{"expired", func(inv *model.Invitation) { inv.ExpiresAt = time.Now().UTC().Add(-time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo2 := newFakeInvitationRepository()
			svc2 := NewInvitationService(repo2, wsRepo, newFakeUserRepository(), &fakePasswordHasher{}, testAuthConfig())
			inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, uuid.New())
			if err != nil {
				t.Fatal(err)
			}
			inv.TokenHash = hash
			inv.TokenPrefix = plaintext[:8]
			tc.mutate(inv)
			seedInvitation(repo2, inv)

			_, err = svc2.GetPublic(context.Background(), plaintext)
			if !errors.Is(err, domainerrors.ErrNotFound) {
				t.Fatalf("%s: err = %v, want ErrNotFound", tc.name, err)
			}
		})
	}
}

func TestInvitationGetPublicReturnsDetails(t *testing.T) {
	svc, repo, wsRepo, _, _ := newTestInvitationService()
	ws, _ := model.NewWorkspace("Acme", "acme", nil)
	_ = wsRepo.Create(context.Background(), ws)

	plaintext := "tok-public-test"
	inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleAdmin, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = hashToken(plaintext)
	inv.TokenPrefix = plaintext[:8]
	seedInvitation(repo, inv)

	got, err := svc.GetPublic(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("GetPublic returned error: %v", err)
	}
	if got.WorkspaceID != ws.ID {
		t.Fatalf("workspace_id = %s, want %s", got.WorkspaceID, ws.ID)
	}
	if got.WorkspaceName != "Acme" {
		t.Fatalf("workspace_name = %q, want Acme", got.WorkspaceName)
	}
	if got.WorkspaceSlug != "acme" {
		t.Fatalf("workspace_slug = %q, want acme", got.WorkspaceSlug)
	}
	if got.InvitedEmail != "guest@example.com" {
		t.Fatalf("invited_email = %q", got.InvitedEmail)
	}
	if got.Role != value.RoleAdmin {
		t.Fatalf("role = %v, want admin", got.Role)
	}
}

func TestInvitationGetPublicRejectsInvalidToken(t *testing.T) {
	svc, _, _, _, _ := newTestInvitationService()
	_, err := svc.GetPublic(context.Background(), "nonexistent-token")
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// InvitationService.Accept
// ---------------------------------------------------------------------------

func TestInvitationAcceptRejectsInvalidToken(t *testing.T) {
	svc, _, _, _, _ := newTestInvitationService()
	_, err := svc.Accept(context.Background(), "bogus-token", "a@example.com", "Nick", "pw", "ua", "1.1.1.1")
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestInvitationAcceptRejectsEmailMismatch(t *testing.T) {
	svc, repo, wsRepo, userRepo, _ := newTestInvitationService()
	ws, _ := model.NewWorkspace("Acme", "acme", nil)
	_ = wsRepo.Create(context.Background(), ws)

	plaintext := "tok-accept-mismatch"
	inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = hashToken(plaintext)
	inv.TokenPrefix = plaintext[:8]
	seedInvitation(repo, inv)

	_, err = svc.Accept(context.Background(), plaintext, "attacker@example.com", "Nick", "pw", "ua", "1.1.1.1")
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	// 邮箱不匹配不得创建任何用户。
	if len(userRepo.users) != 0 {
		t.Fatalf("no user should be created on email mismatch, got %d", len(userRepo.users))
	}
	if repo.acceptCalls != 0 {
		t.Fatalf("AcceptRegistration must not run on email mismatch, got %d", repo.acceptCalls)
	}
}

func TestInvitationAcceptCreatesUserMembershipSessionAtomically(t *testing.T) {
	svc, repo, wsRepo, userRepo, hasher := newTestInvitationService()
	ws, _ := model.NewWorkspace("Acme", "acme", nil)
	_ = wsRepo.Create(context.Background(), ws)

	plaintext := "tok-accept-ok"
	inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleAdmin, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = hashToken(plaintext)
	inv.TokenPrefix = plaintext[:8]
	seedInvitation(repo, inv)

	session, err := svc.Accept(context.Background(), plaintext, "Guest@Example.com", "Guest", "supersecret", "Mozilla/5.0", "127.0.0.1")
	if err != nil {
		t.Fatalf("Accept returned error: %v", err)
	}

	// 单一事务被调用一次，携带 user/membership/session。
	if repo.acceptCalls != 1 {
		t.Fatalf("AcceptRegistration calls = %d, want 1", repo.acceptCalls)
	}
	if repo.acceptLast == nil {
		t.Fatal("AcceptRegistration args not recorded")
	}
	// 密码被哈希（fake hasher: "h:" + password）。
	if repo.acceptLast.user.PasswordHash != "h:supersecret" {
		t.Fatalf("password hash = %q, want h:supersecret", repo.acceptLast.user.PasswordHash)
	}
	// email 规范化为小写。
	if repo.acceptLast.user.Email != "guest@example.com" {
		t.Fatalf("user email = %q, want normalized", repo.acceptLast.user.Email)
	}
	// 用户不是平台管理员。
	if repo.acceptLast.user.IsPlatformAdmin {
		t.Fatal("invited user must never be platform admin")
	}
	// membership 关联同一 workspace + 用户，角色取自邀请。
	if repo.acceptLast.membership.WorkspaceID != ws.ID {
		t.Fatalf("membership workspace = %s, want %s", repo.acceptLast.membership.WorkspaceID, ws.ID)
	}
	if repo.acceptLast.membership.UserID != repo.acceptLast.user.ID {
		t.Fatalf("membership user = %s, want %s", repo.acceptLast.membership.UserID, repo.acceptLast.user.ID)
	}
	if repo.acceptLast.membership.Role != value.RoleAdmin {
		t.Fatalf("membership role = %v, want admin", repo.acceptLast.membership.Role)
	}
	// session 关联新用户。
	if session.UserID != repo.acceptLast.user.ID {
		t.Fatalf("session user = %s, want %s", session.UserID, repo.acceptLast.user.ID)
	}
	if session.UserAgent != "Mozilla/5.0" || session.IPAddr != "127.0.0.1" {
		t.Fatalf("session ua/ip not propagated: ua=%q ip=%q", session.UserAgent, session.IPAddr)
	}
	// hasher 被实际调用。
	_ = hasher
	// user 仓储未被单独调用（创建由事务完成）。
	if len(userRepo.users) != 0 {
		t.Fatalf("userRepo.Create should not be called directly; transaction creates user, got %d", len(userRepo.users))
	}
}

func TestInvitationAcceptNotReusable(t *testing.T) {
	svc, repo, wsRepo, _, _ := newTestInvitationService()
	ws, _ := model.NewWorkspace("Acme", "acme", nil)
	_ = wsRepo.Create(context.Background(), ws)

	plaintext := "tok-reuse"
	inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	inv.TokenHash = hashToken(plaintext)
	inv.TokenPrefix = plaintext[:8]
	seedInvitation(repo, inv)

	// 第二次 AcceptRegistration 模拟邀请已被接受 → 返回 ErrConflict。
	repo.acceptErr = domainerrors.ErrConflict

	_, err = svc.Accept(context.Background(), plaintext, "guest@example.com", "Guest", "pw", "ua", "1.1.1.1")
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

// ---------------------------------------------------------------------------
// InvitationService.Revoke
// ---------------------------------------------------------------------------

func TestInvitationRevokeAuthorization(t *testing.T) {
	t.Run("platform_admin_can_revoke_any", func(t *testing.T) {
		svc, repo, wsRepo, _, _ := newTestInvitationService()
		ws, _ := model.NewWorkspace("Acme", "acme", nil)
		_ = wsRepo.Create(context.Background(), ws)
		creator := uuid.New()
		inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, creator)
		if err != nil {
			t.Fatal(err)
		}
		seedInvitation(repo, inv)

		err = svc.Revoke(context.Background(), inv.ID, uuid.New(), value.RoleMember, true)
		if err != nil {
			t.Fatalf("platform admin revoke: %v", err)
		}
		if repo.revokeCalls != 1 {
			t.Fatalf("revoke calls = %d, want 1", repo.revokeCalls)
		}
	})

	t.Run("owner_can_revoke_any_in_workspace", func(t *testing.T) {
		svc, repo, wsRepo, _, _ := newTestInvitationService()
		ws, _ := model.NewWorkspace("Acme", "acme", nil)
		_ = wsRepo.Create(context.Background(), ws)
		// 邀请由别人创建。
		inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		seedInvitation(repo, inv)

		err = svc.Revoke(context.Background(), inv.ID, uuid.New(), value.RoleOwner, false)
		if err != nil {
			t.Fatalf("owner revoke: %v", err)
		}
	})

	t.Run("creator_admin_can_revoke_own", func(t *testing.T) {
		svc, repo, wsRepo, _, _ := newTestInvitationService()
		ws, _ := model.NewWorkspace("Acme", "acme", nil)
		_ = wsRepo.Create(context.Background(), ws)
		creator := uuid.New()
		inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, creator)
		if err != nil {
			t.Fatal(err)
		}
		seedInvitation(repo, inv)

		err = svc.Revoke(context.Background(), inv.ID, creator, value.RoleAdmin, false)
		if err != nil {
			t.Fatalf("creator admin revoke: %v", err)
		}
	})

	t.Run("non_creator_admin_forbidden", func(t *testing.T) {
		svc, repo, wsRepo, _, _ := newTestInvitationService()
		ws, _ := model.NewWorkspace("Acme", "acme", nil)
		_ = wsRepo.Create(context.Background(), ws)
		inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		seedInvitation(repo, inv)

		err = svc.Revoke(context.Background(), inv.ID, uuid.New(), value.RoleAdmin, false)
		if !errors.Is(err, domainerrors.ErrForbidden) {
			t.Fatalf("non-creator admin: err = %v, want ErrForbidden", err)
		}
		if repo.revokeCalls != 0 {
			t.Fatalf("revoke must not run for non-creator admin, got %d", repo.revokeCalls)
		}
	})

	t.Run("member_forbidden", func(t *testing.T) {
		svc, repo, wsRepo, _, _ := newTestInvitationService()
		ws, _ := model.NewWorkspace("Acme", "acme", nil)
		_ = wsRepo.Create(context.Background(), ws)
		creator := uuid.New()
		inv, err := model.NewInvitation(ws.ID, "guest@example.com", value.RoleMember, creator)
		if err != nil {
			t.Fatal(err)
		}
		seedInvitation(repo, inv)

		err = svc.Revoke(context.Background(), inv.ID, creator, value.RoleMember, false)
		if !errors.Is(err, domainerrors.ErrForbidden) {
			t.Fatalf("member creator: err = %v, want ErrForbidden", err)
		}
	})

	t.Run("not_found_when_missing", func(t *testing.T) {
		svc, _, _, _, _ := newTestInvitationService()
		err := svc.Revoke(context.Background(), uuid.New(), uuid.New(), value.RoleOwner, false)
		if !errors.Is(err, domainerrors.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
