# 飞书同步健壮性改进 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修正飞书同步的快照完整性、稳定文档身份、内容 hash 去重、失败重试、force 合并、删除清理和内容大小保护，使同步在异常目录树和外部服务失败时保持可恢复且可观测。

**Architecture:** 先扩展 source connector 返回的 `TreeSnapshot` 和受限 Fetch 合同，再由 application 层用纯函数 `diff` 生成计划，并通过 Workspace transaction 复用稳定 Document/Revision 身份。数据库删除与对象存储删除分为两个阶段：DB 事务级联清理事实和投影，提交后由幂等 `source_cleanup` Job 删除 raw/parser/asset 对象。Force 不修改 worker payload，而使用 `source_config.sync_requested_force` latch 与 DB Job 状态合并请求。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL 17 + pgvector/zhparser、asynq + Redis、golang-migrate、现有 local/S3 storage adapter、现有 parse/chunk/index pipeline。

## Global Constraints

- 权威设计：`docs/superpowers/specs/2026-08-07-feishu-sync-robustness-improvements-design.md`。
- 所有租户 DB 读写必须携带 `workspace_id` 并运行在 `WithinWorkspace` transaction 内。
- `domain` 不依赖 HTTP、GORM、storage SDK；application 不持有 `*gorm.DB`。
- `DocumentRevision` 不可变；Document 更新创建新 revision 或重试现有未完成 revision。
- `partial` 不是 `JobStatus`；同步结果写入 `knowledge_bases.source_config.sync_last_result`。
- `TreeSnapshot.Complete=false` 时禁止删除任何 Document 或 folder，且全局 cursor 不推进。
- 外部对象存储不参与 PostgreSQL 事务；通过幂等 `source_cleanup` Job 最终清理。
- raw object key 必须包含 `RevisionID`；旧 key 的 Open/Delete 兼容行为必须保留。
- 数据库集成测试只能使用测试运行期临时 `langhuan-test-postgres:pg17` 容器；不得连接 `config.yaml` 数据库。
- 外部飞书 API 测试只使用 fake connector 或 `httptest`，不得发真实网络请求。
- 每个任务按 RED → GREEN → `gofmt` → 聚焦测试 → `git diff --check` → Conventional Commit 执行。

---

## 文件结构与职责

| 路径 | 职责 |
|---|---|
| `internal/domain/model/external_node.go` | 远端节点与抓取结果的纯领域结构。 |
| `internal/domain/model/document.go` | `Document.ContentHash` 和稳定 external identity。 |
| `internal/domain/model/file_tree_node.go` | `FileTreeNode.ExternalID` 与 folder/file upsert 数据。 |
| `internal/domain/model/job.go` | `source_cleanup` KB-scoped Job 合同。 |
| `internal/domain/value/source_delete_policy.go` | `keep/remove` typed policy parser。 |
| `internal/ports/source/connector.go` | `TreeSnapshot`、`FetchOptions`、bounded Fetch 端口。 |
| `internal/ports/storage/raw_document.go` | `RawDocumentInput.RevisionID`。 |
| `internal/ports/storage/errors.go` | 统一的对象不存在错误 sentinel，供 cleanup 跨 local/S3 adapter 判断幂等删除。 |
| `internal/adapters/source/feishu/connector.go` | 完整/部分目录快照、分页 warning、Fetch 大小限制。 |
| `internal/adapters/storage/local/raw_document_store.go` | revision-scoped local raw key。 |
| `internal/adapters/storage/s3/key_builder.go`、`store.go` | revision-scoped S3 raw key，旧 key 兼容。 |
| `internal/application/service/source_sync_diff.go` | 去重、diff、cursor watermark 纯函数。 |
| `internal/application/service/source_sync.go` | 快照应用、Document add/update/retry、sync result。 |
| `internal/application/service/source_sync_store.go` | application transaction/store interface。 |
| `internal/application/service/source_cleanup.go` | DB 删除后对象清理编排。 |
| `internal/application/service/source_cleanup_scheduler.go` | 启动和周期扫描 pending cleanup Job，补偿首次入队失败。 |
| `internal/infrastructure/db/source_sync_store.go` | 稳定 Document/Folder upsert、latch、sync result。 |
| `internal/infrastructure/db/source_cleanup_store.go` | cleanup Job、key 收集、DB cascade 删除。 |
| `internal/infrastructure/db/document_rows.go`、`file_tree_rows.go` | 新字段 Row 定义。 |
| `internal/infrastructure/migrate/migrations/000022_feishu_sync_robustness.*.sql` | schema、索引、Job target check。 |
| `internal/interfaces/worker/source_sync_tasks.go` | force latch 消费和同步 Job 状态推进。 |
| `internal/interfaces/worker/source_cleanup_tasks.go` | cleanup Job worker 适配。 |
| `internal/interfaces/http/knowledge_base_sync_handler.go` | `force` body 解析。 |
| `internal/interfaces/http/knowledge_base_source_policy_handler.go` | typed `source-policy` endpoint。 |
| `internal/infrastructure/config/config.go`、`config.example.yaml` | `max_content_bytes` 默认与校验。 |
| `docs/ARCHITECTURE.md`、`docs/API_ACCESS.md`、`docs/DATABASE_GUIDELINES.md` | 运行、API、数据清理合同同步。 |

## Cross-task Interfaces

后续任务必须使用下列签名，不用同义替代名称：

```go
// internal/ports/source/connector.go
type TreeSnapshot struct {
    Nodes       []model.ExternalNode
    Complete    bool
    Warnings    []string
    MaxEditTime time.Time
}

type FetchOptions struct {
    MaxContentBytes int64
}

// internal/application/service/source_sync_store.go
type SyncResult struct {
    Status              string    `json:"status"` // succeeded|partial|failed
    Complete            bool      `json:"complete"`
    SyncedDocuments     int       `json:"synced_documents"`
    SkippedDocuments    int       `json:"skipped_documents"`
    FailedDocuments     int       `json:"failed_documents"`
    OversizeDocuments   int       `json:"oversize_documents"`
    UnsupportedNodes    int       `json:"unsupported_nodes"`
    DeletedDocuments    int       `json:"deleted_documents"`
    CleanupPending      int       `json:"cleanup_pending"`
    FinishedAt          time.Time `json:"finished_at"`
}

type SourceConnector interface {
    ListTree(ctx context.Context, conn model.SourceConnection, root model.SyncRoot) (TreeSnapshot, error)
    Fetch(ctx context.Context, conn model.SourceConnection, externalID string, options FetchOptions) (model.FetchedDocument, error)
    Provider() string
}

type SyncOptions struct {
    Force bool
}

// The request/result types below live in internal/application/service/source_sync_store.go.
type UpdateDocumentRequest struct {
    WorkspaceID   uuid.UUID
    KnowledgeBaseID uuid.UUID
    ExternalID    string
    DocumentID    uuid.UUID
    RevisionID    uuid.UUID
    Title         string
    ParentNodeID  uuid.UUID
    RawStorageKey string
    SHA256        string
    SizeBytes     int64
    ContentType   string
    FileType      string
    Reason        value.DocumentRevisionReason
}

type RetryDocumentRequest struct {
    WorkspaceID    uuid.UUID
    KnowledgeBaseID uuid.UUID
    DocumentID     uuid.UUID
    RevisionID     uuid.UUID
    SHA256         string
    Title          string
    ParentNodeID   uuid.UUID
}

type SyncWriteResult struct {
    DocumentID uuid.UUID
    RevisionID uuid.UUID
    RevisionNo int64
    JobID      uuid.UUID
    RawKey     string
}

type CleanupObject struct {
    Key   string
    Store string // raw|parser|asset
}

func (s *SourceSyncService) EnqueueSync(ctx context.Context, workspaceID, kbID uuid.UUID, options SyncOptions) (*model.Job, error)
func (s *SourceSyncService) SyncKnowledgeBase(ctx context.Context, workspaceID, kbID uuid.UUID) error
```

