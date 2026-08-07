# 飞书同步健壮性改进设计规格

## 1. 目标与范围

在现有飞书知识库同步能力上，修正增量同步的生命周期和失败恢复语义，降低飞书目录快照不完整、内容漂移、限流、大文档和大规模删除造成的数据风险。

本版本覆盖：

- 将远端目录快照、差异计算和应用结果分层，避免不完整快照触发删除；
- 复用同一个外部文档的稳定 `Document` 身份，以内容 hash 进行增量去重；
- 增加可排队、可观测、可重试的 force 同步；
- 为同步失败、超限和解析失败保留重试机会；
- 增加 `keep` / `remove` 删除策略，其中外部对象清理异步执行；
- 在 connector 读取阶段和 application 阶段实施内容大小限制；
- 保持 sheet/bitable 等非 `docx` 类型暂不导入。

本版本不覆盖：

- sheet/bitable 的异步导出和多子表建模；
- 自动创建或激活新的 Index Generation；
- 对已有人工编辑 Chunk 的冲突处置；
- 图查询或新的检索后端。

force 的准确语义是“在当前 active Generation 下重新获取并重新进入现有 parse/chunk/index 管线”。如果切块或 embedding 配置发生变化，调用方必须先通过现有 Generation API 创建并激活目标 Generation，再触发 force；force 本身不改变 Generation。

## 2. 当前实现约束

实现必须遵守以下已有合同：

- 资源属于 `workspace`，所有 DB 操作进入 `WithinWorkspace`；
- `domain` 和 `application` 不持有 `*gorm.DB`；
- `Document` 与 GORM Row 双层分离；
- `DocumentRevision` 不可变，更新必须创建新 revision；
- `retrieval_entries` 是可重建投影，向量/FTS 写入仍由现有 infrastructure/db 实现；
- 同步 worker 只解码 payload 并调用 application service；
- 数据库测试只能使用测试运行期临时启动的 pgvector/zhparser 容器；
- 不把外部对象存储删除假装成 PostgreSQL 事务的一部分。

当前代码中的以下行为必须在本版本修正，而不是继续沿用：

- 每次同步都新建 Document UUID；
- 更新路径固定使用 `RevisionNo=1`；
- folder 节点没有远端稳定 token，重复同步可能产生名称冲突；
- cursor 可能在节点失败后越过失败节点；
- `POST .../sync` 的 queue TaskID 按 KB 固定，无法表达 force 与普通同步的合并关系。

## 3. 术语与同步状态

### 3.1 远端目录快照

connector 返回 `TreeSnapshot`，而不是裸 `[]ExternalNode`：

```go
type TreeSnapshot struct {
    Nodes       []model.ExternalNode
    Complete    bool
    Warnings    []string
    MaxEditTime time.Time
}
```

语义：

- `Complete=true`：connector 已确认根节点下的可见目录遍历完成，允许执行删除检测；
- `Complete=false`：分页被截断、可恢复限流、部分子树失败或返回结果无法证明完整，只允许新增和更新，禁止删除；
- 鉴权失败、根不存在、连接不可用等无法安全使用快照的错误仍返回 error，不应用任何节点；
- 空且完整的快照是合法结果，表示远端根下没有可见节点；空但不完整的快照不得解释为“远端为空”。

recoverable 的分页/限流问题由 adapter 返回 `TreeSnapshot{Complete:false}` 并附 warning；service 继续应用 snapshot 中可用节点，但同步结果为 `partial`。

### 3.2 同步结果

现有通用 `JobStatus` 不增加 `partial`，仍使用 `succeeded` / `failed`。同步结果另存于 `knowledge_bases.source_config.sync_last_result`：

```json
{
  "status": "succeeded|partial|failed",
  "complete": true,
  "synced_documents": 12,
  "skipped_documents": 30,
  "failed_documents": 1,
  "oversize_documents": 0,
  "unsupported_nodes": 2,
  "deleted_documents": 0,
  "cleanup_pending": 0,
  "finished_at": "2026-08-07T12:00:00Z"
}
```

写入该字段必须使用保留其它 `source_config` 键的 JSONB 更新。`partial` 表示本次结果可用但删除或部分节点处理被保护性跳过；它不表示 worker 任务失败。

