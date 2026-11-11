package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type MembershipRepository struct {
	db *gorm.DB
}

func NewMembershipRepository(db *gorm.DB) *MembershipRepository {
	return &MembershipRepository{db: db}
}

func (r *MembershipRepository) Create(ctx context.Context, membership *model.Membership) error {
	if err := r.db.WithContext(ctx).Create(membershipToRow(membership)).Error; err != nil {
		return translateDBError(err, "创建成员关系失败")
	}
	return nil
}

func (r *MembershipRepository) Get(ctx context.Context, workspaceID, userID uuid.UUID) (*model.Membership, error) {
	var row MembershipRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取成员关系失败: %w", err)
	}
	membership := membershipFromRow(&row)
	if err := r.loadGrants(ctx, []*model.Membership{membership}); err != nil {
		return nil, err
	}
	return membership, nil
}

func (r *MembershipRepository) List(ctx context.Context, workspaceID uuid.UUID) ([]*model.Membership, error) {
	var rows []MembershipRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出成员关系失败: %w", err)
	}
	result := make([]*model.Membership, 0, len(rows))
	for i := range rows {
		result = append(result, membershipFromRow(&rows[i]))
	}
	if err := r.loadGrants(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

// ListByUserID 返回某用户在全部 workspace 的成员关系（用于 /api/v1/auth/me 概要）。
// 概要场景不加载授权集合（KnowledgeBaseIDs 保持 nil）。
func (r *MembershipRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*model.Membership, error) {
	var rows []MembershipRow
	if err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("按用户列出成员关系失败: %w", err)
	}
	result := make([]*model.Membership, 0, len(rows))
	for i := range rows {
		result = append(result, membershipFromRow(&rows[i]))
	}
	return result, nil
}

// loadGrants 为受限成员批量填充授权集合；all 成员与 nil 元素跳过。
func (r *MembershipRepository) loadGrants(ctx context.Context, memberships []*model.Membership) error {
	restrictedIDs := make([]uuid.UUID, 0)
	byMembershipID := make(map[uuid.UUID]*model.Membership)
	for _, m := range memberships {
		if m == nil || !m.KBScope.IsRestricted() {
			continue
		}
		restrictedIDs = append(restrictedIDs, m.ID)
		byMembershipID[m.ID] = m
		m.KnowledgeBaseIDs = []uuid.UUID{}
	}
	if len(restrictedIDs) == 0 {
		return nil
	}
	var grantRows []MemberKnowledgeBaseRow
	if err := r.db.WithContext(ctx).
		Where("membership_id IN ?", restrictedIDs).
		Order("knowledge_base_id ASC").
		Find(&grantRows).Error; err != nil {
		return fmt.Errorf("读取成员知识库授权失败: %w", err)
	}
	for _, grant := range grantRows {
		if m, ok := byMembershipID[grant.MembershipID]; ok {
			m.KnowledgeBaseIDs = append(m.KnowledgeBaseIDs, grant.KnowledgeBaseID)
		}
	}
	return nil
}

// UpdateKBGrants 在单个事务内更新 kb_scope 并整体替换授权集合（幂等）。
// scope 为 all 时清空全部绑定行。返回更新后的成员关系（含新授权集合）。
func (r *MembershipRepository) UpdateKBGrants(ctx context.Context, workspaceID, userID uuid.UUID, scope value.KBScope, kbIDs []uuid.UUID) (*model.Membership, error) {
	if !scope.IsValid() {
		return nil, fmt.Errorf("%w: kb_scope 无效", domainerrors.ErrValidation)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row MembershipRow
		if err := tx.Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
			First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRepositoryNotFound
			}
			return fmt.Errorf("读取成员关系失败: %w", err)
		}
		if err := tx.Model(&MembershipRow{}).
			Where("id = ?", row.ID).
			Updates(map[string]any{
				"kb_scope":   string(scope),
				"updated_at": time.Now().UTC(),
			}).Error; err != nil {
			return fmt.Errorf("更新成员知识库范围失败: %w", err)
		}
		if err := tx.Where("membership_id = ?", row.ID).
			Delete(&MemberKnowledgeBaseRow{}).Error; err != nil {
			return fmt.Errorf("清空成员知识库授权失败: %w", err)
		}
		if scope.IsRestricted() && len(kbIDs) > 0 {
			grants := make([]MemberKnowledgeBaseRow, 0, len(kbIDs))
			seen := make(map[uuid.UUID]struct{}, len(kbIDs))
			now := time.Now().UTC()
			for _, kbID := range kbIDs {
				if kbID == uuid.Nil {
					return fmt.Errorf("%w: 授权集合含空知识库 ID", domainerrors.ErrValidation)
				}
				if _, dup := seen[kbID]; dup {
					continue
				}
				seen[kbID] = struct{}{}
				grants = append(grants, MemberKnowledgeBaseRow{
					MembershipID:    row.ID,
					WorkspaceID:     workspaceID,
					KnowledgeBaseID: kbID,
					CreatedAt:       now,
				})
			}
			if len(grants) > 0 {
				if err := tx.Create(&grants).Error; err != nil {
					return translateDBError(err, "写入成员知识库授权失败")
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, workspaceID, userID)
}

func (r *MembershipRepository) ChangeRole(ctx context.Context, workspaceID, userID uuid.UUID, role value.WorkspaceRole) error {
	result := r.db.WithContext(ctx).Model(&MembershipRow{}).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Updates(map[string]any{
			"role":       string(role),
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return fmt.Errorf("更新成员角色失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *MembershipRepository) Delete(ctx context.Context, workspaceID, userID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Delete(&MembershipRow{})
	if result.Error != nil {
		return fmt.Errorf("删除成员关系失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

func (r *MembershipRepository) CountOwners(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&MembershipRow{}).
		Where("workspace_id = ? AND role = ?", workspaceID, string(value.RoleOwner)).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计 workspace owner 失败: %w", err)
	}
	return count, nil
}

func membershipToRow(membership *model.Membership) *MembershipRow {
	return &MembershipRow{
		ID:          membership.ID,
		WorkspaceID: membership.WorkspaceID,
		UserID:      membership.UserID,
		Role:        string(membership.Role),
		KBScope:     string(membership.KBScope.Normalize()),
		CreatedAt:   membership.CreatedAt,
		UpdatedAt:   membership.UpdatedAt,
	}
}

func membershipFromRow(row *MembershipRow) *model.Membership {
	return &model.Membership{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		UserID:      row.UserID,
		Role:        value.WorkspaceRole(row.Role),
		KBScope:     value.KBScope(row.KBScope).Normalize(),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
