# 琅嬛知识处理与检索数据模型 v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 KnowledgeBase、File/FAQ/Web Document、虚拟文件树、Chunk 与检索存储重建为 Workspace/RLS-ready 的不可变事实层和单活双缓冲检索投影，并交付“FAQ 索引问题、返回回答”的混合检索闭环。

**Architecture:** `documents → document_revisions → document_chunk_sets → chunks → chunk_revisions` 保存通用可追溯事实，FAQ 问题/回答由 Revision 子表整体版本化，File Document 由独立 `file_tree_nodes` 组织。`knowledge_base_index_generations → retrieval_entries` 保存可重建投影，并显式分离 `search_content` 与返回 `content`；所有租户表直接保存 `workspace_id`，发布动作通过 tx-bound Repository 原子切换指针，RLS policy 在后续独立迁移启用。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL、pgvector `halfvec`/HNSW、PostgreSQL FTS/GIN、asynq、Redis、CloudWeGo Eino Embedding、golang-migrate、React 19、TypeScript 7、TanStack Query、Biome、Vitest。

## Global Constraints

- 设计合同以 `docs/superpowers/specs/2026-07-30-knowledge-retrieval-schema-v2-design.md` 为唯一来源；不得重新引入 `chunk_embeddings` 或 `chunk_keywords`。
- 严格 Red → Green → Refactor；每项生产行为先运行能证明缺失行为的失败测试。
- `users / sessions / workspaces / workspace_memberships / workspace_invitations / workspace_api_tokens / model_providers / models` 的结构、数据与授权行为必须保留。
- Migration 是开发期破坏性知识数据重建，不回填旧 KB/Document/Chunk/Vector 数据。
- 每张租户表必须有 `workspace_id uuid NOT NULL` 和 `UNIQUE (workspace_id, id)`；复合外键阻止跨 Workspace、KB、Document lineage。
- 本计划不执行 `ENABLE ROW LEVEL SECURITY`；业务读写进入 Workspace transaction，并执行 transaction-local `set_config('app.workspace_id', ..., true)`。
- Application/domain 不依赖 `*gorm.DB`、Gin、pgvector 或 Eino 类型；事务接口定义在使用方。
- DocumentRevision 表达文件/解析版本；ChunkSet 表达分块产物；重新分块不创建 DocumentRevision。
- `documents.kind` 与 `document_revisions.kind` 固定为 `file/faq/web` 且必须相等；改变 kind 创建新 Document。
- `file_type` 只属于 DocumentRevision：File 必填，FAQ/Web 必须为空；`source_type` 只表达 upload/api/crawler/sync 等采集渠道。
- Web 只落稳定规范化 URL 与 Revision 合同，本计划不实现 crawler。
- FAQ Revision 恰好一个非空 answer、至少一个有序非空 question；修改时提交完整新 Revision。
- FAQ 固定一个 `strategy=faq` Chunk；Embedding/FTS 只使用问题组成的 `search_content`，返回 `content=answer`，答案不得被索引。
- File Tree 独立于 Revision、对象存储和 Generation；rename/move 不递增 `content_version`，不触发 reindex。
- 每个 KB 同事务创建唯一 root；每个 File Document 恰好一个 file node；FAQ/Web 不进入树。
- Chunk 编辑追加 Revision，不覆盖 `source_content`；并发编辑使用 `base_revision_id`。
- KB 生效模型、分块和检索配置来自 active Generation；一个 KB 同时最多一个 `building` Generation。
- RetrievalEntry 同一行保存 `search_content`、返回 `content`、FTS 与向量；只有 active Generation 的 `published` 行可查询，不保存权威 `document_title`。
- 维度只允许 798、1024、2048、3584；查询表达式与 HNSW 部分索引必须完全一致。
- 配置指纹使用确定性 JSON + SHA-256，不含凭证、显示字段、时间和统计。
- member 可读取、上传和 search；Chunk 编辑/启停、Generation 创建/激活要求 admin/owner。
- 失败 staging 默认保留 24 小时，retired Generation 默认保留 7 天，期限来自 YAML。
- 所有 I/O 透传 `context.Context`；日志不记录正文、向量、凭证或第三方原始响应。
- 生产文件按职责拆分，不创建 `utils.go / helpers.go / common.go`。
- 每个后端任务 GREEN 后运行 `go test ./... -count=1`；数据库任务还运行 integration suite。
- 最终合并前运行 Go、Integration、Vet、Web check/test/build 与 `git diff --check`。

## File Responsibility Map

- Domain values: `document_kind.go`、`file_tree_node_type.go`、`document_revision_status.go`、`chunk_status.go`、`index_generation_status.go`、`retrieval_entry_state.go`。
- Domain models: `document_revision.go`、`faq_revision.go`、`file_tree_node.go`、`document_chunk_set.go`、`chunk_revision.go`、`index_generation.go`、`retrieval_entry.go`，并收紧现有 KB/Document/Chunk/Job。
- DB rows: `knowledge_rows.go`、`document_rows.go`、`faq_rows.go`、`file_tree_rows.go`、`chunk_rows.go`、`retrieval_rows.go`、`job_rows.go`。
- DB transaction: `workspace_tx.go`；每个 Repository 只持有注入的 `*gorm.DB`。
- Pipeline: File/Web Parse 保存 DocumentRevision；standard/FAQ Chunk builder 创建 ChunkSet；Index 从 `search_content` 生成并发布 RetrievalEntry。
- Services: `file_tree.go`、`faq_document.go`、`chunk_revision.go`、`index_generation.go`、`search.go`。
- Protocol: typed worker payload，以及 Chunk/Generation/Search REST handlers。

---

### Task 1: 添加破坏性 v2 Migration 与数据库合同测试（已完成）

**Files:**
- Create: `internal/infrastructure/migrate/migrations/000005_knowledge_retrieval_v2.up.sql`
- Create: `internal/infrastructure/migrate/migrations/000005_knowledge_retrieval_v2.down.sql`
- Modify: `internal/infrastructure/migrate/migrate_test.go`
- Create: `internal/infrastructure/db/knowledge_schema_v2_integration_test.go`

**Interfaces:**
- Consumes: migrations 1-4 and existing Workspace/Auth/Model tables.
- Produces: complete v2 tables, composite FKs, CHECKs, GIN and HNSW indexes.

- [x] **Step 1: 写 migration source RED**

```go
func TestMigrationSourceVersion5DefinesKnowledgeRetrievalV2(t *testing.T) {
    src := newSource(t)
    up, _, err := src.ReadUp(5)
    if err != nil { t.Fatal(err) }
    body := readAll(t, up)
    for _, fragment := range []string{
        "CREATE TABLE knowledge_bases",
        "CREATE TABLE file_tree_nodes",
        "CREATE TABLE document_revisions",
        "CREATE TABLE faq_revision_contents",
        "CREATE TABLE faq_revision_questions",
        "CREATE TABLE document_chunk_sets",
        "CREATE TABLE chunk_revisions",
        "CREATE TABLE knowledge_base_index_generations",
        "CREATE TABLE retrieval_entries",
        "workspace_id uuid NOT NULL",
        "kind text NOT NULL",
        "search_content text NOT NULL",
        "fts_document tsvector",
        "embedding halfvec",
        "idx_retrieval_entries_hnsw_1024",
        "idx_retrieval_entries_fts",
    } {
        if !strings.Contains(body, fragment) {
            t.Fatalf("version 5 migration missing %q", fragment)
        }
    }
    for _, forbidden := range []string{
        "CREATE TABLE chunk_embeddings",
        "CREATE TABLE chunk_keywords",
        "ENABLE ROW LEVEL SECURITY",
    } {
        if strings.Contains(body, forbidden) {
            t.Fatalf("version 5 migration contains forbidden %q", forbidden)
        }
    }
}
```