ListTree 鉴权失败、根不存在等 fatal error 返回前，也应 best-effort 写入 `status=failed`、`complete=false` 和 `finished_at`；若该结果写入本身失败，保留原错误链并记录结构化日志。单个节点的 Fetch/存储/入队失败产生 partial 结果，继续处理其它独立节点；成功或 partial 的 source sync Job 标记 succeeded，无法取得可用 snapshot 或无法维持 Workspace/KB 一致性的 fatal error 才把 Job 标记 failed。

## 4. 数据流

```text
ListTree
  -> TreeSnapshot(Complete/Warn/MaxEditTime)
  -> List local source documents + source folders
  -> dedupe + diff
  -> apply folders/documents
       add: Fetch(limited) -> hash -> raw object -> Document/Revision/Job
       update: reuse Document -> Fetch(limited) -> hash -> new or retried Revision/Job
       unchanged: no new Revision, no parse Job
  -> if snapshot.Complete: apply deletion policy
  -> compute safe cursor watermark
  -> write sync_last_result + cursor
```

每一个节点处理结果都分类为：

- `success`：新增/更新事务和 parse 入队成功；
- `unchanged`：hash 相同且本地没有待重试状态；
- `skipped`：cursor 已覆盖且没有待重试状态；
- `retryable_failure`：Fetch、raw store、DB 或 enqueue 失败；
- `oversize`：内容超过限制，保留旧版本并允许后续重试；
- `unsupported`：非 docx，记录 warning，不创建 Document。

## 5. 远端节点与本地投影

### 5.1 稳定标识

远端 `ExternalNode.Token` 必须非空且在一次 snapshot 内唯一。重复 token：

1. 保留第一次出现的节点；
2. 丢弃后续重复项；
3. 添加 warning 和指标；
4. 重复 token 不得进入删除集合。

迁移前若已存在重复本地 `documents.external_id` 或 folder token，迁移应 fail fast 并报告冲突 token，不自动删除或合并业务数据；管理员处理冲突或按 v1.0 前策略重建测试库后再执行迁移。迁移之后建立唯一索引，禁止继续产生重复身份。

### 5.2 Folder 持久化

`file_tree_nodes` 新增 `external_id`，folder 和 file 节点均保存远端 token：

- folder 以 `(workspace_id, knowledge_base_id, node_type='folder', external_id)` upsert；
- file 以 `document_id` 查找并更新 `parent_id/name`；
- folder 的 `parent_id/name` 在每次完整或部分 snapshot 中按 token 更新；
- token 缺失或 parent 不可解析的节点记为失败，不推进其后续安全水位；
- file tree 节点不承担内容 hash、revision 或权限信息。

完整 snapshot 还需要计算 folder 差异：

- 远端仍存在的 folder 走 upsert；
- 远端不存在的 folder 在 document 删除/移动和 file node 更新完成后，按目录深度从深到浅尝试删除；
- 空 folder 可以删除；仍包含因 `on_delete=keep` 保留的 file node 或其它子节点时保留；
- partial snapshot 不删除任何 folder；
- folder 删除失败只影响该节点并把同步结果标记 partial，不回滚已经成功的文档同步。

上传创建的 FileTreeNode 保持 `external_id=NULL`。飞书节点使用以下部分唯一索引：

```sql
CREATE UNIQUE INDEX uq_file_tree_nodes_kb_external
ON file_tree_nodes (workspace_id, knowledge_base_id, external_id)
WHERE external_id IS NOT NULL AND external_id <> '';
```

Document 使用独立索引，允许 FileTreeNode 和其关联 Document 保存同一个远端 token：

```sql
DROP INDEX IF EXISTS idx_documents_kb_external;
CREATE UNIQUE INDEX uq_documents_workspace_kb_external
ON documents (workspace_id, knowledge_base_id, external_id)
WHERE external_id IS NOT NULL AND external_id <> '';
```

### 5.3 本地 diff 视图

```go
type localDocView struct {
    DocumentID       uuid.UUID
    ExternalID       string
    ContentHash      string
    Status           value.DocumentStatus
    ActiveRevisionID *uuid.UUID
    RevisionNo       int64
    RetryRequired    bool
    DeletedAt        *time.Time
}

type updateCandidate struct {
    Remote model.ExternalNode
    Local  localDocView
}

type syncPlan struct {
    ToAdd    []model.ExternalNode
    ToUpdate []updateCandidate
    ToRemove []localDocView
    Skipped  int
    Warnings []string
}
```

