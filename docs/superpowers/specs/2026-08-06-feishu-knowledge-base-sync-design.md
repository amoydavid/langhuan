# 飞书知识库同步设计规格

## 目标

用户在创建知识库时可以选择内容来源：本地上传文件、从飞书云文档同步、从飞书知识库同步。选择飞书来源时，输入飞书 folderToken / wikiToken / 文档 URL，系统自动同步整棵目录树（含文件）入库，并对可向量化的文档执行分块与向量化，从而可被检索。用户可为每个飞书知识库设置定时同步，到期自动重新拉取增量。

支持多个飞书内部应用（同一 Workspace 下可配置多个企业主体），同步任务按应用串行排队，每个应用同时最多 N 个知识库在同步（N 由 `config.yaml` 配置，默认 2），避免单应用并发拉取触发飞书 API 限流。

## 体验

1. 管理员先在「工作区 / 集成」中添加一个或多个飞书内部应用（app_id + app_secret）。
2. 创建知识库时选择来源类型；选飞书时选择已配置的应用，并输入 folderToken / wikiToken / URL。
3. 系统立即触发首次同步：遍历整棵目录树，拉取每个飞书文档并转换成 markdown，建文件树、建文档与修订、入队解析→分块→向量化。
4. 用户可为该知识库设置 cron 定时同步；调度器到点后做基于时间戳的增量同步。
5. 在知识库详情页可查看同步状态、节点数、最近同步时间，并手动触发同步。

## 关键决策

1. **飞书文档以 `file` kind 落库（当 markdown 处理），不新增 DocumentKind**。飞书 docx 同步到本地后与上传的 `.md` 文件在下游管线眼里完全等价（都是「一份 markdown 内容 + sha256 + rawStorageKey」）。这样可完全复用 FileTree 存目录结构、完全复用 parse→chunk→index 下游管线，并满足 `NewDocumentRevision(file)` 的字段约束。来源语义由 `documents.source_type = "feishu"` + 新增 `external_id`（飞书节点稳定 token）承载。

2. **workspace 级多应用连接**。一个 Workspace 下可配置多个飞书内部应用（`workspace_source_connections` 表），每个应用独立凭证、独立限流。知识库创建时显式绑定 `source_connection_id`。

3. **凭证加密复用 `credential_cipher`**。`app_secret` 走 AES-256-GCM（复用 `internal/infrastructure/db/credential_cipher.go` + `config.credentials.encryption_key`），与 MinerU token / model_providers 凭证约定完全一致，不写入 YAML。`app_id` 等非敏感配置存 `config jsonb`。

4. **调度层串行化，不用 asynq 多队列**。已确认 `asynq.Config.Queues` 在 `NewServer` 时静态确定（`cmd/langhuan/main.go:267` 仅 `{"default":1}`），无热加载 API；asynq 队列权重是调度优先级而非并发上限，全局 `Concurrency` 所有队列共享。因此采用进程内 Meta Scheduler（单 goroutine）做 check-then-act：入队前查「该应用当前 in-flight 同步任务数」，未达上限才入队。

5. **飞书目录树映射到现有 FileTree**。不新建目录表，飞书 wiki/drive 的节点层级复用 `file_tree_nodes`（root/folder/file）。

6. **不直接复用 `DocumentIngestService.Ingest`**。`ingestV2` 把 `file` kind + FileTree 节点 + file_type allowlist + multipart io.Reader 绑死在一个事务（`CreateFileDocumentNodeRevisionAndJob`），且按 sha256 去重；飞书同步按 external_id 去重。但复用其底层组件：`RawDocumentStore.Put`、`model.NewDocumentIdentity`、`model.NewDocumentRevision`、`model.NewJob`、`document_parse_start` 任务。

## 现状与复用分析（前因）

### 能直接复用的好底子

