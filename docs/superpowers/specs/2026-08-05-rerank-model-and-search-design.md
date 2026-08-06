# 琅嬛 Rerank 模型与检索重排设计规格

> 状态：Draft，等待评审；本文只定义设计与实施边界，不包含业务代码修改。
>
> 日期：2026-08-05

## 1. 摘要

本规格为琅嬛增加完整的 Rerank 能力闭环：管理员创建 Rerank Provider 连接和模型、执行真实连接测试、在 KnowledgeBase 的新 Index Generation 中选择模型、检索时对混合召回结果做重排，并记录不含正文和凭证的结构化执行日志。

推荐方案把 Rerank 作为 active Generation 的不可变查询阶段快照：

```text
query
  -> query embedding
  -> pgvector topK + PostgreSQL FTS topK
  -> deterministic RRF
  -> 加载 evidence 并按 parent 聚合
  -> 取 rerank_candidate_top_k
  -> Rerank Provider
  -> 稳定排序
  -> final_top_k evidence
```

首个实现范围提供 `rerank_compatible` Provider，使用本规格固定的 `/v1/rerank` 请求/响应合同，可接入遵循该合同的服务。它不是“OpenAI Rerank”——OpenAI 没有统一的 Rerank API 标准。后续厂商协议出现真实需求时，再在同一 `ports/rerank` 下增加原生 adapter。

本规格不改变琅嬛的产品定位：只返回检索 evidence，不生成答案，不编排 Chat/Agent。

## 2. 当前事实与约束

### 2.1 已有能力

- `models.type` 数据库约束和 `value.ModelType` 已预留 `rerank`，但 application service、registry、DTO 和 Web Console 当前只允许 `embedding`。
- 模型配置采用 `model_providers + models` 两层结构，Provider 负责连接、作用域和加密凭证，Model 负责模型名和类型参数。
- Provider 支持 `platform` 和 `workspace` 两种作用域；Workspace 可见平台共享连接和自己的连接。
- KnowledgeBase 的当前 Embedding、分块和检索配置来自唯一 active Generation 的不可变快照。
- 当前检索顺序是 Vector/FTS 召回、RRF 融合、父块聚合、`final_top_k` 截断。
- 多知识库检索会按 Embedding 五元组分组，同组只生成一次 query embedding，再做全局确定性合并。
- REST、MCP 和 Web Console 复用 application service；MCP `knowledge_search` 不单独实现检索规则。
- 日志使用 `log/slog` JSON handler，但 HTTP request ID、检索阶段耗时和统一字段尚未形成合同。

### 2.2 必须保持的约束

- 所有资源显式受 `workspace_id` 隔离，跨 Workspace 资源继续隐藏为 404。
- Rerank 输入可能包含用户查询和文档内容，任何日志都不得记录 query、候选正文、向量、Provider 请求体或响应体。
- Provider credential 继续使用现有 AES-256-GCM 加密，不进入响应、前端 Query cache、日志或错误消息。
- 所有外部 HTTP 调用透传 `context.Context`，支持超时和取消。
- Repository 只做持久化；Rerank 调用、失败策略和排序规则属于 application/adapter。
- 数据库测试只能使用测试期临时启动并销毁的 `langhuan-test-postgres:pg17` 容器。
- Web Console 继续使用现有 AppShell、TanStack Query、React Hook Form、Zod、Tailwind、shadcn/Radix 和 Biome。

## 3. 默认假设与待评审决策

用户本轮要求不提问，因此本文直接采用以下建议默认值。评审时可以逐项调整，但实现前必须得到一份内部一致的最终规格。

1. 首个 Rerank adapter 是 `rerank_compatible`，wire contract 固定为本文第 9 节；不同时实现 Cohere、Jina、DashScope 等多个厂商特例。
2. Rerank 默认关闭；新建 KnowledgeBase 不强制选择 Rerank 模型。
3. 启用 Rerank 后，配置归属 active Generation，不放在可随时改变的 KnowledgeBase 字段中。
4. 默认 `rerank_candidate_top_k=50`，合法范围 `50..200`。当前 `final_top_k` 上限为 50，因此候选数始终覆盖调用方允许请求的最终结果数。
5. 默认失败策略为 `fallback`：仅当远端超时、限流、暂时不可用或返回非法结果时，回退到原 RRF 顺序，并通过响应字段与 Warn 日志明确标记；配置错误、租户错误和快照漂移不回退。
6. 不设置通用 score threshold。不同 Rerank 模型的分数不可直接解释为概率，也不保证都位于 `[0,1]`。
7. 单库每次查询最多发起一次 Rerank 调用；配置一致的多知识库查询全局最多发起一次。
8. 多知识库检索要求所有 active Generation 的 Rerank 快照完全一致，或全部关闭；混合配置返回稳定冲突错误，不尝试归一化不同模型的分数。
9. 首版切换 Rerank 配置仍走现有“创建、构建、激活 Generation”流程。即使只改查询阶段配置也会重建投影；复用投影的快速 Generation 作为后续优化，不进入本规格。
10. 不持久化逐次搜索历史；结构化日志由部署方的日志系统负责采集和保留。

## 4. 目标与非目标

### 4.1 目标

- 允许平台管理员或 Workspace admin/owner 创建、编辑、停用、测试 Rerank 模型。
- 复用既有 Provider 作用域、凭证加密、可见性和权限规则。
- 通过独立 `ports/rerank` 隔离 application 与第三方协议。
- 在 Generation 中保存完整、可验证的 Rerank 模型快照和运行参数。
- 单库 REST、Workspace 多库 REST、MCP `knowledge_search` 使用相同重排算法。
- 父子分块与 FAQ 的检索语义在 Rerank 后保持正确。
- 保留原 RRF 分数，并单独返回 Rerank 分数和实际排序阶段。
- 为连接测试和每次检索记录可关联、可统计、无敏感正文的结构化日志。
- Web Console 可完成“添加连接 -> 添加模型 -> 测试 -> 新建 Generation 启用 -> 检索验证”的完整任务。

### 4.2 非目标

- 不生成 LLM 答案，不增加 ChatModel 或 Agent。
- 不做在线学习、点击反馈训练、自动选择模型或 A/B 实验平台。
- 不做 score threshold、MMR、多阶段多模型级联或按请求指定任意 Rerank 模型。
- 不把不同 Rerank 模型的原始 score 归一化后混排。
- 不拆分现有 PostgreSQL FTS/pgvector `RetrievalRepository`；Rerank 是独立外部能力，不触发索引后端抽象。
- 不新增搜索历史表、日志查询页面、Prometheus 指标或分布式 tracing 后端。
- 不在首版实现只改查询配置时复用旧 Generation 投影的快速路径。
- 不在首版实现厂商自动模型发现。

## 5. 方案比较

### 5.1 方案 A：Rerank 写入 active Generation 快照（推荐）

Rerank 模型身份、配置 hash、候选数和失败策略都保存在 Generation。查询只读取 active Generation。

