package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 web 内容源管理用例（v1.3.0 spec §9.2）的应用层单元行为：
// 创建校验矩阵（KB 存在/每库源数钳制/typed config/连接 provider 匹配/名称冲突）、
// PATCH 白名单与不可变键拒绝、Config 深拷贝合并语义、启停、手动重跑与删除两分支。

// fakeKbSourceStore 实现 KbSourceStore，记录调用供断言。
type fakeKbSourceStore struct {
	sources map[uuid.UUID]*model.KbSource
	// createErr/updateErr 注入持久化错误（如唯一名冲突映射的 ErrConflict）。
	createErr error
	updateErr error
	// pagesBySource 是 CountPagesBySource 的预置计数。
	pagesBySource map[uuid.UUID]SourcePageCounts
	// docIDsBySource 是 ListDocumentIDsBySource 的预置文档集。
	docIDsBySource map[uuid.UUID][]uuid.UUID
	// detachRows 是 DetachSourceDocuments 返回的行数。
	detachRows int64
	// activeSyncBySource 是 CountActiveBySource 的预置在飞任务数。
	activeSyncBySource map[uuid.UUID]int

	createCalls     int
	updateCalls     int
	softDeleteCalls []uuid.UUID
	detachCalls     []uuid.UUID
}

func newFakeKbSourceStore() *fakeKbSourceStore {
	return &fakeKbSourceStore{
		sources:            map[uuid.UUID]*model.KbSource{},
		pagesBySource:      map[uuid.UUID]SourcePageCounts{},
		docIDsBySource:     map[uuid.UUID][]uuid.UUID{},
		activeSyncBySource: map[uuid.UUID]int{},
	}
}

func (r *fakeKbSourceStore) Create(_ context.Context, source *model.KbSource) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	r.sources[source.ID] = source
	return nil
}

func (r *fakeKbSourceStore) Get(_ context.Context, workspaceID, kbID, sourceID uuid.UUID) (*model.KbSource, error) {
	source, ok := r.sources[sourceID]
	if !ok || source.WorkspaceID != workspaceID || source.KnowledgeBaseID != kbID {
		return nil, domainerrors.ErrNotFound
	}
	return source, nil
}

func (r *fakeKbSourceStore) List(_ context.Context, workspaceID, kbID uuid.UUID) ([]*model.KbSource, error) {
	out := make([]*model.KbSource, 0)
	for _, source := range r.sources {
		if source.WorkspaceID == workspaceID && source.KnowledgeBaseID == kbID {
			out = append(out, source)
		}
	}
	return out, nil
}

func (r *fakeKbSourceStore) CountByKB(_ context.Context, _, _ uuid.UUID) (int, error) {
	return len(r.sources), nil
}

func (r *fakeKbSourceStore) Update(_ context.Context, source *model.KbSource) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	r.sources[source.ID] = source
	return nil
}

func (r *fakeKbSourceStore) SoftDelete(_ context.Context, _, _, sourceID uuid.UUID) error {
	r.softDeleteCalls = append(r.softDeleteCalls, sourceID)
	delete(r.sources, sourceID)
	return nil
}

func (r *fakeKbSourceStore) CountPagesBySource(_ context.Context, _, _ uuid.UUID) (map[uuid.UUID]SourcePageCounts, error) {
	return r.pagesBySource, nil
}

func (r *fakeKbSourceStore) ListDocumentIDsBySource(_ context.Context, _, _, sourceID uuid.UUID) ([]uuid.UUID, error) {
	return append([]uuid.UUID(nil), r.docIDsBySource[sourceID]...), nil
}

func (r *fakeKbSourceStore) DetachSourceDocuments(_ context.Context, _, _, sourceID uuid.UUID) (int64, error) {
	r.detachCalls = append(r.detachCalls, sourceID)
	return r.detachRows, nil
}

func (r *fakeKbSourceStore) CountActiveBySource(_ context.Context, _, _, sourceID uuid.UUID) (int, error) {
	return r.activeSyncBySource[sourceID], nil
}

// fakeKbSourceEnqueuer 记录 EnqueueSyncForSource 调用。
type fakeKbSourceEnqueuer struct {
	calls  []uuid.UUID
	forces []bool
	job    *model.Job
	err    error
}

