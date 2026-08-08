package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	id "github.com/dajee/langhuan/internal/domain/id"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
	"github.com/dajee/langhuan/internal/ports/storage"
)

// Logger 是来源同步所需的最小结构化日志接口。
// 实现必须避免把 app_secret、tenant_access_token、文档正文等敏感数据写入 fields。
type Logger interface {
	Info(msg string, fields ...any)
	Warn(msg string, fields ...any)
	Error(msg string, fields ...any)
}

// RootResolver 把用户输入（飞书分享链接或裸 token）解析成同步根。
// 生产环境注入 feishu.ParseURL；测试可注入固定实现。
type RootResolver func(input string) (model.SyncRoot, error)

// SourceSyncServiceDeps 注入来源同步的全部依赖。
type SourceSyncServiceDeps struct {
	KnowledgeBaseRepository KnowledgeBaseSyncRepository
	ConnectionRepository    SourceConnectionRepository
	Selector                *SourceConnectionSelector
	Connector               sourceport.SourceConnector
	RawStore                storage.RawDocumentStore
	Store                   SourceSyncStore
	Queue                   queue.JobQueue
	Logger                  Logger
	// RootResolver 解析 KB.SourceConfig["url"] 为同步根；为 nil 时仅用 root_kind+root_token。
	RootResolver RootResolver
	// MaxContentBytes 限制单次同步文档拉取与落库的字节数；<=0 表示不设上限。
	// Task 7 默认沿用旧的无上限行为，Task 9 从 config 注入真实上限。
	MaxContentBytes int64
}

// SourceSyncService 编排一次飞书知识库的全量同步：
// 列举外部目录树 → 拉取 docx → 写入 File Document + Revision + Job → 入队解析任务。
type SourceSyncService struct {
	kbRepo          KnowledgeBaseSyncRepository
	connRepo        SourceConnectionRepository
	selector        *SourceConnectionSelector
	connector       sourceport.SourceConnector
	rawStore        storage.RawDocumentStore
	store           SourceSyncStore
	queue           queue.JobQueue
	logger          Logger
	resolveRoot     RootResolver
	maxContentBytes int64
}

// NewSourceSyncService 构造一个 SourceSyncService。
func NewSourceSyncService(deps SourceSyncServiceDeps) *SourceSyncService {
	logger := deps.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	return &SourceSyncService{
		kbRepo:          deps.KnowledgeBaseRepository,
		connRepo:        deps.ConnectionRepository,
		selector:        deps.Selector,
		connector:       deps.Connector,
		rawStore:        deps.RawStore,
		store:           deps.Store,
		queue:           deps.Queue,
		logger:          logger,
		resolveRoot:     deps.RootResolver,
		maxContentBytes: deps.MaxContentBytes,
	}
}

// EnqueueSync 为单个知识库创建一个 source_sync 任务并进入队列，返回创建的 Job。
// 任务按 KB 幂等去重（queue.SourceSyncTaskID），同一 KB 同时只允许一个同步任务在队列中。
// KB 的来源连接必须在 EnqueueSync 时已知（已读取 SourceConnectionID），写入 payload 以便 worker 校验。
func (s *SourceSyncService) EnqueueSync(ctx context.Context, workspaceID, kbID uuid.UUID) (*model.Job, error) {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return nil, fmt.Errorf("%w: workspace_id/knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	if s.store == nil || s.queue == nil {
		return nil, fmt.Errorf("%w: 来源同步依赖未配置", domainerrors.ErrValidation)
	}

	kb, err := s.kbRepo.Get(ctx, workspaceID, kbID)
	if err != nil {
		return nil, fmt.Errorf("读取知识库失败: %w", err)
	}
	if !kb.SourceType.IsFeishu() {
		return nil, fmt.Errorf("%w: 知识库来源类型 %q 不支持来源同步", domainerrors.ErrValidation, kb.SourceType)
	}

	var connID uuid.UUID
	if kb.SourceConnectionID != nil {
		connID = *kb.SourceConnectionID
	}

	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		SourceConnectionID: connID,
		Type:               model.SourceSyncJobType, Status: value.JobStatusPending,
		Payload: map[string]any{
			"workspace_id":      workspaceID.String(),
			"knowledge_base_id": kbID.String(),
			"connection_id":     connID.String(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("构造 source_sync Job 失败: %w", err)
	}
	job.Payload["job_id"] = job.ID.String()

	if err := s.store.CreateSourceSyncJob(ctx, job); err != nil {
		return nil, fmt.Errorf("持久化 source_sync Job 失败: %w", err)
	}

	payload, err := json.Marshal(sourceSyncTaskPayload{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		JobID: job.ID, ConnectionID: connID,
	})
	if err != nil {
		return nil, fmt.Errorf("编码 source_sync payload 失败: %w", err)
	}

	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: model.SourceSyncJobType, Payload: payload,
		TaskID: queue.SourceSyncTaskID(workspaceID, kbID),
	}); err != nil {
		return nil, fmt.Errorf("入队 source_sync 任务失败: %w", err)
	}

	s.logger.Info("已入队飞书知识库同步任务",
		"workspace_id", workspaceID.String(),
		"knowledge_base_id", kbID.String(),
		"job_id", job.ID.String(),
	)
	return job, nil
}

