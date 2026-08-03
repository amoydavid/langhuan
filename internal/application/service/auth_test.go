package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/infrastructure/config"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// testAuthConfig 返回一份测试用的 AuthConfig（短 lifetime / 小阈值）。
func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		Session: config.SessionConfig{
			CookieName:      "langhuan_session",
			LifetimeSeconds: 3600,
			SecureCookie:    true,
			Domain:          "",
		},
		RateLimit: config.RateLimitConfig{
			LoginMaxAttempts:   3,
			LoginWindowSeconds: 900,
		},
		Invitation: config.InvitationConfig{
			LifetimeSeconds: 604800,
		},
	}
}

// ---------------------------------------------------------------------------
// AuthService.Login
// ---------------------------------------------------------------------------

func TestLoginSuccessCreatesSessionAndClearsRateLimit(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	hasher := &fakePasswordHasher{}
	limiter := &fakeRateLimiter{}
	seedUser(userRepo, "alice@example.com", "Alice", "correct-pw", false)

	svc := NewAuthService(userRepo, sessRepo, hasher, limiter, testAuthConfig())

	session, err := svc.Login(context.Background(), "alice@example.com", "correct-pw", "Mozilla/5.0", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if session == nil || session.UserID == uuid.Nil {
		t.Fatalf("expected a session with user id, got %+v", session)
	}
	if session.UserAgent != "Mozilla/5.0" || session.IPAddr != "127.0.0.1" {
		t.Fatalf("session ua/ip not propagated: ua=%q ip=%q", session.UserAgent, session.IPAddr)
	}

	// 会话被持久化。
	if _, ok := sessRepo.sessions[session.ID]; !ok {
		t.Fatal("session was not persisted")
	}
	// 限流计数被清零。
	if limiter.resetCount == 0 {
		t.Fatal("limiter.Reset was not called on success")
	}
	// 最后登录时间被刷新。
	if !userRepo.touchedLastLogin[session.UserID] {
		t.Fatal("TouchLastLogin was not called on success")
	}
	// 失败计数未被增加。
	if limiter.failuresCount != 0 {
		t.Fatalf("RecordFailure should not be called on success, got %d", limiter.failuresCount)
	}
}

func TestLoginWrongPasswordRecordsFailure(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	hasher := &fakePasswordHasher{}
	limiter := &fakeRateLimiter{}
	seedUser(userRepo, "alice@example.com", "Alice", "correct-pw", false)

	svc := NewAuthService(userRepo, sessRepo, hasher, limiter, testAuthConfig())

	_, err := svc.Login(context.Background(), "alice@example.com", "WRONG", "ua", "1.1.1.1")
	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for wrong password, got %v", err)
	}
	if limiter.failuresCount != 1 {
		t.Fatalf("expected RecordFailure called once, got %d", limiter.failuresCount)
	}
	// 失败时不得创建会话。
	if len(sessRepo.sessions) != 0 {
		t.Fatalf("expected no session created on wrong password, got %d", len(sessRepo.sessions))
	}
	// 失败时不得清零限流。
	if limiter.resetCount != 0 {
		t.Fatal("Reset should not be called on wrong password")
	}
	// 失败时不得刷新最后登录。
	if len(userRepo.touchedLastLogin) != 0 {
		t.Fatal("TouchLastLogin should not be called on wrong password")
	}
}

func TestLoginUnknownUserRunsVerifyDummyAndReturnsSameError(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	hasher := &fakePasswordHasher{}
	limiter := &fakeRateLimiter{}

	svc := NewAuthService(userRepo, sessRepo, hasher, limiter, testAuthConfig())

	// 未知 email 登录。
	_, unknownErr := svc.Login(context.Background(), "ghost@example.com", "anypw", "ua", "1.1.1.1")
	if !errors.Is(unknownErr, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for unknown user, got %v", unknownErr)
	}
	if !hasher.verifyDummyCalled {
		t.Fatal("VerifyDummy must be called for unknown user to prevent enumeration timing")
	}
	// 未知用户不增加失败计数（避免基于限流副作用枚举）。
	if limiter.failuresCount != 0 {
		t.Fatalf("RecordFailure should not be called for unknown user, got %d", limiter.failuresCount)
	}

	// 同一仓库 + 已知用户但错误密码，返回相同的错误类型/消息（不可枚举区分）。
	seedUser(userRepo, "bob@example.com", "Bob", "real-pw", false)
	hasher2 := &fakePasswordHasher{}
	svc2 := NewAuthService(userRepo, sessRepo, hasher2, &fakeRateLimiter{}, testAuthConfig())
	_, wrongErr := svc2.Login(context.Background(), "bob@example.com", "WRONG", "ua", "1.1.1.1")

	// 两种失败返回完全相同的错误（同哨兵 + 同消息）。
	if !errors.Is(wrongErr, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for wrong password, got %v", wrongErr)
	}
	if unknownErr.Error() != wrongErr.Error() {
		t.Fatalf("unknown-user and wrong-password errors must be identical to avoid enumeration; got %q vs %q", unknownErr.Error(), wrongErr.Error())
	}
	if len(sessRepo.sessions) != 0 {
		t.Fatalf("no session should be created, got %d", len(sessRepo.sessions))
	}
}

