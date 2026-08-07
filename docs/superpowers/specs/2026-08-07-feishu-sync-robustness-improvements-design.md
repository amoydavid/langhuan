# 飞书同步健壮性改进设计规格

## 目标

在已上线的飞书知识库同步功能基础上，增强增量判断精度、安全降级能力和用户可控性，使同步机制在面对飞书 API 边界情况（限流、内容漂移、大规模删除）时行为更加可靠，同时保持对现有架构的最小侵入。

## 动机

当前同步实现存在以下可改进点：

1. **增量判断逻辑分散**：cursor 跳过、删除检测、节点处理散落在 `syncNode` / `syncDocxNode` / `softDeleteMissingDocuments` 三处，缺乏可独立测试的 diff 单元。
2. **仅依赖 EditTime 判断变更**：飞书文档编辑后回退时 EditTime 更新但内容未变，造成不必要的重新拉取和索引重建。
3. **无全量强制同步入口**：修改切块/embedding 配置后无法通过 API 触发全量重建，只能手动清空 `sync_cursor`。
4. **远端目录树不完整时可能误删**：ListTree 分页异常或限流导致返回不完整时，本地文档会被误判为"飞书侧已删除"。
5. **删除策略单一**：飞书侧文档删除后统一软删，用户无法选择彻底清理（含向量和对象存储）。
6. **无内容大小上限保护**：异常大的飞书文档（如嵌入大量图片的 docx 转 markdown 后膨胀）可能导致内存压力和 OOM。

## 关键决策

1. **diff 抽取为同包纯函数，不改动外部接口**。`diff()` 放在 `internal/application/service/source_sync.go` 同文件或同包的 `diff.go` 中，输入远端节点列表 + 本地文档视图 + cursor + force 标志，输出 `SyncPlan{ToAdd, ToUpdate, ToRemove, Skipped}`。纯函数无 I/O，table-driven test 覆盖所有分支。

2. **content_hash 放在 `documents` 表而非仅依赖 revision**。虽然 `document_revisions.sha256` 已有内容哈希，但 diff 阶段需要不 join revision 表即可获取哈希值。在 `documents` 表新增 `content_hash` 字段，每次拉取内容后计算 SHA256 写入。

3. **force 模式通过 HTTP API 参数传入，不改动 worker 任务格式**。`POST .../sync` 增加可选的 `force` 请求体字段，透传到 `EnqueueSync` → payload → `SyncKnowledgeBase`。force 模式下所有 docx 节点都走 Fetch + hash 比对，hash 未变也重建（覆盖配置变更场景）。

4. **安全降级（skipRemoval）是同步引擎内置保护，不可配置**。触发条件：远端文档数 < 本地文档数 / 2 且本地非空时，本次同步跳过删除操作，仅执行新增和更新，并记录 warn 日志。这是一个安全网，不应被关闭。

5. **on_delete 策略存储在 `knowledge_bases.source_config` 中**。默认 `keep`（与当前行为一致：软删保留记录），可选 `remove`（彻底删除 document + revisions + file_tree_nodes + raw storage + 向量）。不新增独立列，避免 migration 复杂度。

6. **内容大小上限通过 `config.yaml` 配置，默认 50MB**。在 `source_sync` 配置节新增 `max_content_bytes`（默认 52428800）。超限文档拉取后检查，超过则跳过并记 warn 日志，不写入 rawStore、不创建 document。

7. **表格类型（sheet/bitable）明确排除，后续单独处理**。当前仅处理 `objType=="docx"` 的飞书文档。sheet/bitable 涉及异步导出 + 轮询 + 多子表拆分，复杂度不适合与本次改进混在一起。代码中对非 docx 类型的 `warn + skip` 行为保持不变，spec 中明确记录此限制。

## 数据模型变更

### 扩展 `documents`

```text
+ content_hash   text   -- 拉取内容 SHA256（hex），用于增量去重；上传文档为空
```

不为 `content_hash` 建独立索引 —— diff 阶段通过 `ListDocumentsByKB` 全量拉取已在 `external_id` 部分索引覆盖范围内。

### 扩展 `knowledge_bases.source_config`（JSONB，无 DDL）

`source_config` 新增可选键：

```json
{
  "on_delete": "keep"       // "keep"（默认）或 "remove"
}
```

