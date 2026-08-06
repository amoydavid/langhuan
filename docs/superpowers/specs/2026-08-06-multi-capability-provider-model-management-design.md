# 琅嬛多能力 Provider 与模型管理体验设计

> 状态：待用户评审
> 日期：2026-08-06
> 影响范围：平台模型、Workspace 模型、Provider capability、模型创建与连接详情
> 关联规格：`docs/superpowers/specs/2026-08-05-rerank-model-and-search-design.md`

## 1. 结论

Provider 不应等同于某一种模型类型。它表示一条可复用的外部服务连接：供应商、Endpoint、认证凭证、作用域和状态。一个 Provider 可以声明多个琅嬛已经实现的 capability，并在同一连接下配置多个不同类型的 Model。

以硅基流动为例：

```text
SiliconFlow Provider Connection
├─ shared base URL + API key
├─ capability: embedding
│  ├─ BGE-M3
│  └─ Qwen3-Embedding-8B
└─ capability: rerank
   ├─ BGE-Reranker-v2-M3
   └─ Qwen3-Reranker-8B
```

推荐同时调整产品模型和界面层级：

1. 后端使用显式 `ProviderDescriptor` 描述一条 Provider 支持的 capability 集合，不再用“第一个命中的 Factory”推断类型。
2. Model 创建时显式选择 `type`，并校验该类型属于 Provider capabilities；编辑时类型不可改变。
3. “模型配置”升级为“模型服务”，提供“全部模型”和“连接管理”两个路由可恢复视图。
4. Provider 详情默认先展示其下模型，连接配置和凭证作为第二工作区，不再让连接字段压过模型任务。
5. 对支持多个 capability 的 Provider，添加模型时先选择类型，再显示该类型的字段；单能力 Provider 直接显示只读类型。

这不是新的数据库关系。当前 `model_providers 1 -> N models` 已经可以表达一条连接下多个模型；需要修正的是 capability 事实源、API 聚合和 Web 交互。

## 2. 当前问题

### 2.1 已正确的部分

- `models.provider_id` 已表达一个 Provider 下多个 Model。
- Model 路由已经嵌套在 Provider 下：`.../model-providers/:provider_id/models`。
- Model 已有 `type=embedding|rerank`，类型专属参数保存在 Model，而不是 Provider。
- Provider 凭证集中加密，一次轮换可以服务其下多个 Model。
- 平台 Provider 可被 Workspace 只读复用，作用域模型无需改变。

### 2.2 需要修正的部分

当前实现仍有隐式“一 Provider 一类型”假设：

- `ProviderFactoryResolver.Resolve` 先查 Embedding，再查 Rerank；同一个 provider key 同时注册两种能力时只会返回第一个命中，capabilities 不会合并。
- Web `ModelForm` 用 `provider.provider === 'rerank_compatible'` 决定渲染 Rerank 或 Embedding 表单，而不是使用用户选择的 Model type。
- Provider 详情文案仍使用“Embedding 模型”“添加 Embedding 模型”，无法表达多类型列表。
- Provider 列表不展示 capability 和模型数量，用户看不出一条连接能提供什么。
- 模型测试结果显示在 Provider 级公共横幅中；当列表有多个类型和多个模型时，结果归属不清楚。
- 连接配置位于详情页首要位置，模型列表在其后；用户需要频繁管理多个模型时信息优先级倒置。

### 2.3 不应采用的补丁

不要为硅基流动分别创建“SiliconFlow Embedding Provider”和“SiliconFlow Rerank Provider”。这会重复保存同一 API key、Endpoint、状态与轮换动作，也会让一次凭证失效产生两份不一致配置。

不要让用户手动勾选任意 capability。Capability 是琅嬛是否实现了对应 adapter 的事实，不是用户偏好；界面只能展示后端 descriptor 已注册的能力。

## 3. 方案比较

### 3.1 方案 A：Provider 主导的层级页面

主页面只列连接，进入连接后管理多个模型。

优点：连接、凭证和权限边界非常清楚；改造量小。

缺点：模型被藏在连接详情里。平台模型较多时，无法跨连接查找、比较和测试模型。

### 3.2 方案 B：扁平模型目录

