package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// MembershipUserSummary 是成员列表可公开的用户摘要。
type MembershipUserSummary struct {
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
}

// Membership 是成员关系对外 DTO。workspace 成员列表会通过一次批量查询填充
// User；用于认证概要的轻量列表保持 User 为 nil。
type Membership struct {
	ID          uuid.UUID              `json:"id"`
	WorkspaceID uuid.UUID              `json:"workspace_id"`
	UserID      uuid.UUID              `json:"user_id"`
	Role        value.WorkspaceRole    `json:"role"`
	User        *MembershipUserSummary `json:"user"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

func MembershipFromModel(membership *model.Membership) *Membership {
	if membership == nil {
		return nil
	}
	return &Membership{
		ID:          membership.ID,
		WorkspaceID: membership.WorkspaceID,
		UserID:      membership.UserID,
		Role:        membership.Role,
		CreatedAt:   membership.CreatedAt,
		UpdatedAt:   membership.UpdatedAt,
	}
}

func MembershipListFromModel(memberships []*model.Membership) []*Membership {
	result := make([]*Membership, 0, len(memberships))
	for _, m := range memberships {
		if m == nil {
			continue
		}
		result = append(result, MembershipFromModel(m))
	}
	return result
}
