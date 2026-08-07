package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// OIDCAuthTxRunner 由 application 定义、infrastructure/db 实现。
// 业务分支留在 service，runner 只建立事务并提供 tx-bound 薄持久化操作。
// 所有会建新 user 的路径（password 首注册、OIDC JIT、OIDC 邀请接受新建 user）
// 必须在事务内先 AcquireBootstrapLock 再 CountUsers，保证 bootstrap 首管理员唯一性。
type OIDCAuthTxRunner interface {
	WithinOIDCAuth(ctx context.Context, fn func(tx OIDCAuthTx) error) error
}

// OIDCAuthTx 是事务内的薄持久化接口。实现层把 gorm.ErrRecordNotFound 映射为
// domainerrors.ErrNotFound，其余错误用 fmt.Errorf 包装。
type OIDCAuthTx interface {
	// AcquireBootstrapLock 获取 bootstrap advisory transaction lock。
	// 所有建 user 路径在 CountUsers 前必须调用，保证首管理员判定原子。
	AcquireBootstrapLock(ctx context.Context) error
	CountUsers(ctx context.Context) (int64, error)
	FindIdentityByIssuerSubject(ctx context.Context, issuer, subject string) (*model.ExternalIdentity, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	FindUserByEmail(ctx context.Context, email string) (*model.User, error)
	CreateUser(ctx context.Context, user *model.User) error
	CreateIdentity(ctx context.Context, identity *model.ExternalIdentity) error
	UpdateIdentityAuth(ctx context.Context, identity *model.ExternalIdentity, rawProfile string) error
	CreateSession(ctx context.Context, session *model.Session) error
	// TouchLastLogin 是 best-effort 更新，失败不回滚认证。
	TouchLastLogin(ctx context.Context, userID uuid.UUID) error
	FindActiveSession(ctx context.Context, sessionID uuid.UUID) (*model.Session, error)
	FindPendingInvitationForUpdate(ctx context.Context, tokenHash string) (*model.Invitation, error)
	CreateMembership(ctx context.Context, membership *model.Membership) error
	MarkInvitationAccepted(ctx context.Context, invitationID, userID uuid.UUID) error
	// CountWorkspaces 统计平台 workspace 总数（单租户自动加入用）。
	CountWorkspaces(ctx context.Context) (int64, error)
	// GetFirstWorkspace 返回创建最早的 workspace（单租户自动加入的目标）。
	GetFirstWorkspace(ctx context.Context) (*model.Workspace, error)
}

// ExternalIdentityReader 是事务外的只读身份查询，用于账号设置页展示。
type ExternalIdentityReader interface {
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]*model.ExternalIdentity, error)
}

// OIDCLoginService 负责 OIDC 登录发起、回调消费、JIT 建号/合并/绑定。
// 不持有 *gorm.DB；事务通过 OIDCAuthTxRunner 注入。
type OIDCLoginService struct {
	provider             authport.OIDCProvider
	stateStore           authport.OIDCStateStore
	authTx               OIDCAuthTxRunner
	identityReader       ExternalIdentityReader
	sessionLife          int // seconds
	issuer               string
	requireEmailVerified bool
	singleTenant         bool         // OIDC 开启时平台为单租户：新用户登录后自动加入唯一 workspace
	log                  *slog.Logger // 可 nil；仅记脱敏字段
}

// NewOIDCLoginService 构造 OIDCLoginService。
// log 可传 nil；服务只记录脱敏字段（不记 sub/email/id_token/access_token）。
func NewOIDCLoginService(
	provider authport.OIDCProvider,
	stateStore authport.OIDCStateStore,
	authTx OIDCAuthTxRunner,
	identityReader ExternalIdentityReader,
	sessionCfg config.SessionConfig,
	oidcCfg config.OIDCConfig,
	singleTenant bool,
	log *slog.Logger,
) *OIDCLoginService {
	return &OIDCLoginService{
		provider:             provider,
		stateStore:           stateStore,
		authTx:               authTx,
		identityReader:       identityReader,
		sessionLife:          sessionCfg.LifetimeSeconds,
		issuer:               strings.TrimSpace(oidcCfg.Issuer),
		requireEmailVerified: oidcCfg.RequireEmailVerified,
		singleTenant:         singleTenant,
		log:                  log,
	}
}

