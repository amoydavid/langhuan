package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/ports/queue"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
	"github.com/dajee/langhuan/internal/ports/storage"
)

// --- fakes -------------------------------------------------------------

type fakeSourceConnector struct {
	tree          []model.ExternalNode
	fetched       map[string]model.FetchedDocument
	fetchFn       func(externalID string) (model.FetchedDocument, error)
	provider      string
	listErr       error
	fetchedTokens []string // 记录所有 Fetch 调用的 externalID（顺序）
	complete      bool     // 控制 snapshot.Complete；默认 true。
	warnings      []string // 附加到 snapshot.Warnings。
	maxEditTime   time.Time
	// failOnce 控制"只失败一次"的 Fetch：externalID -> 互斥+计数。
	failOnceMu      sync.Mutex
	failOnceTokens  map[string]int // externalID -> 剩余失败次数
	fetchErrByToken map[string]error
}

func (c *fakeSourceConnector) ListTree(_ context.Context, _ model.SourceConnection, _ model.SyncRoot) (sourceport.TreeSnapshot, error) {
	if c.listErr != nil {
		return sourceport.TreeSnapshot{}, c.listErr
	}
	complete := true
	if c.complete {
		complete = true
	}
	return sourceport.TreeSnapshot{
		Nodes:       append([]model.ExternalNode(nil), c.tree...),
		Complete:    complete,
		Warnings:    append([]string(nil), c.warnings...),
		MaxEditTime: c.maxEditTime,
	}, nil
}

// failFetchOnce 让下一次针对 externalID 的 Fetch 返回 errFetchOnce（再下次成功）。
func (c *fakeSourceConnector) failFetchOnce(externalID string) {
	c.failOnceMu.Lock()
	defer c.failOnceMu.Unlock()
	if c.failOnceTokens == nil {
		c.failOnceTokens = map[string]int{}
	}
	c.failOnceTokens[externalID]++
}

func (c *fakeSourceConnector) Fetch(_ context.Context, _ model.SourceConnection, externalID string, _ sourceport.FetchOptions) (model.FetchedDocument, error) {
	c.fetchedTokens = append(c.fetchedTokens, externalID)
	c.failOnceMu.Lock()
	if c.failOnceTokens != nil && c.failOnceTokens[externalID] > 0 {
		c.failOnceTokens[externalID]--
		c.failOnceMu.Unlock()
		return model.FetchedDocument{}, fmt.Errorf("fetch not stubbed for %s", externalID)
	}
	if err, ok := c.fetchErrByToken[externalID]; ok {
		c.failOnceMu.Unlock()
		return model.FetchedDocument{}, err
	}
	c.failOnceMu.Unlock()
	if c.fetchFn != nil {
		return c.fetchFn(externalID)
	}
	if doc, ok := c.fetched[externalID]; ok {
		return doc, nil
	}
	return model.FetchedDocument{}, fmt.Errorf("fetch not stubbed for %s", externalID)
}

func (c *fakeSourceConnector) fetchedTokenCount() map[string]int {
	counts := map[string]int{}
	for _, token := range c.fetchedTokens {
		counts[token]++
	}
	return counts
}

func (c *fakeSourceConnector) Provider() string {
	if c.provider == "" {
		return model.SourceProviderFeishu
	}
	return c.provider
}

// fakeSourceSyncStore 用内存 map 模拟事务。
type fakeSourceSyncStore struct {
	mu                 sync.Mutex
	kb                 *model.KnowledgeBase
	nodes              map[uuid.UUID]*model.FileTreeNode
	documents          map[uuid.UUID]*model.Document
	revisions          map[uuid.UUID]*model.DocumentRevision
	jobs               map[uuid.UUID]*model.Job
	docJobByExternal   map[string]uuid.UUID // external token -> document id
	folderByExternal   map[string]uuid.UUID // external token -> folder node id
	createDocCalls     int
	createFolderCalls  int
	createDocDoc       *model.Document
	createDocNode      *model.FileTreeNode
	createDocRevision  *model.DocumentRevision
	createDocJob       *model.Job
	failCalls          int
	lastFailErrorClass string
	lastFailMessage    string
	failErr            error
	activeCount        int
	activeCountErr     error
	// 增量同步 / 删除检测支持
	existingDocuments []*model.Document // ListDocumentsByKB 返回的预置文档（含已软删的）
	listDocsErr       error
	softDeleted       []uuid.UUID // 记录被 SoftDeleteDocument 调用的 document id
	softDeleteErr     error
	// Task 6 新增方法的最小可注入状态
	upsertFolderCalls   int
	upsertFolderErr     error
	createRevCalls      int
	createRevErr        error
	retryCalls          int
	retryErr            error
	deleteCalls         []deleteCall
	deleteErr           error
	activeSyncJob       *model.Job
	forceLatch          bool
	requestSyncErr      error
	consumeLatchErr     error
	finalizeErr         error
	failEnqueueErr      error
	lastSyncResult      *SyncResult
	updateSyncResultErr error
	// kbIDToWorkspace 反查 ListSourceDocuments 用到的 workspace（默认取 kb.WorkspaceID）。
	kbIDToWorkspace map[uuid.UUID]uuid.UUID
}

// deleteCall 记录一次 DeleteSourceDocument 调用的参数，供断言。
type deleteCall struct {
	DocumentID uuid.UUID
	Policy     value.SourceDeletePolicy
}

func newFakeSourceSyncStore(kb *model.KnowledgeBase) *fakeSourceSyncStore {
	return &fakeSourceSyncStore{
		kb:               kb,
		nodes:            map[uuid.UUID]*model.FileTreeNode{},
		documents:        map[uuid.UUID]*model.Document{},
		revisions:        map[uuid.UUID]*model.DocumentRevision{},
		jobs:             map[uuid.UUID]*model.Job{},
		docJobByExternal: map[string]uuid.UUID{},
		folderByExternal: map[string]uuid.UUID{},
		kbIDToWorkspace:  map[uuid.UUID]uuid.UUID{},
	}
}

func (s *fakeSourceSyncStore) WithinWorkspace(
	ctx context.Context,
	_ uuid.UUID,
	fn func(context.Context, SourceSyncTx) error,
) error {
	return fn(ctx, s)
}

func (s *fakeSourceSyncStore) GetKnowledgeBase(_ context.Context, id uuid.UUID) (*model.KnowledgeBase, error) {
	if s.kb == nil || s.kb.ID != id {
		return nil, domainerrors.ErrNotFound
	}
	return s.kb, nil
}

func (s *fakeSourceSyncStore) GetFileTreeNodeForUpdate(_ context.Context, id uuid.UUID) (*model.FileTreeNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.nodes[id]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	return node, nil
}

func (s *fakeSourceSyncStore) CreateFileTreeNode(_ context.Context, node *model.FileTreeNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = node
	s.createFolderCalls++
	return nil
}