优点：

- 与现有 Embedding、chunking、retrieval 配置事实源一致。
- REST、MCP、Web 和多知识库检索不会各自选择不同模型。
- 配置可追溯，日志可以准确关联 Generation。
- 旧 Generation 的检索行为不会被知识库可变字段静默改变。

缺点：

- 当前架构下仅切换 Rerank 也要创建并重建 Generation。
- `knowledge_base_index_generations` 需要增加 nullable 快照字段。

结论：采用。优先正确性与可解释性，投影复用等实际出现成本问题后再设计。

### 5.2 方案 B：KnowledgeBase 保存可变 `rerank_model_id`

优点：修改后立即生效，不需要重建索引。

缺点：active Generation 不再是完整检索配置事实源；历史 Generation 无法解释当时真实排序；多库查询开始和结束之间可能读到不同配置；Provider 参数更新也会产生难以定位的漂移。

结论：不采用。

### 5.3 方案 C：每个搜索请求携带 `rerank_model_id`

优点：调用方最灵活，便于临时实验。

缺点：程序化 API Key 可以探测并调用不属于目标 KB 配置的模型；缓存键、权限、成本控制和日志基数更复杂；同一 KB 没有稳定检索行为。

结论：不采用。未来如做受控实验，应设计独立的评测能力，不污染生产搜索合同。

## 6. 总体架构

```text
┌──────────────────────────────── 管理面 ────────────────────────────────┐
│                                                                      │
│  Web Console / REST                                                  │
│      │                                                               │
│      ├─ 创建 rerank_compatible Provider（连接 + 加密凭证）           │
│      ├─ 创建 type=rerank Model（模型名 + typed parameters）          │
│      ├─ 连接测试                                                     │
│      └─ 创建新 Generation 并选择 Rerank                              │
│                               │                                      │
└───────────────────────────────┼──────────────────────────────────────┘
                                ▼
                    PostgreSQL immutable Generation
                    ├─ embedding snapshot
                    ├─ chunking/retrieval snapshot
                    └─ optional rerank snapshot
                                │
┌──────────────────────────── 查询面 ───────────────────────────────────┐
│                                ▼                                     │
│  REST / MCP -> SearchService -> Vector + FTS -> RRF -> Evidence      │
│                                                   │                  │
│                                                   ▼                  │
│                                      private rerank documents        │
│                                                   │                  │
│                                      RerankClientResolver            │
│                                                   │                  │
│                                      ports/rerank.Client             │
│                                                   │                  │
│                                      rerank_compatible adapter       │
│                                                   │                  │
│                                      external /v1/rerank             │
│                                                   │                  │
│                              validated scores -> stable final order  │
└──────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
                   structured slog events（无 query/content）
```

### 6.1 分层职责

- `domain/value`：Rerank failure mode 等稳定枚举。
- `domain/model`：Generation 的可选 Rerank 快照；仍是纯 struct。
- `ports/rerank`：Rerank 客户端与 factory 接口，由 application 使用方定义。
- `adapters/rerank`：协议实现、HTTP 安全、响应校验和供应商错误清洗。
- `application/service`：模型生命周期、客户端解析、候选构造、调用时机、fallback 和确定性排序。
- `infrastructure/db`：Generation 字段的 Row/codec/repository 与引用统计。
- `interfaces/http` / `interfaces/mcp`：协议解码、权限和 DTO 输出，不实现排序规则。
- `web/src/features`：管理模型、配置 Generation、展示真实检索阶段。

## 7. 领域和 Port 合同

### 7.1 Rerank Port

建议新增 `internal/ports/rerank`，核心合同如下：

```go
type Document struct {
    ID   string
    Text string
}

type RerankInput struct {
    Query     string
    Documents []Document
    TopN      int
}

type RerankItem struct {
    DocumentID string
    Score      float64
}

type RerankResult struct {
    Items []RerankItem
}

type Client interface {
    Rerank(context.Context, RerankInput) (*RerankResult, error)
}
```

`Document.ID` 使用 application 生成的短生命周期 opaque ID，不把 UUID、正文或数据库行交给 adapter 解释。adapter 可以按位置调用上游，但返回前必须恢复并验证 `DocumentID`。

Factory 负责：

- 严格解码 Provider config 和 credentials。
- 严格解码 `type=rerank` 的 Model parameters。
- 构造带安全 HTTP client 的 `rerank.Client`。
- 返回本 Provider 支持的 capability 和 credential field allowlist。
- 把第三方错误转换成不包含响应正文的 typed error。

### 7.2 输入与输出不变量

- Query trim 后长度为 `1..4096` 个 Unicode 字符。
- Documents 数量为 `1..model.parameters.max_documents`。
- 每个文档用于远端调用的文本非空，截断后最大字符数由模型参数控制。
- `TopN` 为 `1..len(Documents)`；琅嬛调用时固定传 `len(Documents)`，要求远端返回全部候选的分数。
- 返回 item 数量必须等于输入文档数。
- 每个 DocumentID 必须恰好返回一次，不允许未知、重复或缺失 ID。
- score 必须是有限浮点数；不要求 `[0,1]`，不在 application 做 sigmoid 或 min-max 归一化。

### 7.3 Runtime Resolver

新增 `RerankClientResolver`，只解析当前 Workspace 可见、Provider active、Model active、`type=rerank` 的模型。

解析结果至少包含：

```text
Client
ModelID
ProviderID
ProviderKey
ModelName
ModelConfigHash
MaxDocuments
MaxQueryChars
MaxDocumentChars
```

`ModelConfigHash` 由以下非敏感、影响语义的字段计算：

```text
provider key
provider config
model_name
model parameters
```

Embedding 模型还必须包含 `dimensions`；Rerank 的 `dimensions` 固定为空，不进入 hash。这样 hash 与两类模型各自真正影响运行语义的字段保持一致。

credential ciphertext、显示名称、状态、创建时间和统计值不得进入 hash。凭证轮换不应让 Generation 失效；Endpoint、模型名或参数变化必须创建新 Provider/Model/Generation。

作为本功能的必要正确性加固，`ResolvedEmbeddingClient` 也应返回并校验当前 `ModelConfigHash`，与 Generation 已保存的 `model_config_hash` 对比。否则现有 Embedding Provider config 或 parameters 被修改后，运行时会静默偏离 Generation 快照。

## 8. 模型配置合同

### 8.1 Provider key：`rerank_compatible`

Provider config：

```json
{
  "base_url": "https://api.example.com",
  "endpoint_path": "/v1/rerank",
  "timeout_seconds": 30,
  "retry_times": 2
}
```

规则：

- `base_url` 必填，不包含 query、fragment 或 credential。
- Workspace Provider 只允许公网 HTTPS；platform Provider 可由平台管理员配置受控的 HTTP/内网地址，沿用现有平台级信任边界。
- `endpoint_path` 必须是以 `/` 开头的相对路径，默认 `/v1/rerank`，不得包含 scheme、host、query、fragment 或 `..`。
- `timeout_seconds` 为 `1..120`，默认 30。
- `retry_times` 为 `0..3`，默认 2。