- [x] **Step 2: 运行 source RED**

Run: `go test ./internal/infrastructure/migrate -run Version5 -count=1`

Expected: FAIL because migration 5 does not exist.

- [x] **Step 3: 写真实 PostgreSQL RED**

Seed Workspace/User/Membership/Provider/Model at version 4, migrate to 5, assert those rows remain, then attempt a cross-tenant Document insert:

```go
err := db.WithContext(ctx).Exec(
    "INSERT INTO documents "+
        "(id, workspace_id, knowledge_base_id, kind, title, source_type, source_uri, status) "+
        "VALUES (?, ?, ?, 'web', 'cross tenant', 'crawler', 'https://example.com/', 'pending')",
    uuid.New(), wsB, kbA,
).Error
if !errors.Is(err, gorm.ErrForeignKeyViolated) {
    t.Fatalf("cross tenant insert error = %v, want FK violation", err)
}
```

- [x] **Step 4: 运行 Integration RED**

Run: `go test -tags=integration ./internal/infrastructure/db -run KnowledgeSchemaV2 -count=1`

Expected: FAIL because v2 tables are absent.

- [x] **Step 5: 实现 Up/Down DDL**

Up drops only old knowledge tables, in this order:

```sql
DROP TABLE IF EXISTS chunk_keywords CASCADE;
DROP TABLE IF EXISTS chunk_embeddings CASCADE;
DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS document_assets CASCADE;
DROP TABLE IF EXISTS chunks CASCADE;
DROP TABLE IF EXISTS documents CASCADE;
DROP TABLE IF EXISTS knowledge_bases CASCADE;
```

Then create `knowledge_bases`, `knowledge_base_index_generations`, `documents`, `document_revisions`, `faq_revision_contents`, `faq_revision_questions`, `document_chunk_sets`, `chunks`, `chunk_revisions`, `file_tree_nodes`, `document_assets`, `jobs` and `retrieval_entries` with every column and constraint from the spec. Add deferred active Generation, file-tree root, Document Revision and Chunk Revision pointer FKs only after both sides exist:

```sql
FOREIGN KEY (workspace_id, id, active_index_generation_id)
REFERENCES knowledge_base_index_generations
  (workspace_id, knowledge_base_id, id)
DEFERRABLE INITIALLY DEFERRED
```

Use the analogous full lineage for Document active Revision and Chunk active Revision. Down drops v2 and recreates the empty v1 knowledge schema after migration 4; it never drops Auth/Workspace/Model tables.

Add kind-aware keys and constraints explicitly:

```sql
UNIQUE (workspace_id, knowledge_base_id, id, kind);
FOREIGN KEY (workspace_id, knowledge_base_id, document_id, kind)
  REFERENCES documents (workspace_id, knowledge_base_id, id, kind);
CHECK (
  (kind = 'file' AND file_type IS NOT NULL AND original_filename IS NOT NULL AND raw_storage_key IS NOT NULL)
  OR (kind = 'faq' AND file_type IS NULL AND original_filename IS NULL AND raw_storage_key IS NULL
      AND normalized_markdown IS NULL AND parse_manifest IS NULL)
  OR (kind = 'web' AND file_type IS NULL AND original_filename IS NULL)
);
```

Use deferred constraint triggers to reject a committed FAQ Revision without exactly one content row/at least one question and a committed File Document without exactly one file node. Add partial unique indexes for one root per KB, one node per File Document, active Web URL identity, FAQ question sequence/normalized text, and case-insensitive shared file/folder siblings.

- [x] **Step 6: 添加检索索引**

```sql
CREATE INDEX idx_retrieval_entries_fts
ON retrieval_entries USING gin (fts_document)
WHERE state = 'published';

CREATE INDEX idx_retrieval_entries_hnsw_1024
ON retrieval_entries
USING hnsw ((embedding::halfvec(1024)) halfvec_cosine_ops)
WITH (m = 16, ef_construction = 64)
WHERE dimension = 1024 AND state = 'published';
```

Repeat HNSW for 798/2048/3584 and add B-tree Workspace/KB/Generation/state indexes.

- [x] **Step 7: 扩充真实数据库约束矩阵**

Prove all of these fail with the expected PostgreSQL constraint class: Document/Revision kind mismatch, FAQ row on File Revision, zero-question FAQ at commit, file node pointing to FAQ/Web, cross-KB parent, second root, second node for one File Document, `folder`/`file` names differing only by case under one parent, and duplicate active normalized Web URL. Also prove Auth/Workspace/Model seed rows survive `up/down/up`.

- [x] **Step 8: 运行 GREEN 并提交**

```bash
go test ./internal/infrastructure/migrate -count=1
go test -tags=integration ./internal/infrastructure/db -run KnowledgeSchemaV2 -count=1
git diff --check
git add internal/infrastructure/migrate internal/infrastructure/db/knowledge_schema_v2_integration_test.go
git commit -m "feat: 重建知识处理数据库结构"
```

### Task 2: 定义版本、Generation 与投影领域合同（已完成）

**Files:**
- Create: `internal/domain/value/document_kind.go`
- Create: `internal/domain/value/file_tree_node_type.go`
- Create: `internal/domain/value/web_source_uri.go`
- Create: `internal/domain/value/document_revision_status.go`
- Create: `internal/domain/value/chunk_status.go`
- Create: `internal/domain/value/index_generation_status.go`
- Create: `internal/domain/value/retrieval_entry_state.go`
- Modify: `internal/domain/model/knowledge_base.go`
- Modify: `internal/domain/model/document.go`
- Create: `internal/domain/model/document_revision.go`
- Create: `internal/domain/model/faq_revision.go`
- Create: `internal/domain/model/file_tree_node.go`
- Create: `internal/domain/model/document_chunk_set.go`
- Modify: `internal/domain/model/chunk.go`
- Create: `internal/domain/model/chunk_revision.go`
- Create: `internal/domain/model/index_generation.go`
- Create: `internal/domain/model/retrieval_entry.go`
- Modify: `internal/domain/model/job.go`
- Modify: `internal/domain/errors/errors.go`
- Create: `internal/application/service/config_hash.go`
- Test: matching `*_test.go` files

**Interfaces:**
- Produces typed states/reasons and v2 structs.
- Produces `CanonicalConfigHash(map[string]any) (string, error)` and stable conflict errors.

- [x] **Step 1: 写领域 RED**

```go
func TestNewUserChunkRevisionRequiresBaseAndEditor(t *testing.T) {
    _, err := NewChunkRevision(NewChunkRevisionInput{
        WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
        DocumentID: uuid.New(), ChunkID: uuid.New(),
        RevisionNo: 2, Content: "edited", EmbeddingContent: "edited",
        Enabled: true, EditSource: value.ChunkEditSourceUser,
    })
    if !errors.Is(err, domainerrors.ErrValidation) {
        t.Fatalf("error = %v, want validation", err)
    }
}
```

Also test Generation activation rejects mismatched content version, a user Revision requires a non-nil base, Document/Revision kind cannot differ, FAQ requires at least one unique normalized question and a non-empty answer, only File can construct a file node, and Web URL normalization removes fragments/default ports while preserving query order.

```go
func TestNewFAQRevisionRejectsAnswerOnly(t *testing.T) {
    _, err := NewFAQRevision(NewFAQRevisionInput{
        DocumentRevision: validFAQDocumentRevision(),
        Answer: "answer",
        Questions: nil,
    })
    if !errors.Is(err, domainerrors.ErrValidation) {
        t.Fatalf("error = %v, want validation", err)
    }
}
```

