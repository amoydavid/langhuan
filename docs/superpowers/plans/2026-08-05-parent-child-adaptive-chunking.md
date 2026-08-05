# 父子分块与自适应分块 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 File/Web 文档交付 child 召回、parent 全文返回的自适应分块、索引、检索与管理台检查体验。

**Architecture:** Generation 保存完整的 v3 分块快照。父子模式的 ChunkSet 同时持久化 parent 与 child，关闭时只持久化 flat；RetrievalEntry 只对应启用的 child/flat。Search 在 child RRF 后按 parent 聚合，flat 直接返回，并附带命中片段。MinerU Markdown 先重建为结构化 manifest，再与 Markdown/DOCX 进入同一分块器。

**Tech Stack:** Go 1.26、Gin、GORM/PostgreSQL/pgvector、golang-migrate、TanStack Query、React 19、TypeScript、Zod、React Hook Form、Tailwind、Radix/shadcn、Vitest。

## Global Constraints

- Domain 与 application 不导入或持有 `*gorm.DB`；所有数据库查询显式带 `workspace_id`。
- parent-child 外键必须包含 workspace、KB、document、revision 与 ChunkSet lineage；数据库测试仅用临时 pgvector + zhparser Docker 容器。
- File/Web 使用 standard chunker v3；FAQ 保持 `strategy=faq` 单块，不能进入父子路径。
- parent 只读且不创建 RetrievalEntry；开启父子分块时 parent/child 同时持久化，child 是唯一可编辑、Embedding、FTS 的层；关闭时只持久化可编辑、Embedding、FTS 的 flat，v2 扁平 ChunkSet 按 flat 兼容读取。
- `content` 保持最终返回正文；Search 新增 `matched_children`。普通 UI 不显示 UUID、hash、原始 metadata 或 payload。
- 管理台复用现有 AppShell、shadcn/Radix、TanStack Query、共享 axios client 和工程绿语义 token；禁止 `any` 与组件内 `fetch`。
- 每个任务遵循测试先行，使用中文 Conventional Commit；迁移和结构重组不得夹带无关行为改变。

---

## 文件结构

| 路径 | 职责 |
|---|---|
| `internal/domain/value/config.go`、`chunk_role.go` | v3 配置、策略、三种角色与校验。 |
| `internal/infrastructure/migrate/migrations/000013_parent_child_chunking.*.sql` | parent-child 列、约束、索引。 |
| `internal/application/pipeline/chunk_strategy.go`、`chunk_hierarchy.go` | 策略选择、回退和 manifest-aware hierarchy。 |
| `internal/adapters/parserprovider/mineru/manifest.go` | PDF Markdown 的结构化重解析。 |
| `internal/infrastructure/db/chunk_*.go` | Row codec、ChunkSet、Document Chunk、child-only source。 |
| `internal/application/service/{index_generation_build,search,multi_knowledge_search}.go` | child staging 和 parent evidence 聚合。 |
| `internal/interfaces/http/*`、`internal/interfaces/mcp/*` | v3 config 和 Search JSON 合同。 |
| `web/src/components/chunking-config-fields.tsx` | 供知识库与候选代次共用的 RHF 分块配置字段。 |
| `web/src/features/chunks/inspector/chunk-inspector.tsx` | parent-child 层级检查器。 |
| `web/src/features/retrieval/retrieval-test.tsx` | 父块正文与命中子块展示。 |

## 前端实施合同：原型与交互

以下原型是 Tasks 7–9 的验收界面；移动端改为单栏但不得删减字段、状态或操作语义。

### 1. 知识库 / 候选代次的分块配置

```text
新建知识库 / 构建候选索引
┌─────────────────────────────────────────────────────┐
│ 分块方式                                             │
│ 策略  [自动选择 ▼]  ⓘ 根据文档结构选择切分方式       │
│                                                     │
│ 父子分块                                  [ 开关 ON ] │
│ 小块大小（用于召回）       [ 384 ]                    │
│ 上下文块大小（用于返回）   [ 4096 ]                   │
│ 父块重叠                   [ 80  ]                    │
│                                                     │
│ 将创建候选索引。构建完成并激活前，当前检索保持不变。   │
└─────────────────────────────────────────────────────┘
```

- `strategy` 是 Select：自动选择、按标题、按文档结构、递归切分。
- 开关开启时显示 child、parent 与 parent overlap；关闭时显示扁平 `chunk_size` 和 `chunk_overlap`。切换不清空已输入的另一套草稿。
- 每个数值字段在失焦和提交时显示 Zod 错误；提交失败不重置表单。创建代次后保持现有候选代次状态，直至用户激活。
- owner/admin 可以创建；member 只读摘要，直达 `?create=true` 仍展示既有 403 状态。

### 2. 文件分块检查器

