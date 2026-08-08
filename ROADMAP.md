# 琅嬛 ROADMAP

琅嬛是一个独立的知识转化与检索服务，定位在 RAG 工程中的知识处理层。它负责把 `pdf/docx/markdown/txt/csv/xlsx` 等内容转成可检索、可向量化、可追溯的结构，并通过 REST 与 MCP 对外提供导入、状态查询、检索和删除能力。

琅嬛不负责调用 LLM 生成答案，不负责 Chat/Agent 编排，也不在首版实现图查询。未来如果引入 PostgreSQL AGE 等图能力，应作为新的索引与检索扩展进入，不污染当前主链路。

## 技术基线

- Go 1.26
- YAML 配置文件：运行配置从 `config.yaml` 加载，不使用环境变量作为主配置入口
- Gin: REST 与 MCP HTTP 入口
- GORM: PostgreSQL 数据访问
- PostgreSQL + pgvector: 主存储、向量索引、全文索引
- asynq + Redis: 异步文档解析、轮询、索引任务
- MinerU Cloud: PDF 转 Markdown（v0.7.0 引入）
- Embedding Provider Registry：支持五种 typed Provider，并已接入 Generation 构建、文档向量化与检索查询
- OSS/对象存储: 保存解析得到的图片等资产
- Web Console: `web/` 目录下的 React + Vite + shadcn/ui 管理台，已通过真实 REST 接口完成认证、Workspace、知识库、文档、成员、邀请和模型管理，并可通过 `web_embed` build tag 嵌入主二进制

## 版本与安装兼容策略

- `v1.0.0` 前仅支持从空数据库全新安装，不承诺内部测试数据跨版本迁移。
- `v1.0.0` 前允许迁移直接重建或删除旧业务结构，不为尚未对外发布的历史 schema 编写数据回填和兼容转换；需要升级内部测试环境时，直接重建数据库并重新导入测试数据。
- “仅支持全新安装”不降低迁移本身的质量要求：从空数据库执行全部迁移必须成功，最终 schema、约束和索引必须通过真实 PostgreSQL + pgvector 集成测试。
- `v1.0.0` 建立首个对外兼容基线；从该版本开始，后续版本必须为受支持版本提供可验证的数据迁移路径，除非通过新的 major 版本明确宣布破坏性变更。

## 架构原则

- 单一进程入口：只有一个 `cmd/langhuan`，同一二进制同时提供 REST、MCP、worker，并在 `web_embed` 构建中托管内嵌的 Web Console 静态资源。
- 分层清晰：领域模型不依赖 HTTP、MCP、数据库、MinerU、OSS、embedding SDK。
- 抽象优先但不过度预留：只为当前要实现的能力定义 port，不为图查询提前写空接口。
- 异步优先：导入文档返回 `document_id/job_id`，后续由任务链完成解析、资产归档、分块、向量化和索引。
- 幂等优先：所有任务以 PostgreSQL 状态为准，重复执行不能产生重复文档、重复 chunk 或重复资产。
- 可追溯：每个 chunk 都能回到 document、文件版本、页码/sheet/行列/offset 等来源锚点。
- Workspace 即租户：workspace 是身份认证、成员角色与资源隔离的边界（v0.2.1 起落地）；v0.6.0 的 Workspace API Key 在租户边界内继续按一个或多个明确绑定的 KnowledgeBase 收窄资源范围。
- 单二进制交付：`web/` 前端产物通过 `go:embed` 一起打进发布二进制，`make linux` 可同时交付 REST、MCP、worker 和管理控制台；开发态仍使用 Vite HMR 与普通 Go 构建。
- 协议命名空间固定分离：SPA 使用 `/` 与浏览器路由，REST 只使用 `/api/v1/*`，MCP over HTTP 保留 `/mcp`；不得再新增根级 REST 路径。

## 目标主链路

```text
File/Web/FAQ 导入
  -> 创建稳定 Document 身份与不可变 DocumentRevision
  -> File 进入独立知识库文件树；FAQ 原子保存问题集合与回答
  -> asynq 按 Revision 驱动 parse/chunk/index
  -> DocumentChunkSet -> parent/child/flat Chunk -> ChunkRevision 保存可追溯事实
  -> EmbeddingClient 只处理可检索 child/flat 的 search_content
  -> RetrievalEntry 只为 child/flat 保存 halfvec、FTS 与返回内容
  -> 原子发布到唯一 active Generation
  -> Vector + FTS + deterministic RRF，按父块聚合后返回 evidence
```

## 当前分块合同

标准分块当前为 `chunker_version=3`。Generation 快照固定保存 `strategy`、`enable_parent_child`、`parent_chunk_size`、`child_chunk_size`、`chunk_size`、`chunk_overlap` 六个字段；默认使用 `auto` 策略、启用父子模式、父块 `4096` 字符、子块 `384` 字符。`auto` 会优先采用标题结构，再尝试启发式章节边界，最后回退 recursive。