Credentials：

```json
{
  "api_key": "secret",
  "custom_headers": {
    "X-Tenant": "optional-secret-value"
  }
}
```

规则：

- `api_key` 必填，统一以 `Authorization: Bearer <api_key>` 发送。
- `custom_headers` 整体加密；禁止覆盖 `Authorization`、`Host`、`Content-Length`、`Content-Type`、`Accept-Encoding` 和 hop-by-hop headers。
- header 名和值都限制长度和数量，拒绝 CR/LF。

### 8.2 Rerank Model parameters

```json
{
  "max_documents": 100,
  "max_query_chars": 4096,
  "max_document_chars": 8192
}
```

规则：

- `max_documents`：`50..200`，默认 100。
- `max_query_chars`：`256..4096`，默认 4096。
- `max_document_chars`：`512..32768`，默认 8192。
- Rerank Model 的 `dimensions` 必须为 `null` 或在请求中省略。
- JSON strict decode，未知字段返回 `invalid_provider_config`。

### 8.3 模型 CRUD 的通用化

现有 Model application service 从“只管理 Embedding”改为按 `ModelType` 路由 capability factory：

- `embedding` 继续进入 Embedding registry。
- `rerank` 进入 Rerank registry。
- `llm` 继续返回 `unsupported_model_type`。

HTTP create/update DTO 的 `dimensions` 改为可选：

- `embedding`：必须存在且命中已支持维度。
- `rerank`：必须不存在或为 JSON `null`。

Model response 的 `dimensions` 使用 nullable/optional 字段；不得用 `0` 表示 Rerank 没有维度。

### 8.4 引用与不可变性

模型或 Provider 一旦被任意 Generation 引用：

- 允许修改 `display_name`、`description`、`status`。
- 允许只轮换 credentials。
- 禁止修改 Provider config、Model `model_name`、`dimensions` 和 `parameters`；需要新建连接或模型。
- 删除继续返回 `model_in_use` / `provider_in_use`。

Repository 的引用统计同时覆盖：

```text
knowledge_base_index_generations.embedding_model_id
knowledge_base_index_generations.rerank_model_id
```

Web 中原来的“知识库引用数”改为准确的“配置快照引用数”，避免把 retired Generation 引用误称为当前知识库数量。

### 8.5 连接测试

Embedding 测试合同保持不变；Rerank 使用固定测试数据：

```text
query:     "langhuan rerank connection test"
documents: ["unrelated sample", "langhuan rerank connection test"]
top_n:    2
```

测试只验证网络、协议、数量、唯一索引和有限 score，不把某个文档必须排第一作为正确性条件。

统一响应：

```json
{
  "ok": true,
  "type": "rerank",
  "duration_ms": 87,
  "dimensions": null,
  "result_count": 2
}
```

Embedding 返回 `type=embedding`、真实 `dimensions`，`result_count` 可省略。测试文本和第三方 response 不写日志。

## 9. `rerank_compatible` Wire Contract

### 9.1 请求

```http
POST {base_url}{endpoint_path}
Authorization: Bearer ***
Content-Type: application/json
Accept: application/json
```

```json
{
  "model": "bge-reranker-v2-m3",
  "query": "用户查询",
  "documents": ["候选一", "候选二"],
  "top_n": 2,
  "return_documents": false
}
```

### 9.2 响应

```json
{
  "results": [
    {"index": 1, "relevance_score": 0.91},
    {"index": 0, "relevance_score": 0.37}
  ]
}
```

规则：

- `index` 必须覆盖请求 documents 的全部位置且不重复。
- `relevance_score` 必须有限。
- 响应允许包含其它字段，但 adapter 只读取 allowlist 字段。
- response body 读取上限为 2 MiB；超过上限视为非法响应。
- 3xx 不自动跟随，或每次 redirect 都重新执行 DNS/IP/协议校验；建议首版直接拒绝 redirect。
- 仅对网络瞬时错误、429 和 5xx 重试；不重试其它 4xx 和非法响应。
- 重试采用有上限的指数退避并服从 context；客户端取消后立即退出。
- typed error 不包含 URL credential、header、request body、response body或完整第三方错误文本。

## 10. Generation 与数据库设计

### 10.1 Migration

新增迁移：

```text
000014_rerank_generation_snapshot.up.sql
000014_rerank_generation_snapshot.down.sql
```

在 `knowledge_base_index_generations` 增加：

```sql
rerank_model_id          uuid NULL REFERENCES models(id) ON DELETE RESTRICT,
rerank_provider_id       uuid NULL REFERENCES model_providers(id) ON DELETE RESTRICT,
rerank_model_name        text NULL,
rerank_model_config_hash text NULL,
rerank_config            jsonb NOT NULL DEFAULT '{}'::jsonb
```

结构约束：

- 关闭 Rerank：四个 nullable 快照字段全部为 NULL，`rerank_config='{}'`。
- 启用 Rerank：四个字段全部非 NULL，`rerank_model_name` 和 hash 非空。
- `rerank_config` 必须是 JSON object。
- 数据库不尝试通过 CHECK 跨表验证 `models.type='rerank'`；application 在同一事务读取并校验。
- 为 `rerank_model_id` 和 `rerank_provider_id` 建普通索引，支持引用统计和删除保护诊断。
- 既有 Generation 原样迁移为 Rerank 关闭，不需要数据回填或重建。

`rerank_config` 首版固定形状：

```json
{
  "candidate_top_k": 50,
  "failure_mode": "fallback"
}
```

### 10.2 Domain snapshot

建议在 Generation 内使用聚焦值对象，而不是继续增加一组散落参数：

```go
type RerankSnapshot struct {
    ModelID        uuid.UUID
    ProviderID     uuid.UUID
    ModelName      string
    ModelConfigHash string
    CandidateTopK  int
    FailureMode    value.RerankFailureMode
}
```

`IndexGeneration.Rerank` 为 `*RerankSnapshot`；nil 表示关闭。Row codec 负责映射到列和 JSONB。

### 10.3 创建 Generation 的 API 输入

```json
{
  "embedding_model_id": "11111111-1111-4111-8111-111111111111",
  "chunking_config": {
    "strategy": "auto",
    "enable_parent_child": true,
    "parent_chunk_size": 4096,
    "child_chunk_size": 384,
    "chunk_size": 1000,
    "chunk_overlap": 100
  },
  "retrieval_config": {
    "fts_config": "zhparser",
    "vector_top_k": 30,
    "keyword_top_k": 30,
    "final_top_k": 10,
    "rrf_k": 60
  },
  "rerank": {
    "enabled": true,
    "model_id": "22222222-2222-4222-8222-222222222222",
    "candidate_top_k": 50,
    "failure_mode": "fallback"
  }
}
```

显式语义：