主页面只列所有模型，Provider 变成模型表单里的一个字段。

优点：模型查找和比较直接。

缺点：弱化凭证共享、连接停用和平台/Workspace 作用域；用户容易重复创建相同连接，连接级故障也难定位。

### 3.3 方案 C：双层资源、双视图（采用）

同一“模型服务”页面提供：

- “全部模型”：跨连接管理和筛选 Model。
- “连接管理”：管理 Provider、Endpoint、凭证和连接状态。
- Provider 详情：保留一条连接的局部上下文，默认展示其全部子模型。

优点：同时保留正确资源边界和高频模型任务；能自然表达一条 SiliconFlow 连接下多个 Embedding/Rerank 模型；与现有路由和数据关系兼容。

代价：需要增加全局模型列表聚合、Provider capability/count 字段和少量路由 search params。

## 4. 产品对象与事实源

### 4.1 UI 名称

| 内部对象 | 普通 UI 名称 | 含义 |
|---|---|---|
| `ModelProvider` | 模型连接 | 供应商服务账号、Endpoint、凭证、作用域和状态 |
| `Model` | 模型 | 一条连接下可被业务选择的具体模型实例 |
| `ProviderCapability` | 支持能力 | 琅嬛已实现的 `embedding`、`rerank` 或 `parser` |

页面正文不使用“Provider 实例”“Factory”“Registry”等实现术语。供应商名称仍可显示为 SiliconFlow、OpenAI、火山方舟等。

### 4.2 不变量

- 一个模型只属于一条模型连接。
- 一条模型连接可包含零到多个模型。
- 一条连接可同时支持多个 capability。
- Model `type` 必须属于 Provider capabilities。
- Model 创建后 `type` 不可修改；切换类型必须新建 Model。
- Provider status 停用时，其下所有 Model 均不可用，但各 Model 自己的 status 保持不变。
- Provider credential 由其下所有 capability 和 Model 共享。
- Parser-only Provider 可以存在，但不显示“添加模型”操作。
- 供应商实际支持但琅嬛尚未实现的能力不显示。例如本规格不因 SiliconFlow 提供 LLM 就开放 `llm`。

### 4.3 Runtime descriptor

新增一个显式 Provider 管理事实源：

```go
type ProviderDescriptor struct {
    Key              string
    Capabilities     []value.ProviderCapability
    CredentialFields []string
    DecodeProvider   func(scope value.ModelScope, config, credentials json.RawMessage) (...)
}
```

每个 capability 仍有自己的运行时 Factory：

```text
ProviderDescriptor("siliconflow")
├─ Embedding Factory("siliconflow")
└─ Rerank Factory("siliconflow")
```

装配启动时必须校验：

- descriptor key 唯一；capability 去重并稳定排序。
- descriptor 声明的每个模型 capability 都存在对应 Factory。
- Factory 不能注册 descriptor 未声明的 capability。
- Provider config 和 credentials 只由 descriptor 规范化一次；不同 capability 不得各自解释出冲突的共享连接配置。

`ProviderFactoryResolver` 不再按 registry 顺序返回第一个命中，而是先读取 descriptor，再按 Model type 精确取得 capability Factory。

### 4.4 SiliconFlow 首个多能力实例

```text
key: siliconflow
capabilities: [embedding, rerank]
config:
  base_url: https://api.siliconflow.cn
  embedding_endpoint_path: /v1/embeddings
  rerank_endpoint_path: /v1/rerank
  timeout_seconds: 60
  retry_times: 2
credentials:
  api_key: encrypted
```

SiliconFlow preset 固定提供上述默认 Endpoint path，并把路径字段折叠在“高级设置”中。用户不能通过删除 `rerank_endpoint_path` 来改变 capability；缺少必需路径时整条连接配置校验失败。

现有 `openai` 继续只声明 Embedding，`rerank_compatible` 继续只声明 Rerank。其它厂商只有在真实 adapter 落地时才逐个增加 capability，不在前端猜测。

## 5. API 与数据合同

### 5.1 Provider options

```json
{
  "providers": [
    {
      "key": "siliconflow",
      "capabilities": ["embedding", "rerank"]
    },
    {
      "key": "rerank_compatible",
      "capabilities": ["rerank"]
    }
  ]
}
```