父子模式下，每个可检索 child 都必须有一个 parent；短文本也生成 parent + child。parent 持有完整返回上下文，不进入向量或全文索引，也不能直接编辑；child 是向量/FTS 召回单元。关闭父子模式时只有 flat，flat 同时承担召回与返回职责。检索会先召回 child/flat，再按 parent 聚合：主结果返回完整父块正文，`matched_children` 标明实际命中的 child；flat 以自身作为结果并在该字段中标记。

PDF 经 MinerU Cloud 转为 Markdown 后，会重新建立结构化 `parse_manifest`，再进入与其它文件相同的分块路径。

## 分层规划

```text
cmd/
  langhuan/

internal/
  interfaces/
    http/
    mcp/
    worker/

  application/
    service/
    pipeline/
    dto/

  domain/
    model/
    value/
    errors/

  ports/
    parser/
    storage/
    embedding/
    index/
    queue/

  adapters/
    parser/
      minerucloud/
      markdown/
      text/
      csv/
      xlsx/
    storage/
      local/
      oss/
    embedding/
      openai/
    index/
      pgvector/
      postgresfts/
    queue/
      asynq/

  infrastructure/
    config/
    db/
    migrate/
    logger/

web/                    # 管理台；web_embed 构建时由该 package 直接嵌入 web/dist
```

> 现状注记：上图中的 `adapters/index/` 目前**尚未拆出**。pgvector 与 PostgreSQL FTS 的读写都并入了 `internal/infrastructure/db/RetrievalRepository`（同一 `retrieval_entries` 投影里同行写 `halfvec` 与 `tsvector`），port 也只有一个合并的 `RetrievalIndex` / `SearchReader`。这与 5.7「避免盲目复杂化」一致——当前 FTS 唯一实现就是 PostgreSQL 内建 `tsvector`，没有可替换后端，因此不为它单独立 port 或建独立 adapter。等真正需要替换 FTS 后端（如 ES/Meilisearch）或多向量库时，再把 FTS/向量读写从 `RetrievalRepository` 拆成 `adapters/index/` 下各自的 port + adapter。

## 版本路线

### v0.1.0 - 工程骨架与基础运行（已完成）

目标：建立可运行的服务骨架和分层边界。

- 初始化 Go 1.26 工程。
- 建立单入口 `cmd/langhuan`。
- 接入 YAML 配置、日志、健康检查。
- 接入 PostgreSQL、Redis、GORM、asynq。
- 本地开发默认连接已存在的 PostgreSQL 数据库 `langhuan`。
- 定义核心 domain model：`KnowledgeBase`、`Document`、`Chunk`、`Asset`、`Job`。
- 定义当前必要 ports：`DocumentParser`、`AssetStore`、`EmbeddingClient`、`VectorIndex`、`KeywordIndex`、`JobQueue`。
- 提供 v0.1.0 基础 REST（v0.2.0 起知识库入口改为 workspace-scoped，以下非 workspace 路由不再保留）：
  - `GET /api/v1/healthz`
  - `POST /api/v1/knowledge-bases`
  - `GET /api/v1/knowledge-bases/:id`

验收标准：

- 服务可启动。
- 数据库迁移可执行。
- asynq worker 可注册任务。
- 单进程内 REST 与 worker 可同时运行。

### v0.2.0 - Workspace、文档导入与异步任务链（已完成）

目标：建立 workspace 作用域，完成导入状态机和任务编排，不要求完成全部格式解析。

- 引入 `Workspace` 模型，知识库归属 workspace。
- 为 v0.1.0 已有知识库提供默认 workspace 数据迁移；不保留非 workspace HTTP 入口。
- 预留 `workspace_api_tokens` 表结构，本版本不实现签发/校验（留到 v0.6.0）。
- 实现 workspace-scoped REST：
  - `POST /api/v1/workspaces`
  - `GET /api/v1/workspaces/:workspace_slug`
  - `POST /api/v1/workspaces/:workspace_slug/knowledge-bases`
  - `GET /api/v1/workspaces/:workspace_slug/knowledge-bases/:id`
  - `POST /api/v1/workspaces/:workspace_slug/knowledge-bases/:id/documents`
- 上传后创建 document/job，返回 `document_id` 和 `job_id`。
- 实现文档状态机：
  - `pending`
  - `parsing_submitted`
  - `parsing`
  - `parsed`
  - `indexing`
  - `completed`
  - `failed`
  - `deleting`
  - `deleted`
- 实现 asynq 任务：
  - `document_parse_start`
  - `document_parse_poll`
  - `document_index`
- 实现任务幂等和失败记录。
- 提供状态查询：
  - `GET /api/v1/workspaces/:workspace_slug/documents/:id`
  - `GET /api/v1/workspaces/:workspace_slug/jobs/:id`

验收标准：

- 知识库必须带有 `workspace_id`，跨 workspace 不能读取或操作。
- 上传文档后可以查询状态。
- worker 重启后任务可继续。
- 重复任务不会重复创建 chunk 或 asset。

### v0.2.1 - 多租户与认证（已完成）

目标：把 workspace 从「数据归属边界」升级为「租户与身份认证边界」，为后续所有对外能力提供统一鉴权基础。