func (f *fakeKbSourceEnqueuer) EnqueueSyncForSource(_ context.Context, _, _, sourceID uuid.UUID, options SyncOptions) (*model.Job, error) {
	f.calls = append(f.calls, sourceID)
	f.forces = append(f.forces, options.Force)
	if f.err != nil {
		return nil, f.err
	}
	if f.job != nil {
		return f.job, nil
	}
	return &model.Job{ID: uuid.New(), Type: model.SourceSyncJobType}, nil
}

// fakeKbSourceRemover 记录 RemoveSourceDocuments 调用，返回可注入的删除数。
type fakeKbSourceRemover struct {
	calls   [][]uuid.UUID
	deleted int
	err     error
}

func (f *fakeKbSourceRemover) RemoveSourceDocuments(_ context.Context, _, _ uuid.UUID, documentIDs []uuid.UUID) (int, error) {
	f.calls = append(f.calls, documentIDs)
	if f.err != nil {
		return 0, f.err
	}
	return f.deleted, nil
}

// fakeKbSourceConnRepo 满足 SourceConnectionRepository：按 ID 精确匹配返回连接。
type fakeKbSourceConnRepo struct {
	conns map[uuid.UUID]model.SourceConnection
}

func newKbSourceConnRepo(workspaceID uuid.UUID, conns ...model.SourceConnection) *fakeKbSourceConnRepo {
	repo := &fakeKbSourceConnRepo{conns: map[uuid.UUID]model.SourceConnection{}}
	for _, conn := range conns {
		conn.WorkspaceID = workspaceID
		repo.conns[conn.ID] = conn
	}
	return repo
}

func (r *fakeKbSourceConnRepo) Create(_ context.Context, _ *model.SourceConnection) error {
	return errors.New("not implemented")
}

func (r *fakeKbSourceConnRepo) Get(_ context.Context, workspaceID, id uuid.UUID) (*model.SourceConnection, error) {
	conn, ok := r.conns[id]
	if !ok || conn.WorkspaceID != workspaceID {
		return nil, domainerrors.ErrNotFound
	}
	return &conn, nil
}

func (r *fakeKbSourceConnRepo) List(_ context.Context, _ uuid.UUID) ([]*model.SourceConnection, error) {
	return nil, errors.New("not implemented")
}

func (r *fakeKbSourceConnRepo) Update(_ context.Context, _ *model.SourceConnection) error {
	return errors.New("not implemented")
}

func (r *fakeKbSourceConnRepo) SoftDelete(_ context.Context, _, _ uuid.UUID) error {
	return errors.New("not implemented")
}

// kbSourceFixture 是被测服务及其 fake KB / store 的组合。
type kbSourceFixture struct {
	svc      *KbSourceService
	kb       *model.KnowledgeBase
	store    *fakeKbSourceStore
	enqueuer *fakeKbSourceEnqueuer
	remover  *fakeKbSourceRemover
}

// newKbSourceFixture 组装被测服务：KB 存在、web 源天花板 100/10。
func newKbSourceFixture(maxSourcesPerKB int) *kbSourceFixture {
	kb := &model.KnowledgeBase{ID: uuid.New(), WorkspaceID: uuid.New(), Name: "测试库"}
	fixture := &kbSourceFixture{
		kb:       kb,
		store:    newFakeKbSourceStore(),
		enqueuer: &fakeKbSourceEnqueuer{},
		remover:  &fakeKbSourceRemover{},
	}
	fixture.svc = NewKbSourceService(KbSourceServiceDeps{
		KBs:             &fakeKBSyncRepo{kb: kb},
		Store:           fixture.store,
		SyncEnqueuer:    fixture.enqueuer,
		Remover:         fixture.remover,
		Limits:          value.WebSourceLimits{MaxPagesCeiling: 100, MaxDepthCeiling: 5},
		MaxSourcesPerKB: maxSourcesPerKB,
	})
	return fixture
}

func (f *kbSourceFixture) withConnections(conns ...model.SourceConnection) *kbSourceFixture {
	f.svc.conns = newKbSourceConnRepo(f.kb.WorkspaceID, conns...)
	return f
}

func (f *kbSourceFixture) create(ctx context.Context, name string, config map[string]any) (*model.KbSource, *model.Job, error) {
	return f.svc.CreateWebSource(ctx, CreateWebSourceInput{
		WorkspaceID: f.kb.WorkspaceID, KnowledgeBaseID: f.kb.ID, Name: name, Config: config,
	})
}