- `rerank` 省略：继承 base Generation 的完整 Rerank 快照选择，但重新从当前 Model 构造并校验 config hash。
- `{"enabled": false}`：新 Generation 关闭 Rerank，其它字段必须省略。
- `enabled=true`：`model_id`、`candidate_top_k`、`failure_mode` 全部必填。
- 选择模型时校验类型、状态、作用域和 Provider 状态。
- `candidate_top_k` 不得超过所选模型的 `max_documents`。
- 初始 KnowledgeBase Generation 默认 `Rerank=nil`。

Generation 的总 `config_hash` 必须包含 Rerank 快照；Rerank credentials 不进入 hash。

### 10.4 模型停用和漂移

- active Generation 引用的 Rerank Model/Provider 被停用后，按照配置错误处理，不自动使用其它模型。
- runtime 解析出的模型 ID、Provider ID、model name、model config hash 必须与 Generation 完全一致。
- 不一致返回 `rerank_snapshot_mismatch`，即使 `failure_mode=fallback` 也不回退；这是配置事实损坏，不是远端暂时故障。

## 11. 检索算法

### 11.1 单知识库

```text
1. 校验 Workspace、KnowledgeBase、query 和 topK override
2. 读取 ready active Generation
3. 解析并校验 Embedding 快照
4. 若启用 Rerank，解析并校验 Rerank 快照；此时只构造客户端，不发远端请求
5. 生成一次 query embedding
6. 在同一 Workspace transaction 中重新确认 active Generation 未切换
7. 分别执行 vector candidates 与 keyword candidates
8. deterministic RRF
9. 加载 evidence 与内部 search_content
10. 按有效 parent/flat 聚合，不做 final_top_k 截断
11. 若 Rerank 关闭：按 RRF 排序并截断 final_top_k
12. 若 Rerank 开启：
    a. 取聚合后前 candidate_top_k
    b. 构造 private rerank documents
    c. 调用一次 Rerank
    d. 校验结果并稳定排序
    e. 截断 final_top_k
13. 返回 evidence，不返回 private rerank text
```

必须先按父块聚合再取 Rerank 候选，避免同一父块的多个 child 占满外部调用名额。

### 11.2 Rerank 文本构造

Repository 的内部 `SearchEvidence` 增加 `SearchContent`，来自 `retrieval_entries.search_content`。它只用于排序，不进入 API DTO。

每个聚合结果的 Rerank 文本按以下规则构造：

- FAQ：只使用命中 entry 的 `search_content`（问题集合），不使用返回用回答。这样继续保持“索引问题、返回回答”的合同。
- File/Web parent：先按 RRF 顺序拼接去重后的 matched child `search_content`，再在剩余预算中补充父块完整正文。
- flat：使用 entry 的 `search_content`。
- 多段之间使用固定分隔符；去重、顺序和截断必须确定性。
- 超出 `max_document_chars` 时保留命中 child 文本优先，再截断补充上下文；不得在 adapter 内随机截断。
- private rerank text 不保存在数据库、不返回给客户端、不写日志。

### 11.3 分数与稳定排序

SearchResult 扩展：

```json
{
  "score": 0.0325,
  "vector_score": 0.83,
  "keyword_score": 0.41,
  "rerank_score": 0.91,
  "ranking_stage": "rerank"
}
```

语义：

- `score` 保持现有 RRF score，避免同名字段在启用 Rerank 后突然改变含义。
- `rerank_score` 只在成功应用 Rerank 时存在。
- `ranking_stage` 为 `rrf`、`rerank` 或 `rrf_fallback`。
- Web 文案不得把 score 显示为“准确率”或百分比。

Rerank 成功后的排序键：

```text
rerank_score DESC
RRF score DESC
knowledge_base_id ASC（多库）
chunk_id ASC
```

关闭或 fallback 后继续使用现有稳定 RRF 排序。

### 11.4 多知识库

在发起 embedding 或检索前，为所有 active Generation 计算 Rerank compatibility key：

```text
disabled
或
(rerank_model_id,
 rerank_provider_id,
 rerank_model_name,
 rerank_model_config_hash,
 candidate_top_k,
 failure_mode)
```

规则：

- 所有 key 都是 `disabled`：保持现有多库 RRF 流程。
- 所有 key 完全相同且启用：完成跨 KB 合并和父块聚合后，对全局前 `candidate_top_k` 发起一次 Rerank。
- key 不同或启停混合：返回 `rerank_configuration_conflict`，不发起任何模型调用。
- 错误响应说明“所选知识库的重排配置不一致，请统一配置或分开检索”，不暴露不可见模型或 KB 的内部详情。
- 多库最终 `final_top_k` 仍由请求 override 或现有服务端上限决定。

这个限制比跨模型 score 归一化更保守，但结果可解释、调用次数可控，也与当前多库 embedding 分组的确定性目标一致。

## 12. 失败处理

### 12.1 Failure mode

```text
fallback（默认）
  远端 timeout / 429 / 5xx / network unavailable / invalid response
  -> 返回原 RRF 排序
  -> ranking_stage=rrf_fallback
  -> Warn 日志

fail
  任意 Rerank 调用失败
  -> 整个搜索失败
  -> 返回稳定、脱敏错误
```

以下错误始终 fail closed，不受 `fallback` 影响：

- Workspace/KB 越界或无权限。
- active Generation 不 ready 或查询过程中切换。
- Rerank Model/Provider disabled、不可见或类型错误。
- Generation snapshot mismatch / config hash mismatch。
- credential 解密失败。
- application 依赖为空、配置 JSON 非法或内部 evidence 不完整。
- 调用方 context 已取消。
- 多知识库 Rerank 配置冲突。

### 12.2 稳定错误

建议新增或细化：

| Domain error | HTTP | code | 说明 |
|---|---:|---|---|
| `ErrRerankConfigurationConflict` | 409 | `rerank_configuration_conflict` | 多库快照不一致 |
| `ErrRerankSnapshotMismatch` | 409 | `rerank_snapshot_mismatch` | 运行时配置偏离 Generation |
| `ErrRerankUnavailable` | 503 | `rerank_unavailable` | fail 模式下远端暂时不可用 |
| `ErrRerankRateLimited` | 503 | `rerank_rate_limited` | 上游限流；有可信值时转发 Retry-After |
| `ErrInvalidRerankResponse` | 502 | `invalid_rerank_response` | 上游响应结构或 score 非法 |
| `ErrRerankInputTooLarge` | 400 | `rerank_input_too_large` | 配置和候选预算不一致 |

HTTP 响应不返回 endpoint、credential、第三方 body 或底层网络文本。MCP 使用同样 code/message 语义。

## 13. REST 与 MCP 合同

### 13.1 Provider options

现有 `supported_providers: string[]` 无法表达 capability，升级为：

```json
{
  "providers": [
    {"key": "openai", "capabilities": ["embedding"]},
    {"key": "rerank_compatible", "capabilities": ["rerank"]},
    {"key": "mineru", "capabilities": ["parser"]}
  ]
}
```