// sourceSyncTaskPayload 是 source_sync 任务的队列载荷（worker 与 HTTP handler 共用）。
type sourceSyncTaskPayload struct {
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	KnowledgeBaseID uuid.UUID `json:"knowledge_base_id"`
	JobID           uuid.UUID `json:"job_id"`
	ConnectionID    uuid.UUID `json:"connection_id,omitempty"`
}

// feishuObjTypeDocx 是飞书侧可同步文档的类型标识。
const feishuObjTypeDocx = "docx"

// SyncKnowledgeBase 对单个飞书知识库执行一次（增量）同步。
//
// 公开入口保持原签名；force 默认为 false。force 的真实值由 worker 从
// ConsumeForceLatch 读取后调用未导出的 syncKnowledgeBase（Task 9 完成 worker 接线）。
func (s *SourceSyncService) SyncKnowledgeBase(ctx context.Context, workspaceID, kbID uuid.UUID) error {
	return s.syncKnowledgeBase(ctx, workspaceID, kbID, false)
}

// syncKnowledgeBase 实现 spec 4 的数据流：
//
//	ListTree → 读本地投影 → diff → 应用 folder/document → 删除闸门 → 安全 cursor → SyncResult。
//
// force=true 时跳过内容 hash 去重，强制为每个匹配节点创建新 revision。
func (s *SourceSyncService) syncKnowledgeBase(ctx context.Context, workspaceID, kbID uuid.UUID, force bool) error {
	if workspaceID == uuid.Nil || kbID == uuid.Nil {
		return fmt.Errorf("%w: workspace_id/knowledge_base_id 不能为空", domainerrors.ErrValidation)
	}
	if s.connector == nil || s.store == nil || s.rawStore == nil || s.queue == nil || s.selector == nil {
		return fmt.Errorf("%w: 来源同步依赖未配置", domainerrors.ErrValidation)
	}

	kb, err := s.kbRepo.Get(ctx, workspaceID, kbID)
	if err != nil {
		return fmt.Errorf("读取知识库失败: %w", err)
	}
	if !kb.SourceType.IsFeishu() {
		return fmt.Errorf("%w: 知识库来源类型 %q 不支持来源同步", domainerrors.ErrValidation, kb.SourceType)
	}
	if kb.SourceConnectionID == nil || *kb.SourceConnectionID == uuid.Nil {
		return fmt.Errorf("%w: 飞书知识库未绑定来源连接", domainerrors.ErrValidation)
	}
	if kb.ActiveIndexGenerationID == nil || *kb.ActiveIndexGenerationID == uuid.Nil {
		return fmt.Errorf("%w: 知识库缺少 active IndexGeneration", domainerrors.ErrValidation)
	}
	generationID := *kb.ActiveIndexGenerationID

	cursor := readSyncCursor(kb.SourceConfig)

	selected, err := s.selector.Select(ctx, workspaceID, *kb.SourceConnectionID)
	if err != nil {
		return fmt.Errorf("解析来源连接凭证失败: %w", err)
	}

	root, err := s.resolveSyncRoot(kb.SourceConfig)
	if err != nil {
		return err
	}

	conn := model.SourceConnection{
		ID:          selected.ConnectionID,
		WorkspaceID: selected.WorkspaceID,
		Provider:    s.connector.Provider(),
		Name:        "",
		Config: map[string]any{
			"app_id": selected.AppID,
		},
		CredentialsCiphertext: append([]byte(nil), selected.AppSecret...),
		Status:                "active",
	}

	// ListTree：致命错误先写 failed 结果再返回 error。
	snapshot, err := s.connector.ListTree(ctx, conn, root)
	if err != nil {
		s.writeSyncResultBestEffort(ctx, workspaceID, kbID, SyncResult{
			Status: "failed", Complete: false, FinishedAt: time.Now().UTC(),
		})
		return fmt.Errorf("列举飞书目录树失败: %w", err)
	}

	// 读本地投影 + diff（纯函数，删除闸门由 snapshot.Complete 控制）。
	localDocs, err := s.store.ListSourceDocuments(ctx, kbID)
	if err != nil {
		return fmt.Errorf("读取本地来源文档投影失败: %w", err)
	}
	plan := diff(snapshot, localDocs, cursor, force)

	// tokenToNodeID 用于 folder/文档节点的父子定位。
	tokenToNodeID := make(map[string]uuid.UUID)
	syncCtx := &syncRunCtx{
		kb:            kb,
		conn:          conn,
		generationID:  generationID,
		tokenToNodeID: tokenToNodeID,
	}

	// 1) 先应用 folder 节点（按 external_id upsert），建立父子关系。
	folderErr := s.applyFolders(ctx, syncCtx, snapshot)
	if folderErr != nil && snapshot.Complete {
		// folder 删除失败不影响已成功的同步，但会让结果变 partial。
		s.logger.Error("飞书知识库 folder 清理失败",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"error", folderErr.Error(),
		)
	}

	// 2) 处理文档节点：add / update / retry / skip / oversize / unsupported。
	outcomes, counts := s.applyDocumentNodes(ctx, syncCtx, snapshot, plan, force)

	// 3) 删除闸门：仅完整 snapshot 才执行删除（spec 5.5）。
	deleted := 0
	cleanupPending := 0
	folderDeleteFailed := folderErr != nil
	if snapshot.Complete {
		deleted, cleanupPending = s.applyMissingDocuments(ctx, workspaceID, kbID, plan.ToRemove)
	}

	// 4) SyncResult 组装。
	result := SyncResult{
		Complete:          snapshot.Complete,
		SyncedDocuments:   counts.synced,
		SkippedDocuments:  counts.skipped + plan.Skipped,
		FailedDocuments:   counts.failed,
		OversizeDocuments: counts.oversize,
		UnsupportedNodes:  counts.unsupported,
		DeletedDocuments:  deleted,
		CleanupPending:    cleanupPending,
		FinishedAt:        time.Now().UTC(),
	}
	result.Status = s.classifySyncResult(snapshot, counts, folderDeleteFailed)
	// 收集 connector 告警 + 远端文档数告警（不改变删除闸门）。
	result = s.attachWarnings(result, snapshot, localDocs)

	if writeErr := s.store.UpdateSyncResult(ctx, workspaceID, kbID, result); writeErr != nil {
		s.logger.Error("回写飞书同步结果失败",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"error", writeErr.Error(),
		)
	}

	// 5) Cursor：仅完整 snapshot 才计算并推进安全 watermark。
	if snapshot.Complete {
		safeCursor := computeSafeCursor(snapshot, outcomes, cursor)
		if !safeCursor.IsZero() && safeCursor.After(cursor) {
			if cursorErr := s.kbRepo.UpdateSyncCursor(ctx, workspaceID, kbID, safeCursor); cursorErr != nil {
				s.logger.Error("回写飞书同步游标失败",
					"workspace_id", workspaceID.String(),
					"knowledge_base_id", kbID.String(),
					"error", cursorErr.Error(),
				)
			}
		}
	}

	s.logger.Info("飞书知识库同步完成",
		"workspace_id", workspaceID.String(),
		"knowledge_base_id", kbID.String(),
		"status", result.Status,
		"synced_documents", result.SyncedDocuments,
		"skipped_documents", result.SkippedDocuments,
		"failed_documents", result.FailedDocuments,
		"deleted_documents", result.DeletedDocuments,
		"complete", snapshot.Complete,
	)
	return nil
}