// seedWebSource 向 fake store 预置一个 urls 模式源。
func (f *kbSourceFixture) seedWebSource(t *testing.T, name string) *model.KbSource {
	t.Helper()
	source, err := model.NewKbSource(model.NewKbSourceInput{
		WorkspaceID: f.kb.WorkspaceID, KnowledgeBaseID: f.kb.ID,
		Kind: value.KbSourceKindWeb, Name: name, Config: webSourceConfig(),
	})
	if err != nil {
		t.Fatalf("NewKbSource: %v", err)
	}
	f.store.sources[source.ID] = source
	return source
}

func webSourceConfig() map[string]any {
	return map[string]any{
		"mode": "urls",
		"urls": []any{"https://example.com/a"},
		"crawl": map[string]any{
			"respect_robots": false,
		},
	}
}

func TestCreateWebSourceValidationMatrix(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]any
		nameArg string
		wantErr error
		errText string
	}{
		{
			name:    "invalid mode rejected",
			config:  map[string]any{"mode": "mystery"},
			nameArg: "源",
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "runtime key rejected",
			config: func() map[string]any {
				config := webSourceConfig()
				config["sync_cursor"] = map[string]any{}
				return config
			}(),
			nameArg: "源",
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "pages over ceiling rejected not clamped",
			config: map[string]any{
				"mode":  "crawl",
				"crawl": map[string]any{"seed_url": "https://example.com/", "max_pages": float64(10000)},
			},
			nameArg: "源",
			wantErr: domainerrors.ErrValidation,
			errText: "超过进程天花板",
		},
		{
			name:    "empty name rejected",
			config:  webSourceConfig(),
			nameArg: "   ",
			wantErr: domainerrors.ErrValidation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newKbSourceFixture(10)
			_, _, err := fixture.create(context.Background(), tt.nameArg, tt.config)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateWebSource error = %v, want %v", err, tt.wantErr)
			}
			if tt.errText != "" && !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("CreateWebSource error = %q, want contains %q", err, tt.errText)
			}
		})
	}
}

// TestCreateWebSourceUnknownKB 校验 KB 必须存在且未删（fake 只返回预置 KB）。
func TestCreateWebSourceUnknownKB(t *testing.T) {
	fixture := newKbSourceFixture(10)
	_, _, err := fixture.svc.CreateWebSource(context.Background(), CreateWebSourceInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), Name: "源", Config: webSourceConfig(),
	})
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCreateWebSourceQuotaClamp(t *testing.T) {
	fixture := newKbSourceFixture(2)
	for i := 0; i < 2; i++ {
		if _, _, err := fixture.create(context.Background(), "源"+string(rune('A'+i)), webSourceConfig()); err != nil {
			t.Fatalf("CreateWebSource #%d: %v", i, err)
		}
	}
	_, _, err := fixture.create(context.Background(), "源C", webSourceConfig())
	if !errors.Is(err, domainerrors.ErrValidation) || !strings.Contains(err.Error(), "上限 2") {
		t.Fatalf("quota error = %v, want 上限 2", err)
	}
}

func TestCreateWebSourceNameConflict(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.store.createErr = domainerrors.ErrConflict
	_, _, err := fixture.create(context.Background(), "文档站", webSourceConfig())
	if !errors.Is(err, domainerrors.ErrConflict) || !strings.Contains(err.Error(), "同名内容源已存在") {
		t.Fatalf("name conflict error = %v, want 同名内容源已存在", err)
	}
}