列表由 runtime registry 生成并稳定排序；前端不硬编码“某 Provider 一定支持哪种模型”。本项目尚未进入 v1.0.0 兼容基线，因此可以同步更新 Web 和测试，不保留旧响应双写。

### 13.2 Model endpoints

路由保持不变：

```text
POST   /api/v1/workspaces/:workspace_slug/model-providers/:provider_id/models
PATCH  /api/v1/workspaces/:workspace_slug/models/:model_id
POST   /api/v1/workspaces/:workspace_slug/models/:model_id/test

POST   /api/v1/admin/model-providers/:provider_id/models
PATCH  /api/v1/admin/models/:model_id
POST   /api/v1/admin/models/:model_id/test
```

Rerank create 示例：

```json
{
  "name": "bge_reranker",
  "display_name": "BGE Reranker v2",
  "description": "中文知识库重排",
  "type": "rerank",
  "model_name": "BAAI/bge-reranker-v2-m3",
  "parameters": {
    "max_documents": 100,
    "max_query_chars": 4096,
    "max_document_chars": 8192
  }
}
```

`dimensions` 不发送。

### 13.3 Selectable models

新增直接的按类型查询接口，避免 Web 当前为获取 selectable Embedding 而先列 Provider 再 N+1 列模型：

```text
GET /api/v1/workspaces/:workspace_slug/models?type=embedding&active=true
GET /api/v1/workspaces/:workspace_slug/models?type=rerank&active=true
```

member 可读；返回当前 Workspace 可见的平台和自有模型。Query key 必须包含 `workspaceSlug + type + active`。

### 13.4 Search response

单库 REST 继续返回数组，多库 REST/MCP 继续返回现有 wrapper；只为每个 result 增加 optional `rerank_score` 和必填 `ranking_stage`。这样不引入不必要的 response envelope 破坏。

MCP `knowledge_search` 输入不增加模型选择参数。工具描述更新为“可能根据 active Generation 配置执行重排”；输出 schema 允许新增字段。fallback 结果必须保留 `ranking_stage=rrf_fallback`，让 Agent 能判断本次结果使用了降级排序。

## 14. 结构化日志规格

### 14.1 Request ID

在 Gin 全局 middleware 中建立 request ID，覆盖 REST 和 `/mcp`：

- 接受合法 `X-Request-ID`：长度 `1..64`，字符集 `[A-Za-z0-9._:-]`。
- 缺失或非法时生成 UUID。
- 写回响应 `X-Request-ID`。
- 通过 request context 传入 application 和 adapter。
- 不把 request ID 当授权或幂等键。

HTTP/MCP adapter 在 context 中增加：

```text
request_id
transport = rest | mcp
principal_kind = user | api_key
```

### 14.2 Event 名称和级别

`SearchService`、`MultiKnowledgeSearchService` 和连接测试 service 通过构造函数接收 `*slog.Logger`，由 `cmd/langhuan` 注入；不在 application 内直接调用包级默认 logger。adapter 返回清洗后的 typed error 和调用结果，搜索级 terminal event 由 application 统一记录，避免同一失败在多层重复记 Error。

| event | level | 时机 |
|---|---|---|
| `model.connection_test.completed` | Info | 模型连接测试成功 |
| `model.connection_test.failed` | Warn | 可预期 Provider/协议失败 |
| `search.completed` | Info | 正常完成，包括无结果 |
| `search.rerank_fallback` | Warn | Rerank 远端失败并回退 RRF |
| `search.failed` | Warn/Error | 校验/冲突用 Warn，内部或不可恢复依赖错误用 Error |
| `rerank.call.completed` | Debug | 每次真实远端调用成功 |
| `rerank.call.failed` | Debug | 每次真实远端调用失败；搜索级 Warn/Error 另记一次 |

每次搜索必须只有一个 terminal `search.completed` 或 `search.failed`。fallback 额外记录一个 Warn，但不能再记录 terminal failed。

### 14.3 搜索日志字段

`search.completed`：

```text
event
request_id
transport
principal_kind
workspace_id
knowledge_base_count
knowledge_base_id                 # 仅单库
generation_count
embedding_group_count
rerank_enabled
rerank_applied
rerank_fallback
query_chars
vector_candidate_count
keyword_candidate_count
fused_candidate_count
grouped_candidate_count
rerank_candidate_count
result_count
ranking_stage
embedding_duration_ms
candidate_search_duration_ms
evidence_load_duration_ms
rerank_duration_ms
total_duration_ms
```

`rerank.call.completed/failed` 可增加：

```text
provider
model_id
provider_id
candidate_count
attempt_count
duration_ms
error_class                      # 失败时
```

不要在 Info 搜索日志中写入 `generation_ids`、多个 KB ID 数组或任意高基数 payload；单个 ID 只用于直接定位。需要更细诊断时由 Debug call event 提供模型 ID。

### 14.4 明确禁止记录

- query 原文、query hash 或可逆摘要。
- `search_content`、父块正文、FAQ 问题/答案、matched child 内容。
- embedding vector、Rerank documents、request/response body。
- API key、session cookie、Provider credential、Authorization 和 custom headers。
- Provider base URL 中可能包含的 query/credential。
- 完整第三方错误 body。

长度、数量、耗时、稳定 error class 和内部 UUID 可以记录。`error` 字段只能使用已经清洗的错误链；adapter 原始 body 不得进入错误链。

### 14.5 并发耗时语义

多库 embedding 和搜索可能并发：

- 阶段 `*_duration_ms` 记录该阶段的 wall-clock elapsed，不对并发子调用求和。
- Debug call event 记录每个真实调用自身耗时。
- `total_duration_ms` 从 application use case 开始到返回为止。
- 所有 duration 为非负整数毫秒；小于 1ms 记录 0，而不是虚构 1ms。

## 15. Web Console 体验规格

> **2026-08-06 设计更新：** 本节 15.1–15.3 中的模型管理信息架构与原型，已由 `docs/superpowers/specs/2026-08-06-multi-capability-provider-model-management-design.md` 替代。新的事实是“一条 Provider 连接可声明多个 capability，并包含多个不同类型的 Model”；本节 15.4 之后的 Generation 与检索体验继续有效。

### 15.1 信息架构与权限

- 不新增全局导航项；沿用“模型”连接详情页和 KnowledgeBase 的“索引配置 / 检索测试”。
- platform admin 在平台模型页管理共享 Rerank；Workspace admin/owner 管理自有 Rerank。
- member 可以读取可见模型和检索结果，但不能创建/编辑模型或 Generation。
- 页面只展示可读名称、Provider、类型、状态、引用数和真实测试结果；不渲染 UUID、config hash、credential 或原始 JSON。

### 15.2 Provider 连接详情：桌面 ASCII 原型

