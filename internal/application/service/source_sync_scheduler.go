package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
)

// SyncEnqueuer 把单个飞书知识库的首次/定时同步入队。
// 由 *SourceSyncService 实现（EnqueueSync）。
type SyncEnqueuer interface {
	EnqueueSync(ctx context.Context, workspaceID, kbID uuid.UUID) (*model.Job, error)
}

// SchedulerStore 提供 Meta Scheduler 限流所需的进行中任务计数。
type SchedulerStore interface {
	CountActiveByConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) (int, error)
}

// SourceSyncSchedulerDeps 注入 Meta Scheduler 的全部依赖。
type SourceSyncSchedulerDeps struct {
	// KBRepo 提供到期飞书知识库列表与 next_sync_at 推进。
	KBRepo KnowledgeBaseSyncRepository
	// Store 提供按 connection 维度的进行中任务计数（限流）。
	Store SchedulerStore
	// SyncService 把单个 KB 入队（首次/定时同步）。
	SyncService SyncEnqueuer
	// MaxConcurrentPerConnection 是每个来源连接的最大并发同步数。
	MaxConcurrentPerConnection int
	// Interval 是 Tick 周期；Run 循环据此休眠。<=0 时由 NewSourceSyncScheduler 兜底为 60s。
	Interval time.Duration
	// Logger 输出调度日志（不含正文/凭证）。
	Logger Logger
}

// SourceSyncScheduler 是 Meta Scheduler：周期性扫描到期飞书知识库，按来源连接限流入队。
// 单 goroutine 运行，check-then-act 在单进程内无竞态。
type SourceSyncScheduler struct {
	kbRepo                     KnowledgeBaseSyncRepository
	store                      SchedulerStore
	syncService                SyncEnqueuer
	maxConcurrentPerConnection int
	interval                   time.Duration
	logger                     Logger
	cronParser                 cron.Parser
}