// ListFileTreeNodes 返回预置 nodes 中属于该 KB 的节点。
func (s *fakeSourceSyncStore) ListFileTreeNodes(_ context.Context, kbID uuid.UUID) ([]*model.FileTreeNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes := make([]*model.FileTreeNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		if kbID == uuid.Nil || n.KnowledgeBaseID == kbID {
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

// DeleteFileTreeNode 删除一个 file tree 节点。
func (s *fakeSourceSyncStore) DeleteFileTreeNode(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if node, ok := s.nodes[id]; ok {
		if node.ExternalID != "" && s.folderByExternal[node.ExternalID] == id {
			delete(s.folderByExternal, node.ExternalID)
		}
	}
	delete(s.nodes, id)
	return nil
}

func (s *fakeSourceSyncStore) CreateSyncedDocumentNodeRevisionAndJob(
	_ context.Context,
	document *model.Document,
	node *model.FileTreeNode,
	revision *model.DocumentRevision,
	job *model.Job,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents[document.ID] = document
	s.revisions[revision.ID] = revision
	s.jobs[job.ID] = job
	s.nodes[node.ID] = node
	s.createDocCalls++
	s.createDocDoc, s.createDocNode, s.createDocRevision, s.createDocJob = document, node, revision, job
	if document.ExternalID != "" {
		s.docJobByExternal[document.ExternalID] = document.ID
	}
	return nil
}

// ListDocumentsByKB 返回预置的 existingDocuments（含已软删的）。
func (s *fakeSourceSyncStore) ListDocumentsByKB(_ context.Context, _ uuid.UUID) ([]*model.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listDocsErr != nil {
		return nil, s.listDocsErr
	}
	return append([]*model.Document(nil), s.existingDocuments...), nil
}

// SoftDeleteDocument 把文档标记为已软删（设置 DeletedAt），并记录调用。
func (s *fakeSourceSyncStore) SoftDeleteDocument(_ context.Context, documentID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.softDeleteErr != nil {
		return s.softDeleteErr
	}
	s.softDeleted = append(s.softDeleted, documentID)
	if doc, ok := s.documents[documentID]; ok && doc.DeletedAt == nil {
		now := time.Now().UTC()
		doc.DeletedAt = &now
		doc.Status = value.DocumentStatusDeleted
	}
	// 同时更新预置 existingDocuments 中的副本状态。
	for _, doc := range s.existingDocuments {
		if doc.ID == documentID && doc.DeletedAt == nil {
			now := time.Now().UTC()
			doc.DeletedAt = &now
		}
	}
	return nil
}

func (s *fakeSourceSyncStore) FailCreatedSync(
	_ context.Context,
	_, documentID, revisionID, jobID uuid.UUID,
	errorClass, message string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCalls++
	s.lastFailErrorClass = errorClass
	s.lastFailMessage = message
	if s.failErr != nil {
		return s.failErr
	}
	doc, ok := s.documents[documentID]
	if !ok || s.revisions[revisionID] == nil || s.jobs[jobID] == nil {
		return domainerrors.ErrNotFound
	}
	doc.Status = value.DocumentStatusFailed
	s.revisions[revisionID].Status = value.DocumentRevisionFailed
	s.revisions[revisionID].ErrorClass = errorClass
	s.revisions[revisionID].ErrorMessage = message
	s.jobs[jobID].Status = value.JobStatusFailed
	s.jobs[jobID].ErrorClass = errorClass
	s.jobs[jobID].ErrorMessage = message
	return nil
}

func (s *fakeSourceSyncStore) CreateSourceSyncJob(_ context.Context, job *model.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return nil
}

// CountActiveByConnection 供 Meta Scheduler 限流；fake 返回可注入的固定值。
func (s *fakeSourceSyncStore) CountActiveByConnection(_ context.Context, _ uuid.UUID, _ uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCountErr != nil {
		return 0, s.activeCountErr
	}
	return s.activeCount, nil
}

// 以下方法覆盖 Task 6 新增的 SourceSyncStore 契约。fakes 尽量忠实于 DB 层语义，
// 以便服务层在不连真实数据库的情况下测试编排逻辑。

// ListSourceDocuments 基于内存中已持久化的 documents/revisions 计算 LocalDocView，
// 并按 DB 层 computeRetryRequired 的语义推导 RetryRequired。
// 预置的 existingDocuments 会被合并进结果（兼容旧测试种子）。
func (s *fakeSourceSyncStore) ListSourceDocuments(_ context.Context, kbID uuid.UUID) ([]LocalDocView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listDocsErr != nil {
		return nil, s.listDocsErr
	}
	seen := make(map[uuid.UUID]bool)
	views := make([]LocalDocView, 0)
	// 先取内存中真实持久化的文档（来自 CreateSyncedDocumentNodeRevisionAndJob / CreateSyncedDocumentRevisionJob）。
	for _, doc := range s.documents {
		if doc.KnowledgeBaseID != kbID || doc.ExternalID == "" {
			continue
		}
		if seen[doc.ID] {
			continue
		}
		seen[doc.ID] = true
		views = append(views, s.viewForDocument(doc))
	}
	// 再合并预置 existingDocuments（用于纯删除检测/重试场景的种子）。
	for _, doc := range s.existingDocuments {
		if doc.KnowledgeBaseID != kbID || doc.ExternalID == "" {
			continue
		}
		if seen[doc.ID] {
			continue
		}
		seen[doc.ID] = true
		views = append(views, s.viewForDocument(doc))
	}
	return views, nil
}

// viewForDocument 聚合单个文档的最新 crawl revision 与 parse Job 状态。
func (s *fakeSourceSyncStore) viewForDocument(doc *model.Document) LocalDocView {
	view := LocalDocView{
		DocumentID:  doc.ID,
		ExternalID:  doc.ExternalID,
		ContentHash: doc.ContentHash,
		Status:      doc.Status,
		DeletedAt:   doc.DeletedAt,
	}
	// 找该文档最新的 crawl revision（按 revision_no 倒序）。
	var latest *model.DocumentRevision
	for _, rev := range s.revisions {
		if rev.DocumentID == doc.ID && rev.Reason == value.DocumentRevisionReasonCrawl {
			if latest == nil || rev.RevisionNo > latest.RevisionNo {
				latest = rev
			}
		}
	}
	failedJob := false
	if latest != nil {
		view.RevisionNo = latest.RevisionNo
		view.ActiveRevisionID = nil // 简化：fake 不维护 active 切换
		latestID := latest.ID
		view.LatestRevisionID = &latestID
		rid := latest.ID
		view.RetryRequired = doc.Status == value.DocumentStatusFailed ||
			latest.Status != value.DocumentRevisionReady
		for _, job := range s.jobs {
			if job.DocumentRevisionID == rid && job.Status == value.JobStatusFailed {
				failedJob = true
			}
		}
		if failedJob {
			view.RetryRequired = true
		}
	}
	return view
}

// UpsertSourceFolder 按 external_id 去重：存在则更新 parent/name，不存在则插入。
func (s *fakeSourceSyncStore) UpsertSourceFolder(_ context.Context, folder *model.FileTreeNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upsertFolderErr != nil {
		return s.upsertFolderErr
	}
	s.upsertFolderCalls++
	if folder == nil {
		return nil
	}
	if existingID, ok := s.folderByExternal[folder.ExternalID]; ok {
		if node, ok := s.nodes[existingID]; ok {
			node.ParentID = folder.ParentID
			node.Name = folder.Name
			return nil
		}
	}
	s.nodes[folder.ID] = folder
	s.folderByExternal[folder.ExternalID] = folder.ID
	return nil
}

// CreateSyncedDocumentRevisionJob 是更新路径的忠实 fake：
// 按 external_id 复用既有 Document，revision_no = max+1，写入新 Revision + Job，
// 更新 Document content_hash/status/title/deleted_at，更新 FileTreeNode parent/name。
func (s *fakeSourceSyncStore) CreateSyncedDocumentRevisionJob(_ context.Context, request UpdateDocumentRequest) (*SyncWriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createRevErr != nil {
		return nil, s.createRevErr
	}
	s.createRevCalls++
	doc, ok := s.documents[request.DocumentID]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	// revision_no = max + 1。
	var maxNo int64
	for _, rev := range s.revisions {
		if rev.DocumentID == request.DocumentID && rev.RevisionNo > maxNo {
			maxNo = rev.RevisionNo
		}
	}
	revisionNo := maxNo + 1
	revision, err := model.NewDocumentRevisionWithID(request.RevisionID, model.NewDocumentRevisionInput{
		WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
		DocumentID: request.DocumentID, Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: revisionNo, Reason: request.Reason,
		OriginalFilename: request.Title, FileType: request.FileType, ContentType: request.ContentType,
		RawStorageKey: request.RawStorageKey, SHA256: request.SHA256, SizeBytes: request.SizeBytes,
		ProcessingVersion: model.CurrentProcessingVersion, Status: value.DocumentRevisionPending,
	})
	if err != nil {
		return nil, err
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
		DocumentID: request.DocumentID, DocumentRevisionID: request.RevisionID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if err != nil {
		return nil, err
	}
	s.revisions[revision.ID] = revision
	s.jobs[job.ID] = job
	// 更新 Document：刷新 hash/status/title，复活（清 deleted_at）。
	now := time.Now().UTC()
	doc.Title = request.Title
	doc.ContentHash = request.SHA256
	doc.Status = value.DocumentStatusPending
	doc.UpdatedAt = now
	doc.DeletedAt = nil
	// 更新 FileTreeNode（按 document_id 定位 file 节点）。
	for _, node := range s.nodes {
		if node.DocumentID != nil && *node.DocumentID == request.DocumentID {
			node.ParentID = &request.ParentNodeID
			node.Name = request.Title
			node.UpdatedAt = now
		}
	}
	return &SyncWriteResult{
		DocumentID: request.DocumentID,
		RevisionID: request.RevisionID,
		RevisionNo: revisionNo,
		JobID:      job.ID,
		RawKey:     request.RawStorageKey,
	}, nil
}

// RetrySourceRevision 复用最新未完成/失败的 revision：重置 status、清错误、新建幂等 parse Job。
func (s *fakeSourceSyncStore) RetrySourceRevision(_ context.Context, request RetryDocumentRequest) (*SyncWriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retryErr != nil {
		return nil, s.retryErr
	}
	s.retryCalls++
	rev, ok := s.revisions[request.RevisionID]
	if !ok {
		return nil, domainerrors.ErrNotFound
	}
	rev.Status = value.DocumentRevisionPending
	rev.ErrorClass = ""
	rev.ErrorMessage = ""
	rev.CompletedAt = nil
	if strings.TrimSpace(request.SHA256) != "" {
		rev.SHA256 = request.SHA256
	}
	revisionNo := rev.RevisionNo
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: request.WorkspaceID, KnowledgeBaseID: request.KnowledgeBaseID,
		DocumentID: request.DocumentID, DocumentRevisionID: request.RevisionID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if err != nil {
		return nil, err
	}
	s.jobs[job.ID] = job
	if doc, ok := s.documents[request.DocumentID]; ok {
		doc.Status = value.DocumentStatusPending
		doc.DeletedAt = nil
	}
	return &SyncWriteResult{
		DocumentID: request.DocumentID,
		RevisionID: request.RevisionID,
		RevisionNo: revisionNo,
		JobID:      job.ID,
	}, nil
}

func (s *fakeSourceSyncStore) DeleteSourceDocument(_ context.Context, documentID uuid.UUID, policy value.SourceDeletePolicy) ([]CleanupObject, []*model.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return nil, nil, s.deleteErr
	}
	s.deleteCalls = append(s.deleteCalls, deleteCall{DocumentID: documentID, Policy: policy})
	if doc, ok := s.documents[documentID]; ok && doc.DeletedAt == nil {
		now := time.Now().UTC()
		doc.DeletedAt = &now
		doc.Status = value.DocumentStatusDeleted
	}
	return nil, nil, nil
}

func (s *fakeSourceSyncStore) RequestSourceSync(_ context.Context, _, _, _ uuid.UUID, _ bool) (*model.Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requestSyncErr != nil {
		return nil, false, s.requestSyncErr
	}
	if s.activeSyncJob != nil {
		return s.activeSyncJob, false, nil
	}
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: s.kb.WorkspaceID, KnowledgeBaseID: s.kb.ID,
		Type: model.SourceSyncJobType, Status: value.JobStatusPending,
	})
	if err != nil {
		return nil, false, err
	}
	s.activeSyncJob = job
	s.jobs[job.ID] = job
	return job, true, nil
}

func (s *fakeSourceSyncStore) ConsumeForceLatch(_ context.Context, _, _, _ uuid.UUID) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumeLatchErr != nil {
		return false, s.consumeLatchErr
	}
	v := s.forceLatch
	s.forceLatch = false
	return v, nil
}

