# 飞书知识库同步 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Workspace 可以配置多个飞书内部应用，创建知识库时选择飞书云文档/飞书知识库来源，输入 token/URL 后自动同步整棵目录树入库并执行分块向量化；同一应用的多个知识库同步任务按 `max_concurrent_per_connection` 串行排队，并支持 cron 定时增量。

**Architecture:** 新增 `workspace_source_connections`（多应用凭证，app_secret 复用 `credential_cipher` 加密）、扩展 `knowledge_bases/documents/jobs` 三表（来源字段 + external_id + connection 维度）。飞书文档以 `file` kind 落库（当 markdown 处理），复用 FileTree 与 parse→chunk→index 下游管线。同步调度采用进程内单 goroutine Meta Scheduler 做 check-then-act（asynq 静态队列不支持热加载/按队列限流），worker 完成后主动续跑。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL 17、asynq + Redis、React 19、TypeScript 7、TanStack Router/Query、React Hook Form、Zod、Tailwind CSS v4、shadcn/Radix、Vitest、Biome。

## Global Constraints

- 权威设计：`docs/superpowers/specs/2026-08-06-feishu-knowledge-base-sync-design.md`。
- 飞书文档以 `file` kind 落库，不新增 DocumentKind；来源语义由 `documents.source_type="feishu"` + `external_id` 承载。
- 凭证（app_secret）只走 AES-256-GCM（`credential_cipher`，AAD 绑定连接 ID），不写入 YAML；`app_id` 等非敏感配置存 `config jsonb`。
- 调度限流只能在 Meta Scheduler 单 goroutine 内 check-then-act，不依赖 asynq 多队列/按队列并发上限。
- 所有 Workspace 数据库访问显式携带 workspace_id 并运行在 Workspace transaction 内。
- 写接口由 SessionAuth + RequireWorkspace + RequireWorkspaceRole(value.RoleAdmin) 保护；member 只读；API Key 不可访问凭证与同步管理。
- 普通界面不显示 UUID、凭证明文、raw payload；同步状态用文字+图标，不只靠颜色。
- 自动化数据库测试只使用测试期临时 Docker PostgreSQL/pgvector；飞书 API 测试只用 `httptest`/fake server，不发真实网络请求。
- 所有生产行为严格测试先行（RED → 实现 → GREEN → 提交 gate）。
- 单进程部署竞态可接受；多进程分布式锁列为风险，首版不做。

---

## 文件结构与职责

| 路径 | 职责 |
|---|---|
| `internal/domain/value/source_type.go` | KnowledgeBaseSourceType 枚举（upload/feishu_drive/feishu_wiki）与校验。 |
| `internal/domain/model/source_connection.go` | SourceConnection 领域模型与构造校验。 |
| `internal/domain/value/sync_root.go` | SyncRoot（kind+token）与 URL 解析结果。 |
| `internal/domain/model/external_node.go` | ExternalNode（飞书树节点）与 FetchedDocument。 |
| `internal/ports/source/connector.go` | SourceConnector port（ListTree/Fetch）。 |
| `internal/adapters/source/feishu/` | 飞书 token/wiki/drive/docx client + connector + url_parser。 |
| `internal/infrastructure/db/source_connection_*.go` | SourceConnectionRow + Repository（CRUD + 按 workspace 查）。 |
| `internal/infrastructure/db/source_sync_store.go` | CreateSourceSyncJob + CountActiveByConnection（限流查询）。 |
| `internal/application/service/source_connection_selector.go` | 凭证选择器（解密 app_secret）。 |
| `internal/application/service/source_sync.go` | SourceSyncService（同步主流程）。 |
| `internal/application/service/source_sync_scheduler.go` | Meta Scheduler（tick + TryDispatchConnection）。 |
| `internal/interfaces/worker/source_sync_tasks.go` | source_sync worker handler。 |
| `internal/interfaces/http/source_connection_handler.go` | 飞书应用管理 REST。 |
| `internal/interfaces/http/knowledge_base.go` | KB 创建扩展来源字段。 |
| `web/src/features/integrations/` | 飞书应用列表/表单 feature。 |
| `web/src/features/knowledge-bases/components/knowledge-base-form.tsx` | KB 创建来源切换。 |

## 跨任务接口锁定

```go
// ports/source/connector.go
type SourceConnector interface {
	ListTree(ctx context.Context, conn model.SourceConnection, root value.SyncRoot) ([]model.ExternalNode, error)
	Fetch(ctx context.Context, conn model.SourceConnection, externalID string) (model.FetchedDocument, error)
}

// model.SourceConnection
type SourceConnection struct {
	ID, WorkspaceID        uuid.UUID
	Provider               string                 // "feishu"
	Name                   string
	Config                 map[string]any         // {app_id, base_url?}
	CredentialsCiphertext  []byte                 // app_secret 密文
	Status                 string                 // active/disabled
	CreatedAt, UpdatedAt   time.Time
	DeletedAt              *time.Time
}

// value.SyncRoot
type SyncRoot struct {
	Kind  string // "drive_folder" | "wiki_node" | "wiki_space"
	Token string
}

// service.SourceSyncService
func (s *SourceSyncService) SyncKnowledgeBase(ctx context.Context, workspaceID, kbID uuid.UUID) error

// service.SourceSyncScheduler
func (s *SourceSyncScheduler) Tick(ctx context.Context) error
func (s *SourceSyncScheduler) TryDispatchConnection(ctx context.Context, workspaceID, connID uuid.UUID) error
```

```ts
// web/src/features/integrations/search-params.ts
export const sourceConnectionSchema = z.object({
  provider: z.literal('feishu'),
  name: z.string().min(1).max(64),
  app_id: z.string().min(1).max(128),
  app_secret: z.string().min(1).max(256),
})
```

---

### Task 1: 放宽 Job 目标约束与状态枚举对齐

**Files:**

- Modify: `internal/infrastructure/migrate/migrations/000016_source_sync.up.sql`（新建）
- Modify: `internal/infrastructure/migrate/migrations/000016_source_sync.down.sql`（新建）
- Modify: `internal/domain/model/job.go`
- Modify: `internal/domain/model/job_test.go`

**Interfaces:**

- `model.NewJob` 接受 `Type="source_sync"` 时允许 `DocumentID/DocumentRevisionID/IndexGenerationID` 全 nil。
- DB `jobs_target_check` 增加第三分支：`(document_id IS NULL AND document_revision_id IS NULL AND index_generation_id IS NULL AND type = 'source_sync')`。

> 这是所有后续同步任务的前置关卡。已确认现有 `jobs_target_check`（`000005...up.sql:478-481`）严格二选一，`NewJob`（`job.go:46-52`）两 nil 直接报错。不先打通这两关，source_sync job 无法落库。

- [x] **Step 1: 写 NewJob 放宽的失败测试。**

```go
func TestNewJobAllowsSourceSyncWithOnlyKnowledgeBase(t *testing.T) {
	job, err := model.NewJob(model.NewJobInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		Type: "source_sync", Status: value.JobStatusPending,
	})
	if err != nil { t.Fatalf("expected nil err, got %v", err) }
	if job.Type != "source_sync" { t.Fatal(job.Type) }
}

func TestNewJobStillRejectsUnknownTypeWithAllNilTargets(t *testing.T) {
	_, err := model.NewJob(model.NewJobInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
		Type: "document_parse_start", Status: value.JobStatusPending,
	})
	if !errors.Is(err, domainerrors.ErrValidation) { t.Fatalf("want ErrValidation, got %v", err) }
}
```

- [x] **Step 2: 运行聚焦测试并确认 RED。**

Run: `go test ./internal/domain/model -run 'NewJobAllowsSourceSync|RejectsUnknownType' -count=1`

Expected: FAIL，`NewJob` 仍对全 nil 报错。

- [x] **Step 3: 放宽 NewJob + 写迁移。**

`NewJob`：仅当 `Type=="source_sync"` 时允许三者全 nil。迁移 `000017`：`ALTER TABLE jobs DROP CONSTRAINT jobs_target_check`，重建带第三分支的 CHECK。

- [x] **Step 4: 运行领域测试 + 集成迁移测试。**

Run: `go test ./internal/domain/model -run NewJob -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/migrate -run V017 -count=1`

Expected: PASS；空库 up→down 全成功，source_sync job 可落库。

- [x] **Step 5: 提交。**

```bash
git add internal/domain/model internal/infrastructure/migrate
git commit -m "feat(job): 放宽目标约束以支持 source_sync 任务"
```

### Task 2: 数据模型与多应用凭证持久化

**Files:**

