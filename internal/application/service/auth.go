package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// invalidLoginMessage 是登录失败的统一对外消息，无论用户不存在还是密码错误
// 均返回此消息，避免通过响应差异枚举有效邮箱。
const invalidLoginMessage = "邮箱或密码不正确"

// invalidLoginError 是返回给调用方的统一登录失败错误。
// errors.Is(err, ErrUnauthorized) 成立，且 Error() 文本恒定。
var invalidLoginError = fmt.Errorf("%w: %s", domainerrors.ErrUnauthorized, invalidLoginMessage)

// AuthService 负责登录、登出与会话认证。
// 它依赖 ports/auth 的 PasswordHasher 与 RateLimiter 抽象，绝不直接依赖
// argon2 或 Redis SDK；仓储通过服务层本地接口注入。
type AuthService struct {
	userRepo        UserRepository
	sessionRepo     SessionRepository
	hasher          authport.PasswordHasher
	limiter         authport.RateLimiter
	sessionLife     time.Duration
	maxAttempts     int
	loginWindow     time.Duration
	passwordEnabled bool
}

// NewAuthService 构造 AuthService，从 config 提取会话寿命与限流参数。
func NewAuthService(
	userRepo UserRepository,
	sessionRepo SessionRepository,
	hasher authport.PasswordHasher,
	limiter authport.RateLimiter,
	cfg config.AuthConfig,
) *AuthService {
	return &AuthService{
		userRepo:        userRepo,
		sessionRepo:     sessionRepo,
		hasher:          hasher,
		limiter:         limiter,
		sessionLife:     time.Duration(cfg.Session.LifetimeSeconds) * time.Second,
		maxAttempts:     cfg.RateLimit.LoginMaxAttempts,
		loginWindow:     time.Duration(cfg.RateLimit.LoginWindowSeconds) * time.Second,
		passwordEnabled: cfg.Password.Enabled,
	}
}

// Login 校验凭据并创建会话。返回的 *model.Session 供 handler 写入 cookie。
//
// 执行顺序固定（防枚举 + 限流优先）：
//  1. 规范化 email（trim + lower）。
//  2. 限流检查：被阻断则直接返回 ErrRateLimited，绝不查询用户。
//  3. 按邮箱查找用户；未知用户执行 VerifyDummy（常量时间，防枚举时序），返回统一失败。
//  4. 校验密码；错误则记录失败计数并返回统一失败。
//  5. 成功：清零限流、创建会话、刷新最后登录时间。
//
// 未知用户与错误密码返回完全相同的错误（同哨兵、同消息），不可枚举区分。
func (s *AuthService) Login(ctx context.Context, email, password, userAgent, ipAddr string) (*model.Session, error) {
	// password.enabled=false 时直接拒绝密码登录（OIDC-first 形态）。
	if !s.passwordEnabled {
		return nil, domainerrors.ErrPasswordLoginDisabled
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))

	// 2. 限流优先：被阻断时不查询用户。
	blocked, err := s.limiter.IsBlocked(ctx, normalizedEmail, s.maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("登录限流检查失败: %w", err)
	}
	if blocked {
		return nil, domainerrors.ErrRateLimited
	}

	// 3. 查找用户。
	user, err := s.userRepo.FindByEmail(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, domainerrors.ErrNotFound) {
			// 未知用户：执行 dummy 校验以抹平时序，然后返回统一失败。
			// 不记录失败计数，避免攻击者通过限流副作用枚举有效邮箱。
			_ = s.hasher.VerifyDummy(password)
			return nil, invalidLoginError
		}
		return nil, fmt.Errorf("查找用户失败: %w", err)
	}

	// 4. 校验密码。无密码账号（OIDC JIT 建号）执行 dummy 校验后返回统一失败，
	// 与未知用户行为一致，防枚举。
	if !user.HasPassword() {
		_ = s.hasher.VerifyDummy(password)
		return nil, invalidLoginError
	}
	ok, err := s.hasher.Verify(user.PasswordHash, password)
	if err != nil {
		return nil, fmt.Errorf("密码校验失败: %w", err)
	}
	if !ok {
		// 错误密码：记录失败计数，返回与未知用户完全一致的失败错误。
		if rerr := s.limiter.RecordFailure(ctx, normalizedEmail, s.loginWindow); rerr != nil {
			return nil, fmt.Errorf("记录登录失败失败: %w", rerr)
		}
		return nil, invalidLoginError
	}

	// 5. 成功：清零限流、创建会话、刷新最后登录时间。
	if rerr := s.limiter.Reset(ctx, normalizedEmail); rerr != nil {
		return nil, fmt.Errorf("清零登录限流失败: %w", rerr)
	}

	session, err := model.NewSession(user.ID, s.sessionLife, userAgent, ipAddr)
	if err != nil {
		return nil, err
	}
	if cerr := s.sessionRepo.Create(ctx, session); cerr != nil {
		return nil, cerr
	}
	// TouchLastLogin 是尽力而为的审计字段更新：会话已创建（认证态已确立），
	// 其失败不得使登录失败并返回 500（否则用户拿不到 cookie 却留下有效会话）。
	_ = s.userRepo.TouchLastLogin(ctx, user.ID)
	return session, nil
}

// Logout 删除指定会话。会话不存在时返回 ErrNotFound（调用方可据此判断幂等性）。
func (s *AuthService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.sessionRepo.Delete(ctx, sessionID)
}

// Authenticate 通过会话 ID 解析出当前用户。
// 仅接受未撤销且未过期的活动会话；会话或用户不存在均返回 ErrUnauthorized，
// 不向调用方区分原因以避免信息泄漏。
func (s *AuthService) Authenticate(ctx context.Context, sessionID uuid.UUID) (*model.User, error) {
	session, err := s.sessionRepo.FindActive(ctx, sessionID)
	if err != nil {
		if errors.Is(err, domainerrors.ErrNotFound) {
			return nil, domainerrors.ErrUnauthorized
		}
		return nil, fmt.Errorf("查找会话失败: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, domainerrors.ErrNotFound) {
			return nil, domainerrors.ErrUnauthorized
		}
		return nil, fmt.Errorf("查找用户失败: %w", err)
	}
	return user, nil
}