```text
分块  [仅看可检索内容 □]                     共 12 个子块
┌─────────────────────────────────────────────────────┐
│ ▾ 上下文块 1 · 4 个子块 · 第 1 章 > 安装              │
│   父块仅提供完整上下文，不参与召回                    │
│   ├─ 子块 1  已启用 · 第 1 章 > 安装      [查看] [编辑] │
│   ├─ 子块 2  已启用 · 第 1 章 > 安装      [查看] [编辑] │
│   └─ 子块 3  已停用 · 第 1 章 > 安装      [查看] [编辑] │
│ ▸ 上下文块 2 · 3 个子块 · 第 2 章 > 配置              │
└─────────────────────────────────────────────────────┘
```

- parent 是可展开组；没有持久化 parent 的短文档显示为独立 child。勾选「仅看可检索内容」仅保留 enabled child，不改变总数口径。
- `?chunk=<child-id>` 打开并滚动到该 child；parent 详情只读展示全文、来源与子块数，绝不显示编辑动作。
- child 详情显示完整上下文只读区，可进入既有 revision 编辑。保存成功后失效此文档 chunks、revisions、知识库摘要及检索缓存；冲突时保留草稿。
- Collapsible、Dialog、编辑控件均支持键盘；关闭 Dialog 恢复到触发元素。平板使用 Sheet，移动端进入 child 列表，触控目标至少 44×44px。

### 3. 检索结果卡

```text
┌ 文档：部署指南  [文件]                     RRF 0.042 ┐
│ 第 2 章 > 运行配置 · 返回完整上下文                     │
│                                                         │
│ 父块全文（SafeMarkdown，完整显示）                      │
│ …                                                       │
│                                                         │
│ 命中片段 2                                               │
│ • 子块片段：配置文件位于…   第 2 章 · 分数 0.042 [定位]  │
│ • 子块片段：重启服务后…     第 2 章 · 分数 0.031 [定位]  │
└─────────────────────────────────────────────────────────┘
```

- 正文始终使用返回的 `content`（父块全文）；命中列表来自 `matched_children`，按服务端顺序展示，分数不格式化为百分比。
- 「定位」链接到首个命中 child 的既有文件深链，不能使用 parent ID。UUID、hash、原始 metadata 和 payload 不在普通界面显示。
- 无结果、加载、失败沿用现有工作面；颜色以外必须有文字和图标表示状态，并遵循减少动效设置。

### Task 1: 定义 v3 配置与父子领域模型

**Files:**

- Create: `internal/domain/value/chunk_role.go`
- Modify: `internal/domain/value/config.go`, `internal/domain/model/chunk.go`
- Test: `internal/domain/value/config_test.go`, `internal/domain/model/chunk_test.go`

**Interfaces:** 产出 `value.ChunkingStrategy`、三角色 `value.ChunkRole`、扩展 `ChunkingConfig`，以及带 `Role/ParentChunkID` 的 `model.Chunk`，供后续所有任务使用。

- [ ] **Step 1: 写失败的领域测试。**

```go
func TestChunkingConfigParentChildDefaultsAndValidation(t *testing.T) {
    cfg := value.DefaultChunkingConfig()
    if cfg.Strategy != value.ChunkStrategyAuto || !cfg.EnableParentChild || cfg.ParentChunkSize != 4096 || cfg.ChildChunkSize != 384 { t.Fatalf("%#v", cfg) }
    cfg.ChildChunkSize = cfg.ParentChunkSize + 1
    if !errors.Is(cfg.Validate(), domainerrors.ErrValidation) { t.Fatal("want validation") }
}

func TestChildChunkRequiresParent(t *testing.T) {
    child := model.Chunk{Role: value.ChunkRoleChild}
    if !errors.Is(child.ValidateLineage(), domainerrors.ErrValidation) { t.Fatal("want validation") }
}
```

- [ ] **Step 2: 运行测试，确认新增符号尚不存在。**

Run: `go test ./internal/domain/value ./internal/domain/model -run 'ParentChild|ChildChunk' -count=1`

Expected: FAIL，编译错误指出 strategy、role、parent 字段未定义。

- [ ] **Step 3: 实现最小领域合同。**

```go
type ChunkingStrategy string
const (
    ChunkingStrategyAuto ChunkingStrategy = "auto"
    ChunkingStrategyHeading ChunkingStrategy = "heading"
    ChunkingStrategyHeuristic ChunkingStrategy = "heuristic"
    ChunkingStrategyRecursive ChunkingStrategy = "recursive"
)
type ChunkRole string
const (
    ChunkRoleParent ChunkRole = "parent"
    ChunkRoleChild ChunkRole = "child"
    ChunkRoleFlat ChunkRole = "flat"
)
type ChunkingConfig struct {
    ChunkSize, ChunkOverlap int
    Strategy ChunkStrategy
    EnableParentChild bool
    ParentChunkSize, ChildChunkSize int
}
```