- `keep`：飞书侧删除后软删文档（`deleted_at` 非空），保留记录和 file_tree_node。
- `remove`：飞书侧删除后彻底清理 —— 软删文档 + 删 file_tree_node + 删 chunks + 删向量 + 删 raw storage 对象。
- 不传或非法值 → 默认 `keep`。

## 配置变更

```yaml
# config.yaml
source_sync:
  scheduler_interval_seconds: 60
  max_concurrent_per_connection: 2
  max_content_bytes: 52428800   # 新增：单文档内容大小上限（字节），默认 50MB
```

## 改进项详细设计

### 改进 1：独立 diff 纯函数

**现状**：增量判断逻辑分散在 `syncNode`（行 429 cursor 跳过）、`syncDocxNode`（Fetch 入口）、`softDeleteMissingDocuments`（删除检测）。

**目标**：抽取 `diff()` 纯函数，单一入口输出完整同步计划。

**设计**：

```go
// localDocView 是 diff 的本地输入视图（从 Document 投影）。
type localDocView struct {
    DocumentID  uuid.UUID
    ExternalID  string
    ContentHash string
}

// updateCandidate 是待更新候选（远端节点 + 本地文档投影）。
type updateCandidate struct {
    Remote model.ExternalNode
    Local  localDocView
}

// syncPlan 是 diff 输出。
type syncPlan struct {
    ToAdd    []model.ExternalNode
    ToUpdate []updateCandidate
    ToRemove []localDocView
    Skipped  int
}

// diff 计算远端目录树与本地文档的差异。纯函数，无 I/O。
// 使用全局 cursor（KB.source_config.sync_cursor）做增量过滤：
//   EditTime > cursor → ToUpdate；EditTime ≤ cursor → Skipped。
// force=true 时所有两边都有的 docx 节点进 ToUpdate，绕过 cursor 门槛。
func diff(
    remote []model.ExternalNode,
    local  []localDocView,
    cursor time.Time,
    force  bool,
) syncPlan
```

**规则**：

| 远端 | 本地 | 条件 | 结果 |
|------|------|------|------|
| 有 | 无 | — | ToAdd |
| 无 | 有 | — | ToRemove |
| 有 | 有 | force=true | ToUpdate |
| 有 | 有 | EditTime > cursor | ToUpdate |
| 有 | 有 | EditTime ≤ cursor | Skipped |

**变更文件**：
- 新增 `internal/application/service/source_sync_diff.go`：`localDocView`、`updateCandidate`、`syncPlan`、`diff()`。
- 修改 `internal/application/service/source_sync.go`：`SyncKnowledgeBase` 中移除内联 cursor 跳过逻辑，改为调用 `diff()` 后按 plan 执行。

**测试**：`internal/application/service/source_sync_diff_test.go`，table-driven 覆盖空远端/空本地/force/cursor 零值/去重。

### 改进 2：ContentHash 去重

**现状**：增量判断仅基于 `EditTime > cursor`。飞书文档编辑后回退导致 EditTime 更新但内容未变 → 假阳性，不必要的重新拉取和索引重建。

**目标**：Fetch 后计算 SHA256 hash，与本地已有 hash 比对。hash 未变时跳过重建 —— cursor 推进后该节点在后续同步中自然被过滤。

**流程变更**（仅影响 `syncDocxNode` → 拆分为 `addDocument` + `updateDocument`）：

```text
syncDocxNode (existing, no change for new documents):
  1. Fetch content from feishu
  2. Check size <= max_content_bytes (see improvement 6)
  3. Compute contentHash = sha256(content)
  4. If new document (ToAdd):
     a. Put to rawStore
     b. Create Document{content_hash} + Revision + FileTreeNode + Job (in tx)
     c. Enqueue parse
  5. If update candidate (ToUpdate):
     a. If contentHash == local.contentHash AND !force:
        → skip rebuild（cursor 推进后该节点在后续同步中自然跳过）
     b. If contentHash != local.contentHash OR force:
        i.  Put to rawStore (overwrite)
        ii. Update Document.content_hash, reset status → pending
        iii. Create new Revision (revision_no++)
        iv. Enqueue parse
```

**新增/修改方法**：
- `SourceSyncTx` 接口新增 `UpdateDocumentForResync(ctx, doc, revision, job)` 用于 force 重建的事务写入（覆盖现有 document 的 content_hash + 创建新 revision + 创建 job）。
- 不需要单独的 `UpdateDocumentContentHash` —— hash 未变时直接跳过，无需写库。