- **管线以 `RawStorageKey` 为唯一取数契约**：`parse_stage.go:64` 仅通过 `ports/storage.RawDocumentStore.Open(ctx, revision.RawStorageKey)` 取字节，完全不关心内容怎么进来。只要在同步入口先把远程内容拉下来 `Put` 进 rawStore，下游 chunk/index/embedding 全自动复用。
- **下游已支持非文件来源**：`parse_stage.go:51`、`chunker.go:61`、两个 repository 都已 `file || web` 并列处理。
- **凭证加密范式成熟**：`credential_cipher.go`（AAD 绑定对象身份）+ `parser_provider_selector.go` 的 SelectMinerU 运行时解密路径，是飞书凭证 selector 的直接范本。
- **异步任务链范式**：`document_tasks.go` 的 `handleAsyncPoll`（Start→入队 poll→延迟重入队→成功后入队 index）可复用于飞书分页/增量轮询。
- **SSRF-safe HTTP client**：`asset_resolver.go:263` 的 `downloadRemote` 已处理远程下载，拉取飞书文档可参考。

### 复用的障碍点（必须改造）

- **`KnowledgeBase` 无来源字段**（`domain/model/knowledge_base.go:15-29`），只有 `Metadata` 兜底。
- **`Document` 无 `external_id`**（`document.go:37-60`），`SourceType` 是无约束自由字符串，`NewDocumentIdentity` 对非 web kind 禁止带 sourceURI（`document.go:85-93`）。
- **`DocumentIngestService.ingestV2` 三重耦合**：file kind + FileTree 节点（`CreateFileDocumentNodeRevisionAndJob`）+ file_type allowlist（`document_ingest.go:95-101`）。
- **FileTree 强约束**：`file_tree_nodes_shape_check`（`000005...up.sql:374-404`）要求 file 节点必须挂 `DocumentKindFile`，且有 trigger 强制每个 File Document 恰好 1 个节点。
- **`jobs` 表无 connection 维度**，`JobRepository` 只有 `Get`（按主键），无「查询进行中任务」方法（`job_repository.go:22-33`）。
- **KB 创建无异步触发先例**：`KnowledgeBaseService.Create`（`knowledge_base.go:91-121`）是纯同步单事务，创建后无 enqueue。

## 数据模型

### 新增 `workspace_source_connections` 表

一个 Workspace 下可配置多个飞书应用，每个应用独立凭证与限流。

```text
id                       uuid PK
workspace_id             uuid → workspaces(id) CASCADE
provider                 text        -- 'feishu'
name                     text        -- 用户可读名，如 "主公司飞书"
config                   jsonb       -- { app_id, base_url?, ... } 非敏感
credentials_ciphertext   bytea       -- app_secret 密文（credential_cipher 加密）
status                   text        -- 'active' / 'disabled'
created_at / updated_at / deleted_at

UNIQUE (workspace_id, provider, name)
UNIQUE (workspace_id, provider, (config->>'app_id'))   -- 同 app_id 禁止重复添加
```

### 扩展 `knowledge_bases`

```text
+ source_type            text NOT NULL DEFAULT 'upload'   -- upload / feishu_drive / feishu_wiki
+ source_config          jsonb NOT NULL DEFAULT '{}'      -- { root_token, root_kind, cron, sync_cursor, next_sync_at }
+ source_connection_id   uuid → workspace_source_connections(id)  -- 飞书来源必填
```

### 扩展 `documents`

```text
+ external_id   text        -- 飞书节点 obj_token；上传文档为空
INDEX (knowledge_base_id, external_id) WHERE external_id IS NOT NULL   -- 增量查找
```

### 扩展 `jobs`（调度限流关键）

```text
+ source_connection_id   uuid
INDEX (workspace_id, source_connection_id, type, status)
  WHERE source_connection_id IS NOT NULL   -- 部分索引，不污染文档流水线 job
```

**实现时必须验证并处理的两个约束**：

- `jobs_target_check`（要求 document 三元组或 generation 非空）——同步 job 可能两者都空，仅填 `knowledge_base_id`。需确认 CHECK 是否放行，否则放松。
- `jobs_status_check` DB 仅允许 4 状态（pending/running/completed/failed），而 Go `value.JobStatus` 有 7 个。趁此对齐，补全 queued/succeeded/cancelled。

### 领域模型同步

`model.KnowledgeBase`（+SourceType/SourceConfig/SourceConnectionID）、`model.Document`（+ExternalID）、`model.Job`（+SourceConnectionID）、新建 `model.SourceConnection`，及对应 Row + codec + `AutoMigrateModels()` 登记。

