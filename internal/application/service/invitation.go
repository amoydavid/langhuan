package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

const (
	// invitationTokenBytes 是邀请 token 的随机字节数（规格 5.3）。
	invitationTokenBytes = 32
	// invitationTokenPrefixLen 是 token_prefix 的明文长度。
	invitationTokenPrefixLen = 8
)

// InvitationRepository 描述 invitation 聚合的仓储抽象（服务层本地接口，
// 由 db.InvitationRepository 实现）。AcceptRegistration 在单一事务内完成
// user/membership/session 的创建与邀请标记已接受，使事务边界落在基础设施层。
type InvitationRepository interface {
	Create(ctx context.Context, invitation *model.Invitation) error
	ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*model.Invitation, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.Invitation, error)
	FindPendingByTokenHash(ctx context.Context, tokenHash string) (*model.Invitation, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	MarkAccepted(ctx context.Context, id, userID uuid.UUID) error
	AcceptRegistration(ctx context.Context, invitation *model.Invitation, user *model.User, membership *model.Membership, session *model.Session) error
}

// CreateInvitationInput 是创建邀请的入参。ActorRole 用于服务层防御性鉴权。
type CreateInvitationInput struct {
	WorkspaceID  uuid.UUID
	InvitedEmail string
	Role         value.WorkspaceRole
	CreatedBy    uuid.UUID
	ActorRole    value.WorkspaceRole
}

// InvitationService 负责邀请的创建、公开查询、接受与撤销。
// token 生成遵循规格 5.3：32 字节随机数经 base64url 编码为明文 token，
// 数据库仅存 SHA-256 hex hash 与 8 字符前缀；明文 token 仅出现在创建响应与接受请求中，
// 绝不入库/入日志/进入公开 DTO。
type InvitationService struct {
	invRepo         InvitationRepository
	wsRepo          WorkspaceRepository
	userRepo        UserRepository
	hasher          authport.PasswordHasher
	inviteLife      time.Duration
	sessionLife     time.Duration
	now             func() time.Time
	passwordEnabled bool
	authTx          OIDCAuthTxRunner // OIDC 邀请接受路径；password 模式可为 nil
	oidcIssuer      string           // AcceptOIDC 使用
}

// NewInvitationService 构造 InvitationService，注入邀请与会话寿命。
func NewInvitationService(
	invRepo InvitationRepository,
	wsRepo WorkspaceRepository,
	userRepo UserRepository,
	hasher authport.PasswordHasher,
	cfg config.AuthConfig,
) *InvitationService {
	return &InvitationService{
		invRepo:         invRepo,
		wsRepo:          wsRepo,
		userRepo:        userRepo,
		hasher:          hasher,
		inviteLife:      time.Duration(cfg.Invitation.LifetimeSeconds) * time.Second,
		sessionLife:     time.Duration(cfg.Session.LifetimeSeconds) * time.Second,
		now:             time.Now,
		passwordEnabled: cfg.Password.Enabled,
		oidcIssuer:      strings.TrimSpace(cfg.OIDC.Issuer),
	}
}

// WithOIDCAuthTx 注入 OIDC 事务 runner，使 AcceptOIDC 可用。
// 仅在 oidc.enabled=true 时由装配层调用。
func (s *InvitationService) WithOIDCAuthTx(tx OIDCAuthTxRunner) *InvitationService {
	s.authTx = tx
	return s
}