前端继续维护本地化供应商显示名和聚焦表单 schema；首版不引入动态 JSON Schema 表单系统。

### 5.2 Provider response

Provider 列表和详情增加：

```json
{
  "capabilities": ["embedding", "rerank"],
  "model_counts": {
    "total": 5,
    "active": 4,
    "embedding": 3,
    "rerank": 2
  }
}
```

数量由服务端聚合返回，前端不得对 Provider 列表执行 N+1 模型查询。`active` 只统计 Model 自身 active；Provider disabled 时 UI 另由 `available=false` 表达实际不可用，不能静默把 active count 改成 0。

### 5.3 Model list

全局模型视图使用：

```text
GET /api/v1/admin/models
    ?type=all|embedding|rerank
    &status=all|active|disabled
    &provider_id=<optional>
    &q=<optional-readable-search>

GET /api/v1/workspaces/:workspace_slug/models
    ?type=all|embedding|rerank
    &status=all|active|disabled
    &scope=all|workspace|platform
    &q=<optional-readable-search>
```

返回 Model 继续包含 Provider summary、`available` 和 `reference_count`。`q` 只匹配模型显示名、内部标识、上游模型名和 Provider 显示名；请求日志只记录查询字符数，不记录原文。

现有 KnowledgeBase/Generation selectable API 仍使用精确 `type + active=true`，不要复用管理页面的宽松筛选结果。

### 5.4 创建与更新校验

- 创建 Model 时后端从 Provider descriptor 校验 `type` 是否受支持。
- `type` 不加入 Model PATCH DTO。
- 单能力 Provider 也必须由后端校验，不能只依赖前端隐藏类型选择。
- Provider capability 不持久化到 Provider row；它由当前部署注册的 descriptor 决定。
- 当历史 Provider 的 descriptor 不再注册时，返回稳定“该连接类型当前不可用”状态；不得把它误显示成已停用。

## 6. 路由与可恢复状态

平台：

```text
/admin/models?view=models&type=all&status=all&q=
/admin/models?view=connections&capability=all&status=all&q=
/admin/models/:providerId?tab=models&type=all&status=all&q=
```

Workspace：

```text
/workspaces/:workspaceSlug/models?view=models&type=all&scope=all&status=all&q=
/workspaces/:workspaceSlug/models?view=connections&capability=all&scope=all&status=all&q=
/workspaces/:workspaceSlug/models/:providerId?tab=models&type=all&status=all&q=
```

规则：

- `view`、`tab`、type/capability、scope、status 和搜索词使用经过 Zod 校验的 TanStack Router search params。
- 默认 `view=models`，因为查看、测试和启停具体模型是高频任务。
- Provider detail 默认 `tab=models`；`tab=connection` 才显示完整连接配置和凭证管理。
- Dialog/Sheet 开关和未提交表单草稿保留为本地状态，不写进 URL。
- 刷新、复制链接、前进和后退恢复相同筛选工作面。

## 7. 桌面 ASCII 原型

### 7.1 全部模型（默认视图）

