package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	authport "github.com/dajee/langhuan/internal/ports/auth"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// ---------------------------------------------------------------------------
// Fakes shared by user_test.go and auth_test.go.
// 所有 fake 均不依赖真实数据库/argon2/Redis，保证测试在毫秒级完成。
// ---------------------------------------------------------------------------

// fakeUserRepository 内存实现 UserRepository 接口。
type fakeUserRepository struct {
	mu               sync.Mutex
	users            map[uuid.UUID]*model.User
	touchedLastLogin map[uuid.UUID]bool
	findByEmailCalls int
	countErr         error
	listByIDsCalls   int
	listByIDsIDs     []uuid.UUID
	resetPasswordArg struct {
		called   bool
		userID   uuid.UUID
		password string
	}
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{
		users:            make(map[uuid.UUID]*model.User),
		touchedLastLogin: make(map[uuid.UUID]bool),
	}
}

func (r *fakeUserRepository) Create(_ context.Context, user *model.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.users {
		if existing.Email == user.Email {
			return domainerrors.ErrConflict
		}
	}
	r.users[user.ID] = user
	return nil
}

func (r *fakeUserRepository) FindByEmail(_ context.Context, email string) (*model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findByEmailCalls++
	normalized := strings.ToLower(strings.TrimSpace(email))
	for _, u := range r.users {
		if u.Email == normalized {
			return u, nil
		}
	}
	return nil, domainerrors.ErrNotFound
}

func (r *fakeUserRepository) FindByID(_ context.Context, id uuid.UUID) (*model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (r *fakeUserRepository) ListByIDs(_ context.Context, ids []uuid.UUID) ([]*model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listByIDsCalls++
	r.listByIDsIDs = append([]uuid.UUID(nil), ids...)
	result := make([]*model.User, 0, len(ids))
	for _, id := range ids {
		if user, ok := r.users[id]; ok {
			result = append(result, user)
		}
	}
	return result, nil
}

func (r *fakeUserRepository) Count(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.countErr != nil {
		return 0, r.countErr
	}
	return int64(len(r.users)), nil
}

func (r *fakeUserRepository) UpdatePassword(_ context.Context, id uuid.UUID, passwordHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domainerrors.ErrNotFound
	}
	u.PasswordHash = passwordHash
	return nil
}

func (r *fakeUserRepository) UpdateEmail(_ context.Context, id uuid.UUID, email string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domainerrors.ErrNotFound
	}
	u.Email = email
	return nil
}

// ResetPassword 记录调用并更新密码哈希。会话撤销的事务语义由 db 层集成测试
// （TestAuthUserRepositoryResetPasswordIntegration）覆盖；本 fake 仅服务于
// ResetPassword 的平台管理员鉴权与正常路径测试。
func (r *fakeUserRepository) ResetPassword(_ context.Context, id uuid.UUID, passwordHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetPasswordArg.called = true
	r.resetPasswordArg.userID = id
	r.resetPasswordArg.password = passwordHash
	u, ok := r.users[id]
	if !ok {
		return domainerrors.ErrNotFound
	}
	u.PasswordHash = passwordHash
	return nil
}

func (r *fakeUserRepository) TouchLastLogin(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[id]; !ok {
		return domainerrors.ErrNotFound
	}
	r.touchedLastLogin[id] = true
	return nil
}

// fakeSessionRepository 内存实现 SessionRepository 接口。
type fakeSessionRepository struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*model.Session
	deleted  map[uuid.UUID]bool
}

func newFakeSessionRepository() *fakeSessionRepository {
	return &fakeSessionRepository{
		sessions: make(map[uuid.UUID]*model.Session),
		deleted:  make(map[uuid.UUID]bool),
	}
}

func (r *fakeSessionRepository) Create(_ context.Context, session *model.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = session
	return nil
}

func (r *fakeSessionRepository) FindActive(_ context.Context, id uuid.UUID) (*model.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok || s.RevokedAt != nil || s.ExpiresAt.Before(time.Now().UTC()) {
		return nil, domainerrors.ErrNotFound
	}
	return s, nil
}

func (r *fakeSessionRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[id]; !ok {
		return domainerrors.ErrNotFound
	}
	delete(r.sessions, id)
	r.deleted[id] = true
	return nil
}

func (r *fakeSessionRepository) DeleteAllForUser(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.sessions {
		if s.UserID == userID {
			delete(r.sessions, id)
			r.deleted[id] = true
		}
	}
	return nil
}

// fakePasswordHasher 极轻量的哈希器：Hash = "h:" + password，避免真实 argon2 开销。
type fakePasswordHasher struct {
	verifyDummyCalled bool
}

var _ authport.PasswordHasher = (*fakePasswordHasher)(nil)

func (h *fakePasswordHasher) Hash(password string) (string, error) {
	return "h:" + password, nil
}

func (h *fakePasswordHasher) Verify(encodedHash, password string) (bool, error) {
	return encodedHash == "h:"+password, nil
}