## 调度与并发模型

### 配置（非敏感运行参数）

```yaml
# config.yaml
source_sync:
  scheduler_interval_seconds: 60    # Meta Scheduler 扫描周期
  max_concurrent_per_connection: 2  # 每个飞书应用同时最多几个 KB 在同步
```

### Meta Scheduler（进程内单 goroutine）

`internal/application/service/source_sync_scheduler.go`，`cmd/langhuan` 启动时起一个 context 受控的 goroutine（`time.Ticker`，符合 AGENTS 5.8），随 worker 生命周期启停。每个 tick：

```text
Tick(ctx):
  1. 查所有 source_type IN (feishu_drive, feishu_wiki) 且 next_sync_at <= now() 的 KB
  2. 按 source_connection_id 分组
  3. 对每个 connection:
       count = jobRepo.CountActive(wsID, connID, "source_sync")  -- status IN ('pending','running')
       可入队数 = max_concurrent_per_connection - count
       为该 connection 下到期的 KB 入队（直到填满额度）
  4. 每个 KB 入队后，next_sync_at := cron.Next(now) 写回 source_config
```

### 降低延迟的主动续跑

worker handler 完成（成功/失败）后主动调用 `scheduler.TryDispatchConnection(wsID, connID)`：立即检查该应用是否还有额度与到期 KB，有则马上入队，避免空等下个 tick。

### 竞态说明

Meta Scheduler 是单 goroutine，check-then-act 在同一 goroutine 顺序执行，**单进程部署无竞态**。asynq worker 并发执行的是「同步动作」而非「入队决策」。多进程部署需引入分布式锁，**首版不做，列为风险**。

## 同步流程

`internal/application/service/source_sync.go` 的 `SyncKnowledgeBase(ctx, kbID)`：

```text
1. 读 KB + 经 SourceConnectionSelector 解密取 app_secret
2. SourceConnector.ListTree(conn, root) → []ExternalNode
3. 对每个 HasDocument && ObjType=="docx" 节点:
   a. 按 (kbID, external_id=token) 查已有 Document
   b. 增量(阶段3): EditTime <= sync_cursor 则跳过；否则 Fetch
   c. connector.Fetch(token) → markdown bytes
   d. rawStore.Put(reader) → key
   e. 建 Document{file kind, source_type="feishu", external_id, title}
      + Revision{markdown, key} + FileTreeNode{层级对应} + Job{parse_start}
      （新增事务方法 CreateSyncedDocumentNodeRevisionAndJob，接受 external_id，不改老接口）
   f. 入队 document_parse_start
4. folder 节点建 FileTreeNode(folder)，维护 map[feishuToken]nodeID 建立父子层级
5. 删除检测(增量): 飞书树中不再出现的 external_id → Document 软删
6. 回写 source_config.sync_cursor = max(EditTime)
```

`internal/interfaces/worker/source_sync_tasks.go`：`TaskSourceSync = "source_sync"`，payload 含 `{WorkspaceID, KnowledgeBaseID, ConnectionID, JobID}`，Handle → MarkRunning → 调 SourceSyncService → MarkSucceeded/Failed → TryDispatchConnection 续跑。

## 飞书适配器

`internal/adapters/source/feishu/` 实现 `ports/source/connector.go` 的 `SourceConnector` 接口：

```text
ListTree(ctx, conn, root) → []ExternalNode   // 递归遍历 wiki 节点树 / drive folder
Fetch(ctx, conn, externalID) → FetchedDoc    // docx → markdown bytes + 元数据
```

子组件：

- `token_client.go`（tenant_access_token 换取 + 缓存刷新）
- `wiki_client.go`（`/wiki/v2/spaces/:space/nodes`）
- `drive_client.go`（`/drive/v1/files?folder_token=`）
- `docx_client.go`（`/docx/v1/documents/:id/raw_content`）
- `url_parser.go`（`feishu.cn/docx|wiki|drive/folder/:id` → {kind, token}）

首版仅处理 `ObjType=="docx"`；sheet/bitable/mindmap 等跳过并记 warning 日志，后续按类型接不同 parser（sheet→csv parser 已有）。