`SourceSyncTaskPayload` 继续只含 `WorkspaceID`、`KnowledgeBaseID`、`JobID`、`ConnectionID`；force 从 DB latch 原子消费，旧 payload 缺失任何新字段仍按 `force=false` 兼容。

---

### Task 1: 锁定领域与端口合同

**Files:**
- Modify: `internal/domain/model/document.go`
- Modify: `internal/domain/model/document_revision.go`
- Modify: `internal/domain/model/file_tree_node.go`
- Modify: `internal/domain/model/job.go`
- Create: `internal/domain/value/source_delete_policy.go`
- Modify: `internal/ports/source/connector.go`
- Modify: `internal/ports/storage/raw_document.go`
- Create: `internal/ports/storage/errors.go`
- Test: `internal/domain/model/document_test.go`
- Test: `internal/domain/model/document_revision_test.go`
- Test: `internal/domain/model/file_tree_node_test.go`
- Test: `internal/domain/model/job_test.go`
- Test: `internal/domain/value/source_delete_policy_test.go`

**Interfaces:**
- `TreeSnapshot`、`FetchOptions` 和 bounded `SourceConnector` 使用 Cross-task Interfaces 的精确签名。
- `SyncResult` 使用 Cross-task Interfaces 的字段名写入 `source_config.sync_last_result`；`Status` 只允许 `succeeded|partial|failed`，不能扩展通用 `JobStatus`。
- `model.Document` 增加 `ContentHash string`；`model.FileTreeNode` 增加 `ExternalID string`。
- `model.NewDocumentRevisionWithID(revisionID uuid.UUID, input NewDocumentRevisionInput) (*DocumentRevision, error)` 使用显式 ID；现有 `NewDocumentRevision` 保持生成随机 ID 的兼容入口并委托给它。
- `RawDocumentInput` 增加 `RevisionID uuid.UUID`，允许旧调用传 `uuid.Nil`，adapter 对旧 key 保持兼容。
- `storage.ErrObjectNotFound` 是跨 adapter 的对象不存在 sentinel；local/S3 adapter 的 `Open`/`Delete` 对应错误必须用 `%w` 包装它，cleanup 统一用 `errors.Is` 判断幂等删除。
- `SourceDeletePolicy` 提供严格的 `ParseSourceDeletePolicy(raw string) (SourceDeletePolicy, error)`（API/create 使用，空值也视为 validation error）和宽容的 `SourceDeletePolicyFromConfig(raw any) SourceDeletePolicy`（历史缺失/空/非法值统一返回 keep），以及 `IsValid()` 和默认常量 `SourceDeleteKeep`。
- `model.NewJob` 允许 `Type == "source_cleanup"` 且 document/revision/generation 三者全 nil；其它类型继续拒绝全 nil。

- [ ] **Step 1: 写失败测试。**

```go
func TestParseSourceDeletePolicy(t *testing.T) {
    if got, err := ParseSourceDeletePolicy("remove"); err != nil || got != SourceDeleteRemove {
        t.Fatalf("remove = %q, %v", got, err)
    }
    if _, err := ParseSourceDeletePolicy("purge"); !errors.Is(err, domainerrors.ErrValidation) {
        t.Fatalf("invalid policy error = %v", err)
    }
    if got := SourceDeletePolicyFromConfig(nil); got != SourceDeleteKeep {
        t.Fatalf("missing historical policy = %q", got)
    }
}

func TestNewJobAllowsSourceCleanupKBOnly(t *testing.T) {
    workspaceID, kbID := uuid.New(), uuid.New()
    _, err := model.NewJob(model.NewJobInput{
        WorkspaceID: workspaceID, KnowledgeBaseID: kbID,
        Type: "source_cleanup", Status: value.JobStatusPending,
    })
    if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/domain/model ./internal/domain/value -run 'SourceDeletePolicy|SourceCleanup|DocumentRevision' -count=1`

Expected: FAIL，因为新字段、policy parser 和 cleanup Job 分支尚不存在。

- [ ] **Step 3: 实现最小合同。**

保持 domain 纯 struct；严格 parser 只接受 `keep/remove`，宽容的 `SourceDeletePolicyFromConfig` 才把历史 nil/空/非法值映射为 keep。`NewDocumentRevision` 委托给 `NewDocumentRevisionWithID(id.New(), input)`，显式入口拒绝 `uuid.Nil`。在 `internal/ports/storage/errors.go` 定义 `var ErrObjectNotFound = errors.New("storage object not found")`。不要在 connector 或 service 重复定义这些类型。

- [ ] **Step 4: 运行 GREEN 与格式检查。**

Run: `gofmt -w internal/domain internal/ports/source internal/ports/storage && go test ./internal/domain/model ./internal/domain/value ./internal/ports/source ./internal/ports/storage`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/domain internal/ports/source internal/ports/storage
git commit -m "feat(source): 锁定同步健壮性领域合同"
```

### Task 2: 增加 schema migration 与 Row/codec

**Files:**
- Create: `internal/infrastructure/migrate/migrations/000022_feishu_sync_robustness.up.sql`
- Create: `internal/infrastructure/migrate/migrations/000022_feishu_sync_robustness.down.sql`
- Modify: `internal/infrastructure/db/document_rows.go`
- Modify: `internal/infrastructure/db/file_tree_rows.go`
- Modify: `internal/infrastructure/db/job_rows.go`
- Modify: `internal/infrastructure/db/knowledge_rows.go`
- Modify: `internal/infrastructure/db/knowledge_v2_codec.go`
- Modify: `internal/infrastructure/db/models_test.go`
- Create: `internal/infrastructure/migrate/migrate_v022_feishu_sync_robustness_integration_test.go`

**Interfaces:**
- Migration version is `000022`, because the repository already contains `000021_document_ingest_idempotency`.
- `documents.content_hash TEXT NULL`。
- `file_tree_nodes.external_id TEXT NULL`。
- Replace old `idx_documents_kb_external` with unique partial `uq_documents_workspace_kb_external` on `(workspace_id, knowledge_base_id, external_id)`.
- Add unique partial `uq_file_tree_nodes_kb_external` on `(workspace_id, knowledge_base_id, external_id)`。
- Extend `jobs_target_check` for `type='source_cleanup'` KB-only jobs; keep `source_sync` branch.
- Migration checks duplicate `documents.external_id` and duplicate `file_tree_nodes.external_id` folder tokens with separate `DO $$ ... RAISE EXCEPTION ... $$` blocks; error text includes workspace/KB/token and it never deletes or merges rows.

- [ ] **Step 1: 写 migration integration RED。**

```go
//go:build integration

