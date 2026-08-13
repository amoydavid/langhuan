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

type InvitationRepository struct {
	db *gorm.DB
}

func NewInvitationRepository(db *gorm.DB) *InvitationRepository {
	return &InvitationRepository{db: db}
}

func (r *InvitationRepository) Create(ctx context.Context, invitation *model.Invitation) error {
	if err := r.db.WithContext(ctx).Create(invitationToRow(invitation)).Error; err != nil {
		return translateDBError(err, "创建邀请失败")
	}
	return nil
}

// ListByWorkspace 返回 workspace 内的全部邀请，按创建时间和 ID 倒序稳定排列。
func (r *InvitationRepository) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*model.Invitation, error) {
	var rows []InvitationRow
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ?", workspaceID).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("列出邀请失败: %w", err)
	}
	result := make([]*model.Invitation, 0, len(rows))
	for i := range rows {
		result = append(result, invitationFromRow(&rows[i]))
	}
	return result, nil
}

// FindByID 按主键查询邀请记录（含任意状态：待处理/已接受/已撤销/已过期）。
// 供应用层在撤销授权时按 id 回查邀请的 CreatedBy 等字段。未找到返回 ErrNotFound。
func (r *InvitationRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Invitation, error) {
	var row InvitationRow
	if err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取邀请失败: %w", err)
	}
	return invitationFromRow(&row), nil
}

// FindPendingByTokenHash 按 token_hash 查询仍待处理（未接受、未撤销、未过期）的邀请。
// 其余情形一律返回 ErrNotFound，不向调用方区分状态以避免信息泄漏。
func (r *InvitationRepository) FindPendingByTokenHash(ctx context.Context, tokenHash string) (*model.Invitation, error) {
	var row InvitationRow
	expiresCmp := "expires_at > now()"
	if r.db.Dialector.Name() == "sqlite" {
		// SQLite 的 datetime() 无法解析 Go time.Time.String() 的 " UTC" 后缀（返回 NULL），
		// 不能包裹列。用参数绑定 time.Now().UTC()，与 oidc_auth_tx_runner 的既有正确写法一致。
		expiresCmp = "expires_at > ?"
	}
	var args []any
	if r.db.Dialector.Name() == "sqlite" {
		args = []any{tokenHash, time.Now().UTC()}
	} else {
		args = []any{tokenHash}
	}
	if err := r.db.WithContext(ctx).
		Where("token_hash = ? AND accepted_at IS NULL AND revoked_at IS NULL AND "+expiresCmp, args...).
		First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRepositoryNotFound
		}
		return nil, fmt.Errorf("读取邀请失败: %w", err)
	}
	return invitationFromRow(&row), nil
}

func (r *InvitationRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&InvitationRow{}).
		Where("id = ? AND accepted_at IS NULL AND revoked_at IS NULL", id).
		Update("revoked_at", time.Now().UTC())
	if result.Error != nil {
		return fmt.Errorf("撤销邀请失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrRepositoryNotFound
	}
	return nil
}

// MarkAccepted 将仍待处理（未接受、未撤销）的邀请标记为已接受。
// WHERE 子句要求 accepted_at IS NULL AND revoked_at IS NULL，由数据库保证
// 同一条邀请只能被接受一次：并发或重复接受时 RowsAffected==0，返回 ErrConflict。
func (r *InvitationRepository) MarkAccepted(ctx context.Context, id, userID uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&InvitationRow{}).
		Where("id = ? AND accepted_at IS NULL AND revoked_at IS NULL", id).
		Updates(map[string]any{
			"accepted_at":      time.Now().UTC(),
			"accepted_user_id": userID,
		})
	if result.Error != nil {
		return fmt.Errorf("标记邀请已接受失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		// 邀请已不存在、已被接受或已被撤销 —— 视为冲突，拒绝重复接受。
		return domainerrors.ErrConflict
	}
	return nil
}

// AcceptRegistration 在单个事务内原子地完成邀请接受流程：
// 创建 user、创建 membership、创建 session，最后将邀请标记为已接受。
// 任一步失败则整体回滚。返回值为领域错误或包裹的通用错误。
func (r *InvitationRepository) AcceptRegistration(
	ctx context.Context,
	invitation *model.Invitation,
	user *model.User,
	membership *model.Membership,
	session *model.Session,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(userToRow(user)).Error; err != nil {
			return translateDBError(err, "接受邀请创建用户失败")
		}
		if err := tx.Create(membershipToRow(membership)).Error; err != nil {
			return translateDBError(err, "接受邀请创建成员关系失败")
		}
		if err := tx.Create(sessionToRow(session)).Error; err != nil {
			return translateDBError(err, "接受邀请创建会话失败")
		}
		// 仅当邀请仍待处理（未接受、未撤销）时才标记。WHERE 子句由数据库
		// 保证单条邀请只能被接受一次；并发接受时 RowsAffected==0 视为冲突。
		result := tx.Model(&InvitationRow{}).
			Where("id = ? AND accepted_at IS NULL AND revoked_at IS NULL", invitation.ID).
			Updates(map[string]any{
				"accepted_at":      time.Now().UTC(),
				"accepted_user_id": user.ID,
			})
		if result.Error != nil {
			return translateDBError(result.Error, "接受邀请标记邀请失败")
		}
		if result.RowsAffected == 0 {
			return domainerrors.ErrConflict
		}
		return nil
	})
}

func invitationToRow(invitation *model.Invitation) *InvitationRow {
	var acceptedUserID *uuid.UUID
	if invitation.AcceptedUserID != uuid.Nil {
		id := invitation.AcceptedUserID
		acceptedUserID = &id
	}
	return &InvitationRow{
		ID:             invitation.ID,
		WorkspaceID:    invitation.WorkspaceID,
		InvitedEmail:   invitation.InvitedEmail,
		Role:           string(invitation.Role),
		TokenHash:      invitation.TokenHash,
		TokenPrefix:    invitation.TokenPrefix,
		ExpiresAt:      invitation.ExpiresAt,
		AcceptedAt:     invitation.AcceptedAt,
		AcceptedUserID: acceptedUserID,
		RevokedAt:      invitation.RevokedAt,
		CreatedBy:      invitation.CreatedBy,
		CreatedAt:      invitation.CreatedAt,
	}
}

func invitationFromRow(row *InvitationRow) *model.Invitation {
	var acceptedUserID uuid.UUID
	if row.AcceptedUserID != nil {
		acceptedUserID = *row.AcceptedUserID
	}
	return &model.Invitation{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		InvitedEmail:   row.InvitedEmail,
		Role:           value.WorkspaceRole(row.Role),
		TokenHash:      row.TokenHash,
		TokenPrefix:    row.TokenPrefix,
		ExpiresAt:      row.ExpiresAt,
		AcceptedAt:     row.AcceptedAt,
		AcceptedUserID: acceptedUserID,
		RevokedAt:      row.RevokedAt,
		CreatedBy:      row.CreatedBy,
		CreatedAt:      row.CreatedAt,
	}
}