func TestCreateWebSourceConnectionValidation(t *testing.T) {
	browserConn := model.SourceConnection{ID: uuid.New(), Provider: "browser", Status: "active"}
	firecrawlConn := model.SourceConnection{ID: uuid.New(), Provider: "firecrawl", Status: "active"}
	disabledConn := model.SourceConnection{ID: uuid.New(), Provider: "browser", Status: "disabled"}

	tests := []struct {
		name    string
		config  map[string]any
		connID  uuid.UUID
		wantErr error
		errText string
	}{
		{
			name:   "http engine without connection ok",
			config: webSourceConfig(),
		},
		{
			name: "render engine requires connection",
			config: map[string]any{
				"mode":  "urls",
				"urls":  []any{"https://example.com/"},
				"fetch": map[string]any{"engine": "render"},
			},
			wantErr: domainerrors.ErrValidation,
			errText: "需要 fetch.connection_id",
		},
		{
			name: "render engine with browser connection ok",
			config: map[string]any{
				"mode":  "urls",
				"urls":  []any{"https://example.com/"},
				"fetch": map[string]any{"engine": "render", "connection_id": browserConn.ID.String()},
			},
			connID: browserConn.ID,
		},
		{
			name: "render engine with mismatched provider rejected",
			config: map[string]any{
				"mode":  "urls",
				"urls":  []any{"https://example.com/"},
				"fetch": map[string]any{"engine": "render", "connection_id": firecrawlConn.ID.String()},
			},
			connID:  firecrawlConn.ID,
			wantErr: domainerrors.ErrValidation,
			errText: "不允许 firecrawl 连接",
		},
		{
			name: "disabled connection rejected",
			config: map[string]any{
				"mode":  "urls",
				"urls":  []any{"https://example.com/"},
				"fetch": map[string]any{"engine": "render", "connection_id": disabledConn.ID.String()},
			},
			connID:  disabledConn.ID,
			wantErr: domainerrors.ErrValidation,
			errText: "已停用",
		},
		{
			name: "missing connection rejected",
			config: map[string]any{
				"mode":  "urls",
				"urls":  []any{"https://example.com/"},
				"fetch": map[string]any{"engine": "render", "connection_id": uuid.New().String()},
			},
			wantErr: domainerrors.ErrValidation,
			errText: "不存在",
		},
		{
			name: "auto engine accepts any external provider",
			config: map[string]any{
				"mode":  "urls",
				"urls":  []any{"https://example.com/"},
				"fetch": map[string]any{"engine": "auto", "connection_id": firecrawlConn.ID.String()},
			},
			connID: firecrawlConn.ID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newKbSourceFixture(10).
				withConnections(browserConn, firecrawlConn, disabledConn)
			source, job, err := fixture.create(context.Background(), "文档站", tt.config)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || (tt.errText != "" && !strings.Contains(err.Error(), tt.errText)) {
					t.Fatalf("CreateWebSource error = %v, want %v (%q)", err, tt.wantErr, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateWebSource error = %v", err)
			}
			if source.Status != value.KbSourceStatusActive {
				t.Fatalf("status = %q, want active（缺省）", source.Status)
			}
			if len(fixture.enqueuer.calls) != 1 || fixture.enqueuer.calls[0] != source.ID {
				t.Fatalf("首次同步未入队: calls=%v", fixture.enqueuer.calls)
			}
			if job == nil {
				t.Fatal("job = nil, want 首次同步 Job")
			}
		})
	}
}

func TestCreateWebSourceEnqueueFailureDoesNotRollback(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.enqueuer.err = errors.New("queue down")
	source, job, err := fixture.create(context.Background(), "文档站", webSourceConfig())
	if err != nil {
		t.Fatalf("CreateWebSource error = %v", err)
	}
	if job != nil {
		t.Fatalf("job = %v, want nil（入队失败）", job.ID)
	}
	if _, ok := fixture.store.sources[source.ID]; !ok {
		t.Fatal("源未落库：入队失败不应回滚创建")
	}
}

func TestUpdateWebSourcePatchMatrix(t *testing.T) {
	status := value.KbSourceStatus("paused")
	empty := "  "
	tests := []struct {
		name    string
		input   func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput
		wantErr error
		errText string
	}{
		{
			name: "no fields",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID, SourceID: source.ID}
			},
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "unknown top level key rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"mode": "crawl"}}
			},
			wantErr: domainerrors.ErrValidation,
			errText: "不允许修改",
		},
		{
			name: "runtime key rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"sync_last_result": map[string]any{}}}
			},
			wantErr: domainerrors.ErrValidation,
			errText: "不允许修改",
		},
		{
			name: "immutable crawl.seed_url rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"crawl": map[string]any{"seed_url": "https://other.com/"}}}
			},
			wantErr: domainerrors.ErrValidation,
			errText: "crawl.seed_url",
		},
		{
			name: "immutable fetch.engine rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"fetch": map[string]any{"engine": "render"}}}
			},
			wantErr: domainerrors.ErrValidation,
			errText: "fetch.engine",
		},
		{
			name: "immutable sitemap.url rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"sitemap": map[string]any{"url": "https://other.com/sitemap.xml"}}}
			},
			wantErr: domainerrors.ErrValidation,
			errText: "sitemap.url",
		},
		{
			name: "immutable fetch.connection_id rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"fetch": map[string]any{"connection_id": uuid.New().String()}}}
			},
			wantErr: domainerrors.ErrValidation,
			errText: "fetch.connection_id",
		},
		{
			name: "invalid merged value rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"on_delete": "bogus"}}
			},
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "unknown sub object key rejected after merge",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Config: map[string]any{"crawl": map[string]any{"max_deapth": float64(3)}}}
			},
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "empty name rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Name: &empty}
			},
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "invalid status rejected",
			input: func(source *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: source.ID, Status: &status}
			},
			wantErr: domainerrors.ErrValidation,
		},
		{
			name: "missing source",
			input: func(_ *model.KbSource, kb *model.KnowledgeBase) UpdateWebSourceInput {
				return UpdateWebSourceInput{WorkspaceID: kb.WorkspaceID, KnowledgeBaseID: kb.ID,
					SourceID: uuid.New(), Name: ptrKbSourceString("新名")}
			},
			wantErr: domainerrors.ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newKbSourceFixture(10)
			source := fixture.seedWebSource(t, "文档站")
			input := tt.input(source, fixture.kb)
			_, _, err := fixture.svc.UpdateWebSource(context.Background(), input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateWebSource error = %v, want %v", err, tt.wantErr)
			}
			if tt.errText != "" && !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("UpdateWebSource error = %q, want contains %q", err, tt.errText)
			}
		})
	}
}