**变更文件**：
- `internal/domain/model/document.go`：`Document` 加 `ContentHash` 字段。
- `internal/infrastructure/db/document_row.go`：Row 加 `ContentHash` 列。
- `internal/infrastructure/migrate/migrations/`：新迁移文件 `ALTER TABLE documents ADD COLUMN content_hash TEXT`。
- `internal/application/service/source_sync.go`：`syncDocxNode` 按 plan 分派 add/update 路径。
- `internal/application/service/source_sync_store.go`：扩展接口。

### 改进 3：Force 全量同步模式

**现状**：仅支持增量同步，无 API 绕过增量门槛。

**目标**：`POST .../sync` 支持 `{"force": true}`，强制所有 docx 节点进 ToUpdate，且 content_hash 未变也触发重建。

**API 变更**：

```
POST /api/v1/workspaces/:slug/knowledge-bases/:id/sync
Content-Type: application/json

{"force": true}    // 可选，默认 false

→ 202 {"job_id": "..."}
```

**数据流**：`handler` → `EnqueueSync`（payload 加 `force`）→ worker → `SyncKnowledgeBase`（传入 force）→ `diff(remote, local, cursor, force=true)` → 所有已有文档进 ToUpdate → `updateDocument(force=true)` 时 hash 未变也重建。

**变更文件**：
- `internal/interfaces/http/knowledge_base_sync_handler.go`：body 解析 `force`。
- `internal/application/service/source_sync.go`：`EnqueueSync` payload + `SyncKnowledgeBase` 签名。
- `internal/interfaces/worker/source_sync_tasks.go`：payload decode。

### 改进 4：安全降级保护

**现状**：ListTree 返回的目录树不完整时（分页异常、限流截断），所有不在树中的本地文档会被误判为"飞书侧已删除"并软删。

**目标**：内置保护 —— 远端文档数明显少于本地时跳过删除操作。

**规则**：

```go
skipRemoval := len(local) > 0 && len(remote) > 0 && len(remote)*2 < len(local)
```

触发时：
- 只执行 ToAdd 和 ToUpdate，跳过所有 ToRemove。
- sync 状态标记为 `partial`（诚实反映降级）。
- 记 warn 日志，含 `remote_count`、`local_count`。

此保护不可配置，是硬编码的安全网。

**变更文件**：
- `internal/application/service/source_sync.go`：`SyncKnowledgeBase` 中在 diff 后、apply 前加入判断。

### 改进 5：on_delete 策略

**现状**：飞书侧文档删除后统一软删（`deleted_at` 非空），保留所有关联数据。

**目标**：用户可在知识库创建/编辑时选择删除策略。

**配置位置**：`knowledge_bases.source_config.on_delete`，可选值：

| 值 | 行为 |
|----|------|
| `keep`（默认） | 软删文档（设 `deleted_at`），保留 file_tree_node、chunks、向量、raw storage |
| `remove` | 软删文档 + 删 file_tree_node + 删 chunks + 删向量 + 删 raw storage |

**实现**：在 `softDeleteMissingDocuments` 中按 `on_delete` 分派：
- `keep` → 现有逻辑不变（`SoftDeleteDocument`）。
- `remove` → 新事务方法 `HardDeleteDocument(ctx, docID)`，包含：删 file_tree_node、删 chunks、删向量（调 index adapter）、删 raw storage（调 storage adapter）、软删 document。

**变更文件**：
- `internal/application/service/source_sync_store.go`：`SourceSyncTx` 接口新增 `HardDeleteDocument`。
- `internal/application/service/source_sync.go`：`softDeleteMissingDocuments` 读 `on_delete` 分派。
- `internal/infrastructure/db/`：Repository 实现 `HardDeleteDocument`。

### 改进 6：内容大小保护

**现状**：无内容大小上限检查。极端大的飞书文档可能导致内存压力和 OOM。

**目标**：Fetch 后、Put rawStore 前检查大小，超限跳过。

**配置**：`source_sync.max_content_bytes`（默认 52428800 = 50MB）。

**检查点**：在 `syncDocxNode` 中，`connector.Fetch` 返回后、`rawStore.Put` 之前：

```go
if len(markdown) > maxContentBytes {
    s.logger.Warn("飞书文档过大，跳过同步",
        "external_token", external.Token,
        "size_bytes", len(markdown),
        "max_bytes", maxContentBytes,
    )
    return nil  // 跳过，不创建 document
}
```

超限跳过不计入失败计数（`failed`），因为这是预期内的保护行为。