- [x] **Step 2: 写 config hash RED**

```go
func TestCanonicalConfigHashIgnoresMapOrder(t *testing.T) {
    a := map[string]any{"chunk_size": 512, "nested": map[string]any{"b": 2, "a": 1}}
    b := map[string]any{"nested": map[string]any{"a": 1, "b": 2}, "chunk_size": 512}
    hashA, _ := CanonicalConfigHash(a)
    hashB, _ := CanonicalConfigHash(b)
    if hashA != hashB || len(hashA) != 64 { t.Fatalf("%q %q", hashA, hashB) }
}
```

- [x] **Step 3: 运行 RED**

Run: `go test ./internal/domain/... ./internal/application/service -run 'DocumentKind|FAQRevision|FileTree|Revision|Generation|CanonicalConfigHash' -count=1`

Expected: FAIL with missing types/functions.

- [x] **Step 4: 实现 typed models**

Use explicit string constants for every CHECK value. Constructors validate non-nil lineage IDs, revision/version ranges, kind-specific fields, FAQ question normalization/sequence, system/user editor pairing, enabled non-empty content and supported dimension. `Document.Kind` has no mutator; File title updates remain package-private to the tree service path. `NormalizeWebSourceURI` uses `net/url`: strip fragment, lowercase scheme/host, remove default ports, normalize empty path to `/`, preserve encoded path and query pair order, and perform no network request. Add `ErrRevisionConflict`, `ErrGenerationStale`, `ErrManualEditConfirmationRequired`, `ErrGenerationNotReady`, `ErrFAQChunkImmutable`, `ErrFileTreeCycle`, `ErrFileTreeNotEmpty` and `ErrFileTreeNameConflict`.

- [x] **Step 5: 实现 canonical hash**

```go
func CanonicalConfigHash(value map[string]any) (string, error) {
    encoded, err := canonicalJSON(value)
    if err != nil { return "", fmt.Errorf("编码配置指纹失败: %w", err) }
    sum := sha256.Sum256(encoded)
    return hex.EncodeToString(sum[:]), nil
}
```

`canonicalJSON` recursively sorts object keys, preserves `json.Number`, rejects NaN/Inf and accepts only JSON values.

- [x] **Step 6: 运行 GREEN 并提交**

```bash
gofmt -w internal/domain internal/application/service/config_hash*
go test ./internal/domain/... ./internal/application/service -run 'DocumentKind|FAQRevision|FileTree|Revision|Generation|CanonicalConfigHash' -count=1
go test ./... -count=1
git add internal/domain internal/application/service/config_hash*
git commit -m "feat: 定义知识版本与索引代次合同"
```

### Task 3: 拆分 Row、Codec 与 v2 映射（已完成）

**Files:**
- Create: `internal/infrastructure/db/knowledge_rows.go`
- Create: `internal/infrastructure/db/document_rows.go`
- Create: `internal/infrastructure/db/faq_rows.go`
- Create: `internal/infrastructure/db/file_tree_rows.go`
- Create: `internal/infrastructure/db/chunk_rows.go`
- Create: `internal/infrastructure/db/retrieval_rows.go`
- Create: `internal/infrastructure/db/job_rows.go`
- Create: `internal/infrastructure/db/knowledge_v2_codec.go`
- Create: `internal/infrastructure/db/knowledge_v2_codec_test.go`
- Modify: `internal/infrastructure/db/models.go`
- Modify: `internal/infrastructure/db/models_test.go`

**Interfaces:**
- Consumes Task 2 domain types and `JSONMap`.
- Produces focused GORM Rows and lossless `toRow/fromRow` codecs.

- [x] **Step 1: 写 Row round-trip RED**

Table-drive each Row. Include all three Document kinds, FAQ answer/questions in sequence, root/folder/file nodes, kind-preserving Revision, ChunkRevision user editor, SourceAnchor, context header, active pointer and nil metadata normalization.

- [x] **Step 2: 运行 RED**

Run: `go test ./internal/infrastructure/db -run 'V2|RowRoundTrip' -count=1`

Expected: FAIL because new Rows/codecs are absent.

- [x] **Step 3: 实现职责化 Rows**

