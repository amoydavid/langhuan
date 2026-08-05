# 父子分块与自适应分块设计

## 目标

为 File/Web 文档提供两层分块：小的 child chunk 负责 Embedding 与 FTS 召回，较大的 parent chunk 提供最终返回的完整上下文。分块策略扩展为 `auto`、`heading`、`heuristic`、`recursive`，同时保留琅嬛已有的 parse manifest、表格完整行和来源锚点合同。

FAQ 保持现有的单一 `strategy=faq` 语义，不使用父子分块。

## 配置与策略

`ChunkingConfig` 扩展为：

- `strategy`: `auto | heading | heuristic | recursive`；默认 `auto`。
- `enable_parent_child`: 默认 `true`。
- `parent_chunk_size`: 默认 4096 rune。
- `child_chunk_size`: 默认 384 rune。
- 既有 `chunk_size` 在关闭父子分块时继续作为扁平块大小；开启时由 `child_chunk_size` 取代。
- 既有 `chunk_overlap` 继续作为 parent overlap；child overlap 固定为 child size 的约 20%。

配置在 KnowledgeBase 创建、REST/MCP 请求和 Generation 创建中传递，并完整写入 Generation 的不可变 `chunking_config` 快照及 config hash。初始与新建 Generation 都使用 chunker v3。

`auto` 依序选择并验证输出：

1. `heading`：依据 manifest 标题路径及 Markdown 标题做标题感知分块，Embedding 内容包含 breadcrumb。
2. `heuristic`：依据分页符、编号章节、中英文章节标记、全大写标题、分隔线和空白段落等边界分块。
3. `recursive`：按段落、换行、句末标点的优先级递归切分，作为最终兜底。

标题、代码和表格仍优先使用 manifest。表格仅在完整行之间切分，每个 table chunk 重复表头；代码块不能被启发式边界切断。策略结果须经过大小与碎片率校验，失败时降级至下一策略。

## 父子事实模型

`chunks` 继续是稳定来源事实，但新增：

- `role`: `parent | child | flat`。
- `parent_chunk_id`: child 指向同一 ChunkSet 内的 parent；parent 与 flat 为 NULL。

父、子与 flat 均拥有完整 lineage、`source_content`、SourceAnchor、metadata 与首个 system ChunkRevision。parent 是由当前分块配置派生的上下文容器，在管理台只读；child 与 flat 继续沿用现有人工 revision 编辑能力。开启父子分块时，检索匹配 child 并返回派生 parent 全文；关闭时，检索匹配 flat 并返回自身全文。父块不会被人工编辑为与召回文本无关的独立内容。

父子分块开启时，v3 的 File/Web 文档即使只产生一个、且正文与唯一 child 完全相同的 parent，也必须同时持久化 parent 与 child。关闭父子分块时，只持久化可检索的 flat；不产生 parent 或 child。已存在的 v2 扁平 ChunkSet 按 flat 兼容读取。

迁移使用 workspace/KB/document/revision/chunk-set 复合外键与约束，确保 parent-child 关联不能跨租户或跨分块集，并为 parent 查询建立索引。

## 分块流水线

开启父子分块时，标准 ChunkStage 先通过策略生成 parent windows，再在每个 parent 内按策略生成 child windows，并为所有结果计算准确的来源锚点与 metadata。关闭时，ChunkStage 直接生成 flat。child 与 flat 的 sequence 在整篇文档内全局单调递增；parent 独立排序，供 UI 与检索解析使用。

DOCX 和 Markdown 已有结构化 manifest，可直接进入该路径。PDF 先由 MinerU 转 Markdown，再使用 Markdown parser 重建 block manifest；重建后的锚点仍标记为 PDF 文档级锚点。重解析失败属于可诊断的解析错误，不能静默退化成单 paragraph 分块。

chunker v3 与新的完整配置共同参与 ChunkSet config hash。已激活的 v2 Generation 保持不变；创建 v3 Generation 时，只要 chunker version 或 chunking config 有差异，就必须重新分块，随后经原有 inactive Generation 原子发布流程切换。

## 索引与检索

只有启用的 child 与 flat ChunkRevision 创建 `RetrievalEntry`，写入 Embedding 和 FTS。parent 不建检索投影。

查询先对 child 执行既有 vector、FTS、RRF。融合结果再按 parent 聚合：同一 parent 只返回一次，采用其命中 child 的最高融合分数。结果：

- `content` 是 parent 全文；flat 返回自身全文。
- source anchor 和 metadata 指向最终返回正文。
- 新增 `matched_children`，记录命中 child 或 flat 的 ID、角色、锚点、片段与分数，便于高亮与溯源。