将 `StandardChunkerVersion` 更新为 `3`。`Normalize` 对缺省配置补 `auto/on/4096/384`，`Validate` 接受四个策略；校验 parent 512..8192、child 64..2048、child 不大于 parent；关闭父子时沿用 `chunk_size > 0` 与 `0 <= overlap < chunk_size`。`Chunk.ValidateLineage` 要求 parent/flat 无 parent ID、child 有 parent ID。

- [ ] **Step 4: 验证领域层。**

Run: `go test ./internal/domain/value ./internal/domain/model -count=1`

Expected: PASS。

- [ ] **Step 5: 提交。**

```bash
git add internal/domain/value internal/domain/model
git commit -m "feat(chunk): 定义父子分块领域配置"
```

### Task 2: 迁移并持久化父子 lineage

**Files:**

- Create: `internal/infrastructure/migrate/migrations/000013_parent_child_chunking.up.sql`, `internal/infrastructure/migrate/migrations/000013_parent_child_chunking.down.sql`
- Modify: `internal/infrastructure/db/chunk_rows.go`, `internal/infrastructure/db/chunk_set_repository.go`, `internal/infrastructure/db/chunk_repository.go`, `internal/infrastructure/db/document_chunks_repository.go`
- Test: `internal/infrastructure/migrate/migrate_v013_parent_child_integration_test.go`, `internal/infrastructure/db/chunk_set_repository_integration_test.go`, `internal/infrastructure/db/document_chunks_repository_integration_test.go`

**Interfaces:** 消费 Task 1，产出原子写入/读取的完整层级；Document Chunk 列表稳定按 parent 后 child 排序。

- [ ] **Step 1: 写临时数据库失败用例。**

```go
func TestV013RejectsCrossChunkSetParent(t *testing.T) {
    db := integrationDatabase(t)
    parent := seedChunk(t, db, seedA, setA.ID, value.ChunkRoleParent, nil)
    child := seedChunk(t, db, seedA, setB.ID, value.ChunkRoleChild, &parent.ID)
    if err := db.Create(child).Error; err == nil { t.Fatal("cross-set parent must fail") }
}
```

- [ ] **Step 2: 运行测试，确认约束尚未存在。**

Run: `go test -tags=integration ./internal/infrastructure/migrate ./internal/infrastructure/db -run 'V013|ParentChild' -count=1`

Expected: FAIL，`role`/`parent_chunk_id` 不存在或非法关联被接受。

- [ ] **Step 3: 写迁移和 Repository codec。**

```sql
ALTER TABLE chunks ADD COLUMN role text NOT NULL DEFAULT 'child';
ALTER TABLE chunks ADD COLUMN parent_chunk_id uuid;
ALTER TABLE chunks ADD CONSTRAINT chunks_role_check CHECK (role IN ('parent','child','flat'));
ALTER TABLE chunks ADD CONSTRAINT chunks_parent_shape_check CHECK ((role IN ('parent','flat') AND parent_chunk_id IS NULL) OR (role = 'child' AND parent_chunk_id IS NOT NULL));
ALTER TABLE chunks ADD CONSTRAINT chunks_parent_fk FOREIGN KEY
  (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, parent_chunk_id)
  REFERENCES chunks (workspace_id, knowledge_base_id, document_id, document_revision_id, chunk_set_id, id)
  DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX idx_chunks_parent ON chunks (workspace_id, chunk_set_id, parent_chunk_id, sequence);
```

在 `ChunkRow` 与 `chunkV2ToRow/chunkV2FromRow` 映射 `Role/ParentChunkID`。`encodeChunkSetBuild` 校验 child 的 parent 在同批次、完整 lineage 相同，并允许 parent/child 各自 sequence；flat 不得引用 parent。先插入所有 Chunk rows，再插入 revisions。Document Chunk seek cursor 改为 `(role_rank, sequence, id)`，查询使用 `ORDER BY CASE role WHEN 'parent' THEN 0 WHEN 'child' THEN 1 ELSE 2 END, sequence, id`，避免两层独立 sequence 漏页。

- [ ] **Step 4: 验证迁移和真实 PostgreSQL 行为。**

Run: `make test-image && go test -tags=integration -p 1 ./internal/infrastructure/migrate ./internal/infrastructure/db -run 'V013|ParentChild|DocumentChunk' -count=1`

Expected: PASS；跨 ChunkSet/Workspace、child 无 parent 均失败，合法层级可稳定读回。

- [ ] **Step 5: 提交。**

```bash
git add internal/infrastructure/migrate internal/infrastructure/db
git commit -m "feat(db): 持久化父子分块关系"
```

### Task 3: 实现 manifest-aware 自适应 hierarchy

**Files:**

- Create: `internal/application/pipeline/chunk_strategy.go`, `internal/application/pipeline/chunk_strategy_test.go`, `internal/application/pipeline/chunk_hierarchy.go`, `internal/application/pipeline/chunk_hierarchy_test.go`
- Modify: `internal/application/pipeline/chunker.go`, `internal/application/pipeline/chunk_stage.go`, `internal/application/pipeline/chunker_test.go`, `internal/application/pipeline/chunk_stage_test.go`

