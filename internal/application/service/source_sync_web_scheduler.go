package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// 本文件实现 Meta Scheduler 的 web 内容源调度（v1.3.0 spec §9.1 执行隔离），与飞书
// 调度（source_sync_scheduler.go 的 dispatch/dispatchGroup）同构：
//
//	到期扫描（ListDueWebSources）→ 分组（有 connection 按 connection 组，沿用
//	max_concurrent_per_connection；无连接源自成单源组，额度 1）→ connection 组限额
//	+ 全局在飞上限（max_concurrent_syncs，仅统计 target 为 web 源的 active source_sync
//	Job）→ 入队 → 推进 next_sync_at（按源 config sync.cron）。
//
// 在飞封顶动机：asynq 队列权重只是调度优先级、全局 queue.concurrency 由全部任务类型
// 共享，多个长爬取会挤占 parse/index 等短任务的 worker 槽。超额到期源不入队也不推进
// next_sync_at，留待下次扫描或同 connection 任务完成后的续跑（TryDispatchConnection）。

// webSourceGroup 是 web 源的调度分组。有 connection 的源按 (workspace, connection)
// 归组共享限额；无连接源（http 引擎）每组只含一个源、额度为 1。
type webSourceGroup struct {
	workspaceID  uuid.UUID
	connectionID uuid.UUID // uuid.Nil 表示无连接单源组
	sources      []DueWebSource
}