// VerifyDummy 记录被调用以供测试断言，并始终返回 nil（模拟常量时间消耗）。
func (h *fakePasswordHasher) VerifyDummy(_ string) error {
	h.verifyDummyCalled = true
	return nil
}

// fakeRateLimiter 内存限流器，可注入 block 状态并记录调用。
type fakeRateLimiter struct {
	mu            sync.Mutex
	blocked       bool
	blockErr      error
	failuresCount int
	resetCount    int
	recordFailErr error
}

func (r *fakeRateLimiter) IsBlocked(_ context.Context, _ string, _ int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blockErr != nil {
		return false, r.blockErr
	}
	return r.blocked, nil
}

func (r *fakeRateLimiter) RecordFailure(_ context.Context, _ string, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recordFailErr != nil {
		return r.recordFailErr
	}
	r.failuresCount++
	return nil
}

func (r *fakeRateLimiter) Reset(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetCount++
	return nil
}

// seedUser 在 fake repo 中放入一个已存在的用户并返回它。
func seedUser(repo *fakeUserRepository, email, nickname, password string, isAdmin bool) *model.User {
	u, err := model.NewUser(email, nickname, "h:"+password)
	if err != nil {
		panic(err)
	}
	u.IsPlatformAdmin = isAdmin
	_ = repo.Create(context.Background(), u)
	return u
}

// ---------------------------------------------------------------------------
// UserService.RegisterFirstUser
// ---------------------------------------------------------------------------