// sanitizeNextPath 校验登录后跳转路径：必须是无 scheme/host 的站内绝对路径，
// 拒绝 //（开放重定向）、反斜杠、控制字符、可编码为绝对 URL 的值。
func sanitizeNextPath(next string) (string, error) {
	next = strings.TrimSpace(next)
	if next == "" {
		return "/", nil
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "", fmt.Errorf("%w: next 路径非法", domainerrors.ErrValidation)
	}
	if strings.ContainsAny(next, "\\\x00\r\n\t") {
		return "", fmt.Errorf("%w: next 路径含非法字符", domainerrors.ErrValidation)
	}
	// 解析后必须仍是站内路径（Scheme/Host 为空）。
	parsed, err := url.Parse(next)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "", fmt.Errorf("%w: next 路径解析为绝对 URL", domainerrors.ErrValidation)
	}
	return next, nil
}

// BeginLogin 生成 IdP 跳转 URL，返回 (authURL, browserNonce, state)。
// invitationToken 非空时仅保存其 sha256 hash（明文不写 Redis）。
// actorUserID/sessionID 非 Nil 时记入 state 用于绑定流程。
func (s *OIDCLoginService) BeginLogin(ctx context.Context, next string, invitationToken string, actorUserID, sessionID uuid.UUID) (string, string, string, error) {
	cleanNext, err := sanitizeNextPath(next)
	if err != nil {
		return "", "", "", err
	}

	browserNonce, err := randomOIDCNonce()
	if err != nil {
		return "", "", "", fmt.Errorf("生成 browser nonce 失败: %w", err)
	}
	oidcNonce, err := randomOIDCNonce()
	if err != nil {
		return "", "", "", fmt.Errorf("生成 oidc nonce 失败: %w", err)
	}
	pkceVerifier, err := randomOIDCPKCEVerifier()
	if err != nil {
		return "", "", "", fmt.Errorf("生成 pkce verifier 失败: %w", err)
	}
	challenge := pkceS256Challenge(pkceVerifier)

	payload := authport.OIDCStatePayload{
		Next:         cleanNext,
		BrowserNonce: browserNonce,
		OIDCNonce:    oidcNonce,
		PKCEVerifier: pkceVerifier,
	}
	if invitationToken = strings.TrimSpace(invitationToken); invitationToken != "" {
		payload.InvitationTokenHash = sha256HexString(invitationToken)
	}
	if actorUserID != uuid.Nil {
		payload.BindActorID = actorUserID
	}
	if sessionID != uuid.Nil {
		payload.BindSessionID = sessionID
	}

	state, err := s.stateStore.Issue(ctx, payload)
	if err != nil {
		return "", "", "", err
	}
	authURL := s.provider.AuthCodeURL(state, oidcNonce, challenge)
	return authURL, browserNonce, state, nil
}

// ConsumeAndExchange 取出一次性 state 并完成 OIDC code exchange。
// 返回 (payload, profile) 供 handler 层分派到登录/邀请接受/绑定。
func (s *OIDCLoginService) ConsumeAndExchange(ctx context.Context, code, state, browserNonce string) (*authport.OIDCStatePayload, *authport.OIDCProfile, error) {
	payload, err := s.stateStore.Consume(ctx, state, browserNonce)
	if err != nil {
		return nil, nil, err
	}
	profile, err := s.provider.Exchange(ctx, code, payload.PKCEVerifier, payload.OIDCNonce)
	if err != nil {
		return nil, nil, err
	}
	if err := validateOIDCProfile(profile, s.requireEmailVerified); err != nil {
		s.logOIDCProfileError(err)
		return nil, nil, err
	}
	return payload, profile, nil
}

// logOIDCProfileError 记录脱敏的 profile 校验失败原因（不记 sub/email 明文）。
func (s *OIDCLoginService) logOIDCProfileError(err error) {
	if s.log == nil {
		return
	}
	s.log.Warn("oidc profile 校验失败",
		slog.String("reason", err.Error()),
		slog.String("issuer", s.issuer),
	)
}