// syncRunCtx 聚合一次同步内多个节点共享的不可变上下文。
type syncRunCtx struct {
	kb            *model.KnowledgeBase
	conn          model.SourceConnection
	generationID  uuid.UUID
	tokenToNodeID map[string]uuid.UUID // external token -> folder node id
}

// syncCounts 累加文档节点的处理计数。
type syncCounts struct {
	synced      int
	skipped     int
	failed      int
	oversize    int
	unsupported int
}

// classifySyncResult 决定本次同步的 status：
// fatal=failed（已在 ListTree 路径处理），节点失败/不完整 snapshot/folder 删除失败=partial，全部成功=succeeded。
func (s *SourceSyncService) classifySyncResult(snapshot sourceport.TreeSnapshot, counts syncCounts, folderDeleteFailed bool) string {
	if counts.failed > 0 {
		return "partial"
	}
	if !snapshot.Complete {
		return "partial"
	}
	if folderDeleteFailed {
		return "partial"
	}
	return "succeeded"
}

// attachWarnings 附加 connector 告警，并在远端 docx 数低于本地未删除 docx 数一半时写高优先级告警。
// 该告警不改变 Complete 删除闸门（spec 7.3）。
func (s *SourceSyncService) attachWarnings(result SyncResult, snapshot sourceport.TreeSnapshot, localDocs []LocalDocView) SyncResult {
	// connector 告警在 SyncResult 中没有专门字段，记录到日志即可（spec 仅要求记录）。
	for _, w := range snapshot.Warnings {
		if strings.TrimSpace(w) != "" {
			s.logger.Warn("飞书同步 connector 告警", "warning", w)
		}
	}
	// 远端 docx 数低于本地未删除 docx 数一半 => 高优先级告警。
	remoteDocx := 0
	for _, node := range snapshot.Nodes {
		if node.HasDocument && node.ObjType == feishuObjTypeDocx {
			remoteDocx++
		}
	}
	localNonDeleted := 0
	for _, doc := range localDocs {
		if doc.ExternalID != "" && doc.DeletedAt == nil {
			localNonDeleted++
		}
	}
	if localNonDeleted > 0 && remoteDocx*2 < localNonDeleted {
		s.logger.Warn("飞书同步远端文档数显著少于本地，可能存在列举截断",
			"remote_docx_count", remoteDocx,
			"local_doc_count", localNonDeleted,
		)
	}
	return result
}

