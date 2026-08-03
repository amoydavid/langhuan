package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// WorkspaceTxRunner creates a transaction carrying tenant-local PostgreSQL context.
type WorkspaceTxRunner struct {
	db *gorm.DB
}

// NewWorkspaceTxRunner creates a Workspace-scoped transaction runner.
func NewWorkspaceTxRunner(database *gorm.DB) *WorkspaceTxRunner {
	return &WorkspaceTxRunner{db: database}
}

// WithinWorkspace runs fn atomically after setting app.workspace_id with transaction-local scope.
func (r *WorkspaceTxRunner) WithinWorkspace(
	ctx context.Context,
	workspaceID uuid.UUID,
	fn func(*gorm.DB) error,
) error {
	if workspaceID == uuid.Nil {
		return fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	if fn == nil {
		return fmt.Errorf("%w: Workspace 事务回调不能为空", domainerrors.ErrValidation)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Exec(
			"SELECT set_config('app.workspace_id', ?, true)",
			workspaceID.String(),
		).Error; err != nil {
			return fmt.Errorf("设置 Workspace 数据库上下文失败: %w", err)
		}
		return fn(tx.WithContext(ctx))
	})
}
