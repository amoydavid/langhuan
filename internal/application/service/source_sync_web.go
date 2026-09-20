package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件编排 web 内容源（kb_sources.kind='web'）的 source_sync 同步（v1.3.0 spec
// §3.2/§6.5/§8）。编排主语从 KB 泛化为源：diff/cursor/force latch/删除按源执行；
// 纯函数层（diff/computeSafeCursor/classifyUpdate）与飞书路径（source_sync.go）零改动，
// 仅"从哪拿树 / diff 范围 / 结果写到哪"源化。
//
// 关键编排义务（T7 评审移交）：
//   - web 快照按（path 段数升序）输出，按序处理即父先到——目录索引页（页面节点）可能
//     是某目录链上 folder 的父，因此本路径按快照顺序单趟处理 folder 与 page，并把
//     folder 与 page 两类 Token 同时注册进父子定位映射（只注册 folder 会让「父为
//     索引页的 folder 下的子页」必然定位失败，见 web/connector_tree.go 注释）；
//   - Fetch 错误统一交 webadapter.ClassifyError 分类：gone→远端删除（ToRemove 候选，
//     受 Complete 闸门约束）、blocked/robots_disallowed/skipped/skipped_non_html→
//     跳过+warning 非 retryable、retryable/oversize→Job 级 RetryRequired/oversize；
//   - 引擎未接入错误（errors.Is(err, domainerrors.ErrValidation)）判源配置性 failed，
//     整轮终止且错误链保留 ErrValidation 供 worker SkipRetry，避免源陷入重试循环。

// webWarningURLOwnedByOtherSource 是跨源冲突告警码（spec §3.2 先到先得）。
const webWarningURLOwnedByOtherSource = "url_owned_by_other_source"

// webSyncRunCtx 聚合一次 web 源同步内多个节点共享的上下文。
// syncRunCtx 提供 kb/conn/generationID/tokenToNodeID（folder upsert 等共享 helper 复用）。
type webSyncRunCtx struct {
	*syncRunCtx
	source *model.KbSource
	// config 是注入凭证后的源配置副本（__credentials 仅内存透传，绝不落库、不进日志）。
	config map[string]any
	// connectionID 是源配置引用的连接（fetch.connection_id，spec §4.1）；http 引擎为
	// uuid.Nil。仅用于给 parse Job 标注 source_connection_id；传给 connector 的 conn
	// 始终为零值（T7 合同：web connector 不消费 connection）。
	connectionID uuid.UUID
	// occupiedByOther 是「同 KB 内被其它源/KB 级文档占用的 external_id → 归属源 ID
	//（uuid.Nil 表示 KB 级文档）」。ToAdd 节点命中即跳过 + warning，不覆盖不删除。
	occupiedByOther map[string]uuid.UUID
	// goneRemovals 承载 update 路径中「Fetch 判 404/410（远端删除）」的本地文档，
	// 在删除闸门内随 plan.ToRemove 一起处理（spec §6.5）。
	goneRemovals []LocalDocView
}

// addGoneRemoval 追加一个 gone（远端删除）移除候选。
func (c *webSyncRunCtx) addGoneRemoval(view LocalDocView) {
	c.goneRemovals = append(c.goneRemovals, view)
}

