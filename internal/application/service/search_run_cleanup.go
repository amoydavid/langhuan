package service

import (
	"context"
	"fmt"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// SearchRunCleanupStore 是 SearchRun 清理所需的持久化接口。
type SearchRunCleanupStore interface {
	DeleteExpired(context.Context, time.Time, int) (int64, error)
}

// SearchRunCleanupService 批量清理已过期的 SearchRun。
type SearchRunCleanupService struct {
	store SearchRunCleanupStore
	now   func() time.Time
}

// NewSearchRunCleanupService 创建 SearchRun 清理服务。
func NewSearchRunCleanupService(store SearchRunCleanupStore) *SearchRunCleanupService {
	return &SearchRunCleanupService{store: store, now: time.Now}
}

// Run 清理一批已过 expires_at 的 SearchRun，返回删除行数。
func (s *SearchRunCleanupService) Run(ctx context.Context, before time.Time, limit int) (int64, error) {
	if s.store == nil {
		return 0, fmt.Errorf("%w: SearchRun cleanup store 不能为空", domainerrors.ErrValidation)
	}
	if limit <= 0 {
		return 0, fmt.Errorf("%w: SearchRun cleanup limit 必须为正", domainerrors.ErrValidation)
	}
	return s.store.DeleteExpired(ctx, before, limit)
}

// RunNow 用当前时间执行清理。
func (s *SearchRunCleanupService) RunNow(ctx context.Context, limit int) (int64, error) {
	return s.Run(ctx, s.now(), limit)
}