```go
type RetrievalEntryRow struct {
    ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
    WorkspaceID        uuid.UUID `gorm:"type:uuid;not null"`
    KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null"`
    IndexGenerationID  uuid.UUID `gorm:"type:uuid;not null"`
    DocumentID         uuid.UUID `gorm:"type:uuid;not null"`
    DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null"`
    ChunkSetID         uuid.UUID `gorm:"type:uuid;not null"`
    ChunkID            uuid.UUID `gorm:"type:uuid;not null"`
    ChunkRevisionID    uuid.UUID `gorm:"type:uuid;not null"`
    State              string
    SearchContent      string
    Content            string
    SourceAnchor       JSONMap `gorm:"type:jsonb"`
    Metadata           JSONMap `gorm:"type:jsonb"`
    FTSDocument        string `gorm:"column:fts_document;type:tsvector"`
    Embedding          *string `gorm:"type:halfvec"`
    Dimension          *int
    CreatedAt          time.Time
    PublishedAt        *time.Time
    RetiredAt          *time.Time
}
```

Remove only knowledge Rows from `models.go`; leave Auth/Model organization unchanged. `FAQRevisionContentRow` uses `DocumentRevisionID` as its primary key, question Rows retain explicit sequence, and `FileTreeNodeRow` never embeds storage/version fields. Reuse typed ParseManifest/SourceAnchor codecs and reject unknown config fields.

- [x] **Step 4: 运行 GREEN 并提交**

```bash
gofmt -w internal/infrastructure/db
go test ./internal/infrastructure/db -run 'V2|RowRoundTrip|Codec|Models' -count=1
go test ./... -count=1
git add internal/infrastructure/db
git commit -m "refactor: 拆分知识持久化行模型"
```

### Task 4: 建立 Workspace Transaction Boundary（已完成）

**Files:**
- Create: `internal/infrastructure/db/workspace_tx.go`
- Create: `internal/infrastructure/db/workspace_tx_integration_test.go`
- Create: `internal/application/service/workspace_store.go`
- Create: `internal/application/service/workspace_store_test.go`

**Interfaces:**
- Produces Infrastructure `WorkspaceTxRunner.WithinWorkspace`.
- Produces minimal application stores for KB/root creation, File ingest, File Tree, FAQ revision, publish, Chunk edit and Generation; none exposes GORM.

- [x] **Step 1: 写 tenant-local RED**

Inside the callback query `current_setting('app.workspace_id', true)` and assert the supplied UUID; return a sentinel error, then query outside and assert the value did not leak.

- [x] **Step 2: 运行 RED**

Run: `go test -tags=integration ./internal/infrastructure/db -run WorkspaceTxRunner -count=1`

Expected: FAIL because runner is undefined.

- [x] **Step 3: 实现 runner**

```go
func (r *WorkspaceTxRunner) WithinWorkspace(
    ctx context.Context, workspaceID uuid.UUID, fn func(*gorm.DB) error,
) error {
    if workspaceID == uuid.Nil {
        return fmt.Errorf("%w: workspace_id 不能为空", domainerrors.ErrValidation)
    }
    return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := tx.Exec(
            "SELECT set_config('app.workspace_id', ?, true)",
            workspaceID.String(),
        ).Error; err != nil {
            return fmt.Errorf("设置 Workspace 数据库上下文失败: %w", err)
        }
        return fn(tx.WithContext(ctx))
    })
}
```

- [x] **Step 4: 定义使用方接口**

```go
type DocumentIngestTx interface {
    GetKnowledgeBase(context.Context, uuid.UUID) (*model.KnowledgeBase, error)
    FindReusableRevision(context.Context, uuid.UUID, string, int) (*model.Document, *model.DocumentRevision, *model.Job, error)
    GetFileTreeNodeForUpdate(context.Context, uuid.UUID) (*model.FileTreeNode, error)
    CreateFileDocumentNodeRevisionAndJob(context.Context, *model.Document, *model.FileTreeNode, *model.DocumentRevision, *model.Job) error
}
type DocumentIngestStore interface {
    WithinWorkspace(context.Context, uuid.UUID, func(context.Context, DocumentIngestTx) error) error
}
```

Define separate minimal KB/root, file-tree, FAQ, publish/edit/generation stores. File-tree move owns a typed `WouldCreateCycle(ctx, nodeID, newParentID) (bool, error)` query implemented with recursive CTE; FAQ create owns a single aggregate write for Document/Revision/content/questions. Infrastructure adapters construct tx-bound Repositories inside the runner; no context-hidden DB handle.

- [x] **Step 5: 运行 GREEN 并提交**

```bash
gofmt -w internal/infrastructure/db/workspace_tx* internal/application/service/workspace_store*
go test -tags=integration ./internal/infrastructure/db -run WorkspaceTxRunner -count=1
go test ./... -count=1
git add internal/infrastructure/db/workspace_tx* internal/application/service/workspace_store*
git commit -m "feat: 建立 Workspace 事务边界"
```

### Task 5: 使用初始 Generation 与文件树 Root 创建 KnowledgeBase（已完成）

**Files:**
- Modify: `internal/domain/model/knowledge_base.go`
- Modify: `internal/application/dto/knowledge_base.go`
- Modify: `internal/application/service/knowledge_base.go`
- Modify: `internal/application/service/knowledge_base_model_binder.go`
- Create: `internal/application/service/knowledge_base_generation.go`
- Modify: `internal/infrastructure/db/knowledge_base_repository.go`
- Create: `internal/infrastructure/db/index_generation_repository.go`
- Create: `internal/infrastructure/db/file_tree_repository.go`
- Create: `internal/infrastructure/db/knowledge_base_generation_integration_test.go`
- Modify: `internal/interfaces/http/knowledge_base.go`
- Test: matching service/HTTP tests

**Interfaces:**
- Produces atomic KB + unique root + empty ready Generation creation.
- Preserves current REST/Web request and response compatibility.

- [x] **Step 1: 写 RED**

Create a KB and assert non-nil active Generation, non-nil file-tree root, status ready, zero content/indexed version, both pointer equalities, root `parent_id/document_id` null and no second root can commit.

- [x] **Step 2: 运行 RED**

Run: `go test ./internal/application/service -run CreateKnowledgeBaseCreatesActiveEmptyGeneration -count=1`

Expected: FAIL because KB stores model/config directly.

- [x] **Step 3: 实现 create transaction**

Resolve visible active Model/Provider, construct immutable model/chunk/retrieval snapshots, pre-generate root and Generation IDs, insert KB already carrying both target IDs, then insert the root and ready zero-entry Generation before commit. Deferred FKs allow this insert order while `file_tree_root_id` remains non-null; a deferred trigger also verifies the target node type is `root`.

```go
func DefaultRetrievalConfig() RetrievalConfig {
    return RetrievalConfig{
        FTSConfig: "simple", VectorTopK: 30,
        KeywordTopK: 30, FinalTopK: 10, RRFK: 60,
    }
}
```

KB DTO continues returning `embedding_model` and `chunking_config`, resolved from active Generation.

- [x] **Step 4: 运行 GREEN 并提交**

```bash
gofmt -w internal/domain/model/knowledge_base.go internal/application internal/infrastructure/db internal/interfaces/http/knowledge_base.go
go test ./internal/application/service ./internal/interfaces/http -run KnowledgeBase -count=1
go test -tags=integration ./internal/infrastructure/db -run KnowledgeBaseGeneration -count=1
go test ./... -count=1
git add internal/domain/model/knowledge_base.go internal/application internal/infrastructure/db internal/interfaces/http/knowledge_base.go
git commit -m "feat: 使用索引代次创建知识库"
```

### Task 6: 重写 File 导入为 Document + Node + Revision + Job（已完成）

**Files:**
- Modify: `internal/application/service/document_ingest.go`
- Modify: `internal/application/service/document_ingest_test.go`
- Modify: `internal/application/service/document.go`
- Modify: `internal/application/dto/document.go`
- Modify: `internal/application/dto/job.go`
- Modify: `internal/infrastructure/db/document_repository.go`
- Create: `internal/infrastructure/db/document_revision_repository.go`
- Modify: `internal/infrastructure/db/file_tree_repository.go`
- Modify: `internal/infrastructure/db/job_repository.go`
- Modify: `internal/infrastructure/db/document_repository_integration_test.go`
- Modify: `internal/interfaces/http/document.go`

**Interfaces:**
- Produces atomic stable File Document + one file node + revision 1 + parse Job.
- Queue payload becomes `{workspace_id, knowledge_base_id, document_id, document_revision_id, job_id}`.
- HTTP multipart accepts optional `parent_node_id`, optional `node_name`, and existing `dedupe`.

- [x] **Step 1: 写 ingest RED**

Assert raw fields/original filename live on Revision, unparsed Document has no active revision, omitted parent uses KB root, omitted node name uses normalized original filename, and queue payload includes Workspace/Revision. Add conflict cases for non-folder parent, cross-KB parent and case-insensitive sibling collision; assert no Document/Revision/Job is committed.

- [x] **Step 2: 运行 RED**

Run: `go test ./internal/application/service -run IngestCreatesDocumentRevisionAndWorkspaceTask -count=1`

Expected: FAIL because raw fields remain on Document.

- [x] **Step 3: 实现 v2 ingest**

```go
document := model.NewDocument(wsID, kbID, value.DocumentKindFile, nodeName, sourceType, "")
revision := model.NewDocumentRevision(model.NewDocumentRevisionInput{
    WorkspaceID: wsID, KnowledgeBaseID: kbID, DocumentID: document.ID,
    Kind: value.DocumentKindFile,
    RevisionNo: 1, Reason: value.DocumentRevisionReasonIngest,
    OriginalFilename: uploadFilename, FileType: fileType, ContentType: contentType,
    RawStorageKey: rawObject.Key, SHA256: hash, SizeBytes: actualSize,
    ProcessingVersion: model.CurrentProcessingVersion,
    Status: value.DocumentRevisionPending,
})
```

After raw object storage succeeds, enter one Workspace transaction: lock the requested/default root node, require root/folder, normalize/validate `node_name`, create the File Document, file node, Revision and Job, then enqueue only after commit. On transaction failure schedule raw-object compensation. Dedupe searches ready File Revisions within Workspace + KB + SHA + processing version; `dedupe=true` returns the existing Document and node instead of creating a second tree entry. Document API returns kind, current node name/path and active Revision summary but never `raw_storage_key`.

- [x] **Step 4: 运行 GREEN 并提交**

```bash
gofmt -w internal/application internal/infrastructure/db internal/interfaces/http
go test ./internal/application/service ./internal/interfaces/http -run 'Ingest|Document' -count=1
go test -tags=integration ./internal/infrastructure/db -run Document -count=1
go test ./... -count=1
git add internal/application internal/infrastructure/db internal/interfaces/http
git commit -m "feat: 使用不可变文档版本导入"
```

### Task 6A: 实现 File Tree 查询、目录 CRUD、重命名与移动（已完成）

**Files:**
- Create: `internal/application/dto/file_tree.go`
- Create: `internal/application/service/file_tree.go`
- Create: `internal/application/service/file_tree_test.go`
- Modify: `internal/infrastructure/db/file_tree_repository.go`
- Create: `internal/infrastructure/db/file_tree_repository_integration_test.go`
- Create: `internal/interfaces/http/file_tree_handler.go`
- Create: `internal/interfaces/http/file_tree_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/errors.go`

**Interfaces:**
- Produces tree listing plus folder create/rename/move/delete operations scoped by Workspace + KB.
- File rename updates `file_tree_nodes.name` and `documents.title` in the same transaction.
- Tree-only mutations never call queue/index services and never update `knowledge_bases.content_version`.

- [x] **Step 1: 写 service/HTTP RED**

Table-drive: create nested folders; folder/file share the same case-insensitive sibling namespace; move folder into itself/descendant returns `ErrFileTreeCycle`; delete non-empty folder returns `ErrFileTreeNotEmpty`; file rename updates returned Document title; move/rename preserves Revision ID, raw storage key, content version and active Generation.

```go
func TestMoveFolderRejectsDescendant(t *testing.T) {
    err := service.Move(ctx, MoveFileTreeNodeInput{
        WorkspaceID: wsID, KnowledgeBaseID: kbID,
        NodeID: parentID, NewParentID: childID,
    })
    if !errors.Is(err, domainerrors.ErrFileTreeCycle) {
        t.Fatalf("error = %v, want file tree cycle", err)
    }
}
```

- [x] **Step 2: 运行 RED**

```bash
go test ./internal/application/service ./internal/interfaces/http -run FileTree -count=1
go test -tags=integration ./internal/infrastructure/db -run FileTree -count=1
```

Expected: FAIL because the service, routes and recursive CTE store do not exist.

- [x] **Step 3: 实现 Repository 与事务规则**

List nodes by explicit `(workspace_id, knowledge_base_id)` and build the response tree in application code. For move, lock target and both parents, require target parents be root/folder, and run:

```sql
WITH RECURSIVE descendants AS (
  SELECT id FROM file_tree_nodes
  WHERE workspace_id = ? AND knowledge_base_id = ? AND id = ?
  UNION ALL
  SELECT child.id
  FROM file_tree_nodes child
  JOIN descendants d ON child.parent_id = d.id
  WHERE child.workspace_id = ? AND child.knowledge_base_id = ?
)
SELECT EXISTS (SELECT 1 FROM descendants WHERE id = ?);
```

Rely on the sibling unique index for the final race-safe conflict check and map PostgreSQL unique violations to `ErrFileTreeNameConflict`. Delete checks child existence while holding the folder lock. File rename updates node + Document title; folder rename only updates node.

- [x] **Step 4: 实现 HTTP 合同**

Expose exact routes:

```text
GET    /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree
POST   /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree/folders
PATCH  /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree/nodes/:node_id
DELETE /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/file-tree/nodes/:node_id
```

PATCH accepts optional `name` and `parent_id`, with at least one required. Map name conflict/cycle/non-empty to distinct HTTP 409 codes; cross-Workspace/KB remains 404. Return node `id/parent_id/node_type/name/document_id` and current tree path; never return object-storage keys.

- [x] **Step 5: 运行 GREEN 并提交**

```bash
gofmt -w internal/application internal/infrastructure/db internal/interfaces/http
go test ./internal/application/service ./internal/interfaces/http -run FileTree -count=1
go test -tags=integration ./internal/infrastructure/db -run FileTree -count=1
go test ./... -count=1
git add internal/application internal/infrastructure/db internal/interfaces/http
git commit -m "feat: 支持知识库文件树"
```

### Task 7: 将 File/Web 解析和标准分块落到 Revision 与 ChunkSet（已完成）

**Files:**
- Modify: `internal/application/pipeline/document_pipeline.go`
- Modify: `internal/application/pipeline/document_pipeline_test.go`
- Modify: `internal/application/pipeline/parse_stage.go`
- Modify: `internal/application/pipeline/chunk_stage.go`
- Modify: `internal/application/pipeline/chunker.go`
- Modify: `internal/application/pipeline/chunker_test.go`
- Create: `internal/infrastructure/db/chunk_set_repository.go`
- Modify: `internal/infrastructure/db/chunk_repository.go`
- Create: `internal/infrastructure/db/chunk_revision_repository.go`
- Create: `internal/infrastructure/db/chunk_set_repository_integration_test.go`

**Interfaces:**
- Produces `RunParse(ctx, workspaceID, revisionID)` and `RunChunk(ctx, workspaceID, revisionID, generationID) (uuid.UUID, error)`.
- Replaces mutable `SaveParseResult(documentID)` and `ReplaceDocumentChunks(documentID)`.
- Rejects FAQ in parser/standard chunker; FAQ uses Task 7A.

- [x] **Step 1: 写 RED**

ParseStage test asserts it completes a File/Web DocumentRevision without activating Document. ChunkStage test runs twice and asserts the same `strategy=standard` ChunkSet ID and one system revision per Chunk. Passing FAQ returns a typed validation error before raw storage/parser calls.

- [x] **Step 2: 运行 RED**

Run: `go test ./internal/application/pipeline -run 'CompletesRevision|IdempotentSet' -count=1`

Expected: FAIL because stages still mutate Document and replace its Chunks.

- [x] **Step 3: 实现 Revision parse**

Load `workspace_id + revision_id`, open `revision.RawStorageKey`, parse and validate manifest, then mark the same revision ready. A retry of a ready revision returns success without reopening raw storage.

- [x] **Step 4: 实现 ChunkSet build**

```go
type ChunkInput struct {
    WorkspaceID        uuid.UUID
    KnowledgeBaseID    uuid.UUID
    DocumentID         uuid.UUID
    DocumentRevisionID uuid.UUID
    ChunkSetID         uuid.UUID
    Kind               value.DocumentKind
    Title              string
    Markdown           string
    Manifest           model.ParseManifest
}
```

Hash typed ChunkingConfig with `strategy=standard`; lock/create the unique ChunkSet; transactionally recreate only a building/failed set; batch insert Chunks and system revision 1; set Chunk active pointers and exact count; mark set ready.

- [x] **Step 5: 运行 GREEN 并提交**

```bash
gofmt -w internal/application/pipeline internal/infrastructure/db
go test ./internal/application/pipeline -count=1
go test -tags=integration ./internal/infrastructure/db -run ChunkSet -count=1
go test ./... -count=1
git add internal/application/pipeline internal/infrastructure/db
git commit -m "feat: 按解析版本生成分块集合"
```

### Task 7A: 实现 FAQ 完整 Revision 与固定单 Chunk 流水线（已完成）

**Files:**
- Create: `internal/application/dto/faq_document.go`
- Create: `internal/application/service/faq_document.go`
- Create: `internal/application/service/faq_document_test.go`
- Create: `internal/application/pipeline/faq_chunk_stage.go`
- Create: `internal/application/pipeline/faq_chunk_stage_test.go`
- Create: `internal/infrastructure/db/faq_repository.go`
- Create: `internal/infrastructure/db/faq_repository_integration_test.go`
- Create: `internal/interfaces/http/faq_document_handler.go`
- Create: `internal/interfaces/http/faq_document_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/worker/document_tasks.go`

**Interfaces:**
- Produces FAQ create/get/update APIs where update requires `base_revision_id` and supplies the complete question array plus answer.
- Produces `BuildFAQChunkSet(ctx, workspaceID, revisionID) (uuid.UUID, error)` without parser or normal KB chunking config.
- Produces exactly one `strategy=faq` Chunk and one system ChunkRevision per FAQ Revision.

- [x] **Step 1: 写 aggregate/service RED**

Table-drive empty answer, zero questions, whitespace questions, duplicate normalized questions, non-contiguous sequence and stale base Revision. Assert create/update writes one answer row and all questions atomically; a failed update leaves the old active Revision unchanged.

```go
func TestUpdateFAQCreatesCompleteRevision(t *testing.T) {
    got, err := service.Update(ctx, UpdateFAQInput{
        WorkspaceID: wsID, KnowledgeBaseID: kbID, DocumentID: documentID,
        BaseRevisionID: activeRevisionID,
        Questions: []string{"如何退款？", "退款流程是什么？"},
        Answer: "请在订单页申请退款。",
    })
    if err != nil { t.Fatal(err) }
    if got.RevisionNo != 2 || len(got.Questions) != 2 { t.Fatalf("got %#v", got) }
}
```

- [x] **Step 2: 运行 service RED**

Run: `go test ./internal/application/service -run FAQ -count=1`

Expected: FAIL because FAQ aggregate/store are absent.

- [x] **Step 3: 实现 FAQ 事务写入**

Normalize each question with Unicode whitespace trim/collapse and project-defined case folding, preserve submitted display text, assign sequence from array order, and reject duplicates after normalization. Create `Document(kind=faq, source_type=api, source_uri=NULL)` plus pending/ready Revision, content row and all question rows in one Workspace transaction. Update locks Document, compares `active_revision_id`, inserts a complete next Revision and enqueues only after commit.

- [x] **Step 4: 写并实现固定 Chunk RED→GREEN**

Assert the stage is idempotent and produces exactly:

```go
source := "Q: 如何退款？\nQ: 退款流程是什么？\nA: 请在订单页申请退款。"
revision := model.ChunkRevision{
    Content: "请在订单页申请退款。",
    EmbeddingContent: "如何退款？\n退款流程是什么？",
    EditSource: value.ChunkEditSourceSystem,
    Enabled: true,
}
```

Use a versioned constant FAQ strategy/config hash; never read Generation `chunking_config`. Mark ChunkSet ready with `chunk_count=1` and route directly to index/publish.

- [x] **Step 5: 实现 HTTP/worker 与冲突映射**

Expose exact routes:

```text
POST /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents/faq
GET  /api/v1/workspaces/:workspace_slug/documents/:document_id/faq
PUT  /api/v1/workspaces/:workspace_slug/documents/:document_id/faq
```

POST accepts `{title, questions: string[], answer}`; PUT additionally requires `base_revision_id` and treats questions as a complete replacement. Member follows current Document-write authorization. Return 400 for invalid aggregate, 404 cross-Workspace, 409 `revision_conflict`; never expose old/active answer mix. Worker branches on Revision kind: FAQ skips parse and calls FAQ chunk stage; File/Web retain normal pipeline.

- [x] **Step 6: 运行 GREEN 并提交**

```bash
gofmt -w internal/application internal/infrastructure/db internal/interfaces/http internal/interfaces/worker
go test ./internal/application/... ./internal/interfaces/http ./internal/interfaces/worker -run FAQ -count=1
go test -tags=integration ./internal/infrastructure/db -run FAQ -count=1
go test ./... -count=1
git add internal/application internal/infrastructure/db internal/interfaces/http internal/interfaces/worker
git commit -m "feat: 支持 FAQ 文档版本与分块"
```

### Task 8: 实现统一 RetrievalEntry 与文档发布（已完成）

**Files:**
- Create: `internal/application/service/embedding_client_resolver.go`
- Create: `internal/application/service/embedding_client_resolver_test.go`
- Modify: `internal/application/service/model_connection.go`
- Replace: `internal/ports/index/index.go`
- Create: `internal/infrastructure/db/retrieval_repository.go`
- Create: `internal/infrastructure/db/retrieval_repository_integration_test.go`
- Modify: `internal/application/pipeline/index_stage.go`
- Create: `internal/application/pipeline/index_stage_test.go`
- Create: `internal/infrastructure/db/document_publish_store.go`
- Create: `internal/infrastructure/db/document_publish_store_integration_test.go`

**Interfaces:**
- Produces reusable `EmbeddingClientResolver.Resolve(ctx, workspaceID, modelID)`.
- Produces `StageBatch` and atomic `PublishDocument`; staging accepts distinct `SearchContent` and returned `Content`.

- [x] **Step 1: 写 resolver/projection RED**

Test Workspace and platform Providers, disabled records, invalid ciphertext and dimension mismatch. Integration test stages one normal and one FAQ 1024-d entry, publishes them and asserts Document/Chunk pointers and KB content versions all changed in one transaction. The fake Embedder and FTS spy must receive FAQ questions but never its answer.

- [x] **Step 2: 运行 RED**

```bash
go test ./internal/application/service -run EmbeddingClientResolver -count=1
go test -tags=integration ./internal/infrastructure/db -run StageAndPublishRetrievalEntry -count=1
```

Expected: FAIL because resolver and projection store are absent.

- [x] **Step 3: 提取 resolver**

```go
type ResolvedEmbeddingClient struct {
    Client embedding.EmbeddingClient
    ModelID, ProviderID uuid.UUID
    ModelName string
    Dimensions int
}
type EmbeddingClientResolver interface {
    Resolve(context.Context, uuid.UUID, uuid.UUID) (*ResolvedEmbeddingClient, error)
}
```

Both connection test and IndexStage reuse Provider visibility, credential decryption, typed Factory and dimension validation.

- [x] **Step 4: 实现 staging 与 batching**

`StageBatch` inserts lineage/`search_content`/`content` in batches. For File/Web, map active ChunkRevision `embedding_content → search_content` and `content → content`; for FAQ, load ordered question rows into `search_content` and answer into `content`. Then use parameterized SQL:

```sql
UPDATE retrieval_entries
SET fts_document = to_tsvector(?::regconfig, search_content),
    embedding = ?::halfvec,
    dimension = ?