- email + password + 昵称自建用户体系，argon2id 哈希。
- 数据库 session：HttpOnly cookie，登录/登出/`GET /api/v1/auth/me`。
- workspace 内 owner/admin/member 三档角色，用户与 workspace 为 N:N membership。
- platform_admin 为唯一可创建 workspace 的身份；「邀请即注册即加入」流程。
- workspace 增加全局唯一 `slug`，HTTP 路由改为 `:workspace_slug`。
- `SessionAuth` / `RequirePlatformAdmin` / `RequireWorkspace` / `RequireWorkspaceRole` 中间件链，handler 只读 `AuthContext`。
- 跨 workspace 访问统一返回 `404`；登录失败按 email 维度限流（Redis）。
- worker/asynq 链路零改动，仍只使用 workspace UUID。

验收标准（详见 `docs/superpowers/specs/2026-07-29-v0.2.1-multi-tenant-auth-design.md`）：

- 首个用户自动成为 platform_admin；此后注册必须携带有效邀请。
- owner/admin/member 权限矩阵与 owner 唯一性约束生效。
- `go test ./...`、集成测试、`go vet ./...` 全部通过；明文密码/token/session 不进日志与 commit。

### v0.3.0 - 多格式解析与分块（已完成）

目标：不依赖外部解析服务，先跑通 Markdown/TXT/CSV/XLSX/DOCX 的导入 -> normalized markdown -> chunk 全链路，形成稳定 chunk 模型。PDF/MinerU 暂不在本版本范围内（见 v0.7.0）。

> 历史合同注记：本版本中的单层 `512/80` 分块是当时的交付基线；当前实际合同以「当前分块合同」的 v3 父子/flat 模式为准。

- 实现 Markdown parser。
- 实现 TXT parser。
- 实现 CSV parser。
- 实现 XLSX parser。
- 实现 DOCX parser adapter，优先采用可替换方案，不把解析细节写入业务层。
- 保存 version 1 `parse_manifest`，记录结构块、warning、UTF-8 byte span 与 typed source anchor。
- 实现 Chunker：
  - Markdown heading-aware split
  - recursive split
  - overlap
  - table-aware split
  - `content` 与 `embedding_content` 分离
- 保存 chunk 元数据：
  - sequence
  - title/context header
  - source offset/line
  - sheet/row/column、paragraph/table anchors
- 分块单位固定为 Unicode 字符数，默认 `512/80`，允许 `overlap=0`。
- PDF 及未知格式在 raw file、Document、Job 和队列副作用前返回 HTTP 415；PDF/MinerU 延后到 v0.7.0。
- DOCX 图片不抽取，只产生非致命 warning；CSV/XLSX 当前采用首个非空行作表头的基础表格模型。

验收标准：

- Markdown、TXT、CSV、XLSX、DOCX 均通过真实 PostgreSQL + Redis/asynq + HTTP/worker E2E 到达 `completed`。
- 每种格式都保存真实 normalized Markdown、version 1 manifest 和至少一个 chunk。
- chunk 可回溯到原始 document 与对应 offset、sheet/行列或 DOCX 段落/表格位置。
- 表格只在完整数据行之间切分，每个 table chunk 重复表头；超长单行完整保留并标记 oversized。
- PDF 返回 HTTP 415，且 raw storage、documents、jobs 均无新增副作用。

### v0.3.1 - 模型配置与选择（已完成）

目标：在文档向量化之前先建立可管理、可隔离、可追踪的 Embedding 模型配置合同。

- 使用 `model_providers + models` 两层结构；Provider 保存连接和加密凭证，Model 保存真实模型名、维度和模型级参数。
- 支持平台共享与 Workspace 自有两种作用域；Workspace 可见范围固定为“平台共享 + 当前 Workspace 自有”。
- platform_admin 管理平台记录；Workspace owner/admin 管理自有记录；member 可读取并选择。
- 支持 OpenAI、ARK、Ollama、DashScope、TencentCloud 的 Eino typed adapter 与真实连接测试；Qianfan 明确返回不支持。
- Provider 凭证使用 AES-256-GCM 保存；唯一密钥来自未提交的 `config.yaml`，示例配置提供 `openssl rand -base64 32` 生成命令。
- Embedding 维度只允许已建立 HNSW 索引的 798、1024、2048、3584，默认 1024。
- v0.3.1 当时由知识库直接保存非空 `embedding_model_id`；v0.4.0 起生效模型改由 active Generation 的不可变快照持有，创建与后续 Generation 换模仍校验模型作用域、类型、状态及维度，不自动 fallback。
- Web Console 提供 Workspace 模型页、平台模型页、Provider 详情、typed 表单、连接测试和知识库模型选择；完整左侧侧边栏始终保留。
- 本版本当时不执行文档向量化；该能力现已由 v0.4.0 的统一 RetrievalEntry 投影接管。仍不实现 LLM 回答或 Rerank。

验收标准：

- 平台共享、自有模型、跨 Workspace 404、角色权限、AES 密文、引用冲突和知识库显式绑定均通过真实 PostgreSQL/Gin/Auth E2E。
- 数据库不为 `provider` 增加枚举 CHECK，未来新增 Provider 不要求修改数据库枚举。