func (s *fakeSourceSyncStore) FinalizeSourceSyncJob(_ context.Context, _, _, jobID uuid.UUID, status value.JobStatus, _ string) (*model.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalizeErr != nil {
		return nil, s.finalizeErr
	}
	if job, ok := s.jobs[jobID]; ok {
		job.Status = status
	}
	s.activeSyncJob = nil
	if s.forceLatch {
		next, err := model.NewJob(model.NewJobInput{
			WorkspaceID: s.kb.WorkspaceID, KnowledgeBaseID: s.kb.ID,
			Type: model.SourceSyncJobType, Status: value.JobStatusPending,
		})
		if err != nil {
			return nil, err
		}
		s.forceLatch = false
		s.jobs[next.ID] = next
		s.activeSyncJob = next
		return next, nil
	}
	return nil, nil
}

func (s *fakeSourceSyncStore) FailSourceSyncEnqueue(_ context.Context, _, _, jobID uuid.UUID, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failEnqueueErr != nil {
		return s.failEnqueueErr
	}
	if job, ok := s.jobs[jobID]; ok {
		job.Status = value.JobStatusFailed
		job.ErrorMessage = message
	}
	return nil
}

func (s *fakeSourceSyncStore) UpdateSyncResult(_ context.Context, _, _ uuid.UUID, result SyncResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSyncResult = &result
	return s.updateSyncResultErr
}

// fakeSyncRawStore 返回基于 document id 的固定 key，记录 put/delete。
type fakeSyncRawStore struct {
	puts    []storage.RawDocumentInput
	deletes []string
	putErr  error
}

func (s *fakeSyncRawStore) Put(_ context.Context, input storage.RawDocumentInput) (*storage.RawDocumentObject, error) {
	if s.putErr != nil {
		return nil, s.putErr
	}
	body, _ := io.ReadAll(input.Reader)
	s.puts = append(s.puts, storage.RawDocumentInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		DocumentID: input.DocumentID, FileName: input.FileName, ContentType: input.ContentType,
		Reader: bytes.NewReader(body), SizeBytes: int64(len(body)),
	})
	return &storage.RawDocumentObject{
		Key: "raw/" + input.DocumentID.String(), SizeBytes: int64(len(body)),
		SHA256: "sha-" + input.DocumentID.String(), ContentType: input.ContentType,
	}, nil
}

func (s *fakeSyncRawStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *fakeSyncRawStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	return nil
}

// fakeSyncQueue 记录入队请求。
type fakeSyncQueue struct {
	requests []queue.JobRequest
	err      error
}

func (q *fakeSyncQueue) Enqueue(_ context.Context, req queue.JobRequest) (*queue.JobHandle, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.requests = append(q.requests, req)
	return &queue.JobHandle{ID: "queued"}, nil
}

// recordingLogger 记录 Warn/Error/Info 调用用于断言。
type recordingLogger struct {
	mu       sync.Mutex
	warnings []logEntry
	errors   []logEntry
	infos    []logEntry
}

type logEntry struct {
	msg    string
	fields []any
}

func (l *recordingLogger) Info(msg string, fields ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, logEntry{msg, fields})
}

func (l *recordingLogger) Warn(msg string, fields ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, logEntry{msg, fields})
}

func (l *recordingLogger) Error(msg string, fields ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, logEntry{msg, fields})
}

func (l *recordingLogger) warningForObjType(objType string) *logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.warnings {
		for f := 0; f+1 < len(l.warnings[i].fields); f += 2 {
			if k, _ := l.warnings[i].fields[f].(string); k == "obj_type" {
				if v, _ := l.warnings[i].fields[f+1].(string); v == objType {
					return &l.warnings[i]
				}
			}
		}
	}
	return nil
}

// --- harness -----------------------------------------------------------

type sourceSyncHarness struct {
	workspaceID uuid.UUID
	kb          *model.KnowledgeBase
	connID      uuid.UUID
	store       *fakeSourceSyncStore
	raw         *fakeSyncRawStore
	queue       *fakeSyncQueue
	logger      *recordingLogger
	connector   *fakeSourceConnector
	svc         *SourceSyncService
}

// newFeishuKB 构造一个绑定来源连接的飞书知识库（直接 new struct 绕过构造器校验细节）。
func newFeishuKB(t *testing.T, workspaceID uuid.UUID, rootToken string) *model.KnowledgeBase {
	t.Helper()
	connID := uuid.New()
	generationID := uuid.New()
	kb, err := model.NewKnowledgeBaseWithSource(
		workspaceID, "feishu-kb", "", uuid.New(), nil, nil,
		value.SourceTypeFeishuWiki,
		map[string]any{"root_token": rootToken, "root_kind": "wiki_node"},
		&connID,
	)
	if err != nil {
		t.Fatal(err)
	}
	root := &model.FileTreeNode{
		ID: uuid.New(), WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID,
		NodeType: value.FileTreeNodeRoot,
	}
	kb.FileTreeRootID = root.ID
	kb.ActiveIndexGenerationID = &generationID
	return kb
}

func newSourceSyncHarness(t *testing.T, tree []model.ExternalNode) *sourceSyncHarness {
	t.Helper()
	workspaceID := uuid.New()
	kb := newFeishuKB(t, workspaceID, "root-node-token")
	store := newFakeSourceSyncStore(kb)
	// 预置 KB 的 file tree root，供 GetFileTreeNodeForUpdate 用到时返回。
	store.nodes[kb.FileTreeRootID] = &model.FileTreeNode{
		ID: kb.FileTreeRootID, WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID,
		NodeType: value.FileTreeNodeRoot,
	}
	connector := &fakeSourceConnector{tree: tree}
	logger := &recordingLogger{}
	selector := NewSourceConnectionSelector(&fakeSyncConnRepo{conn: buildSelectedConn(workspaceID, *kb.SourceConnectionID)}, passthroughCipher{})
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: kb},
		Selector:                selector,
		Connector:               connector,
		RawStore:                &fakeSyncRawStore{},
		Store:                   store,
		Queue:                   &fakeSyncQueue{},
		Logger:                  logger,
	})
	return &sourceSyncHarness{
		workspaceID: workspaceID,
		kb:          kb,
		connID:      *kb.SourceConnectionID,
		store:       store,
		raw:         nil,
		queue:       nil,
		logger:      logger,
		connector:   connector,
		svc:         svc,
	}
}

// buildSelectedConn 不是 SelectedSourceConnection；用于 fake repo 返回 model.SourceConnection。
// 见 fakeSyncConnRepo.Get 的注释。
func buildSelectedConn(workspaceID, connID uuid.UUID) model.SourceConnection {
	return model.SourceConnection{
		ID:                    connID,
		WorkspaceID:           workspaceID,
		Provider:              model.SourceProviderFeishu,
		Name:                  "feishu-conn",
		Config:                map[string]any{"app_id": "cli_test_app"},
		CredentialsCiphertext: []byte("plaintext-secret"),
		Status:                "active",
	}
}

// rewireHarness 让 harness 持有真实注入的 raw/queue，并重建 svc 以便测试观察它们。
func (h *sourceSyncHarness) rewire(raw *fakeSyncRawStore, q *fakeSyncQueue, connector *fakeSourceConnector) {
	h.raw = raw
	h.queue = q
	h.connector = connector
	selector := NewSourceConnectionSelector(&fakeSyncConnRepo{conn: buildSelectedConn(h.workspaceID, h.connID)}, passthroughCipher{})
	h.svc = NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: h.kb},
		Selector:                selector,
		Connector:               connector,
		RawStore:                raw,
		Store:                   h.store,
		Queue:                   q,
		Logger:                  h.logger,
	})
}

// --- harness 断言 helpers（覆盖 spec 12.2 测试场景） ---

// syncOnce 用完整的（默认）快照执行一次同步；返回底层 error。
func (h *sourceSyncHarness) syncOnce(t *testing.T) {
	t.Helper()
	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}
}

// documentByExternalID 按 external_id 取最近一次同步后的 Document。
func (h *sourceSyncHarness) documentByExternalID(externalID string) *model.Document {
	for _, doc := range h.store.documents {
		if doc.ExternalID == externalID {
			return doc
		}
	}
	return nil
}

// revisionCount 返回某 Document 的 crawl revision 数量。
func (h *sourceSyncHarness) revisionCount(documentID uuid.UUID) int {
	count := 0
	for _, rev := range h.store.revisions {
		if rev.DocumentID == documentID && rev.Reason == value.DocumentRevisionReasonCrawl {
			count++
		}
	}
	return count
}

// latestRevisionNo 返回某 Document 最新 crawl revision 的 revision_no；无则 0。
func (h *sourceSyncHarness) latestRevisionNo(documentID uuid.UUID) int64 {
	var maxNo int64
	for _, rev := range h.store.revisions {
		if rev.DocumentID == documentID && rev.Reason == value.DocumentRevisionReasonCrawl && rev.RevisionNo > maxNo {
			maxNo = rev.RevisionNo
		}
	}
	return maxNo
}