func TestUpdateWebSourceMergeAndDeepCopy(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")

	oldConfig := source.Config
	patched, job, err := fixture.svc.UpdateWebSource(context.Background(), UpdateWebSourceInput{
		WorkspaceID: fixture.kb.WorkspaceID, KnowledgeBaseID: fixture.kb.ID, SourceID: source.ID,
		Name:   ptrKbSourceString("改名站"),
		Status: ptrKbSourceStatus(value.KbSourceStatusDisabled),
		Config: map[string]any{
			"urls":      []any{"https://example.com/b", "https://example.com/c"},
			"crawl":     map[string]any{"max_depth": float64(4)},
			"on_delete": "remove",
		},
	})
	if err != nil {
		t.Fatalf("UpdateWebSource error = %v", err)
	}
	if patched.Name != "改名站" || patched.Status != value.KbSourceStatusDisabled {
		t.Fatalf("patched = %q/%q", patched.Name, patched.Status)
	}
	// 深拷贝语义：旧 config map 不被原地修改（T4 评审移交）。
	if _, changed := oldConfig["on_delete"]; changed {
		t.Fatal("旧 config 被原地修改：深拷贝语义被破坏")
	}
	// urls 整体替换；crawl 子对象递归合并（respect_robots 保留 + max_depth 更新）。
	typed, err := value.ParseWebSourceConfig(patched.Config, fixture.svc.limits)
	if err != nil {
		t.Fatalf("patched config 非法: %v", err)
	}
	if len(typed.URLs) != 2 || typed.URLs[0] != "https://example.com/b" {
		t.Fatalf("urls 应整体替换: %v", typed.URLs)
	}
	if typed.Crawl.MaxDepth != 4 || typed.Crawl.RespectRobots {
		t.Fatalf("crawl 合并错误: %+v", typed.Crawl)
	}
	if typed.OnDelete != value.SourceDeleteRemove {
		t.Fatalf("on_delete = %q, want remove", typed.OnDelete)
	}
	// PATCH 成功即入队（非 force）。
	if len(fixture.enqueuer.calls) != 1 || fixture.enqueuer.calls[0] != source.ID || fixture.enqueuer.forces[0] {
		t.Fatalf("PATCH 后入队错误: calls=%v forces=%v", fixture.enqueuer.calls, fixture.enqueuer.forces)
	}
	if job == nil {
		t.Fatal("job = nil, want PATCH 同步 Job")
	}
}

// TestUpdateWebSourcePartialDoesNotMutateNestedMap 断言合并产物与旧 map 独立：
// 合并写入深拷贝副本，不触碰旧引用持有的嵌套对象。
func TestUpdateWebSourcePartialDoesNotMutateNestedMap(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")
	oldCrawl := source.Config["crawl"].(map[string]any)

	if _, _, err := fixture.svc.UpdateWebSource(context.Background(), UpdateWebSourceInput{
		WorkspaceID: fixture.kb.WorkspaceID, KnowledgeBaseID: fixture.kb.ID, SourceID: source.ID,
		Config: map[string]any{"crawl": map[string]any{"max_pages": float64(20)}},
	}); err != nil {
		t.Fatalf("UpdateWebSource error = %v", err)
	}
	if _, changed := oldCrawl["max_pages"]; changed {
		t.Fatal("旧 crawl 子对象被原地修改：合并必须写进深拷贝副本")
	}
}