```text
┌─ AppShell / 平台管理 ─────────────────────────────────────────────────────────────────────┐
│ 模型服务                                                          [+ 添加模型]          │
│ 管理平台共享的模型，以及它们使用的外部服务连接。                                          │
│                                                                                          │
│ [全部模型 12] [连接管理 4]                                                               │
│                                                                                          │
│ [搜索模型或连接…________________] [类型：全部 ▼] [状态：全部 ▼]                          │
│                                                                                          │
│ 模型                     类型        模型连接             上游模型 / 配置      状态   操作 │
│ ──────────────────────────────────────────────────────────────────────────────────────── │
│ BGE M3                   Embedding   SiliconFlow 平台     BAAI/bge-m3           可用   ⋯   │
│                                       1024 维 · 快照引用 3                                │
│                                                                                          │
│ BGE Reranker v2          Rerank      SiliconFlow 平台     BAAI/bge-reranker…    可用   ⋯   │
│                                       最大候选 100 · 快照引用 1                           │
│                                                                                          │
│ Text Embedding 3 Large   Embedding   OpenAI 生产          text-embedding-3…     停用   ⋯   │
│                                       3072 维 · 快照引用 0                                │
│                                                                                          │
│ 12 个模型                                                         < 1 / 1 >              │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

交互：

- 行主链接打开所属 Provider detail，并聚焦该模型；操作菜单包含“测试模型、编辑、停用/启用、删除”。
- 模型 active 但 Provider disabled 时，状态显示“连接已停用”，不是“可用”或“模型已停用”。
- 测试结果在当前行下方展开并使用 `aria-live`；不同模型的结果不会覆盖到页面公共横幅。
- `reference_count` 文案统一为“配置快照引用”，不称为知识库数量。
- Provider 名称是可读链接；普通表格不显示 Provider/Model UUID。

### 7.2 连接管理

```text
┌─ 模型服务 ────────────────────────────────────────────────────────────────────────────────┐
│ [全部模型 12] [连接管理 4]                                           [+ 配置新连接]       │
│                                                                                          │
│ [搜索连接…____________________] [能力：全部 ▼] [状态：全部 ▼]                            │
│                                                                                          │
│ 模型连接             供应商           支持能力                 模型       凭证       状态 │
│ ──────────────────────────────────────────────────────────────────────────────────────── │
│ SiliconFlow 平台      硅基流动         [Embedding] [Rerank]      5 / 4 可用  已配置     运行中│
│ OpenAI 生产           OpenAI Compatible [Embedding]              3 / 3 可用  已配置     运行中│
│ Rerank 兼容测试       Rerank Compatible [Rerank]                 1 / 0 可用  已配置     已停用│
│ MinerU PDF            MinerU Cloud      [Parser]                  不适用      已配置     运行中│
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

交互：

- 整行进入连接详情，最后一列保留带可访问名称的操作菜单。
- capability badge 来自 Provider response，不从 provider key 在前端硬编码推导。
- Parser-only Provider 的模型数量显示“不适用”，不会出现“添加模型”。
- Workspace 页面分成“当前 Workspace”和“平台共享（只读）”两组；筛选条件对两组同时生效。

### 7.3 配置新连接：SiliconFlow

```text
┌─ 配置模型连接 ───────────────────────────────────────────────────────┐
│ 供应商 *                                                            │
│ [硅基流动 SiliconFlow ▼]                                            │
│ 支持能力  [Embedding] [Rerank]                                      │
│ 能力由当前琅嬛部署决定，保存后可在此连接下添加多个模型。             │
│                                                                      │
│ 连接标识 *                     显示名称 *                            │
│ [siliconflow_prod________]     [SiliconFlow 平台________________]    │
│                                                                      │
│ API 地址 *                                                          │
│ [https://api.siliconflow.cn____________________________________]    │
│                                                                      │
│ API Key *                                                           │
│ [••••••••••••••••••••••••____________________________________]    │
│ 凭证只在本次提交中发送，保存后不会回填。                             │
│                                                                      │
│ 高级设置 ▸  Embedding /v1/embeddings · Rerank /v1/rerank            │
│ 描述                                                                 │
│ [平台统一的 SiliconFlow 模型连接_______________________________]    │
│                                                                      │
│                    [取消] [仅保存连接] [保存并添加第一个模型]        │
└──────────────────────────────────────────────────────────────────────┘
```

规则：

- capability 只读展示，不能勾选。
- 切换供应商时重置供应商专属字段；已输入的 API key 立即从表单内存清除。
- 主操作是“保存并添加第一个模型”；成功后进入 Provider detail 并打开添加模型工作面。
- “仅保存连接”用于先建立连接、稍后配置模型；成功后进入空的 Provider detail。
- 网络或校验失败保留非敏感草稿；凭证是否保留遵循当前安全表单合同，离开 Dialog/Sheet 时清除。

### 7.4 Provider 详情：模型优先

