package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// MembershipRepository 描述 membership 聚合的仓储抽象（服务层本地接口，
// 由 db.MembershipRepository 实现）。
type MembershipRepository interface {
	Create(ctx context.Context, membership *model.Membership) error
	Get(ctx context.Context, workspaceID, userID uuid.UUID) (*model.Membership, error)
	List(ctx context.Context, workspaceID uuid.UUID) ([]*model.Membership, error)
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]*model.Membership, error)
	ChangeRole(ctx context.Context, workspaceID, userID uuid.UUID, role value.WorkspaceRole) error
	Delete(ctx context.Context, workspaceID, userID uuid.UUID) error
	CountOwners(ctx context.Context, workspaceID uuid.UUID) (int64, error)
}

// MembershipUserRepository 描述成员列表批量富化所需的最小用户读取能力。
type MembershipUserRepository interface {
	ListByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.User, error)
}

// MembershipService 负责成员关系的列举、角色变更与移除。
// 鉴权（仅 owner 可变更角色/移除成员）在方法内做防御性校验；最终所有者不可被
// 降级或移除，以保证 workspace 永不失去 owner。
type MembershipService struct {
	repo  MembershipRepository
	users MembershipUserRepository
}

func NewMembershipService(repo MembershipRepository, users MembershipUserRepository) *MembershipService {
	return &MembershipService{repo: repo, users: users}
}

// List 返回 workspace 的全部成员关系。鉴权（member+ 可见）由中间件完成。
func (s *MembershipService) List(ctx context.Context, workspaceID uuid.UUID) ([]*dto.Membership, error) {
	memberships, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := dto.MembershipListFromModel(memberships)
	if len(result) == 0 {
		return result, nil
	}
	userIDs := make([]uuid.UUID, 0, len(result))
	for _, membership := range result {
		userIDs = append(userIDs, membership.UserID)
	}
	users, err := s.users.ListByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	usersByID := make(map[uuid.UUID]*model.User, len(users))
	for _, user := range users {
		if user != nil {
			usersByID[user.ID] = user
		}
	}
	for _, membership := range result {
		if user := usersByID[membership.UserID]; user != nil {
			membership.User = &dto.MembershipUserSummary{Email: user.Email, Nickname: user.Nickname}
		}
	}
	return result, nil
}

// Get 返回单条成员关系。
func (s *MembershipService) Get(ctx context.Context, workspaceID, userID uuid.UUID) (*dto.Membership, error) {
	m, err := s.repo.Get(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	return dto.MembershipFromModel(m), nil
}

// ListForUser 返回某用户在全部 workspace 的成员关系（用于 GET /api/v1/auth/me 的 workspace 概要）。
func (s *MembershipService) ListForUser(ctx context.Context, userID uuid.UUID) ([]*dto.Membership, error) {
	memberships, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return dto.MembershipListFromModel(memberships), nil
}

// ChangeRole 变更目标成员的角色。仅 owner 可调用；不得将最后一名 owner 降级
// 为非 owner（防止 workspace 失去所有者）。提升他人为 owner 始终允许。
func (s *MembershipService) ChangeRole(ctx context.Context, workspaceID, targetUserID uuid.UUID, newRole value.WorkspaceRole, actorRole value.WorkspaceRole) (*dto.Membership, error) {
	if !actorRole.AtLeast(value.RoleOwner) {
		return nil, domainerrors.ErrForbidden
	}

	// 若目标当前是 owner 且将降级为非 owner，需校验是否为唯一 owner。
	current, err := s.repo.Get(ctx, workspaceID, targetUserID)
	if err != nil {
		return nil, err
	}
	if current.Role == value.RoleOwner && newRole != value.RoleOwner {
		count, err := s.repo.CountOwners(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, domainerrors.ErrConflict
		}
	}

	if err := s.repo.ChangeRole(ctx, workspaceID, targetUserID, newRole); err != nil {
		return nil, err
	}
	updated, err := s.repo.Get(ctx, workspaceID, targetUserID)
	if err != nil {
		return nil, err
	}
	return dto.MembershipFromModel(updated), nil
}

// Remove 移除目标成员。仅 owner 可调用；最后一名 owner 不可被移除。
func (s *MembershipService) Remove(ctx context.Context, workspaceID, targetUserID uuid.UUID, actorRole value.WorkspaceRole) error {
	if !actorRole.AtLeast(value.RoleOwner) {
		return domainerrors.ErrForbidden
	}

	current, err := s.repo.Get(ctx, workspaceID, targetUserID)
	if err != nil {
		return err
	}
	if current.Role == value.RoleOwner {
		count, err := s.repo.CountOwners(ctx, workspaceID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return domainerrors.ErrConflict
		}
	}

	return s.repo.Delete(ctx, workspaceID, targetUserID)
}