```text
┌─ AppShell ─────────────────────────────────────────────────────────────┐
│ 模型 / Rerank Compatible API / 连接详情                              │
│                                                                      │
│ Rerank Compatible API                         [已启用]  [编辑连接]    │
│ 用于兼容 /v1/rerank 协议的重排服务                                  │
│ Provider: rerank_compatible · Workspace 自有                         │
│ 凭证：已配置（api_key, custom_headers）                              │
│                                                                      │
│ 模型                                              [添加模型]          │
│ [全部 2] [Embedding 0] [Rerank 2]                                    │
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │ BGE Reranker v2                         [Rerank] [已启用]         │ │
│ │ BAAI/bge-reranker-v2-m3                                        │ │
│ │ 最大候选 100 · 单文档 8192 字符 · 配置快照引用 1               │ │
│ │                              [测试连接] [编辑] [停用] [删除]    │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│ 空状态：此连接还没有 Rerank 模型。                [添加第一个模型]   │
└──────────────────────────────────────────────────────────────────────┘
```

Provider 只支持 Rerank 时不显示空的 Embedding tab；上图 tab 仅表示支持多 capability 的未来兼容布局。前端根据后端 `capabilities` 渲染，不用 Provider key 推断。

### 15.3 添加 Rerank 模型：桌面 ASCII 原型

```text
┌─ 添加模型 ────────────────────────────────────────────────────────────┐
│ 模型类型                                                             │
│ (●) Rerank                                                           │
│                                                                      │
│ 内部标识 *                 显示名称 *                                 │
│ [bge_reranker________]     [BGE Reranker v2____________________]      │
│                                                                      │
│ 上游模型名 *                                                         │
│ [BAAI/bge-reranker-v2-m3______________________________________]      │
│                                                                      │
│ 最大候选文档 *             查询最大字符 *                            │
│ [100____]                  [4096___]                                 │
│                                                                      │
│ 单文档最大字符 *                                                      │
│ [8192___]                                                            │
│                                                                      │
│ 描述                                                                 │
│ [中文知识库重排_______________________________________________]      │
│                                                                      │
│                                      [取消] [保存并返回]              │
└──────────────────────────────────────────────────────────────────────┘
```

- 使用 RHF + discriminated Zod schema；Embedding 与 Rerank 表单字段不混在一个全字段 schema 中。
- 编辑失败、409 引用冲突或网络失败时保留草稿，并聚焦错误摘要或对应字段。
- 保存后精确失效 Provider models 和 `selectable models(type=rerank)` Query。

### 15.4 Generation 向导的 Rerank 配置

现有三步向导保持三步，Rerank 放在“检索配置”，不新增只含一个开关的第四步。

```text
┌─ 新建索引配置：步骤 3 / 3 检索配置 ──────────────────────────────────┐
│ 全文检索配置     [zhparser ▼]                                       │
│ Vector topK      [30]      Keyword topK [30]                         │
│ Final topK       [10]      RRF K        [60]                         │
│                                                                      │
│ ── Rerank 重排 ────────────────────────────────────────────────────  │
│ [✓] 启用重排                                                        │
│                                                                      │
│ Rerank 模型 *                                                        │
│ [BGE Reranker v2 · Workspace 自有 ▼]                                │
│                                                                      │
│ 候选数量 *                  失败策略 *                                │
│ [50____]                    [回退到 RRF ▼]                            │
│ 将融合后的候选交给模型，最终仍返回 Final topK。                      │
│ 回退会保留结果，并在检索结果和日志中标记。                           │
│                                                                      │
│ 此修改会创建并重建一个新的 Generation，激活后才生效。                │
│                                      [上一步] [创建索引配置]          │
└──────────────────────────────────────────────────────────────────────┘
```

状态规则：

- 开关关闭时隐藏模型、候选数和失败策略字段，并提交 `{"enabled":false}`。
- 没有可选 Rerank 模型时显示原因和有权限用户可用的“前往模型”链接；不显示空 Select。
- member 只读查看 active Generation 的 Rerank 摘要，不看到创建按钮。
- Generation 列表在配置摘要中显示“Rerank：关闭”或“BAAI/bge-reranker-v2-m3 · 候选 50 · 回退到 RRF”。名称来自 Generation 已保存的 `rerank_model_name`，不依赖当前可变的 Model `display_name`，也不额外暴露 UUID 或 config hash。

### 15.5 检索测试结果

```text
┌─ 检索测试 ────────────────────────────────────────────────────────────┐
│ [如何处理退款申请？____________________________________] [检索]      │
│ 高级参数：Vector 30 · Keyword 30 · Final 10                          │
│                                                                      │
│ 找到 10 条证据 · 当前索引配置 2026-08-05 16:20                       │
│ 排序：Rerank · 总耗时 487 ms                                        │
│                                                                      │
│ ① 退款处理规范                                      [FAQ]            │
│    Rerank 0.9142      RRF 0.0325                                     │
│    来源：问题 1–3                                                   │
│    ┌ 完整上下文 / 回答 ───────────────────────────────────────────┐  │
│    │ 用户提交退款申请后，由审核人员按规范核验并处理。             │  │
│    └──────────────────────────────────────────────────────────────┘  │
│    [查看来源] [打开 Chunk]                                           │
└──────────────────────────────────────────────────────────────────────┘
```

fallback：

```text
┌ ! 重排服务暂时不可用，已按 RRF 融合顺序返回结果。                    ┐
│   本次结果仍可使用；管理员可在模型页面测试连接。                    │
└──────────────────────────────────────────────────────────────────────┘
```

- `ranking_stage=rerank`：Rerank score 为主，RRF 为次。
- `ranking_stage=rrf`：只显示 RRF，不保留空的 Rerank 占位。
- `ranking_stage=rrf_fallback`：展示持久的页面内 warning，不能只用 Toast。
- score 使用原始小数和说明文字，不转换为百分比或“相关度 91%”。
- 排序标签只依据响应的 `ranking_stage` 显示 `Rerank` 或 `RRF`；耗时沿用 Web 现有的客户端总请求耗时，不冒充 Provider Rerank 阶段耗时。

### 15.6 移动端 ASCII 原型

```text
┌──────────────────────────┐
│ ☰  检索测试              │
├──────────────────────────┤
│ [如何处理退款申请？____] │
│ [        检索          ] │
│ 高级参数 ▾               │
├──────────────────────────┤
│ 排序：Rerank             │
│ 10 条 · 487 ms           │
│                          │
│ ① 退款处理规范   [FAQ]   │
│ Rerank 0.9142            │
│ RRF 0.0325               │
│ 来源：问题 1–3           │
│                          │
│ 回答正文摘要……           │
│                          │
│ [查看来源] [打开 Chunk]  │
├──────────────────────────┤
│ ② ……                     │
└──────────────────────────┘
```

- 桌面模型卡在移动端仍为单栏卡片，不缩成横向表格。
- 触控目标至少 `44×44px`；状态同时有文字，不能只靠颜色。
- Dialog/Sheet 关闭后焦点返回触发按钮；模型测试完成消息使用 `aria-live`。
- loading skeleton 与最终布局同构；失败、只读、无权限、空状态均有明确文案。

