package service

import (
	"context"
	"fmt"

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
	// UpdateKBGrants 在单个事务内更新 kb_scope 并整体替换授权集合。
	UpdateKBGrants(ctx context.Context, workspaceID, userID uuid.UUID, scope value.KBScope, kbIDs []uuid.UUID) (*model.Membership, error)
}

// MembershipUserRepository 描述成员列表批量富化所需的最小用户读取能力。
type MembershipUserRepository interface {
	ListByIDs(ctx context.Context, ids []uuid.UUID) ([]*model.User, error)
}

// MembershipKBValidator 校验授权集合中的知识库是否都属于本 workspace。
// 由 db.KnowledgeBaseRepository 实现。
type MembershipKBValidator interface {
	AllKnowledgeBasesExist(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) (bool, error)
}

// MembershipService 负责成员关系的列举、角色变更与移除。
// 鉴权（仅 owner 可变更角色/移除成员）在方法内做防御性校验；最终所有者不可被
// 降级或移除，以保证 workspace 永不失去 owner。
type MembershipService struct {
	repo        MembershipRepository
	users       MembershipUserRepository
	kbValidator MembershipKBValidator
}

func NewMembershipService(repo MembershipRepository, users MembershipUserRepository, kbValidators ...MembershipKBValidator) *MembershipService {
	var kbValidator MembershipKBValidator
	if len(kbValidators) > 0 {
		kbValidator = kbValidators[0]
	}
	return &MembershipService{repo: repo, users: users, kbValidator: kbValidator}
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

// SetKBGrantsInput 是成员知识库授权的写入口。Scope=all 时 KnowledgeBaseIDs 被忽略；
// Scope=restricted 时 KnowledgeBaseIDs 可为空集合（= 看不到任何知识库）。
type SetKBGrantsInput struct {
	WorkspaceID  uuid.UUID
	TargetUserID uuid.UUID
	// ActorRole 是调用者在 workspace 内的角色；要求 admin 及以上。
	ActorRole value.WorkspaceRole
	Scope     value.KBScope
	// KnowledgeBaseIDs 必须全部指向本 workspace 内存在且未删的知识库。
	KnowledgeBaseIDs []uuid.UUID
}

// SetKBGrants 设置目标成员的知识库可见范围与授权集合（幂等整体替换）。
// 仅 admin 及以上可调用；admin/owner 目标拒绝设置（该约束仅对 member 生效，
// 拒绝而非静默忽略，避免产生「设置了但不生效」的困惑态）。
func (s *MembershipService) SetKBGrants(ctx context.Context, input SetKBGrantsInput) (*dto.Membership, error) {
	if !input.ActorRole.AtLeast(value.RoleAdmin) {
		return nil, domainerrors.ErrForbidden
	}
	if !input.Scope.IsValid() {
		return nil, fmt.Errorf("%w: kb_scope 必须是 all/restricted", domainerrors.ErrValidation)
	}
	target, err := s.repo.Get(ctx, input.WorkspaceID, input.TargetUserID)
	if err != nil {
		return nil, err
	}
	if target.Role.AtLeast(value.RoleAdmin) {
		return nil, fmt.Errorf("%w: admin/owner 角色隐含全部知识库，不适用知识库授权", domainerrors.ErrValidation)
	}
	if input.Scope.IsRestricted() && len(input.KnowledgeBaseIDs) > 0 {
		if s.kbValidator == nil {
			return nil, fmt.Errorf("%w: 知识库授权校验器未装配", domainerrors.ErrValidation)
		}
		exist, err := s.kbValidator.AllKnowledgeBasesExist(ctx, input.WorkspaceID, input.KnowledgeBaseIDs)
		if err != nil {
			return nil, err
		}
		if !exist {
			return nil, fmt.Errorf("%w: 授权集合含不存在或不属于本 workspace 的知识库", domainerrors.ErrValidation)
		}
	}
	updated, err := s.repo.UpdateKBGrants(ctx, input.WorkspaceID, input.TargetUserID, input.Scope, input.KnowledgeBaseIDs)
	if err != nil {
		return nil, err
	}
	return dto.MembershipFromModel(updated), nil
}