func TestV022AddsFeishuSyncRobustnessSchema(t *testing.T) {
    database, migrator := newZhparserMigrationTest(t)
    require.NoError(t, migrator.Migrate(22))

    for _, tc := range []struct{ table, column string }{
        {"documents", "content_hash"},
        {"file_tree_nodes", "external_id"},
    } {
        var count int
        require.NoError(t, database.QueryRowContext(context.Background(), `
            SELECT count(*) FROM information_schema.columns
            WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
        `, tc.table, tc.column).Scan(&count))
        require.Equal(t, 1, count, "%s.%s", tc.table, tc.column)
    }

    for _, indexName := range []string{
        "uq_documents_workspace_kb_external",
        "uq_file_tree_nodes_kb_external",
    } {
        var count int
        require.NoError(t, database.QueryRowContext(context.Background(), `
            SELECT count(*) FROM pg_indexes
            WHERE schemaname = 'public' AND indexname = $1
        `, indexName).Scan(&count))
        require.Equal(t, 1, count, "index %s", indexName)
    }

    var targetCheck string
    require.NoError(t, database.QueryRowContext(context.Background(), `
        SELECT pg_get_constraintdef(oid)
        FROM pg_constraint
        WHERE conname = 'jobs_target_check'
    `).Scan(&targetCheck))
    require.Contains(t, targetCheck, "source_cleanup")

    workspaceID := "12222222-2222-4222-8222-222222222222"
    kbID := "13333333-3333-4333-8333-333333333333"
    rootID := "14444444-4444-4444-8444-444444444444"
    _, err := database.ExecContext(context.Background(), `
        INSERT INTO workspaces (id, name, slug) VALUES ($1, 'v022-job-ws', 'v022-job-ws');
        INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($2, $1, 'v022-job-kb', $3);
        INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($3, $1, $2, 'root', '');
        INSERT INTO jobs (workspace_id, knowledge_base_id, type, status) VALUES ($1, $2, 'source_cleanup', 'pending');
    `, workspaceID, kbID, rootID)
    require.NoError(t, err, "source_cleanup KB-only job must satisfy jobs_target_check")
}

func TestV022RejectsDuplicateDocumentExternalID(t *testing.T) {
    database, migrator := newZhparserMigrationTest(t)
    require.NoError(t, migrator.Migrate(21))
    ctx := context.Background()
    tx, err := database.BeginTx(ctx, nil)
    require.NoError(t, err)
    defer tx.Rollback()
    exec := func(query string, args ...any) {
        _, execErr := tx.ExecContext(ctx, query, args...)
        require.NoError(t, execErr)
    }
    workspaceID := "32222222-2222-4222-8222-222222222222"
    kbID := "33333333-3333-4333-8333-333333333333"
    rootID := "34444444-4444-4444-8444-444444444444"
    firstDocID := "35555555-5555-4555-8555-555555555555"
    secondDocID := "36666666-6666-4666-8666-666666666666"
    exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'v022-ws', 'v022-ws')`, workspaceID)
    exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, 'v022-kb', $3)`, kbID, workspaceID, rootID)
    exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
    exec(`INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status, external_id) VALUES ($1, $2, $3, 'file', 'one', 'feishu', 'pending', 'dup-token')`, firstDocID, workspaceID, kbID)
    exec(`INSERT INTO documents (id, workspace_id, knowledge_base_id, kind, title, source_type, status, external_id) VALUES ($1, $2, $3, 'file', 'two', 'feishu', 'pending', 'dup-token')`, secondDocID, workspaceID, kbID)
    require.NoError(t, tx.Commit())
    err := migrator.Migrate(22)
    require.Error(t, err)
    require.Contains(t, err.Error(), "duplicate")
}

func TestV022RejectsDuplicateFolderExternalID(t *testing.T) {
    database, migrator := newZhparserMigrationTest(t)
    require.NoError(t, migrator.Migrate(21))
    ctx := context.Background()
    tx, err := database.BeginTx(ctx, nil)
    require.NoError(t, err)
    defer tx.Rollback()
    exec := func(query string, args ...any) {
        _, execErr := tx.ExecContext(ctx, query, args...)
        require.NoError(t, execErr)
    }
    exec(`ALTER TABLE file_tree_nodes ADD COLUMN external_id text`)
    workspaceID := "42222222-2222-4222-8222-222222222222"
    kbID := "43333333-3333-4333-8333-333333333333"
    rootID := "44444444-4444-4444-8444-444444444444"
    folderOneID := "45555555-5555-4555-8555-555555555555"
    folderTwoID := "46666666-6666-4666-8666-666666666666"
    exec(`INSERT INTO workspaces (id, name, slug) VALUES ($1, 'v022-folder-ws', 'v022-folder-ws')`, workspaceID)
    exec(`INSERT INTO knowledge_bases (id, workspace_id, name, file_tree_root_id) VALUES ($1, $2, 'v022-folder-kb', $3)`, kbID, workspaceID, rootID)
    exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, node_type, name) VALUES ($1, $2, $3, 'root', '')`, rootID, workspaceID, kbID)
    exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, external_id) VALUES ($1, $2, $3, $4, 'folder', 'one', 'dup-folder')`, folderOneID, workspaceID, kbID, rootID)
    exec(`INSERT INTO file_tree_nodes (id, workspace_id, knowledge_base_id, parent_id, node_type, name, external_id) VALUES ($1, $2, $3, $4, 'folder', 'two', 'dup-folder')`, folderTwoID, workspaceID, kbID, rootID)
    require.NoError(t, tx.Commit())
    err = migrator.Migrate(22)
    require.Error(t, err)
    require.Contains(t, err.Error(), "duplicate")
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test -tags=integration -p 1 ./internal/infrastructure/migrate -run V022 -count=1`

Expected: FAIL，因为 000022 和新列不存在。

测试使用仓库已有的 `newZhparserMigrationTest` 和 `migrator.Migrate`；不得回退到 `config.yaml` DSN，也不得调用仓库中不存在的迁移数据库或断言 helper。

- [ ] **Step 3: 编写 up/down migration。**

up migration 先在已有 `documents.external_id` 上检测 duplicate；随后增加 `content_hash` 和 `file_tree_nodes.external_id`（`IF NOT EXISTS`），再检测 folder duplicate，最后替换文档索引、创建 folder 部分唯一索引并重建 target check。每个冲突都用 `RAISE EXCEPTION` 中止迁移并包含 workspace/KB/token。down migration 按逆序删除 constraint/index/columns，不删除 `source_config` JSONB 键。不要使用 `DROP TABLE` 或静默数据合并。

- [ ] **Step 4: 更新 Row/codec 并运行集成测试。**

Run: `gofmt -w internal/infrastructure/db && go test -tags=integration -p 1 ./internal/infrastructure/migrate ./internal/infrastructure/db -run 'V022|Document.*Codec|FileTree' -count=1`

Expected: PASS；从空库 up/down 成功，重复数据迁移明确失败。

- [ ] **Step 5: 提交。**

```bash
git add internal/infrastructure/migrate internal/infrastructure/db
git commit -m "feat(db): 增加飞书同步稳健性 schema"
```

### Task 3: revision-scoped raw storage key

**Files:**
- Modify: `internal/adapters/storage/local/raw_document_store.go`
- Modify: `internal/adapters/storage/local/asset_store.go`
- Modify: `internal/adapters/storage/s3/key_builder.go`
- Modify: `internal/adapters/storage/s3/store.go`
- Modify: `internal/adapters/storage/local/raw_document_store_test.go`
- Modify: `internal/adapters/storage/local/asset_store_test.go`
- Modify: `internal/adapters/storage/s3/key_builder_test.go`
- Modify: `internal/adapters/storage/s3/store_test.go`
- Modify: `internal/application/service/document_ingest.go`
- Modify: `internal/application/service/document_ingest_test.go`

**Interfaces:**
- 当 `RevisionID != uuid.Nil` 时，local key 为 `{workspace}/{kb}/{document}/{revision}/original.{ext}`，S3 key 为 `raw-documents/{workspace}/{kb}/{document}/{revision}/original.{ext}`。
- `DocumentIngestService` 在 raw Put 前预分配 revision ID，把它同时传给 `RawDocumentInput.RevisionID` 和 `model.NewDocumentRevisionWithID`；Task 7 对 source sync 使用同一模式。
- 当 `RevisionID == uuid.Nil` 时仅为尚未迁移的调用方保留当前 Put 行为；`Open/Delete` 始终直接使用数据库保存的完整 key，因此旧 local key 和旧 S3 zero-UUID key 不需要重写即可继续访问。
- local `Open/Delete` 把 `os.ErrNotExist` 包装为 `storage.ErrObjectNotFound`；S3 `Open/Delete` 通过 `errors.As(err, smithy.APIError)` 将 `NoSuchKey`/`NotFound` 映射为同一 sentinel。S3-compatible 服务若对不存在对象的 `DeleteObject` 直接返回成功，也保持成功。
- `RawDocumentObject.Key/SHA256/SizeBytes` 语义不变。

- [ ] **Step 1: 写失败测试。**

```go
func TestRawDocumentRevisionKeysDoNotOverwrite(t *testing.T) {
    first := putRaw(t, uuid.New(), []byte("one"))
    second := putRaw(t, uuid.New(), []byte("two"))
    if first.Key == second.Key { t.Fatalf("revision keys collide: %q", first.Key) }
    assertRawContent(t, first.Key, "one")
    assertRawContent(t, second.Key, "two")
}