补充交付：模型连接已升级为显式多能力 Provider descriptor；同一连接可承载多个 Embedding/Rerank 模型。SiliconFlow 已接入共享 API Key、双 Endpoint、模型能力/数量聚合和 Web Console 的“全部模型 / 连接管理”双视图；Workspace 与平台管理目录支持 type/status/scope/provider/q 筛选，Generation selectable 接口合同保持不变。Provider descriptor 的能力标识不再固定白名单，并新增可选 ModelCatalog port：OpenAI-compatible 与 SiliconFlow 可从上游模型列表快速填充模型表单，选择不会自动保存，未来供应商可独立接入目录归一化。
- v0.4.0 已把已解析的 Embedder 接入按 Revision/Generation 驱动的索引流水线。

### v0.4.0 - 知识事实模型 v2、Embedding 与混合检索（已完成）

目标：完成 SaaS/RLS-ready 的 File/FAQ/Web 知识事实层和可重建检索闭环。

- 所有知识业务表直接保存 `workspace_id`，使用复合外键守住 Workspace/KB/Document lineage；业务读写进入 transaction-local Workspace 数据库上下文，为后续 RLS 做准备，但本版本不启用 policy。
- Document kind 固定为 `file|faq|web`；文件类型、raw key、hash 与解析产物归属不可变 DocumentRevision。
- FAQ 以“一组问题 + 一个回答”的完整 Revision 保存，固定生成一个 FAQ Chunk；问题进入 Embedding/FTS，召回返回回答。
- File Document 使用独立 `file_tree_nodes` 组织；rename/move 不改变内容版本、对象键或 Generation。
- 使用 `DocumentRevision -> DocumentChunkSet -> Chunk -> ChunkRevision` 保存解析、分块与人工编辑历史；当前标准分块在 Chunk 层显式表达 parent/child/flat lineage。
- 使用单 active、双缓冲 `knowledge_base_index_generations` 保存模型、分块和检索配置快照，支持重建、stale 检测和原子激活。
- `retrieval_entries` 同行保存 `search_content`、返回 `content`、FTS 与 halfvec；当前只为 child/flat 建立投影，父块正文在检索时作为完整上下文返回；投影可删除并从事实层重建。
- 实现固定维度向量查询、PostgreSQL FTS 与确定性 RRF；File 返回当前树节点名，FAQ/Web 返回当前 Document 标题。
- 实现 Chunk Revision 编辑/启停、Document 软删除、投影退役和有限批量保留清理。

向量索引基线：

- `retrieval_entries.embedding` 使用 pgvector `halfvec`，每行记录 dimension。
- 按维度建 HNSW 部分索引：798 / 1024 / 2048 / 3584，均 `halfvec_cosine_ops`、`m = 16, ef_construction = 64`，`WHERE dimension = N`。
- 查询侧必须用与索引完全一致的表达式 `embedding::halfvec(N)` + `WHERE dimension = N`，否则规划器无法命中、退化为全表扫描。
- 新增维度时必须先补迁移和固定查询 SQL，再扩展模型校验。

验收标准：

- File/FAQ/Web 都能进入版本化事实层并发布到 active Generation。
- FAQ 问题可命中且返回回答，回答独有词不能命中。
- 跨 Workspace 复合外键与 Repository 查询均拒绝；Auth/Workspace/Model 数据在破坏性知识迁移后保留。
- Generation 切换原子，内容变化触发 stale，纯文件树变化不触发 stale。
- Document 删除立即退出检索；Revision 与原始对象保留到恢复窗口结束。
- HNSW `EXPLAIN` 证明四种固定查询表达式可使用对应部分索引。

### 截至 v0.4.0 的 Web Console 与交付能力（已完成）

以下能力已提前完成，不再作为未来版本重复规划：

- Web Console 已接入真实认证、Workspace、知识库、文档、成员、邀请、Provider 和 Model REST 接口；浏览器认证只使用 HttpOnly session cookie。
- SPA 使用显式 Workspace 路由和短知识库路径 `/workspaces/$workspaceSlug/kb/$kbId`，所有请求统一访问同源 `/api/v1/*`。
- 前端已具备响应式侧边栏、固定 Header、面包屑、主题与外观设置，并使用 TanStack Query、React Hook Form、Zod 和统一 Axios client。
- `web_embed` build tag 已将 `web/dist` 嵌入发布二进制；SPA fallback 不覆盖 `/api/v1/*`、`/mcp` 和保留的协议路径。
- `make dev` / `make web` 保持前后端分离开发，`make linux` 执行前端构建并产出包含 Web Console 的 Linux 单二进制。

截至 v0.4.0，管理台已经可用，但仍偏向“把接口接出来”：核心任务缺少连贯引导，知识库页面信息层级单薄，检索、文件树、FAQ、Chunk 和 Generation 等既有能力尚未形成完整操作体验。该缺口已由 v0.5.0 集中解决。

### v0.5.0 - Web Console 整体使用体验（已完成）