// NewSourceSyncScheduler 构造一个 Meta Scheduler。
func NewSourceSyncScheduler(deps SourceSyncSchedulerDeps) *SourceSyncScheduler {
	maxConcurrent := deps.MaxConcurrentPerConnection
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	interval := deps.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	logger := deps.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	return &SourceSyncScheduler{
		kbRepo:                     deps.KBRepo,
		store:                      deps.Store,
		syncService:                deps.SyncService,
		maxConcurrentPerConnection: maxConcurrent,
		interval:                   interval,
		logger:                     logger,
		// 5 字段标准 cron 表达式：minute hour dom month dow。
		cronParser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// connectionGroup 是按 (workspace, connection) 维度归集的到期 KB 列表。
type connectionGroup struct {
	workspaceID  uuid.UUID
	connectionID uuid.UUID
	kbs          []DueKnowledgeBase
}

// groupDueKBs 按 (workspaceID, connectionID) 归集到期 KB，保持稳定顺序。
func groupDueKBs(due []DueKnowledgeBase) []connectionGroup {
	groups := make(map[string]*connectionGroup)
	order := make([]string, 0, len(due))
	for _, item := range due {
		key := fmt.Sprintf("%s|%s", item.WorkspaceID, item.SourceConnectionID)
		group, ok := groups[key]
		if !ok {
			group = &connectionGroup{workspaceID: item.WorkspaceID, connectionID: item.SourceConnectionID}
			groups[key] = group
			order = append(order, key)
		}
		group.kbs = append(group.kbs, item)
	}
	result := make([]connectionGroup, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	return result
}

// Tick 执行一次调度扫描：列出所有到期飞书 KB → 按 connection 分组 → 限流入队 → 推进 next_sync_at。
func (s *SourceSyncScheduler) Tick(ctx context.Context) error {
	if s.kbRepo == nil || s.store == nil || s.syncService == nil {
		return fmt.Errorf("%w: Meta Scheduler 依赖未配置", domainerrors.ErrValidation)
	}
	return s.dispatch(ctx, uuid.Nil)
}

// TryDispatchConnection 针对单个 connection 派发到期 KB（worker 完成后调用以续跑，避免空等）。
func (s *SourceSyncScheduler) TryDispatchConnection(ctx context.Context, workspaceID, connectionID uuid.UUID) error {
	if s.kbRepo == nil || s.store == nil || s.syncService == nil {
		return fmt.Errorf("%w: Meta Scheduler 依赖未配置", domainerrors.ErrValidation)
	}
	if connectionID == uuid.Nil {
		return nil
	}
	// TryDispatchConnection 按 connection 过滤到期 KB；workspaceID 用于日志，ListDueFeishuKBs 已按 connection 收敛。
	_ = workspaceID
	return s.dispatch(ctx, connectionID)
}

// dispatch 是 Tick/TryDispatchConnection 的共享实现。connectionFilter 为零值表示不过滤。
func (s *SourceSyncScheduler) dispatch(ctx context.Context, connectionFilter uuid.UUID) error {
	now := time.Now().UTC()
	due, err := s.kbRepo.ListDueFeishuKBs(ctx, now, connectionFilter)
	if err != nil {
		return fmt.Errorf("列出到期飞书知识库失败: %w", err)
	}
	if len(due) == 0 {
		return nil
	}

	for _, group := range groupDueKBs(due) {
		if err := s.dispatchGroup(ctx, group, now); err != nil {
			// 单组失败不中断其它组；只记录错误。
			s.logger.Error("调度 connection 组失败",
				"workspace_id", group.workspaceID.String(),
				"connection_id", group.connectionID.String(),
				"error", err.Error(),
			)
		}
	}
	return nil
}

// dispatchGroup 处理单个 connection 组：按额度入队到期 KB，入队后推进 next_sync_at。
func (s *SourceSyncScheduler) dispatchGroup(ctx context.Context, group connectionGroup, now time.Time) error {
	active, err := s.store.CountActiveByConnection(ctx, group.workspaceID, group.connectionID)
	if err != nil {
		return fmt.Errorf("统计 connection 进行中任务失败: %w", err)
	}
	available := s.maxConcurrentPerConnection - active
	if available <= 0 {
		return nil
	}

	enqueued := 0
	for _, dueKB := range group.kbs {
		if enqueued >= available {
			break
		}
		if _, err := s.syncService.EnqueueSync(ctx, group.workspaceID, dueKB.ID); err != nil {
			s.logger.Error("入队飞书知识库同步失败",
				"workspace_id", group.workspaceID.String(),
				"knowledge_base_id", dueKB.ID.String(),
				"error", err.Error(),
			)
			continue
		}
		enqueued++
		// 入队成功后推进 next_sync_at（基于 source_config.cron）。失败只记日志。
		if err := s.advanceNextSyncAt(ctx, group.workspaceID, dueKB.ID, now); err != nil {
			s.logger.Warn("推进 next_sync_at 失败",
				"workspace_id", group.workspaceID.String(),
				"knowledge_base_id", dueKB.ID.String(),
				"error", err.Error(),
			)
		}
	}
	return nil
}

// advanceNextSyncAt 读取 KB source_config.cron，解析后计算下次同步时间并更新 next_sync_at。
// 无 cron 字段时跳过（仅手动触发）；cron 无效时清除 next_sync_at 避免死循环。
func (s *SourceSyncScheduler) advanceNextSyncAt(ctx context.Context, workspaceID, kbID uuid.UUID, now time.Time) error {
	kb, err := s.kbRepo.Get(ctx, workspaceID, kbID)
	if err != nil {
		return fmt.Errorf("读取知识库失败: %w", err)
	}
	cronExpr, _ := kb.SourceConfig["cron"].(string)
	if cronExpr == "" {
		// 无 cron：清除 next_sync_at，仅手动触发。
		return s.kbRepo.UpdateNextSyncAt(ctx, workspaceID, kbID, time.Time{})
	}
	sched, err := s.cronParser.Parse(cronExpr)
	if err != nil {
		// cron 无效：清除 next_sync_at 避免持续命中到期条件。
		s.logger.Warn("解析 source_config.cron 失败，清除 next_sync_at",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"cron", cronExpr,
			"error", err.Error(),
		)
		return s.kbRepo.UpdateNextSyncAt(ctx, workspaceID, kbID, time.Time{})
	}
	next := sched.Next(now)
	return s.kbRepo.UpdateNextSyncAt(ctx, workspaceID, kbID, next)
}

// Run 启动周期调度循环，直到 ctx 取消。周期来自 Interval（config）。
func (s *SourceSyncScheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.logger.Info("启动来源同步 Meta Scheduler",
		"interval_seconds", int(s.interval.Seconds()),
		"max_concurrent_per_connection", s.maxConcurrentPerConnection,
	)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("Meta Scheduler Tick 失败", "error", err.Error())
			}
		}
	}
}