// RunSourceSyncJobForSource 是源级（web）source_sync 任务的 worker 应用层入口，
// latch 流程与 KB 级 RunSourceSyncJob 同构（spec 8.2）：
//
//  1. ConsumeSourceForceLatch：原子读取并清空源级 force latch；
//  2. syncWebSource(force)：执行 web 源同步数据流；
//  3. FinalizeSourceSyncJobForSource：源行锁定事务内终态化 Job；
//     latch 在运行期间被重新置位时返回下一个待入队的源级 source_sync Job。
func (s *SourceSyncService) RunSourceSyncJobForSource(ctx context.Context, workspaceID, kbID, sourceID, jobID uuid.UUID) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil || jobID == uuid.Nil {
		return fmt.Errorf("%w: workspace_id/knowledge_base_id/source_id/job_id 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil || s.queue == nil {
		return fmt.Errorf("%w: 来源同步依赖未配置", domainerrors.ErrValidation)
	}

	// 1) 消费源级 force latch（失败时降级为 force=false，不阻断同步）。
	force, latchErr := s.store.ConsumeSourceForceLatch(ctx, workspaceID, kbID, sourceID, jobID)
	if latchErr != nil {
		s.logger.Warn("消费源级 force latch 失败，降级为非强制同步",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"kb_source_id", sourceID.String(),
			"job_id", jobID.String(),
			"error", latchErr.Error(),
		)
		force = false
	}

	// 2) 执行 web 源同步数据流。
	syncErr := s.syncWebSource(ctx, workspaceID, kbID, sourceID, force)

	// 3) 终态化 Job；latch 在运行期间被重置位时创建下一个 Job 并入队。
	status := value.JobStatusCompleted
	errorMessage := ""
	if syncErr != nil {
		status = value.JobStatusFailed
		errorMessage = syncErr.Error()
	}
	nextJob, finErr := s.store.FinalizeSourceSyncJobForSource(ctx, workspaceID, kbID, sourceID, jobID, status, errorMessage)
	if finErr != nil {
		s.logger.Error("终态化源级 source_sync Job 失败",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"kb_source_id", sourceID.String(),
			"job_id", jobID.String(),
			"error", finErr.Error(),
		)
		if syncErr != nil {
			return errors.Join(syncErr, fmt.Errorf("终态化源级 Job 失败: %w", finErr))
		}
		return fmt.Errorf("终态化源级 Job 失败: %w", finErr)
	}
	if nextJob != nil {
		// 后续 Job 同样按源入队（含 connection 标注与按源推导的 Timeout）。
		// 读取源失败/已软删时记日志并放弃：源删除后无需续跑；latch 已消费。
		source, srcErr := s.store.GetSyncKbSource(ctx, workspaceID, kbID, sourceID)
		if srcErr != nil {
			s.logger.Warn("读取源以入队后续 source_sync 任务失败，等待 scheduler 恢复",
				"workspace_id", workspaceID.String(),
				"knowledge_base_id", kbID.String(),
				"kb_source_id", sourceID.String(),
				"job_id", nextJob.ID.String(),
				"error", srcErr.Error(),
			)
		} else if err := s.enqueueSourceSyncTaskForSource(ctx, workspaceID, kbID, source, nextJob.ID, s.webSyncTaskTimeoutFor(source)); err != nil {
			s.logger.Warn("入队后续源级 source_sync 任务失败，等待 scheduler 恢复",
				"workspace_id", workspaceID.String(),
				"knowledge_base_id", kbID.String(),
				"kb_source_id", sourceID.String(),
				"job_id", nextJob.ID.String(),
				"error", err.Error(),
			)
		}
	}
	return syncErr
}

// enqueueSourceSyncTaskForSource 把一个源级 source_sync Job 入队 asynq
// （按 (workspace,kb,source) 幂等 TaskID）。payload 标注 connection（worker 完成后
// 触发同 connection 续跑）；timeout 由调用方按源推导传入（spec §9.1：
// min(页数基准×单页耗时×2, 硬上限)），避免入队与日志两处重复推导。
func (s *SourceSyncService) enqueueSourceSyncTaskForSource(ctx context.Context, workspaceID, kbID uuid.UUID, source *model.KbSource, jobID uuid.UUID, timeout time.Duration) error {
	connectionID, err := WebFetchConnectionID(source.Config)
	if err != nil {
		// 非法 connection_id 属源配置性错误：入队失败 → Job 标记失败，错误链保留给调用方。
		return fmt.Errorf("解析 web 源 fetch.connection_id 失败: %w", err)
	}
	payload, err := json.Marshal(sourceSyncTaskPayload{
		WorkspaceID:     workspaceID,
		KnowledgeBaseID: kbID,
		SourceID:        source.ID,
		JobID:           jobID,
		ConnectionID:    connectionID,
	})
	if err != nil {
		return fmt.Errorf("编码源级 source_sync payload 失败: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type:    model.SourceSyncJobType,
		Payload: payload,
		TaskID:  queue.SourceSyncTaskIDForSource(workspaceID, kbID, source.ID),
		Timeout: timeout,
	}); err != nil {
		return fmt.Errorf("入队源级 source_sync 任务失败: %w", err)
	}
	return nil
}