// parseJobCount 返回某 external 对应 Document 的 pending document_parse_start Job 数。
func (h *sourceSyncHarness) parseJobCount(externalID string) int {
	doc := h.documentByExternalID(externalID)
	if doc == nil {
		return 0
	}
	count := 0
	for _, job := range h.store.jobs {
		if job.DocumentID == doc.ID && job.Type == "document_parse_start" &&
			(job.Status == value.JobStatusPending) {
			count++
		}
	}
	return count
}

// lastCursor 取最近一次 UpdateSyncCursor 写入的 cursor；无则零值。
func (h *sourceSyncHarness) lastCursor() time.Time {
	kbRepo, ok := h.svc.kbRepo.(*fakeKBSyncRepo)
	if !ok || len(kbRepo.syncCursorCalls) == 0 {
		return time.Time{}
	}
	return kbRepo.syncCursorCalls[len(kbRepo.syncCursorCalls)-1].Cursor
}

// syncResult 取最近一次 UpdateSyncResult 写入的 SyncResult。
func (h *sourceSyncHarness) syncResult() *SyncResult { return h.store.lastSyncResult }

// documentDeleted 返回某 external 对应 Document 是否已软删。
func (h *sourceSyncHarness) documentDeleted(externalID string) bool {
	doc := h.documentByExternalID(externalID)
	if doc == nil {
		return false
	}
	return doc.DeletedAt != nil
}

// folderByExternal 按 external_id 取 folder 节点。
func (h *sourceSyncHarness) folderByExternal(externalID string) *model.FileTreeNode {
	if id, ok := h.store.folderByExternal[externalID]; ok {
		return h.store.nodes[id]
	}
	return nil
}

type fakeKBSyncRepo struct {
	kb              *model.KnowledgeBase
	items           map[uuid.UUID]*model.KnowledgeBase
	dueList         []DueKnowledgeBase
	dueErr          error
	dueByConnection map[uuid.UUID][]DueKnowledgeBase
	nextSyncAtCalls []nextSyncAtCall
	nextSyncAtErr   error
	syncCursorCalls []syncCursorCall
	syncCursorErr   error
}

type nextSyncAtCall struct {
	WorkspaceID uuid.UUID
	KBID        uuid.UUID
	NextSyncAt  time.Time
}

type syncCursorCall struct {
	WorkspaceID uuid.UUID
	KBID        uuid.UUID
	Cursor      time.Time
}

func (r *fakeKBSyncRepo) Get(_ context.Context, workspaceID, id uuid.UUID) (*model.KnowledgeBase, error) {
	if r.kb != nil && r.kb.WorkspaceID == workspaceID && r.kb.ID == id {
		return r.kb, nil
	}
	if kb, ok := r.items[id]; ok && kb.WorkspaceID == workspaceID {
		return kb, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (r *fakeKBSyncRepo) ListDueFeishuKBs(_ context.Context, _ time.Time, connectionID uuid.UUID) ([]DueKnowledgeBase, error) {
	if r.dueErr != nil {
		return nil, r.dueErr
	}
	if connectionID != uuid.Nil {
		if r.dueByConnection != nil {
			return append([]DueKnowledgeBase(nil), r.dueByConnection[connectionID]...), nil
		}
		var filtered []DueKnowledgeBase
		for _, item := range r.dueList {
			if item.SourceConnectionID == connectionID {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}
	return append([]DueKnowledgeBase(nil), r.dueList...), nil
}

func (r *fakeKBSyncRepo) UpdateNextSyncAt(_ context.Context, workspaceID, kbID uuid.UUID, nextSyncAt time.Time) error {
	r.nextSyncAtCalls = append(r.nextSyncAtCalls, nextSyncAtCall{WorkspaceID: workspaceID, KBID: kbID, NextSyncAt: nextSyncAt})
	return r.nextSyncAtErr
}

func (r *fakeKBSyncRepo) UpdateSyncCursor(_ context.Context, workspaceID, kbID uuid.UUID, cursor time.Time) error {
	r.syncCursorCalls = append(r.syncCursorCalls, syncCursorCall{WorkspaceID: workspaceID, KBID: kbID, Cursor: cursor})
	return r.syncCursorErr
}

// fakeSyncConnRepo 满足 SourceConnectionRepository，但来源同步路径只用 Get。
type fakeSyncConnRepo struct {
	conn   model.SourceConnection
	getErr error
}

func (r *fakeSyncConnRepo) Create(_ context.Context, _ *model.SourceConnection) error {
	return errors.New("not implemented")
}

func (r *fakeSyncConnRepo) Get(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*model.SourceConnection, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	c := r.conn
	return &c, nil
}

func (r *fakeSyncConnRepo) List(_ context.Context, _ uuid.UUID) ([]*model.SourceConnection, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeSyncConnRepo) Update(_ context.Context, _ *model.SourceConnection) error {
	return errors.New("not implemented")
}

func (r *fakeSyncConnRepo) SoftDelete(_ context.Context, _, _ uuid.UUID) error {
	return errors.New("not implemented")
}

// passthroughCipher 不做加解密变换，便于测试。
type passthroughCipher struct{}

func (passthroughCipher) Encrypt(_ uuid.UUID, plaintext []byte) ([]byte, error) {
	return append([]byte(nil), plaintext...), nil
}
func (passthroughCipher) Decrypt(_ uuid.UUID, ciphertext []byte) ([]byte, error) {
	return append([]byte(nil), ciphertext...), nil
}

// --- tests -------------------------------------------------------------

// docxNode / folderNode helper 构造 ExternalNode。
func docxNode(token, parent, title string) model.ExternalNode {
	return model.ExternalNode{Token: token, ParentToken: parent, Title: title, ObjType: "docx", HasDocument: true}
}

func folderNode(token, parent, title string) model.ExternalNode {
	return model.ExternalNode{Token: token, ParentToken: parent, Title: title, ObjType: "folder", HasDocument: false}
}

// buildMixedTree 构造 2 folder + 3 docx 的树（folderA 下 2 docx，folderB 下 1 docx）。
func buildMixedTree() []model.ExternalNode {
	return []model.ExternalNode{
		folderNode("folderA", "", "目录A"),
		folderNode("folderB", "", "目录B"),
		docxNode("docA1", "folderA", "文档A1"),
		docxNode("docA2", "folderA", "文档A2"),
		docxNode("docB1", "folderB", "文档B1"),
	}
}

func TestSyncKnowledgeBaseFetchesAndEnqueuesParse(t *testing.T) {
	tree := buildMixedTree()
	h := newSourceSyncHarness(t, tree)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docA1": {Markdown: []byte("# 文档A1"), Title: "文档A1", ObjType: "docx"},
		"docA2": {Markdown: []byte("# 文档A2"), Title: "文档A2", ObjType: "docx"},
		"docB1": {Markdown: []byte("# 文档B1"), Title: "文档B1", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	if got, want := len(q.requests), 3; got != want {
		t.Fatalf("enqueued parse jobs = %d, want %d", got, want)
	}
	if got, want := h.store.createDocCalls, 3; got != want {
		t.Fatalf("created documents = %d, want %d", got, want)
	}
	// 新流程通过 UpsertSourceFolder（按 external_id upsert）落地 folder。
	if got, want := h.store.upsertFolderCalls, 2; got != want {
		t.Fatalf("upserted folders = %d, want %d", got, want)
	}

	// 每个 document_parse_start 任务类型与 TaskID。
	for i, req := range q.requests {
		if req.Type != "document_parse_start" {
			t.Fatalf("request[%d].Type = %q, want document_parse_start", i, req.Type)
		}
		if req.TaskID == "" {
			t.Fatalf("request[%d].TaskID empty", i)
		}
	}

	// external_id 正确写入 document。
	for external, docID := range h.store.docJobByExternal {
		doc := h.store.documents[docID]
		if doc.ExternalID != external {
			t.Fatalf("document %s external_id = %q, want %q", docID, doc.ExternalID, external)
		}
		if doc.SourceType != model.SourceProviderFeishu {
			t.Fatalf("document %s source_type = %q, want feishu", docID, doc.SourceType)
		}
	}
	if len(h.store.docJobByExternal) != 3 {
		t.Fatalf("external map size = %d, want 3", len(h.store.docJobByExternal))
	}

	// folder 树结构：folderA/folderB 的 parent == KB root，doc 的 parent == folder。
	var folderA, folderB *model.FileTreeNode
	for _, n := range h.store.nodes {
		if n.Name == "目录A" {
			folderA = n
		}
		if n.Name == "目录B" {
			folderB = n
		}
	}
	if folderA == nil || folderB == nil {
		t.Fatalf("missing folders: A=%v B=%v", folderA, folderB)
	}
	if folderA.ParentID == nil || *folderA.ParentID != h.kb.FileTreeRootID {
		t.Fatalf("folderA parent = %v, want %s", folderA.ParentID, h.kb.FileTreeRootID)
	}
	// 任意一个 doc 的父应为某个 folder。
	foundParentedDoc := false
	for _, n := range h.store.nodes {
		if n.NodeType == value.FileTreeNodeFile {
			if n.ParentID != nil && (*n.ParentID == folderA.ID || *n.ParentID == folderB.ID) {
				foundParentedDoc = true
			}
		}
	}
	if !foundParentedDoc {
		t.Fatalf("no doc node attached to a synced folder")
	}
}

func TestSyncSkipsNonDocxNodesWithWarning(t *testing.T) {
	tree := []model.ExternalNode{
		docxNode("docX", "", "文档X"),
		{Token: "sheetY", ParentToken: "", Title: "表格Y", ObjType: "sheet", HasDocument: true},
	}
	h := newSourceSyncHarness(t, tree)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docX": {Markdown: []byte("# X"), Title: "文档X", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	if got, want := len(q.requests), 1; got != want {
		t.Fatalf("enqueued parse jobs = %d, want %d (sheet should be skipped)", got, want)
	}
	if got, want := h.store.createDocCalls, 1; got != want {
		t.Fatalf("created documents = %d, want %d", got, want)
	}
	if entry := h.logger.warningForObjType("sheet"); entry == nil {
		t.Fatalf("expected a warning log for skipped sheet node; warnings = %#v", h.logger.warnings)
	}
}

func TestSyncContinuesOnSingleFetchError(t *testing.T) {
	tree := []model.ExternalNode{
		docxNode("docFail", "", "失败文档"),
		docxNode("docOK", "", "正常文档"),
	}
	h := newSourceSyncHarness(t, tree)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetchFn := func(externalID string) (model.FetchedDocument, error) {
		if externalID == "docFail" {
			return model.FetchedDocument{}, errors.New("feishu fetch boom")
		}
		return model.FetchedDocument{Markdown: []byte("# OK"), Title: "正常文档", ObjType: "docx"}, nil
	}
	connector := &fakeSourceConnector{tree: tree, fetchFn: fetchFn}
	h.rewire(raw, q, connector)

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase should not fail on single fetch error, got = %v", err)
	}

	if got, want := len(q.requests), 1; got != want {
		t.Fatalf("enqueued parse jobs = %d, want %d (only the OK doc)", got, want)
	}
	if got, want := h.store.createDocCalls, 1; got != want {
		t.Fatalf("created documents = %d, want %d", got, want)
	}
	if len(h.logger.errors) == 0 {
		t.Fatalf("expected an error log for the failed fetch; errors empty")
	}
}

func TestSyncRejectsNonFeishuKB(t *testing.T) {
	workspaceID := uuid.New()
	kb, err := model.NewKnowledgeBase(workspaceID, "upload-kb", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	kb.FileTreeRootID = uuid.New()
	gen := uuid.New()
	kb.ActiveIndexGenerationID = &gen

	store := newFakeSourceSyncStore(kb)
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: kb},
		Selector:                NewSourceConnectionSelector(&fakeSyncConnRepo{}, passthroughCipher{}),
		Connector:               &fakeSourceConnector{},
		RawStore:                &fakeSyncRawStore{},
		Store:                   store,
		Queue:                   &fakeSyncQueue{},
		Logger:                  &recordingLogger{},
	})

	err = svc.SyncKnowledgeBase(context.Background(), workspaceID, kb.ID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if store.createDocCalls != 0 {
		t.Fatalf("no documents should be created for non-feishu KB; got %d", store.createDocCalls)
	}
}

// TestEnqueueSyncPersistsJobAndEnqueuesDedupedTask 验证 EnqueueSync 创建 source_sync
// Job 落库并以稳定的 TaskID 入队（同 KB 幂等）。
func TestEnqueueSyncPersistsJobAndEnqueuesDedupedTask(t *testing.T) {
	workspaceID := uuid.New()
	kb := newFeishuKB(t, workspaceID, "root-node-token")
	store := newFakeSourceSyncStore(kb)
	q := &fakeSyncQueue{}
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: kb},
		Selector:                NewSourceConnectionSelector(&fakeSyncConnRepo{}, passthroughCipher{}),
		Connector:               &fakeSourceConnector{},
		RawStore:                &fakeSyncRawStore{},
		Store:                   store,
		Queue:                   q,
		Logger:                  &recordingLogger{},
	})

	job, err := svc.EnqueueSync(context.Background(), workspaceID, kb.ID)
	if err != nil {
		t.Fatalf("EnqueueSync err = %v", err)
	}
	if job == nil || job.Type != model.SourceSyncJobType || job.WorkspaceID != workspaceID || job.KnowledgeBaseID != kb.ID {
		t.Fatalf("job = %#v", job)
	}
	if job.SourceConnectionID != *kb.SourceConnectionID {
		t.Fatalf("job SourceConnectionID = %s, want %s", job.SourceConnectionID, *kb.SourceConnectionID)
	}
	if stored, ok := store.jobs[job.ID]; !ok || stored != job {
		t.Fatalf("job 未持久化到 store: %v", stored)
	}
	if len(q.requests) != 1 {
		t.Fatalf("enqueue requests = %d, want 1", len(q.requests))
	}
	req := q.requests[0]
	if req.Type != model.SourceSyncJobType {
		t.Fatalf("queue type = %s", req.Type)
	}
	if req.TaskID != queue.SourceSyncTaskID(workspaceID, kb.ID) {
		t.Fatalf("TaskID = %s, want stable dedup id", req.TaskID)
	}
}