WHERE workspace_id = ? AND id = ? AND state = 'staging';
```

IndexStage loads ready ChunkSet/active ChunkRevisions in sequence order, sends only each entry's `search_content` to the Embedding client, uses Model `batch_size`, validates result count, finite values and dimension, and never logs questions/answers/content/vector.

- [x] **Step 5: 实现原子 Document publish**

In one Workspace transaction lock KB, Document, Revision and active Generation; verify expected content version; require a complete staging entry for every enabled ChunkRevision; retire old Document entries; publish new entries; switch Document/Chunk pointers; increment KB content version and copy it to Generation indexed content version.

- [x] **Step 6: 运行 GREEN 并提交**

```bash
gofmt -w internal/application internal/ports/index internal/infrastructure/db
go test ./internal/application/... -run 'Embedding|IndexStage' -count=1
go test -tags=integration ./internal/infrastructure/db -run 'Retrieval|PublishDocument' -count=1
go test ./... -count=1
git add internal/application internal/ports/index internal/infrastructure/db
git commit -m "feat: 原子发布统一检索投影"
```

### Task 9: 重写 Worker Payload 与幂等流程（已完成）

**Files:**
- Modify: `internal/interfaces/worker/document_tasks.go`
- Modify: `internal/interfaces/worker/document_tasks_test.go`
- Modify: `internal/interfaces/worker/document_tasks_integration_test.go`
- Modify: `internal/adapters/queue/asynq/queue.go`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- Produces typed Workspace/Revision/Generation payload and kind-aware chain: File/Web parse → standard chunk; FAQ → fixed FAQ chunk; both index → publish.

- [x] **Step 1: 写 RED**

Decode a payload without Workspace/Revision and assert validation fails on `workspace_id` before repository calls.

- [x] **Step 2: 运行 RED**

Run: `go test ./internal/interfaces/worker -run RequiresWorkspaceAndRevision -count=1`

Expected: FAIL because payload requires only Document/Job.

- [x] **Step 3: 实现 payload**

```go
type DocumentTaskPayload struct {
    WorkspaceID        uuid.UUID `json:"workspace_id"`
    KnowledgeBaseID    uuid.UUID `json:"knowledge_base_id"`
    DocumentID         uuid.UUID `json:"document_id"`
    DocumentRevisionID uuid.UUID `json:"document_revision_id"`
    GenerationID       uuid.UUID `json:"generation_id,omitempty"`
    JobID              uuid.UUID `json:"job_id"`
}
```

Every handler enters Workspace transaction before reading Job/Revision. Remove `GetWorkspaceID(documentID)` bootstrap. Verify Job lineage equals payload.

- [x] **Step 4: 实现幂等与失败语义**

- ready Revision skips parse;
- ready ChunkSet skips chunking;
- already-published Revision in current Generation skips index;
- FAQ never opens raw storage or invokes parser; File/Web never load FAQ tables;
- failed-state persistence succeeds before `asynq.SkipRetry`;
- Queue TaskID is `<type>:<workspace>:<revision>:<generation>`.

- [x] **Step 5: 运行 GREEN 并提交**

```bash
gofmt -w internal/interfaces/worker internal/adapters/queue cmd/langhuan
go test ./internal/interfaces/worker ./internal/adapters/queue -count=1
go test -tags=integration ./internal/interfaces/worker -count=1
go test ./... -count=1
git add internal/interfaces/worker internal/adapters/queue cmd/langhuan
git commit -m "feat: 按 Workspace 版本驱动文档任务"
```

### Task 10: 增加 Chunk Revision 编辑、启停与 REST（已完成）

**Files:**
- Create: `internal/application/dto/chunk.go`
- Create: `internal/application/service/chunk_revision.go`
- Create: `internal/application/service/chunk_revision_test.go`
- Create: `internal/infrastructure/db/chunk_revision_store.go`
- Create: `internal/infrastructure/db/chunk_revision_store_integration_test.go`
- Create: `internal/interfaces/http/chunk_revision_handler.go`
- Create: `internal/interfaces/http/chunk_revision_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/errors.go`

**Interfaces:**
- Produces GET Chunk, GET revisions and POST revision endpoints.
- POST requires `base_revision_id/content/context_header/enabled`.

- [x] **Step 1: 写 service/permission RED**

Stale base must return `ErrRevisionConflict`. A Chunk belonging to `kind=faq` must return `ErrFAQChunkImmutable` before creating/indexing a Revision. Table-drive member→403, admin/owner→202 for File/Web POST and admin/owner→409 `faq_chunk_immutable` for FAQ.

- [x] **Step 2: 运行 RED**

Run: `go test ./internal/application/service ./internal/interfaces/http -run 'ChunkEdit|ChunkRevision' -count=1`

Expected: FAIL because service/routes are absent.

- [x] **Step 3: 实现 append/index/publish**

Load Document kind under the Workspace/KB lineage; reject FAQ and direct clients to the FAQ update API. For File/Web validate UTF-8 and at most 100,000 runes, create pending user Revision under lock and enqueue targeted indexing. Publish input:

```go
type PublishChunkRevisionInput struct {
    WorkspaceID, KnowledgeBaseID, GenerationID uuid.UUID
    ChunkID, BaseRevisionID, NewRevisionID uuid.UUID
    ExpectedContentVersion int64
}
```

The transaction rechecks base/pointer/content version, retires old entry, publishes a new enabled entry or none when disabled, switches Chunk pointer and advances versions.

- [x] **Step 4: 实现 HTTP mapping**

400 `validation_error`; 403 member; 404 cross Workspace; 409 `revision_conflict`; 202 accepted Revision DTO.

- [x] **Step 5: 运行 GREEN 并提交**

```bash
gofmt -w internal/application internal/infrastructure/db internal/interfaces/http
go test ./internal/application/service ./internal/interfaces/http -run 'ChunkEdit|ChunkRevision' -count=1
go test -tags=integration ./internal/infrastructure/db -run ChunkRevision -count=1
go test ./... -count=1
git add internal/application internal/infrastructure/db internal/interfaces/http
git commit -m "feat: 支持分块修订与启停"
```

### Task 11: 实现 Generation 重建、stale 检测与激活（已完成）

**Files:**
- Create: `internal/application/dto/index_generation.go`
- Create: `internal/application/service/index_generation.go`
- Create: `internal/application/service/index_generation_test.go`
- Create: `internal/infrastructure/db/index_generation_store.go`
- Create: `internal/infrastructure/db/index_generation_store_integration_test.go`
- Create: `internal/interfaces/http/index_generation_handler.go`
- Create: `internal/interfaces/http/index_generation_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/worker/document_tasks.go`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- Produces Generation list/create/activate APIs and rebuild task.
- Enforces content-version CAS and manual-edit confirmation.

- [x] **Step 1: 写 RED**

Activation without archive confirmation returns `ErrManualEditConfirmationRequired`; mutate FAQ/File content after build and assert `ErrGenerationStale`; rename/move a file-tree node after build and assert activation still succeeds and returns the current name.

- [x] **Step 2: 运行 RED**

```bash
go test ./internal/application/service -run ActivateGeneration -count=1
go test -tags=integration ./internal/infrastructure/db -run IndexGenerationStore -count=1
```

Expected: FAIL because rebuild/activation store is absent.

- [x] **Step 3: 实现 create/build**

Lock KB; reject another building Generation; snapshot active pointer/content version/model/chunk/retrieval config; validate new Model; compute hashes/manual-edit count; insert building row and enqueue deterministic task. Model-only/retrieval-only rebuild reuses File/Web/FAQ ChunkSets. Changed ordinary ChunkingConfig creates new `strategy=standard` sets for File/Web only and always reuses FAQ `strategy=faq` sets. Stage/publish entries inside the inactive Generation, then set it ready.

- [x] **Step 4: 实现 activation**

Lock KB/candidate; verify base pointer, source content version and ready status; require `archive_manual_edits=true` when pending; retire old Generation; switch only KB active pointer; record activation. Changed chunk config archives replaced File/Web ChunkSets without deleting history and never archives FAQ sets merely because ordinary config changed. File-tree mutations do not touch content version or this CAS.

- [x] **Step 5: 实现权限与错误**

GET list member+; create/activate admin/owner. Return distinct 409 codes for building conflict, stale, not-ready and missing confirmation.

- [x] **Step 6: 运行 GREEN 并提交**

```bash
gofmt -w internal/application internal/infrastructure/db internal/interfaces/http internal/interfaces/worker cmd/langhuan
go test ./internal/application/service ./internal/interfaces/http -run Generation -count=1
go test -tags=integration ./internal/infrastructure/db -run Generation -count=1
go test ./... -count=1
git add internal/application internal/infrastructure/db internal/interfaces/http internal/interfaces/worker cmd/langhuan
git commit -m "feat: 支持知识库索引代次切换"
```

### Task 12: 实现 Workspace-scoped Vector/FTS/RRF Search（已完成）

**Files:**
- Replace: `internal/ports/index/index.go`
- Modify: `internal/ports/index/index_test.go`
- Create: `internal/application/dto/search.go`
- Create: `internal/application/service/search.go`
- Create: `internal/application/service/search_test.go`
- Modify: `internal/infrastructure/db/retrieval_repository.go`
- Create: `internal/infrastructure/db/retrieval_search_integration_test.go`
- Create: `internal/interfaces/http/search_handler.go`
- Create: `internal/interfaces/http/search_handler_test.go`
- Modify: `internal/interfaces/http/router.go`

**Interfaces:**
- Produces `POST /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/search`.
- Produces deterministic RRF evidence with projection `content` and current Document/file-node display name.

- [x] **Step 1: 写 RRF/tenant RED**

Two candidate lists with B present in both must rank B first; ties sort UUID ascending. Integration seed two Workspaces/Generations and assert search A never returns B. Seed an FAQ whose answer contains an answer-only token: question token must hit and return the answer, answer-only token must not FTS-hit. Rename a File node after indexing and assert search immediately returns the new name without changing RetrievalEntry.

- [x] **Step 2: 运行 RED**

```bash
go test ./internal/application/service -run ReciprocalRankFusion -count=1
go test -tags=integration ./internal/infrastructure/db -run RetrievalSearch -count=1
```

Expected: FAIL because RRF/search queries are absent.

- [x] **Step 3: 实现四个固定 vector SQL**

For dimension 1024:

```sql
SELECT chunk_revision_id, document_id, content,
       1 - ((embedding::halfvec(1024)) <=> (?::halfvec(1024))) AS score
