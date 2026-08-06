package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

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
	tree     []model.ExternalNode
	fetched  map[string]model.FetchedDocument
	fetchFn  func(externalID string) (model.FetchedDocument, error)
	provider string
	listErr  error
}

func (c *fakeSourceConnector) ListTree(_ context.Context, _ model.SourceConnection, _ model.SyncRoot) ([]model.ExternalNode, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]model.ExternalNode(nil), c.tree...), nil
}

func (c *fakeSourceConnector) Fetch(_ context.Context, _ model.SourceConnection, externalID string) (model.FetchedDocument, error) {
	if c.fetchFn != nil {
		return c.fetchFn(externalID)
	}
	if doc, ok := c.fetched[externalID]; ok {
		return doc, nil
	}
	return model.FetchedDocument{}, fmt.Errorf("fetch not stubbed for %s", externalID)
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
	kb *model.KnowledgeBase
}

func (r *fakeKBSyncRepo) Get(_ context.Context, workspaceID, id uuid.UUID) (*model.KnowledgeBase, error) {
	if r.kb == nil || r.kb.WorkspaceID != workspaceID || r.kb.ID != id {
		return nil, domainerrors.ErrNotFound
	}
	return r.kb, nil
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
