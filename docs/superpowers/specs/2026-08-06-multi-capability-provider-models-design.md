# 多能力 Provider 与多模型管理 Spec

**状态：** 已实现，2026-08-06 自检通过

## 1. 背景与目标

一个 Provider 连接可能提供多个能力和多个模型。以 SiliconFlow 为例，同一个 API 地址和 API Key 可以同时承载 Embedding 与 Rerank；平台不应再把“Provider 连接”和“单个模型”混为一层。

本次设计建立两层资源：

- **Provider connection**：共享连接配置、凭证、能力声明和状态。
- **Model**：连接下的具体模型、模型类型、模型参数、启用状态和使用引用。

目标是让管理员能够在一个连接下添加多个 Embedding/Rerank 模型，准确知道能力与数量，安全地查看连接状态，并在模型目录中按能力、状态、作用域和连接筛选。

非目标：不在本次引入 LLM Chat/Agent、图查询、凭证明文展示或自由编辑未知 Provider 配置。

## 2. 产品合同

### 2.1 领域关系

```text
Workspace / Platform
        |
        +-- Provider connection (siliconflow-conn)
        |     shared: base_url, endpoints, timeout, retry, api_key
        |     capabilities: [embedding, rerank]
        |          |
        |          +-- Model: BAAI/bge-m3       type=embedding
        |          +-- Model: bge-reranker-v2   type=rerank
        |
        +-- Provider connection (openai-conn)
              capabilities: [embedding]
                   +-- Model: text-embedding-3-small
```

Provider descriptor 是运行时显式能力合同：同一 provider key 只注册一个 descriptor，descriptor 可以声明多个 capability，并提供共享配置解码器。Factory registry 负责按能力枚举和构造具体 transport；ModelService 在创建/更新模型时校验 descriptor 是否声明目标类型。

### 2.2 Provider 连接字段

服务端返回 `capabilities` 和 `model_counts`，但不返回 API key、凭证明文、raw config 或 hash。

```json
{
  "provider": "siliconflow",
  "capabilities": ["embedding", "rerank"],
  "model_counts": {
    "total": 4,
    "active": 3,
    "embedding": 2,
    "rerank": 2
  },
  "credential_configured": true,
  "status": "active"
}
```

SiliconFlow 默认共享配置：`https://api.siliconflow.cn`、`/v1/embeddings`、`/v1/rerank`、timeout 60 秒、retry 2 次；两个 Factory 使用同一 `provider=siliconflow` 和同一 Bearer API key。

### 2.3 模型目录筛选

管理目录 `GET /api/v1/admin/models` 支持 `type`、`status`、`scope`、`provider_id`、`q`；Workspace 目录支持同样的可见性边界和筛选。Generation selectable 接口继续只返回目标类型的 active 模型，保持 `type=embedding|rerank` 与 `active` 合同不变。

## 3. Web 信息架构与 ASCII 原型

### 3.1 模型服务总页

```text
+--------------------------------------------------------------------------------+
| 模型服务                                      [新建连接]                       |
| 管理平台中所有模型与连接。模型属于连接，连接可提供多个能力。                 |
|                                                                                |
| [全部模型]  [连接管理]                         [搜索模型或连接........] [筛选] |
| 类型: [全部 v]  状态: [全部 v]  作用域: [全部 v]  能力: [全部 v]              |
+--------------------------------------------------------------------------------+
| 模型名                  类型       Provider       连接状态   模型状态   操作   |
| BAAI/bge-m3             EMBEDDING  SiliconFlow    已连接     可用       ···    |
| BAAI/bge-reranker-v2    RERANK     SiliconFlow    已连接     可用       ···    |
| text-embedding-3-small  EMBEDDING  OpenAI         已停用     不可用     ···    |
+--------------------------------------------------------------------------------+
| 移动端：每行改为卡片，首行显示模型名/类型，第二行显示连接和状态，操作置底。   |
+--------------------------------------------------------------------------------+
```

交互规则：默认打开“全部模型”；切换 tab、筛选条件和搜索词写入 URL query，可复制/刷新后恢复；列表请求使用 TanStack Query，query key 包含 scope、workspace 和全部筛选值；变更成功后失效相关目录缓存。

### 3.2 连接管理视图

```text
+--------------------------------------------------------------------------------+
| 连接管理                                                       [新建连接]     |
+--------------------------------------------------------------------------------+
| 连接名称          Provider       能力                  模型数    凭证     状态 |
| 生产检索           SiliconFlow    EMBEDDING · RERANK    4 (3 active) 已配置  ● |
| 本地向量           Ollama         EMBEDDING             2 (2 active) 未配置  ● |
| 旧连接              OpenAI         EMBEDDING             1 (0 active) 已配置  ○ |
+--------------------------------------------------------------------------------+
| [查看连接] [编辑连接] [停用/启用] [删除]                                     |
+--------------------------------------------------------------------------------+
```

能力 badge 必须来自服务端 descriptor options，前端不按 provider key 猜测。连接停用时，连接下模型仍保留但显示“连接已停用”，模型停用显示“模型已停用”；两者不可混为一个状态。

### 3.3 连接详情

