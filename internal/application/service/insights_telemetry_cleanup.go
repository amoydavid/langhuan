package service

import (
	"context"
	"fmt"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// TelemetryCleanupStore 是检索遥测清理所需的持久化接口（D3：两表生命周期
// 独立于 search_runs.expires_at，各自按保留期清理）。
type TelemetryCleanupStore interface {
	// DeleteExpiredCitations 删除 created_at 早于 before 的引用行。
	DeleteExpiredCitations(context.Context, time.Time, int) (int64, error)
	// DeleteExpiredQueryLogs 删除 created_at 早于 before 的查询原文行。
	DeleteExpiredQueryLogs(context.Context, time.Time, int) (int64, error)
}

// InsightsTelemetryCleanupService 按各自保留期批量清理两张遥测表。
// 关键语义：引用/原文行不随 search_run 过期级联删除——run 已清理但引用行
// 仍在是删除预警（citation-summary）依赖的状态；只有自身保留期到了才删。
type InsightsTelemetryCleanupService struct {
	store             TelemetryCleanupStore
	citationRetention time.Duration
	queryLogRetention time.Duration
	now               func() time.Time
}

// NewInsightsTelemetryCleanupService 创建遥测清理服务；保留期必须为正。
func NewInsightsTelemetryCleanupService(
	store TelemetryCleanupStore,
	citationRetention, queryLogRetention time.Duration,
) *InsightsTelemetryCleanupService {
	return &InsightsTelemetryCleanupService{
		store: store, citationRetention: citationRetention,
		queryLogRetention: queryLogRetention, now: time.Now,
	}
}

// RunNow 清理一批过期的引用行与查询原文行，返回各自删除行数。
func (s *InsightsTelemetryCleanupService) RunNow(ctx context.Context, limit int) (int64, int64, error) {
	if s == nil || s.store == nil {
		return 0, 0, fmt.Errorf("%w: 遥测清理 store 不能为空", domainerrors.ErrValidation)
	}
	if limit <= 0 {
		return 0, 0, fmt.Errorf("%w: 遥测清理 limit 必须为正", domainerrors.ErrValidation)
	}
	now := s.now()
	citationCutoff := now.Add(-s.citationRetention)
	logCutoff := now.Add(-s.queryLogRetention)
	citationsDeleted, err := s.store.DeleteExpiredCitations(ctx, citationCutoff, limit)
	if err != nil {
		return 0, 0, err
	}
	logsDeleted, err := s.store.DeleteExpiredQueryLogs(ctx, logCutoff, limit)
	if err != nil {
		return citationsDeleted, 0, err
	}
	return citationsDeleted, logsDeleted, nil
}