// TestEnqueueSyncRejectsNonFeishuKB 验证非飞书 KB 不能入队 source_sync。
func TestEnqueueSyncRejectsNonFeishuKB(t *testing.T) {
	workspaceID := uuid.New()
	// 直接构造一个 upload 类型 KB（绕过 NewKnowledgeBaseWithSource）。
	kb := &model.KnowledgeBase{
		ID: uuid.New(), WorkspaceID: workspaceID, SourceType: value.SourceTypeUpload,
		SourceConfig: map[string]any{},
	}
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: &fakeKBSyncRepo{kb: kb},
		Store:                   newFakeSourceSyncStore(kb),
		Queue:                   &fakeSyncQueue{},
		Logger:                  &recordingLogger{},
	})

	if _, err := svc.EnqueueSync(context.Background(), workspaceID, kb.ID); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// 确保未使用的 sourceport 导入在仅依赖常量时仍被引用（避免编译期未使用）。
var _ = sourceport.SyncRootWikiNode

// --- 增量同步 / 删除检测 helpers ---

// docxNodeWithEdit 构造一个带 EditTime 的 docx 节点。
func docxNodeWithEdit(token, parent, title string, editTime time.Time) model.ExternalNode {
	return model.ExternalNode{Token: token, ParentToken: parent, Title: title, ObjType: "docx", HasDocument: true, EditTime: editTime}
}

// setKBSyncCursor 把 KB.SourceConfig["sync_cursor"] 设置为给定时间（RFC3339）。
func setKBSyncCursor(kb *model.KnowledgeBase, cursor time.Time) {
	if kb.SourceConfig == nil {
		kb.SourceConfig = map[string]any{}
	}
	kb.SourceConfig["sync_cursor"] = cursor.UTC().Format(time.RFC3339)
}

// makeExistingExternalDoc 构造一个已存在的 Document（带 external_id），用于预置 fake store。
func makeExistingExternalDoc(workspaceID, kbID uuid.UUID, externalID, title string) *model.Document {
	doc, err := model.NewDocumentIdentityWithExternal(
		workspaceID, kbID, value.DocumentKindFile, title, model.SourceProviderFeishu, "", externalID, nil,
	)
	if err != nil {
		panic(err)
	}
	return doc
}

// mustBuildRevision 构造一个 crawl revision，用于预置 fake store（模拟已就绪/失败的 source revision）。
func mustBuildRevision(workspaceID, kbID, documentID uuid.UUID, markdown string, revisionNo int64, status value.DocumentRevisionStatus) *model.DocumentRevision {
	hash := contentHashOf([]byte(markdown))
	rev, err := model.NewDocumentRevision(model.NewDocumentRevisionInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID, DocumentID: documentID,
		Kind: value.DocumentKindFile, DocumentKind: value.DocumentKindFile,
		RevisionNo: revisionNo, Reason: value.DocumentRevisionReasonCrawl,
		OriginalFilename: "x.md", FileType: "markdown", ContentType: "text/markdown",
		RawStorageKey: "raw/" + documentID.String(), SHA256: hash, SizeBytes: int64(len(markdown)),
		ProcessingVersion: model.CurrentProcessingVersion, Status: status,
	})
	if err != nil {
		panic(err)
	}
	return rev
}

// --- 增量同步 / 删除检测 tests ---

// TestIncrementalSyncSkipsUnchangedNodes 验证 cursor 之后未变更的 docx 节点（EditTime <= cursor、
// 无重试）由 diff 归类为 Skipped，不进入 Fetch；而变更的节点（EditTime 晚于 cursor）仍被 Fetch。
func TestIncrementalSyncSkipsUnchangedNodes(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour) // docA 晚于 cursor
	tree := []model.ExternalNode{
		docxNodeWithEdit("docA", "", "文档A", t2), // t2 > cursor(t1) → ToAdd → Fetch
		docxNodeWithEdit("docB", "", "文档B", t1), // t1 == cursor → Skipped（不 Fetch）
	}
	h := newSourceSyncHarness(t, tree)
	setKBSyncCursor(h.kb, t1)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docA": {Markdown: []byte("# A"), Title: "文档A", ObjType: "docx"},
		"docB": {Markdown: []byte("# B"), Title: "文档B", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	// 预置 docB 为本地已就绪文档（matching hash），使其 EditTime<=cursor + 无重试 => Skipped。
	docB := makeExistingExternalDoc(h.workspaceID, h.kb.ID, "docB", "文档B")
	docB.ContentHash = contentHashOf([]byte("# B"))
	docB.Status = value.DocumentStatusReady
	h.store.existingDocuments = []*model.Document{docB}
	// 让 ListSourceDocuments 算出 RetryRequired=false：为 docB 建一个 ready revision。
	readyRev := mustBuildRevision(h.workspaceID, h.kb.ID, docB.ID, "# B", 1, value.DocumentRevisionReady)
	h.store.revisions[readyRev.ID] = readyRev

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	fetchCounts := connector.fetchedTokenCount()
	if fetchCounts["docA"] != 1 {
		t.Fatalf("docA 应被 Fetch 一次，实际 %d", fetchCounts["docA"])
	}
	if fetchCounts["docB"] != 0 {
		t.Fatalf("docB 应被跳过（EditTime == cursor），实际 Fetch %d 次", fetchCounts["docB"])
	}
	if got, want := len(q.requests), 1; got != want {
		t.Fatalf("enqueued parse jobs = %d, want %d (only docA)", got, want)
	}
	if got, want := h.store.createDocCalls, 1; got != want {
		t.Fatalf("created documents = %d, want %d (only docA)", got, want)
	}
}