// groupDueWebSources 按 connection 归集到期 web 源，保持稳定顺序；
// 无连接源各自成组（键为源 ID），保证"无连接源每组额度 1"的语义。
func groupDueWebSources(due []DueWebSource) []webSourceGroup {
	groups := make(map[string]*webSourceGroup)
	order := make([]string, 0, len(due))
	for _, src := range due {
		var key string
		if src.ConnectionID != uuid.Nil {
			key = fmt.Sprintf("%s|%s", src.WorkspaceID, src.ConnectionID)
		} else {
			key = fmt.Sprintf("source|%s", src.ID)
		}
		group, ok := groups[key]
		if !ok {
			group = &webSourceGroup{workspaceID: src.WorkspaceID, connectionID: src.ConnectionID}
			groups[key] = group
			order = append(order, key)
		}
		group.sources = append(group.sources, src)
	}
	result := make([]webSourceGroup, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	return result
}

// web 调度依赖未装配（WebStore/SourceService 任一为 nil）时跳过 web 源扫描。
func (s *SourceSyncScheduler) webDispatchEnabled() bool {
	return s.webStore != nil && s.sourceService != nil
}

// dispatchWebSources 扫描到期 web 源并入队。connectionFilter 为零值表示全量扫描
// （Tick）；非零值仅派发该 connection 的源（worker 完成后的同 connection 续跑）。
func (s *SourceSyncScheduler) dispatchWebSources(ctx context.Context, connectionFilter uuid.UUID) error {
	if !s.webDispatchEnabled() {
		return nil
	}
	now := time.Now().UTC()
	due, err := s.webStore.ListDueWebSources(ctx, now)
	if err != nil {
		return fmt.Errorf("列出到期 web 内容源失败: %w", err)
	}
	// connection 过滤在 Go 侧完成：ListDueWebSources 已把 config 的
	// fetch.connection_id 解析进 ConnectionID，无需方言化 JSON 查询。
	if connectionFilter != uuid.Nil {
		filtered := make([]DueWebSource, 0, len(due))
		for _, src := range due {
			if src.ConnectionID == connectionFilter {
				filtered = append(filtered, src)
			}
		}
		due = filtered
	}
	for _, group := range groupDueWebSources(due) {
		capReached, err := s.dispatchWebGroup(ctx, group, now)
		if err != nil {
			// 单组失败不中断其它组；只记录错误。
			s.logger.Error("调度 web 内容源组失败",
				"workspace_id", group.workspaceID.String(),
				"connection_id", group.connectionID.String(),
				"error", err.Error(),
			)
			continue
		}
		if capReached {
			// 全局在飞已触顶：本轮停止派发（剩余源不入队、不推进 next_sync_at），
			// 无需再对剩余组逐组重读必然触顶的全局计数。
			return nil
		}
	}
	return nil
}

// dispatchWebGroup 处理单个 web 源组：先按组额度（connection 组沿用
// maxConcurrentPerConnection，无连接单源组额度 1）计算可用额度，再逐源入队；
// 每次入队前重新读取全局在飞计数（仅 web 源任务），达到 maxConcurrentWebSyncs
// 即停止（返回 capReached=true，调用方据此结束本轮，剩余源不入队、不推进
// next_sync_at，留待下次扫描或续跑）。
func (s *SourceSyncScheduler) dispatchWebGroup(ctx context.Context, group webSourceGroup, now time.Time) (capReached bool, err error) {
	available := 1
	if group.connectionID != uuid.Nil {
		// 有连接组沿用 per-connection 限额；web 源 Job 行上标注 source_connection_id，
		// 与飞书任务在同一 connection 维度计数（同连接共享上游配额）。
		active, err := s.webStore.CountActiveByConnection(ctx, group.workspaceID, group.connectionID)
		if err != nil {
			return false, fmt.Errorf("统计 connection 进行中任务失败: %w", err)
		}
		available = s.maxConcurrentPerConnection - active
	}
	if available <= 0 {
		return false, nil
	}

	for _, src := range group.sources {
		if available <= 0 {
			return false, nil
		}
		// 同源串行（入队前检查）：该源已有 active 任务时跳过——不占组额度、
		// 不推进 next_sync_at，源保持到期状态，待任务完成后的同 connection
		// 续跑（TryDispatchConnection）或下次扫描重新派发（spec §9.1）。
		// 权威的原子保证在 RequestSourceSyncForSource 事务内（reuse-or-create）。
		activeBySource, err := s.webStore.CountActiveBySource(ctx, src.WorkspaceID, src.KnowledgeBaseID, src.ID)
		if err != nil {
			return false, fmt.Errorf("统计源进行中任务失败: %w", err)
		}
		if activeBySource > 0 {
			continue
		}
		// 全局在飞上限：入队前重读计数（本次入队产生的 pending Job 会计入下轮判断）。
		activeGlobal, err := s.webStore.CountActiveWebSourceJobs(ctx)
		if err != nil {
			return false, fmt.Errorf("统计 web 源全局进行中任务失败: %w", err)
		}
		if activeGlobal >= s.maxConcurrentWebSyncs {
			s.logger.Info("web 源在飞已达全局上限，剩余到期源留待下次扫描",
				"workspace_id", group.workspaceID.String(),
				"connection_id", group.connectionID.String(),
				"active_web_syncs", activeGlobal,
				"max_concurrent_web_syncs", s.maxConcurrentWebSyncs,
			)
			return true, nil
		}
		if _, err := s.sourceService.EnqueueSyncForSource(ctx, src.WorkspaceID, src.KnowledgeBaseID, src.ID, SyncOptions{Force: false}); err != nil {
			s.logger.Error("入队 web 内容源同步失败",
				"workspace_id", src.WorkspaceID.String(),
				"knowledge_base_id", src.KnowledgeBaseID.String(),
				"kb_source_id", src.ID.String(),
				"error", err.Error(),
			)
			continue
		}
		available--
		// 入队成功后推进 next_sync_at（基于源 config sync.cron）。失败只记日志，
		// 不回滚入队（已入队任务正常执行，下轮 cron 周期会重新对齐）。
		if err := s.advanceWebSourceNextSyncAt(ctx, src, now); err != nil {
			s.logger.Warn("推进 web 源 next_sync_at 失败",
				"workspace_id", src.WorkspaceID.String(),
				"knowledge_base_id", src.KnowledgeBaseID.String(),
				"kb_source_id", src.ID.String(),
				"error", err.Error(),
			)
		}
	}
	return false, nil
}

// advanceWebSourceNextSyncAt 读取源 config sync.cron，解析后计算下次同步时间并更新
// next_sync_at。无 cron 时清除 next_sync_at（仅手动触发）；cron 无效时清除避免死循环。
// 语义与飞书 advanceNextSyncAt 对齐。
func (s *SourceSyncScheduler) advanceWebSourceNextSyncAt(ctx context.Context, src DueWebSource, now time.Time) error {
	cronExpr := webSourceCron(src.Config)
	if cronExpr == "" {
		return s.webStore.UpdateWebSourceNextSyncAt(ctx, src.WorkspaceID, src.KnowledgeBaseID, src.ID, time.Time{})
	}
	sched, err := s.cronParser.Parse(cronExpr)
	if err != nil {
		s.logger.Warn("解析 web 源 sync.cron 失败，清除 next_sync_at",
			"workspace_id", src.WorkspaceID.String(),
			"knowledge_base_id", src.KnowledgeBaseID.String(),
			"kb_source_id", src.ID.String(),
			"cron", cronExpr,
			"error", err.Error(),
		)
		return s.webStore.UpdateWebSourceNextSyncAt(ctx, src.WorkspaceID, src.KnowledgeBaseID, src.ID, time.Time{})
	}
	return s.webStore.UpdateWebSourceNextSyncAt(ctx, src.WorkspaceID, src.KnowledgeBaseID, src.ID, sched.Next(now))
}