目标：把管理台从“可调用真实接口”提升为“无需理解内部模型也能完成知识导入、检查、修订和检索验证”的完整工作台。本版本优化信息架构、任务闭环和交互反馈，不以单纯换肤或增加装饰性 Dashboard 为目标。

- 重整全局信息架构：复用现有 `WorkspaceSwitcher -> Workspace 导航组 -> NavUser` 侧边栏；平台管理员追加独立平台管理组，知识库能力进入页面内二级导航，外观与退出等用户操作保留在 `NavUser`/头像菜单。
- 提供首次使用引导和持续可见的准备状态：`创建/配置模型连接 -> 创建模型 -> 创建知识库 -> 导入内容 -> 等待索引 -> 验证检索`；每一步都给出真实状态、下一步动作和失败恢复入口。
- 将知识库详情重构为聚焦的工作台，至少包含：
  - 概览：当前模型、内容版本、active Generation、stale/构建状态和最近任务。
  - 内容：统一浏览 File 与 FAQ，文件使用真实 File Tree；Web 只展示已有数据合同，不在本版本实现 crawler。
  - 检索测试：输入查询并展示融合分数、来源、完整上下文、实际命中的 child/flat、metadata 和 document anchors，可继续定位到原文或 Chunk。
  - 索引：查看 Generation 历史、构建进度、失败原因、stale 状态，并为有权限的用户提供创建和激活操作。
  - 设置：管理知识库名称与描述；Embedding 模型、分块和检索配置通过新 Generation 构建并在激活后生效。
- 补齐已有后端能力的管理界面：File Tree 文件夹/节点操作、FAQ 创建与编辑、Chunk 详情/修订/启停、Document/Job 进度和错误诊断。
- 统一异步操作体验：提交后立即显示进行状态，轮询只在需要时发生；成功后刷新相关 Query，失败时展示可行动的错误信息和重试入口，不要求用户手动刷新页面猜测结果。
- 建立一致的 loading、skeleton、空状态、无权限状态、404、错误恢复、成功提示、危险操作确认和表单校验反馈。
- 权限对用户可解释：owner/admin/member 看到与自身权限一致的入口；隐藏不可用动作时仍能从页面说明理解由谁处理，而不是只在提交后收到 403。
- 完成桌面、窄屏和移动端适配；核心流程支持键盘操作、可见焦点、语义化标签和基本无障碍检查。
- 继续只展示真实后端数据，不虚构统计值；为改善当前流程允许新增小型查询或聚合接口，但不在 handler 或前端复制业务规则。

本版本不包含：

- Workspace API Token、MCP 业务工具或其它程序化鉴权。
- PDF/MinerU、crawler、新文档类型、Rerank 或 LLM 回答生成。
- 更换 React/TanStack/shadcn/ui 技术栈，或脱离真实任务的大规模 Design System 重写。

验收标准：

- 使用真实单二进制和临时 PostgreSQL/Redis 环境，通过浏览器完整走通“初始化 -> 配置模型 -> 创建知识库 -> 导入/创建内容 -> 等待索引 -> 检索验证 -> 查看来源 -> 修订并重建”的主流程。
- File、FAQ、Chunk Revision、Generation 和混合检索均可通过 Web Console 使用，不需要借助 curl 或直接操作数据库。
- owner/admin/member 的可见操作和后端权限矩阵一致，跨 Workspace 数据不泄漏。
- 每个核心页面都有明确的 loading、空、错误和完成状态；异步操作完成后无需刷新浏览器才能看到结果。
- 浏览器 E2E、组件交互测试、`pnpm check`、`pnpm test`、`pnpm build` 和单二进制 SPA smoke 全部通过。

完成证据：Workspace readiness、KnowledgeBase summary/Job/Chunk 查询与基本信息更新均已接入真实 REST；Web Console 已交付独立深链的 Overview、All、Files、FAQ、Web、Search、Indexes、Settings 页面，File Tree 仅接收 File，FAQ/Web 使用桌面表格与移动卡片。member 可管理内容与检索，Chunk Revision、Generation 和知识库设置写操作仍为 admin/owner。组件/浏览器测试、临时 pgvector + Redis 集成测试以及真实 `web_embed` 深链 smoke 均已覆盖该边界。

### v0.6.0 - Workspace API Key、MCP 与程序化访问（已完成）

目标：在已交付的 REST 混合检索和 v0.5.0 可视化工作台之上，完成独立于浏览器 session 的程序化消费闭环。

详细设计：[`docs/superpowers/specs/2026-08-01-v0.6.0-workspace-api-key-mcp-design.md`](docs/superpowers/specs/2026-08-01-v0.6.0-workspace-api-key-mcp-design.md)。产品界面与新合同统一使用 “Workspace API Key”，已有表名 `workspace_api_tokens` 保留。