// syncWebSource 实现单个 web 源的一次（增量）同步，数据流与飞书路径同构：
//
//	ListTree → 按源读本地投影 → diff → 单趟应用 folder/page → 删除闸门 →
//	安全 cursor → SyncResult（写入 kb_sources.sync_last_result）。
//
// force=true 时跳过内容 hash 去重，强制为每个匹配节点创建新 revision（既有机制）。
func (s *SourceSyncService) syncWebSource(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, force bool) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil || sourceID == uuid.Nil {
		return fmt.Errorf("%w: workspace_id/knowledge_base_id/source_id 不能为空", domainerrors.ErrValidation)
	}
	if s.webConnector == nil || s.store == nil || s.rawStore == nil || s.queue == nil || s.kbRepo == nil || s.selector == nil {
		// selector 供凭证预解密（fetch.connection_id 源），缺失属装配错误，
		// 提前给出可诊断错误而非运行期 nil panic（对齐飞书路径守卫）。
		return fmt.Errorf("%w: web 源同步依赖未配置", domainerrors.ErrValidation)
	}

	kb, err := s.kbRepo.Get(ctx, workspaceID, kbID)
	if err != nil {
		return fmt.Errorf("读取知识库失败: %w", err)
	}
	if kb.ActiveIndexGenerationID == nil || *kb.ActiveIndexGenerationID == uuid.Nil {
		return fmt.Errorf("%w: 知识库缺少 active IndexGeneration", domainerrors.ErrValidation)
	}
	generationID := *kb.ActiveIndexGenerationID

	source, err := s.store.GetSyncKbSource(ctx, workspaceID, kbID, sourceID)
	if err != nil {
		return fmt.Errorf("读取内容源失败: %w", err)
	}
	if source.Kind != value.KbSourceKindWeb {
		return fmt.Errorf("%w: 内容源种类 %q 不支持 web 同步", domainerrors.ErrValidation, source.Kind)
	}
	if source.Status != value.KbSourceStatusActive {
		// 已停用源：入队前检查保证不再新派发；已入队任务幂等跳过（spec §9.1 同源单 active）。
		s.logger.Info("跳过已停用内容源的同步",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"kb_source_id", sourceID.String(),
		)
		return nil
	}

	config, connectionID, err := s.webSourceConfigWithCredentials(ctx, workspaceID, source)
	if err != nil {
		// 配置/凭证属源配置性错误：错误链保留 ErrValidation，worker 判永久失败不重试。
		s.writeWebSourceResultBestEffort(ctx, workspaceID, kbID, sourceID, failedWebSyncResult())
		return fmt.Errorf("解析 web 源配置失败: %w", err)
	}

	// connector 按 provider 解析（spec §3.2）：web 源固定用注入的 web connector。
	conn := model.SourceConnection{} // T7 合同：web connector 不消费 connection（凭证经 SourceConfig 注入）。
	root := webSyncRoot(config)
	snapshot, err := s.webConnector.ListTree(ctx, conn, root, sourceport.ListOptions{SourceConfig: config})
	if err != nil {
		s.writeWebSourceResultBestEffort(ctx, workspaceID, kbID, sourceID, failedWebSyncResult())
		// 引擎未接入/配置非法（ErrValidation）→ worker SkipRetry；其余（网络/5xx）→ asynq 重试。
		return fmt.Errorf("列举 web 页面树失败: %w", err)
	}

	cursor := readSyncCursor(source.SyncCursor)

	// 本地投影：按源圈定 diff 范围（「源期望全集」对「源本地全集」），并构建
	// 跨源占用表用于先到先得裁决（spec §3.2）。
	allDocs, err := s.store.ListSourceDocuments(ctx, kbID)
	if err != nil {
		return fmt.Errorf("读取本地来源文档投影失败: %w", err)
	}
	ownDocs := filterLocalDocsBySource(allDocs, &sourceID)
	plan := diff(snapshot, ownDocs, cursor, force)

	syncCtx := &webSyncRunCtx{
		syncRunCtx: &syncRunCtx{
			kb:            kb,
			conn:          conn,
			generationID:  generationID,
			tokenToNodeID: make(map[string]uuid.UUID, len(snapshot.Nodes)),
		},
		source:          source,
		config:          config,
		connectionID:    connectionID,
		occupiedByOther: occupiedExternalIDsByOthers(allDocs, sourceID),
	}

	// 预注册本源既有页面 Token → file 节点 ID：目录索引页（页面节点）可能是 folder
	// 链上的父，更新/跳过路径的索引页必须先于子 folder/子页可解析（T7 编排义务）。
	if err := s.preregisterWebPageTokens(ctx, syncCtx, ownDocs); err != nil {
		s.logger.Warn("预注册 web 页面父子映射失败，目录索引页嵌套链可能退化",
			"workspace_id", workspaceID.String(),
			"kb_source_id", sourceID.String(),
			"error", err.Error(),
		)
	}

	// 单趟按快照顺序处理（父先到）：folder → upsert；page → add/update/skip。
	outcomes, counts, warnings, fatalErr := s.applyWebSnapshotNodes(ctx, syncCtx, snapshot, plan, force)
	if fatalErr != nil {
		// 引擎未接入等源配置性失败：整轮判 failed，不走 RetryRequired（T7 评审移交）。
		s.writeWebSourceResultBestEffort(ctx, workspaceID, kbID, sourceID, failedWebSyncResult())
		return fmt.Errorf("应用 web 快照失败: %w", fatalErr)
	}

	// 删除闸门：仅完整 snapshot 才执行删除（spec 5.5）；gone 分类在 snapshot 完整时
	// 并入 ToRemove（spec §6.5 远端删除）。
	deleted := 0
	cleanupPending := 0
	folderDeleteFailed := false
	policy := value.SourceDeletePolicyFromConfig(source.Config["on_delete"])
	if snapshot.Complete {
		removals := append([]LocalDocView(nil), plan.ToRemove...)
		removals = append(removals, syncCtx.goneRemovals...)
		deleted, cleanupPending = s.applyMissingDocumentsWithPolicy(ctx, workspaceID, kbID, policy, removals)
		if folderErr := s.deleteMissingFolders(ctx, syncCtx.syncRunCtx, snapshot); folderErr != nil {
			folderDeleteFailed = true
			s.logger.Error("web 源 folder 清理失败",
				"workspace_id", workspaceID.String(),
				"kb_source_id", sourceID.String(),
				"error", folderErr.Error(),
			)
		}
	}

	// SyncResult 组装 + cursor 推进（语义与飞书一致）。
	result := SyncResult{
		Complete:          snapshot.Complete,
		SyncedDocuments:   counts.synced,
		SkippedDocuments:  counts.skipped + plan.Skipped,
		FailedDocuments:   counts.failed,
		OversizeDocuments: counts.oversize,
		DeletedDocuments:  deleted,
		CleanupPending:    cleanupPending,
		FinishedAt:        time.Now().UTC(),
	}
	result.Status = s.classifySyncResult(snapshot, counts, folderDeleteFailed)
	s.logWebWarnings(workspaceID, sourceID, snapshot.Warnings, plan.Warnings, warnings)

	if writeErr := s.store.UpdateSourceSyncResult(ctx, workspaceID, kbID, sourceID, result); writeErr != nil {
		s.logger.Error("回写 web 源同步结果失败",
			"workspace_id", workspaceID.String(),
			"kb_source_id", sourceID.String(),
			"error", writeErr.Error(),
		)
	}

	if snapshot.Complete {
		safeCursor := computeSafeCursor(snapshot, outcomes, cursor)
		if !safeCursor.IsZero() && safeCursor.After(cursor) {
			if cursorErr := s.store.UpdateSourceSyncCursor(ctx, workspaceID, kbID, sourceID, safeCursor); cursorErr != nil {
				s.logger.Error("回写 web 源同步游标失败",
					"workspace_id", workspaceID.String(),
					"kb_source_id", sourceID.String(),
					"error", cursorErr.Error(),
				)
			}
		}
	}

	s.logger.Info("web 源同步完成",
		"workspace_id", workspaceID.String(),
		"knowledge_base_id", kbID.String(),
		"kb_source_id", sourceID.String(),
		"status", result.Status,
		"synced_documents", result.SyncedDocuments,
		"skipped_documents", result.SkippedDocuments,
		"failed_documents", result.FailedDocuments,
		"deleted_documents", result.DeletedDocuments,
		"complete", snapshot.Complete,
	)
	return nil
}