// TestIncrementalSyncSoftDeletesMissingDocuments 验证完整 snapshot 中本地存在但远端不存在的文档被删除。
func TestIncrementalSyncSoftDeletesMissingDocuments(t *testing.T) {
	tree := []model.ExternalNode{
		docxNode("docA", "", "文档A"),
	}
	h := newSourceSyncHarness(t, tree)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docA": {Markdown: []byte("# A"), Title: "文档A", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	// 预置一个 DB 里已存在但飞书树中已不存在的文档 docD。
	docD := makeExistingExternalDoc(h.workspaceID, h.kb.ID, "docD", "已删除文档")
	h.store.existingDocuments = []*model.Document{docD}

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	// 新流程通过 DeleteSourceDocument 删除（完整 snapshot 删除闸门）。
	if len(h.store.deleteCalls) != 1 || h.store.deleteCalls[0].DocumentID != docD.ID {
		t.Fatalf("expected docD %s to be deleted; got %v", docD.ID, h.store.deleteCalls)
	}
}

// TestIncrementalSyncDoesNotResoftDeleteAlreadyDeleted 验证已软删的文档不会被重复删除。
func TestIncrementalSyncDoesNotResoftDeleteAlreadyDeleted(t *testing.T) {
	tree := []model.ExternalNode{
		docxNode("docA", "", "文档A"),
	}
	h := newSourceSyncHarness(t, tree)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docA": {Markdown: []byte("# A"), Title: "文档A", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	// 预置一个已软删的文档 docD（DeletedAt 非空）。
	docD := makeExistingExternalDoc(h.workspaceID, h.kb.ID, "docD", "已删除文档")
	deletedAt := time.Now().UTC()
	docD.DeletedAt = &deletedAt
	h.store.existingDocuments = []*model.Document{docD}

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	if len(h.store.deleteCalls) != 0 {
		t.Fatalf("已软删的 docD 不应被重复删除；deleteCalls = %v", h.store.deleteCalls)
	}
}

// TestIncrementalSyncAdvancesCursorValue 验证完整 snapshot 全部成功后，UpdateSyncCursor 被调用，
// 值为安全 watermark（spec 6.4：仅文档节点 EditTime 的成功前缀，folder 不参与 watermark）。
func TestIncrementalSyncAdvancesCursorValue(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour)
	t3 := t2.Add(1 * time.Hour) // folder EditTime 最大，但 folder 不参与 watermark
	tree := []model.ExternalNode{
		folderNodeWithEdit("folderA", "", "目录A", t3),
		docxNodeWithEdit("docA", "folderA", "文档A", t2), // 文档最大 EditTime
		docxNodeWithEdit("docB", "folderA", "文档B", t1),
	}
	workspaceID := uuid.New()
	kb := newFeishuKB(t, workspaceID, "root-node-token")
	store := newFakeSourceSyncStore(kb)
	store.nodes[kb.FileTreeRootID] = &model.FileTreeNode{
		ID: kb.FileTreeRootID, WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID,
		NodeType: value.FileTreeNodeRoot,
	}
	kbRepo := &fakeKBSyncRepo{kb: kb}
	connector := &fakeSourceConnector{tree: tree, fetched: map[string]model.FetchedDocument{
		"docA": {Markdown: []byte("# A"), Title: "文档A", ObjType: "docx"},
		"docB": {Markdown: []byte("# B"), Title: "文档B", ObjType: "docx"},
	}}
	selector := NewSourceConnectionSelector(&fakeSyncConnRepo{conn: buildSelectedConn(workspaceID, *kb.SourceConnectionID)}, passthroughCipher{})
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: kbRepo,
		Selector:                selector,
		Connector:               connector,
		RawStore:                &fakeSyncRawStore{},
		Store:                   store,
		Queue:                   &fakeSyncQueue{},
		Logger:                  &recordingLogger{},
	})

	if err := svc.SyncKnowledgeBase(context.Background(), workspaceID, kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	if len(kbRepo.syncCursorCalls) != 1 {
		t.Fatalf("expected 1 UpdateSyncCursor call, got %d", len(kbRepo.syncCursorCalls))
	}
	got := kbRepo.syncCursorCalls[0].Cursor
	// watermark 只取文档节点 EditTime 的成功前缀；folder(t3) 不参与。
	want := t2.UTC()
	if !got.Equal(want) {
		t.Fatalf("sync_cursor = %v, want %v (文档 watermark)", got, want)
	}
}

// folderNodeWithEdit 构造一个带 EditTime 的 folder 节点。
func folderNodeWithEdit(token, parent, title string, editTime time.Time) model.ExternalNode {
	return model.ExternalNode{Token: token, ParentToken: parent, Title: title, ObjType: "folder", HasDocument: false, EditTime: editTime}
}

// TestFirstSyncIsFullWhenNoCursor 验证无 cursor 时所有节点都被 Fetch（全量同步）。
func TestFirstSyncIsFullWhenNoCursor(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour)
	tree := []model.ExternalNode{
		docxNodeWithEdit("docA", "", "文档A", t2),
		docxNodeWithEdit("docB", "", "文档B", t1),
	}
	h := newSourceSyncHarness(t, tree)
	// 不设置 sync_cursor（KB.SourceConfig 无 sync_cursor 字段）。
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docA": {Markdown: []byte("# A"), Title: "文档A", ObjType: "docx"},
		"docB": {Markdown: []byte("# B"), Title: "文档B", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	fetchCounts := connector.fetchedTokenCount()
	if fetchCounts["docA"] != 1 || fetchCounts["docB"] != 1 {
		t.Fatalf("无 cursor 时应全量 Fetch docA 和 docB；got docA=%d docB=%d", fetchCounts["docA"], fetchCounts["docB"])
	}
	if got, want := h.store.createDocCalls, 2; got != want {
		t.Fatalf("created documents = %d, want %d", got, want)
	}
}

// TestIncrementalSyncFetchesZeroEditTimeNode 验证 EditTime 零值的节点（如 drive 文件）即使有 cursor 也总是 Fetch。
func TestIncrementalSyncFetchesZeroEditTimeNode(t *testing.T) {
	cursor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tree := []model.ExternalNode{
		// EditTime 零值（未设置）→ 无法判断变更 → 总是 Fetch。
		docxNode("docZero", "", "零值文档"),
	}
	h := newSourceSyncHarness(t, tree)
	setKBSyncCursor(h.kb, cursor)
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	fetched := map[string]model.FetchedDocument{
		"docZero": {Markdown: []byte("# Zero"), Title: "零值文档", ObjType: "docx"},
	}
	connector := &fakeSourceConnector{tree: tree, fetched: fetched}
	h.rewire(raw, q, connector)

	if err := h.svc.SyncKnowledgeBase(context.Background(), h.workspaceID, h.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}

	fetchCounts := connector.fetchedTokenCount()
	if fetchCounts["docZero"] != 1 {
		t.Fatalf("EditTime 零值的节点应总是 Fetch；got %d", fetchCounts["docZero"])
	}
}

// --- Task 7：稳定文档增量同步测试（spec 12.2） ---
//
// 下面这组测试覆盖 spec 12.2 的核心断言：稳定 Document 身份、内容 hash 去重、
// RetryRequired 重试、oversize 保护、partial snapshot 不删除不推进 cursor、
// folder upsert/删除。所有断言都基于内存 fake 的真实状态。

// sourceSyncTestEnv 是 Task 7 测试的统一环境：自带可注入快照/fetch 的 connector、
// 忠实 store、raw/queue/kbRepo，以及 maxContentBytes 配置。
type sourceSyncTestEnv struct {
	workspaceID uuid.UUID
	kb          *model.KnowledgeBase
	kbRepo      *fakeKBSyncRepo
	store       *fakeSourceSyncStore
	raw         *fakeSyncRawStore
	queue       *fakeSyncQueue
	logger      *recordingLogger
	connector   *fakeSourceConnector
	svc         *SourceSyncService
}

// newSourceSyncServiceTestEnv 构造一个最小可用环境：单 docx 节点 + 默认 fetched。
func newSourceSyncServiceTestEnv(t *testing.T) *sourceSyncTestEnv {
	t.Helper()
	workspaceID := uuid.New()
	kb := newFeishuKB(t, workspaceID, "root-node-token")
	store := newFakeSourceSyncStore(kb)
	store.nodes[kb.FileTreeRootID] = &model.FileTreeNode{
		ID: kb.FileTreeRootID, WorkspaceID: workspaceID, KnowledgeBaseID: kb.ID,
		NodeType: value.FileTreeNodeRoot,
	}
	kbRepo := &fakeKBSyncRepo{kb: kb}
	connector := &fakeSourceConnector{complete: true}
	raw := &fakeSyncRawStore{}
	q := &fakeSyncQueue{}
	logger := &recordingLogger{}
	selector := NewSourceConnectionSelector(&fakeSyncConnRepo{conn: buildSelectedConn(workspaceID, *kb.SourceConnectionID)}, passthroughCipher{})
	svc := NewSourceSyncService(SourceSyncServiceDeps{
		KnowledgeBaseRepository: kbRepo,
		Selector:                selector,
		Connector:               connector,
		RawStore:                raw,
		Store:                   store,
		Queue:                   q,
		Logger:                  logger,
	})
	return &sourceSyncTestEnv{
		workspaceID: workspaceID, kb: kb, kbRepo: kbRepo, store: store,
		raw: raw, queue: q, logger: logger, connector: connector, svc: svc,
	}
}