```text
┌─ ← 返回模型服务 ──────────────────────────────────────────────────────────────────────────┐
│ SiliconFlow 平台                            [运行中] [Embedding] [Rerank]                  │
│ 硅基流动 · 平台共享连接                                              [⋯ 连接操作]          │
│                                                                                          │
│ [模型 5] [连接设置]                                                                      │
│                                                                                          │
│ 此连接下的模型                                      [类型：全部 ▼] [+ 添加模型]           │
│ [搜索当前连接的模型…___________________________]                                         │
│                                                                                          │
│ ┌ BGE M3 ──────────────────────────────────────────────────────────────────────────────┐ │
│ │ [Embedding] [可用]  BAAI/bge-m3                                                      │ │
│ │ 1024 维 · 批量 32 · 配置快照引用 3                                                   │ │
│ │                                                [测试] [编辑] [停用] [⋯]               │ │
│ └──────────────────────────────────────────────────────────────────────────────────────┘ │
│ ┌ BGE Reranker v2 ─────────────────────────────────────────────────────────────────────┐ │
│ │ [Rerank] [可用]  BAAI/bge-reranker-v2-m3                                            │ │
│ │ 最大候选 100 · 查询 4096 字符 · 单文档 8192 字符 · 配置快照引用 1                   │ │
│ │                                                [测试] [编辑] [停用] [⋯]               │ │
│ └──────────────────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

连接操作菜单：

- 编辑连接信息
- 轮换凭证
- 停用/启用连接
- 删除连接

“轮换凭证”必须独立于一般编辑。它影响这条连接下所有模型，确认文案明确说明影响范围，但不展示 API key 或模型内部 ID。

### 7.5 添加模型：多能力选择

```text
┌─ 向 SiliconFlow 平台添加模型 ────────────────────────────────────────┐
│ 模型类型 *                                                          │
│ ┌─────────────────────────┐  ┌─────────────────────────┐            │
│ │ ● Embedding             │  │ ○ Rerank                │            │
│ │ 将文本转换为检索向量    │  │ 对召回候选重新排序      │            │
│ └─────────────────────────┘  └─────────────────────────┘            │
│                                                                      │
│ 模型标识 *                     显示名称 *                            │
│ [bge_m3__________________]     [BGE M3__________________________]    │
│                                                                      │
│ 供应商模型名称 *                                                    │
│ [BAAI/bge-m3___________________________________________________]    │
│                                                                      │
│ 向量维度 *                     批量大小 *                            │
│ [1024 ▼]                       [32____]                              │
│                                                                      │
│ 描述                                                                 │
│ [中文和多语言检索向量模型_____________________________________]    │
│                                                                      │
│                                         [取消] [保存模型]            │
└──────────────────────────────────────────────────────────────────────┘
```

切换到 Rerank 后，同一位置替换为：

```text
┌─ 类型专属配置：Rerank ──────────────────────────────────────────────┐
│ 最大候选文档 *       查询最大字符 *       单文档最大字符 *           │
│ [100____]            [4096____]            [8192____]               │
└──────────────────────────────────────────────────────────────────────┘
```

规则：

- 类型选项只显示 Provider capabilities 与琅嬛 Model types 的交集。
- 从全局“添加模型”进入时，第一字段是“模型连接”；选择连接后再显示其类型选项。
- 从 Provider detail 进入时连接已固定；如果只有一种模型 capability，类型显示为只读 badge，不显示伪选择器。
- 切换类型会清除另一类型的专属字段并恢复该类型默认值；已输入的公共名称字段保留。
- 编辑 Model 时类型只读；表单由 `model.type` 决定，不再由 provider key 决定。
- 保存成功关闭工作面、刷新模型/连接计数与 selectable queries，并把焦点移回新模型的行或卡片。

### 7.6 连接设置

```text
┌─ SiliconFlow 平台 / 连接设置 ─────────────────────────────────────────────────────────────┐
│ 公开连接配置                                                        [编辑连接信息]        │
│ 供应商              硅基流动                                                              │
│ API 地址            https://api.siliconflow.cn                                           │
│ Embedding 路径      /v1/embeddings                                                       │
│ Rerank 路径         /v1/rerank                                                           │
│ 超时 / 重试         60 秒 / 2 次                                                         │
│                                                                                          │
│ 凭证                                                                   [轮换凭证]        │
│ 状态                已加密保存                                                           │
│ 字段                API Key                                                              │
│ 说明                浏览器不会读取或回填现有凭证。                                       │
│                                                                                          │
│ 危险操作                                                                                 │
│ [停用连接]  将使 4 个当前启用模型不可用，但不会改变各模型自身状态。                      │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

