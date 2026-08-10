package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
)

// SearchRunStore 持久化检索运行快照。所有读写都显式带 workspace_id。
type SearchRunStore interface {
	// Create 创建一个 running SearchRun（含 Generation 快照）。
	Create(context.Context, *model.SearchRun) error
	// Complete 在 Workspace transaction 中把 running SearchRun 推进到终态。
	// 只允许 running -> 终态；Not Found 映射为 ErrNotFound。
	Complete(context.Context, uuid.UUID, uuid.UUID, model.SearchRunCompletion) error
	// Get 读取一个 SearchRun 及其 Generation 快照；跨 Workspace 返回 ErrNotFound。
	Get(context.Context, uuid.UUID, uuid.UUID) (*model.SearchRun, error)
	// DeleteExpired 批量删除已过 expires_at 的 SearchRun，返回删除行数。
	DeleteExpired(context.Context, time.Time, int) (int64, error)
}