### 15.7 URL、Query 与缓存

- 现有 canonical routes 不变。
- 检索输入和 topK 继续由 typed search params 恢复；Rerank 配置来自服务端 active Generation，不进入 URL。
- 模型列表类型筛选如需要分享/刷新恢复，使用 `?type=rerank` typed search param。
- selectable query key：`['models','workspace',workspaceSlug,'selectable','rerank']`。
- Generation mutation 成功后精确失效 generation list/detail、KB summary 和 settings query；激活成功后同时失效 search page 的 active Generation query。
- 不用 Zustand 保存模型选择或检索执行状态。

## 16. 安全与资源保护

- Workspace `rerank_compatible` endpoint 使用现有 SSRF-safe client：DNS 解析、IP 分类、连接目标、redirect 全链路校验。
- 限制 query 字符、候选数量、单候选字符、序列化请求体和响应体大小。
- 不允许压缩炸弹式无限解压响应；响应 body 上限在解码前生效。
- 所有网络调用拥有 Provider timeout，并服从上游 request context 更短的 deadline。
- 自定义 headers 加密保存并使用 allowlist/denylist 校验。
- Rerank Model 和 Provider 的可见性在 application 中按 Workspace 再校验，不能只相信前端下拉框。
- API Key 没有新增模型管理 scope；它只能通过既有 `search:read` 使用 active Generation 已选好的 Rerank。
- 远端 Provider 不接收 Workspace ID、Document ID、Chunk ID、用户名、文件名或来源锚点；只发送 query、候选文本、模型名和必要协议字段。

## 17. 测试策略

### 17.1 Domain / application 单元测试

- Rerank Model 接受 nil dimensions，Embedding 仍要求合法 dimensions。
- ModelType 到正确 factory 的路由；`llm` 继续拒绝。
- 已引用模型的 model name/parameters 和 Provider config 不可修改，credential rotation 可修改。
- Generation Rerank snapshot 的全空/全非空不变量和 config hash。
- `rerank` 省略继承、显式关闭、显式启用的 request 语义。
- RRF -> parent grouping -> candidate topK -> rerank -> final topK 的顺序。
- FAQ 只用问题 search_content 构造 Rerank 文本。
- parent 使用 matched children 优先、确定性去重和截断。
- 非有限、重复、缺失、未知 DocumentID 的响应被拒绝。
- Rerank score 相同按 RRF、KB ID、Chunk ID 稳定排序。
- `fallback` 只处理允许的远端错误；snapshot mismatch/context cancel 不回退。
- 多库全部关闭、全部相同、启停混合、模型不同、参数不同五类 compatibility matrix。
- 配置一致的多库只调用一次 Rerank。

### 17.2 Adapter 测试

使用 `httptest.Server`，不发真实网络请求：

- request method/path/header/body 精确合同。
- 200 合法响应、乱序 index 恢复、extra field 忽略。
- 401/403/404 不重试；429/5xx/网络错误按上限重试。
- timeout 和 context cancellation。
- response 超限、invalid JSON、NaN/Inf、重复/越界/缺失 index。
- 错误字符串和 slog capture 中不存在 API key、query、document、response body。
- Workspace 私网 URL、DNS rebinding、redirect 到私网等 SSRF 负向用例。

### 17.3 Repository / migration 集成测试

仅使用临时 `langhuan-test-postgres:pg17`：

- 从空库执行全部迁移。
- 既有 Generation 迁移后 `rerank_model_id IS NULL` 且 `rerank_config={}`。
- snapshot 全空/全非空 CHECK。
- Model/Provider FK 和删除保护。
- Workspace 可见 Rerank 模型查询的正向与跨租户负向矩阵。
- 引用统计同时覆盖 embedding 和 rerank。
- up/down migration 在本项目 v1 前的全新安装策略下可执行。

### 17.4 HTTP / MCP 测试

- capability-aware Provider options。
- Rerank model create/update/test DTO，dimensions 缺失/null/错误组合。
- admin/owner/member/platform admin 权限矩阵。
- Generation 启用/关闭 Rerank 的 strict JSON 和错误映射。
- 单库结果新增 `rerank_score` / `ranking_stage`。
- 多库配置冲突 409，MCP 返回同语义 tool error。
- fallback 仍返回 200 和 evidence，结果标记 `rrf_fallback`。
- fail 模式返回脱敏 502/503。
- X-Request-ID 生成、接受、拒绝非法值和响应回传。

### 17.5 日志合同测试

用内存 `slog.Handler` 捕获 event：

- 成功、空结果、fallback、fail 各自只有一个 terminal event。
- 所有必填字段存在且 duration/count 非负。
- 多库并发阶段记录 wall-clock 语义。
- query、候选正文、API key、custom header、第三方 body 不出现在序列化日志。
- adapter 清洗后的 error class 稳定。

### 17.6 Web 测试

- Provider capability 决定可创建的模型类型。
- RHF/Zod 对 Embedding/Rerank 条件字段的正负用例。
- 可选 Rerank 模型按 Workspace/type/active 隔离缓存。
- Generation 开关关闭、无模型、只读、提交失败保留草稿。
- 检索结果 `rerank`、`rrf`、`rrf_fallback` 三种状态。
- 桌面与移动卡片信息等价，不出现 UUID/config hash。
- 键盘操作、焦点恢复、错误关联和触控 class。
- i18n 至少同步中文和英文资源，不以硬编码文案绕过现有机制。

### 17.7 E2E

使用临时 PostgreSQL/pgvector/zhparser、Redis 和本地 fake Rerank HTTP server：

```text
创建 Provider
  -> 创建 Rerank Model
  -> 测试连接
  -> 创建 KB / 导入内容
  -> 创建启用 Rerank 的 Generation
  -> 等待 ready 并激活
  -> REST 单库搜索验证顺序
  -> MCP knowledge_search 验证同序和字段
  -> fake Provider 失败验证 fallback
  -> 检查结构化日志无敏感内容
```

禁止连接真实第三方模型或本机长期运行数据库。

## 18. 实施分片与文件边界

以下是 spec 级实施顺序，不替代后续逐步 TDD implementation plan。

### Phase 1：Rerank port、registry 与兼容 adapter

预计新增：

```text
internal/ports/rerank/rerank.go
internal/ports/rerank/factory.go
internal/adapters/rerank/registry.go
internal/adapters/rerank/provider_error.go
internal/adapters/rerank/validated_client.go
internal/adapters/rerank/compatible/factory.go
internal/adapters/rerank/compatible/client.go
```

Provider URL/header 安全能力应复用现有聚焦实现；若当前 embedding 内部 helper 无法跨 capability 使用，只提取真正公共的 HTTP 安全部分，不创建模糊 `utils.go`。

### Phase 2：通用化模型管理

