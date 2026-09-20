package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	webadapter "github.com/dajee/langhuan/internal/adapters/source/web"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	id "github.com/dajee/langhuan/internal/domain/id"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
	"github.com/dajee/langhuan/internal/ports/storage"
)

// 本文件承载 web 源同步的节点级应用逻辑（source_sync_web.go 的姊妹文件）：
// 快照单趟分派、页面建档/更新/重试、Fetch 失败分类消费与 source_meta 合并回写。
// 运行流程（latch/ListTree/cursor/结果写回）见 source_sync_web.go。

// applyWebSnapshotNodes 按快照顺序单趟处理 folder 与 page 节点（父先到，T7 编排义务）：
//   - folder：upsert 并注册 Token（共享 helper）；
//   - page：按 diff 计划分派 add/update/skip；不在计划内的节点（去重丢弃/空 token）
//     已由 diff 记 warning，静默跳过。
//
// 返回节点结果（喂 computeSafeCursor）、计数、过程告警与致命错误（源配置性 failed）。
func (s *SourceSyncService) applyWebSnapshotNodes(
	ctx context.Context,
	syncCtx *webSyncRunCtx,
	snapshot sourceport.TreeSnapshot,
	plan syncPlan,
	force bool,
) ([]nodeOutcome, syncCounts, []string, error) {
	counts := syncCounts{}
	outcomes := make([]nodeOutcome, 0, len(plan.ToAdd)+len(plan.ToUpdate)+len(plan.SkippedNodes))
	warnings := make([]string, 0)

	addSet := make(map[string]model.ExternalNode, len(plan.ToAdd))
	for _, node := range plan.ToAdd {
		addSet[node.Token] = node
	}
	updateSet := make(map[string]updateCandidate, len(plan.ToUpdate))
	for _, candidate := range plan.ToUpdate {
		updateSet[candidate.Remote.Token] = candidate
	}
	skippedSet := make(map[string]model.ExternalNode, len(plan.SkippedNodes))
	for _, node := range plan.SkippedNodes {
		skippedSet[node.Token] = node
	}

	for _, node := range snapshot.Nodes {
		if !node.HasDocument {
			// folder 节点：upsert + 注册 Token（parent-first 顺序保证父可解析）。
			s.upsertSyncFolder(ctx, syncCtx.syncRunCtx, node)
			continue
		}
		if candidate, isUpdate := updateSet[node.Token]; isUpdate {
			outcome, fatal, removal := s.updateWebSourceDocument(ctx, syncCtx, candidate, &counts, force, &warnings)
			if fatal {
				return outcomes, counts, warnings, fmt.Errorf("%w: 页面 %s 拉取不可服务（源配置性失败）", domainerrors.ErrValidation, node.Token)
			}
			if removal != nil {
				syncCtx.addGoneRemoval(*removal)
			}
			outcomes = append(outcomes, outcome)
			continue
		}
		if addNode, isAdd := addSet[node.Token]; isAdd {
			outcome, fatal := s.addWebSourceDocument(ctx, syncCtx, addNode, &counts, &warnings)
			if fatal {
				return outcomes, counts, warnings, fmt.Errorf("%w: 页面 %s 拉取不可服务（源配置性失败）", domainerrors.ErrValidation, node.Token)
			}
			outcomes = append(outcomes, outcome)
			continue
		}
		if skipNode, isSkipped := skippedSet[node.Token]; isSkipped {
			// 被 cursor 覆盖的节点视为成功 outcome，否则 watermark 前缀被阻断（spec 6.4）。
			outcomes = append(outcomes, nodeOutcome{Token: skipNode.Token, EditTime: skipNode.EditTime, Result: nodeResultSuccess})
		}
	}
	return outcomes, counts, warnings, nil
}

