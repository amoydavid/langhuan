package service

import (
	"bytes"
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
}

// SourceSyncService 编排一次飞书知识库的全量同步：
// 列举外部目录树 → 拉取 docx → 写入 File Document + Revision + Job → 入队解析任务。
type SourceSyncService struct {
	kbRepo      KnowledgeBaseSyncRepository
	connRepo    SourceConnectionRepository
	selector    *SourceConnectionSelector
	connector   sourceport.SourceConnector
	rawStore    storage.RawDocumentStore
	store       SourceSyncStore
	queue       queue.JobQueue
	logger      Logger
	resolveRoot RootResolver
}

// NewSourceSyncService 构造一个 SourceSyncService。
func NewSourceSyncService(deps SourceSyncServiceDeps) *SourceSyncService {
	logger := deps.Logger
	if logger == nil {
		logger = noopLogger{}
	}
	return &SourceSyncService{
		kbRepo:      deps.KnowledgeBaseRepository,
		connRepo:    deps.ConnectionRepository,
		selector:    deps.Selector,
		connector:   deps.Connector,
		rawStore:    deps.RawStore,
		store:       deps.Store,
		queue:       deps.Queue,
		logger:      logger,
		resolveRoot: deps.RootResolver,
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

// SyncKnowledgeBase 对单个飞书知识库执行一次（增量）同步：
// 列举外部目录树 → 按 sync_cursor 跳过未变更 docx → 拉取变更 docx → 写入 Document/Revision/Job → 入队；
// 同步后做删除检测（软删飞书侧已删除的文档）并回写新的 sync_cursor。
func (s *SourceSyncService) SyncKnowledgeBase(ctx context.Context, workspaceID, kbID uuid.UUID) error {
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

	// 读 sync_cursor：KB.SourceConfig["sync_cursor"]（RFC3339 字符串）；无则零值=全量。
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

	nodes, err := s.connector.ListTree(ctx, conn, root)
	if err != nil {
		return fmt.Errorf("列举飞书目录树失败: %w", err)
	}

	// 同步开始前，读出该 KB 下所有 external_id 非空的文档（含已软删的），用于删除检测。
	existingDocs, err := s.listDocumentsForDeleteDetection(ctx, workspaceID, kbID)
	if err != nil {
		return fmt.Errorf("读取 KB 已有文档失败: %w", err)
	}

	// 飞书 token → 本地 FileTreeNode ID。根节点的父 = KB.FileTreeRootID。
	tokenToNodeID := make(map[string]uuid.UUID)
	// aliveSet 记录本次同步在飞书侧仍存活的 docx external_id，用于删除检测。
	aliveSet := make(map[string]bool)
	state := syncNodeState{cursor: cursor}
	synced := 0
	failed := 0
	for _, external := range nodes {
		// 跟踪所有节点的 EditTime 最大值（含 folder 和被跳过的 docx）。
		if !external.EditTime.IsZero() && external.EditTime.After(state.maxEditTime) {
			state.maxEditTime = external.EditTime
		}
		// docx 节点无论是否跳过 Fetch，都视为存活（用于删除检测）。
		if external.HasDocument && external.ObjType == feishuObjTypeDocx {
			aliveSet[external.Token] = true
		}
		if err := s.syncNode(ctx, kb, conn, generationID, external, tokenToNodeID, &state); err != nil {
			failed++
			// 单个节点失败不中断整棵树；只记录错误（不含正文/凭证）。
			s.logger.Error("同步飞书节点失败",
				"workspace_id", workspaceID.String(),
				"knowledge_base_id", kbID.String(),
				"external_token", external.Token,
				"obj_type", external.ObjType,
				"error", err.Error(),
			)
			continue
		}
		if external.HasDocument && external.ObjType == feishuObjTypeDocx && !state.lastSkipped {
			synced++
		}
	}

	// 删除检测：DB 里存在、飞书树里不存在、且当前未软删的文档，软删。
	deleted, deleteErr := s.softDeleteMissingDocuments(ctx, workspaceID, existingDocs, aliveSet)
	if deleteErr != nil {
		// 删除检测失败不阻塞已成功的同步，只记录错误（不含正文/凭证）。
		s.logger.Error("飞书知识库删除检测失败",
			"workspace_id", workspaceID.String(),
			"knowledge_base_id", kbID.String(),
			"error", deleteErr.Error(),
		)
	}

	// 回写 sync_cursor（仅在 maxEditTime 非零值时）。
	if !state.maxEditTime.IsZero() {
		if cursorErr := s.kbRepo.UpdateSyncCursor(ctx, workspaceID, kbID, state.maxEditTime); cursorErr != nil {
			s.logger.Error("回写飞书同步游标失败",
				"workspace_id", workspaceID.String(),
				"knowledge_base_id", kbID.String(),
				"error", cursorErr.Error(),
			)
		}
	}

	s.logger.Info("飞书知识库同步完成",
		"workspace_id", workspaceID.String(),
		"knowledge_base_id", kbID.String(),
		"synced_documents", synced,
		"failed_nodes", failed,
		"deleted_documents", deleted,
		"total_nodes", len(nodes),
	)
	return nil
}

// syncNodeState 是节点循环的可变状态：cursor 用于增量跳过判断，
// maxEditTime 跟踪所有处理节点的 EditTime 最大值，lastSkipped 标记上一个节点是否被增量跳过。
type syncNodeState struct {
	cursor       time.Time
	maxEditTime  time.Time
	lastSkipped  bool
	skippedCount int
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

// listDocumentsForDeleteDetection 读出该 KB 下所有 external_id 非空的文档（含已软删的）。
func (s *SourceSyncService) listDocumentsForDeleteDetection(ctx context.Context, workspaceID, kbID uuid.UUID) ([]*model.Document, error) {
	var docs []*model.Document
	if err := s.store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
		var err error
		docs, err = tx.ListDocumentsByKB(txCtx, kbID)
		return err
	}); err != nil {
		return nil, err
	}
	return docs, nil
}

// softDeleteMissingDocuments 把 DB 里存在、飞书树里不存在（不在 aliveSet）、且当前未软删的文档软删。
// 返回实际软删的文档数。
func (s *SourceSyncService) softDeleteMissingDocuments(
	ctx context.Context,
	workspaceID uuid.UUID,
	existingDocs []*model.Document,
	aliveSet map[string]bool,
) (int, error) {
	deleted := 0
	for _, doc := range existingDocs {
		if doc.ExternalID == "" {
			continue
		}
		if aliveSet[doc.ExternalID] {
			continue
		}
		if doc.DeletedAt != nil {
			continue
		}
		docID := doc.ID
		if err := s.store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
			return tx.SoftDeleteDocument(txCtx, docID)
		}); err != nil {
			return deleted, fmt.Errorf("软删文档 %s 失败: %w", docID, err)
		}
		deleted++
		s.logger.Info("软删飞书侧已删除的文档",
			"workspace_id", workspaceID.String(),
			"document_id", docID.String(),
			"external_id", doc.ExternalID,
		)
	}
	return deleted, nil
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

