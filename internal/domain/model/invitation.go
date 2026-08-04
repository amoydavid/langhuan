package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// Invitation 表示一条 workspace 邀请记录。
// TokenHash 为 invitation token 的 SHA-256 hash，TokenPrefix 为 token 前 8 位。
// 明文 token 不得入库、入日志。
type Invitation struct {
	ID             uuid.UUID
	WorkspaceID    uuid.UUID
	InvitedEmail   string
	Role           value.WorkspaceRole
	TokenHash      string
	TokenPrefix    string
	ExpiresAt      time.Time
	AcceptedAt     *time.Time
	AcceptedUserID uuid.UUID
	RevokedAt      *time.Time
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
}

// invitationLifetime 是领域层默认的邀请有效期（7 天）。
// 应用层可依据配置覆盖 ExpiresAt。
const invitationLifetime = 7 * 24 * time.Hour

// NewInvitation 创建并校验邀请。token 的生成与 hash 由应用层负责（Task 3/5），
// 因此本构造器只校验 workspace、email、role，并设置默认过期时间。
func NewInvitation(workspaceID uuid.UUID, invitedEmail string, role value.WorkspaceRole, createdBy uuid.UUID) (*Invitation, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}

	normalizedEmail, err := normalizeEmail(invitedEmail)
	if err != nil {
		return nil, err
	}

	if !role.IsValid() {
		return nil, fmt.Errorf("%w: workspace 角色无效", domainerrors.ErrValidation)
	}

	now := time.Now().UTC()
	return &Invitation{
		ID:           id.New(),
		WorkspaceID:  workspaceID,
		InvitedEmail: normalizedEmail,
		Role:         role,
		ExpiresAt:    now.Add(invitationLifetime),
		CreatedBy:    createdBy,
		CreatedAt:    now,
	}, nil
}

// IsPending 判断邀请是否仍待处理：未被接受、未被撤销，且尚未过期。
func (i Invitation) IsPending(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && i.ExpiresAt.After(now)
}