配置字段使用供应商专属可读标签，不使用 `Object.entries(config)` 直接渲染原始 key/value。Provider config 拒绝带 query、fragment 或 userinfo 的 Endpoint，API 只返回已规范化的 origin/path；任何 URL 都不得保存 credential。

## 8. 移动端 ASCII 原型

### 8.1 全部模型

```text
┌──────────────────────────────┐
│ ☰  模型服务            [+]  │
│ [全部模型] [连接管理]        │
├──────────────────────────────┤
│ [搜索模型或连接…________]    │
│ [类型：全部 ▼] [状态：全部▼]│
├──────────────────────────────┤
│ BGE M3              [可用]   │
│ Embedding · 1024 维           │
│ SiliconFlow 平台              │
│ BAAI/bge-m3                   │
│ 配置快照引用 3          [⋯]  │
├──────────────────────────────┤
│ BGE Reranker v2     [可用]   │
│ Rerank · 最大候选 100         │
│ SiliconFlow 平台              │
│ BAAI/bge-reranker-v2-m3       │
│ 配置快照引用 1          [⋯]  │
└──────────────────────────────┘
```

移动端不用横向滚动表格。卡片保留名称、类型、连接、类型专属摘要、真实状态、引用数和操作入口。

### 8.2 Provider 详情

```text
┌──────────────────────────────┐
│ ← 模型服务                  │
│ SiliconFlow 平台      [⋯]   │
│ [运行中] [Embedding][Rerank]│
│ 硅基流动 · 平台共享          │
├──────────────────────────────┤
│ [模型 5] [连接设置]          │
│ [搜索…_______________] [+]  │
├──────────────────────────────┤
│ BGE M3              [可用]   │
│ Embedding · 1024 维           │
│ BAAI/bge-m3                   │
│ [测试] [编辑]           [⋯]  │
├──────────────────────────────┤
│ BGE Reranker v2     [可用]   │
│ Rerank · 候选 100             │
│ BAAI/bge-reranker-v2-m3       │
│ [测试] [编辑]           [⋯]  │
└──────────────────────────────┘
```

添加/编辑模型在移动端使用全高 Sheet，同一时间只呈现一个纵向表单；底部操作区 sticky，但不能遮挡字段错误。关闭 Sheet 后焦点返回触发按钮或新建模型卡片。

### 8.3 Workspace 平台共享只读状态

```text
┌──────────────────────────────┐
│ SiliconFlow 平台             │
│ [平台共享 · 只读] [运行中]  │
│ [Embedding] [Rerank]         │
├──────────────────────────────┤
│ 这些模型由平台管理员维护。   │
│ 当前 Workspace 可在知识库中  │
│ 选择可用模型，但不能修改连接 │
│ 或模型配置。                 │
├──────────────────────────────┤
│ BGE M3              [可用]   │
│ BGE Reranker v2     [可用]   │
└──────────────────────────────┘
```

只读页面隐藏写操作，不渲染一排 disabled 按钮。说明谁维护该连接，以及当前 Workspace 仍可执行的真实任务。

## 9. 关键交互流程

### 9.1 新连接并添加多个模型

```text
连接管理
  -> 配置新连接
  -> 选择 SiliconFlow
  -> 展示只读 capabilities
  -> 保存并添加第一个模型
  -> 进入连接详情 / 添加模型
  -> 选择 Embedding，保存 BGE M3
  -> 焦点落到 BGE M3
  -> 再次添加模型
  -> 选择 Rerank，保存 BGE Reranker
  -> 两个模型共用同一连接与凭证
```

### 9.2 测试模型

```text
点击某一模型“测试”
  -> 只禁用该模型的冲突操作
  -> 行内显示“正在测试 BGE M3…”
  -> 成功：行内显示类型正确的结果与 duration
  -> 失败：行内持久 error，保留“重新测试”
```

Embedding 成功显示维度；Rerank 成功显示返回结果数。不得用 `dimensions=null` 拼出“null 维”。测试状态不冒充长期健康状态；当前不持久化测试历史，因此页面刷新后清除该结果。