## API

### 飞书应用管理（先于 KB 创建配置）

- `POST /api/v1/workspaces/:slug/source-connections` — 创建（body: `{provider, name, app_id, app_secret}`），secret 加密存储。
- `GET /api/v1/workspaces/:slug/source-connections` — 列表，不返回 secret。
- `PATCH /api/v1/workspaces/:slug/source-connections/:id` — 更新凭证 / 启停。
- `DELETE /api/v1/workspaces/:slug/source-connections/:id`。
- 仅 workspace owner/admin 可写；member 只读；API Key 不可访问。

### 知识库创建扩展

`createKnowledgeBaseRequest` 增加 `source_type`、`source_config`、`source_connection_id`。`source_type` 为 `feishu_*` 时校验 connection 必填、source_config 必含 root token、可含 cron。KB 创建成功（事务提交后）立即入队首次 `source_sync`。

### 手动同步

`POST /api/v1/workspaces/:slug/knowledge-bases/:id/sync` — 立即入队 `source_sync`，受同样的 per-connection 限流约束。

## 前端交互

### 1. 侧边栏新增「集成」入口

workspace 侧边栏「工作区」分组新增「集成」项（Plugs/Plug 图标），仅 owner/admin 可见。新增独立路由 `/workspaces/$slug/integrations`（平铺，与 members/api-keys 一致）。

```text
┌─ 琅嬛 ──────────────────────────────────┐
│  ◉ 默认工作区                    ▾  │
│ ───────────────────────────────────── │
│  工作区                                 │
│   ▢ 概览          LayoutDashboard      │
│   ▢ 知识库        BookOpen             │
│   ▢ 模型          Boxes                │
│   ▢ 集成          Plug            ← 新 │
│   ─ 成员区 ─                            │
│   ▢ 成员          Users                │
│   ▢ 邀请          MailPlus             │
│   ▢ API Key       KeyRound             │
│   ▢ 检索设置      SlidersHorizontal    │
└────────────────────────────────────────┘
```

### 2. 集成页 — 飞书应用列表

卡片网格（`grid gap-4 md:grid-cols-2 xl:grid-cols-3`，`resource-card` 样式），每张卡一个应用。

```text
工作区 / 集成

集成应用
在此 Workspace 中接入外部内容源。飞书应用凭证在此统一管理，
创建同步型知识库时选择对应应用。

┌──────────────────────────┐  ┌──────────────────────────┐
│  [icon-tile] 飞书        │  │  [icon-tile] 飞书        │
│  主公司飞书        [已启用]│  │  子公司飞书        [已停用]│
│  App ID: cli_a1b2…       │  │  App ID: cli_x9y8…       │
│  绑定知识库: 3 个          │  │  绑定知识库: 0 个          │
│                [编辑][停用]│  │                [编辑][启用]│
└──────────────────────────┘  └──────────────────────────┘

┌──────────────────────────────────────────────────────┐
│  ┌──┐                                                  │
│  │ +│  添加飞书应用                                     │
│  └──┘  接入一个新的飞书内部应用                          │
└──────────────────────────────────────────────────────┘
```

### 3. 添加 / 编辑飞书应用（窄页单 Card）

`max-w-3xl` 窄页 + 单 Card + react-hook-form 垂直 grid + 左对齐主按钮 + toast 反馈（与 KB 创建页一致）。

```text
工作区 / 集成 / 添加飞书应用

┌─ 添加飞书应用 ─────────────────────────────────────────┐
│  将应用凭证保存在此 Workspace，供同步型知识库使用。       │
├────────────────────────────────────────────────────────┤
│                                                          │
│  应用名称 *                                               │
│  ┌──────────────────────────────────────────────────┐  │
│  │ 主公司飞书                                         │  │
│  └──────────────────────────────────────────────────┘  │
│                                                          │
│  App ID *                                                │
│  ┌──────────────────────────────────────────────────┐  │
│  │ cli_a1b2c3d4e5f6                                  │  │
│  └──────────────────────────────────────────────────┘  │
│  飞书开放平台 → 应用详情 → 凭证与基础信息                  │
│                                                          │
│  App Secret *                                            │
│  ┌──────────────────────────────────────────────────┐  │
│  │ ••••••••••••••••••••••••••••••••                  │  │
│  └──────────────────────────────────────────────────┘  │
│  加密存储，不会明文回显                                    │
│                                                          │
│  ℹ 应用需开通权限: wiki:wiki:readonly、                  │
│     docx:document:readonly、drive:drive:readonly         │
│                                                          │
└───────────────────────────────────────────  [测试并保存] ┘
```

