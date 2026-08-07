package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// UserRepository 描述 user 聚合的仓储抽象（服务层本地接口，由 db.UserRepository 实现）。
// ResetPassword 在仓储内部以单一事务完成密码更新与会话撤销，使事务边界落在
// 持有 *gorm.DB 的基础设施层，服务层不感知数据库句柄。
type UserRepository interface {
	Create(ctx context.Context, user *model.User) error
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	Count(ctx context.Context) (int64, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	// ResetPassword 原子地更新密码并撤销该用户的全部会话（事务内完成）。
	ResetPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
}

// SessionRepository 描述 session 聚合的仓储抽象（服务层本地接口）。
type SessionRepository interface {
	Create(ctx context.Context, session *model.Session) error
	FindActive(ctx context.Context, id uuid.UUID) (*model.Session, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteAllForUser(ctx context.Context, userID uuid.UUID) error
}

// UserService 负责用户自注册（首个平台管理员）与平台管理员重置密码。
// 它不创建会话；会话由 AuthService 在登录时创建。ResetPassword 的会话撤销
// 由 user 仓储的 ResetPassword 在事务内完成，因此本服务不持有 session 仓储。
type UserService struct {
	repo            UserRepository
	hasher          authport.PasswordHasher
	passwordEnabled bool
}

// NewUserService 构造一个仅依赖 user 仓储与 hasher 的 UserService。
func NewUserService(repo UserRepository, hasher authport.PasswordHasher, passwordEnabled bool) *UserService {
	return &UserService{repo: repo, hasher: hasher, passwordEnabled: passwordEnabled}
}

// IsInitialized reports whether the installation already has at least one user.
func (s *UserService) IsInitialized(ctx context.Context) (bool, error) {
	count, err := s.repo.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("统计用户失败: %w", err)
	}
	return count > 0, nil
}

// RegisterFirstUser 注册平台首位用户并赋予 platform_admin 角色。
// 仅当当前用户数为 0 时允许；已存在用户则返回 ErrConflict，关闭首注册通道。
// 该流程绝不创建 membership 或 session——首用户的登录与成员关系由后续流程处理。
func (s *UserService) RegisterFirstUser(ctx context.Context, email, nickname, password string) (*dto.AuthenticatedUser, error) {
	// password.enabled=false 时关闭 password 首注册通道，bootstrap 由 OIDC JIT 完成。
	if !s.passwordEnabled {
		return nil, domainerrors.ErrPasswordRegistrationDisabled
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return nil, fmt.Errorf("%w: 密码不能为空", domainerrors.ErrValidation)
	}

	count, err := s.repo.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计用户失败: %w", err)
	}
	if count != 0 {
		// 首用户通道已关闭——拒绝重复首注册。
		return nil, domainerrors.ErrConflict
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("密码哈希失败: %w", err)
	}

	user, err := model.NewUser(email, nickname, hash)
	if err != nil {
		return nil, err
	}
	// 首位用户即为平台管理员。
	user.IsPlatformAdmin = true

	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}
	return dto.AuthenticatedUserFromModel(user), nil
}

// ResetPassword 由平台管理员重置目标用户的密码，并撤销该用户的全部会话。
// 仅 platform_admin 可调用；非管理员一律 ErrForbidden。密码更新与会话撤销
// 在仓储事务内原子完成。
func (s *UserService) ResetPassword(ctx context.Context, actorUserID uuid.UUID, actorIsPlatformAdmin bool, targetUserID uuid.UUID, newPassword string) error {
	if !actorIsPlatformAdmin {
		return domainerrors.ErrForbidden
	}
	newPassword = strings.TrimSpace(newPassword)
	if newPassword == "" {
		return fmt.Errorf("%w: 密码不能为空", domainerrors.ErrValidation)
	}

	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	if err := s.repo.ResetPassword(ctx, targetUserID, hash); err != nil {
		return err
	}
	return nil
}

// GetByID 返回指定用户的非敏感视图（用于 GET /api/v1/auth/me）。未找到返回仓储错误。
func (s *UserService) GetByID(ctx context.Context, userID uuid.UUID) (*dto.AuthenticatedUser, error) {
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return dto.AuthenticatedUserFromModel(user), nil
}