- 实现 `workspace_api_tokens` 签发、前缀、最小权限 scope、可选到期/不限期、吊销与鉴权；SHA-256 hash 只用于鉴权索引，域隔离的 AES-256-GCM 密文只用于 owner/admin 后续 reveal/复制。
- 每个 API Key 必须绑定当前 Workspace 内一个或多个 KnowledgeBase；除 key 自己创建并原子加入绑定的新知识库外，REST/MCP 只能访问绑定范围，越界资源统一返回 `404`。
- 浏览器继续使用 HttpOnly session；REST/MCP 客户端使用 Bearer API Key。两种身份写入统一 `AuthContext`，但机器身份额外携带 scope 与允许的 KnowledgeBase ID 集合。
- 将 `server.public_base_url` 收敛为全局 `server.base_url`，统一派生 Web、REST、MCP 和邀请链接，不增加 MCP 专用 public URL。
- 保持已交付的 REST 检索、Document 删除和 Chunk 读取合同，并补齐程序化调用所需的稳定错误结构和使用文档。
- MCP 工具：
  - `knowledge_base_create`
  - `document_ingest`
  - `document_status`
  - `knowledge_search`
  - `document_delete`
  - `chunk_get`
- MCP 挂在同一个 HTTP 服务的 `/mcp`，不进入 `/api/v1/*` 或 SPA fallback。
- REST 与 MCP 复用 application service，不在 MCP tool 中重复实现检索、权限或状态机规则。
- `knowledge_search` 支持同时检索一个或多个已绑定 KnowledgeBase：按 active Generation 的 Embedding 模型快照分组、每组只生成一次 query embedding、组内检索后用统一 RRF 确定性合并，并在每条结果中返回 KnowledgeBase 来源。

验收标准：

- REST 与 MCP 返回一致的多知识库检索证据结构，包含 KnowledgeBase 来源、score、source、metadata、chunk text 和 document anchors，不返回 LLM 生成答案。
- Workspace API Key 可独立于登录 session 完成授权；owner/admin 可随时从详情页再次 reveal/复制；过期、scope 不足、知识库越界或吊销后按合同失败。
- 六个 MCP 工具通过真实 HTTP、临时 PostgreSQL/Redis 和跨 Workspace 负向 E2E。
- 日志、错误响应和 MCP 内容中不泄漏 token、Provider credential 或完整用户文档。

完成证据：v0.6.0 已交付。全局 `server.base_url` 派生 Web/REST/MCP/邀请地址（生产 HTTPS）；Workspace API Key 采用 `lhk_`+32-byte 随机的 SHA-256 鉴权 + HKDF/AES-256-GCM 可恢复密文，owner/admin 可重复 reveal；六个 typed MCP 工具（`knowledge_base_create`、`document_ingest`、`document_status`、`knowledge_search`、`document_delete`、`chunk_get`）由 mcp-go v0.57.0 stateless Streamable HTTP 提供，`/mcp` 只接受 Bearer；多知识库 `knowledge_search` 按 Embedding 模型五元组快照分组、每组一次 query embedding、统一 RRF 确定性合并；Web Console 提供列表/整页创建/详情 reveal/吊销。验收命令：`go test ./...`、`go test -tags=integration -p 1 ./internal/infrastructure/db ./internal/infrastructure/migrate ./cmd/langhuan`（含 v060 真实 E2E：admin 创建 key→Bearer REST 多库检索→reveal→吊销→401，以及 `/mcp` 有效 Bearer `tools/list` scope 过滤）、`pnpm --dir web check`、`pnpm --dir web test`（250 通过）、`pnpm --dir web build` 全部 exit 0。接入文档见 `docs/API_ACCESS.md`。

### 飞书知识库内容源同步（已交付）

在 v0.6.0 程序化消费闭环之上，新增飞书云文档/知识库作为内容源，复用现有 parse/chunk/index 管线，不引入新的 DocumentKind。飞书文档以 `file` kind 落库（当 Markdown 处理），来源语义由 `documents.source_type="feishu"` + `external_id`（飞书节点稳定 token）承载。

- 多飞书应用：`workspace_source_connections` 支持每个 Workspace 注册多个飞书内部应用；`app_id` 存 `config jsonb`，`app_secret` 经 `credential_cipher` AES-256-GCM 加密落库（AAD 前缀 `source-connection:`，与 model-provider 物理隔离），不写入 YAML；List/Get 不回显 secret。
- 飞书 SourceConnector 适配器（`internal/adapters/source/feishu`）基于飞书官方 SDK `github.com/larksuite/oapi-sdk-go/v3`，实现 `ListTree`（递归 wiki/drive 节点树）与 `Fetch`（docx raw_content → markdown），通过薄接口 `feishuAPI` 抽象 SDK 调用使编排逻辑可单测。
- 全量 + 增量同步：`SourceSyncService` 列举整棵目录树 → 按 `source_config.sync_cursor`（obj_edit_time）增量跳过未变更 docx → Fetch → rawStore.Put → 单事务建 Document(file, external_id) + Revision(reason=crawl, markdown) + FileTreeNode + Job(document_parse_start) → 入队复用现有解析管线；删除检测软删飞书侧已删除的文档。
- Meta Scheduler 限流：进程内单 goroutine（`SourceSyncScheduler`）按 `source_connection_id` 限流，`max_concurrent_per_connection`（`config.yaml`，默认 2）通过 `idx_jobs_conn_active` 部分索引查询；cron 推进 `next_sync_at`；worker 完成后 `TryDispatchConnection` 续跑。
- HTTP CRUD：`POST/GET/PATCH/DELETE /api/v1/workspaces/:slug/source-connections`（Session admin/owner，API Key 不可达）、`POST /api/v1/workspaces/:slug/knowledge-bases/:id/sync`（返回 `202 {job_id}`）；KB 创建支持 `source_type`/`source_config`/`source_connection_id`。
- 数据库：迁移 000017 放宽 `jobs_target_check` 第三分支（source_sync 仅 KB），000018 新增 `workspace_source_connections` 表与 `knowledge_bases`/`documents`/`jobs` 来源字段及部分索引。
- Web Console：集成页（飞书应用列表/表单）与 KB 创建来源切换、详情同步状态、手动同步。