「测试并保存」：先用凭证换 tenant_access_token 验证可用，成功后加密落库；失败显示飞书返回的错误，不落库。

### 4. 知识库创建页 — 来源类型切换

在现有 KB 创建表单顶部新增「内容来源」分段控件（RadioGroup / Tabs），切换后下方表单变化。上传来源保持现有字段不变。

```text
工作区 / 知识库 / 新建

┌─ 新建知识库 ───────────────────────────────────────────┐
│  把内容组织成可检索、可追溯的知识库。                     │
├────────────────────────────────────────────────────────┤
│                                                          │
│  内容来源                                                │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐       │
│  │ ◉ 本地上传   │ │   飞书云文档 │ │   飞书知识库 │       │
│  └─────────────┘ └─────────────┘ └─────────────┘       │
│                                                          │
│  ─── 以下为「本地上传」字段（默认） ──────────────────   │
│                                                          │
│  名称 *                                                  │
│  ┌──────────────────────────────────────────────────┐  │
│  │ 产品手册                                          │  │
│  └──────────────────────────────────────────────────┘  │
│  描述 / Embedding 模型 / 分块策略 / ...（现有字段）       │
│                                                          │
└───────────────────────────────────────────  [创建] ───┘
```

切换到「飞书知识库」时（仅 `source_type=feishu_*` 时出现的字段，嵌入同一 Card）：

```text
│  内容来源                                                │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐       │
│  │   本地上传   │ │   飞书云文档 │ │ ◉ 飞书知识库 │       │
│  └─────────────┘ └─────────────┘ └─────────────┘       │
│                                                          │
│  名称 *                                                  │
│  ┌──────────────────────────────────────────────────┐  │
│  │ 产品知识库                                        │  │
│  └──────────────────────────────────────────────────┘  │
│                                                          │
│  描述                                                    │
│  ┌──────────────────────────────────────────────────┐  │
│  │                                                      │
│  └──────────────────────────────────────────────────┘  │
│                                                          │
│  飞书应用 *                                              │
│  ┌──────────────────────────────────────────────────┐  │
│  │ 主公司飞书                                     ▾  │  │
│  └──────────────────────────────────────────────────┘  │
│  从「工作区 / 集成」中已配置的应用里选择                  │
│                                                          │
│  知识库 Token / 链接 *                                   │
│  ┌──────────────────────────────────────────────────┐  │
│  │ https://xxx.feishu.cn/wiki/wikcnB…              ↗  │  │
│  └──────────────────────────────────────────────────┘  │
│  粘贴 wiki 链接或 nodeToken，将同步该节点下的整棵目录树    │
│                                                          │
│  Embedding 模型 / 分块策略 / ...（现有字段）              │
│                                                          │
│  ⚙ 定时同步 (可选)        [● 启用]                      │
│  ┌──────────────────────────────────────────────────┐  │
│  │ cron 表达式: 0 */6 * * *    [每天 6 小时一次]      │  │
│  └──────────────────────────────────────────────────┘  │
│                                                          │
└───────────────────────────────────────────  [创建并同步]┘
```

输入 token/URL 后可显示「待同步」预览（节点数量、是否可达），确认后创建。

### 5. 知识库详情工作台 — 同步状态

详情页头部 Badge 显示同步状态；概览 tab 展示同步信息；右上角「手动同步」按钮。