```text
+--------------------------------------------------------------------------------+
| ← 连接管理   生产检索                         [测试连接] [编辑连接]           |
| SiliconFlow   ● 已连接   EMBEDDING  RERANK                                   |
|                                                                                |
| [默认模型]  [连接设置]                                                        |
+--------------------------------------------------------------------------------+
| 默认模型                                                                      |
| Embedding  [BAAI/bge-m3                         v]             [编辑模型]     |
| Rerank     [BAAI/bge-reranker-v2                 v]             [编辑模型]     |
|                                                                                |
| 该连接下的模型 (4)                                      [添加模型]            |
| 类型        模型名                         状态                    操作       |
| EMBEDDING   BAAI/bge-m3                    可用                    ···        |
| RERANK      BAAI/bge-reranker-v2            可用                    ···        |
+--------------------------------------------------------------------------------+
```

“连接设置”只展示安全摘要：Base URL、两个 endpoint、timeout、retry、API key“已配置/未配置”；不展示 raw config、凭证明文或 hash。平台共享连接在 Workspace 详情页只读。

### 3.4 新建连接与模型编辑

```text
+-------------------------------- 新建 Provider 连接 ---------------------------+
| Provider *     [SiliconFlow v]                                                 |
| 连接名称 *     [生产检索________________]                                     |
| 能力           [✓ Embedding] [✓ Rerank]   (由服务端 options 限定)              |
| Base URL       [https://api.siliconflow.cn________________________]            |
| Embedding path [/v1/embeddings_______________________________]                 |
| Rerank path    [/v1/rerank____________________________________]                 |
| API Key *      [••••••••••••••••••••••••••••]                                  |
|                                      [取消] [保存连接]                         |
+--------------------------------------------------------------------------------+

+-------------------------------- 添加模型 -------------------------------------+
| 连接: SiliconFlow                 [Embedding] [Rerank]                         |
| 模型类型 *    (• Embedding) (  Rerank)     ← 创建 SiliconFlow 时选择          |
| 模型名称 *    [BAAI/bge-m3_______________________________]                    |
| 展示名称      [BGE M3____________________________________]                    |
| 维度          [1024________]   批量大小 [32________]  (Embedding only)          |
| 参数 JSON     [{...____________________________________]  (按类型 schema)      |
|                                      [取消] [保存模型]                         |
+--------------------------------------------------------------------------------+
```

编辑已有模型时，类型由 `model.type` 路由，禁止按 provider key 推断；Parser-only provider 不显示模型编辑器。保存前用 React Hook Form + Zod 校验，服务端再次按 descriptor 和 Factory codec 校验。

## 4. 交互状态与错误处理

| 场景 | UI 行为 |
|---|---|
| 首次加载 | 表格/卡片骨架屏；失败显示可重试错误，不清空已有缓存 |
| 无模型 | 显示“还没有模型”，提供“添加模型”入口 |
| 无权限 | 连接和模型可读时隐藏写操作；无读取权限显示 403 页面 |
| 连接未配置凭证 | 连接标记“未配置”，测试按钮引导编辑凭证 |
| 连接停用 | 模型仍可查看；模型测试/设为默认等运行操作禁用并说明原因 |
| 删除被引用模型 | 服务端返回 `model_in_use`，弹窗列出引用并拒绝删除 |
| Provider 不支持目标类型 | 服务端返回 `unsupported_provider`，表单保留输入并定位类型字段 |
| 测试连接 | 结果按 model ID 保存；成功显示维度或 rerank 结果数量，失败显示脱敏错误和 request/job 上下文 |

## 5. 日志与安全

日志采用结构化 key-value，至少包含 `workspace_id`（如适用）、`provider_id`、`model_id`、`job_id`、`request_id`、`model_type`、`provider`、`latency_ms`、`status`。允许记录 endpoint path 和结果数量，不得记录 API key、Authorization header、完整文档内容、raw credentials、hash 或完整请求体。

关键事件：`provider.created/updated/tested/enabled/disabled`、`model.created/updated/tested/enabled/disabled/deleted`、`retrieval.rerank.started/completed/failed`。错误分类保持稳定（timeout、unreachable、rejected、invalid_response），便于告警和审计。

## 6. 验收标准

- 一个 SiliconFlow 连接可创建至少一个 Embedding 与一个 Rerank 模型，两个 endpoint 使用同一凭证。
- Provider 列表显示能力 badge、总数、active 数、Embedding 数、Rerank 数和凭证配置状态。
- 模型目录支持 type/status/scope/provider/q 筛选，平台与 Workspace 可见性边界不泄漏。
- 模型编辑按 `model.type` 路由；连接停用与模型停用有不同文案和操作约束。
- 前端不展示 raw config/API key/hash；测试结果按 model ID 隔离。
- descriptor 只由已装配 Factory 构造，多能力 key 合并为一个 descriptor；最小 registry 不因缺少无关 Provider 失败。
- 集成测试只使用运行期间临时 Docker PostgreSQL/Redis。

## 7. Spec 自检记录

- 需求覆盖：Provider 多能力、连接/模型分层、SiliconFlow 双 endpoint、前端 ASCII、筛选、状态、日志、安全、验收均有章节。
- 反向检查：未引入 Chat/Agent、图查询、凭证明文或按 provider key 推断模型类型。
- 可实现性：API、领域 descriptor、Factory registry、Web URL state 和测试命令均与仓库现有合同一致。