### 9.3 停用连接

停用前确认：

```text
停用“SiliconFlow 平台”？

此连接下有 5 个模型，其中 4 个当前启用。停用连接后，这些模型将不能用于
新建 Generation 或运行检索；模型自身的启用/停用状态不会改变。

[取消] [停用连接]
```

成功后：

- Provider 显示“已停用”。
- active child models 显示“连接已停用”。
- selectable model Query 立即失效。
- 已引用的 active Generation 按既有 fail-closed/fallback 合同处理，不在前端伪造可用状态。

### 9.4 轮换凭证

轮换凭证是独立表单，只接受新凭证，不显示当前值。提交成功后：

- 清空输入内存。
- 保留 Provider config 与所有 Model 不变。
- 不改变 Generation config hash。
- 提示“新凭证已用于此连接下的全部模型”；不自动依次测试所有模型。

## 10. 页面状态

### 10.1 Loading

- 首次加载使用与模型表格/连接卡片同构的 Skeleton。
- 切换筛选时保留旧数据，显示局部 pending，不清空整个页面。
- 单模型测试只锁定该模型行；不要用一个全页 `busy` 禁止其它模型操作。

### 10.2 Empty

| 场景 | 文案与操作 |
|---|---|
| 没有任何连接 | “还没有模型连接。先配置供应商 Endpoint 和凭证。” + 配置新连接 |
| 连接下没有模型 | “连接已保存，但还没有可供业务选择的模型。” + 添加第一个模型 |
| 全局没有模型但已有连接 | “已有连接，尚未添加模型。” + 添加模型 |
| 筛选无结果 | “没有符合当前筛选的模型。” + 清除筛选 |
| 只读用户无结果 | 解释由管理员维护，不显示无权限创建操作 |

### 10.3 Error

- 路由加载失败显示受影响资源和“重新加载”，不使用只有 Toast 的错误。
- 表单校验、409 引用冲突和网络失败保留非敏感草稿。
- Provider descriptor 缺失显示“该连接类型当前未在服务端启用”，不暴露 registry/Factory 错误。
- Model type 不受支持返回稳定错误，并将焦点移到类型选择或错误摘要。
- 错误中不得包含 credential、Authorization、custom headers 或第三方 body。

## 11. 权限

| 角色/作用域 | 查看连接与模型 | 添加/编辑模型 | 编辑/轮换连接 | 启停/删除 |
|---|---:|---:|---:|---:|
| platform admin / platform | 是 | 是 | 是 | 是 |
| Workspace owner/admin / 自有连接 | 是 | 是 | 是 | 是 |
| Workspace owner/admin / 平台共享 | 是 | 否 | 否 | 否 |
| Workspace member | 是 | 否 | 否 | 否 |

前端只改善可理解性，后端继续执行全部权限与 Workspace 隔离。跨 Workspace Provider/Model 保持 404 隐藏语义。

## 12. 响应式与无障碍

- 桌面模型和连接是二维数据，使用表格；移动端改为单栏卡片，信息和主操作等价。
- Dialog、Sheet、Select、DropdownMenu、AlertDialog 使用现有 shadcn/Radix primitive。
- 类型卡片使用真实 radio group 语义，支持方向键和 Space/Enter。
- 纯图标操作有明确 accessible name；Tooltip 只补充说明。
- 状态始终包含文字，不只依靠绿色/灰色。
- 触控目标至少 `44×44px`，相邻菜单目标不重叠。
- 打开表单时焦点进入标题或首个字段；失败时聚焦第一个字段错误或错误摘要；关闭后返回触发点。
- 测试模型结果使用 polite `aria-live`，失败使用 `role=alert`。
- 页面无横向溢出；长上游模型名允许换行或局部截断并提供可访问完整文本。
- 浅色、深色和跟随系统主题均复用现有语义 token，不新增裸供应商品牌色。

## 13. Query 与 mutation 失效

推荐 Query key：

```text
['model-providers', scope, workspaceSlug, providerFilters]
['model-provider', scope, workspaceSlug, providerId]
['models', scope, workspaceSlug, globalModelFilters]
['models', scope, workspaceSlug, providerId, providerModelFilters]
['models', scope, workspaceSlug, 'selectable', modelType, true]
['model-provider-options', scope, workspaceSlug]
```

