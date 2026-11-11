package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
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
	// KBScope 是知识库可见范围（all | restricted）；约束仅对 member 生效，
	// admin/owner 隐含全量。零值按 all 处理（存量行为）。
	KBScope value.KBScope
	// KnowledgeBaseIDs 是受限 member 的显式授权集合（聚合加载，非行内存储）；
	// 仅 KBScope=restricted 时非空。
	KnowledgeBaseIDs []uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewMembership 创建并校验成员关系。workspace 与 user 都不能为零值，role 必须合法。
// 新成员默认 kb_scope=all（邀请接受后可由 admin 收紧）。
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
		ID:          id.New(),
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
		KBScope:     value.KBScopeAll,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}