`RetryRequired=true` 的文档包括：Document 处于 failed、最新 source Revision 没有成功完成、或对应 parse Job 入队/执行失败。已软删但远端重新出现的文档必须进入 `ToUpdate`，并在成功更新时恢复 `deleted_at=NULL` 和可用状态。

### 5.4 diff 规则

`diff()` 是同包纯函数，无 I/O。输入为去重后的 `TreeSnapshot`、本地视图、cursor 和 force；输出不负责删除外部对象，也不修改 cursor。函数直接读取 `snapshot.Complete` 生成或抑制 `ToRemove`，不接受可被调用方错误传入的独立 `allowRemoval` 参数。

| 远端 | 本地 | 条件 | 结果 |
|---|---|---|---|
| 有 | 无 | — | `ToAdd` |
| 无 | 有且未删除 | `snapshot.Complete=true` | `ToRemove` |
| 无 | 有且已删除 | — | 忽略 |
| 有 | 有 | `force=true` | `ToUpdate` |
| 有 | 有 | `RetryRequired=true` | `ToUpdate` |
| 有 | 有 | EditTime 未知 | Fetch 后 hash 判断 |
| 有 | 有 | EditTime > cursor | `ToUpdate` |
| 有 | 有 | EditTime <= cursor 且无重试需求 | `Skipped` |

`EditTime` 零值永远不能用于推进 cursor；零值节点每次同步都可以 Fetch，由 hash 决定是否重建。

### 5.5 删除闸门

删除只在 `snapshot.Complete=true` 时执行。`Complete=false` 时：

- 过滤掉全部 `ToRemove`；
- 保留新增和更新；
- 结果写为 `partial`；
- warning 包含 `remote_doc_count`、`local_doc_count`、snapshot warnings。

数量比较仅用于告警，不改变删除动作：当远端 docx 数小于本地未删除 docx 数的一半时，即使 snapshot 完整，也记录高优先级 warning。删除依据仍然只有完整快照；空完整快照同样可以删除全部远端已删除文档。

## 6. Content Hash 与稳定更新

### 6.1 数据模型

`documents` 新增：

```text
content_hash text NULL
```

它表示“当前已接受的最新 source revision 的内容 SHA-256”，不是“已成功发布到检索索引”的 hash。上传文档保持 NULL。

`document_revisions.sha256` 仍保存 revision 事实，两者在创建/更新 revision 的同一 DB 事务中写入。hash 相同但 Document/Revision 处于待重试状态时，不得仅凭 hash 跳过。

### 6.2 新文档

1. connector 在受限读取下返回 markdown；
2. application 计算 SHA-256，并检查实际字节数；
3. application 预分配 revision ID，并用该 ID 构造 revision 唯一 raw object key 后写入；
4. 同一 Workspace 事务创建 Document、FileTreeNode、Revision、parse Job；
5. 事务提交后入队 parse；入队失败时标记 Document/Revision/Job failed，且不推进安全 cursor。

### 6.3 已有文档

1. 按 `(workspace_id, knowledge_base_id, external_id)` 锁定现有 Document；
2. Fetch 后计算 hash；
3. hash 相同且 `RetryRequired=false` 且非 force：不创建 revision，不入队 parse；
4. hash 变化或 force：在同一事务中计算 `revision_no = max + 1`，创建新 Revision 和 parse Job，更新 Document 的 `content_hash/status`；`active_revision_id` 继续由现有 pipeline 在新 revision 成功发布时切换；
5. hash 相同且 `RetryRequired=true` 且非 force：复用最新的未完成/失败 Revision，重新创建或重置幂等 parse Job，不创建内容相同的新 Revision；
6. 更新 FileTreeNode 的标题和父节点；
7. 新 revision 的 raw storage key 不得覆盖旧 revision；
8. 若事务提交成功但队列入队失败，保留新 revision 但标记失败，下一次同步必须因 `RetryRequired=true` 重试。

`UpdateDocumentForResync` 必须是 application 定义的最小 transaction contract，不能由 service 直接操作 GORM。该方法必须锁定 Document，校验 workspace/KB/external lineage，并保证 revision 序号递增。

为支持 revision 唯一对象路径，`RawDocumentInput` 增加 `RevisionID`，local/S3 adapter 都把它纳入 key。领域层提供可显式传入预分配 ID 的 revision 构造入口；预分配 ID 不代表 revision 已落库，raw 写入或 DB 事务失败时仍执行现有补偿删除。