// writeSyncResultBestEffort 尽力写 SyncResult；写失败只记录日志，不掩盖原始错误链。
func (s *SourceSyncService) writeSyncResultBestEffort(ctx context.Context, workspaceID, kbID uuid.UUID, result SyncResult) {
	if writeErr := s.store.UpdateSyncResult(ctx, workspaceID, kbID, result); writeErr != nil {
		s.logger.Error("回写飞书同步失败结果失败",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"error", writeErr.Error(),
		)
	}
}

// applyFolders 按 external_id upsert 所有 folder 节点；
// 完整 snapshot 时删除远端已不存在的空 folder（深度优先，保留含子节点/文档的 folder）。
// partial snapshot 不删 folder。
func (s *SourceSyncService) applyFolders(ctx context.Context, syncCtx *syncRunCtx, snapshot sourceport.TreeSnapshot) error {
	// 先 upsert 所有 folder。
	for _, node := range snapshot.Nodes {
		if node.HasDocument {
			continue
		}
		if strings.TrimSpace(node.Token) == "" {
			continue
		}
		parentNodeID, ok := resolveParentNodeID(syncCtx.kb, node, syncCtx.tokenToNodeID)
		if !ok {
			continue
		}
		folder, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
			ParentID: &parentNodeID, NodeType: value.FileTreeNodeFolder, Name: node.Title,
			ExternalID: node.Token,
		})
		if err != nil {
			s.logger.Error("构造同步 folder 失败",
				"workspace_id", syncCtx.kb.WorkspaceID.String(),
				"external_token", node.Token,
				"error", err.Error(),
			)
			continue
		}
		if err := s.store.UpsertSourceFolder(ctx, folder); err != nil {
			s.logger.Error("写入同步 folder 失败",
				"workspace_id", syncCtx.kb.WorkspaceID.String(),
				"external_token", node.Token,
				"error", err.Error(),
			)
		}
		syncCtx.tokenToNodeID[node.Token] = folder.ID
	}

	if !snapshot.Complete {
		return nil
	}

	// 完整 snapshot：删除远端不存在的空 folder（深度优先，叶子优先）。
	remoteFolderTokens := make(map[string]bool, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if !node.HasDocument && node.Token != "" {
			remoteFolderTokens[node.Token] = true
		}
	}
	var firstErr error
	err := s.store.WithinWorkspace(ctx, syncCtx.kb.WorkspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
		all, err := tx.ListFileTreeNodes(txCtx, syncCtx.kb.ID)
		if err != nil {
			firstErr = err
			return err
		}
		nodeByID := make(map[uuid.UUID]*model.FileTreeNode, len(all))
		for _, n := range all {
			nodeByID[n.ID] = n
		}
		// 找出本地存在但远端不存在的 folder（按 external_id 对账）。
		var missing []*model.FileTreeNode
		for _, n := range all {
			if n.NodeType != value.FileTreeNodeFolder || n.ExternalID == "" {
				continue
			}
			if remoteFolderTokens[n.ExternalID] {
				continue
			}
			missing = append(missing, n)
		}
		// 计算每个 missing folder 的深度（从父链累计），用于深度优先排序。
		depthOf := func(id uuid.UUID) int {
			depth := 0
			current := id
			seen := map[uuid.UUID]bool{}
			for {
				node, ok := nodeByID[current]
				if !ok || node.ParentID == nil {
					break
				}
				parent := *node.ParentID
				if parent == syncCtx.kb.FileTreeRootID {
					break
				}
				if seen[current] {
					break // 防环
				}
				seen[current] = true
				depth++
				current = parent
				if depth > 64 {
					break
				}
			}
			return depth
		}
		sort.SliceStable(missing, func(i, j int) bool {
			return depthOf(missing[i].ID) > depthOf(missing[j].ID)
		})
		// 子节点集合：用于判断 folder 是否仍非空（含子 folder 或 file 文档）。
		hasChild := make(map[uuid.UUID]bool)
		for _, n := range all {
			if n.ParentID != nil && *n.ParentID != syncCtx.kb.FileTreeRootID {
				hasChild[*n.ParentID] = true
			}
		}
		// 已删除的节点实时维护，保证深度优先链式清理。
		deleted := make(map[uuid.UUID]bool)
		for _, folder := range missing {
			if deleted[folder.ID] {
				continue
			}
			if hasChild[folder.ID] {
				// 仍含子节点（file 文档或未删除的子 folder）：保留。
				continue
			}
			if delErr := tx.DeleteFileTreeNode(txCtx, folder.ID); delErr != nil {
				if firstErr == nil {
					firstErr = delErr
				}
				continue
			}
			deleted[folder.ID] = true
			// 父 folder 可能因这次删除而变空：标记 hasChild 需重新评估，
			// 但深度优先顺序已保证父在子之后处理，所以无需更新。
		}
		return nil
	})
	if err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// applyDocumentNodes 遍历 snapshot 文档节点，按类型分流处理：