// addWebSourceDocument 处理 web 源新页面（spec §3.3）：
// 跨源冲突裁决 → 定位父节点 → Fetch → 内容 hash → revision-scoped raw Put →
// 事务创建 Document(kind=web, kb_source_id, source_meta)/FileTree/Revision/Job → parse 入队。
// 返回 fatal=true 表示源配置性失败（引擎未接入等），编排层终止整轮。
func (s *SourceSyncService) addWebSourceDocument(
	ctx context.Context,
	syncCtx *webSyncRunCtx,
	external model.ExternalNode,
	counts *syncCounts,
	warnings *[]string,
) (nodeOutcome, bool) {
	// 跨源冲突（spec §3.2）：目标 URL 已归属其它源/KB 级文档 → 跳过 + warning，
	// 不更新不删除（先到先得）。
	if owner, occupied := syncCtx.occupiedByOther[external.Token]; occupied {
		counts.skipped++
		*warnings = append(*warnings, fmt.Sprintf("%s: %s", webWarningURLOwnedByOtherSource, external.Token))
		s.logger.Warn("跳过被其它内容源占用的 web 页面 URL",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"knowledge_base_id", syncCtx.kb.ID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_id", external.Token,
			"owner_source_id", owner.String(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false
	}

	parentNodeID, ok := resolveParentNodeID(syncCtx.kb, external, syncCtx.tokenToNodeID)
	if !ok {
		counts.failed++
		s.logger.Error("新 web 页面父节点尚未建立",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_token", external.Token,
			"parent_token", external.ParentToken,
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	fetched, err := s.webConnector.Fetch(ctx, syncCtx.conn, external.Token, sourceport.FetchOptions{
		MaxContentBytes: s.maxContentBytes,
		SourceConfig:    syncCtx.config,
	})
	if err != nil {
		if handled, fatal := s.classifyWebFetchError(err, syncCtx, external, counts, warnings, nil); handled || fatal {
			return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, fatal
		}
		// gone（新增即远端删除）：本地无文档可删，跳过 + warning，不算失败。
		counts.skipped++
		*warnings = append(*warnings, fmt.Sprintf("remote_gone: %s", external.Token))
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false
	}

	markdown := fetched.Markdown
	if markdown == nil {
		markdown = []byte{}
	}
	// 应用层第二道超限检查（spec §7.2）。
	if s.maxContentBytes > 0 && int64(len(markdown)) > s.maxContentBytes {
		counts.oversize++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	contentHash := contentHashOf(markdown)
	title := resolveTitle(fetched, external)

	// web 页面身份（spec §3.3）：external_id = source_uri = 规范化 URL，kind='web'。
	document, err := model.NewDocumentIdentityWithExternal(
		syncCtx.kb.WorkspaceID, syncCtx.kb.ID, value.DocumentKindWeb, title,
		string(value.KbSourceKindWeb), external.Token, external.Token, nil,
	)
	if err != nil {
		counts.failed++
		s.logger.Warn("构造 web 页面文档身份失败", "workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(), "external_id", external.Token, "error", err.Error())
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}
	document.ContentHash = contentHash
	sourceID := syncCtx.source.ID
	document.KbSourceID = &sourceID
	document.SourceMeta = fetched.Meta

	revisionID := id.New()
	rawObject, err := s.rawStore.Put(ctx, storage.RawDocumentInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: document.ID, RevisionID: revisionID,
		FileName: title + ".md", ContentType: "text/markdown",
		Reader: bytes.NewReader(markdown), SizeBytes: int64(len(markdown)),
	})
	if err != nil {
		counts.failed++
		s.logger.Warn("持久化 web 页面原始内容失败", "workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(), "external_id", external.Token, "error", err.Error())
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	revision, err := model.NewDocumentRevisionWithID(revisionID, model.NewDocumentRevisionInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID, DocumentID: document.ID,
		// revision.kind 是 documents(ws,kb,id,kind) 复合外键的一部分：web 文档的
		// revision 必须 kind='web'（schema 000005 document_revisions_document_fk）。
		// Web Revision 校验禁止 OriginalFilename/FileType（领域模型 contract）。
		Kind: value.DocumentKindWeb, DocumentKind: value.DocumentKindWeb,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonCrawl,
		ContentType:   "text/markdown",
		RawStorageKey: rawObject.Key, SHA256: contentHash, SizeBytes: rawObject.SizeBytes,
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		counts.failed++
		s.logger.Warn("构造 web 页面修订失败", "workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(), "external_id", external.Token, "error", err.Error())
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		SourceConnectionID: syncCtx.connectionID,
		Type:               documentParseStartJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		counts.failed++
		s.logger.Warn("构造 web 页面解析任务失败", "workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(), "external_id", external.Token, "error", err.Error())
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}
	// worker 的 lineage 校验要求 parse Job 的 DB payload 携带目标 generation
	// （与 document_ingest.go 的文件上传路径同构），否则任务加载即失败。
	job.Payload["index_generation_id"] = syncCtx.generationID.String()

	var fileNode *model.FileTreeNode
	err = s.store.WithinWorkspace(ctx, syncCtx.kb.WorkspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
		if _, err := tx.GetKnowledgeBase(txCtx, syncCtx.kb.ID); err != nil {
			return err
		}
		documentID := document.ID
		node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
			ParentID: &parentNodeID, NodeType: value.FileTreeNodeFile,
			Name: title, DocumentID: &documentID, DocumentKind: value.DocumentKindWeb,
			ExternalID: external.Token,
		})
		if err != nil {
			return fmt.Errorf("构造 file 节点失败: %w", err)
		}
		fileNode = node
		return tx.CreateSyncedDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		// 并发竞争下同 KB 其它源先建了同 URL 文档（唯一索引冲突）：
		// 与预检同语义——先到先得，本源跳过 + warning（spec §3.2）。
		if errors.Is(err, domainerrors.ErrConflict) || errors.Is(err, domainerrors.ErrFileTreeNameConflict) {
			counts.skipped++
			*warnings = append(*warnings, fmt.Sprintf("%s: %s", webWarningURLOwnedByOtherSource, external.Token))
			s.logger.Warn("web 页面 URL 建档冲突，归属先到源",
				"workspace_id", syncCtx.kb.WorkspaceID.String(),
				"kb_source_id", syncCtx.source.ID.String(),
				"external_id", external.Token,
				"error", err.Error(),
			)
			return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false
		}
		counts.failed++
		s.logger.Error("web 页面建档事务失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_id", external.Token,
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}
	// 注册页面 Token：目录索引页可能是后续 folder 的父（T7 编排义务）。
	syncCtx.tokenToNodeID[external.Token] = fileNode.ID

	if err := s.enqueueParseJob(ctx, syncCtx.syncRunCtx, document.ID, revision.ID, syncCtx.generationID, job.ID); err != nil {
		counts.failed++
		s.logger.Error("入队 web 页面解析任务失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_id", external.Token,
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	counts.synced++
	return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false
}

// updateWebSourceDocument 处理 web 源已有页面（spec §6.3/§8）：
// 条件请求（IfNoneMatch/IfModifiedSince 来自 documents.source_meta）→ 304 按 unchanged
// 处理并合并回写 source_meta；200 正常抽取并按 hash/force 分流 skip/新 revision/retry。
// gone 分类返回 removal 候选（调用方在删除闸门内处理）；fatal 表示源配置性失败。
func (s *SourceSyncService) updateWebSourceDocument(
	ctx context.Context,
	syncCtx *webSyncRunCtx,
	candidate updateCandidate,
	counts *syncCounts,
	force bool,
	warnings *[]string,
) (nodeOutcome, bool, *LocalDocView) {
	external := candidate.Remote
	local := candidate.Local
	parentNodeID, ok := resolveParentNodeID(syncCtx.kb, external, syncCtx.tokenToNodeID)
	if !ok {
		// 父节点缺失：复用既有 parent（落库层会在 update 时覆盖），不阻断。
		parentNodeID = syncCtx.kb.FileTreeRootID
	}

	meta := sourceport.SourceMetaFromMap(local.SourceMeta)
	// 条件请求仅用于非强制增量（spec §8 第二级）；force 语义是「全量重抓 + hash 判断」
	//（既有机制），必须绕过 304 短路，否则 hash 未变也建新 revision 的 force 无法落地。
	// LastEngine 透传不受 force 影响：last_engine 短路是内容通道的沿用（spec §6.2），
	// 与增量判定无关；条件头与短路冲突时由 connector 侧裁决（短路命中时剥离条件头）。
	fetchOptions := sourceport.FetchOptions{
		MaxContentBytes: s.maxContentBytes,
		SourceConfig:    syncCtx.config,
		LastEngine:      meta.LastEngine,
	}
	if !force {
		fetchOptions.IfNoneMatch = meta.ETag
		fetchOptions.IfModifiedSince = meta.LastModified
	}
	fetched, err := s.webConnector.Fetch(ctx, syncCtx.conn, external.Token, fetchOptions)
	if err != nil {
		if handled, fatal := s.classifyWebFetchError(err, syncCtx, external, counts, warnings, &local); handled || fatal {
			return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, fatal, nil
		}
		// gone（更新时远端删除，spec §6.5）：完整 snapshot 下进 ToRemove（闸门约束）；
		// 本节点按成功 outcome 处理，让 cursor watermark 越过它。
		counts.skipped++
		*warnings = append(*warnings, fmt.Sprintf("remote_gone: %s", external.Token))
		s.logger.Warn("web 页面远端已删除，按源删除策略处理",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_id", external.Token,
			"document_id", local.DocumentID.String(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false, &local
	}

	if fetched.NotModified {
		// 304 → unchanged：刷新 last_fetch_at 等观测值，不建 revision（spec §8 第二级）。
		s.mergeWriteSourceMeta(ctx, syncCtx, local, fetched.Meta)
		counts.skipped++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false, nil
	}

	markdown := fetched.Markdown
	if markdown == nil {
		markdown = []byte{}
	}
	if s.maxContentBytes > 0 && int64(len(markdown)) > s.maxContentBytes {
		// 超限已有文档：保留旧版本（与飞书同语义）。
		counts.oversize++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false, nil
	}
	s.appendFetchedWarnings(external.Token, fetched.Warnings, warnings)

	contentHash := contentHashOf(markdown)
	title := resolveTitle(fetched, external)
	hashUnchanged := contentHash == local.ContentHash

	// spec §6.3/§8 分类：
	//   - hash 未变且无重试需求且非 force：跳过（不建 revision），但仍刷新 source_meta
	//     （etag/last_modified 可能轮转）；
	//   - hash 变化或 force：创建新 revision（同事务写 content_hash/source_meta）；
	//   - hash 未变但 RetryRequired：复用 failed/pending revision 重试（既有机制）。
	if hashUnchanged && !local.RetryRequired && !force {
		s.mergeWriteSourceMeta(ctx, syncCtx, local, fetched.Meta)
		counts.skipped++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false, nil
	}
	if !hashUnchanged || force {
		outcome, fatal := s.createWebUpdateRevision(ctx, syncCtx, external, local, title, parentNodeID, markdown, contentHash, fetched.Meta, counts, warnings)
		return outcome, fatal, nil
	}
	outcome := s.retryRevision(ctx, syncCtx.syncRunCtx, local, title, parentNodeID, markdown, contentHash, counts, external)
	return outcome, false, nil
}

// createWebUpdateRevision 内容变化或 force 时创建新 revision：与飞书路径同一 store 事务，
// 差异仅在于同事务写入 source_meta 并校验源级归属（UpdateDocumentRequest.KbSourceID）。
func (s *SourceSyncService) createWebUpdateRevision(
	ctx context.Context,
	syncCtx *webSyncRunCtx,
	external model.ExternalNode,
	local LocalDocView,
	title string,
	parentNodeID uuid.UUID,
	markdown []byte,
	contentHash string,
	freshMeta map[string]any,
	counts *syncCounts,
	warnings *[]string,
) (nodeOutcome, bool) {
	revisionID := id.New()
	rawObject, err := s.rawStore.Put(ctx, storage.RawDocumentInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: local.DocumentID, RevisionID: revisionID,
		FileName: title + ".md", ContentType: "text/markdown",
		Reader: bytes.NewReader(markdown), SizeBytes: int64(len(markdown)),
	})
	if err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	sourceID := syncCtx.source.ID
	result, err := s.store.CreateSyncedDocumentRevisionJob(ctx, UpdateDocumentRequest{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		ExternalID: external.Token, DocumentID: local.DocumentID, RevisionID: revisionID,
		KbSourceID:   &sourceID, // 同事务归属校验：归属其它源即冲突（跨源竞态兜底）。
		GenerationID: syncCtx.generationID,
		Title:        title, ParentNodeID: parentNodeID,
		RawStorageKey: rawObject.Key, SHA256: contentHash,
		SizeBytes: rawObject.SizeBytes, ContentType: "text/markdown", FileType: "markdown",
		Reason:     value.DocumentRevisionReasonCrawl,
		SourceMeta: mergeSourceMeta(local.SourceMeta, freshMeta),
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		if errors.Is(err, domainerrors.ErrConflict) {
			// 跨源竞态：URL 已被其它源占用（先到先得），本源跳过 + warning
			// url_owned_by_other_source（spec §3.2，与 add 路径同语义）。
			counts.skipped++
			*warnings = append(*warnings, fmt.Sprintf("%s: %s", webWarningURLOwnedByOtherSource, external.Token))
			s.logger.Warn("web 页面 URL 建档冲突，归属先到源",
				"workspace_id", syncCtx.kb.WorkspaceID.String(),
				"kb_source_id", syncCtx.source.ID.String(),
				"document_id", local.DocumentID.String(),
				"external_id", external.Token,
			)
			return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false
		}
		counts.failed++
		s.logger.Error("创建 web 页面新 revision 失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"document_id", local.DocumentID.String(),
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	if err := s.enqueueParseJob(ctx, syncCtx.syncRunCtx, local.DocumentID, revisionID, syncCtx.generationID, result.JobID); err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}, false
	}

	counts.synced++
	return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}, false
}

// classifyWebFetchError 统一消费 Fetch 错误（spec §6.5 失败分类映射）。
// 返回 handled=true 表示错误已归类处理（调用方直接返回）；fatal=true 表示源配置性
// 失败（引擎未接入等，ErrValidation），编排层终止整轮。gone 分类（远端删除）由调用方
// 按 add/update 语境分别处理，本函数不做 handled 短路（handled=false, fatal=false）。
//   - blocked/robots_disallowed/skipped/skipped_non_html → 跳过 + warning，非 retryable；
//   - oversize → 保留 oversize 计数与失败 outcome（Job 级可见）；
//   - retryable（网络/429/5xx/超时）→ 失败 outcome，下一轮由 RetryRequired 兜底
//     （LocalDocView.RetryRequired 由 store 按 revision/Job 状态推导）。
func (s *SourceSyncService) classifyWebFetchError(
	err error,
	syncCtx *webSyncRunCtx,
	external model.ExternalNode,
	counts *syncCounts,
	warnings *[]string,
	local *LocalDocView,
) (handled, fatal bool) {
	// 引擎未接入错误判源配置性 failed，不走 RetryRequired（T7 评审移交）。
	if errors.Is(err, domainerrors.ErrValidation) {
		counts.failed++
		s.logger.Error("web 页面拉取遭遇源配置性错误",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_token", external.Token,
			"error", err.Error(),
		)
		return true, true
	}
	switch webadapter.ClassifyError(err) {
	case webadapter.OutcomeGone:
		// 远端删除：调用方按 add/update 语境处理。
		return false, false
	case webadapter.OutcomeBlocked, webadapter.OutcomeRobotsDisallowed,
		webadapter.OutcomeSkipped, webadapter.OutcomeSkippedNonHTML:
		// 认证墙/robots 禁止/不可恢复 4xx/非 HTML：跳过 + warning，不算 retryable。
		counts.skipped++
		*warnings = append(*warnings, fmt.Sprintf("%s: %s", webadapter.ClassifyError(err), external.Token))
		fields := []any{
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_id", external.Token,
			"outcome", string(webadapter.ClassifyError(err)),
			"error", err.Error(),
		}
		if local != nil {
			fields = append(fields, "document_id", local.DocumentID.String())
		}
		s.logger.Warn("web 页面拉取被跳过（非重试类）", fields...)
		return true, false
	case webadapter.OutcomeOversize:
		counts.oversize++
		return true, false
	default:
		// 网络错误/429/5xx/超时（retryable）：失败 outcome，走 Job 级 RetryRequired 语义。
		counts.failed++
		fields := []any{
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"kb_source_id", syncCtx.source.ID.String(),
			"external_id", external.Token,
			"error", err.Error(),
		}
		if local != nil {
			fields = append(fields, "document_id", local.DocumentID.String())
		}
		s.logger.Error("拉取 web 页面失败（可重试类）", fields...)
		return true, false
	}
}

// mergeWriteSourceMeta 把本次抓取观测值合并进旧 source_meta 并回写（304 与 hash 未变路径）。
// 合并语义见 ports/source.SourceMeta.ToMap：零值省略，观测键覆盖旧值、未观测键保留。
func (s *SourceSyncService) mergeWriteSourceMeta(ctx context.Context, syncCtx *webSyncRunCtx, local LocalDocView, fresh map[string]any) {
	if len(fresh) == 0 {
		return
	}
	merged := mergeSourceMeta(local.SourceMeta, fresh)
	if err := s.store.UpdateDocumentSourceMeta(ctx, syncCtx.kb.WorkspaceID, local.DocumentID, merged); err != nil {
		s.logger.Warn("回写 web 页面 source_meta 失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"document_id", local.DocumentID.String(),
			"error", err.Error(),
		)
	}
}

// mergeSourceMeta 合并新旧 source_meta：本次观测键覆盖旧值，未观测键保留旧值。
func mergeSourceMeta(old, fresh map[string]any) map[string]any {
	merged := make(map[string]any, len(old)+len(fresh))
	for key, val := range old {
		merged[key] = val
	}
	for key, val := range fresh {
		merged[key] = val
	}
	return merged
}

// appendFetchedWarnings 把 FetchedDocument.Warnings（robots_unavailable、
// readability_fallback 等）并入同步告警，不影响内容采纳（T7 合同）。
func (s *SourceSyncService) appendFetchedWarnings(token string, fetchedWarnings []string, warnings *[]string) {
	for _, w := range fetchedWarnings {
		if strings.TrimSpace(w) == "" {
			continue
		}
		*warnings = append(*warnings, fmt.Sprintf("%s: %s", w, token))
	}
}