### 6.4 失败与 cursor

完整 snapshot 中，同步节点按远端 `EditTime` 升序形成安全前缀。只有成功、unchanged、明确 cursor skip 的连续节点才能推进 watermark；在第一个 retryable failure/oversize/parent failure 处停止推进，后续已处理节点允许下次重复检查。同一 EditTime 的节点必须全部成功后才能把 cursor 推进到该时间。零值 EditTime 不参与 watermark。

`snapshot.Complete=false` 时全局 cursor 完全不推进。partial snapshot 中已成功处理的节点允许下次通过 hash 再次确认，以换取确定性的失败恢复。

因此：

- Fetch 失败不会被 cursor 永久越过；
- 超限文档保留旧版本且下次仍会尝试；
- ListTree 不完整时完全不推进 cursor；
- cursor 更新失败只记录错误，不把本次同步报告为完全成功。

## 7. 内容大小保护

### 7.1 配置

```yaml
source_sync:
  scheduler_interval_seconds: 60
  max_concurrent_per_connection: 2
  max_content_bytes: 52428800
```

默认 50 MiB。必须在配置 defaults 和 validation 中校验 `>0`，并在 `config.example.yaml` 说明该限制作用于 connector 返回的 markdown 字节数。

### 7.2 connector 层限制

为避免“完整 Fetch 后才检查”仍然造成 OOM，端口改为：

```go
type FetchOptions struct {
    MaxContentBytes int64
}

Fetch(ctx context.Context, conn model.SourceConnection, externalID string, options FetchOptions) (model.FetchedDocument, error)
```

Feishu HTTP adapter 必须使用 response body 上限/流式读取；超过上限时停止读取并返回可识别的 oversize error。application 层仍对最终 `len(markdown)` 做第二次检查，防止 adapter 实现错误。

### 7.3 超限语义

- 新文档超限：不创建 Document，不写 raw store；
- 已有文档超限：保留当前 Document/Revision，不覆盖 raw，不更新 hash；
- 超限计入 `oversize_documents` 和 warning，不计入外部 API failure；
- 超限节点不推进 cursor，后续可通过调高配置或 force 重试。

## 8. Force 同步与队列语义

### 8.1 API

```http
POST /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/sync
Content-Type: application/json

{"force": true}
```

请求体可省略，默认 `force=false`。响应保持 `202 {"job_id":"..."}`。

HTTP service 通过 `SyncOptions{Force bool}` 把用户意图传给 application；application 将 force 写入 DB latch。worker payload 继续携带 workspace/KB/job lineage，worker 从 DB 原子消费 force latch，不依赖清空 cursor，也不尝试修改已入队的 asynq payload。

### 8.2 同一 KB 的并发与合并

同一 `(workspace, knowledge_base)` 仍最多一个 source sync 任务处于 pending/running。普通同步和 force 同时请求时：

- 新增 `knowledge_bases.source_config.sync_requested_force` 布尔 latch，缺省 false；
- 每次 enqueue 在 Workspace 事务中锁定 KB，并把 latch 更新为 `existing OR requested_force`；
- 若已有 pending/running source_sync Job，不创建第二个 Job，直接返回该 job_id；
- worker 开始一次同步前，以原子操作读取并清除 latch；本轮 `force` 取被消费的值；
- 同步执行期间又收到 force 请求时，latch 会重新变为 true；worker 完成本轮后检查 latch，若为 true 则原子创建并入队下一轮 source_sync Job；
- 普通请求不能把已有 force 意图降级；多个 force 请求可合并为一次后续 force；
- DB Job 是幂等和并发判断的权威来源，queue TaskID 使用 job_id，避免修改已入队 asynq payload；
- DB Job 创建成功但 asynq enqueue 失败时必须把 Job 标记 failed，并保留 latch，供 scheduler/下一次请求重新派发；
- worker 完成时必须在同一个 KB 锁定事务中标记当前 Job 终态、检查 latch 并按需创建后续 Job，避免“检查 latch 后、标记完成前”到达的 force 请求丢失。

调度器的 cron/manual 首次同步默认 `force=false`，手动 force 只由 API 触发。Meta Scheduler 启动和周期扫描时还必须检查“latch=true 且没有 active source_sync Job”的 KB，并重新创建/派发任务，保证 enqueue 失败后 force 意图不会永久滞留。