**变更文件**：
- `internal/infrastructure/config/config.go`：`SourceSyncConfig` 加 `MaxContentBytes`。
- `config.example.yaml`：加注释说明。
- `internal/application/service/source_sync.go`：`SourceSyncService` 加 `maxContentBytes` 字段，`syncDocxNode` 中检查。

### 明确排除：表格类型（sheet/bitable）

飞书电子表格和多位表格的同步涉及异步导出任务（飞书 export_task API 需创建导出 → 轮询状态 → 下载）以及多子表拆分逻辑，与 docx 的同步拉取模型差异显著。本次改进不涉及表格类型处理，当前代码中对非 docx 节点的 `warn + skip` 行为保持不变。表格类型同步将在后续独立设计。

## API 变更汇总

| 端点 | 变更 |
|------|------|
| `POST .../sync` | 请求体新增可选 `force: bool`（默认 false） |
| `POST .../knowledge-bases` | `source_config` 新增可选 `on_delete` 字段（`"keep"` / `"remove"`，默认 `"keep"`） |
| `PATCH .../knowledge-bases/:id` | 同上，允许修改 `source_config.on_delete` |

无破坏性变更。`force` 和 `on_delete` 均有默认值，现有调用方不受影响。

## 风险与取舍

1. **content_hash 在 document 上而非 revision 上的冗余**：每次内容变更需同时更新 `documents.content_hash` 和 `document_revisions.sha256`。这是有意为之的冗余 —— diff 阶段需要不 join revision 表即可获取哈希值，否则 `ListDocumentsByKB` 需要 join 或 N+1 查询，损害性能。两个字段的写入在同一事务中，不存在不一致窗口。

2. **force 模式下所有 docx 节点重新 Fetch**：对大型知识库可能触发大量飞书 API 调用。通过 per-connection 限流保护。后续可考虑先比较 `remote_modified_at` 做第一层过滤，但首版 force 保持简单。

3. **安全降级阈值硬编码为 `2×`**：`len(remote)*2 < len(local)` 是一个经验阈值。如果飞书侧确实批量删除了超过一半的文档，则需两次同步才能完全删除（第一次 skipRemoval 触发，第二次因 local 减少不再触发）。这是有意为之的安全偏保守策略。

4. **`remove` 策略的 raw storage 清理依赖 storage adapter**：如果 raw storage 删除失败，记 warn 日志但不回滚文档软删 —— 孤儿对象可由后续存储清理任务处理，不应阻塞同步流程。

5. **表格类型排除**：用户可能在飞书目录中放置 sheet/bitable，同步结果中这些文件无对应文档记录（仅 warn 日志）。需在 UI 中明确提示"仅支持飞书文档（docx）类型"，后续版本再扩展。

## 验收标准

1. `diff()` 纯函数有完整的 table-driven test，覆盖空输入、全新增、全删除、增量跳过、force 全量、重复 token 去重。
2. `content_hash` 比对正确：内容未变 → 跳过重建（不创建新 revision、不入队解析），cursor 推进后该节点在后续同步中自然跳过；内容变更 → 正常重建。
3. `force=true` 同步时所有已有文档走重建路径（新建 revision + 入队解析），不受 cursor 或 content_hash 限制。
4. 远端文档数 < 本地文档数 / 2 时跳过删除操作，同步状态标记为 `partial`，记 warn 日志。
5. `on_delete=remove` 时飞书侧删除的文档被彻底清理（document、file_tree_node、chunks、向量、raw storage 均清理）。
6. 超过 `max_content_bytes` 的文档被跳过（记 warn），不影响其他文档同步，不计入失败计数。
7. 迁移文件 `up/down` 正确，`documents` 表新增 `content_hash` 列。
8. 现有同步功能的回归测试通过（增量同步、多应用限流、cron 定时、手动触发）。

## 分阶段交付

- **阶段 1**：diff 纯函数抽取 + 单元测试。（不改行为，仅重构）
- **阶段 2**：数据模型变更 —— `content_hash` 迁移 + Document 模型 + Repository + 迁移文件。
- **阶段 3**：同步引擎改进 —— content_hash 去重 + force 模式 + skipRemoval + on_delete + max_content_bytes。含 `SourceSyncStore` 接口扩展和 Repository 实现。
- **阶段 4**：HTTP API + worker payload 适配（force 参数透传）。
- **阶段 5**：集成测试覆盖所有改进项的端到端行为。