func TestLoginBlockedDoesNotQueryUser(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	hasher := &fakePasswordHasher{}
	limiter := &fakeRateLimiter{blocked: true}
	// 即便用户存在，阻断时也不应查询。
	seedUser(userRepo, "alice@example.com", "Alice", "correct-pw", false)

	svc := NewAuthService(userRepo, sessRepo, hasher, limiter, testAuthConfig())

	_, err := svc.Login(context.Background(), "alice@example.com", "correct-pw", "ua", "1.1.1.1")
	if !errors.Is(err, domainerrors.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited when blocked, got %v", err)
	}
	if userRepo.findByEmailCalls != 0 {
		t.Fatalf("FindByEmail must not be called when blocked, got %d calls", userRepo.findByEmailCalls)
	}
	// 阻断时不得做哈希计算。
	if hasher.verifyDummyCalled {
		t.Fatal("VerifyDummy must not run when blocked")
	}
}

// ---------------------------------------------------------------------------
// AuthService.Logout
// ---------------------------------------------------------------------------

func TestLogoutDeletesSession(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	hasher := &fakePasswordHasher{}
	limiter := &fakeRateLimiter{}
	user := seedUser(userRepo, "alice@example.com", "Alice", "pw", false)
	sess := newActiveSession(user.ID)
	_ = sessRepo.Create(context.Background(), sess)

	svc := NewAuthService(userRepo, sessRepo, hasher, limiter, testAuthConfig())

	if err := svc.Logout(context.Background(), sess.ID); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if _, ok := sessRepo.sessions[sess.ID]; ok {
		t.Fatal("session should be deleted after logout")
	}
}

// ---------------------------------------------------------------------------
// AuthService.Authenticate
// ---------------------------------------------------------------------------

func TestAuthenticateRejectsInvalidSession(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()

	svc := NewAuthService(userRepo, sessRepo, &fakePasswordHasher{}, &fakeRateLimiter{}, testAuthConfig())

	_, err := svc.Authenticate(context.Background(), uuid.New())
	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for invalid session, got %v", err)
	}
}

func TestAuthenticateReturnsUserForActiveSession(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	user := seedUser(userRepo, "alice@example.com", "Alice", "pw", true)
	sess := newActiveSession(user.ID)
	_ = sessRepo.Create(context.Background(), sess)

	svc := NewAuthService(userRepo, sessRepo, &fakePasswordHasher{}, &fakeRateLimiter{}, testAuthConfig())

	got, err := svc.Authenticate(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("got user id %s, want %s", got.ID, user.ID)
	}
	if !got.IsPlatformAdmin {
		t.Fatal("platform admin flag should be propagated")
	}
}

func TestAuthenticateRejectsExpiredSession(t *testing.T) {
	userRepo := newFakeUserRepository()
	sessRepo := newFakeSessionRepository()
	user := seedUser(userRepo, "alice@example.com", "Alice", "pw", false)
	// 构造一个已过期的会话。
	expired := newActiveSession(user.ID)
	expired.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	_ = sessRepo.Create(context.Background(), expired)

	svc := NewAuthService(userRepo, sessRepo, &fakePasswordHasher{}, &fakeRateLimiter{}, testAuthConfig())

	_, err := svc.Authenticate(context.Background(), expired.ID)
	if !errors.Is(err, domainerrors.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for expired session, got %v", err)
	}
}