// syncNode 处理单个外部节点：folder 落库；docx 拉取+落库+入队；其它文档类型跳过。
// state 跟踪增量游标与最大 EditTime；state.lastSkipped 在调用后被设置为该节点是否被增量跳过。
func (s *SourceSyncService) syncNode(
	ctx context.Context,
	kb *model.KnowledgeBase,
	conn model.SourceConnection,
	generationID uuid.UUID,
	external model.ExternalNode,
	tokenToNodeID map[string]uuid.UUID,
	state *syncNodeState,
) error {
	state.lastSkipped = false
	parentNodeID, ok := resolveParentNodeID(kb, external, tokenToNodeID)
	if !ok {
		return fmt.Errorf("父节点尚未建立: parent_token=%q", external.ParentToken)
	}

	switch {
	case !external.HasDocument:
		// folder 节点：单独事务建节点，记录到 map。
		folderNode, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
			ParentID: &parentNodeID, NodeType: value.FileTreeNodeFolder, Name: external.Title,
		})
		if err != nil {
			return fmt.Errorf("构造 folder 节点失败: %w", err)
		}
		if err := s.store.WithinWorkspace(ctx, kb.WorkspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
			if _, err := tx.GetKnowledgeBase(txCtx, kb.ID); err != nil {
				return err
			}
			return tx.CreateFileTreeNode(txCtx, folderNode)
		}); err != nil {
			return fmt.Errorf("写入 folder 节点失败: %w", err)
		}
		tokenToNodeID[external.Token] = folderNode.ID
		return nil

	case external.ObjType == feishuObjTypeDocx:
		// 增量跳过：EditTime 非零值且不超过 cursor 时跳过 Fetch（仍视为存活，已在循环外收集）。
		if !external.EditTime.IsZero() && !state.cursor.IsZero() && !external.EditTime.After(state.cursor) {
			state.lastSkipped = true
			state.skippedCount++
			s.logger.Info("跳过未变更节点",
				"workspace_id", kb.WorkspaceID.String(),
				"knowledge_base_id", kb.ID.String(),
				"external_token", external.Token,
			)
			return nil
		}
		return s.syncDocxNode(ctx, kb, conn, generationID, external, parentNodeID, tokenToNodeID)

	default:
		// 非文档类型（folder 已在上面处理）或非 docx 文档类型（sheet/bitable/...）：跳过。
		// 不记录正文，也不记录凭证。
		s.logger.Warn("跳过不支持的飞书节点类型",
			"workspace_id", kb.WorkspaceID.String(),
			"knowledge_base_id", kb.ID.String(),
			"external_token", external.Token,
			"obj_type", external.ObjType,
		)
		return nil
	}
}

