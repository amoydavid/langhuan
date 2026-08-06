package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// WorkspaceSearchSettings 描述一个 Workspace 的默认查询阶段策略。
// Rerank 为 nil 时，单库和多库搜索均只使用 RRF。
type WorkspaceSearchSettings struct {
	WorkspaceID uuid.UUID
	Rerank      *RerankSnapshot
	UpdatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate 校验 Workspace 归属和可选 Rerank 快照。
func (s *WorkspaceSearchSettings) Validate() error {
	if s == nil || s.WorkspaceID == uuid.Nil {
		return fmt.Errorf("%w: Workspace Search Settings 无效", domainerrors.ErrValidation)
	}
	return s.Rerank.Validate()
}

// Clone 返回 WorkspaceSearchSettings 的深拷贝。
func (s *WorkspaceSearchSettings) Clone() *WorkspaceSearchSettings {
	if s == nil {
		return nil
	}
	clone := *s
	clone.Rerank = s.Rerank.Clone()
	return &clone
}