**Interfaces:** 消费 Task 1 配置和现有 `ParseManifest`；产出带角色、parent 引用、准确 anchor 的 chunks/revisions。

- [ ] **Step 1: 写策略和 hierarchy 的失败测试。**

```go
func TestAutoStrategyPrefersHeadingThenFallsBackToRecursive(t *testing.T) {
    got := selectStrategy(markdownManifestWithThreeHeadings(), value.DefaultChunkingConfig())
    if got[0] != value.ChunkStrategyHeading || got[len(got)-1] != value.ChunkStrategyRecursive { t.Fatal(got) }
}
func TestChunkerEmbedsChildrenAndLinksParents(t *testing.T) {
    chunks, _, err := NewChunker().Chunk(input, value.DefaultChunkingConfig())
    if err != nil { t.Fatal(err) }
    if countRole(chunks, value.ChunkRoleParent) == 0 { t.Fatal("want parent") }
    for _, c := range roleChunks(chunks, value.ChunkRoleChild) { if c.ParentChunkID == nil || c.EmbeddingContent == "" { t.Fatal(c) } }
}
```

- [ ] **Step 2: 运行测试，确认 flat chunker 未满足合同。**

Run: `go test ./internal/application/pipeline -run 'AutoStrategy|LinksParents' -count=1`

Expected: FAIL，策略函数不存在或 parent 数量为零。

- [ ] **Step 3: 实现策略、回退和 materialization。**

```go
type chunkDraft struct {
    role value.ChunkRole
    parentDraft int // child 指向 parent draft；独立 child 为 -1
    content string; headingPath []string
    anchor value.SourceAnchor; metadata map[string]any
}
```

`heading` 依据 manifest heading path 切分并写 breadcrumb；`heuristic` 仅在普通 text block 识别分页符、编号章节、中英文标题、全大写与分隔线；`recursive` 使用段落、换行、句末优先边界。每种策略先保护 code block、表格完整行与重复表头，再校验大小/碎片率，失败按 heading → heuristic → recursive 回退。parent 使用 `ParentChunkSize/ChunkOverlap`，child 使用 `ChildChunkSize/ChildChunkSize/5`；关闭父子只生成 child。开启父子时，即使只有一个且正文完全相同的 child，也必须持久化 parent。先为 parent 分配 UUID，再赋给 child；parent system revision 为 `ready`，child 为 `pending`。v3 全字段进入 config hash，Generation version 差异也必须触发 rechunk。

- [ ] **Step 4: 运行 pipeline 回归。**

Run: `go test ./internal/application/pipeline -count=1`

