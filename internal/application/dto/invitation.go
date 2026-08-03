package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// Invitation 是创建邀请后返回给发起者（admin/owner）的 DTO。
// 它包含 TokenPrefix（便于后台识别）但绝不包含 TokenHash 或明文 token——
// 明文 token 仅在创建响应的 invite_url 中出现一次（由 HTTP 层拼接 host 构建）。
type Invitation struct {
	ID           uuid.UUID           `json:"id"`
	WorkspaceID  uuid.UUID           `json:"workspace_id"`
	InvitedEmail string              `json:"invited_email"`
	Role         value.WorkspaceRole `json:"role"`
	TokenPrefix  string              `json:"token_prefix"`
	ExpiresAt    time.Time           `json:"expires_at"`
	CreatedAt    time.Time           `json:"created_at"`
}

// InvitationStatus 是邀请管理列表使用的稳定状态。
type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "pending"
	InvitationStatusAccepted InvitationStatus = "accepted"
	InvitationStatusExpired  InvitationStatus = "expired"
	InvitationStatusRevoked  InvitationStatus = "revoked"
)

// InvitationListItem 是 workspace 管理端的邀请列表项。
// 它只暴露 token 前缀，不包含 token hash 或明文 token。
type InvitationListItem struct {
	ID           uuid.UUID           `json:"id"`
	WorkspaceID  uuid.UUID           `json:"workspace_id"`
	InvitedEmail string              `json:"invited_email"`
	Role         value.WorkspaceRole `json:"role"`
	TokenPrefix  string              `json:"token_prefix"`
	Status       InvitationStatus    `json:"status"`
	ExpiresAt    time.Time           `json:"expires_at"`
	AcceptedAt   *time.Time          `json:"accepted_at"`
	RevokedAt    *time.Time          `json:"revoked_at"`
	CreatedBy    uuid.UUID           `json:"created_by"`
	CreatedAt    time.Time           `json:"created_at"`
}

func InvitationFromModel(invitation *model.Invitation) *Invitation {
	if invitation == nil {
		return nil
	}
	return &Invitation{
		ID:           invitation.ID,
		WorkspaceID:  invitation.WorkspaceID,
		InvitedEmail: invitation.InvitedEmail,
		Role:         invitation.Role,
		TokenPrefix:  invitation.TokenPrefix,
		ExpiresAt:    invitation.ExpiresAt,
		CreatedAt:    invitation.CreatedAt,
	}
}

// PublicInvitation 是 GET /api/v1/invitations/:token 返回给受邀者的公开 DTO。
// 它只暴露邀请的基本展示信息（workspace 名、锁定 email、角色、过期时间），
// 绝不包含 token_hash、明文 token、接受/撤销状态等敏感字段。
// 过期/已接受/已撤销的邀请一律不返回此 DTO（服务层返回 ErrNotFound），
// 避免通过响应差异枚举邀请状态。
type PublicInvitation struct {
	WorkspaceID   uuid.UUID           `json:"workspace_id"`
	WorkspaceName string              `json:"workspace_name"`
	WorkspaceSlug string              `json:"workspace_slug"`
	InvitedEmail  string              `json:"invited_email"`
	Role          value.WorkspaceRole `json:"role"`
	ExpiresAt     time.Time           `json:"expires_at"`
}