主要修改：

```text
internal/application/service/model.go
internal/application/service/model_connection.go
internal/application/service/model_provider.go
internal/application/service/provider_factory_resolver.go
internal/application/dto/model.go
internal/interfaces/http/model_handler.go
internal/interfaces/http/model_provider_handler.go
cmd/langhuan/main.go
```

目标是支持 capability-aware Provider options、Rerank CRUD/test 和引用不可变性；不重写既有 Embedding adapters。

### Phase 3：Generation 快照与数据库

主要新增/修改：

```text
internal/infrastructure/migrate/migrations/000014_rerank_generation_snapshot.*.sql
internal/domain/model/index_generation.go
internal/domain/value/rerank.go
internal/infrastructure/db/knowledge_rows.go
internal/infrastructure/db/knowledge_v2_codec.go
internal/infrastructure/db/model_repository.go
internal/application/service/index_generation.go
internal/application/dto/index_generation.go
internal/interfaces/http/index_generation_handler.go
```

### Phase 4：单库和多库搜索

建议保持文件聚焦，避免继续扩大现有 service：

```text
internal/application/service/rerank_client_resolver.go
internal/application/service/search_rerank.go
internal/application/service/search.go
internal/application/service/multi_knowledge_search.go
internal/application/dto/search.go
internal/ports/index/index.go
internal/infrastructure/db/retrieval_search_repository.go
```

`search_rerank.go` 只负责 private document 构造、兼容 key、调用/fallback 和稳定排序；Vector/FTS SQL 不移动。

### Phase 5：REST、MCP 和日志

主要修改：

```text
internal/interfaces/http/search_handler.go
internal/interfaces/http/middleware.go
internal/interfaces/http/errors.go
internal/interfaces/mcp/contracts.go
internal/interfaces/mcp/tools.go
internal/infrastructure/logger/logger.go
cmd/langhuan/main.go
config.example.yaml（只在确有新增全局运行参数时修改）
```

Rerank timeout/retry/输入限制属于 Provider/Model typed config，不在 YAML 重复定义。

### Phase 6：Web Console

主要修改/新增：

```text
web/src/features/models/types.ts
web/src/features/models/schemas/*
web/src/features/models/components/model-form.tsx
web/src/features/models/components/model-card.tsx
web/src/features/models/components/provider-form.tsx
web/src/features/models/components/model-provider-detail-content.tsx
web/src/features/models/api.ts
web/src/features/models/queries.ts
web/src/features/index-generations/generation-form-schema.ts
web/src/features/index-generations/generation-form.tsx
web/src/features/index-generations/schemas.ts
web/src/features/index-generations/types.ts
web/src/features/retrieval/schemas.ts
web/src/features/retrieval/retrieval-test.tsx
web/src/lib/i18n/locales/*
```

生产文件按职责拆分 schema 和字段组合，不把 Embedding/Rerank/Provider 所有分支继续堆进单个超大表单文件。

## 19. 发布与兼容策略

- 本项目尚未到 v1.0.0，允许同步升级 Provider options 和 Model dimensions DTO，不维护双版本响应。
- migration 让所有既有 Generation 默认关闭 Rerank，因此发布后既有搜索排序不变。
- Rerank registry 或 Provider 未配置时服务仍可启动，Embedding 和检索保持可用。
- Web 前端必须与后端同一二进制发布，避免 capability DTO 新旧不匹配。
- 建议拆成两个可独立审查和回滚的发布分片：先交付向前兼容的 schema 与模型 CRUD/测试，再交付 Generation 与搜索使用；不为此增加运行时 feature flag。
- 若上线后 Rerank Provider 不稳定，管理员可创建关闭 Rerank 的新 Generation；单次远端故障按默认 `fallback` 不阻断搜索。

## 20. 验收标准

功能：

- admin/owner 能创建 `rerank_compatible` 连接和 `type=rerank` 模型并成功测试。
- member 能看到可见的 active Rerank 模型，但不能修改。
- 新 KnowledgeBase 默认不启用 Rerank，既有搜索结果合同和顺序保持不变。
- admin/owner 能通过新 Generation 启用、切换或关闭 Rerank，只有激活后影响搜索。
- 单库 REST、Workspace 多库 REST 与 MCP 使用相同排序规则。
- 配置一致的多库只调用一次 Rerank；配置不一致在模型调用前返回稳定 409/tool error。
- FAQ Rerank 使用问题集合而不是答案，File/Web 返回完整父块 evidence。
- Rerank 成功返回 `rerank_score + ranking_stage=rerank`；fallback 返回原 RRF 顺序和 `ranking_stage=rrf_fallback`。

安全与可靠性：

- Workspace endpoint 不能访问受限内网地址或通过 redirect/DNS 绕过 SSRF 防护。
- Provider 请求数、文档数、query/document 字符和响应体都有上限。
- context cancel 和 timeout 能终止 retry 与外部调用。
- credential、query、文档正文、向量和第三方 body 不进入日志或错误响应。
- Generation 快照与 runtime 模型 hash 不一致时拒绝执行，不静默漂移。

前端：

- 模型、Generation、检索测试三处形成完整操作闭环。
- desktop/mobile 保持任务等价，触控、键盘、焦点和状态文字符合现有 Console 合同。
- loading、empty、read-only、forbidden、failed、fallback、completed 都有真实状态表现。
- URL 深链和刷新可恢复检索输入与模型类型筛选。

验证命令：

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

涉及外部 Rerank 的测试只使用 `httptest` 或进程内 fake server；涉及数据库的测试只使用测试期临时 Docker 容器。

## 21. 后续方向（不进入本规格）

- 为 DashScope、Cohere、Jina、Voyage 等增加原生 adapter，并用各自 typed response 代替兼容假设。
- 为仅改变 RRF/Rerank 的 Generation 增加安全的投影复用或复制快速路径。
- 建立离线检索评测数据集，对比 RRF 与 Rerank 的 nDCG/MRR/Recall，而不是依赖单次人工观感。
- 在 v0.8.0 可观测性工作中把同一字段合同映射到 metrics/traces。
- 出现真实异构多库需求后，再设计跨模型 rank fusion；在此之前保持配置一致性限制。

## 22. Spec 自审结论

- 范围：覆盖模型添加、连接测试、Generation 使用、单库/多库搜索、REST/MCP、日志、Web 和验证；没有扩展到 LLM、评测平台或搜索历史。
- 一致性：Rerank 的唯一事实源是 active Generation；UI、REST、MCP 和日志均引用该快照。
- 安全性：正文只在检索 application 与 adapter 调用期间存在，不进入日志、持久搜索历史或 API 额外字段。
- 兼容性：既有 Generation 迁移后默认关闭，现有搜索顺序不变；明确列出的 Provider options 与 nullable dimensions 是 pre-v1 同步合同升级。
- 无占位：本文没有依赖未定义的 fallback、分数归一化或异构多库行为；所有默认值和错误边界均已给出。