验收：飞书协议正确性由 Task 4 的 `feishuAPI` fake 单测覆盖（薄接口设计下协议逻辑单测已充分）；worker 业务正确性由 Task 6 单测覆盖（fake store/connector）；持久化层（`CreateSourceSyncJob` / `CountActiveByConnection` / `CreateSyncedDocumentNodeRevisionAndJob` / `SoftDeleteDocument` / `UpdateSyncCursor`）由 `internal/infrastructure/db/source_sync_store_integration_test.go` 的 DB 集成测试覆盖；全链路 e2e 因官方 SDK mock 复杂度，依赖真实飞书凭证手动验收。文档见 `docs/ARCHITECTURE.md` 8.1、`docs/API_ACCESS.md` 8.2、`docs/DATABASE_GUIDELINES.md` 9.z。

#### 飞书同步健壮性改进（已交付）

在上述飞书同步能力之上，修正增量同步的生命周期与失败恢复语义，降低目录快照不完整、内容漂移、限流、大文档与大规模删除造成的数据风险。本改进不扩展首版能力边界（sheet/bitable 的异步导出与多子表建模仍未支持）：

- 快照完整性闸门：`SourceConnector.ListTree` 返回 `TreeSnapshot{Complete, Warnings, MaxEditTime}`；`Complete=false` 时禁止删除任何 Document/folder 且全局不推进 cursor；fatal error 返回 error 不应用任何节点。
- 稳定 Document 身份：以 `external_id` 复用同一 Document（不再每次新建 UUID），revision_no 单调递增；`documents.content_hash` 记录最新已接受 source revision 的 SHA-256，hash 未变且无待重试状态时不重建。
- 失败可恢复：Fetch/落库/入队失败、超限文档、零值 EditTime 节点都不会被 cursor 永久越过；`RetryRequired` 覆盖 hash 去重，失败后下次仍会重试。
- force 同步与队列合并：`POST .../sync {"force":true}` 写 `source_config.sync_requested_force` latch，worker 原子消费后在当前 active Generation 下重建所有 docx；同一 KB 复用 active Job，force 意图在 enqueue/consume/finalize 竞态下不丢失；force 不创建/激活新 IndexGeneration。
- 删除策略与异步清理：`source_config.on_delete` 支持 `keep`（默认，软删保留审计/恢复）/ `remove`（DB 级联删除后由幂等 `source_cleanup` Job 异步清理外部对象）；`PATCH .../source-policy` 只更新 `on_delete` 且保留其它运行期配置。
- 内容大小保护：`source_sync.max_content_bytes`（默认 50MiB）在 connector 与 application 两层生效，超限不无界读入。
- 数据库：迁移 000022 新增 `documents.content_hash`、`file_tree_nodes.external_id`、两个 (workspace,kb,external_id) 唯一部分索引、`jobs_target_check` 第四分支（source_cleanup 仅 KB），并对历史重复 external_id fail-fast。

验收：纯函数（diff/去重/安全 cursor watermark）由表驱动单测覆盖；service 编排、worker latch 流程、cleanup 幂等/重试、force 合并、partial 结果由应用层单测覆盖；持久化层与迁移由临时 pgvector/zhparser 容器的 DB 集成测试覆盖。设计规格见 `docs/superpowers/specs/2026-08-07-feishu-sync-robustness-improvements-design.md`。

### v0.7.0 - MinerU Cloud PDF 解析与资产归档（已完成）

目标：在可视化和程序化消费闭环之上，补齐 PDF 这一最重的输入格式。

- 实现 S3-compatible `RawDocumentStore` 与 `AssetStore`。
- 实现 `parser/minerucloud` adapter，支持 MinerU Cloud 异步流程：
  - 申请上传地址。
  - 上传 PDF。
  - 轮询解析状态。
  - 获取 Markdown 或 zip 结果。
- 实现 `AssetResolver`：
  - 处理 Markdown 图片 `![alt](url)`。
  - 处理 HTML `<img src="...">`。
  - 处理 base64/data URI 图片。
  - 处理 zip 内相对路径图片。
  - 通过统一的 SSRF-safe HTTP client 下载远程图片并上传到自有对象存储。