// folder（跳过，已在 applyFolders 处理）/ docx（add/update/retry）/ 不支持类型（告警+计数）。
// 返回每个文档节点的处理结果（喂给 computeSafeCursor）与计数。
func (s *SourceSyncService) applyDocumentNodes(
	ctx context.Context,
	syncCtx *syncRunCtx,
	snapshot sourceport.TreeSnapshot,
	plan syncPlan,
	force bool,
) ([]nodeOutcome, syncCounts) {
	counts := syncCounts{}
	outcomes := make([]nodeOutcome, 0)

	// 处理 ToAdd。
	for _, node := range plan.ToAdd {
		outcome := s.addSourceDocument(ctx, syncCtx, node, &counts)
		outcomes = append(outcomes, outcome)
	}
	// 处理 ToUpdate。
	for _, candidate := range plan.ToUpdate {
		outcome := s.updateSourceDocument(ctx, syncCtx, candidate, &counts, force)
		outcomes = append(outcomes, outcome)
	}
	// 不支持的文档类型（sheet/bitable 等）：告警 + 计数，不算失败。
	for _, node := range snapshot.Nodes {
		if !node.HasDocument || node.ObjType == feishuObjTypeDocx {
			continue
		}
		counts.unsupported++
		s.logger.Warn("跳过不支持的飞书节点类型",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"knowledge_base_id", syncCtx.kb.ID.String(),
			"external_token", node.Token,
			"obj_type", node.ObjType,
		)
	}

	return outcomes, counts
}