// webSourceConfigWithCredentials 返回注入凭证后的源配置副本（spec §3.1 凭证预解密注入）：
//   - 无 connection（http 引擎，fetch.connection_id 为空）：零拷贝语义返回浅拷贝，
//     无凭证注入，conn 零值；
//   - 有 connection：预解密 credential_cipher 后经 SourceConfig["__credentials"] 注入
//     （T11 引擎消费）；密钥绝不落库、绝不进日志。
//
// 注入写入的是源配置的浅拷贝（KbSource.Config 按引用保存，禁止原地修改——T4 评审移交）。
func (s *SourceSyncService) webSourceConfigWithCredentials(ctx context.Context, workspaceID uuid.UUID, source *model.KbSource) (map[string]any, uuid.UUID, error) {
	config := make(map[string]any, len(source.Config)+1)
	for key, val := range source.Config {
		config[key] = val
	}

	connectionID, err := WebFetchConnectionID(config)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if connectionID == uuid.Nil {
		// http 引擎无需凭证（spec §6.1）：不注入、不解析 connection。
		return config, uuid.Nil, nil
	}
	selected, err := s.selector.Select(ctx, workspaceID, connectionID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("解析来源连接凭证失败: %w", err)
	}
	credentials := map[string]any{"provider": selected.Provider}
	if len(selected.AppSecret) > 0 {
		// 注入键按 provider 区分（firecrawl/jina=api_key，browser=token）。
		if value.SourceProvider(selected.Provider) == value.SourceProviderBrowser {
			credentials["token"] = string(selected.AppSecret)
		} else {
			credentials["api_key"] = string(selected.AppSecret)
		}
	}
	// 非敏感端点透传（T11 引擎消费）：browser 的 CDP endpoint_url 是 render 引擎
	// 的连接入口；firecrawl 的 endpoint_url 覆盖官方云 endpoint（自托管栈）。键名
	// 统一 endpoint_url，与 connector 侧消费约定（web 包 credentialsEndpointKey）一致。
	if endpointURL, ok := selected.Config["endpoint_url"].(string); ok && strings.TrimSpace(endpointURL) != "" {
		credentials["endpoint_url"] = strings.TrimSpace(endpointURL)
	}
	config["__credentials"] = credentials
	return config, connectionID, nil
}