```text
产品知识库                    [同步中]  最近: 2026-08-06 12:30
概览   内容   检索   索引   设置                    [↻ 手动同步]

概览
┌──────────────────────────────────────────────────────┐
│  来源        飞书知识库 · 主公司飞书                     │
│  根节点      wikcnB...(产品空间)                         │
│  目录节点    128 个                                      │
│  文档        96 个 · 已向量化 80 个 · 处理中 3 个         │
│  同步状态    ● 同步中（拉取飞书文档…）                    │
│  最近同步    2026-08-06 12:30                           │
│  下次同步    2026-08-06 18:00（每 6 小时）               │
│  游标        obj_edit_time = 2026-08-06T11:50:00+08:00  │
└──────────────────────────────────────────────────────┘

文件树（同步自飞书，与飞书目录结构一致）
┌──────────────────────────────────────────────────────┐
│  ▾ 产品空间                                            │
│    ▾ 使用指南                  [96 篇 · 已同步]        │
│      · 快速开始.docx            ✓ 已向量化              │
│      · 安装部署.docx            ⟳ 处理中                │
│      · 常见问题.docx            ✓ 已向量化              │
│    ▸ API 文档                                          │
│    ▸ 发行说明                                          │
└──────────────────────────────────────────────────────┘
```

同步状态用文字 + 图标表达，不只靠颜色（符合设计系统）：✓ 已向量化（绿）、⟳ 处理中（琥珀）、⚠ 失败（红，带重试）、– 跳过（灰，非 docx）。

### 6. 空状态 / 错误状态

- 无飞书应用时，创建页应用下拉显示「请先在 工作区/集成 添加飞书应用」+ 跳转链接。
- 同步失败（凭证失效 / token 无权限）：工作台显示错误原因 + 「去集成页更新凭证」入口。
- 未配置定时：下次同步显示「手动触发」。

## 风险与取舍

1. **FileTree 同级 name 唯一约束**：飞书可能有同名节点。首版用「标题 + 必要时短 token 后缀」规避；后续可给 file_tree_nodes 加 `external_ref` 列放宽约束。
2. **多进程竞态**：首版单进程部署，Meta Scheduler 单 goroutine 无竞态；多进程需 Redis 分布式锁（未做）。
3. **飞书 API 限流**：适配器内部做并发控制 + 重试，大树分批；per-connection 并发上限本身即保护。
4. **`jobs_target_check` / status CHECK**：实现时必须验证并顺带对齐状态枚举。
5. **非 docx 文档**：首版跳过 sheet/bitable/mindmap 记日志；后续接 csv parser 等。
6. **`CreateFileDocumentNodeRevisionAndJob` 复用**：新增并行方法 `CreateSyncedDocumentNodeRevisionAndJob` 接受 external_id，不改老接口避免回归。

## 验收标准

1. Workspace owner/admin 能在「集成」页增删改多个飞书应用，app_secret 加密存储、不明文回显；member 不可写。
2. 创建知识库时可选择本地上传 / 飞书云文档 / 飞书知识库三种来源；选飞书时能选择已配置应用并输入 token/URL。
3. 飞书来源 KB 创建后立即触发首次同步，整棵目录树映射为 FileTree，每个 docx 文档被解析、分块、向量化并可检索。
4. 同一飞书应用下多个 KB 的同步任务按 `max_concurrent_per_connection` 串行排队，超额任务等待，不并发冲击飞书 API。
5. 配置 cron 的 KB 到期自动增量同步（基于 obj_edit_time 游标），跳过未变更节点，软删飞书中已删除的节点。
6. 知识库详情页正确展示来源、同步状态、节点数、最近/下次同步时间；可手动触发同步。
7. 同步状态不只靠颜色，同时有文字与图标；凭证失效等错误有明确原因与修复入口。
8. 迁移、Repository、Service、HTTP、前端测试覆盖：多应用、限流、增量、删除检测、凭证加解密、权限。

## 分阶段交付

- **阶段 0**：数据模型 + 迁移（多应用表、KB/Document/jobs 扩展、状态枚举对齐）。
- **阶段 1**：飞书适配器（SourceConnector + token/wiki/drive/docx client + url_parser）。
- **阶段 2**：全量同步 + 手动触发（单应用单 KB，先不接限流，验证端到端「飞书→markdown→分块→检索」）。
- **阶段 3**：多应用 + Meta Scheduler 限流 + cron 定时增量。
- **阶段 4**：HTTP API + 前端（集成页、KB 创建来源切换、详情同步状态）。