// List 返回 workspace 的邀请管理视图。仅 admin+ 可调用；pending 邀请优先，
// 其余和同组均按 created_at DESC、id DESC 稳定排序。
func (s *InvitationService) List(ctx context.Context, workspaceID uuid.UUID, actorRole value.WorkspaceRole) ([]*dto.InvitationListItem, error) {
	if !actorRole.AtLeast(value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	invitations, err := s.invRepo.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	result := make([]*dto.InvitationListItem, 0, len(invitations))
	for _, invitation := range invitations {
		if invitation == nil {
			continue
		}
		status := dto.InvitationStatusPending
		switch {
		case invitation.AcceptedAt != nil:
			status = dto.InvitationStatusAccepted
		case invitation.RevokedAt != nil:
			status = dto.InvitationStatusRevoked
		case !invitation.ExpiresAt.After(now):
			status = dto.InvitationStatusExpired
		}
		result = append(result, &dto.InvitationListItem{
			ID:           invitation.ID,
			WorkspaceID:  invitation.WorkspaceID,
			InvitedEmail: invitation.InvitedEmail,
			Role:         invitation.Role,
			TokenPrefix:  invitation.TokenPrefix,
			Status:       status,
			ExpiresAt:    invitation.ExpiresAt,
			AcceptedAt:   invitation.AcceptedAt,
			RevokedAt:    invitation.RevokedAt,
			CreatedBy:    invitation.CreatedBy,
			CreatedAt:    invitation.CreatedAt,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		iPending := result[i].Status == dto.InvitationStatusPending
		jPending := result[j].Status == dto.InvitationStatusPending
		if iPending != jPending {
			return iPending
		}
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.After(result[j].CreatedAt)
		}
		return result[i].ID.String() > result[j].ID.String()
	})
	return result, nil
}

// hashInvitationToken 对明文 token 计算 SHA-256 并以 hex 编码返回（存储用）。
func hashInvitationToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// generateInvitationToken 生成 32 字节随机数，base64url（无填充）编码为明文 token。
func generateInvitationToken() (string, error) {
	buf := make([]byte, invitationTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成邀请 token 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Create 创建邀请。仅 admin+ 可创建；admin 不得邀请 owner（仅 owner 可邀请 owner）。
// 返回的 DTO 不含 token_hash，明文 token 作为第二返回值交由 handler 拼接 invite_url。
func (s *InvitationService) Create(ctx context.Context, input CreateInvitationInput) (*dto.Invitation, string, error) {
	// 鉴权：admin+；且 admin 不得指定 owner。
	if !input.ActorRole.AtLeast(value.RoleAdmin) {
		return nil, "", domainerrors.ErrForbidden
	}
	if input.ActorRole == value.RoleAdmin && input.Role == value.RoleOwner {
		return nil, "", domainerrors.ErrForbidden
	}

	plaintextToken, err := generateInvitationToken()
	if err != nil {
		return nil, "", err
	}
	tokenHash := hashInvitationToken(plaintextToken)
	tokenPrefix := plaintextToken
	if len(tokenPrefix) > invitationTokenPrefixLen {
		tokenPrefix = tokenPrefix[:invitationTokenPrefixLen]
	}

	// email 规范化由 model.NewInvitation 完成（trim + lower + 校验）。
	invitation, err := model.NewInvitation(input.WorkspaceID, input.InvitedEmail, input.Role, input.CreatedBy)
	if err != nil {
		return nil, "", err
	}
	// 应用层覆盖 token、hash 与过期时间（领域构造器仅给默认值）。
	invitation.TokenHash = tokenHash
	invitation.TokenPrefix = tokenPrefix
	invitation.ExpiresAt = time.Now().UTC().Add(s.inviteLife)

	if err := s.invRepo.Create(ctx, invitation); err != nil {
		return nil, "", err
	}
	return dto.InvitationFromModel(invitation), plaintextToken, nil
}

// GetPublic 返回邀请的公开展示信息（仅对待处理邀请）。
// 过期/已接受/已撤销/不存在一律 ErrNotFound，避免通过响应差异枚举邀请状态。
func (s *InvitationService) GetPublic(ctx context.Context, plaintextToken string) (*dto.PublicInvitation, error) {
	tokenHash := hashInvitationToken(strings.TrimSpace(plaintextToken))
	invitation, err := s.invRepo.FindPendingByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}

	public := &dto.PublicInvitation{
		WorkspaceID:  invitation.WorkspaceID,
		InvitedEmail: invitation.InvitedEmail,
		Role:         invitation.Role,
		ExpiresAt:    invitation.ExpiresAt,
	}
	// 尽力富化 workspace 名/slug；失败不致命（仍返回带 id 的公开信息）。
	if ws, err := s.wsRepo.Get(ctx, invitation.WorkspaceID); err == nil {
		public.WorkspaceName = ws.Name
		public.WorkspaceSlug = ws.Slug
	}
	return public, nil
}

// Accept 校验 token 与 email 匹配，随后在单一事务内创建 user/membership/session
// 并标记邀请已接受。邮箱不匹配返回 ErrForbidden（防止 A 的邀请被 B 顶用）。
// 已被接受/撤销的邀请返回 ErrConflict（不可复用）。
func (s *InvitationService) Accept(ctx context.Context, plaintextToken, email, nickname, password, userAgent, ipAddr string) (*model.Session, error) {
	// password.enabled=false 时关闭 password 邀请接受，改走 OIDC AcceptOIDC。
	if !s.passwordEnabled {
		return nil, domainerrors.ErrPasswordRegistrationDisabled
	}
	tokenHash := hashInvitationToken(strings.TrimSpace(plaintextToken))
	invitation, err := s.invRepo.FindPendingByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}

	// 校验请求 email 与邀请锁定 email 一致（均规范化后比较）。
	normalizedEmail, err := normalizeEmailForInvitation(email)
	if err != nil {
		return nil, err
	}
	if normalizedEmail != invitation.InvitedEmail {
		return nil, domainerrors.ErrForbidden
	}

	password = strings.TrimSpace(password)
	if password == "" {
		return nil, fmt.Errorf("%w: 密码不能为空", domainerrors.ErrValidation)
	}
	passwordHash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("密码哈希失败: %w", err)
	}

	// 受邀注册用户永远不是平台管理员。
	user, err := model.NewUser(normalizedEmail, nickname, passwordHash)
	if err != nil {
		return nil, err
	}
	membership, err := model.NewMembership(invitation.WorkspaceID, user.ID, invitation.Role)
	if err != nil {
		return nil, err
	}
	session, err := model.NewSession(user.ID, s.sessionLife, userAgent, ipAddr)
	if err != nil {
		return nil, err
	}

	if err := s.invRepo.AcceptRegistration(ctx, invitation, user, membership, session); err != nil {
		return nil, err
	}
	return session, nil
}