// WebFetchConnectionID 从源 config 读取 fetch.connection_id（缺失/空 → uuid.Nil）。
// 导出供 db 层在源级 Job 落库时标注 source_connection_id（同 connection 限流统计用）。
func WebFetchConnectionID(config map[string]any) (uuid.UUID, error) {
	fetchObj, _ := config["fetch"].(map[string]any)
	if fetchObj == nil {
		return uuid.Nil, nil
	}
	raw, _ := fetchObj["connection_id"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: fetch.connection_id 不是合法 UUID: %q", domainerrors.ErrValidation, raw)
	}
	return parsed, nil
}

// webSyncRoot 从源 config 推导 SyncRoot（spec §5.5）：crawl→seed_url、sitemap→sitemap.url、
// urls→空串（URL 列表从 SourceConfig 读取）。
func webSyncRoot(config map[string]any) model.SyncRoot {
	token := ""
	if crawl, ok := config["crawl"].(map[string]any); ok {
		if seed, ok := crawl["seed_url"].(string); ok {
			token = strings.TrimSpace(seed)
		}
	}
	if sitemap, ok := config["sitemap"].(map[string]any); ok {
		if sitemapURL, ok := sitemap["url"].(string); ok && strings.TrimSpace(sitemapURL) != "" {
			token = strings.TrimSpace(sitemapURL)
		}
	}
	return model.SyncRoot{Kind: sourceport.SyncRootWeb, Token: token}
}