### 8.3 force 处理规则

- 所有远端 docx 与本地 Document 进入 Fetch；
- hash 未变也创建新 revision 并入队 parse；
- 远端新 docx 仍走 `ToAdd`；
- snapshot 不完整时 force 也禁止删除；
- force 不修改 active Generation，不绕过 Generation 的配置快照和发布校验。

## 9. 删除策略与异步清理

### 9.1 配置

`knowledge_bases.source_config.on_delete` 允许：

- `keep`（默认）：Document 设为 deleted，保留 revision、chunks、retrieval entries、file tree 和对象，以便审计/恢复；查询必须继续排除 deleted Document；
- `remove`：先在 DB 事务内删除 Document，利用现有外键 cascade 删除 file tree、revisions、chunks、chunk revisions、retrieval entries 和相关 Jobs，并在事务中收集待删除的 raw/parser/asset storage keys。

两种策略都必须让知识库摘要、Generation 统计和检索结果反映当前未删除文档集合：`keep` 通过查询过滤 deleted Document，`remove` 在级联删除后调用现有统计刷新能力。删除策略不修改 Generation 的不可变配置快照。

读取历史数据时，非法值和缺失值均按 `keep` 处理；create/source-policy API 收到非法值时返回 validation error，不静默纠正用户输入。读取和写入策略使用小型 value parser，不在各 handler/service 中重复字符串判断。

### 9.2 外部对象清理

对象存储不参与 DB 事务。`remove` 的应用流程：

1. 在 Workspace DB 事务中锁定 Document，收集所有 revision raw keys、parser raw keys 和 document asset keys；
2. 创建 `source_cleanup` KB-scoped Job（payload 携带 workspace/KB/document/keys）；若对象 key 数量超过具名批大小，则拆成多个 cleanup Job，避免单个 DB/Redis payload 无界增长；
3. 删除 Document，依靠 FK cascade 清理数据库投影；cleanup Job 只关联 KB，不引用即将删除的 Document FK；
4. 事务提交后入队 cleanup task；
5. cleanup worker 逐个幂等删除对象，失败保留 Job failed 并允许重试；
6. `sync_last_result.cleanup_pending` 统计尚未完成的清理任务。

迁移必须扩展 jobs 的 target check，明确允许 `source_cleanup` 仅关联 KB；新增 worker handler 只解码和转发 application cleanup service。Meta Scheduler 启动和周期扫描时必须重新派发仍为 pending 的 cleanup Job，保证“DB 已提交、首次 enqueue 失败”不会形成永久孤儿任务。不得在 `SourceSyncTx` 内调用 storage/index adapter，也不得在删除失败时宣称“全部清理完成”。

### 9.3 删除恢复

远端文档重新出现时：

- `keep` 文档复用原 Document ID，清空 `deleted_at`，创建新 revision（或在 hash 未变且旧 revision ready 时直接恢复）；
- `remove` 文档因身份已清理，按新文档创建；
- cleanup 尚未完成时仍允许同 external token 新建身份；新的 Document/Revision 使用新 UUID 和 revision 唯一对象 key，因此不会覆盖待清理对象。

### 9.4 API 修改

现有 `PATCH /knowledge-bases/:id` 只更新 name/description，不直接接收整个 `source_config`。新增独立的 typed API：

```http
PATCH /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/source-policy
{"on_delete":"remove"}
```

该操作要求 admin/owner，并只允许修改 `on_delete`，不得覆盖 `root_token`、`sync_cursor`、`next_sync_at`。从 `keep` 改为 `remove` 不自动清理历史 deleted 文档；如需清理，使用单独的显式 cleanup endpoint/任务。

创建飞书知识库时仍允许在 `source_config.on_delete` 传入初始策略，但 application 必须使用同一个 typed parser 校验；缺失时写入或读取为 `keep`。

## 10. API、端口与文件变更

### Application / domain