// syncDocxNode 处理单个 docx 节点：Fetch → Put 原始文件 → 事务写库 → 入队解析。
func (s *SourceSyncService) syncDocxNode(
	ctx context.Context,
	kb *model.KnowledgeBase,
	conn model.SourceConnection,
	generationID uuid.UUID,
	external model.ExternalNode,
	parentNodeID uuid.UUID,
	tokenToNodeID map[string]uuid.UUID,
) error {
	fetched, err := s.connector.Fetch(ctx, conn, external.Token)
	if err != nil {
		return fmt.Errorf("拉取飞书文档失败: %w", err)
	}
	title := strings.TrimSpace(fetched.Title)
	if title == "" {
		title = strings.TrimSpace(external.Title)
	}
	if title == "" {
		title = external.Token
	}

	document, err := model.NewDocumentIdentityWithExternal(
		kb.WorkspaceID, kb.ID, value.DocumentKindFile, title, model.SourceProviderFeishu, "", external.Token, nil,
	)
	if err != nil {
		return fmt.Errorf("构造 Document 失败: %w", err)
	}

	markdown := fetched.Markdown
	if markdown == nil {
		markdown = []byte{}
	}
	rawObject, err := s.rawStore.Put(ctx, storage.RawDocumentInput{
		WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID, DocumentID: document.ID,
		FileName: title + ".md", ContentType: "text/markdown",
		Reader: bytes.NewReader(markdown), SizeBytes: int64(len(markdown)),
	})
	if err != nil {
		return fmt.Errorf("保存飞书文档原始内容失败: %w", err)
	}

	revision, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID, DocumentID: document.ID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: 1, Reason: value.DocumentRevisionReasonCrawl,
		OriginalFilename: title + ".md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: rawObject.Key, SHA256: rawObject.SHA256, SizeBytes: rawObject.SizeBytes,
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		return s.deleteRawAfterError(ctx, rawObject.Key, fmt.Errorf("构造 DocumentRevision 失败: %w", err))
	}

	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
		DocumentID: document.ID, DocumentRevisionID: revision.ID,
		SourceConnectionID: conn.ID,
		Type:               documentParseStartJobType, Status: value.JobStatusPending,
		Payload: map[string]any{
			"workspace_id":         kb.WorkspaceID.String(),
			"knowledge_base_id":    kb.ID.String(),
			"document_id":          document.ID.String(),
			"document_revision_id": revision.ID.String(),
		},
	})
	if err != nil {
		return s.deleteRawAfterError(ctx, rawObject.Key, fmt.Errorf("构造解析任务失败: %w", err))
	}
	job.Payload["job_id"] = job.ID.String()

	var fileNode *model.FileTreeNode
	err = s.store.WithinWorkspace(ctx, kb.WorkspaceID, func(txCtx context.Context, tx SourceSyncTx) error {
		if _, err := tx.GetKnowledgeBase(txCtx, kb.ID); err != nil {
			return err
		}
		documentID := document.ID
		node, err := model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
			ParentID: &parentNodeID, NodeType: value.FileTreeNodeFile,
			Name: title, DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
		})
		if err != nil {
			return fmt.Errorf("构造 file 节点失败: %w", err)
		}
		fileNode = node
		return tx.CreateSyncedDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
	})
	if err != nil {
		return s.deleteRawAfterError(ctx, rawObject.Key, err)
	}
	tokenToNodeID[external.Token] = fileNode.ID

	queuePayload, err := json.Marshal(map[string]string{
		"workspace_id":         kb.WorkspaceID.String(),
		"knowledge_base_id":    kb.ID.String(),
		"document_id":          document.ID.String(),
		"document_revision_id": revision.ID.String(),
		"generation_id":        generationID.String(),
		"job_id":               job.ID.String(),
	})
	if err != nil {
		return fmt.Errorf("编码解析任务 payload 失败: %w", err)
	}
	if _, err := s.queue.Enqueue(ctx, queue.JobRequest{
		Type: documentParseStartJobType, Payload: queuePayload,
		TaskID: queue.DocumentTaskID(documentParseStartJobType, kb.WorkspaceID, revision.ID, generationID),
	}); err != nil {
		cause := fmt.Errorf("入队飞书文档解析任务失败: %w", err)
		if persistErr := s.store.FailCreatedSync(ctx, kb.WorkspaceID, document.ID, revision.ID, job.ID, "enqueue_error", cause.Error()); persistErr != nil {
			return errors.Join(cause, fmt.Errorf("持久化飞书文档入队失败状态失败: %w", persistErr))
		}
		return cause
	}
	return nil
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

func (s *SourceSyncService) deleteRawAfterError(ctx context.Context, key string, primary error) error {
	if deleteErr := s.rawStore.Delete(ctx, key); deleteErr != nil {
		return fmt.Errorf("%w; 删除飞书原始文件失败: %w", primary, deleteErr)
	}
	return primary
}

// noopLogger 是 Logger 的空实现，避免 nil 解引用。
type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}