// LoginOrProvision 处理常规登录/JIT 建号/email 合并（spec §6.3）。
// 返回新建的 session。
func (s *OIDCLoginService) LoginOrProvision(ctx context.Context, profile *authport.OIDCProfile, userAgent, ipAddr string) (*model.Session, error) {
	if err := validateOIDCProfile(profile, s.requireEmailVerified); err != nil {
		s.logOIDCProfileError(err)
		return nil, err
	}
	var session *model.Session
	err := s.authTx.WithinOIDCAuth(ctx, func(tx OIDCAuthTx) error {
		identity, err := tx.FindIdentityByIssuerSubject(ctx, s.issuer, profile.Subject)
		if err != nil && !errors.Is(err, domainerrors.ErrNotFound) {
			return err
		}
		var user *model.User
		if identity != nil {
			// 命中 identity：复用 user，刷新 last_auth_at / raw_profile。
			user, err = tx.FindUserByID(ctx, identity.UserID)
			if err != nil {
				return err
			}
			if err := tx.UpdateIdentityAuth(ctx, identity, profile.RawProfile); err != nil {
				return err
			}
		} else {
			// identity 未命中：email 存在时按 email 查现有 user 决定合并或 JIT；
			// email 缺失（IdP 未返回）时无法合并，直接 JIT 建无 email 用户。
			normalizedEmail := normalizeEmailLocal(profile.Email)
			if normalizedEmail != "" {
				user, err = tx.FindUserByEmail(ctx, normalizedEmail)
				if err != nil && !errors.Is(err, domainerrors.ErrNotFound) {
					return err
				}
			}
			if user == nil {
				// JIT 建号：持 bootstrap lock，count==0 授 platform_admin。
				if err := tx.AcquireBootstrapLock(ctx); err != nil {
					return err
				}
				count, err := tx.CountUsers(ctx)
				if err != nil {
					return err
				}
				nickname := deriveNickname(profile)
				user, err = model.NewProvisionalUser(normalizedEmail, nickname)
				if err != nil {
					return err
				}
				user.IsPlatformAdmin = count == 0
				if err := tx.CreateUser(ctx, user); err != nil {
					return err
				}
				// 单租户模式（OIDC 开启）：仅对本次 JIT 新建的用户自动加入唯一
				// workspace——已存在的用户（identity 复用 / email 合并）不自动
				// 加入，避免被移除的成员在重新登录时被自动恢复。
				// 平台尚无 workspace 时（bootstrap：首个用户稍后创建）跳过。
				if s.singleTenant {
					count, err := tx.CountWorkspaces(ctx)
					if err != nil {
						return err
					}
					if count > 0 {
						ws, err := tx.GetFirstWorkspace(ctx)
						if err != nil {
							return err
						}
						membership, err := model.NewMembership(ws.ID, user.ID, value.RoleMember)
						if err != nil {
							return err
						}
						if err := tx.CreateMembership(ctx, membership); err != nil {
							return err
						}
					}
				}
			}
			// 给 user 绑定 identity（合并与 JIT 都需要）。
			newIdentity, err := model.NewExternalIdentity(user.ID, s.issuer, profile.Subject, normalizedEmail, profile.EmailVerified, profile.RawProfile)
			if err != nil {
				return err
			}
			if err := tx.CreateIdentity(ctx, newIdentity); err != nil {
				return err
			}
		}

		// 单租户模式（OIDC 开启）：新用户自动加入唯一 workspace 的逻辑在 JIT
		// 建号分支内完成（仅新建用户，见上），复用/合并的老用户不自动加入。

		sess, err := model.NewSession(user.ID, lifetimeDuration(s.sessionLife), userAgent, ipAddr)
		if err != nil {
			return err
		}
		if err := tx.CreateSession(ctx, sess); err != nil {
			return err
		}
		_ = tx.TouchLastLogin(ctx, user.ID)
		session = sess
		return nil
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

// BindIdentity 已登录用户绑定 OIDC（spec §6.5）。
// 回调时必须重新认证 session 并确认 actor/session 与 state 一致。
// 不执行 email 合并、不改变 user email、不替换 session。
func (s *OIDCLoginService) BindIdentity(ctx context.Context, actorUserID uuid.UUID, profile *authport.OIDCProfile) error {
	return s.authTx.WithinOIDCAuth(ctx, func(tx OIDCAuthTx) error {
		existing, err := tx.FindIdentityByIssuerSubject(ctx, s.issuer, profile.Subject)
		if err != nil && !errors.Is(err, domainerrors.ErrNotFound) {
			return err
		}
		if existing != nil && existing.UserID != actorUserID {
			return domainerrors.ErrConflict
		}
		if existing != nil {
			// 已绑自己：幂等成功。
			return nil
		}
		identity, err := model.NewExternalIdentity(actorUserID, s.issuer, profile.Subject, normalizeEmailLocal(profile.Email), profile.EmailVerified, profile.RawProfile)
		if err != nil {
			return err
		}
		return tx.CreateIdentity(ctx, identity)
	})
}

// ListIdentities 返回当前 user 的外部身份非敏感摘要。
func (s *OIDCLoginService) ListIdentities(ctx context.Context, userID uuid.UUID) ([]*model.ExternalIdentity, error) {
	return s.identityReader.ListByUserID(ctx, userID)
}

// NeedsEmailCompletion 报告用户是否缺 email（OIDC IdP 未返回），需要补齐资料。
// 事务内只读查询用户；用户不存在返回 false（调用方按无需补齐处理）。
func (s *OIDCLoginService) NeedsEmailCompletion(ctx context.Context, userID uuid.UUID) (bool, error) {
	var needs bool
	err := s.authTx.WithinOIDCAuth(ctx, func(tx OIDCAuthTx) error {
		user, err := tx.FindUserByID(ctx, userID)
		if err != nil {
			return err
		}
		needs = strings.TrimSpace(user.Email) == ""
		return nil
	})
	if err != nil {
		return false, err
	}
	return needs, nil
}

// validateOIDCProfile 校验 profile 的 sub 与 email 策略。
//
// email 是可选字段：IdP 出于隐私（email 视为敏感字段）可能不返回 email，
// 此时允许无 email 的 OIDC 用户（JIT 建号 email 为空）。email 存在时必须格式
// 合法；requireEmailVerified=true 时 email 必须已验证。
//
// 返回的错误都包装 domainerrors.ErrUnauthorized（HTTP 层映射不变），
// 但携带脱敏原因（missing_sub / invalid_email / email_not_verified），
// 供日志定位，不泄露 sub/email 明文。
func validateOIDCProfile(profile *authport.OIDCProfile, requireEmailVerified bool) error {
	if profile == nil || strings.TrimSpace(profile.Subject) == "" {
		return fmt.Errorf("%w: oidc profile 缺 sub", domainerrors.ErrUnauthorized)
	}
	if strings.TrimSpace(profile.Email) != "" {
		if _, err := normalizeEmailService(profile.Email); err != nil {
			return fmt.Errorf("%w: oidc profile email 格式非法", domainerrors.ErrUnauthorized)
		}
		if requireEmailVerified && !profile.EmailVerified {
			return fmt.Errorf("%w: oidc profile email 未验证", domainerrors.ErrUnauthorized)
		}
	}
	return nil
}

// deriveNickname 从 profile 派生昵称（Name > PreferredUsername > Subject 截断）。
func deriveNickname(profile *authport.OIDCProfile) string {
	for _, candidate := range []string{profile.Name, profile.PreferredUsername} {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	sub := strings.TrimSpace(profile.Subject)
	if len(sub) > 32 {
		sub = sub[:32]
	}
	if sub == "" {
		return "oidc-user"
	}
	return sub
}

// normalizeEmailLocal 复用领域层的 email 规范化（trim + lower + 合法性）。
func normalizeEmailLocal(email string) string {
	normalized, err := normalizeEmailService(email)
	if err != nil {
		// validateOIDCProfile 已在上游拦截非法 email；此处兜底返回原值的 trim+lower。
		return strings.ToLower(strings.TrimSpace(email))
	}
	return normalized
}

// normalizeEmailService 是 domain/model.normalizeEmail 的应用层别名，
// 避免 service 反向依赖 domain/model 的未导出函数（通过 model.User 间接复用同一规则）。
func normalizeEmailService(email string) (string, error) {
	// 通过构造一个临时 user 校验 email，复用同一规范化逻辑而不复制代码。
	tmp, err := model.NewUser(email, "x", "$argon2id$placeholder")
	if err != nil {
		return "", err
	}
	return tmp.Email, nil
}

func lifetimeDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 604800 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

func sha256HexString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// randomOIDCNonce 生成 OIDC/browser nonce（32 字节随机数的 base64url）。
func randomOIDCNonce() (string, error) {
	return randomBase64URL(32)
}

// randomOIDCPKCEVerifier 生成 PKCE code_verifier（43-128 字符；这里用 48 字节 base64url）。
func randomOIDCPKCEVerifier() (string, error) {
	return randomBase64URL(48)
}

// pkceS256Challenge 根据 verifier 计算 S256 code_challenge（base64url(sha256(verifier))）。
func pkceS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