- `internal/domain/model/document.go`：增加 `ContentHash`；补充稳定 external identity 的构造/恢复语义；
- `internal/domain/model/file_tree_node.go`：增加 `ExternalID`；
- `internal/domain/model/job.go`：允许 `source_cleanup` 使用仅关联 KB 的 Job lineage；
- `internal/domain/value/source_delete_policy.go`：定义 `keep/remove` 解析和值校验；
- `internal/application/service/source_sync_diff.go`：`diff()`、去重和安全 watermark 纯函数；
- `internal/application/service/source_sync.go`：按 snapshot、plan、node outcome 编排；
- `internal/application/service/source_sync_store.go`：增加稳定文档 upsert、revision 递增、force latch、删除快照收集、sync result 更新合同；
- `internal/application/service/source_cleanup.go`：编排 DB 删除后对象清理；
- `internal/ports/source/connector.go`：引入 `TreeSnapshot`、`FetchOptions` 和 oversize/partial 错误语义；
- `internal/ports/storage/raw_document.go`：`RawDocumentInput` 增加 `RevisionID`；沿用现有 `RawDocumentStore` / `AssetStore` 删除接口，不新增“事务存储”抽象。

### Infrastructure

- `internal/infrastructure/db/document_rows.go`、`file_tree_rows.go`：增加列和 codec；
- `internal/infrastructure/db/source_sync_store.go`：实现稳定更新、文件树 upsert、删除 key 收集和 sync result 更新；
- `internal/infrastructure/db/source_cleanup_store.go`：实现 KB scoped cleanup job 与 DB 删除事务；
- `internal/infrastructure/db/retrieval_search_repository.go`：确保 `on_delete=keep` 后 deleted Document 不参与检索；
- `internal/infrastructure/config/config.go`：增加 defaults/validation；
- `internal/infrastructure/migrate/migrations/`：新增递增 migration，包含 `documents.content_hash`、`file_tree_nodes.external_id`、唯一索引、jobs target check 和 down migration。

### Interfaces / worker

- `internal/interfaces/http/knowledge_base_sync_handler.go`：解析可选 `force` body；
- `internal/interfaces/http/knowledge_base_source_policy_handler.go`：新增 source-policy endpoint；
- `internal/interfaces/worker/source_sync_tasks.go`：payload 保持 lineage/job 字段兼容；worker 通过 application service 原子消费 force latch，并在同一终态事务中创建被合并的后续请求；
- 新增 cleanup worker handler；
- scheduler 和知识库创建后的首次入队调用新的 `SyncOptions{Force:false}`。

### Feishu adapter

- `internal/adapters/source/feishu/connector.go`：返回 `TreeSnapshot`，区分完整/部分结果；
- 对列表分页和 Fetch 响应实施 context 透传、大小上限和明确的 recoverable/permanent 错误分类。
- `internal/adapters/storage/local`、`internal/adapters/storage/s3`：raw object key 纳入 `RevisionID`，并保持旧 key 的 Open/Delete 兼容。

## 11. 迁移与兼容

当前最高迁移为 `000021_document_ingest_idempotency`，本功能使用 `000022`；如果合并前迁移序列再次变化，则按最终分支递增调整，但 spec/实现/测试必须一致。

迁移顺序：

1. 检测重复 `documents.external_id`，发现冲突则 fail fast，不自动删除数据；
2. 检测重复 folder token，发现冲突则 fail fast；
3. 增加 nullable `content_hash`；
4. 增加 `file_tree_nodes.external_id`；
5. 建立 workspace/KB 范围内的部分唯一索引；
6. 扩展 jobs target check 允许 `source_cleanup` KB-scoped job；
7. down migration 按依赖逆序删除约束/索引/列，但不删除 JSONB 中的运行期配置键。

`content_hash` 不能回填为任意值。已有飞书文档若无法可靠读取 active revision 的内容 hash，保持 NULL；首次同步会 Fetch 并建立 hash。

## 12. 测试与验收标准

### 12.1 纯函数测试

`source_sync_diff_test.go` 覆盖：

- 空完整快照、空不完整快照；
- 全新增、全删除、部分删除保护；
- cursor 增量、zero EditTime、force；
- RetryRequired、deleted 恢复；
- 重复 remote token、重复 local external ID；
- 安全 watermark 在失败/超限/零值时间后的结果。

### 12.2 Application/service 测试

- 新文档创建一次后再次同步复用同一个 Document ID；
- 内容未变不创建新 revision；
- 内容变化 revision_no 递增；
- force 即使 hash 未变也创建新 revision；
- parse 入队失败后下一次同步会重试；
- pipeline/Document failed 时同 hash 仍会重试；
- 超限新文档不落库，超限已有文档保留旧版本；
- partial snapshot 只新增/更新，不删除、不越过安全 cursor；
- folder 和 file 节点重复同步使用 upsert，不产生名称冲突；
- 完整 snapshot 删除空的失踪 folder，保留含 `keep` 文档的 folder；partial snapshot 不删 folder；
- force 与普通任务并发时不会丢失 force 意图。