// addSourceDocument 处理新文档（spec 6.2）：
// 预分配 revisionID → 有上限 Fetch → 内容 hash → 超限判断 → revision-scoped raw Put →
// tx 创建 Document/FileTree/Revision/Job → parse enqueue。超限不落库。
func (s *SourceSyncService) addSourceDocument(
	ctx context.Context,
	syncCtx *syncRunCtx,
	external model.ExternalNode,
	counts *syncCounts,
) nodeOutcome {
	parentNodeID, ok := resolveParentNodeID(syncCtx.kb, external, syncCtx.tokenToNodeID)
	if !ok {
		counts.failed++
		s.logger.Error("新文档父节点尚未建立",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"external_token", external.Token,
			"parent_token", external.ParentToken,
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	fetched, err := s.connector.Fetch(ctx, syncCtx.conn, external.Token, sourceport.FetchOptions{
		MaxContentBytes: s.maxContentBytes,
	})
	if err != nil {
		if errors.Is(err, sourceport.ErrSourceContentTooLarge) {
			counts.oversize++
			return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
		}
		counts.failed++
		s.logger.Error("拉取飞书新文档失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"external_token", external.Token,
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	markdown := fetched.Markdown
	if markdown == nil {
		markdown = []byte{}
	}
	// 应用层第二道超限检查（spec 7.2）。
	if s.maxContentBytes > 0 && int64(len(markdown)) > s.maxContentBytes {
		counts.oversize++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	contentHash := contentHashOf(markdown)
	title := resolveTitle(fetched, external)

	document, err := model.NewDocumentIdentityWithExternal(
		syncCtx.kb.WorkspaceID, syncCtx.kb.ID, value.DocumentKindFile, title,
		model.SourceProviderFeishu, "", external.Token, nil,
	)
	if err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}
	document.ContentHash = contentHash

	revisionID := id.New()
	rawObject, err := s.rawStore.Put(ctx, storage.RawDocumentInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: document.ID, RevisionID: revisionID,
		FileName: title + ".md", ContentType: "text/markdown",
		Reader: bytes.NewReader(markdown), SizeBytes: int64(len(markdown)),
	})
	if err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	revision, err := model.NewDocumentRevisionWithID(revisionID, model.NewDocumentRevisionInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonCrawl,
		OriginalFilename: title + ".md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: rawObject.Key, SHA256: contentHash, SizeBytes: rawObject.SizeBytes,
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		SourceConnectionID: syncCtx.conn.ID,
		Type:               documentParseStartJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	var fileNode *model.FileTreeNode
	err = s.store.WithinWorkspace(ctx, syncCtx.kb.WorkspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
		if _, err := tx.GetKnowledgeBase(txCtx, syncCtx.kb.ID); err != nil {
			return err
		}
		documentID := document.ID
		node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
			ParentID: &parentNodeID, NodeType: value.FileTreeNodeFile,
			Name: title, DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
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
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}
	syncCtx.tokenToNodeID[external.Token] = fileNode.ID

	if err := s.enqueueParseJob(ctx, syncCtx, document.ID, revision.ID, syncCtx.generationID, job.ID); err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	counts.synced++
	return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}
}

// updateSourceDocument 处理已有文档（spec 6.3）：
// 复用 Document → 有上限 Fetch → 内容 hash → 决定 skip / new revision / retry。
func (s *SourceSyncService) updateSourceDocument(
	ctx context.Context,
	syncCtx *syncRunCtx,
	candidate updateCandidate,
	counts *syncCounts,
	force bool,
) nodeOutcome {
	external := candidate.Remote
	local := candidate.Local
	parentNodeID, ok := resolveParentNodeID(syncCtx.kb, external, syncCtx.tokenToNodeID)
	if !ok {
		// 父节点缺失：复用既有 parent（落库层会在 update 时覆盖），不阻断。
		parentNodeID = syncCtx.kb.FileTreeRootID
	}

	fetched, err := s.connector.Fetch(ctx, syncCtx.conn, external.Token, sourceport.FetchOptions{
		MaxContentBytes: s.maxContentBytes,
	})
	if err != nil {
		if errors.Is(err, sourceport.ErrSourceContentTooLarge) {
			// 超限已有文档：保留旧版本。
			counts.oversize++
			return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
		}
		counts.failed++
		s.logger.Error("拉取飞书已有文档失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"external_token", external.Token,
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	markdown := fetched.Markdown
	if markdown == nil {
		markdown = []byte{}
	}
	if s.maxContentBytes > 0 && int64(len(markdown)) > s.maxContentBytes {
		counts.oversize++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	contentHash := contentHashOf(markdown)
	title := resolveTitle(fetched, external)

	hashUnchanged := contentHash == local.ContentHash

	// spec 6.3 分类：
	//   - hash 未变且无重试需求：跳过（不入队，不建 revision）。
	//   - hash 变化或 force：创建新 revision。
	//   - hash 未变但 RetryRequired：复用 failed/pending revision 重试。
	if hashUnchanged && !local.RetryRequired {
		counts.skipped++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}
	}
	if !hashUnchanged || force {
		return s.createUpdateRevision(ctx, syncCtx, external, local, title, parentNodeID, markdown, contentHash, counts)
	}
	return s.retryRevision(ctx, syncCtx, local, title, parentNodeID, contentHash, counts, external)
}

// createUpdateRevision 内容变化或 force 时创建新 revision（spec 6.3 更新路径）。
func (s *SourceSyncService) createUpdateRevision(
	ctx context.Context,
	syncCtx *syncRunCtx,
	external model.ExternalNode,
	local LocalDocView,
	title string,
	parentNodeID uuid.UUID,
	markdown []byte,
	contentHash string,
	counts *syncCounts,
) nodeOutcome {
	revisionID := id.New()
	rawObject, err := s.rawStore.Put(ctx, storage.RawDocumentInput{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: local.DocumentID, RevisionID: revisionID,
		FileName: title + ".md", ContentType: "text/markdown",
		Reader: bytes.NewReader(markdown), SizeBytes: int64(len(markdown)),
	})
	if err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	result, err := s.store.CreateSyncedDocumentRevisionJob(ctx, UpdateDocumentRequest{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		ExternalID: external.Token, DocumentID: local.DocumentID, RevisionID: revisionID,
		Title: title, ParentNodeID: parentNodeID,
		RawStorageKey: rawObject.Key, SHA256: contentHash,
		SizeBytes: rawObject.SizeBytes, ContentType: "text/markdown", FileType: "markdown",
		Reason: value.DocumentRevisionReasonCrawl,
	})
	if err != nil {
		s.deleteRawBestEffort(ctx, rawObject.Key)
		counts.failed++
		s.logger.Error("创建同步文档新 revision 失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"document_id", local.DocumentID.String(),
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	if err := s.enqueueParseJob(ctx, syncCtx, local.DocumentID, revisionID, syncCtx.generationID, result.JobID); err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	counts.synced++
	return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}
}

// retryRevision 内容未变但 RetryRequired 时复用最新未完成/失败的 source revision 重试（spec 6.3）。
// 复用的 revision id 来自 LocalDocView.LatestRevisionID；该字段始终指向最新的 crawl revision，
// 无论它是否完成，重试都基于它（不创建相同 hash 的新 revision）。
func (s *SourceSyncService) retryRevision(
	ctx context.Context,
	syncCtx *syncRunCtx,
	local LocalDocView,
	title string,
	parentNodeID uuid.UUID,
	contentHash string,
	counts *syncCounts,
	external model.ExternalNode,
) nodeOutcome {
	retryRevisionID := local.LatestRevisionID
	if retryRevisionID == nil || *retryRevisionID == uuid.Nil {
		// 没有记录的 source revision：回退到创建新 revision 路径，保证幂等。
		return s.createUpdateRevision(ctx, syncCtx, external, local, title, parentNodeID, []byte{}, contentHash, counts)
	}

	result, err := s.store.RetrySourceRevision(ctx, RetryDocumentRequest{
		WorkspaceID: syncCtx.kb.WorkspaceID, KnowledgeBaseID: syncCtx.kb.ID,
		DocumentID: local.DocumentID, RevisionID: *retryRevisionID,
		SHA256: contentHash, Title: title, ParentNodeID: parentNodeID,
	})
	if err != nil {
		counts.failed++
		s.logger.Error("重试同步文档 revision 失败",
			"workspace_id", syncCtx.kb.WorkspaceID.String(),
			"document_id", local.DocumentID.String(),
			"error", err.Error(),
		)
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	if err := s.enqueueParseJob(ctx, syncCtx, local.DocumentID, *retryRevisionID, syncCtx.generationID, result.JobID); err != nil {
		counts.failed++
		return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultFailure}
	}

	counts.synced++
	return nodeOutcome{Token: external.Token, EditTime: external.EditTime, Result: nodeResultSuccess}
}

// enqueueParseJob 入队 document_parse_start；失败时把刚创建的 revision/job 标记失败。

// applyMissingDocuments 执行删除策略（spec 5.5）。返回删除文档数与待清理对象数。
// 仅在 snapshot.Complete=true 时调用（删除闸门）。
func (s *SourceSyncService) applyMissingDocuments(
	ctx context.Context,
	workspaceID, kbID uuid.UUID,
	toRemove []LocalDocView,
) (int, int) {
	kb, err := s.kbRepo.Get(ctx, workspaceID, kbID)
	if err != nil {
		return 0, 0
	}
	policy := value.SourceDeletePolicyFromConfig(kb.SourceConfig["on_delete"])
	deleted := 0
	cleanupPending := 0
	for _, view := range toRemove {
		objects, delErr := s.store.DeleteSourceDocument(ctx, view.DocumentID, policy)
		if delErr != nil {
			s.logger.Error("删除同步文档失败",
				"workspace_id", workspaceID.String(),
				"document_id", view.DocumentID.String(),
				"policy", policy.String(),
				"error", delErr.Error(),
			)
			continue
		}
		deleted++
		cleanupPending += len(objects)
		s.logger.Info("删除飞书侧已不存在的文档",
			"workspace_id", workspaceID.String(),
			"document_id", view.DocumentID.String(),
			"external_id", view.ExternalID,
			"policy", policy.String(),
		)
	}
	return deleted, cleanupPending
}

// enqueueParseJob 入队 document_parse_start；失败时把刚创建的 revision/job 标记失败。
func (s *SourceSyncService) enqueueParseJob(
	ctx context.Context,
	syncCtx *syncRunCtx,
	documentID, revisionID, generationID, jobID uuid.UUID,
) error {
	queuePayload, err := json.Marshal(map[string]string{
		"workspace_id":         syncCtx.kb.WorkspaceID.String(),
		"knowledge_base_id":    syncCtx.kb.ID.String(),
		"document_id":          documentID.String(),
		"document_revision_id": revisionID.String(),
		"generation_id":        generationID.String(),
		"job_id":               jobID.String(),
	})
	if err != nil {
		return fmt.Errorf("编码解析任务 payload 失败: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: documentParseStartJobType, Payload: queuePayload,
		TaskID: queue.DocumentTaskID(documentParseStartJobType, syncCtx.kb.WorkspaceID, revisionID, generationID),
	}); err != nil {
		cause := fmt.Errorf("入队飞书文档解析任务失败: %w", err)
		if persistErr := s.store.FailCreatedSync(ctx, syncCtx.kb.WorkspaceID, documentID, revisionID, jobID, "enqueue_error", cause.Error()); persistErr != nil {
			return errors.Join(cause, fmt.Errorf("持久化飞书文档入队失败状态失败: %w", persistErr))
		}
		return cause
	}
	return nil
}

// contentHashOf 用 sha256 计算 markdown 的 hex 内容 hash（spec 6.3 去重依据）。
func contentHashOf(markdown []byte) string {
	sum := sha256.Sum256(markdown)
	return hex.EncodeToString(sum[:])
}

// resolveTitle 取 fetched 标题，回退到 external 标题，再回退到 token。
func resolveTitle(fetched model.FetchedDocument, external model.ExternalNode) string {
	title := strings.TrimSpace(fetched.Title)
	if title == "" {
		title = strings.TrimSpace(external.Title)
	}
	if title == "" {
		title = external.Token
	}
	return title
}

// resolveParentNodeID 解析某节点的父节点本地 ID。
// 根节点（ParentToken 为空）的父 = KB.FileTreeRootID。
func resolveParentNodeID(kb *model.KnowledgeBase, external model.ExternalNode, tokenToNodeID map[string]uuid.UUID) (uuid.UUID, bool) {
	if strings.TrimSpace(external.ParentToken) == "" {
		return kb.FileTreeRootID, true
	}
	parentID, ok := tokenToNodeID[external.ParentToken]
	return parentID, ok
}

// readSyncCursor 从 KB.SourceConfig["sync_cursor"]（RFC3339 字符串）解析出增量游标。
// 无字段或解析失败时返回零值（全量同步）。
func readSyncCursor(sourceConfig map[string]any) time.Time {
	if sourceConfig == nil {
		return time.Time{}
	}
	raw, _ := sourceConfig["sync_cursor"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// resolveSyncRoot 把 KB.SourceConfig 解析为同步根。
// 优先用 "url" 字段调 RootResolver（feishu.ParseURL）；否则用 root_kind + root_token，
// root_kind 缺省 wiki_node。
func (s *SourceSyncService) resolveSyncRoot(sourceConfig map[string]any) (model.SyncRoot, error) {
	if urlStr, _ := sourceConfig["url"].(string); strings.TrimSpace(urlStr) != "" {
		if s.resolveRoot != nil {
			root, err := s.resolveRoot(urlStr)
			if err != nil {
				return model.SyncRoot{}, fmt.Errorf("解析飞书同步根 URL 失败: %w", err)
			}
			return root, nil
		}
		// 未注入 RootResolver 时，把 url 当作裸 token 处理。
		return model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: strings.TrimSpace(urlStr)}, nil
	}

	token, _ := sourceConfig["root_token"].(string)
	if strings.TrimSpace(token) == "" {
		return model.SyncRoot{}, fmt.Errorf("%w: 知识库 SourceConfig 缺少 root_token/url", domainerrors.ErrValidation)
	}
	kind, _ := sourceConfig["root_kind"].(string)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = sourceport.SyncRootWikiNode
	}
	return model.SyncRoot{Kind: kind, Token: strings.TrimSpace(token)}, nil
}

// deleteRawBestEffort 尽力删除已写入的原始文件；失败只记录日志，不掩盖主错误。
func (s *SourceSyncService) deleteRawBestEffort(ctx context.Context, key string) {
	if key == "" {
		return
	}
	if err := s.rawStore.Delete(ctx, key); err != nil {
		s.logger.Error("删除飞书原始文件失败（best-effort）", "key", key, "error", err.Error())
	}
}

// noopLogger 是 Logger 的空实现，避免 nil 解引用。
type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