func TestUserServiceIsInitialized(t *testing.T) {
	tests := []struct {
		name string
		seed bool
		want bool
	}{
		{name: "zero users", want: false},
		{name: "existing user", seed: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeUserRepository()
			if tt.seed {
				seedUser(repo, "first@example.com", "First", "pw", true)
			}
			svc := NewUserService(repo, &fakePasswordHasher{}, true)

			got, err := svc.IsInitialized(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("IsInitialized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserServiceIsInitializedWrapsCountError(t *testing.T) {
	wantErr := errors.New("count failed")
	repo := newFakeUserRepository()
	repo.countErr = wantErr
	svc := NewUserService(repo, &fakePasswordHasher{}, true)

	_, err := svc.IsInitialized(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("IsInitialized() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestFirstUserRegisterIsPlatformAdmin(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo, &fakePasswordHasher{}, true)

	out, err := svc.RegisterFirstUser(context.Background(), "Alice@Example.COM", "Alice", "supersecret")
	if err != nil {
		t.Fatalf("RegisterFirstUser returned error: %v", err)
	}
	if !out.IsPlatformAdmin {
		t.Fatalf("first user must be platform admin; got is_platform_admin=%v", out.IsPlatformAdmin)
	}
	if out.Email != "alice@example.com" {
		t.Fatalf("email = %q, want normalized", out.Email)
	}

	// 首次注册只创建 user，绝不创建 session。
	if len(repo.users) != 1 {
		t.Fatalf("expected exactly 1 user stored, got %d", len(repo.users))
	}
	stored := repo.users[out.ID]
	if stored == nil {
		t.Fatalf("user %s not stored", out.ID)
	}
	if !stored.IsPlatformAdmin {
		t.Fatal("stored user must be platform admin")
	}
	if stored.PasswordHash != "h:supersecret" {
		t.Fatalf("password hash = %q, want hashed value", stored.PasswordHash)
	}
}

func TestFirstUserRegisterRejectsSecondFirstUser(t *testing.T) {
	repo := newFakeUserRepository()
	seedUser(repo, "first@example.com", "First", "pw", true)
	svc := NewUserService(repo, &fakePasswordHasher{}, true)

	_, err := svc.RegisterFirstUser(context.Background(), "second@example.com", "Second", "pw")
	if !errors.Is(err, domainerrors.ErrConflict) {
		t.Fatalf("expected ErrConflict when a user already exists, got %v", err)
	}
	// 第二次注册不得创建新用户。
	if len(repo.users) != 1 {
		t.Fatalf("expected exactly 1 user still, got %d", len(repo.users))
	}
}

func TestFirstUserRegisterRejectsEmptyPassword(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo, &fakePasswordHasher{}, true)

	_, err := svc.RegisterFirstUser(context.Background(), "alice@example.com", "Alice", "")
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty password, got %v", err)
	}
	if len(repo.users) != 0 {
		t.Fatalf("expected no user stored on validation failure, got %d", len(repo.users))
	}
}

func TestRegisterFirstUserRejectsWhenPasswordDisabled(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo, &fakePasswordHasher{}, false) // password.enabled=false

	_, err := svc.RegisterFirstUser(context.Background(), "first@example.com", "First", "pw")
	if !errors.Is(err, domainerrors.ErrPasswordRegistrationDisabled) {
		t.Fatalf("expected ErrPasswordRegistrationDisabled, got %v", err)
	}
	if len(repo.users) != 0 {
		t.Fatalf("no user should be created when password disabled, got %d", len(repo.users))
	}
}

// ---------------------------------------------------------------------------
// UserService.ChangePassword
// ---------------------------------------------------------------------------

func TestChangePasswordSuccess(t *testing.T) {
	repo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	svc := NewUserService(repo, hasher, true)
	user := seedUser(repo, "alice@example.com", "Alice", "old-pw", false)

	err := svc.ChangePassword(context.Background(), user.ID, "old-pw", "new-pw")
	if err != nil {
		t.Fatalf("ChangePassword error: %v", err)
	}
	if repo.users[user.ID].PasswordHash != "h:new-pw" {
		t.Fatalf("password hash = %q, want h:new-pw", repo.users[user.ID].PasswordHash)
	}
}

func TestChangePasswordRejectsWrongOldPassword(t *testing.T) {
	repo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	svc := NewUserService(repo, hasher, true)
	user := seedUser(repo, "alice@example.com", "Alice", "old-pw", false)

	err := svc.ChangePassword(context.Background(), user.ID, "wrong-old", "new-pw")
	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for wrong old password, got %v", err)
	}
}

func TestChangePasswordRejectsEmptyInputs(t *testing.T) {
	repo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	svc := NewUserService(repo, hasher, true)
	user := seedUser(repo, "alice@example.com", "Alice", "old-pw", false)

	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "empty old", old: "", new: "new-pw"},
		{name: "empty new", old: "old-pw", new: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.ChangePassword(context.Background(), user.ID, tt.old, tt.new)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

func TestChangePasswordRejectsPasswordlessAccount(t *testing.T) {
	repo := newFakeUserRepository()
	hasher := &fakePasswordHasher{}
	svc := NewUserService(repo, hasher, true)
	// 预置无密码账号（OIDC JIT）：seedUser 后清空 hash。
	user := seedUser(repo, "ada@example.com", "Ada", "dummy", false)
	repo.users[user.ID].PasswordHash = ""

	err := svc.ChangePassword(context.Background(), user.ID, "anything", "new-pw")
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for passwordless account, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UserService.UpdateProfileEmail
// ---------------------------------------------------------------------------

func TestUpdateProfileEmailSuccess(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo, &fakePasswordHasher{}, true)
	user := seedUser(repo, "ada@example.com", "Ada", "pw", false)
	// 模拟 OIDC 无 email 用户：清空 email。
	repo.users[user.ID].Email = ""

	err := svc.UpdateProfileEmail(context.Background(), user.ID, "New@Example.COM")
	if err != nil {
		t.Fatalf("UpdateProfileEmail error: %v", err)
	}
	if repo.users[user.ID].Email != "new@example.com" {
		t.Fatalf("email = %q, want normalized new@example.com", repo.users[user.ID].Email)
	}
}

func TestUpdateProfileEmailRejectsInvalid(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo, &fakePasswordHasher{}, true)
	user := seedUser(repo, "ada@example.com", "Ada", "pw", false)

	tests := []struct {
		name  string
		email string
	}{
		{name: "empty", email: ""},
		{name: "invalid", email: "not-an-email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.UpdateProfileEmail(context.Background(), user.ID, tt.email)
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// UserService.ResetPassword
// ---------------------------------------------------------------------------

func TestResetPasswordRequiresPlatformAdmin(t *testing.T) {
	repo := newFakeUserRepository()
	target := seedUser(repo, "victim@example.com", "Victim", "oldpw", false)
	svc := NewUserService(repo, &fakePasswordHasher{}, true)

	err := svc.ResetPassword(context.Background(), uuid.New(), false, target.ID, "newpw")
	if !errors.Is(err, domainerrors.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for non-admin actor, got %v", err)
	}
	// 非管理员不得修改密码。
	if repo.resetPasswordArg.called {
		t.Fatal("ResetPassword must not be called when actor is not admin")
	}
}

func TestResetPasswordAdminUpdatesPassword(t *testing.T) {
	userRepo := newFakeUserRepository()
	target := seedUser(userRepo, "victim@example.com", "Victim", "oldpw", false)

	svc := NewUserService(userRepo, &fakePasswordHasher{}, true)

	if err := svc.ResetPassword(context.Background(), uuid.New(), true, target.ID, "brand-new"); err != nil {
		t.Fatalf("ResetPassword returned error: %v", err)
	}

	// 密码已更新。
	if userRepo.users[target.ID].PasswordHash != "h:brand-new" {
		t.Fatalf("password hash = %q, want updated hash", userRepo.users[target.ID].PasswordHash)
	}
	// 事务性 ResetPassword 被调用（会话撤销的事务语义由 db 集成测试覆盖）。
	if !userRepo.resetPasswordArg.called || userRepo.resetPasswordArg.userID != target.ID {
		t.Fatalf("ResetPassword not invoked for target; called=%v id=%v", userRepo.resetPasswordArg.called, userRepo.resetPasswordArg.userID)
	}
}

// newActiveSession 构造一个未过期、未撤销的会话。
func newActiveSession(userID uuid.UUID) *model.Session {
	s, err := model.NewSession(userID, time.Hour, "ua", "1.1.1.1")
	if err != nil {
		panic(err)
	}
	return s
}