// snapshotWithDoc 配置 connector 返回单个 docx 节点 + fetched 内容，并触发一次同步。
func (e *sourceSyncTestEnv) snapshotWithDoc(token string, markdown string) {
	e.connector.tree = []model.ExternalNode{docxNode(token, "", token)}
	e.connector.fetched = map[string]model.FetchedDocument{
		token: {Markdown: []byte(markdown), Title: token, ObjType: "docx"},
	}
}

// snapshotWithDocs 配置多 docx 节点。
func (e *sourceSyncTestEnv) snapshotWithDocs(docs ...fakeDoc) {
	tree := make([]model.ExternalNode, 0, len(docs))
	fetched := map[string]model.FetchedDocument{}
	for _, d := range docs {
		tree = append(tree, docxNode(d.token, d.parent, d.title))
		fetched[d.token] = model.FetchedDocument{Markdown: []byte(d.markdown), Title: d.title, ObjType: "docx"}
	}
	e.connector.tree = tree
	e.connector.fetched = fetched
}

type fakeDoc struct {
	token, parent, title, markdown string
}

func (e *sourceSyncTestEnv) syncOnce(t *testing.T) {
	t.Helper()
	if err := e.svc.SyncKnowledgeBase(context.Background(), e.workspaceID, e.kb.ID); err != nil {
		t.Fatalf("SyncKnowledgeBase error = %v", err)
	}
}

func (e *sourceSyncTestEnv) syncOnceWithForce(t *testing.T, force bool) {
	t.Helper()
	if err := e.svc.syncKnowledgeBase(context.Background(), e.workspaceID, e.kb.ID, force); err != nil {
		t.Fatalf("syncKnowledgeBase(force=%v) error = %v", force, err)
	}
}

func (e *sourceSyncTestEnv) documentByExternalID(externalID string) *model.Document {
	for _, doc := range e.store.documents {
		if doc.ExternalID == externalID {
			return doc
		}
	}
	return nil
}

func (e *sourceSyncTestEnv) revisionCount(documentID uuid.UUID) int {
	count := 0
	for _, rev := range e.store.revisions {
		if rev.DocumentID == documentID && rev.Reason == value.DocumentRevisionReasonCrawl {
			count++
		}
	}
	return count
}

func (e *sourceSyncTestEnv) parseJobCount(externalID string) int {
	doc := e.documentByExternalID(externalID)
	if doc == nil {
		return 0
	}
	count := 0
	for _, job := range e.store.jobs {
		if job.DocumentID == doc.ID && job.Type == "document_parse_start" &&
			job.Status == value.JobStatusPending {
			count++
		}
	}
	return count
}

func (e *sourceSyncTestEnv) lastCursor() time.Time {
	if len(e.kbRepo.syncCursorCalls) == 0 {
		return time.Time{}
	}
	return e.kbRepo.syncCursorCalls[len(e.kbRepo.syncCursorCalls)-1].Cursor
}

func (e *sourceSyncTestEnv) documentDeleted(externalID string) bool {
	doc := e.documentByExternalID(externalID)
	return doc != nil && doc.DeletedAt != nil
}

// markDocumentReady 模拟 pipeline 完成：把最新 crawl revision 标记为 ready、
// document 标记为 ready，使下一次同步 ListSourceDocuments 算出 RetryRequired=false。
func (e *sourceSyncTestEnv) markDocumentReady(externalID string) {
	doc := e.documentByExternalID(externalID)
	if doc == nil {
		return
	}
	doc.Status = value.DocumentStatusReady
	if rev := latestCrawlRevision(e.store, doc.ID); rev != nil {
		rev.Status = value.DocumentRevisionReady
		now := time.Now().UTC()
		rev.CompletedAt = &now
	}
}

// TestSyncReusesExternalDocumentAndSkipsUnchangedContent（spec 12.2 #1/#2）：
// 新文档创建一次后再次同步复用同一 Document ID；内容未变不创建新 revision。
func TestSyncReusesExternalDocumentAndSkipsUnchangedContent(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDoc("doc-1", "same")
	env.syncOnce(t)

	first := env.documentByExternalID("doc-1")
	if first == nil {
		t.Fatalf("doc-1 未创建")
	}
	firstRevisionCount := env.revisionCount(first.ID)

	// 模拟 pipeline 完成：把最新 revision 标记为 ready、document 标记为 ready，
	// 这样下一次同步 ListSourceDocuments 算出 RetryRequired=false，hash 未变即可跳过。
	env.markDocumentReady("doc-1")

	// 第二次同步相同内容：EditTime 零值会再次进入 ToUpdate（fetch + hash），
	// 但 hash 未变、无重试、无 force => 不创建新 revision。
	env.syncOnce(t)

	second := env.documentByExternalID("doc-1")
	if second.ID != first.ID {
		t.Fatalf("第二次同步应复用同一 Document ID; first=%s second=%s", first.ID, second.ID)
	}
	if got := env.revisionCount(first.ID); got != firstRevisionCount {
		t.Fatalf("内容未变不应创建新 revision; got %d want %d", got, firstRevisionCount)
	}
}

// TestSyncContentChangeIncrementsRevisionNo（spec 12.2 #3）：内容变化 revision_no 递增。
func TestSyncContentChangeIncrementsRevisionNo(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDoc("doc-1", "v1")
	env.syncOnce(t)

	doc := env.documentByExternalID("doc-1")
	if env.latestRevisionNo(doc.ID) != 1 {
		t.Fatalf("首次 revision_no 应为 1; got %d", env.latestRevisionNo(doc.ID))
	}

	env.snapshotWithDoc("doc-1", "v2-changed")
	env.syncOnce(t)

	if got, want := env.latestRevisionNo(doc.ID), int64(2); got != want {
		t.Fatalf("内容变化后 revision_no 应递增为 2; got %d", got)
	}
	if got := env.revisionCount(doc.ID); got != 2 {
		t.Fatalf("应有 2 个 crawl revision; got %d", got)
	}
}

func (env *sourceSyncTestEnv) latestRevisionNo(documentID uuid.UUID) int64 {
	var maxNo int64
	for _, rev := range env.store.revisions {
		if rev.DocumentID == documentID && rev.Reason == value.DocumentRevisionReasonCrawl && rev.RevisionNo > maxNo {
			maxNo = rev.RevisionNo
		}
	}
	return maxNo
}

// TestSyncForceCreatesNewRevisionEvenWhenHashUnchanged（spec 12.2 #4）：
// force 即使 hash 未变也创建新 revision。
func TestSyncForceCreatesNewRevisionEvenWhenHashUnchanged(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDoc("doc-1", "same")
	env.syncOnce(t)

	doc := env.documentByExternalID("doc-1")
	before := env.revisionCount(doc.ID)

	// force 同步：即使内容相同也应创建新 revision。
	env.snapshotWithDoc("doc-1", "same")
	env.syncOnceWithForce(t, true)

	if got := env.revisionCount(doc.ID); got != before+1 {
		t.Fatalf("force 应创建新 revision; got %d want %d", got, before+1)
	}
}

// TestSyncFailureDoesNotAdvanceCursorAndRetries（spec 12.2 #5/#6）：
// 拉取失败后 cursor 不推进；下一次同步会重试（成功后入队一次）。
func TestSyncFailureDoesNotAdvanceCursorAndRetries(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.connector.failFetchOnce("doc-1")
	env.snapshotWithDoc("doc-1", "content")
	env.syncOnce(t)

	// 失败同步不应推进 cursor。
	if cursor := env.lastCursor(); !cursor.IsZero() {
		t.Fatalf("失败同步不应推进 cursor; got %v", cursor)
	}
	if res := env.store.lastSyncResult; res == nil || res.Status != "partial" {
		t.Fatalf("失败应写 partial 结果; got %#v", res)
	}

	// 下一次同步恢复成功：应入队解析。
	env.snapshotWithDoc("doc-1", "content")
	env.syncOnce(t)

	if got := env.parseJobCount("doc-1"); got != 1 {
		t.Fatalf("重试成功后应有 1 个 pending parse job; got %d", got)
	}
}

// TestSyncFailedDocumentRetriesSameHash（spec 12.2 #6）：
// Document 处于 failed 时，即使 hash 相同也会重试（复用 revision）。
func TestSyncFailedDocumentRetriesSameHash(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDoc("doc-1", "content")
	env.syncOnce(t)

	doc := env.documentByExternalID("doc-1")
	// 模拟 pipeline 失败：把文档 + 最新 revision 标为 failed，parse job 标为 failed。
	if rev := latestCrawlRevision(env.store, doc.ID); rev != nil {
		rev.Status = value.DocumentRevisionFailed
		rev.ErrorClass = "boom"
	}
	doc.Status = value.DocumentStatusFailed
	for _, job := range env.store.jobs {
		if job.DocumentID == doc.ID {
			job.Status = value.JobStatusFailed
		}
	}

	// 再次同步相同内容：RetryRequired=true，应重试（不创建新 revision）。
	env.snapshotWithDoc("doc-1", "content")
	env.syncOnce(t)

	if got, want := env.revisionCount(doc.ID), 1; got != want {
		t.Fatalf("failed 同 hash 重试不应创建新 revision; got %d want %d", got, want)
	}
	if got := env.parseJobCount("doc-1"); got != 1 {
		t.Fatalf("failed 重试后应有 1 个 pending parse job; got %d", got)
	}
}