// occupiedExternalIDsByOthers 构建「被其它源/KB 级文档占用的 external_id」表：
// 值为归属源 ID（KB 级文档为 uuid.Nil）。own 源自己的文档不占用（走 update 路径）。
func occupiedExternalIDsByOthers(all []LocalDocView, sourceID uuid.UUID) map[string]uuid.UUID {
	occupied := make(map[string]uuid.UUID, len(all))
	for _, doc := range all {
		if doc.ExternalID == "" || doc.KbSourceID != nil && *doc.KbSourceID == sourceID {
			continue
		}
		owner := uuid.Nil
		if doc.KbSourceID != nil {
			owner = *doc.KbSourceID
		}
		occupied[doc.ExternalID] = owner
	}
	return occupied
}

// preregisterWebPageTokens 预注册本源既有页面 Token → file 节点 ID：
// 目录索引页（页面节点）可能是 folder 链上的父，更新/跳过路径也要可解析
// （快照按序处理只保证「先处理」，更新/跳过路径不新建节点、因此不会注册 Token）。
func (s *SourceSyncService) preregisterWebPageTokens(ctx context.Context, syncCtx *webSyncRunCtx, ownDocs []LocalDocView) error {
	ownDocIDs := make(map[uuid.UUID]bool, len(ownDocs))
	for _, doc := range ownDocs {
		ownDocIDs[doc.DocumentID] = true
	}
	return s.store.WithinWorkspace(ctx, syncCtx.kb.WorkspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
		nodes, err := tx.ListFileTreeNodes(txCtx, syncCtx.kb.ID)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			if node.ExternalID == "" || node.DocumentID == nil || !ownDocIDs[*node.DocumentID] {
				continue
			}
			syncCtx.tokenToNodeID[node.ExternalID] = node.ID
		}
		return nil
	})
}

// logWebWarnings 汇总记录 web 同步过程告警（快照/去重/过程三类），不改删除闸门。
func (s *SourceSyncService) logWebWarnings(workspaceID, sourceID uuid.UUID, snapshotWarnings, planWarnings, warnings []string) {
	for _, w := range snapshotWarnings {
		if strings.TrimSpace(w) != "" {
			s.logger.Warn("web 源同步 connector 告警", "workspace_id", workspaceID.String(), "kb_source_id", sourceID.String(), "warning", w)
		}
	}
	for _, w := range planWarnings {
		if strings.TrimSpace(w) != "" {
			s.logger.Warn("web 源同步 diff 告警", "workspace_id", workspaceID.String(), "kb_source_id", sourceID.String(), "warning", w)
		}
	}
	for _, w := range warnings {
		if strings.TrimSpace(w) != "" {
			s.logger.Warn("web 源同步过程告警", "workspace_id", workspaceID.String(), "kb_source_id", sourceID.String(), "warning", w)
		}
	}
}

// failedWebSyncResult 是致命失败的 SyncResult（ListTree/配置路径用，与飞书同形）。
func failedWebSyncResult() SyncResult {
	return SyncResult{Status: "failed", Complete: false, FinishedAt: time.Now().UTC()}
}

// writeWebSourceResultBestEffort 尽力写源级 SyncResult；写失败只记录日志，不掩盖原始错误链。
func (s *SourceSyncService) writeWebSourceResultBestEffort(ctx context.Context, workspaceID, kbID, sourceID uuid.UUID, result SyncResult) {
	if writeErr := s.store.UpdateSourceSyncResult(ctx, workspaceID, kbID, sourceID, result); writeErr != nil {
		s.logger.Error("回写 web 源同步失败结果失败",
			"workspace_id", workspaceID.String(),
			"kb_source_id", sourceID.String(),
			"error", writeErr.Error(),
		)
	}
}
