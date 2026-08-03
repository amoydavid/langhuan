package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

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
	return membershipFromRow(&row), nil
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
	return result, nil
}

// ListByUserID 返回某用户在全部 workspace 的成员关系（用于 /api/v1/auth/me 概要）。
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
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}