func latestCrawlRevision(s *fakeSourceSyncStore, documentID uuid.UUID) *model.DocumentRevision {
	var latest *model.DocumentRevision
	for _, rev := range s.revisions {
		if rev.DocumentID == documentID && rev.Reason == value.DocumentRevisionReasonCrawl {
			if latest == nil || rev.RevisionNo > latest.RevisionNo {
				latest = rev
			}
		}
	}
	return latest
}

// TestSyncOversizeNewDocumentNotPersisted（spec 7.3）：超限新文档不落库。
func TestSyncOversizeNewDocumentNotPersisted(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.svc.maxContentBytes = 10
	env.snapshotWithDoc("doc-1", "this content is definitely longer than ten bytes")
	env.syncOnce(t)

	if doc := env.documentByExternalID("doc-1"); doc != nil {
		t.Fatalf("超限新文档不应落库; got %#v", doc)
	}
	if res := env.store.lastSyncResult; res == nil || res.OversizeDocuments != 1 {
		t.Fatalf("应计数 oversize=1; got %#v", res)
	}
}

// TestSyncOversizeExistingDocumentKeepsOldVersion（spec 7.3）：超限已有文档保留旧版本。
func TestSyncOversizeExistingDocumentKeepsOldVersion(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDoc("doc-1", "small")
	env.syncOnce(t)
	doc := env.documentByExternalID("doc-1")
	oldHash := doc.ContentHash

	// 设上限后同步大内容：应保留旧版本（不更新 hash，不创建新 revision）。
	env.svc.maxContentBytes = 5
	env.snapshotWithDoc("doc-1", "this is a much larger content")
	env.syncOnce(t)

	if got := env.documentByExternalID("doc-1").ContentHash; got != oldHash {
		t.Fatalf("超限已有文档应保留旧 hash; got %q want %q", got, oldHash)
	}
	if got := env.revisionCount(doc.ID); got != 1 {
		t.Fatalf("超限已有文档不应创建新 revision; got %d", got)
	}
	if res := env.store.lastSyncResult; res == nil || res.OversizeDocuments != 1 {
		t.Fatalf("应计数 oversize=1; got %#v", res)
	}
}

// TestPartialSnapshotNeverDeletesOrAdvancesCursor（spec 5.5/6.4）：
// partial snapshot 不删除文档、不推进 cursor。
func TestPartialSnapshotNeverDeletesOrAdvancesCursor(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	// 先 seed 一个已有文档（带 external_id），并设置一个 cursor。
	seeded := makeExistingExternalDoc(env.workspaceID, env.kb.ID, "existing", "已存在文档")
	env.store.existingDocuments = []*model.Document{seeded}
	setKBSyncCursor(env.kb, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	before := env.lastCursor() // 当前还没有任何 cursor 写入，为零值

	// partial snapshot（空节点 + Complete=false）。
	env.connector.complete = false
	env.connector.tree = nil
	env.syncOnce(t)

	if env.documentDeleted("existing") {
		t.Fatalf("partial snapshot 不应删除文档")
	}
	if got := env.lastCursor(); !got.Equal(before) {
		t.Fatalf("partial snapshot 不应推进 cursor; before=%v got=%v", before, got)
	}
	if res := env.store.lastSyncResult; res == nil || !res.Complete {
		t.Fatalf("SyncResult.Complete 应为 false; got %#v", res)
	}
}

// TestCompleteSnapshotDeletesMissingDocument（spec 5.5）：
// 完整 snapshot 中本地存在但远端不存在的文档被删除。
func TestCompleteSnapshotDeletesMissingDocument(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	seeded := makeExistingExternalDoc(env.workspaceID, env.kb.ID, "docD", "已删除文档")
	env.store.existingDocuments = []*model.Document{seeded}

	// 完整 snapshot 只有 docA，docD 不在远端 => 删除。
	env.snapshotWithDocs(fakeDoc{token: "docA", title: "文档A", markdown: "# A"})
	env.syncOnce(t)

	if len(env.store.deleteCalls) != 1 {
		t.Fatalf("应删除 1 个文档; got %d", len(env.store.deleteCalls))
	}
	if env.store.deleteCalls[0].DocumentID != seeded.ID {
		t.Fatalf("应删除 seeded docD; got %s", env.store.deleteCalls[0].DocumentID)
	}
}

// TestFolderUpsertDoesNotDuplicateOnResync（spec 12.2 #9）：
// folder 和 file 节点重复同步使用 upsert，不产生名称冲突。
func TestFolderUpsertDoesNotDuplicateOnResync(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	tree := []model.ExternalNode{
		folderNode("folderA", "", "目录A"),
		docxNode("docA1", "folderA", "文档A1"),
	}
	env.connector.tree = tree
	env.connector.fetched = map[string]model.FetchedDocument{
		"docA1": {Markdown: []byte("# A1"), Title: "文档A1", ObjType: "docx"},
	}
	env.syncOnce(t)
	firstFolderCalls := env.store.upsertFolderCalls
	firstFolder := env.store.folderByExternal["folderA"]

	// 再同步同一棵树：folder upsert 应复用，不重复。
	env.syncOnce(t)

	if env.store.upsertFolderCalls != firstFolderCalls+1 {
		t.Fatalf("folder upsert 调用应递增 1; first=%d got=%d", firstFolderCalls, env.store.upsertFolderCalls)
	}
	if second := env.store.folderByExternal["folderA"]; second != firstFolder {
		t.Fatalf("重复同步 folder 应复用同一节点; first=%s second=%s", firstFolder, second)
	}
}

// TestCompleteSnapshotDeletesEmptyMissingFolder：完整 snapshot 删除失踪的空 folder。
// 该行为依赖服务层的 folder 删除逻辑（partial 不删）。
func TestCompleteSnapshotDeletesEmptyMissingFolder(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	// 先 seed 一个 folder（带 external_id）。
	seedFolder := &model.FileTreeNode{
		ID: uuid.New(), WorkspaceID: env.workspaceID, KnowledgeBaseID: env.kb.ID,
		NodeType: value.FileTreeNodeFolder, Name: "目录X", ExternalID: "folderX",
		ParentID: &env.kb.FileTreeRootID,
	}
	env.store.nodes[seedFolder.ID] = seedFolder
	env.store.folderByExternal["folderX"] = seedFolder.ID

	// 完整 snapshot 不包含 folderX => 应删除空 folder。
	env.snapshotWithDocs(fakeDoc{token: "docA", title: "文档A", markdown: "# A"})
	env.syncOnce(t)

	if _, stillThere := env.store.folderByExternal["folderX"]; stillThere {
		t.Fatalf("完整 snapshot 应删除失踪的空 folderX")
	}
}

// TestSyncWritesSyncResultWithCounts：SyncResult 完整填充计数与 finished_at。
func TestSyncWritesSyncResultWithCounts(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.snapshotWithDocs(
		fakeDoc{token: "docA", title: "A", markdown: "# A"},
		fakeDoc{token: "docB", title: "B", markdown: "# B"},
	)
	env.syncOnce(t)

	res := env.store.lastSyncResult
	if res == nil {
		t.Fatalf("应写入 SyncResult")
	}
	if res.SyncedDocuments != 2 {
		t.Fatalf("SyncedDocuments = %d, want 2", res.SyncedDocuments)
	}
	if res.Status != "succeeded" {
		t.Fatalf("Status = %q, want succeeded", res.Status)
	}
	if res.Complete != true {
		t.Fatalf("Complete = %v, want true", res.Complete)
	}
	if res.FinishedAt.IsZero() {
		t.Fatalf("FinishedAt 不应为零值")
	}
}

// TestSyncCompleteAdvancesCursorToSafeWatermark：
// 完整 snapshot 全部成功时，cursor 推进到安全 watermark。
func TestSyncCompleteAdvancesCursorToSafeWatermark(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	env.connector.tree = []model.ExternalNode{
		docxNodeWithEdit("docA", "", "A", t1),
		docxNodeWithEdit("docB", "", "B", t2),
	}
	env.connector.fetched = map[string]model.FetchedDocument{
		"docA": {Markdown: []byte("# A"), Title: "A", ObjType: "docx"},
		"docB": {Markdown: []byte("# B"), Title: "B", ObjType: "docx"},
	}
	env.syncOnce(t)

	if len(env.kbRepo.syncCursorCalls) != 1 {
		t.Fatalf("应调用 UpdateSyncCursor 一次; got %d", len(env.kbRepo.syncCursorCalls))
	}
	got := env.kbRepo.syncCursorCalls[0].Cursor
	if !got.Equal(t2.UTC()) {
		t.Fatalf("cursor 应推进到最大 EditTime %v; got %v", t2.UTC(), got.UTC())
	}
}

// TestSyncFatalListErrorWritesFailedResult：ListTree 致命错误应写 failed 结果并返回 error。
func TestSyncFatalListErrorWritesFailedResult(t *testing.T) {
	env := newSourceSyncServiceTestEnv(t)
	env.connector.listErr = errors.New("fatal boom")

	err := env.svc.SyncKnowledgeBase(context.Background(), env.workspaceID, env.kb.ID)
	if err == nil {
		t.Fatalf("ListTree 致命错误应返回 error")
	}
	res := env.store.lastSyncResult
	if res == nil || res.Status != "failed" {
		t.Fatalf("应写 failed SyncResult; got %#v", res)
	}
}