### 12.3 Adapter/worker 测试

- Feishu 分页完整遍历返回 `Complete=true`；
- 可恢复分页/限流返回 `Complete=false` 和 warning；
- Fetch 在响应超过限制时提前中止；
- 旧 source_sync payload 仍可按原 lineage/job 字段解码，force 统一从 DB latch 获取；
- cleanup worker 对已删除/不存在对象幂等；
- cleanup 失败可重试并更新 `cleanup_pending`；
- DB 已创建但首次 enqueue 失败的 pending cleanup Job 会被 scheduler 重新派发；
- local/S3 raw key 对不同 revision 唯一，旧 revision 的 Open/Delete 行为不受影响。

### 12.4 数据库集成测试

使用测试运行期临时 `langhuan-test-postgres:pg17` 容器，从空库执行全部迁移并验证：

- content_hash/file_tree external_id 列与唯一索引；
- duplicate 数据迁移保护；
- Document 删除对 revision/chunk/retrieval/file tree 的 cascade；
- deleted Document 即使保留 retrieval entries 也不能被检索命中；
- keep/remove 后知识库摘要与 Generation 统计不继续计入已删除文档；
- source_cleanup Job 的约束和 workspace 隔离；
- `source_config` JSONB 局部更新不覆盖 root/cursor/cron；
- force latch 的 enqueue/consume/finalize 事务不会丢失运行期间到达的 force 请求；
- revision_no 并发递增不会产生重复序号。

### 12.5 验收标准

1. 不完整远端快照永远不会触发本地删除；完整空快照可以正确执行删除策略。
2. 同一个飞书 token 在本地始终对应稳定 Document 身份，不产生重复 Document 或固定 revision_no=1。
3. 失败、超限和 enqueue error 不会被 cursor 永久越过，后续同步可重试。
4. hash 未变且无待重试状态时不重建；force 或待重试状态时会重建。
5. `remove` 删除数据库事实和检索投影，并通过独立 cleanup job 异步删除外部对象；任何清理失败均可观测、可重试。
6. source sync 的通用 JobStatus 不新增 partial；同步结果可通过 `sync_last_result.status` 查询。
7. max_content_bytes 在 adapter 和 application 两层生效，超限不会造成完整内容无界读入。
8. 现有 cron、scheduler connection 限流、手动同步、workspace 隔离和旧 payload 兼容测试通过。
9. create/source-policy API 对非法 `on_delete` 返回 validation error，且不能覆盖运行期 source_config 字段。

## 13. 分阶段交付

- **阶段 1：合同与纯函数**。引入 `TreeSnapshot`、FetchOptions、diff、去重和安全 watermark 单元测试；不改变数据库行为。
- **阶段 2：稳定身份与迁移**。增加 Document hash、folder external_id、唯一索引、Document/Revision upsert 和真实 PostgreSQL 迁移测试。
- **阶段 3：同步应用流程**。实现 add/update、hash 去重、RetryRequired、超限保护、partial sync result 和 cursor 安全更新。
- **阶段 4：force 队列/API**。实现 SyncOptions、任务合并/幂等、HTTP force 参数、worker payload 兼容和并发测试。
- **阶段 5：删除与 cleanup**。实现 on_delete 策略、source_cleanup Job/worker、对象 key 收集、重试和清理失败测试。
- **阶段 6：回归与文档**。补齐 adapter、scheduler、HTTP、集成测试，更新 `docs/ARCHITECTURE.md`、`docs/API_ACCESS.md`、`config.example.yaml`。

## 14. 明确取舍

1. 删除安全以 `TreeSnapshot.Complete` 为硬闸门，数量阈值只做告警，不再用“远端数量小于一半”推断完整性。
2. force 不负责 Generation 生命周期，避免把 source sync 与 index generation build 混成一个不可重试的事务。
3. `remove` 不是跨系统原子删除；数据库事实在事务中删除，外部对象由幂等 cleanup job 最终清理。
4. `content_hash` 表示最新已接受 source revision；失败状态由 `RetryRequired` 覆盖 hash 去重，避免失败后永久跳过。
5. 继续跳过 sheet/bitable，但把不支持类型和 snapshot 完整性纳入可观测同步结果。