创建/更新/启停/删除 Model 后至少失效：

- 所属 Provider 的模型列表。
- 当前全局模型列表。
- Provider list/detail 的 `model_counts`。
- 对应 Model type 的 selectable queries。
- 使用该模型展示可用性的 Generation/KnowledgeBase query。

Provider credential/config/status mutation 后，失效该 Provider 下所有 capability 的 selectable queries。不要清空整个 Query Cache。

## 14. 验收标准

产品合同：

- 一条 SiliconFlow Provider 可同时显示 Embedding 与 Rerank capability。
- 用户能在同一连接下分别创建至少两个 Embedding 和两个 Rerank Model。
- 创建 Model 时类型来自 Provider capabilities；编辑时从 `model.type` 渲染正确表单。
- Provider 停用后子模型显示“连接已停用”，恢复后模型原 status 不变。
- Parser-only Provider 不出现添加模型操作。
- 平台共享连接在 Workspace 中保持可读、可选择但不可编辑。

界面：

- “全部模型”和“连接管理”可通过 URL 恢复筛选和视图。
- Provider 详情默认展示模型，不再先展示大块连接配置。
- 多模型测试结果各自归属于对应行或卡片。
- 桌面表格与移动卡片保留同等业务信息。
- 普通 DOM 不出现 UUID、config hash、credential 或原始 config JSON dump。

验证：

- Provider descriptor 合并/校验使用 table-driven tests。
- API 测试覆盖同一 Provider 多 capability、非法 Model type 和聚合 counts。
- Web 测试覆盖双视图 search params、类型切换、单能力只读类型、多能力表单、共享只读、连接停用和单行测试状态。
- E2E 使用一条 fake SiliconFlow 连接同时响应 `/v1/embeddings` 与 `/v1/rerank`，验证两个类型共享凭证并分别可用。
- 数据库 E2E 仍只使用测试期临时 Docker PostgreSQL；fake Provider 不访问真实外部服务。

## 15. 与现有 Rerank 规格和计划的关系

本规格覆盖并替代原 Rerank 规格中以下管理台假设：

- Provider options 只能由第一个 registry 命中得出 capability。
- `rerank_compatible` Provider 与 Rerank Model 类型一一绑定的前端分支。
- Provider 详情只有一种模型类型的标题、空状态和添加表单。
- Provider 级公共连接测试结果横幅。

以下 Rerank 合同不变：

- active Generation 保存不可变 Rerank snapshot。
- Rerank 的检索顺序、fallback、多库一致性、score 和日志安全规则。
- Model type 专属参数与连接测试 wire contract。
- Provider/Model 引用后的不可变字段规则。

已完成的 Rerank 后端和 Web 工作不回滚。本规格批准后，应为以下内容编写增量实现计划：

1. Provider descriptor 与多 capability 精确 Factory 路由。
2. 注册 key 为 `siliconflow` 的薄 Embedding/Rerank Factory，并分别复用现有 compatible transport；Provider schema 和凭证解码仍只由 SiliconFlow descriptor 负责。
3. Provider capability/model count API。
4. 全局模型列表与 URL 筛选。
5. Provider detail、添加模型和移动端交互重构。

## 16. Spec 自检

- 资源一致性：Provider 是连接，Model 是其下实例；界面、API 和 runtime 使用相同关系。
- Capability 一致性：能力来自服务端 descriptor，不由用户勾选或前端根据 provider key 猜测。
- 权限一致性：平台、自有 Workspace 和平台共享只读边界未改变。
- 状态一致性：Provider status、Model status 与实际 `available` 分开表达。
- 安全一致性：凭证仍只在独立表单提交；普通 UI 不展示 UUID/hash/raw config/credential。
- 路由一致性：可共享筛选进入 typed search params，表单草稿保持本地。
- 响应式一致性：桌面二维表格与移动单栏卡片任务等价。
- 范围一致性：不引入动态表单引擎、不开放 LLM、不自动发现供应商模型、不改变 Rerank 查询算法。
- 实施边界：这是增量设计，不把已经完成的 Rerank 工作描述为尚未实现。