// TestUpdateWebSourceNameConflictMapsConflict 改名撞唯一索引映射为 ErrConflict。
func TestUpdateWebSourceNameConflictMapsConflict(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")
	fixture.store.updateErr = domainerrors.ErrConflict
	_, _, err := fixture.svc.UpdateWebSource(context.Background(), UpdateWebSourceInput{
		WorkspaceID: fixture.kb.WorkspaceID, KnowledgeBaseID: fixture.kb.ID, SourceID: source.ID,
		Name: ptrKbSourceString("占用名"),
	})
	if !errors.Is(err, domainerrors.ErrConflict) || !strings.Contains(err.Error(), "同名内容源已存在") {
		t.Fatalf("rename conflict error = %v, want 同名内容源已存在", err)
	}
}

func TestSetWebSourceStatus(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")
	ctx := context.Background()

	if err := fixture.svc.SetWebSourceStatus(ctx, fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, value.KbSourceStatusDisabled); err != nil {
		t.Fatalf("SetWebSourceStatus(disabled) error = %v", err)
	}
	if got := fixture.store.sources[source.ID].Status; got != value.KbSourceStatusDisabled {
		t.Fatalf("status = %q, want disabled", got)
	}
	if err := fixture.svc.SetWebSourceStatus(ctx, fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, value.KbSourceStatus("off")); err == nil {
		t.Fatal("非法状态应报错")
	}
	if err := fixture.svc.SetWebSourceStatus(ctx, fixture.kb.WorkspaceID, fixture.kb.ID, uuid.New(), value.KbSourceStatusActive); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("未知源 error = %v, want ErrNotFound", err)
	}
}

func TestSyncWebSourceDelegates(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")
	ctx := context.Background()

	if _, err := fixture.svc.SyncWebSource(ctx, fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, true); err != nil {
		t.Fatalf("SyncWebSource error = %v", err)
	}
	if len(fixture.enqueuer.calls) != 1 || fixture.enqueuer.calls[0] != source.ID || !fixture.enqueuer.forces[0] {
		t.Fatalf("calls=%v forces=%v", fixture.enqueuer.calls, fixture.enqueuer.forces)
	}
	if _, err := fixture.svc.SyncWebSource(ctx, fixture.kb.WorkspaceID, fixture.kb.ID, uuid.New(), false); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("未知源 error = %v, want ErrNotFound", err)
	}
}

func TestDeleteWebSourceKeepBranch(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.store.detachRows = 3
	source := fixture.seedWebSource(t, "文档站")

	if err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, "keep"); err != nil {
		t.Fatalf("DeleteWebSource error = %v", err)
	}
	if len(fixture.store.detachCalls) != 1 || fixture.store.detachCalls[0] != source.ID {
		t.Fatalf("detach calls = %v", fixture.store.detachCalls)
	}
	if len(fixture.remover.calls) != 0 {
		t.Fatal("keep 分支不应删除页面")
	}
	if len(fixture.store.softDeleteCalls) != 1 || fixture.store.softDeleteCalls[0] != source.ID {
		t.Fatalf("soft delete calls = %v", fixture.store.softDeleteCalls)
	}

	// 空参数默认 keep。
	fixture2 := newKbSourceFixture(10)
	source2 := fixture2.seedWebSource(t, "文档站")
	if err := fixture2.svc.DeleteWebSource(context.Background(), fixture2.kb.WorkspaceID, fixture2.kb.ID, source2.ID, ""); err != nil {
		t.Fatalf("DeleteWebSource(默认) error = %v", err)
	}
	if len(fixture2.store.detachCalls) != 1 {
		t.Fatal("默认策略应为 keep（断链）")
	}
}

func TestDeleteWebSourceRemoveBranch(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.remover.deleted = 2
	source := fixture.seedWebSource(t, "文档站")
	fixture.store.docIDsBySource[source.ID] = []uuid.UUID{uuid.New(), uuid.New()}

	if err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, "remove"); err != nil {
		t.Fatalf("DeleteWebSource error = %v", err)
	}
	if len(fixture.remover.calls) != 1 || len(fixture.remover.calls[0]) != 2 {
		t.Fatalf("remover calls = %v", fixture.remover.calls)
	}
	if len(fixture.store.softDeleteCalls) != 1 {
		t.Fatal("remove 分支应软删源行")
	}
}

