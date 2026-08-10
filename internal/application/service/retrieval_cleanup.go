package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// RetrievalCleanupOptions controls retention windows and the maximum work per run.
type RetrievalCleanupOptions struct {
	FailedStagingRetention     time.Duration
	RetiredGenerationRetention time.Duration
	BatchSize                  int
}

// RetrievalCleanupRequest is one bounded, Workspace-scoped cleanup operation.
type RetrievalCleanupRequest struct {
	WorkspaceID         uuid.UUID
	FailedStagingBefore time.Time
	RetiredBefore       time.Time
	BatchSize           int
}

// RetrievalCleanupResult reports physically removed rebuildable projection rows.
type RetrievalCleanupResult struct {
	DeletedEntries     int64
	DeletedGenerations int64
}

// RetrievalCleanupGlobalRequest 是一次跨 workspace 的全局清理请求。
type RetrievalCleanupGlobalRequest struct {
	FailedStagingBefore time.Time
	RetiredBefore       time.Time
	BatchSize           int
}

// RetrievalCleanupStore physically removes expired rebuildable data.
type RetrievalCleanupStore interface {
	Cleanup(context.Context, RetrievalCleanupRequest) (RetrievalCleanupResult, error)
	// CleanupGlobal 跨 workspace 批量清理过期投影，供定时调度使用。
	CleanupGlobal(context.Context, RetrievalCleanupGlobalRequest) (RetrievalCleanupResult, error)
}

// RetrievalCleanupService applies configured retention windows to one Workspace.
type RetrievalCleanupService struct {
	store   RetrievalCleanupStore
	options RetrievalCleanupOptions
	now     func() time.Time
}

// NewRetrievalCleanupService creates a bounded cleanup service.
func NewRetrievalCleanupService(store RetrievalCleanupStore, options RetrievalCleanupOptions) *RetrievalCleanupService {
	return &RetrievalCleanupService{store: store, options: options, now: time.Now}
}

// Cleanup removes at most one configured batch of expired rebuildable data.
func (s *RetrievalCleanupService) Cleanup(
	ctx context.Context,
	workspaceID uuid.UUID,
) (RetrievalCleanupResult, error) {
	if workspaceID == uuid.Nil {
		return RetrievalCleanupResult{}, fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
	}
	if s == nil || s.store == nil {
		return RetrievalCleanupResult{}, fmt.Errorf("%w: Retrieval cleanup store 不能为空", domainerrors.ErrValidation)
	}
	if s.options.FailedStagingRetention <= 0 || s.options.RetiredGenerationRetention <= 0 ||
		s.options.BatchSize < 1 || s.options.BatchSize > 10000 {
		return RetrievalCleanupResult{}, fmt.Errorf("%w: Retrieval cleanup 配置无效", domainerrors.ErrValidation)
	}
	now := s.now().UTC()
	result, err := s.store.Cleanup(ctx, RetrievalCleanupRequest{
		WorkspaceID:         workspaceID,
		FailedStagingBefore: now.Add(-s.options.FailedStagingRetention),
		RetiredBefore:       now.Add(-s.options.RetiredGenerationRetention),
		BatchSize:           s.options.BatchSize,
	})
	if err != nil {
		return RetrievalCleanupResult{}, fmt.Errorf("清理 Retrieval 投影失败: %w", err)
	}
	return result, nil
}

// CleanupGlobal 跨 workspace 批量清理过期投影，供定时调度使用。
func (s *RetrievalCleanupService) CleanupGlobal(ctx context.Context) (RetrievalCleanupResult, error) {
	if s == nil || s.store == nil {
		return RetrievalCleanupResult{}, fmt.Errorf("%w: Retrieval cleanup store 不能为空", domainerrors.ErrValidation)
	}
	if s.options.FailedStagingRetention <= 0 || s.options.RetiredGenerationRetention <= 0 ||
		s.options.BatchSize < 1 || s.options.BatchSize > 10000 {
		return RetrievalCleanupResult{}, fmt.Errorf("%w: Retrieval cleanup 配置无效", domainerrors.ErrValidation)
	}
	now := s.now().UTC()
	return s.store.CleanupGlobal(ctx, RetrievalCleanupGlobalRequest{
		FailedStagingBefore: now.Add(-s.options.FailedStagingRetention),
		RetiredBefore:       now.Add(-s.options.RetiredGenerationRetention),
		BatchSize:           s.options.BatchSize,
	})
}