// AcceptOIDC 在 OIDC 回调中接受邀请（spec §6.4）。
// 凭据是 profile.Email（已由 provider.Exchange 验签）与 IdP 签名。
// invitationTokenHash 为邀请 token 的 sha256 hex；profile 来自 IdP 回调。
//
// 流程：
//  1. 校验 token hash 格式与 profile（sub/email/email_verified 策略）。
//  2. FindPendingByTokenHash 快速失败（不作为并发安全依据）。
//  3. email 匹配 invitation.InvitedEmail。
//  4. 事务内 FOR UPDATE 重读 invitation，按 issuer+sub/email 决定复用/合并/JIT 建 user，
//     建 membership，标记 accepted，建 session。
func (s *InvitationService) AcceptOIDC(ctx context.Context, invitationTokenHash string, profile *authport.OIDCProfile, userAgent, ipAddr string) (*model.Session, error) {
	if s.authTx == nil {
		return nil, fmt.Errorf("%w: OIDC 邀请接受未配置事务 runner", domainerrors.ErrValidation)
	}
	if err := validateInvitationTokenHash(invitationTokenHash); err != nil {
		return nil, err
	}
	if err := validateOIDCProfile(profile, true); err != nil {
		return nil, err
	}

	normalizedEmail := normalizeEmailLocal(profile.Email)

	var session *model.Session
	err := s.authTx.WithinOIDCAuth(ctx, func(tx OIDCAuthTx) error {
		invitation, err := tx.FindPendingInvitationForUpdate(ctx, invitationTokenHash)
		if err != nil {
			return err
		}
		if normalizedEmail != invitation.InvitedEmail {
			return domainerrors.ErrForbidden
		}

		// 决定 user：identity 命中复用 / email 命中合并 / 均未命中 JIT 建号。
		var user *model.User
		identity, idErr := tx.FindIdentityByIssuerSubject(ctx, s.oidcIssuer, profile.Subject)
		if idErr != nil && !errors.Is(idErr, domainerrors.ErrNotFound) {
			return idErr
		}
		if identity != nil {
			user, err = tx.FindUserByID(ctx, identity.UserID)
			if err != nil {
				return err
			}
		} else {
			emailUser, feErr := tx.FindUserByEmail(ctx, normalizedEmail)
			if feErr != nil && !errors.Is(feErr, domainerrors.ErrNotFound) {
				return feErr
			}
			user = emailUser
			if user == nil {
				// JIT 建号：持 bootstrap lock，count==0 授 platform_admin。
				if err := tx.AcquireBootstrapLock(ctx); err != nil {
					return err
				}
				count, err := tx.CountUsers(ctx)
				if err != nil {
					return err
				}
				user, err = model.NewProvisionalUser(normalizedEmail, deriveNickname(profile))
				if err != nil {
					return err
				}
				user.IsPlatformAdmin = count == 0
				if err := tx.CreateUser(ctx, user); err != nil {
					return err
				}
			}
			newIdentity, err := model.NewExternalIdentity(user.ID, s.oidcIssuer, profile.Subject, normalizedEmail, profile.EmailVerified, profile.RawProfile)
			if err != nil {
				return err
			}
			if err := tx.CreateIdentity(ctx, newIdentity); err != nil {
				return err
			}
		}

		// 建 membership（role 取自 invitation）。
		membership, err := model.NewMembership(invitation.WorkspaceID, user.ID, invitation.Role)
		if err != nil {
			return err
		}
		if err := tx.CreateMembership(ctx, membership); err != nil {
			return err
		}

		// 标记 invitation accepted（WHERE accepted_at IS NULL 防并发重放）。
		if err := tx.MarkInvitationAccepted(ctx, invitation.ID, user.ID); err != nil {
			return err
		}

		sess, err := model.NewSession(user.ID, s.sessionLife, userAgent, ipAddr)
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

// validateInvitationTokenHash 校验 token hash 为 64 位小写 SHA-256 hex。
func validateInvitationTokenHash(tokenHash string) error {
	tokenHash = strings.TrimSpace(tokenHash)
	if len(tokenHash) != 64 {
		return fmt.Errorf("%w: invitation token hash 格式非法", domainerrors.ErrUnauthorized)
	}
	for _, r := range tokenHash {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return fmt.Errorf("%w: invitation token hash 含非法字符", domainerrors.ErrUnauthorized)
		}
	}
	return nil
}

// Revoke 撤销邀请。授权规则：
//   - platform_admin 可撤销任意邀请；
//   - workspace owner 可撤销该 workspace 任意邀请；
//   - 其它 admin+ 角色仅可撤销自己创建的邀请；
//   - 其余一律 ErrForbidden。
//
// 仅仍待处理（未接受/未撤销）的邀请可被撤销；终态邀请由仓储返回 ErrNotFound。
func (s *InvitationService) Revoke(ctx context.Context, invitationID, actorUserID uuid.UUID, actorRole value.WorkspaceRole, isPlatformAdmin bool) error {
	invitation, err := s.invRepo.FindByID(ctx, invitationID)
	if err != nil {
		return err
	}

	allowed := isPlatformAdmin ||
		actorRole.AtLeast(value.RoleOwner) ||
		(actorRole.AtLeast(value.RoleAdmin) && invitation.CreatedBy == actorUserID)
	if !allowed {
		return domainerrors.ErrForbidden
	}

	return s.invRepo.Revoke(ctx, invitationID)
}

// normalizeEmailForInvitation 复刻 model 层的 email 规范化（trim + lower），
// 用于在不创建模型的前提下比较请求 email 与邀请锁定 email。
func normalizeEmailForInvitation(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return "", fmt.Errorf("%w: email 不能为空", domainerrors.ErrValidation)
	}
	return normalized, nil
}