- Modify: `internal/domain/value/source_type.go`（新建）
- Modify: `internal/domain/model/source_connection.go`（新建）
- Modify: `internal/domain/model/knowledge_base.go`
- Modify: `internal/domain/model/document.go`
- Modify: `internal/domain/model/job.go`
- Modify: `internal/infrastructure/db/source_connection_rows.go`（新建）
- Modify: `internal/infrastructure/db/source_connection_repository.go`（新建）
- Modify: `internal/infrastructure/db/knowledge_rows.go`
- Modify: `internal/infrastructure/db/document_rows.go`
- Modify: `internal/infrastructure/db/job_rows.go`
- Modify: `internal/infrastructure/db/models.go`（AutoMigrateModels 登记）
- Modify: `internal/infrastructure/migrate/migrations/000016_*.up.sql`（本任务补全表结构）
- Test: 对应 `_test.go`

**Interfaces:**

- `value.KnowledgeBaseSourceType`（upload/feishu_drive/feishu_wiki + `IsValid()`）。
- `model.SourceConnection` 构造校验：name 非空、provider 固定集合、scope/workspace_id 组合一致。
- `model.KnowledgeBase` 加 `SourceType/SourceConfig/SourceConnectionID`；`NewKnowledgeBase` 签名扩展。
- `model.Document` 加 `ExternalID string`；`model.Job` 加 `SourceConnectionID *uuid.UUID`。

- [x] **Step 1: 写领域模型失败测试。**

```go
func TestSourceConnectionRejectsEmptySecret(t *testing.T) {
	_, err := model.NewSourceConnection(model.NewSourceConnectionInput{
		WorkspaceID: ws, Provider: "feishu", Name: "主公司飞书",
		AppID: "cli_a1b2", AppSecret: "",
	})
	if !errors.Is(err, domainerrors.ErrValidation) { t.Fatal(err) }
}

func TestNewKnowledgeBaseAcceptsFeishuSourceWithConnection(t *testing.T) {
	kb, err := model.NewKnowledgeBase(ws, "产品手册", "", modelID, chunking,
		value.SourceTypeFeishuWiki, map[string]any{"root_token": "wikcnB"}, &connID)
	if err != nil { t.Fatal(err) }
	if kb.SourceType != value.SourceTypeFeishuWiki { t.Fatal(kb.SourceType) }
}

func TestNewKnowledgeBaseFeishuRequiresConnection(t *testing.T) {
	_, err := model.NewKnowledgeBase(ws, "x", "", modelID, chunking,
		value.SourceTypeFeishuWiki, map[string]any{}, nil)
	if !errors.Is(err, domainerrors.ErrValidation) { t.Fatal(err) }
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/domain/model -run 'SourceConnection|NewKnowledgeBase' -count=1`

Expected: FAIL，类型与构造器尚不存在。

- [x] **Step 3: 实现领域模型、Row、Repository、迁移。**

迁移补全：新建 `workspace_source_connections`（UNIQUE(workspace_id,provider,name) + UNIQUE(workspace_id,provider,(config->>'app_id'))）；扩展 `knowledge_bases`（source_type DEFAULT 'upload'、source_config jsonb、source_connection_id FK）；扩展 `documents`（external_id + 部分索引）；扩展 `jobs`（source_connection_id + 部分索引）。Row + codec + `AutoMigrateModels()` 追加 `&SourceConnectionRow{}`。

- [x] **Step 4: 运行领域测试 + 临时库集成测试。**

Run: `go test ./internal/domain/model ./internal/infrastructure/db -run 'SourceConnection|KnowledgeBase|ExternalID' -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'SourceConnection' -count=1`

Expected: PASS；多应用、同名/同 app_id 拒绝、external_id 索引、KB 来源字段 round-trip。

- [x] **Step 5: 提交。**

```bash
git add internal/domain internal/infrastructure/db internal/infrastructure/migrate
git commit -m "feat(db): 建立飞书多应用连接与来源字段"
```

### Task 3: SourceConnection 凭证加密 Service 与 Selector

**Files:**

- Create: `internal/application/service/source_connection.go`
- Create: `internal/application/service/source_connection_test.go`
- Create: `internal/application/service/source_connection_selector.go`
- Create: `internal/application/service/source_connection_selector_test.go`
- Modify: `internal/application/service/source_connection_selector.go`（复用 `credentialDecryptor` 子集接口模式）

**Interfaces:**

- `SourceConnectionService.Create/Update/List/Delete`：app_secret 经 `cipher.Encrypt(connID, []byte)` 落库；List 不回显 secret。
- `SourceConnectionSelector.Select(ctx, workspaceID, connID) (SelectedSourceConnection, error)`：解密返回 `{Connection, AppSecret}`，供 SourceConnector 使用。

> AAD 复用：`credential_cipher.go:71` 的 `credentialAAD` 当前前缀是 `"model-provider:"`。飞书连接要么新增一个 `source-connection:` 前缀的 cipher 变体，要么把 AAD 函数参数化。**推荐**：新增 `db.NewSourceConnectionCredentialCipher` 复用同一 key 但 AAD 前缀为 `"source-connection:" + id`，与 model-provider 物理隔离（同一密文不可跨用途解密）。

- [x] **Step 1: 写 Service + Selector 失败测试。**

```go
func TestCreateSourceConnectionEncryptsSecretAndHidesOnList(t *testing.T) {
	svc := newSourceConnectionServiceWithFakeCipher(t)
	created, err := svc.Create(ctx, CreateSourceConnectionInput{
		WorkspaceID: ws, Provider: "feishu", Name: "主公司飞书", AppID: "cli_a1", AppSecret: "secret",
	})
	if err != nil { t.Fatal(err) }
	if !bytes.Contains(created.CredentialsCiphertext, []byte("encrypted:secret")) { t.Fatal("not encrypted") }
	listed, _ := svc.List(ctx, ws)
	if listed[0].AppSecret != "" || listed[0].AppID != "cli_a1" { t.Fatal("secret leaked or app_id missing") }
}

func TestSelectorDecryptsSecretForRunner(t *testing.T) {
	selected, err := selector.Select(ctx, ws, connID)
	if err != nil { t.Fatal(err) }
	if string(selected.AppSecret) != "secret" { t.Fatal("decrypt failed") }
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/application/service -run 'SourceConnection|SelectorDecrypts' -count=1`

Expected: FAIL，Service/Selector 不存在。

- [x] **Step 3: 实现加密 Service + Selector + cipher 变体。**

Service `Create`：构造 SourceConnection（不含密文）→ 落库拿 connID → `cipher.Encrypt(connID, []byte(secret))` → 回写密文（同一 Workspace 事务）。Selector：`repo.Get` → `cipher.Decrypt(connID, ciphertext)` → 返回。新增 `db.NewSourceConnectionCredentialCipher(key)`，AAD 前缀 `source-connection:`。

- [x] **Step 4: 运行聚焦测试 + go vet。**

Run: `go test ./internal/application/service ./internal/infrastructure/db -run 'SourceConnection|Selector' -count=1 && go vet ./internal/application/service/...`

Expected: PASS；secret 不在 List 回显、不在日志/DOM、解密正确。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/infrastructure/db
git commit -m "feat(source): 飞书应用凭证加密与选择器"
```

### Task 4: SourceConnector port 与飞书适配器

**Files:**

- Create: `internal/ports/source/connector.go`
- Create: `internal/domain/value/sync_root.go`
- Create: `internal/domain/model/external_node.go`
- Create: `internal/adapters/source/feishu/token_client.go`
- Create: `internal/adapters/source/feishu/wiki_client.go`
- Create: `internal/adapters/source/feishu/drive_client.go`
- Create: `internal/adapters/source/feishu/docx_client.go`
- Create: `internal/adapters/source/feishu/url_parser.go`
- Create: `internal/adapters/source/feishu/connector.go`
- Test: 各 `*_test.go`（全部用 `httptest` fake server）

**Interfaces:**

- `SourceConnector.ListTree` 按 `SyncRoot.Kind` 分派 wiki/drive，递归返回 `[]ExternalNode{Token, ParentToken, Title, ObjType, HasDocument, EditTime}`。
- `SourceConnector.Fetch` 调 `/docx/v1/documents/:id/raw_content` 返回 `FetchedDocument{Markdown []byte, Title, EditTime, ObjType}`。
- `token_client` 换 `tenant_access_token` + 内存缓存 + 过期刷新。

- [x] **Step 1: 写 fake server 驱动的适配器失败测试。**

```go
func TestListTreeWalksWikiNodesRecursively(t *testing.T) {
	srv := newFakeFeishuServer(t) // 返回根节点 + 2 子节点（1 docx + 1 folder）
	conn := newFakeConnection(srv.URL)
	nodes, err := connector.ListTree(ctx, conn, value.SyncRoot{Kind: "wiki_node", Token: "root"})
	if err != nil { t.Fatal(err) }
	if len(nodes) != 3 { t.Fatalf("want 3 nodes, got %d", len(nodes)) } // root + 2
}

func TestFetchReturnsDocxMarkdown(t *testing.T) {
	srv := newFakeFeishuServer(t)
	doc, err := connector.Fetch(ctx, newFakeConnection(srv.URL), "doccnX")
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(doc.Markdown), "# 快速开始") { t.Fatal("markdown missing") }
}