FROM retrieval_entries
WHERE workspace_id = ? AND knowledge_base_id = ?
  AND index_generation_id = ? AND state = 'published'
  AND dimension = 1024
ORDER BY (embedding::halfvec(1024)) <=> (?::halfvec(1024))
LIMIT ?;
```

Use four fixed statements after dimension validation; never interpolate arbitrary dimension. FTS uses `plainto_tsquery` and `ts_rank_cd` against stored `fts_document` with identical tenant/generation filters; it never rebuilds a query document from returned `content`.

- [x] **Step 4: 实现 RRF/evidence**

```go
type SearchResult struct {
    ChunkID, ChunkRevisionID, DocumentID uuid.UUID
    DocumentKind value.DocumentKind
    Content, DocumentName string
    SourceAnchor value.SourceAnchor
    Score float64
    VectorScore, KeywordScore *float64
    Metadata map[string]any
}
```

After RRF selects final candidate IDs, batch load current Documents and LEFT JOIN the unique file node inside the same Workspace context. File uses node name and verifies its `documents.title` mirror; FAQ/Web use current `documents.title`. Do not trust a title snapshot in RetrievalEntry. Member+ may search. Clamp final topK to 1-50; omitted values use Generation defaults. Add `EXPLAIN (COSTS OFF)` integration coverage with `enable_seqscan=off` to prove expression/index compatibility.

- [x] **Step 5: 运行 GREEN 并提交**

```bash
gofmt -w internal/ports/index internal/application internal/infrastructure/db internal/interfaces/http
go test ./internal/ports/index ./internal/application/service ./internal/interfaces/http -run 'Search|RRF' -count=1
go test -tags=integration ./internal/infrastructure/db -run RetrievalSearch -count=1
go test ./... -count=1
git add internal/ports/index internal/application internal/infrastructure/db internal/interfaces/http
git commit -m "feat: 实现知识库混合检索"
```

### Task 13: 清理、文档、Web 兼容与完整回归（已完成）

**Files:**
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/config_test.go`
- Modify: `config.example.yaml`
- Create: `internal/application/service/retrieval_cleanup.go`
- Create: `internal/application/service/retrieval_cleanup_test.go`
- Create: `internal/infrastructure/db/retrieval_cleanup_repository.go`
- Create: `internal/infrastructure/db/retrieval_cleanup_repository_integration_test.go`
- Modify: `internal/application/service/document.go`
- Modify: `internal/infrastructure/db/document_repository.go`
- Modify: `internal/interfaces/http/document.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `ROADMAP.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DATABASE_GUIDELINES.md`
- Modify: `web/src/features/knowledge-bases/data/schema.ts`
- Modify: `web/src/features/knowledge-bases/data/api.ts`
- Test: existing Web KnowledgeBase/Document tests

**Interfaces:**
- Produces retention config, idempotent cleanup and compatible Web DTO parsing.

- [x] **Step 1: 写 config/cleanup RED**

Assert defaults are 24h/168h and cleanup deletes only expired failed staging/retired Generation data, never active Generation rows. File Document delete retires projection before object cleanup and removes its file node atomically; deleting FAQ/Web never touches unrelated tree nodes.

- [x] **Step 2: 运行 RED**

```bash
go test ./internal/infrastructure/config ./internal/application/service -run 'RetrievalRetention|Cleanup' -count=1
go test -tags=integration ./internal/infrastructure/db -run RetrievalCleanup -count=1
```

Expected: FAIL because config/cleanup are absent.

- [x] **Step 3: 实现 YAML 与 batch cleanup**

```yaml
retrieval:
  failed_staging_retention: 24h
  retired_generation_retention: 168h
  cleanup_batch_size: 1000