- 保存服务端 MinerU Provider credential（支持过期、启停、轮换），不作为长期 YAML 配置。
- 将 normalized Markdown、重新解析得到的结构化 parse manifest 和 `document_assets` 归属到当前 Document Revision，继续进入统一 ChunkSet/Generation 分块流程。
- Web Console 补充 PDF 上传、解析阶段进度、warning、失败诊断和资产预览。

验收标准：

- PDF 经 MinerU Cloud 转成 Markdown，重新解析为结构化 manifest 后通过统一事实层到达 active Generation、可被 REST/Web/MCP 检索。
- Markdown 中图片地址替换为自有对象存储地址；资产有 hash、mime、size、storage key 和访问合同记录。
- 远程资源下载通过 DNS/IP/redirect 全链路 SSRF 防护，异常资源受大小、数量和超时限制。
- 解析失败保存可诊断错误，重复 poll/index 不产生重复 Revision、Chunk 或 Asset。

### v0.8.0 - 可靠性与运维能力

目标：让服务具备长期内部运行和进入 v1.0.0 发布验收所需的可观测、可恢复和资源保护能力。

- 明确 asynq 重试、退避、终止失败和 dead-letter 检查策略。
- 提供 Document/Generation reindex 与失败任务重试入口，并保证幂等。
- 记录 upload/parse/chunk/embedding/index/publish 分阶段耗时和关键计数。
- 统一结构化日志字段和敏感信息脱敏。
- 提供基础 health/readiness、metrics 和队列/worker 可见性。
- 为 failed/staging/retired 投影接入可控的定时 cleanup，保留 active Generation 和恢复窗口内的事实数据。
- 支持受限批量导入，并配置文件大小、图片数量、图片大小、解压后大小、chunk 数量、embedding batch 和并发限制。
- 完成数据库、对象存储与配置的备份/恢复演练；`v1.0.0` 前仍按“仅支持全新安装”政策处理 schema 变更。

验收标准：

- 可定位导入慢、解析失败、embedding 失败、索引失败和发布失败发生在哪个阶段，并关联 workspace/document/revision/job/generation。
- 大文件、压缩炸弹、异常图片、慢 Provider 和积压任务不会无限占用 worker 资源。
- 失败文档可安全重试，重复请求和 worker 重启不破坏 active Generation。
- 在空数据库与空对象存储上可重复完成安装、备份、恢复和单二进制 smoke。

### v1.0.0 - 首次对外发布与兼容基线

目标：冻结首个可对外使用的产品、协议和数据兼容基线。从该版本起，不再以“删除内部测试库重新安装”替代正式升级路径。

- 确认并记录稳定的 REST、MCP、认证、错误码、检索证据和异步状态合同。
- 发布完整的安装、配置、初始化、模型接入、备份恢复、升级、监控和故障排查文档。
- 明确受支持的 PostgreSQL/pgvector、Redis、对象存储、浏览器和部署平台版本。
- 对默认配置、凭证管理、Cookie、Bearer token、文件处理、SSRF、日志脱敏和租户隔离完成发布前安全审查。
- 冻结 v1 schema 兼容起点；此后的每次 schema 变更必须提供真实数据升级测试、回滚/恢复说明和支持版本范围。
- 以单一版本化二进制发布，不要求额外部署独立 Web 静态站点。

验收标准：

- 新用户仅依赖发布文档即可从空环境完成安装，并通过 Web Console、REST 和 MCP 跑通完整知识处理与检索链路。
- 发布构建、数据库/Redis/worker 集成、浏览器 E2E、MCP E2E、安全负向测试、备份恢复和故障恢复演练全部通过。
- 版本号、二进制、Web Console、MCP server、文档和可观测信息报告同一个 release version。
- 仓库明确记录从 v1.0.0 开始的数据兼容承诺和后续迁移准入规则。

## 暂不进入首版的能力

- Chat/Agent/回答生成。
- LLM 摘要、问题生成、自动实体关系抽取。
- PostgreSQL AGE 图查询。
- GraphRAG。
- 多向量数据库适配。
- ~~IM、飞书、Notion、语雀等数据源同步。~~ 飞书知识库同步已交付（见上方「飞书知识库内容源同步」）；Notion、语雀等其它数据源仍不在首版范围。

## 未来方向

- 接入 PostgreSQL AGE 做图索引和图查询。
- 增加外部写入 entities/relations 的 API。
- ~~增加 rerank adapter。~~ 已于当前版本交付：`rerank_compatible` Provider、Generation 重排快照、单库/多库重排与结构化日志。
- 增加更多 parser adapter。
- 扩展 workspace 权限模型，例如资源级权限或更细粒度的 token scope。
- ~~增加数据源同步框架。~~ 已部分交付：飞书知识库同步框架（`SourceConnector` port + 飞书适配器 + Meta Scheduler）已落地，支持接入新的内容源 provider；详见 ROADMAP「飞书知识库内容源同步」与 `docs/ARCHITECTURE.md` 8.1。
- 把 FTS / 向量读写从 `RetrievalRepository` 拆成 `adapters/index/` 下各自的 port + adapter。触发条件：真正出现可替换的全文检索后端（如 Elasticsearch / Meilisearch）或多向量库适配需求时再拆；否则保持当前合并实现，避免提前抽象。