`chunk_get` 保持按 Chunk ID 获取；文档 Chunk 列表公开 `role` 与 parent 关系。既有调用方继续读取 `content`，因此不会因响应扩展失效。

## 管理台体验

所有页面继续位于既有 authenticated AppShell 和知识库工作台内，复用现有 shadcn/Radix 表单、Tabs、Dialog、StatusBadge、SafeMarkdown 与工程绿语义 token。普通界面使用可读文档名、策略名、分块数量和状态，不渲染 UUID、config hash 或原始 metadata。

### 分块配置入口

新建知识库表单保留现有的名称、描述与 Embedding 模型主流程，并在分块字段下增加「分块方式」可展开区。新建索引代次页面复用同一配置字段，作为现有三步向导的第 2 步；后者是修改已存在知识库分块方式的唯一入口，明确告知会创建候选代次且当前检索不会中断。

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

`strategy` 使用 Select，选项为「自动选择、按标题、按文档结构、递归切分」。父子开关开启时显示 parent/child 大小与 parent overlap；关闭时显示既有扁平块大小与 overlap，父/子草稿值保留以便再次开启。Zod 与后端同时验证 parent 为 512–8192、child 为 64–2048、child 不大于 parent，现有 overlap 仍须小于生效的父块大小。字段错误原位显示且提交失败时保留草稿。管理员/owner 可创建候选代次；member 只可查看当前代次摘要，直达 `?create=true` 保持现有 403 体验。

### 文档分块检查器

文件详情的既有「分块」Tab 改为层级检查器。桌面端以 parent 作为可展开组，child 为组内可选择卡片；父子模式的短文本也显示为仅含一个 child 的 parent 组。关闭父子模式及历史 v2 的 ChunkSet 显示为独立 flat 卡片。查询参数继续使用 `?chunk=<chunk-id>` 选择和深链，打开检索来源时定位到命中的 child 或 flat。

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

选中 child 后沿用现有详情 Dialog，展示正文、来源和修订历史，并新增「完整上下文」只读区及返回 parent 的锚点。选中 parent 时 Dialog 仅展示完整上下文、来源和所含 child 列表，不显示编辑动作。编辑 child 成功后精确失效该文档的 chunk、revision、知识库摘要与相关检索缓存；revision 冲突继续保留草稿并呈现现有的最新版本处理。

### 检索结果

检索测试页每个结果卡的正文直接渲染 parent 全文。卡片同时用可读状态说明「命中 N 个子块」，并在下方列出命中片段、锚点与融合分数；「定位命中」链接使用第一个命中 child 的既有文件深链，而不是 parent ID。

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

### 状态、响应式与无障碍

Generation 构建中时，索引页保留候选代次的真实状态；不虚构分块进度。构建失败显示后端错误摘要和现有支持的操作；完成后显示「可激活」，激活成功后刷新工作台、文档 chunk 和检索 Query。没有分块时，检查器说明文档尚未完成索引，并链接到真实 Job；无权限、加载中和失败状态沿用现有骨架与 Error 工作面。

宽桌面显示父组及 child 列表；平板将 parent 详情移入 Sheet；移动端改为单栏 parent 卡片，点入后显示 child 列表和返回操作。功能语义与桌面一致，触控目标至少 44×44px。所有开关、Select、展开组、详情 Dialog 与编辑 Dialog 都支持键盘；打开/关闭 Dialog 时恢复焦点，状态除颜色外提供文字和图标，并遵循减少动效设置。

## 非目标

本次不引入 token limit、用户自定义 separators、语言配置和 chunk preview UI；这些独立于父子检索主链路，可后续添加。

## 测试与验收

实施遵循测试先行，覆盖：

- auto 的选择、输出校验与降级；标题 breadcrumb、代码完整性、表格行和表头规则；
- 父子关联、短文本 parent/child 同时持久化、锚点与 sequence 的确定性；
- child-only 索引、按 parent 去重、父块正文和 matched child 返回；
- v2 到 v3 的重新分块与 Generation 原子发布；
- REST/MCP 配置输入、响应兼容性和参数校验；
- 知识库创建表单、候选代次向导、父子检查器、检索结果命中片段、深链与角色可见性；
- 桌面/移动端信息等价、键盘焦点、Dialog 焦点恢复、表单字段错误和浅/深色主题；
- 临时 Docker pgvector 数据库上的迁移、复合外键、索引和检索集成测试。

所有数据库测试仅使用测试运行期启动的 PostgreSQL + pgvector 容器。