```

Durations must be positive; batch is 1-10000. Select stable IDs with `FOR UPDATE SKIP LOCKED`, delete at most one batch/transaction, and exclude active Generation by KB pointer.

- [x] **Step 4: 同步 docs/Web**

Update ROADMAP/Architecture/Database Guidelines for direct tenant key, composite FK, immutable `DocumentKind`, FAQ aggregate, File Tree, Revision/ChunkSet, single-active Generation, `search_content` versus returned `content`, and Workspace transaction. Remove current-target references to old index tables and authoritative RetrievalEntry title snapshots. Keep existing KB model-selection UX; parse active-Generation-derived model/config, Document kind/current name and active-Revision summary. Do not add Chunk editor, FAQ editor or full File Tree UI in this backend/schema plan.

- [x] **Step 5: 运行 focused GREEN**

```bash
gofmt -w internal cmd
go test ./internal/infrastructure/config ./internal/application/service -run 'RetrievalRetention|Cleanup|Delete' -count=1
go test -tags=integration ./internal/infrastructure/db -run 'RetrievalCleanup|Delete' -count=1
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

- [x] **Step 6: 运行完整验收**

```bash
go test ./... -count=1
go test -tags=integration ./... -count=1
go vet ./...
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
git diff --check
```

Expected: every command exits 0. Evidence includes cross-tenant FK rejection, atomic Generation switch, concurrent Chunk/FAQ conflicts, FAQ answer exclusion from FTS/Embedding, file-tree cycle/name/delete constraints, rename-without-reindex, HNSW expression compatibility and Auth/Model preservation.

- [x] **Step 7: 提交闭环**

```bash
git status --short
git add config.example.yaml ROADMAP.md docs/ARCHITECTURE.md docs/DATABASE_GUIDELINES.md internal cmd \
  web/src/features/knowledge-bases/data/schema.ts \
  web/src/features/knowledge-bases/data/api.ts
git diff --cached --check
git commit -m "feat: 完成知识检索数据模型 v2"
```

Remove unrelated paths from the index before commit; never stage credentials, raw documents, caches or user-owned pre-existing modifications.

## RLS Follow-up Boundary

This plan stops before enabling RLS. A separate design/plan must create a non-owner application role without `BYPASSRLS`, enable+force RLS, add tenant `USING/WITH CHECK` policies, prove HTTP/worker/MCP/cleanup/admin paths enter `WithinWorkspace`, use a separate migration role, and run a two-Workspace negative matrix. No v2 table or Repository signature should need another lineage refactor.