func TestDeleteWebSourceRemovePartialFailureKeepsSource(t *testing.T) {
	fixture := newKbSourceFixture(10)
	fixture.remover.deleted = 1
	source := fixture.seedWebSource(t, "文档站")
	fixture.store.docIDsBySource[source.ID] = []uuid.UUID{uuid.New(), uuid.New()}

	err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, "remove")
	if err == nil || !strings.Contains(err.Error(), "1/2 未完成") {
		t.Fatalf("部分删除 error = %v, want 1/2 未完成", err)
	}
	if len(fixture.store.softDeleteCalls) != 0 {
		t.Fatal("页面未删净时不得软删源行（可重试）")
	}
}

func TestDeleteWebSourceRejectsUnknownPagesPolicy(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")
	err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, "purge")
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("pages=purge error = %v, want ErrValidation", err)
	}
}

// TestDeleteWebSourceRejectsWhileSyncActive 在飞守卫（T10 评审 Minor-2）：
// 源有进行中的同步任务时，keep/remove 两分支都拒绝删除（ErrConflict，稍后可重试），
// 且不得触碰页面与源行——remove 分支软删源后新建页面会挂在已删源下残留。
func TestDeleteWebSourceRejectsWhileSyncActive(t *testing.T) {
	for _, policy := range []string{"keep", "remove"} {
		t.Run(policy, func(t *testing.T) {
			fixture := newKbSourceFixture(10)
			fixture.remover.deleted = 1
			source := fixture.seedWebSource(t, "文档站")
			fixture.store.activeSyncBySource[source.ID] = 1
			fixture.store.docIDsBySource[source.ID] = []uuid.UUID{uuid.New()}

			err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, policy)
			if !errors.Is(err, domainerrors.ErrConflict) || !strings.Contains(err.Error(), "正在进行的同步任务") {
				t.Fatalf("pages=%s error = %v, want ErrConflict(正在进行的同步任务)", policy, err)
			}
			if len(fixture.store.detachCalls) != 0 || len(fixture.remover.calls) != 0 || len(fixture.store.softDeleteCalls) != 0 {
				t.Fatalf("pages=%s 在飞任务存在时不得执行删除: detach=%v remove=%v softDelete=%v",
					policy, fixture.store.detachCalls, fixture.remover.calls, fixture.store.softDeleteCalls)
			}

			// 任务结束后同参数重试成功。
			fixture.store.activeSyncBySource[source.ID] = 0
			if err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID, policy); err != nil {
				t.Fatalf("pages=%s 重试删除 error = %v", policy, err)
			}
			if len(fixture.store.softDeleteCalls) != 1 {
				t.Fatalf("pages=%s 重试后应软删源行", policy)
			}
		})
	}
}

func TestDeleteWebSourceMissingSource(t *testing.T) {
	fixture := newKbSourceFixture(10)
	err := fixture.svc.DeleteWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, uuid.New(), "keep")
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestListAndGetWebSourcesWithCounts(t *testing.T) {
	fixture := newKbSourceFixture(10)
	source := fixture.seedWebSource(t, "文档站")
	fixture.store.pagesBySource[source.ID] = SourcePageCounts{PageCount: 7, FailedPageCount: 2}

	views, err := fixture.svc.ListWebSources(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID)
	if err != nil {
		t.Fatalf("ListWebSources error = %v", err)
	}
	if len(views) != 1 || views[0].PageCount != 7 || views[0].FailedPageCount != 2 {
		t.Fatalf("views = %+v", views)
	}
	view, err := fixture.svc.GetWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, source.ID)
	if err != nil {
		t.Fatalf("GetWebSource error = %v", err)
	}
	if view.Source.ID != source.ID || view.PageCount != 7 {
		t.Fatalf("view = %+v", view)
	}
	if _, err := fixture.svc.GetWebSource(context.Background(), fixture.kb.WorkspaceID, fixture.kb.ID, uuid.New()); !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("未知源 error = %v, want ErrNotFound", err)
	}
}

func ptrKbSourceString(s string) *string { return &s }

func ptrKbSourceStatus(s value.KbSourceStatus) *value.KbSourceStatus { return &s }