func TestUrlParserExtractsWikiToken(t *testing.T) {
	root, err := feishu.ParseURL("https://xxx.feishu.cn/wiki/wikcnB123")
	if err != nil || root.Kind != "wiki_node" || root.Token != "wikcnB123" { t.Fatal(root, err) }
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/adapters/source/feishu/... -count=1`

Expected: FAIL，包不存在。

- [x] **Step 3: 实现 port + value + model + 飞书 client。**

HTTP client 复用 SSRF-safe 配置（参考 `adapters/httpclient`）；token_client 缓存到 `sync.Map` 或 `sync.Mutex` 保护的 struct；ListTree 递归 wiki `/wiki/v2/spaces/:space/nodes` 与 drive `/drive/v1/files?folder_token=`；非 docx 节点记 warning 日志（不含正文）。

- [x] **Step 4: 运行适配器测试 + SSRSF 校验。**

Run: `go test ./internal/adapters/source/feishu/... ./internal/adapters/httpclient -count=1`

Expected: PASS；私网/redirect 拒绝、token 刷新、递归树、markdown 解析、URL 解析全部正确，无真实网络请求。

- [x] **Step 5: 提交。**

```bash
git add internal/ports/source internal/adapters/source
git commit -m "feat(feishu): 实现飞书 SourceConnector 适配器"
```

### Task 5: SourceSyncService 全量同步（不接限流）

**Files:**

- Create: `internal/application/service/source_sync.go`
- Create: `internal/application/service/source_sync_test.go`
- Create: `internal/infrastructure/db/source_sync_store.go`
- Modify: `internal/infrastructure/db/document_ingest_store.go`（新增 `CreateSyncedDocumentNodeRevisionAndJob`）

**Interfaces:**

- `SourceSyncService.SyncKnowledgeBase(ctx, workspaceID, kbID)`：读 KB+凭证 → ListTree → 对每个 docx 节点 Fetch → `rawStore.Put` → 建 Document{file, external_id} + Revision{markdown} + FileTreeNode + Job{parse_start} → 入队 `document_parse_start`。
- `CreateSyncedDocumentNodeRevisionAndJob`：接受 `externalID`，复用现有事务封装，不改老接口。

> 复用确认：`RawDocumentStore.Put(ctx, RawDocumentInput{Reader, ...})` 直接吃 `io.Reader`（`ports/storage/raw_document.go:10-25`）；`model.NewDocumentIdentity` + `NewDocumentRevision(file)` 字段约束可满足（filename=`标题.md`、file_type=`markdown`）。**不**复用 `DocumentIngestService.Ingest`（file kind + FileTree + allowlist 三重耦合 + sha256 去重）。

- [x] **Step 1: 写全量同步失败测试（mock connector + 临时库）。**

```go
func TestSyncKnowledgeBaseFetchesAndEnqueuesParse(t *testing.T) {
	env := newSyncTestEnv(t) // 临时库 + mock connector（返回 1 folder + 2 docx）
	err := env.svc.SyncKnowledgeBase(ctx, ws, kbID)
	if err != nil { t.Fatal(err) }
	docs, _ := env.docRepo.ListByKB(ctx, ws, kbID)
	if len(docs) != 2 { t.Fatalf("want 2 docs, got %d", len(docs)) }
	if docs[0].ExternalID == "" || docs[0].SourceType != "feishu" { t.Fatal("source fields missing") }
	// 断言 2 个 document_parse_start job 已入队（mock queue 记录）
	if len(env.queue.enqueued) != 2 { t.Fatalf("want 2 enqueued, got %d", len(env.queue.enqueued)) }
}

func TestSyncSkipsNonDocxNodesWithWarning(t *testing.T) {
	env := newSyncTestEnv(t) // connector 返回 1 docx + 1 sheet
	_ = env.svc.SyncKnowledgeBase(ctx, ws, kbID)
	docs, _ := env.docRepo.ListByKB(ctx, ws, kbID)
	if len(docs) != 1 { t.Fatalf("sheet should be skipped, got %d", len(docs)) }
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/application/service -run SyncKnowledgeBase -count=1`

Expected: FAIL，service/store 不存在。

- [x] **Step 3: 实现 Service + store。**

Service 持有 `SourceConnector`、`SourceConnectionSelector`、`rawStore`、`sourceSyncStore`、`docRepo`、`fileTreeRepo`、`queue`。folder → FileTreeNode(folder)，docx → Fetch→Put→建 Document+Revision+FileTreeNode(file)+Job，维护 `map[feishuToken]nodeID` 建层级。入队 `document_parse_start`（复用现有 TaskID 规则）。

- [x] **Step 4: 运行聚焦 + 集成测试。**

Run: `go test ./internal/application/service ./internal/infrastructure/db -run 'SyncKnowledgeBase|CreateSyncedDocument' -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'CreateSyncedDocument' -count=1`

Expected: PASS；2 docx 落库 + external_id + 2 job 入队、sheet 跳过、folder 树结构正确。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/infrastructure/db
git commit -m "feat(source): 飞书全量同步入库与文件树构建"
```

### Task 6: source_sync worker handler 与手动触发 API

**Files:**

- Create: `internal/interfaces/worker/source_sync_tasks.go`
- Create: `internal/interfaces/worker/source_sync_tasks_test.go`
- Modify: `internal/ports/queue/queue.go`（`SourceSyncTaskID`）
- Modify: `internal/interfaces/http/knowledge_base.go`（`POST /:slug/knowledge-bases/:id/sync`）
- Modify: `internal/interfaces/http/router.go`
- Modify: `cmd/langhuan/main.go`（worker mux 注册 + SourceSyncService 装配）

**Interfaces:**

- `const TaskSourceSync = "source_sync"`，`SourceSyncPayload{WorkspaceID, KnowledgeBaseID, ConnectionID, JobID}`。
- Handler：MarkRunning → `SourceSyncService.SyncKnowledgeBase` → MarkSucceeded/Failed。
- `SourceSyncTaskID(workspaceID, kbID)` 幂等规则。
- `POST .../knowledge-bases/:id/sync`：admin/owner 入队 source_sync job。

> 装配参考：worker 注册参照 `index_generation_tasks.go`（最简模板）；service 装配参照 `main.go:531-536` document ingest；`runtimeServices` struct（`main.go:90-139`）加 `sourceSync` 字段。本任务**不接限流**（直接入队），限流在 Task 7 加。

- [x] **Step 1: 写 handler + API 失败测试。**

```go
func TestSourceSyncHandlerRunsAndMarksSucceeded(t *testing.T) {
	env := newWorkerTestEnv(t)
	task := asynq.NewTask(TaskSourceSync, payloadBytes)
	err := env.handler.Handle(env.ctx, task)
	if err != nil { t.Fatal(err) }
	if env.syncSvc.syncedKBID != kbID { t.Fatal("not synced") }
	if env.jobRepo.Get(env.ctx, ws, jobID).Status != value.JobStatusCompleted { t.Fatal("not completed") }
}

func TestManualSyncEndpointRejectsMember(t *testing.T) {
	w := httptest.NewRecorder()
	env.router.PATCH("/api/v1/workspaces/"+slug+"/knowledge-bases/"+kbID+"/sync", body, asMember)
	if w.Code != 403 { t.Fatalf("want 403, got %d", w.Code) }
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/interfaces/worker ./internal/interfaces/http -run 'SourceSync|ManualSync' -count=1`

Expected: FAIL，handler/路由不存在。

- [x] **Step 3: 实现 handler + TaskID + 路由 + 装配。**

Handler 结构体持 `Runner interface{ SyncKnowledgeBase(ctx,ws,kb) error }` + `Store`（MarkRunning/Succeeded/Failed 复用 `DocumentTaskDBStore`）+ Logger。路由用 `RequireWorkspaceRole(value.RoleAdmin)`。`main.go` 装配 `feishu.NewConnector()` + `SourceConnectionSelector` + `SourceSyncService`，worker mux 加 `worker.RegisterSourceSyncHandler`。

- [x] **Step 4: 运行 worker + http 测试。**

Run: `go test ./internal/interfaces/worker ./internal/interfaces/http ./cmd/langhuan -run 'SourceSync|ManualSync|App' -count=1`

Expected: PASS；handler 推进 job 状态、member 403、admin 入队成功。

- [x] **Step 5: 提交。**

```bash
git add internal/interfaces/worker internal/interfaces/http internal/ports/queue cmd/langhuan
git commit -m "feat(source): source_sync worker 与手动触发 API"
```

### Task 7: Meta Scheduler 按应用限流 + KB 创建入队

**Files:**

- Create: `internal/application/service/source_sync_scheduler.go`
- Create: `internal/application/service/source_sync_scheduler_test.go`
- Modify: `internal/infrastructure/db/source_sync_store.go`（`CountActiveByConnection`）
- Modify: `internal/application/service/knowledge_base.go`（Create 后入队）
- Modify: `internal/interfaces/http/knowledge_base.go`（KB 创建来源字段）
- Modify: `internal/interfaces/http/knowledge_base_handler_test.go`
- Modify: `internal/infrastructure/config/config.go` + `config.example.yaml`（`source_sync`）
- Modify: `cmd/langhuan/main.go`（scheduler goroutine 启停）

**Interfaces:**

- `SourceSyncScheduler.Tick(ctx)`：查到期 KB → 按 connection 分组 → `CountActiveByConnection` → 按额度入队 → 写回 next_sync_at。
- `TryDispatchConnection(ctx, ws, connID)`：worker 完成后续跑。
- `config.SourceSyncConfig{SchedulerIntervalSeconds, MaxConcurrentPerConnection}`。
- KB 创建：`source_type=feishu_*` 时事务提交后入队首次 source_sync。

> 限流前置：`source_sync` job 写入时填 `source_connection_id`（Task 2 已加列）。`CountActiveByConnection` 查 `status IN ('pending','running')`，命中 Task 1 加的部分索引。

- [x] **Step 1: 写限流与 KB 创建入队失败测试。**

```go
func TestSchedulerRespectsPerConnectionConcurrency(t *testing.T) {
	env := newSchedulerTestEnv(t, MaxConcurrentPerConnection: 1)
	// 两个 KB 绑同一 connection，都已到期；先入队一个 running job 占额度
	env.jobRepo.seedRunningSourceSyncJob(ws, connID)
	err := env.scheduler.Tick(ctx)
	if err != nil { t.Fatal(err) }
	if len(env.queue.enqueued) != 1 { t.Fatalf("want 1 (capped), got %d", len(env.queue.enqueued)) }
}

func TestTryDispatchConnectionFillsFreedSlot(t *testing.T) {
	env := newSchedulerTestEnv(t, MaxConcurrentPerConnection: 2)
	// 额度满 → worker 完成 → TryDispatch 应再入队 1 个
	_ = env.scheduler.TryDispatchConnection(ctx, ws, connID)
	if len(env.queue.enqueued) != 1 { t.Fatal("did not fill slot") }
}

func TestCreateFeishuKbEnqueuesFirstSyncAfterCommit(t *testing.T) {
	env := newKbCreateTestEnv(t)
	_, err := env.svc.Create(ctx, CreateKnowledgeBaseInput{
		WorkspaceID: ws, Name: "飞书KB", EmbeddingModelID: modelID,
		SourceType: value.SourceTypeFeishuWiki, SourceConnectionID: &connID,
		SourceConfig: map[string]any{"root_token": "wikcnB"},
	})
	if err != nil { t.Fatal(err) }
	if len(env.queue.enqueued) != 1 || env.queue.enqueued[0].Type != TaskSourceSync { t.Fatal("first sync not enqueued") }
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/application/service ./internal/interfaces/http -run 'Scheduler|TryDispatch|CreateFeishuKb' -count=1`

Expected: FAIL，scheduler/CountActive/KB 创建来源字段不存在。

- [x] **Step 3: 实现 scheduler + CountActive + KB 创建扩展。**

Scheduler 用 `time.Ticker` 单 goroutine（context 受控，随 worker server 启停）；`cron.Next(now)` 算 next_sync_at（用 `robfig/cron/v3`）。KB 创建：`WithinWorkspace` 提交后（参照 `index_generation.go:188` 事务后入队先例）入队；`KnowledgeBaseService` 加 `sourceSyncEnqueuer` 依赖。`createKnowledgeBaseRequest` 加 source_type/source_config/source_connection_id。

- [x] **Step 4: 运行聚焦测试 + go vet。**

Run: `go test ./internal/application/service ./internal/interfaces/http -run 'Scheduler|TryDispatch|CreateFeishu|SourceSync' -count=1 && go vet ./internal/application/service/...`

Expected: PASS；限流 cap 生效、续跑填空、KB 创建后入队、cron 推进。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/interfaces/http internal/infrastructure/db internal/infrastructure/config cmd/langhuan config.example.yaml
git commit -m "feat(source): 按应用限流的定时同步与 KB 创建入队"
```

### Task 8: 增量同步与删除检测

**Files:**

- Modify: `internal/application/service/source_sync.go`
- Modify: `internal/application/service/source_sync_test.go`
- Modify: `internal/infrastructure/db/document_repository.go`（`ListExternalIDsByKB`、软删）

**Interfaces:**

- 同步前读 `source_config.sync_cursor`，跳过 `EditTime <= cursor` 节点；同步后回写 `max(EditTime)`。
- 飞书树不再出现的 external_id → Document 软删（deleted_at）。

> 本任务建立在 Task 5 全量逻辑之上，复用 ListTree 结果做差集。

- [x] **Step 1: 写增量与删除检测失败测试。**

```go
func TestIncrementalSyncSkipsUnchangedAndDeletesMissing(t *testing.T) {
	env := newSyncTestEnv(t)
	// 首次全量：docA(editTime=T1) + docB(T1)
	_ = env.svc.SyncKnowledgeBase(ctx, ws, kbID)
	// 第二次：docA(T2 变更) + docB(T1 未变) + docC(T2 新增)，docD 已从飞书删除
	env.connector.setNodes(docA_T2, docB_T1, docC_T2) // docD 不在树里
	_ = env.svc.SyncKnowledgeBase(ctx, ws, kbID)
	docs, _ := env.docRepo.ListByKB(ctx, ws, kbID) // 含软删
	active := filterActive(docs)
	if len(active) != 3 || !containsExternal(active, "docA") && !containsExternal(active, "docC") { t.Fatal("sync result wrong") }
	if !isSoftDeleted(docs, "docD") { t.Fatal("docD not deleted") }
	if env.connector.fetchCount["docB"] != 0 { t.Fatal("docB should be skipped") } // 增量跳过
}
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/application/service -run 'IncrementalSync|DeletesMissing' -count=1`

Expected: FAIL，增量/软删未实现。

- [x] **Step 3: 实现增量 + 软删。**

读 cursor → 跳过未变更 → 回写 cursor；ListTree 结果 set 与 DB external_id set 做差集，缺失的软删。

- [x] **Step 4: 运行测试。**

Run: `go test ./internal/application/service -run 'Sync|Incremental' -count=1`

Expected: PASS；未变更跳过、删除软删、cursor 推进。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/infrastructure/db
git commit -m "feat(source): 飞书增量同步与删除检测"
```

### Task 9: 飞书应用管理 API

**Files:**

- Create: `internal/interfaces/http/source_connection_handler.go`
- Create: `internal/interfaces/http/source_connection_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/application/dto/source_connection.go`

**Interfaces:**

- `POST/GET/PATCH/DELETE /api/v1/workspaces/:slug/source-connections`，admin/owner 写、member 读、API Key 不可访问。
- GET 不回显 secret；PATCH 可更新凭证或启停。

- [x] **Step 1: 写 handler 失败测试。**

```go
func TestCreateSourceConnectionRejectsMember(t *testing.T) { /* 403 */ }
func TestListSourceConnectionsHidesSecret(t *testing.T) { /* secret 字段为空 */ }
func TestPatchSourceConnectionRotatesSecret(t *testing.T) { /* 更新后旧密文替换 */ }
func TestApiKeyCannotAccessSourceConnections(t *testing.T) { /* 401/403 */ }
```

- [x] **Step 2: 运行并确认 RED。**

Run: `go test ./internal/interfaces/http -run SourceConnection -count=1`

Expected: FAIL。

- [x] **Step 3: 实现 handler + 路由 + DTO。**

严格 JSON 解码；DTO 不含 secret 字段；PATCH 区分 config 更新与凭证轮换。

- [x] **Step 4: 运行测试。**

Run: `go test ./internal/interfaces/http ./cmd/langhuan -run SourceConnection -count=1`

Expected: PASS；权限、secret 隐藏、轮换正确。

- [x] **Step 5: 提交。**

```bash
git add internal/interfaces/http internal/application/dto
git commit -m "feat(http): 飞书应用管理 API"
```

### Task 10: Web — 集成页（飞书应用列表与表单）

**Files:**

- Create: `web/src/features/integrations/api.ts`
- Create: `web/src/features/integrations/queries.ts`
- Create: `web/src/features/integrations/schemas.ts`
- Create: `web/src/features/integrations/components/source-connection-list.tsx`
- Create: `web/src/features/integrations/components/source-connection-form.tsx`
- Create: `web/src/features/integrations/components/*.test.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/integrations/index.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/integrations/new.tsx`
- Modify: `web/src/components/layout/workspace-navigation.ts`（加「集成」入口，owner/admin 可见）
- Modify: `web/src/lib/i18n/locales/{zh,en}/integrations.ts`
- Modify: generated `web/src/routeTree.gen.ts`（仅通过路由生成命令）

**Interfaces:**

- 列表卡片网格（`resource-card`），显示 name/app_id/status/绑定 KB 数；空态 `border-dashed` Card。
- 表单窄页单 Card（max-w-3xl，RHF+Zod），「测试并保存」先调验证再 mutation；secret 不回显。

- [x] **Step 1: 写 API/schema/组件失败测试。**

```tsx
it('hides secret on list and shows app_id', async () => {
  const screen = await render(<SourceConnectionList ... />)
  await expect.element(screen.getByText('cli_a1b2…')).toBeVisible()
  await expect.element(screen.queryByLabelText('App Secret')).not.toBeInTheDocument()
})
it('requires name, app_id, app_secret', () => {
  expect(() => sourceConnectionSchema.parse({ provider: 'feishu', name: '', app_id: '', app_secret: '' })).toThrow()
})
```

- [x] **Step 2: 运行并确认 RED。**

Run: `pnpm --dir web test -- integrations`

Expected: FAIL。

- [x] **Step 3: 实现 feature + 路由 + 导航。**

走 `apiClient`（HttpOnly cookie）；queryKey `['source-connections', ws]`；mutation 后 invalidate；表单错误用 FormMessage；加载 Skeleton、错误 Alert、空态 Card。

- [x] **Step 4: 验证组件、路由、build。**

Run: `pnpm --dir web test -- integrations && pnpm --dir web check && pnpm --dir web build`

Expected: PASS；secret 不在 DOM、权限入口对 member 隐藏、空/加载/错误态齐全。

- [x] **Step 5: 提交。**

```bash
git add web/src/features/integrations web/src/routes web/src/components/layout web/src/lib/i18n
git commit -m "feat(web): 飞书集成页与应用管理"
```

### Task 11: Web — KB 创建来源切换与详情同步状态

**Files:**

- Modify: `web/src/features/knowledge-bases/components/knowledge-base-form.tsx`
- Modify: `web/src/features/knowledge-bases/schemas.ts`
- Modify: `web/src/features/knowledge-bases/api.ts`
- Modify: `web/src/features/knowledge-bases/components/*.test.tsx`
- Modify: `web/src/features/knowledge-bases/workbench/workbench-layout.tsx`（同步状态 Badge + 手动同步按钮）
- Modify: `web/src/features/knowledge-bases/workbench/overview.tsx`（同步信息卡）
- Modify: `web/src/lib/i18n/locales/{zh,en}/knowledge-bases.ts`

**Interfaces:**

- 表单顶部「内容来源」RadioGroup（本地上传/飞书云文档/飞书知识库）；选飞书时出现应用 Select + token/URL Input + cron 可选 Switch。
- 详情 Badge 同步状态（文字+图标，不只靠颜色）；概览展示来源/节点数/最近下次同步；「手动同步」按钮（admin）。

- [ ] **Step 1: 写来源切换与详情状态失败测试。**

```tsx
it('shows feishu fields only when source is feishu', async () => {
  const screen = await render(<KnowledgeBaseForm />)
  await expect.element(screen.queryByLabelText('飞书应用')).not.toBeInTheDocument()
  await user.click(screen.getByRole('radio', { name: /飞书知识库/ }))
  await expect.element(screen.getByLabelText('飞书应用')).toBeVisible()
  await expect.element(screen.getByLabelText('知识库 Token / 链接')).toBeVisible()
})
it('renders sync status badge with text and icon', async () => {
  const screen = await render(<WorkbenchLayout kb={syncingKb} />)
  await expect.element(screen.getByText('同步中')).toBeVisible()
})
```

- [ ] **Step 2: 运行并确认 RED。**

Run: `pnpm --dir web test -- knowledge-base-form workbench`

Expected: FAIL。

- [ ] **Step 3: 实现来源切换、详情状态、手动同步 mutation。**

来源字段条件渲染；无应用时下拉显示跳转链接；同步状态用 Badge + Lucide 图标（✓⟳⚠–）；手动同步 mutation 后 invalidate KB summary。

- [ ] **Step 4: 验证表单、详情、build。**

Run: `pnpm --dir web test -- knowledge-base-form workbench && pnpm --dir web check && pnpm --dir web build`

Expected: PASS；来源切换、状态可读、桌面/移动等价、无 UUID/secret 在 DOM。

- [ ] **Step 5: 提交。**

```bash
git add web/src/features/knowledge-bases web/src/lib/i18n
git commit -m "feat(web): 知识库创建来源切换与同步状态"
```

### Task 12: 飞书适配器 HTTP 协议 e2e（httptest fake server）

**Files:**

- Create: `internal/adapters/source/feishu/e2e_protocol_test.go`（`//go:build integration`）
- Modify: `internal/adapters/source/feishu/testserver/`（fake 飞书 OpenAPI server，可复用于 cmd 级 e2e）

**Interfaces:**

- 用 `httptest.NewServer` 起 fake 飞书 OpenAPI，验证 connector 的 HTTP 协议行为：tenant_access_token 换取/刷新、wiki/drive 递归分页、docx raw_content、4xx/5xx/限流响应、Bearer/tenant_access_token 头正确。
- fake server 实现 `feishu/testserver` 子包，返回固定 fixture（2 folder + 3 docx + 1 sheet），后续 cmd e2e 通过把 `conn.Config.base_url` 指向同一 server 复用。

> 分层依据（团队既有模式）：`siliconflow_e2e_test.go:24` 与 `mineru/client_test.go:14` 均用 httptest 验证 adapter 级 HTTP 协议；全链路 e2e（Task 14）则用 fake factory 注入 `buildRuntimeServices`，不起 HTTP。本任务只覆盖"协议正确性"。

- [ ] **Step 1: 写协议级失败测试。**

```go
//go:build integration
func TestConnectorRespectsTenantAccessTokenCacheAndRefresh(t *testing.T) {
	srv := testserver.New(t) // fake 飞书
	srv.SetTokenExpiry(1 * time.Second)
	conn := testserver.NewConnection(srv.URL, "app-id", "secret")
	c := feishu.NewConnector()
	_, _ = c.ListTree(ctx, conn, value.SyncRoot{Kind: "wiki_node", Token: "root"}) // 首次换 token
	_, _ = c.ListTree(ctx, conn, value.SyncRoot{Kind: "wiki_node", Token: "root"}) // 命中缓存
	if srv.TokenExchangeCount() != 1 { t.Fatalf("want 1 token exchange, got %d", srv.TokenExchangeCount()) }
	time.Sleep(2 * time.Second)
	_, _ = c.ListTree(ctx, conn, value.SyncRoot{Kind: "wiki_node", Token: "root"}) // 过期刷新
	if srv.TokenExchangeCount() != 2 { t.Fatalf("want refresh, got %d", srv.TokenExchangeCount()) }
}

func TestConnectorRetriesOnRateLimit429(t *testing.T) { /* 429 → 退避重试 → 最终成功 */ }
func TestConnectorPropagatesAuthFailure(t *testing.T) { /* 401 app_secret 错 → 返回领域错误 */ }
func TestWikiPaginationWalksAllPages(t *testing.T) { /* page_token 递归直到 has_more=false */ }
func TestDriveFolderRecursion(t *testing.T) { /* drive folder 嵌套子 folder */ }
func TestNonDocxNodesSkippedWithWarning(t *testing.T) { /* sheet 节点返回但 HasDocument=false */ }
```

- [ ] **Step 2: 运行并确认 RED。**

Run: `go test -tags=integration ./internal/adapters/source/feishu/... -run 'Connector|Wiki|Drive|NonDocx' -count=1`

Expected: FAIL，testserver 子包不存在。

- [ ] **Step 3: 实现 fake server + 补协议测试。**

`testserver` 子包：可配置的 httptest handler，按 path 分派 `/auth/v3/tenant_access_token/internal`、`/wiki/v2/spaces/*/nodes`、`/drive/v1/files`、`/open-apis/docx/v1/documents/*/raw_content`；支持注入 fixture、token 过期、429/401/500 注入、计数器。无真实网络请求。

- [ ] **Step 4: 运行适配器 e2e + SSRF 校验。**

Run: `go test -tags=integration ./internal/adapters/source/feishu/... ./internal/adapters/httpclient -count=1`

Expected: PASS；token 缓存/刷新、重试、分页递归、错误传播、非 docx 跳过全部正确，无外网请求。

- [ ] **Step 5: 提交。**

```bash
git add internal/adapters/source/feishu
git commit -m "test(feishu): 飞书 OpenAPI 协议级 e2e 与 fake server"
```

### Task 13: source_sync worker 级 e2e（fakeAsyncConnector）

**Files:**

- Create: `internal/interfaces/worker/source_sync_tasks_e2e_test.go`（`//go:build integration`）
- Create: `internal/interfaces/worker/fake_source_connector.go`（测试用）

**Interfaces:**

- 仿 `pdf_pipeline_test.go:22` 的 `fakeAsyncParser` 模式：写 `fakeSourceConnector` 实现 `source.SourceConnector`，返回固定 `[]ExternalNode` + `FetchedDocument`，不起 HTTP。
- 用 `integrationJobQueue`（`document_tasks_integration_test.go:238` 的 in-memory queue）直接驱动 `source_sync` handler，验证：MarkRunning→Sync→MarkSucceeded、失败→MarkFailed、幂等（重复执行不产生副作用）、TryDispatchConnection 续跑。
- **不**起真 redis/asynq（那是 Task 14 的全链路 e2e 职责）。

> 覆盖 spec 验收 3（树→FileTree→向量化链路在 worker 层闭合）、验收 4（失败重试）、幂等铁律。

- [ ] **Step 1: 写 worker 级失败测试。**

```go
//go:build integration
func TestSourceSyncHandlerBuildsFileTreeAndMarksSucceeded(t *testing.T) {
	env := newWorkerE2E(t) // 临时库 + fakeSourceConnector（2 folder + 3 docx）
	err := env.handler.Handle(env.ctx, env.syncTask(kbID))
	if err != nil { t.Fatal(err) }
	nodes := env.fileTreeRepo.List(ctx, ws, kbID)
	if len(nodes) != 5 { t.Fatalf("want root+2folder+2file=5 nodes, got %d", len(nodes)) } // 假设结构
	docs := env.docRepo.ListByKB(ctx, ws, kbID)
	if len(docs) != 3 || docs[0].ExternalID == "" { t.Fatal("source fields missing") }
	job := env.jobRepo.Get(ctx, ws, env.jobID)
	if job.Status != value.JobStatusCompleted { t.Fatalf("want completed, got %s", job.Status) }
}

func TestSourceSyncHandlerIdempotentOnRerun(t *testing.T) {
	env := newWorkerE2E(t)
	_ = env.handler.Handle(env.ctx, env.syncTask(kbID))
	_ = env.handler.Handle(env.ctx, env.syncTask(kbID)) // 重复
	docs := env.docRepo.ListByKB(ctx, ws, kbID)
	if len(docs) != 3 { t.Fatalf("rerun produced dup docs: %d", len(docs)) }
}

func TestSourceSyncHandlerMarksFailedOnFetchError(t *testing.T) {
	env := newWorkerE2E(t)
	env.connector.SetFetchError("doccnX", errors.New("feishu 500"))
	err := env.handler.Handle(env.ctx, env.syncTask(kbID))
	if err == nil { t.Fatal("want error") }
	if env.jobRepo.Get(ctx, ws, env.jobID).Status != value.JobStatusFailed { t.Fatal("not failed") }
}

func TestTryDispatchFillsFreedSlot(t *testing.T) { /* worker 完成后续跑同 connection 队列 */ }
```

- [ ] **Step 2: 运行并确认 RED。**

Run: `go test -tags=integration ./internal/interfaces/worker -run 'SourceSyncHandler|TryDispatch' -count=1`

Expected: FAIL，fakeSourceConnector / handler e2e 不存在。

- [ ] **Step 3: 实现 fake connector + worker e2e。**

`fakeSourceConnector`：可配置返回节点、Fetch 错误注入、调用计数。用 `integrationJobQueue`（已存在模式）驱动 handler；断言 FileTree 结构、Document source 字段、job 状态、幂等性。

- [ ] **Step 4: 运行 worker e2e。**

Run: `make test-image && go test -tags=integration -p 1 ./internal/interfaces/worker -run 'SourceSync' -count=1`

Expected: PASS；树构建、source 字段、状态推进、幂等、失败、续跑全部正确，无 HTTP/外网。

- [ ] **Step 5: 提交。**

```bash
git add internal/interfaces/worker
git commit -m "test(worker): source_sync worker 级 e2e 与 fake connector"
```

### Task 14: 全链路 e2e（真 redis + asynq + fake 飞书，覆盖 spec 全部验收）

**Files:**

- Create: `cmd/langhuan/feishu_source_sync_e2e_test.go`（`//go:build integration`，`package main`）
- Modify: `cmd/langhuan/v030_e2e_test.go` 或新建 `cmd/langhuan/e2e_helpers.go`（抽取共享 `startV030E2E` 或新增 `startFeishuSyncE2E`）
- Create: `cmd/langhuan/web_embed_feishu_routes_e2e_test.go`（`//go:build integration`）

**Interfaces:**

- 复用 `startV030E2E(t)` 模式（`v030_e2e_test.go:187`）：`testsupport.NewMigratedPostgres` + `testsupport.NewIsolatedRedis` + 真 asynq Server + `httptest.NewServer(buildHTTPRouter)`。
- 飞书依赖注入：仿 `v030FakeFactory`（`v030_e2e_test.go:42`）写 `fakeFeishuSourceFactory`，通过新的装配钩子注入 `buildRuntimeServices`；fake 飞书 OpenAPI 用 Task 12 的 `feishu/testserver`（不起独立 HTTP，由 fake connector 直接返回 fixture）。
- 异步等待：`waitReady` 风格（50ms 轮询 / 15s deadline，见 `v030_e2e_test.go:359`）。
- **必须覆盖 spec 验收标准 1-8 的每一条**（见下方矩阵）。

> 覆盖：验收 1（多应用+加密）、2（来源选择）、3（树→检索）、4（限流串行）、5（增量+删除）、6（详情状态+手动同步）、7（状态文字+图标）、权限子项。

- [ ] **Step 1: 写全链路 e2e 失败测试（按 spec 验收逐条命名）。**

```go
//go:build integration

// 验收 1：多应用、凭证加密、不明文回显
func TestFeishuMultiAppCredentialsEncryptedAndHiddenE2E(t *testing.T) {
	env := startFeishuSyncE2E(t)
	admin := env.loginAdmin()
	connA := env.createSourceConnection(admin, ws, "主公司飞书", "cli_a", "secret-a")
	connB := env.createSourceConnection(admin, ws, "子公司飞书", "cli_b", "secret-b")
	listed := env.listSourceConnections(admin, ws)
	if len(listed) != 2 || listed[0].AppSecret != "" { t.Fatal("secret leaked or count wrong") }
	// 直接查库验证加密（密文 != 明文，且可解密回原值）
	row := env.fetchConnectionRow(connA.ID)
	if bytes.Equal(row.CredentialsCiphertext, []byte("secret-a")) { t.Fatal("not encrypted") }
	decrypted := env.decryptSecret(connA.ID, row.CredentialsCiphertext)
	if string(decrypted) != "secret-a" { t.Fatal("decrypt mismatch") }
}

// 验收 2 + 3：选飞书知识库 → 整树同步 → 可检索
func TestFeishuWikiFullSyncEndToEndSearchableE2E(t *testing.T) {
	env := startFeishuSyncE2E(t)
	admin := env.loginAdmin()
	conn := env.createSourceConnection(admin, ws, "主公司飞书", "cli_a", "secret")
	env.fakeFeishu.SetTree(fixtureWikiTree(2, 3)) // 2 folder + 3 docx
	kb := env.createFeishuKB(admin, ws, conn.ID, "wikcnRoot", value.SourceTypeFeishuWiki)
	env.waitSyncCompleted(admin, ws, kb.ID, 15*time.Second)
	result := env.search(admin, ws, []string{kb.ID}, "快速开始")
	if len(result) == 0 { t.Fatal("feishu doc not searchable after full sync") }
	doc := env.firstDocument(admin, ws, kb.ID)
	if doc.SourceType != "feishu" || doc.ExternalID == "" { t.Fatal("source fields missing") }
}

// 验收 4：同应用多 KB 串行排队，不超 max_concurrent_per_connection
func TestFeishuSameAppSerialQueueConcurrencyCapE2E(t *testing.T) {
	env := startFeishuSyncE2E(t, withMaxConcurrentPerConnection(1))
	admin := env.loginAdmin()
	conn := env.createSourceConnection(admin, ws, "飞书", "cli", "secret")
	env.fakeFeishu.SetDelay(2 * time.Second) // 拖慢 Fetch 让任务重叠
	kb1 := env.createFeishuKB(admin, ws, conn.ID, "root1", value.SourceTypeFeishuWiki)
	kb2 := env.createFeishuKB(admin, ws, conn.ID, "root2", value.SourceTypeFeishuWiki)
	// 同时创建两个 → 只有 1 个在 running，另一个 pending
	running := env.countActiveSourceSyncJobs(ws, conn.ID, "running")
	if running != 1 { t.Fatalf("want 1 running, got %d", running) }
	pending := env.countActiveSourceSyncJobs(ws, conn.ID, "pending")
	if pending < 1 { t.Fatalf("want >=1 pending, got %d", pending) }
	env.waitSyncCompleted(admin, ws, kb2.ID, 30*time.Second)
}

// 验收 5：cron 增量同步，跳过未变更，软删已删除
func TestFeishuIncrementalSyncSkipsAndSoftDeletesE2E(t *testing.T) {
	env := startFeishuSyncE2E(t)
	admin := env.loginAdmin()
	conn := env.createSourceConnection(admin, ws, "飞书", "cli", "secret")
	env.fakeFeishu.SetTree(fixtureTree(docA_T1, docB_T1, docD_T1))
	kb := env.createFeishuKB(admin, ws, conn.ID, "root", value.SourceTypeFeishuWiki)
	env.waitSyncCompleted(admin, ws, kb.ID, 15*time.Second)
	// 第二次：docA 变更、docB 不变、docD 删除、docC 新增
	env.fakeFeishu.SetTree(fixtureTree(docA_T2, docB_T1, docC_T2))
	env.triggerManualSync(admin, ws, kb.ID)
	env.waitSyncCompleted(admin, ws, kb.ID, 15*time.Second)
	docs := env.listDocuments(admin, ws, kb.ID)
	if !hasActiveExternal(docs, "docA", "docB", "docC") { t.Fatal("active set wrong") }
	if !isSoftDeleted(docs, "docD") { t.Fatal("docD not soft-deleted") }
	if env.fakeFeishu.FetchCount("docB") != 1 { t.Fatal("docB should be skipped on 2nd sync") } // 仅首次 fetch
}

// 验收 6：详情同步状态 + 手动同步
func TestFeishuKBDetailShowsSyncStatusAndManualSyncE2E(t *testing.T) {
	env := startFeishuSyncE2E(t)
	admin := env.loginAdmin()
	conn := env.createSourceConnection(admin, ws, "飞书", "cli", "secret")
	kb := env.createFeishuKB(admin, ws, conn.ID, "root", value.SourceTypeFeishuWiki)
	env.waitSyncCompleted(admin, ws, kb.ID, 15*time.Second)
	summary := env.getKBSummary(admin, ws, kb.ID)
	if summary.SourceType != "feishu_wiki" || summary.SyncStatus == "" { t.Fatal("sync status missing") }
	env.triggerManualSync(admin, ws, kb.ID) // 手动触发再次同步
	env.waitSyncCompleted(admin, ws, kb.ID, 15*time.Second)
}

// 验收 1/9：权限边界
func TestFeishuSourceConnectionPermissionsE2E(t *testing.T) {
	env := startFeishuSyncE2E(t)
	admin := env.loginAdmin()
	member := env.loginMember(ws)
	if env.createSourceConnectionShouldFail(member, ws, ...) != http.StatusForbidden { t.Fatal("member can create") }
	if env.listSourceConnections(member, ws) == nil { /* member 可读，不应 403 */ }
	apiKey := env.createAPIKey(admin, ws, /* bound */)
	if env.bearerCreateSourceConnection(apiKey, ws, ...) != http.StatusUnauthorized { t.Fatal("apikey can manage") }
}

// 验收 7（前端路由 + 同步状态可渲染）：复用 v050 SPA embed 模式
func TestFeishuWebEmbedRoutesAndSyncBadgeE2E(t *testing.T) {
	env := startFeishuSyncE2E(t)
	admin := env.loginAdmin()
	conn := env.createSourceConnection(admin, ws, "飞书", "cli", "secret")
	kb := env.createFeishuKB(admin, ws, conn.ID, "root", value.SourceTypeFeishuWiki)
	// SPA embed：GET 集成页 / KB 详情路由返回 <div id="root">
	if env.getSPA(admin, ws, "/workspaces/"+ws+"/integrations").Body == "" { t.Fatal("integrations route missing") }
	if env.getSPA(admin, ws, "/workspaces/"+ws+"/kb/"+kb.ID).Body == "" { t.Fatal("kb detail route missing") }
}
```

- [ ] **Step 2: 运行全链路 e2e 并确认失败。**

Run: `make test-image && go test -tags=integration -p 1 ./cmd/langhuan -run 'Feishu' -count=1`

Expected: FAIL，`startFeishuSyncE2E` helper / fake factory 注入钩子 / 装配开关缺失。

- [ ] **Step 3: 实现 e2e helper、fake factory 注入与 fake 飞书 fixture。**

新增 `startFeishuSyncE2E(t, opts...)`：基于 `startV030E2E` 结构，注入 `fakeFeishuSourceFactory` 到 `buildRuntimeServices`（需要装配层暴露一个 `sourceConnector` 注入点，类似现有 `embeddingRegistry` 参数）。fake 飞书 fixture 集中在 `cmd/langhuan/feishu_fixtures_test.go`。`waitSyncCompleted` 复用 `waitReady` 的 50ms/15s 轮询。helper 提供 `countActiveSourceSyncJobs`（直查 jobs 表 source_connection_id + status）、`fetchConnectionRow`（直查密文）、`decryptSecret`（调 cipher）。

- [ ] **Step 4: 运行全链路 e2e。**

Run: `make test-image && go test -tags=integration -p 1 ./cmd/langhuan -run 'Feishu' -count=1`

Expected: 全部 PASS；8 条验收逐条命中，数据库只用临时容器，飞书只用 fake fixture（无外网），日志扫描无 secret/document 全文。

- [ ] **Step 5: 提交。**

```bash
git add cmd/langhuan
git commit -m "test(e2e): 飞书同步全链路 e2e 覆盖 spec 验收"
```

### Task 15: E2E 覆盖核对、安全扫描与文档闭环

**Files:**

- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/API_ACCESS.md`
- Modify: `docs/DATABASE_GUIDELINES.md`
- Modify: `ROADMAP.md`
- Modify: spec（补实现后修正 / 标注已验证）
- Modify: `config.example.yaml`（`source_sync` 注释）

**Interfaces:**

- 把 Task 12-14 的 e2e 与 spec 验收标准做显式核对矩阵（每条验收标注由哪个 e2e 函数覆盖）。
- 安全扫描：全 e2e 输出无 secret/app_secret 明文、无 document 全文、无 raw payload。
- 文档同步：ARCHITECTURE 同步数据流、API_ACCESS 补 source-connections/sync/来源字段、DATABASE_GUIDELINES 补多应用表与 job source_connection_id、ROADMAP 标已交付证据。

- [ ] **Step 1: 建立验收→e2e 核对矩阵并查漏。**

| Spec 验收 | E2E 覆盖（文件:函数） |
|---|---|
| 1. 多应用+加密+不回显 | `feishu_source_sync_e2e:TestFeishuMultiAppCredentialsEncryptedAndHiddenE2E` |
| 2. 来源选择+绑定 connection | `feishu_source_sync_e2e:TestFeishuWikiFullSyncEndToEndSearchableE2E`（createFeishuKB 带 connection） |
| 3. 树→FileTree→向量化→检索 | `feishu_source_sync_e2e:TestFeishuWikiFullSyncEndToEndSearchableE2E` + `source_sync_tasks_e2e:TestSourceSyncHandlerBuildsFileTreeAndMarksSucceeded` |
| 4. 按应用串行排队 | `feishu_source_sync_e2e:TestFeishuSameAppSerialQueueConcurrencyCapE2E` |
| 5. cron 增量+跳过+软删 | `feishu_source_sync_e2e:TestFeishuIncrementalSyncSkipsAndSoftDeletesE2E` |
| 6. 详情状态+手动同步 | `feishu_source_sync_e2e:TestFeishuKBDetailShowsSyncStatusAndManualSyncE2E` |
| 7. 状态文字+图标（路由可渲染） | `web_embed_feishu_routes_e2e:TestFeishuWebEmbedRoutesAndSyncBadgeE2E` |
| 权限（admin/member/apikey） | `feishu_source_sync_e2e:TestFeishuSourceConnectionPermissionsE2E` |
| 协议（token/分页/重试/错误） | `feishu/e2e_protocol_test.go`（Task 12） |
| 幂等/失败/续跑 | `source_sync_tasks_e2e_test.go`（Task 13） |

- [ ] **Step 2: 运行安全扫描（e2e 输出无敏感泄漏）。**

Run:

```bash
make test-image
go test -tags=integration -p 1 ./cmd/langhuan ./internal/interfaces/worker ./internal/adapters/source/feishu/... -run 'Feishu|SourceSync' -count=1 -v 2>&1 | \
  rg -i 'secret-a|secret-b|app_secret|cli_.*secret|tenant_access_token:.{40}' && echo "LEAK DETECTED" || echo "clean"
```

Expected: 输出 `clean`；密文落库、日志不含明文 secret/token/document 全文。

- [ ] **Step 3: 更新文档。**

ARCHITECTURE 补飞书同步数据流（KB→connection→ListTree→Fetch→rawStore→parse→chunk→index + Meta Scheduler 限流）；API_ACCESS 补 `/source-connections`、`/knowledge-bases/:id/sync`、KB 创建来源字段与权限；DATABASE_GUIDELINES 补 `workspace_source_connections`、`jobs.source_connection_id` 部分索引、`jobs_target_check` 第三分支；ROADMAP 标已交付证据；spec 标注已由哪条 e2e 验证。

- [ ] **Step 4: 运行完整验证。**

Run:

```bash
gofmt -w .
go test ./...
go vet ./...
make test-integration
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
git diff --check
```

Expected: 全部 exit 0；数据库只用临时容器（DSN 来自 `LANGHUAN_TEST_DATABASE_DSN`，回退禁连 `config.yaml` 库）；飞书只用 fake；e2e 覆盖矩阵无空洞。

- [ ] **Step 5: 提交最终闭环。**

```bash
git add docs ROADMAP.md config.example.yaml
git commit -m "docs(source): 飞书同步 e2e 验收闭环与文档"
```

---

## Spec → Plan 覆盖矩阵

| Spec 要求 | 实现 Tasks | E2E 覆盖 |
|---|---|---|
| 多飞书应用、凭证加密、不明文回显 | 2、3、9 | Task 14 `TestFeishuMultiAppCredentialsEncryptedAndHiddenE2E`（多应用 + 直查密文 + 解密回原值 + List 不回显） |
| KB 来源选择（upload/feishu_drive/feishu_wiki）+ 绑定 connection | 2、7、11 | Task 14 `TestFeishuWikiFullSyncEndToEndSearchableE2E`（createFeishuKB 带 connection） |
| 飞书目录树 → FileTree + docx markdown + 下游管线 | 4、5 | Task 13 `TestSourceSyncHandlerBuildsFileTreeAndMarksSucceeded` + Task 14 全链路检索命中 |
| 按应用串行排队、`max_concurrent_per_connection` | 7 | Task 14 `TestFeishuSameAppSerialQueueConcurrencyCapE2E`（双 KB 同 app + 拖慢 Fetch + 断言 running=1/pending≥1） |
| cron 定时增量、跳过未变更、删除软删 | 7、8 | Task 14 `TestFeishuIncrementalSyncSkipsAndSoftDeletesE2E`（docB 不重 fetch + docD 软删） |
| 创建后立即首次同步、手动触发 | 6、7 | Task 14 `TestFeishuKBDetailShowsSyncStatusAndManualSyncE2E`（summary + triggerManualSync） |
| 详情同步状态、节点数、手动同步 | 11 | Task 14 同上 + Task 13 续跑 |
| 状态文字+图标不靠颜色 | 11 | Task 14 `TestFeishuWebEmbedRoutesAndSyncBadgeE2E`（SPA embed 路由可渲染） |
| 权限（admin 写、member 读、API Key 拒绝） | 6、9 | Task 14 `TestFeishuSourceConnectionPermissionsE2E`（member 403 / apikey 401） |
| 飞书协议（token 缓存/刷新、分页、重试、错误传播、非 docx 跳过） | 4 | Task 12 `feishu/e2e_protocol_test.go`（httptest fake） |
| 幂等、失败重试、worker 续跑 | 6、7 | Task 13 `source_sync_tasks_e2e_test.go`（fakeAsyncConnector + integrationJobQueue） |
| 数据库隔离、fake、测试先行 | 全部 Task | Task 15 安全扫描 + 核对矩阵（DSN 仅来自临时容器） |
| job 目标约束放宽（source_sync 仅 KB） | 1 | Task 1 集成迁移测试 |

## E2E 测试分层总览（显式要求）

飞书同步的 e2e 严格遵循团队既有分层，**禁止任一层缺位**：

| 层级 | 文件 | 外部依赖 mock | asynq | 覆盖什么 | 何时跑 |
|---|---|---|---|---|---|
| **L1 适配器协议** | `internal/adapters/source/feishu/e2e_protocol_test.go` | `httptest.NewServer`（`feishu/testserver` 子包） | 不涉及 | 飞书 OpenAPI 协议正确性：token 缓存/刷新、wiki/drive 递归分页、429 重试、401 错误传播、非 docx 跳过 | `go test -tags=integration ./internal/adapters/source/feishu/...` |
| **L2 worker 级** | `internal/interfaces/worker/source_sync_tasks_e2e_test.go` | `fakeSourceConnector`（实现 port，返回 fixture，不起 HTTP） | `integrationJobQueue`（in-memory，直接驱动 handler，不起 redis） | source_sync handler 的业务正确性：FileTree 构建、source 字段、job 状态推进、幂等（重复执行无副作用）、失败→MarkFailed、TryDispatchConnection 续跑 | `go test -tags=integration -p 1 ./internal/interfaces/worker` |
| **L3 全链路** | `cmd/langhuan/feishu_source_sync_e2e_test.go` | `fakeFeishuSourceFactory` 注入 `buildRuntimeServices`（仿 `v030FakeFactory`）+ `feishu/testserver` fixture | 真 redis（`testsupport.NewIsolatedRedis`）+ 真 asynq Server + `waitReady` 轮询 | **spec 验收 1-8 逐条命中**：多应用加密、来源选择、整树检索、限流串行、增量删除、详情状态、手动同步、权限、SPA 路由 | `go test -tags=integration -p 1 ./cmd/langhuan -run Feishu` |
| **L4 验收核对** | `cmd/langhuan`（文档） | — | — | 验收→e2e 核对矩阵无空洞 + 安全扫描（无 secret/document 泄漏）+ 文档同步 | Task 15 |

强制约束：
- **L3 必须起真 redis + 真 asynq Server**（复用 `startV030E2E` 模式，`v030_e2e_test.go:187`），用 `waitReady` 风格轮询等待（50ms 步长 / 15s deadline，见 `v030_e2e_test.go:359`）。**禁止**用 `ProcessImmediately`/`asynqtest` 同步模式（仓库无此先例）。
- **L1/L2 不起真 redis/asynq**：L1 只 httptest，L2 用 `integrationJobQueue`（`document_tasks_integration_test.go:238`）。
- **外部依赖首选 fake factory/port 注入**（团队模式 A，`v030_e2e_test.go:42`），只有 L1 校验 HTTP 协议细节时才用 httptest（模式 B）。
- **数据库只能来自临时容器**：`testsupport.NewMigratedPostgres(t)`，DSN 走 `LANGHUAN_TEST_DATABASE_DSN`，回退禁连 `config.yaml` 库（AGENTS 5.10）。
- **前端 e2e**：web/ 无前后端联调先例（仅组件级 `vitest-browser-react`），故 L3 用 Go 侧 `v050_e2e` 的 SPA embed 模式（`v050_e2e_test.go:20`）覆盖集成页/KB 详情路由可渲染，不新建浏览器 e2e 体系。

## Plan 自检

- 顺序：Job 约束放宽 → 数据模型 → 凭证 Service → 飞书适配器 → 全量同步 → worker/API → 限流与 KB 创建入队 → 增量/删除 → 应用管理 API → 前端集成页 → 前端 KB 来源 → **L1 协议 e2e → L2 worker e2e → L3 全链路 e2e → L4 核对/文档**。每一步都建立在前一步的可验证产物上。
- **e2e 显式分层**：L1（协议 httptest）→ L2（worker fake connector）→ L3（全链路 真 redis+asynq+fake factory）→ L4（验收核对+安全扫描），四层缺一不可，且每层 mock 方式与团队既有模式对齐。
- **spec 验收全覆盖**：8 条验收标准每条都映射到具名 e2e 函数（见覆盖矩阵），无遗漏。
- 阻碍前置：Task 1 先打通 `jobs_target_check` + `NewJob`，否则后续 source_sync job 无法落库（探索确认的双重关卡）。
- 复用边界：明确复用 `RawDocumentStore.Put` / `model.NewDocumentIdentity` / `NewDocumentRevision(file)` / `document_parse_start` 任务 / `DocumentTaskDBStore` 状态推进 / `startV030E2E` + `testsupport.NewMigratedPostgres/NewIsolatedRedis` + `integrationJobQueue` + `waitReady`；**不**复用 `DocumentIngestService.Ingest`（三重耦合）；**不**用 asynq 多队列（静态队列硬约束）；**不**用 `ProcessImmediately`（无先例）。
- 事实源：capability/connection 来源始终来自 DB；cron 始终来自 source_config；同步额度始终来自 `CountActiveByConnection`。
- 安全：app_secret 只用 cipher（AAD 前缀 `source-connection:` 物理隔离）；日志/DOM 不出现 secret、raw payload、UUID；状态文字+图标。
- 数据库：迁移幂等、版本号递增、事务用 tx、临时 Docker 集成测试。
- 竞态：Meta Scheduler 单 goroutine 单进程；多进程分布式锁列为风险，首版不做。
- 无省略步骤：每个 Task 含 RED → 实现 → GREEN → 提交 gate，带可粘贴 Run 命令。
