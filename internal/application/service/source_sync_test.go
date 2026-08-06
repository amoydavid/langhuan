package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
}

func (c *fakeSourceConnector) ListTree(_ context.Context, _ model.SourceConnection, _ model.SyncRoot) ([]model.ExternalNode, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]model.ExternalNode(nil), c.tree...), nil
}

func (c *fakeSourceConnector) Fetch(_ context.Context, _ model.SourceConnection, externalID string) (model.FetchedDocument, error) {
	c.fetchedTokens = append(c.fetchedTokens, externalID)
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
}

func newFakeSourceSyncStore(kb *model.KnowledgeBase) *fakeSourceSyncStore {
	return &fakeSourceSyncStore{
		kb:               kb,
		nodes:            map[uuid.UUID]*model.FileTreeNode{},
		documents:        map[uuid.UUID]*model.Document{},
		revisions:        map[uuid.UUID]*model.DocumentRevision{},
		jobs:             map[uuid.UUID]*model.Job{},
		docJobByExternal: map[string]uuid.UUID{},
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
	if got, want := h.store.createFolderCalls, 2; got != want {
		t.Fatalf("created folders = %d, want %d", got, want)
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

// --- 增量同步 / 删除检测 tests ---

// TestIncrementalSyncSkipsUnchangedNodes 验证 cursor 之后未变更的 docx 节点跳过 Fetch，
// 而变更的节点（EditTime 晚于 cursor）仍被 Fetch。
func TestIncrementalSyncSkipsUnchangedNodes(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour) // docA 晚于 cursor
	tree := []model.ExternalNode{
		docxNodeWithEdit("docA", "", "文档A", t2), // t2 > cursor(t1) → Fetch
		docxNodeWithEdit("docB", "", "文档B", t1), // t1 == cursor → 跳过
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

// TestIncrementalSyncSoftDeletesMissingDocuments 验证飞书树中不存在的已有文档被软删。
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

	if len(h.store.softDeleted) != 1 || h.store.softDeleted[0] != docD.ID {
		t.Fatalf("expected docD %s to be soft-deleted; got %v", docD.ID, h.store.softDeleted)
	}
}

// TestIncrementalSyncDoesNotResoftDeleteAlreadyDeleted 验证已软删的文档不会被重复软删。
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

	if len(h.store.softDeleted) != 0 {
		t.Fatalf("已软删的 docD 不应被重复软删；soft-deleted = %v", h.store.softDeleted)
	}
}

// TestIncrementalSyncAdvancesCursorValue 验证同步成功后 UpdateSyncCursor 被调用，
// 值为所有节点 EditTime 的最大值（含 folder）。
func TestIncrementalSyncAdvancesCursorValue(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour)
	t3 := t2.Add(1 * time.Hour) // 最大
	tree := []model.ExternalNode{
		folderNodeWithEdit("folderA", "", "目录A", t3),
		docxNodeWithEdit("docA", "folderA", "文档A", t2),
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
	want := t3.UTC()
	if !got.Equal(want) {
		t.Fatalf("sync_cursor = %v, want %v (maxEditTime)", got, want)
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