Expected: PASS；覆盖中文 rune、短文本 parent/child 同时持久化、标题 breadcrumb、表格行/表头、代码保护与确定性。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/pipeline
git commit -m "feat(chunk): 实现自适应父子分块器"
```

### Task 4: 将 PDF Markdown 重建为结构化 manifest

**Files:**

- Modify: `internal/adapters/parserprovider/mineru/manifest.go`, `internal/adapters/parserprovider/mineru/parser.go`
- Test: `internal/adapters/parserprovider/mineru/manifest_test.go`, `internal/adapters/parserprovider/mineru/parser_test.go`

**Interfaces:** 产出 SourceType 为 `pdf` 的 heading/table/code manifest，供 Task 3 统一使用。

- [ ] **Step 1: 写失败测试。**

```go
func TestBuildParsedDocumentReparsesMinerUMarkdown(t *testing.T) {
    parsed, err := buildParsedDocument(context.Background(), "# 安装\n\n正文\n\n| 名称 | 值 |\n| - | - |\n| A | 1 |", "v1")
    if err != nil { t.Fatal(err) }
    if len(parsed.Manifest.Blocks) < 3 || parsed.Manifest.Blocks[0].Kind != model.BlockKindHeading { t.Fatal(parsed.Manifest) }
    for _, b := range parsed.Manifest.Blocks { if b.SourceAnchor.SourceType != "pdf" { t.Fatal(b.SourceAnchor) } }
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test ./internal/adapters/parserprovider/mineru -run ReparseMinerU -count=1`

Expected: FAIL，当前只创建一个 paragraph block。

- [ ] **Step 3: Write minimal implementation.**

将 `buildParsedDocument` 改为接收 `context.Context`，调用 `markdown.New().Parse(ctx, parser.ParseInput{FileType: "markdown", Content: []byte(markdown)})`。遍历 blocks/warnings，将每个 SourceAnchor 的 `SourceType` 重写为 `pdf`；保留 MinerU zip asset candidates，重解析失败包装为 `MinerU Markdown 结构化解析失败: %w`。

- [ ] **Step 4: Run test to verify it passes.**

Run: `go test ./internal/adapters/parserprovider/mineru -count=1`

Expected: PASS；空 Markdown、任务失败与 zip 图片回归仍通过。

- [ ] **Step 5: Commit.**

```bash
git add internal/adapters/parserprovider/mineru
git commit -m "fix(mineru): 重建 PDF 结构化分块清单"
```

### Task 5: 仅索引 child，并按 parent 聚合检索证据

**Files:**

- Modify: `internal/ports/index/index.go`, `internal/application/service/index_generation_build.go`, `internal/application/service/search.go`, `internal/application/service/multi_knowledge_search.go`, `internal/application/dto/search.go`
- Modify: `internal/infrastructure/db/chunk_set_repository.go`, `internal/infrastructure/db/retrieval_search_repository.go`
- Test: `internal/application/service/index_generation_build_test.go`, `internal/application/service/search_test.go`, `internal/application/service/multi_knowledge_search_test.go`, `internal/infrastructure/db/retrieval_search_integration_test.go`

**Interfaces:** 产出 child-only RetrievalEntry 和带 `MatchedChildren` 的父级 SearchResult。

- [ ] **Step 1: Write the failing test.**

```go
func TestGenerationBuildStagesOnlyEnabledChildren(t *testing.T) {
    entries, _, err := svc.stage(ctx, request, generation, []*indexport.Source{parentChildSource()})
    if err != nil { t.Fatal(err) }
    if len(entries) != 2 || entries[0].ChunkID == parentChildSource().Chunks[0].ID { t.Fatal(entries) }
}
func TestSearchGroupsMatchingChildrenUnderOneParent(t *testing.T) {
    got, err := service.Search(ctx, SearchInput{WorkspaceID: ws, KnowledgeBaseID: kb, Query: "配置"})
    if err != nil { t.Fatal(err) }
    if len(got) != 1 || len(got[0].MatchedChildren) != 2 || got[0].Content != "完整父块" { t.Fatal(got) }
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test ./internal/application/service -run 'StagesOnlyEnabledChildren|GroupsMatchingChildren' -count=1`

Expected: FAIL，entry 包含 parent 或每个 child 返回独立结果。

- [ ] **Step 3: Write minimal implementation.**

```go
type MatchedChild struct {
    ChunkID uuid.UUID `json:"chunk_id"`; ChunkRevisionID uuid.UUID `json:"chunk_revision_id"`
    Content string `json:"content"`; SourceAnchor map[string]any `json:"source_anchor"`
    Score float64 `json:"score"`; VectorScore *float64 `json:"vector_score,omitempty"`; KeywordScore *float64 `json:"keyword_score,omitempty"`
}
```

`GetReadyIndexSource` 读取完整 ChunkSet，但 `stage` 跳过 parent 和 disabled child。`LoadEvidence` 从 child retrieval entry 联接 child，并 left join parent/current revision；flat chunk 用 `COALESCE(parent.id, child.id)` 返回自身。RRF 后按 `(knowledge_base_id, effective_parent_id)` 聚合，最高分 child 保留为 top-level `chunk_id/score`，父块的正文/anchor/metadata 成为结果；MatchedChildren 按分数降序、child UUID 升序。单库和多库都在 finalTopK 截断前做该聚合。

- [ ] **Step 4: Run test to verify it passes.**

Run: `make test-image && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'Retrieval.*Parent|Search.*Parent' -count=1 && go test ./internal/application/service -run 'GenerationBuild|Search|MultiKnowledgeSearch' -count=1`

Expected: PASS；parent 无 RetrievalEntry，多个 child 命中只返回一个 parent，flat v2 chunk 返回自身正文。

- [ ] **Step 5: Commit.**

```bash
git add internal/ports/index internal/application/service internal/application/dto internal/infrastructure/db
git commit -m "feat(search): 子块召回并返回父块上下文"
```

### Task 6: 扩展 REST/MCP、Chunk DTO 与 parent 编辑边界

**Files:**

- Modify: `internal/application/dto/chunk.go`, `internal/application/dto/knowledge_base.go`, `internal/application/service/index_generation.go`, `internal/application/service/knowledge_base_generation.go`, `internal/application/service/chunk_revision.go`
- Modify: `internal/interfaces/http/knowledge_base.go`, `internal/interfaces/http/index_generation_handler.go`, `internal/interfaces/mcp/tools.go`, `internal/interfaces/mcp/adapters.go`
- Test: `internal/interfaces/http/index_generation_handler_test.go`, `internal/interfaces/mcp/server_test.go`, `internal/application/service/chunk_revision_test.go`

**Interfaces:** 输出完整 v3 config、Chunk `role/parent_chunk_id`、Search `matched_children`，并拒绝 parent revision 写入。

- [ ] **Step 1: Write the failing test.**

```go
func createGenerationRequest(t *testing.T, body string) (*generationHTTPServiceFake, *httptest.ResponseRecorder) {
    t.Helper()
    workspaceID, userID, kbID := uuid.New(), uuid.New(), uuid.New()
    fake := &generationHTTPServiceFake{created: &dto.IndexGeneration{ID: uuid.New()}}
    handler := indexGenerationHandler{service: fake}
    router := gin.New()
    router.POST("/knowledge-bases/:id/index-generations", func(c *gin.Context) {
        c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID, Role: value.RoleAdmin})
        handler.create(c)
    })
    request := httptest.NewRequest(http.MethodPost, "/knowledge-bases/"+kbID.String()+"/index-generations", bytes.NewBufferString(body))
    request.Header.Set("Content-Type", "application/json")
    recorder := httptest.NewRecorder(); router.ServeHTTP(recorder, request)
    return fake, recorder
}
func TestIndexGenerationCreateAcceptsV3ChunkingConfig(t *testing.T) {
    fake, recorder := createGenerationRequest(t, `{"chunking_config":{"strategy":"auto","enable_parent_child":true,"parent_chunk_size":4096,"child_chunk_size":384,"chunk_size":512,"chunk_overlap":80}}`)
    if recorder.Code != http.StatusAccepted { t.Fatalf("status=%d", recorder.Code) }
    got := *fake.createInput.ChunkingConfig
    if got.Strategy != value.ChunkStrategyAuto || !got.EnableParentChild || got.ParentChunkSize != 4096 || got.ChildChunkSize != 384 { t.Fatalf("config=%#v", got) }
}
func TestChunkRevisionRejectsParent(t *testing.T) {
    store := newFakeChunkRevisionStore(value.DocumentKindFile)
    store.chunk.Role = value.ChunkRoleParent
    svc := NewChunkRevisionService(store, &fakeChunkRevisionQueue{})
    _, err := svc.Create(context.Background(), CreateChunkRevisionInput{WorkspaceID: store.chunk.WorkspaceID, KnowledgeBaseID: store.chunk.KnowledgeBaseID, ChunkID: store.chunk.ID, BaseRevisionID: *store.chunk.ActiveRevisionID, Content: "不能编辑", Enabled: true, EditorUserID: uuid.New(), ActorRole: value.RoleAdmin})
    if !errors.Is(err, domainerrors.ErrValidation) { t.Fatal(err) }
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test ./internal/interfaces/http ./internal/interfaces/mcp ./internal/application/service -run 'V3Chunking|RejectsParent' -count=1`

Expected: FAIL，strict JSON/MCP schema 缺字段或 parent edit 被接受。

- [ ] **Step 3: Write minimal implementation.**

所有 HTTP/MCP 输入输出支持 `strategy`、`enable_parent_child`、`parent_chunk_size`、`child_chunk_size`、`chunk_size`、`chunk_overlap`。缺失新字段采用默认值，显式 false 不得被覆盖。`generationChunkingConfig` 序列化六字段，`decodeChunkingConfig` 兼容 v2 两字段 map 并规范化 v3 snapshot。`dto.Chunk` 增加 role/nullable parent ID；ChunkRevision service 加载 parent 后返回“父块由分块配置派生，不能直接编辑”。

- [ ] **Step 4: Run test to verify it passes.**

Run: `go test ./internal/interfaces/http ./internal/interfaces/mcp ./internal/application/service -count=1`

Expected: PASS；旧 payload 可读、新 payload 被严格校验。

- [ ] **Step 5: Commit.**

```bash
git add internal/application/dto internal/application/service internal/interfaces/http internal/interfaces/mcp
git commit -m "feat(api): 暴露父子分块与自适应配置"
```

### Task 7: 将管理台表单接入完整 v3 config

**Files:**

- Create: `web/src/components/chunking-config-fields.tsx`, `web/src/components/chunking-config-fields.test.tsx`
- Modify: `web/src/features/knowledge-bases/schemas.ts`, `web/src/features/knowledge-bases/types.ts`, `web/src/features/knowledge-bases/components/knowledge-base-form.tsx`
- Modify: `web/src/features/index-generations/generation-form-schema.ts`, `web/src/features/index-generations/generation-form.tsx`, `web/src/features/index-generations/types.ts`
- Test: `web/src/features/knowledge-bases/components/knowledge-base-form.test.tsx`, `web/src/features/index-generations/generation-form.test.tsx`
- Modify: `web/src/lib/i18n/locales/zh/knowledgeBases.ts`, `web/src/lib/i18n/locales/en/knowledgeBases.ts`, `web/src/lib/i18n/locales/zh/indexGenerations.ts`, `web/src/lib/i18n/locales/en/indexGenerations.ts`

**Interfaces:** 复用 `/indexes?create=true` 三步表单，产出带策略、父子开关和尺寸的 typed request。

- [ ] **Step 1: Write the failing test.**

```tsx
it('submits the v3 parent-child snapshot', async () => {
  const screen = await render(<GenerationForm {...props} />)
  await userEvent.click(screen.getByRole('button', { name: '下一步：分块配置' }))
  await expect.element(screen.getByLabelText('父子分块')).toBeChecked()
  expect(screen.getByLabelText('小块大小（用于召回）')).toHaveValue(384)
})
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `pnpm --dir web test -- generation-form.test.tsx knowledge-base-form.test.tsx`

Expected: FAIL，现有 schema/body 只有 size/overlap。

- [ ] **Step 3: Write minimal implementation.**

在 `web/src/components/chunking-config-fields.tsx` 新增完整 `chunkingConfigSchema` 和可复用的 `ChunkingConfigFields`，不使用 `z.record`。Generation 第 2 步和 KB 创建表单共用此组件：Select strategy、Switch parent-child、开启时 child/parent/parent-overlap，关闭时扁平 size/overlap；RHF 保留切换前的草稿，Zod 显示字段错误。复用 FormField/FormMessage/HintLabel 与 i18n，禁止改 shadcn primitive；member 仍只见现有 403。

- [ ] **Step 4: Run test to verify it passes.**

Run: `pnpm --dir web test -- generation-form.test.tsx knowledge-base-form.test.tsx && pnpm --dir web check`

Expected: PASS；关闭再开启不丢父子值，字段错误可访问。

- [ ] **Step 5: Commit.**

```bash
git add web/src/features/knowledge-bases web/src/features/index-generations web/src/lib/i18n/locales
git commit -m "feat(web): 配置自适应父子分块"
```

### Task 8: 将文件分块检查器改为层级视图

**Files:**

- Modify: `web/src/features/chunks/schemas.ts`, `web/src/features/chunks/types.ts`, `web/src/features/chunks/inspector/chunk-inspector.tsx`, `web/src/features/chunks/inspector/chunk-detail-dialog.tsx`
- Modify: `web/src/routes/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId.tsx`
- Test: `web/src/features/chunks/inspector/chunk-inspector.test.tsx`, `web/src/features/chunks/inspector/chunk-detail-dialog.test.tsx`

**Interfaces:** 消费 Task 6 Chunk role/parent ID，保留 `?chunk=<child-id>` 深链。

- [ ] **Step 1: Write the failing test.**

```tsx
it('groups children under a parent and hides parent edit', async () => {
  const screen = await render(<ChunkInspector {...props} chunks={parentAndChildren} />)
  await expect.element(screen.getByText('上下文块 1 · 2 个子块')).toBeVisible()
  expect(screen.queryByRole('button', { name: '编辑上下文块 1' })).toBeNull()
})
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `pnpm --dir web test -- chunk-inspector.test.tsx chunk-detail-dialog.test.tsx`

Expected: FAIL，平铺卡片未提供 parent group。

- [ ] **Step 3: Write minimal implementation.**

Zod schema 加 `role: z.enum(['parent','child'])` 和 `parent_chunk_id: z.uuid().nullable()`。Inspector 以 `Map<parentId, Chunk[]>` 按 parent sequence 渲染 `Collapsible` group、StatusBadge child 行和可访问按钮；只有 v2 兼容读取中的无 parent child 才归入独立分块。parent Dialog 仅显示全文、来源和 child 数；child Dialog 显示只读完整上下文并可编辑。文件路由只传 RoleChild 给 `ChunkRevisionForm`；关闭 Dialog 删除 `chunk` search 并恢复焦点。

- [ ] **Step 4: Run test to verify it passes.**

Run: `pnpm --dir web test -- chunk-inspector.test.tsx chunk-detail-dialog.test.tsx && pnpm --dir web check`

Expected: PASS；child 深链滚动、parent 只读、Dialog 焦点恢复。

- [ ] **Step 5: Commit.**

```bash
git add web/src/features/chunks web/src/routes/_authenticated/workspaces
git commit -m "feat(web): 以层级检查父子分块"
```

### Task 9: 展示父块正文与命中 child 片段

**Files:**

- Modify: `web/src/features/retrieval/schemas.ts`, `web/src/features/retrieval/retrieval-test.tsx`
- Test: `web/src/features/retrieval/schemas.test.ts`, `web/src/features/retrieval/retrieval-test.test.tsx`
- Modify: `web/src/lib/i18n/locales/zh/retrieval.ts`, `web/src/lib/i18n/locales/en/retrieval.ts`

**Interfaces:** 消费 Task 5 `content=parent` 与 `matched_children`。

- [ ] **Step 1: Write the failing test.**

```tsx
it('shows parent context and deep-links from the best child', async () => {
  const screen = await render(<RetrievalTest {...props} useResults={parentResultStub()} />)
  await expect.element(screen.getByText('完整父块正文')).toBeVisible()
  await expect.element(screen.getByText('命中片段 2')).toBeVisible()
  await expect.element(screen.getByRole('link', { name: '定位命中' })).toHaveAttribute('href', expect.stringContaining(`chunk=${childId}`))
})
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `pnpm --dir web test -- retrieval-test.test.tsx schemas.test.ts`

Expected: FAIL，Zod 不接受 `matched_children` 或 UI 无命中片段。

- [ ] **Step 3: Write minimal implementation.**

```ts
const matchedChildSchema = z.object({
  chunk_id: z.uuid(), chunk_revision_id: z.uuid(), content: z.string(),
  source_anchor: z.record(z.string(), z.unknown()), score: z.number(),
  vector_score: z.number().optional(), keyword_score: z.number().optional(),
})
```

Result schema 添加非空 `matched_children`。检索卡用现有 SafeMarkdown 完整渲染 parent content，显示“返回完整上下文”和“命中片段 {count}”；每个片段显示安全纯文本 preview、锚点、RRF 和定位 link，link 使用 `matched_children[0].chunk_id`。保留查看来源及分数不是百分比的说明。

- [ ] **Step 4: Run test to verify it passes.**

Run: `pnpm --dir web test -- retrieval-test.test.tsx schemas.test.ts && pnpm --dir web build`

Expected: PASS；普通 DOM 不输出 UUID。

- [ ] **Step 5: Commit.**

```bash
git add web/src/features/retrieval web/src/lib/i18n/locales
git commit -m "feat(web): 展示父块上下文与命中片段"
```

### Task 10: 全链路验证与文档收尾

**Files:**

- Modify: `docs/API_ACCESS.md`（仅当其中已有 SearchResult/config JSON 样例）
- Test: `internal/interfaces/worker/document_tasks_integration_test.go`, `internal/infrastructure/db/retrieval_search_integration_test.go`, `cmd/langhuan/main_test.go`, `web/src/features/knowledge-bases/v050-flow.test.tsx`

**Interfaces:** 验证 Tasks 1–9：Markdown/DOCX/PDF → v3 Generation → REST/MCP Search → Web 深链。

- [ ] **Step 1: Write the failing test.**

```go
func TestDocumentPipelinePublishesChildIndexAndParentSearchEvidence(t *testing.T) {
    ctx, db, redis := integrationStack(t)
    kb, document := importMarkdownFixture(ctx, db, redis, "# 安装\\n\\n"+strings.Repeat("配置内容。", 800))
    activateV3Generation(t, ctx, db, redis, kb.ID)
    chunks := listDocumentChunks(t, ctx, db, kb.ID, document.ID)
    if countRole(chunks, value.ChunkRoleParent) == 0 || countRole(chunks, value.ChunkRoleChild) == 0 { t.Fatalf("chunks=%#v", chunks) }
    if got := countRetrievalEntriesForRole(t, ctx, db, value.ChunkRoleParent); got != 0 { t.Fatalf("parent entries=%d", got) }
    result := searchFixture(t, ctx, kb.ID, "配置")
    if result.Content == "" || len(result.MatchedChildren) == 0 { t.Fatalf("result=%#v", result) }
}
```

- [ ] **Step 2: Run test to verify it fails.**

Run: `go test -tags=integration -p 1 ./internal/interfaces/worker ./internal/infrastructure/db ./cmd/langhuan -run 'ParentSearchEvidence|V3' -count=1`

Expected: FAIL，缺 parent/child 或 Search JSON 缺 matched children。

- [ ] **Step 3: Write minimal integration fixtures and docs.**

Markdown/DOCX fixture 验证 heading parent；MinerU fake 验证 PDF manifest headings；table fixture 验证 child 保留表头与 row anchor。HTTP/MCP 断言新 config、parent edit 拒绝和 child edit 入队；Web flow 断言层级 inspector 与 child deep link。更新已有 API 样例的 v3 config 与 `matched_children`。

- [ ] **Step 4: Run full verification.**

Run:

```bash
gofmt -w internal cmd
go test ./...
go test -tags=integration -p 1 ./internal/infrastructure/db ./internal/infrastructure/migrate ./internal/interfaces/worker ./cmd/langhuan
go vet ./...
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
git diff --check
```

Expected: 所有命令 exit 0，数据库测试只使用临时 Docker 容器。

- [ ] **Step 5: Commit.**

```bash
git add docs/API_ACCESS.md cmd internal web
git commit -m "test: 覆盖父子分块全链路"
```

## 实施前复核

- 配置、角色、迁移、ChunkSet、PDF 结构化、child-only index、父级聚合、REST/MCP、表单、检查器、检索卡与响应式测试均有任务覆盖。
- 后续类型在 Task 1、Task 5 或 Task 6 定义；所有查询 workspace-scoped；FAQ、v3 短文本的严格 parent-child 关系和 v2 flat ChunkSet 兼容读取均有明确规则。
- 每个任务先失败、后实现、再回归并单独提交；计划不连接本机长期数据库。