func TestLocalDeleteMissingRawObjectMapsSentinel(t *testing.T) {
    store := NewRawDocumentStore(t.TempDir())
    err := store.Delete(context.Background(), "missing/object.md")
    if !errors.Is(err, portstorage.ErrObjectNotFound) {
        t.Fatalf("Delete() error = %v", err)
    }
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/adapters/storage/local -run RevisionKeys -count=1 && go test ./internal/adapters/storage/s3 -run RevisionKeys -count=1`

Expected: FAIL，因为 `RawDocumentInput` 当前不携带 revision ID，key 会覆盖。

- [ ] **Step 3: 实现 key 分支和兼容读取。**

local adapter 使用 `filepath.Join` 保持路径安全；S3 adapter 复用 `RawDocumentKey`。实现一个 adapter 内部错误映射 helper，local raw/asset 与 S3 raw/asset 都向上返回 `storage.ErrObjectNotFound`。不要更改 asset key 或 parser result key。上传服务必须先生成 revision ID，再写 raw 对象，数据库失败仍沿用现有补偿删除。

- [ ] **Step 4: 运行 adapter 测试。**

Run: `gofmt -w internal/adapters/storage/local internal/adapters/storage/s3 internal/application/service && go test ./internal/adapters/storage/local ./internal/adapters/storage/s3 ./internal/application/service -run 'RawDocument|DocumentIngest|RevisionKey|ObjectNotFound' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/adapters/storage/local internal/adapters/storage/s3 internal/application/service/document_ingest.go internal/application/service/document_ingest_test.go
git commit -m "feat(storage): 为同步 revision 隔离原始对象 key"
```

### Task 4: Source connector 快照完整性与 bounded Fetch

**Files:**
- Modify: `internal/adapters/source/feishu/connector.go`
- Modify: `internal/adapters/source/feishu/connector_test.go`
- Modify: `internal/ports/source/connector.go`

**Interfaces:**
- `ListTree` 返回 `source.TreeSnapshot`；`TreeSnapshot`、`FetchOptions`、`ErrSourceContentTooLarge` 全部定义在 `internal/ports/source/connector.go`，application 侧统一以 `sourceport.TreeSnapshot` 引用。
- fatal 鉴权、根不存在、不可恢复 API error 返回 error 且不返回可应用 snapshot。
- `Fetch` 接受 `FetchOptions{MaxContentBytes}`；HTTP body 使用 `io.LimitReader`/Content-Length 预检，超过限制返回 sentinel oversize error，不能无界缓存。
- 完整遍历成功时计算所有非零节点 `EditTime` 的最大值填入 `TreeSnapshot.MaxEditTime`；partial snapshot 即使有最大值也不推进 application cursor。
- 一次 snapshot 内 token 去重由 application pure function 负责，adapter 不改变首项顺序。

- [ ] **Step 1: 写 connector RED。**

```go
func TestListTreeReturnsIncompleteOnRecoverablePageFailure(t *testing.T) {
    connector := testConnector(fakeAPIWithSecondPageRateLimit())
    snapshot, err := connector.ListTree(ctx, testConn(), root)
    if err != nil { t.Fatal(err) }
    if snapshot.Complete { t.Fatal("recoverable page failure must be incomplete") }
    if len(snapshot.Warnings) == 0 { t.Fatal("missing warning") }
}

func TestFetchStopsAtMaxContentBytes(t *testing.T) {
    connector := testConnector(fakeAPIWithBody(bytes.Repeat([]byte("x"), 1024)))
    _, err := connector.Fetch(ctx, testConn(), "doccn1", source.FetchOptions{MaxContentBytes: 128})
    if !errors.Is(err, source.ErrSourceContentTooLarge) { t.Fatalf("err = %v", err) }
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/adapters/source/feishu -run 'Incomplete|MaxContentBytes' -count=1`

Expected: FAIL，因为端口仍返回裸节点数组且 Fetch 无大小参数。

- [ ] **Step 3: 实现分页状态和限制读取。**

完整树只有所有递归 child 和分页成功后才置 `Complete=true`。对可恢复错误保留已获取节点、附 warning；对 fatal error 返回 error。Fetch 先检查 Content-Length，再通过受限 reader 读取，超过上限立即停止并返回 `ErrSourceContentTooLarge`。

- [ ] **Step 4: 运行 adapter 全测试。**

Run: `gofmt -w internal/ports/source internal/adapters/source/feishu && go test ./internal/adapters/source/feishu`

Expected: PASS，现有完整 wiki/drive pagination 测试和新增 partial/oversize 测试同时通过。

- [ ] **Step 5: 提交。**

```bash
git add internal/ports/source internal/adapters/source/feishu
git commit -m "feat(source): 增加飞书快照完整性和内容大小限制"
```

### Task 5: diff、去重和安全 cursor watermark

**Files:**
- Create: `internal/application/service/source_sync_diff.go`
- Create: `internal/application/service/source_sync_diff_test.go`

**Interfaces:**
- `diff(snapshot sourceport.TreeSnapshot, local []localDocView, cursor time.Time, force bool) syncPlan`。
- 纯函数内部去重 remote token 和 local external ID；remote 重复项保留首项，local 重复项保留首项用于更新但把该 token 从 `ToRemove` 排除，二者都加入 `Warnings`，绝不自动合并业务数据。
- `sourceport.TreeSnapshot.Complete=false` 时 `ToRemove` 为空；zero `EditTime` 不推进 watermark。
- `computeSafeCursor(snapshot sourceport.TreeSnapshot, outcomes []nodeOutcome, previous time.Time) time.Time` 只返回完整 snapshot 的成功前缀时间；同 EditTime 节点必须全部成功。

- [ ] **Step 1: 写 table-driven RED。**

```go
func TestDiffRules(t *testing.T) {
    cases := []struct {
        name string
        snapshot sourceport.TreeSnapshot
        local []localDocView
        cursor time.Time
        force bool
        wantAdd, wantUpdate, wantRemove, wantSkipped int
    }{
        {name: "incomplete never removes", snapshot: sourceport.TreeSnapshot{Nodes: nodes("remote"), Complete: false}, local: docs("missing"), wantAdd: 1},
        {name: "complete removes", snapshot: sourceport.TreeSnapshot{Nodes: nil, Complete: true}, local: docs("missing"), wantRemove: 1},
        {name: "force updates unchanged", snapshot: sourceport.TreeSnapshot{Nodes: nodes("same"), Complete: true}, local: docs("same"), force: true, wantUpdate: 1},
        {name: "retry updates unchanged", snapshot: sourceport.TreeSnapshot{Nodes: nodes("same"), Complete: true}, local: failedDocs("same"), wantUpdate: 1},
    }
    // assert plan counts and warning count for duplicate tokens.
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/application/service -run 'TestDiffRules|TestSafeCursor' -count=1`

Expected: FAIL，因为 `diff`, local view 和 watermark 尚不存在。

- [ ] **Step 3: 实现纯函数。**

使用 map 建立 remote/local index；remote duplicate 保留第一次；local duplicate 保留首项用于更新、从删除集合排除并产生 warning。表格规则以 `RetryRequired` 优先于 cursor，以 `force` 优先于 hash 判断。实现 `nodeOutcome` 和安全前缀扫描，不执行 I/O。

- [ ] **Step 4: 运行 service 单测与静态检查。**

Run: `gofmt -w internal/application/service/source_sync_diff.go && go test ./internal/application/service -run 'TestDiffRules|TestSafeCursor' -count=1 && go vet ./internal/application/service`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/service/source_sync_diff.go internal/application/service/source_sync_diff_test.go
git commit -m "feat(source): 提取同步 diff 和安全 cursor 计算"
```

### Task 6: DB source sync store：稳定 Document/Folder upsert、latch 与结果

**Files:**
- Modify: `internal/application/service/source_sync_store.go`
- Modify: `internal/infrastructure/db/source_sync_store.go`
- Modify: `internal/infrastructure/db/file_tree_repository.go`
- Modify: `internal/infrastructure/db/source_sync_store_integration_test.go`
- Modify: `internal/infrastructure/db/knowledge_base_repository.go`

**Interfaces:**
- `ListSourceDocuments(ctx, kbID) ([]localDocView, error)` 返回含 deleted/failed/retry 信息的投影。
- `UpsertSourceFolder(ctx, folder *model.FileTreeNode) error` 按 workspace/KB/external ID 锁定并更新 parent/name。
- `CreateSyncedDocumentRevisionJob(ctx, request UpdateDocumentRequest) (*SyncWriteResult, error)` 在一个 tx 内锁定 Document、递增 revision、创建 Revision+Job、更新 hash/status 和 FileTreeNode。
- `RetrySourceRevision(ctx, request RetryDocumentRequest) (*SyncWriteResult, error)` 复用最新未完成/失败 revision，不创建同 hash 新 revision。
- `DeleteSourceDocument(ctx, documentID uuid.UUID, policy SourceDeletePolicy) ([]CleanupObject, error)`：keep 更新 deleted/status；remove 收集对象 key、创建 KB-only cleanup Job 后删除 Document。
- `RequestSourceSync(ctx, workspaceID, kbID, connectionID uuid.UUID, requestedForce bool) (job *model.Job, created bool, err error)` 在 KB 锁定事务中执行 latch=`old OR requestedForce`，复用 active Job 或创建新 Job。
- `ConsumeForceLatch(ctx, workspaceID, kbID, jobID uuid.UUID) (bool, error)` 原子读取并清除 latch；`FinalizeSourceSyncJob(ctx, workspaceID, kbID, jobID uuid.UUID, status value.JobStatus, errorMessage string) (*model.Job, error)` 在同一 KB 锁定事务中标记终态并在 latch=true 时创建下一 Job。
- `FailSourceSyncEnqueue(ctx, workspaceID, kbID, jobID uuid.UUID, message string) error` 标记首次 enqueue 失败但保留 force latch，供 scheduler 恢复。
- `UpdateSyncResult(ctx, workspaceID, kbID, result SyncResult) error` 使用 `jsonb_set` 保留 root/cursor/cron 等其它键。

- [ ] **Step 1: 写数据库集成 RED。**

```go
func TestSourceSyncStoreReusesDocumentAndIncrementsRevision(t *testing.T) {
    ctx, database := newAuthTestDB(t)
    seed := insertKnowledgeSchemaSeed(t, ctx, database)
    document, revision, node, job := newFileIngestAggregate(t, seed.workspaceID, seed.kbID, seed.rootID, "source.md")
    document.ExternalID = "external-1"
    document.SourceType = "feishu"
    require.NoError(t, NewDocumentIngestDBStore(database).WithinWorkspace(ctx, seed.workspaceID,
        func(txCtx context.Context, tx appservice.DocumentIngestTx) error {
            return tx.CreateFileDocumentNodeRevisionAndJob(txCtx, document, node, revision, job)
        }))
    store := NewSourceSyncDBStore(database)
    got, err := store.CreateSyncedDocumentRevisionJob(ctx, UpdateDocumentRequest{
        WorkspaceID: seed.workspaceID, KnowledgeBaseID: seed.kbID, ExternalID: "external-1",
        DocumentID: document.ID, RevisionID: uuid.New(), Title: "source.md", ParentNodeID: seed.rootID,
        RawStorageKey: "raw/new", SHA256: "new-hash", SizeBytes: 3, ContentType: "text/markdown",
        FileType: "markdown", Reason: value.DocumentRevisionReasonCrawl,
    })
    require.NoError(t, err)
    require.Equal(t, document.ID, got.DocumentID)
    require.Equal(t, int64(2), got.RevisionNo)
}

func TestConsumeAndFinalizeForceLatchIsAtomic(t *testing.T) {
    ctx, database := newAuthTestDB(t)
    seed := insertKnowledgeSchemaSeed(t, ctx, database)
    store := NewSourceSyncDBStore(database)
    current, created, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, uuid.New(), true)
    require.NoError(t, err)
    require.True(t, created)
    force, err := store.ConsumeForceLatch(ctx, seed.workspaceID, seed.kbID, current.ID)
    require.NoError(t, err)
    require.True(t, force)
    same, created, err := store.RequestSourceSync(ctx, seed.workspaceID, seed.kbID, current.SourceConnectionID, true)
    require.NoError(t, err)
    require.False(t, created)
    require.Equal(t, current.ID, same.ID)
    next, err := store.FinalizeSourceSyncJob(ctx, seed.workspaceID, seed.kbID, current.ID, value.JobStatusSucceeded, "")
    require.NoError(t, err)
    require.NotNil(t, next)
    require.NotEqual(t, current.ID, next.ID)
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test -tags=integration -p 1 ./internal/infrastructure/db -run 'ReusesDocument|ForceLatch' -count=1`

Expected: FAIL，因为 store contract 和实现不存在。

- [ ] **Step 3: 实现 tx-bound store。**

所有查询显式 `workspace_id`、`knowledge_base_id`；revision 递增使用 `SELECT ... FOR UPDATE` 锁住 Document 后计算 max；`active_revision_id` 不在 source transaction 内提前切换。`content_hash` 只在接受 source revision 的同一事务中更新，上传 Document 保持 NULL。remove 先收集 raw/parser/asset keys 和创建 cleanup Job，再删除 Document，让 FK cascade 清理 retrieval/chunk/file tree。`UpdateSyncResult` 必须用 `jsonb_set` 局部更新，保留 root/cursor/cron/latch 等键。

- [ ] **Step 4: 运行 DB 集成测试。**

Run: `make test-image && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'SourceSync|ForceLatch|Cleanup' -count=1`

Expected: PASS；测试只连接临时 pgvector/zhparser 容器。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/service/source_sync_store.go internal/infrastructure/db
git commit -m "feat(source): 增加稳定同步事务和 force latch"
```

### Task 7: SourceSyncService add/update/retry、folder diff、partial result

**Files:**
- Modify: `internal/application/service/source_sync.go`
- Modify: `internal/application/service/source_sync_test.go`
- Modify: `internal/application/service/source_sync_store.go`
- Modify: `internal/application/service/source_sync_diff.go`

**Interfaces:**
- `SyncKnowledgeBase` 读取 KB/connection/root，调用 `ListTree`，读取 local projection，调用 `diff`，按 folder/doc plan 应用。
- Add path：预分配 `revision.ID` → bounded Fetch → application 用 `sha256.Sum256(markdown)` 计算内容 hash → revision-scoped raw Put → tx 创建 Document/FileTree/Revision/Job → parse enqueue。
- Update path：按 external ID 复用 Document；hash unchanged 无 retry 时跳过；hash changed/force 创建 revision；same hash retry 复用 failed/pending revision。
- Update path 成功后清空 `deleted_at` 并恢复可用状态；`keep` 文档远端重新出现时复用原 Document ID，`remove` 文档已级联删除时按新身份创建。新 revision 由 `NewDocumentRevisionWithID` 使用预分配 ID 构造，pipeline 仍负责 active revision 发布。
- Folder path：按 external ID upsert，完整 snapshot 删除深度优先的空 folder；partial snapshot 不删 folder。
- `sync_last_result.status`：fatal=`failed`，节点失败/不完整 snapshot/folder 删除失败=`partial`，全部成功=`succeeded`。
- 完整填充 `SyncResult` 的计数和 `finished_at`；记录 `remote_doc_count`、`local_doc_count` 与 connector warnings。远端 docx 数低于本地未删除 docx 数一半时写高优先级告警，但不能改变 `Complete` 删除闸门。
- 完整 snapshot 才计算并写安全 cursor；partial 完全不写 cursor。

- [ ] **Step 1: 写 application RED。**

```go
func TestSyncReusesExternalDocumentAndSkipsUnchangedContent(t *testing.T) {
    env := newSourceSyncServiceTestEnv(t)
    require.NoError(t, env.syncOnce(snapshotWithDoc("doc-1", "same")))
    first := env.documentByExternalID("doc-1")
    firstRevisionCount := env.revisionCount(first.ID)
    require.NoError(t, env.syncOnce(snapshotWithDoc("doc-1", "same")))
    require.Equal(t, first.ID, env.documentByExternalID("doc-1").ID)
    require.Equal(t, firstRevisionCount, env.revisionCount(first.ID))
}

func TestSyncFailureDoesNotAdvanceCursorAndRetries(t *testing.T) {
    env := newSourceSyncServiceTestEnv(t)
    env.connector.failFetchOnce("doc-1")
    require.NoError(t, env.syncOnce(snapshotWithDoc("doc-1", "content")))
    require.True(t, env.lastCursor().IsZero())
    require.NoError(t, env.syncOnce(snapshotWithDoc("doc-1", "content")))
    require.Equal(t, 1, env.parseJobCount("doc-1"))
}

func TestPartialSnapshotNeverDeletesOrAdvancesCursor(t *testing.T) {
    env := seededSourceSyncEnvironment(t)
    before := env.lastCursor()
    require.NoError(t, env.syncOnce(sourceport.TreeSnapshot{Nodes: nil, Complete: false}))
    require.False(t, env.documentDeleted("existing"))
    require.Equal(t, before, env.lastCursor())
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/application/service -run 'ReusesExternal|FailureDoesNotAdvance|PartialSnapshot' -count=1`

Expected: FAIL，因为当前实现每次创建新 Document、固定 revision 1 且直接推进 cursor。

- [ ] **Step 3: 实现 service 编排。**

将当前 `syncNode` 拆成 folder upsert、`addSourceDocument`、`updateSourceDocument`、`retrySourceDocument` 和 `applyMissingDocuments`；节点错误记录 outcome 后继续其它节点。application 对最终 `len(markdown)` 做第二次大小检查，超限新文档不落库、已有文档保留旧 hash/revision；不支持的 sheet/bitable 只增加 `unsupported_nodes` 和 warning。raw Put、DB tx、queue enqueue 的补偿遵循 spec，enqueue error 标记新 revision failed。ListTree fatal 时 best-effort 写 `status=failed, complete=false, finished_at`，写结果失败只记录结构化日志并保留原错误链。

- [ ] **Step 4: 运行 application 测试。**

Run: `gofmt -w internal/application/service && go test ./internal/application/service -run 'SourceSync|ReusesExternal|FailureDoesNotAdvance|PartialSnapshot' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/service/source_sync.go internal/application/service/source_sync_diff.go internal/application/service/source_sync_store.go internal/application/service/source_sync_test.go
git commit -m "feat(source): 实现稳定文档增量同步"
```

### Task 8: 删除策略与 cleanup service/worker

**Files:**
- Create: `internal/application/service/source_cleanup.go`
- Create: `internal/application/service/source_cleanup_test.go`
- Create: `internal/interfaces/worker/source_cleanup_tasks.go`
- Create: `internal/interfaces/worker/source_cleanup_tasks_test.go`
- Create: `internal/infrastructure/db/source_cleanup_store.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `internal/infrastructure/db/retrieval_search_repository.go`
- Modify: `internal/application/service/knowledge_base_summary.go`
- Create: `internal/application/service/source_cleanup_scheduler.go`
- Create: `internal/application/service/source_cleanup_scheduler_test.go`
- Modify: `internal/infrastructure/db/job_repository.go`

**Interfaces:**
- cleanup payload 为 `{workspace_id, knowledge_base_id, job_id, object_keys}`；对象 key 超过具名 batch size 时拆 Job。
- 在 `source_cleanup.go` 声明具名常量 `const sourceCleanupObjectBatchSize = 100`，批次切分按稳定 key 顺序进行；每个 Job 的 payload 只含一个批次。
- cleanup worker 只解码 payload、调用 `SourceCleanupService.Run`、推进 Job 状态；重复删除不存在对象视为成功。
- `SourceCleanupScheduler.RequeuePending(ctx)` 启动时调用一次，`Tick(ctx)` 周期调用；它只扫描当前 Workspace 可见且 `status=pending` 的 `source_cleanup` Job，使用 `job_id` 作为 TaskID，入队失败保留 pending 并记录 warning。
- keep 策略的 Document 不参与检索和摘要统计；remove 依赖 FK cascade 清理 retrieval/chunk/revision/file tree，外部对象提交后异步删除。

- [ ] **Step 1: 写 cleanup RED。**

```go
func TestCleanupTreatsMissingObjectAsSuccess(t *testing.T) {
    env := newCleanupTestEnv(t)
    env.storage.deleteErr = storage.ErrObjectNotFound
    require.NoError(t, env.service.Run(ctx, cleanupRequest("missing-key")))
    require.Equal(t, value.JobStatusSucceeded, env.jobStatus())
}

func TestKeepDeletedDocumentIsExcludedFromSearch(t *testing.T) {
    env := seededDeletedDocumentSearchEnv(t)
    results, err := env.search.Search(ctx, queryInput("deleted text"))
    require.NoError(t, err)
    require.Empty(t, results)
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/application/service ./internal/interfaces/worker ./internal/infrastructure/db -run 'Cleanup|KeepDeleted' -count=1`

Expected: FAIL，因为 cleanup service/worker 和 deleted 过滤不存在。

- [ ] **Step 3: 实现 DB 删除与对象 cleanup。**

`remove` transaction 先锁 Document、收集 revision raw/parser/asset keys、创建 KB-only Job，再删除 Document；application 在事务提交后 enqueue。cleanup 使用 `errors.Is(err, storage.ErrObjectNotFound)` 判定幂等成功；其它错误保留 failed 以便重试。`source_cleanup` 的 payload 只携带 workspace/KB/job lineage 与有限批次 object keys，不携带即将删除的 Document 外键。

`SourceCleanupScheduler` 通过 `ListPendingSourceCleanupJobs` 读取 Job，并在启动/每次 Tick 调用 `EnqueueCleanupJob`；注册到 `cmd/langhuan/main.go` 的 scheduler 生命周期，确保 DB 提交后首次 asynq 入队失败不会留下永久孤儿。

- [ ] **Step 4: 运行 focused tests 和 integration tests。**

Run: `gofmt -w internal/application/service internal/interfaces/worker internal/infrastructure/db cmd/langhuan && go test ./internal/application/service ./internal/interfaces/worker -run 'Cleanup|KeepDeleted|RequeuePending' -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'Cleanup|DeletedDocument' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/service/source_cleanup.go internal/application/service/source_cleanup_scheduler.go internal/interfaces/worker/source_cleanup_tasks.go internal/infrastructure/db/source_cleanup_store.go internal/infrastructure/db/job_repository.go internal/interfaces/worker internal/infrastructure/db/retrieval_search_repository.go internal/application/service/knowledge_base_summary.go cmd/langhuan/main.go
git commit -m "feat(source): 增加删除策略和异步对象清理"
```

### Task 9: 配置、force API 与 worker latch 流程

**Files:**
- Modify: `internal/infrastructure/config/config.go`
- Modify: `config.example.yaml`
- Modify: `internal/application/service/source_sync.go`
- Modify: `internal/application/service/source_sync_scheduler.go`
- Modify: `internal/interfaces/worker/source_sync_tasks.go`
- Modify: `internal/interfaces/worker/source_sync_tasks_test.go`
- Modify: `internal/application/service/source_sync_scheduler_test.go`
- Modify: `internal/interfaces/http/knowledge_base_sync_handler.go`
- Modify: `internal/interfaces/http/knowledge_base_sync_handler_test.go`
- Modify: `internal/application/service/knowledge_base.go`
- Modify: `internal/application/service/knowledge_base_test.go`

**Interfaces:**
- `SourceSyncConfig.MaxContentBytes int64` 默认 `52428800`，`applyDefaults` 和 `validateSourceSync` 都要求 `>0`。
- HTTP sync body `{"force":true}` 解析为 `SyncOptions{Force:true}`；空 body 为 false；未知字段仍由 `decodeStrictJSON` 拒绝。
- `RequestSourceSync` 在 KB tx 中设置 `sync_requested_force = old || requested`；active Job 存在则复用 job ID。
- worker 开始时调用 `ConsumeForceLatch` 原子消费 latch；结束时调用 `FinalizeSourceSyncJob` 在同一 KB 锁定 tx 标记 Job 终态并创建后续 Job；scheduler 负责 latch=true 且无 active Job 的恢复派发。
- KB 创建首次同步显式传 `SyncOptions{Force:false}`；普通 cron/manual 不设置 force。
- `SyncEnqueuer` 和 `SourceSyncScheduler` 的接口统一改为 `EnqueueSync(ctx, workspaceID, kbID, options SyncOptions)`；scheduler 的 cron/续跑调用固定传 `SyncOptions{Force:false}`，只 HTTP 手动 force 传 true。

- [ ] **Step 1: 写失败测试。**

```go
func TestManualSyncForceSetsLatchAndReturnsExistingJob(t *testing.T) {
    env := newSyncHTTPTestEnv(t)
    first := env.enqueue(false)
    second := env.enqueue(true)
    require.Equal(t, first.ID, second.ID)
    require.True(t, env.forceLatch())
}

func TestWorkerConsumesForceAndDispatchesPendingLatch(t *testing.T) {
    env := newSourceSyncWorkerEnv(t)
    env.setForceLatch(true)
    require.NoError(t, env.handleSourceSyncTask())
    require.True(t, env.syncOptions.Force)
    env.requestForceDuringRun()
    require.Equal(t, 2, env.sourceSyncJobCount())
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/interfaces/http ./internal/interfaces/worker ./internal/application/service ./internal/infrastructure/config -run 'Force|MaxContentBytes|Latch' -count=1`

Expected: FAIL，因为 API 尚无 body、latch 尚未接入，配置无大小上限。

- [ ] **Step 3: 实现配置和请求合并。**

只在 application/store transaction 中读写 latch；不修改已入队 asynq payload。TaskID 使用 `job_id`，scheduler/worker 以 DB Job 状态为权威。同步 service 从 `ConsumeForceLatch` 获得 force 参数并传入 `diff`。

- [ ] **Step 4: 运行 HTTP/worker/config/scheduler 测试。**

Run: `gofmt -w internal/infrastructure/config internal/application/service internal/interfaces/http internal/interfaces/worker && go test ./internal/infrastructure/config ./internal/application/service ./internal/interfaces/http ./internal/interfaces/worker -run 'Force|MaxContentBytes|Latch|Scheduler' -count=1`

Expected: PASS；旧 payload 仍解码，force 请求不丢失，非法 JSON 被拒绝。

- [ ] **Step 5: 提交。**

```bash
git add internal/infrastructure/config config.example.yaml internal/application/service internal/interfaces/http internal/interfaces/worker
git commit -m "feat(source): 增加 force 同步和大小上限配置"
```

### Task 10: source-policy API 与来源配置保护

**Files:**
- Create: `internal/interfaces/http/knowledge_base_source_policy_handler.go`
- Create: `internal/interfaces/http/knowledge_base_source_policy_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/openapi_routes.go`
- Modify: `internal/application/service/knowledge_base.go`
- Modify: `internal/infrastructure/db/knowledge_base_repository.go`
- Modify: `internal/application/dto/knowledge_base.go`

**Interfaces:**
- `PATCH /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/source-policy` body `{ "on_delete": "keep|remove" }`。
- handler 要求 admin/owner，使用 typed request，不接受整个 `source_config`。
- service 只更新 `source_config.on_delete`，保留 root/cursor/cron/next_sync_at/latch/sync_last_result。
- create 飞书 KB 使用同一个 policy parser；非法值返回 `ErrValidation`，历史缺失按 keep。

- [ ] **Step 1: 写 HTTP/service RED。**

```go
func TestSourcePolicyRejectsUnknownValue(t *testing.T) {
    response := patchSourcePolicy(t, `{"on_delete":"purge"}`)
    require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestSourcePolicyPreservesRuntimeSourceConfig(t *testing.T) {
    env := newSourcePolicyEnv(t, map[string]any{
        "root_token": "wikcn-root", "sync_cursor": "2026-08-07T00:00:00Z",
        "cron": "0 * * * *", "on_delete": "keep",
    })
    require.NoError(t, env.update(SourceDeleteRemove))
    cfg := env.sourceConfig()
    require.Equal(t, "wikcn-root", cfg["root_token"])
    require.Equal(t, "2026-08-07T00:00:00Z", cfg["sync_cursor"])
    require.Equal(t, "remove", cfg["on_delete"])
}
```

- [ ] **Step 2: 运行 RED。**

Run: `go test ./internal/interfaces/http ./internal/application/service ./internal/infrastructure/db -run 'SourcePolicy' -count=1`

Expected: FAIL，因为 source-policy route 和 typed update contract 不存在。

- [ ] **Step 3: 实现局部 JSONB 更新和路由。**

使用 `jsonb_set(source_config, '{on_delete}', to_jsonb(?::text))`，SQL WHERE 必须包含 workspace/id/deleted_at IS NULL；不要把整个 map 从 HTTP 覆盖回数据库。

- [ ] **Step 4: 运行 focused tests。**

Run: `gofmt -w internal/interfaces/http internal/application/service internal/infrastructure/db && go test ./internal/interfaces/http ./internal/application/service ./internal/infrastructure/db -run 'SourcePolicy' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/interfaces/http internal/application/service/knowledge_base.go internal/infrastructure/db/knowledge_base_repository.go internal/application/dto/knowledge_base.go
git commit -m "feat(api): 增加飞书删除策略配置接口"
```

### Task 11: SourceSyncService 全量回归与真实 pipeline 边界测试

**Files:**
- Modify: `internal/application/service/source_sync_test.go`
- Create: `internal/application/service/source_sync_robustness_test.go`
- Modify: `internal/interfaces/worker/source_sync_tasks_test.go`
- Modify: `internal/infrastructure/db/source_sync_store_integration_test.go`
- Create: `internal/infrastructure/db/source_cleanup_store_integration_test.go`

**Interfaces:**
- 验证 source sync 单个节点失败不阻塞其它节点，但结果为 partial；fatal ListTree error 标记 failed。
- 验证 partial 同步返回 nil 给 worker，worker 将通用 Job 标记 `succeeded`，只在 fatal error 时标记 `failed`；旧 payload 缺失 force 字段仍从 DB latch 得到 false。
- 验证 folder 深度删除、keep/remove 恢复、cleanup pending 统计和 workspace 隔离。
- 验证 parse pipeline 仍以新 revision/job payload 进入现有 `document_parse_start`，source sync 不直接调用 parser/embedding/index。

- [ ] **Step 1: 添加端到端行为测试。**

```go
func TestOneNodeFailureContinuesAndWritesPartialResult(t *testing.T) {
    env := newSourceSyncServiceTestEnv(t)
    env.connector.failFetchOnce("bad-doc")
    require.NoError(t, env.syncOnce(snapshotWithDocs("good-doc", "bad-doc")))
    require.Equal(t, "partial", env.syncResult().Status)
    require.NotEmpty(t, env.documentByExternalID("good-doc"))
    require.Empty(t, env.documentByExternalID("bad-doc"))
}

func TestFatalListTreeWritesFailedResult(t *testing.T) {
    env := newSourceSyncServiceTestEnv(t)
    env.connector.fatalListError = sourceport.ErrSourceUnavailable
    require.Error(t, env.syncOnce(sourceport.TreeSnapshot{}))
    require.Equal(t, "failed", env.syncResult().Status)
}
```

- [ ] **Step 2: 运行 integration RED/GREEN。**

Run: `make test-image && go test -tags=integration -p 1 ./internal/application/service ./internal/interfaces/worker ./internal/infrastructure/db -run 'SourceSync|Cleanup|Partial|Fatal' -count=1`

Expected: 所有 robustness 场景 PASS，数据库来自临时容器。

- [ ] **Step 3: 运行完整后端测试。**

Run: `go test ./...`

Expected: PASS；若测试环境无 `LANGHUAN_TEST_DATABASE_DSN`，数据库测试按项目约定 skip 或由测试 helper 拉起容器，不能回退到 config.yaml DSN。

- [ ] **Step 4: 运行静态检查和 diff 检查。**

Run: `gofmt -w . && go vet ./... && git diff --check`

Expected: 无 gofmt/go vet/diff whitespace 错误。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/service internal/interfaces/worker internal/infrastructure/db
git commit -m "test(source): 覆盖飞书同步健壮性回归场景"
```

### Task 12: 更新架构、API、数据库指南与路线图

**Files:**
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/API_ACCESS.md`
- Modify: `docs/DATABASE_GUIDELINES.md`
- Modify: `config.example.yaml`
- Modify: `ROADMAP.md`

**Interfaces:**
- Architecture 说明 `TreeSnapshot.Complete` 删除闸门、stable external Document、force latch、partial result 和 cleanup flow。
- API 文档说明 sync body、source-policy endpoint、权限、202 response、非法 policy 的 400 response。
- DB 指南说明 `content_hash`、folder external_id 唯一索引、source_cleanup KB-only Job、DB/对象存储两阶段清理和测试隔离。
- config 示例包含 `source_sync.max_content_bytes: 52428800`。
- ROADMAP 标记本次 robustness 改进的交付范围，不宣称 sheet/bitable 已支持。

- [ ] **Step 1: 写文档核对清单。**

```text
[ ] ARCHITECTURE 仍说明 force 不创建/激活 Generation
[ ] ARCHITECTURE 说明 incomplete snapshot 不删除且不推进 cursor
[ ] API_ACCESS 说明 force 与 source-policy 的请求/响应/权限
[ ] DATABASE_GUIDELINES 说明 cleanup 不在 DB transaction 内调用 storage
[ ] config.example.yaml 包含 max_content_bytes 默认值
[ ] ROADMAP 没有把 sheet/bitable 写成已交付
```

- [ ] **Step 2: 更新文档并扫描旧语义。**

Run: `rg -n 'skipRemoval|partial JobStatus|彻底清理|清空 sync_cursor|固定 RevisionNo|sheet/bitable.*支持' docs/ARCHITECTURE.md docs/API_ACCESS.md docs/DATABASE_GUIDELINES.md ROADMAP.md`

Expected: 不出现与新 spec 冲突的描述。

- [ ] **Step 3: 运行 Markdown/diff 检查。**

Run: `git diff --check && awk '/^```/{n++} END {if (n % 2 != 0) exit 1}' docs/superpowers/plans/2026-08-07-feishu-sync-robustness-improvements-implementation.md`

Expected: PASS。

- [ ] **Step 4: 提交文档。**

```bash
git add docs/ARCHITECTURE.md docs/API_ACCESS.md docs/DATABASE_GUIDELINES.md config.example.yaml ROADMAP.md
git commit -m "docs(source): 更新飞书同步健壮性合同"
```

## Final Verification Checklist

- [x] `go test ./...` 通过，数据库测试没有连接开发库。
- [x] `make test-image` 成功，integration tests 使用临时 pgvector/zhparser 容器。
- [x] `go vet ./...` 通过。
- [x] `git diff --check` 通过。
- [x] 不完整 snapshot 不删除 Document/folder，且不推进 cursor。
- [x] 完整空 snapshot 可按 keep/remove 删除远端缺失 Document。
- [x] 同 external token 复用 Document ID，revision number 单调递增。
- [x] hash unchanged 跳过；force/retry 创建或复用正确 revision。
- [x] raw key 按 RevisionID 隔离，旧 key 可读写。
- [x] remove 删除 DB 投影并异步 cleanup 外部对象，失败可重试。
- [x] force latch 在 enqueue/consume/finalize 竞态下不丢失。
- [x] source sync Job 只在 fatal error 时 failed，partial 结果仍 succeeded + `sync_last_result.status=partial`。
- [x] 非法 `on_delete` API 返回 400，不覆盖 root/cursor/cron/latch。
- [x] Conventional Commits 提交信息使用中文主体。
