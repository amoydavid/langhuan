package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// Membership 表示用户在某个 workspace 中的成员关系与角色。
type Membership struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        value.WorkspaceRole
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewMembership 创建并校验成员关系。workspace 与 user 都不能为零值，role 必须合法。
func NewMembership(workspaceID, userID uuid.UUID, role value.WorkspaceRole) (*Membership, error) {
	if workspaceID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: user_id 不能为空", domainerrors.ErrValidation)
	}
	if !role.IsValid() {
		return nil, fmt.Errorf("%w: workspace 角色无效", domainerrors.ErrValidation)
	}

	now := time.Now().UTC()
	return &Membership{
		ID:          uuid.New(),
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}
