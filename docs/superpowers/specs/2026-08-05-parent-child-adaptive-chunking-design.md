# 父子分块与自适应分块设计

## 目标

为 File/Web 文档提供两层分块：小的 child chunk 负责 Embedding 与 FTS 召回，较大的 parent chunk 提供最终返回的完整上下文。分块策略扩展为接近 WeKnora 的 `auto`、`heading`、`heuristic`、`recursive`，同时保留琅嬛已有的 parse manifest、表格完整行和来源锚点合同。

FAQ 保持现有的单一 `strategy=faq` 语义，不使用父子分块。

## 配置与策略

`ChunkingConfig` 扩展为：

- `strategy`: `auto | heading | heuristic | recursive`；默认 `auto`。
- `enable_parent_child`: 默认 `true`。
- `parent_chunk_size`: 默认 4096 rune。
- `child_chunk_size`: 默认 384 rune。
- 既有 `chunk_overlap` 继续作为 parent overlap；child overlap 固定为 child size 的约 20%。

配置在 KnowledgeBase 创建、REST/MCP 请求和 Generation 创建中传递，并完整写入 Generation 的不可变 `chunking_config` 快照及 config hash。初始与新建 Generation 都使用 chunker v3。

`auto` 依序选择并验证输出：

1. `heading`：依据 manifest 标题路径及 Markdown 标题做标题感知分块，Embedding 内容包含 breadcrumb。
2. `heuristic`：依据分页符、编号章节、中英文章节标记、全大写标题、分隔线和空白段落等边界分块。
3. `recursive`：按段落、换行、句末标点的优先级递归切分，作为最终兜底。

标题、代码和表格仍优先使用 manifest。表格仅在完整行之间切分，每个 table chunk 重复表头；代码块不能被启发式边界切断。策略结果须经过大小与碎片率校验，失败时降级至下一策略。

## 父子事实模型

`chunks` 继续是稳定来源事实，但新增：

- `role`: `parent | child`。
- `parent_chunk_id`: child 指向同一 ChunkSet 内的 parent，parent 为 NULL。

父、子均拥有完整 lineage、`source_content`、SourceAnchor、metadata 与首个 system ChunkRevision。每一层都可独立追加人工 revision；编辑 parent 不会隐式覆盖 child，反之亦然。检索始终匹配 child 的有效 revision，并返回 parent 的有效 revision，两者的独立修订历史会在接口中明确暴露。

对一个 parent 只产生一个、且正文完全相同的 child 时，不持久化冗余 parent；该 child 以自身作为最终上下文，行为等价于返回 parent 全文。

迁移使用 workspace/KB/document/revision/chunk-set 复合外键与约束，确保 parent-child 关联不能跨租户或跨分块集，并为 parent 查询建立索引。

## 分块流水线

标准 ChunkStage 先通过策略生成 parent windows，再在每个 parent 内按策略生成 child windows，并为所有结果计算准确的来源锚点与 metadata。child 的 sequence 在整篇文档内全局单调递增；parent 独立排序，供 UI 与检索解析使用。

DOCX 和 Markdown 已有结构化 manifest，可直接进入该路径。PDF 先由 MinerU 转 Markdown，再使用 Markdown parser 重建 block manifest；重建后的锚点仍标记为 PDF 文档级锚点。重解析失败属于可诊断的解析错误，不能静默退化成单 paragraph 分块。

chunker v3 与新的完整配置共同参与 ChunkSet config hash。已激活的 v2 Generation 保持不变；创建 v3 Generation 时，只要 chunker version 或 chunking config 有差异，就必须重新分块，随后经原有 inactive Generation 原子发布流程切换。

## 索引与检索

只有启用的 child ChunkRevision 创建 `RetrievalEntry`，写入 Embedding 和 FTS。parent 不建检索投影。

查询先对 child 执行既有 vector、FTS、RRF。融合结果再按 parent 聚合：同一 parent 只返回一次，采用其命中 child 的最高融合分数。结果：

- `content` 是 parent 全文（短文本无持久化 parent 时为 child 全文）。
- source anchor 和 metadata 指向最终返回正文。
- 新增 `matched_children`，记录命中 child 的 ID、锚点、片段与分数，便于高亮与溯源。

`chunk_get` 保持按 Chunk ID 获取；文档 Chunk 列表公开 `role` 与 parent 关系。既有调用方继续读取 `content`，因此不会因响应扩展失效。

## 非目标

本次不引入 WeKnora 的 token limit、用户自定义 separators、语言配置和 chunk preview UI；这些独立于父子检索主链路，可后续添加。

## 测试与验收

实施遵循测试先行，覆盖：

- auto 的选择、输出校验与降级；标题 breadcrumb、代码完整性、表格行和表头规则；
- 父子关联、短文本冗余 parent 消除、锚点与 sequence 的确定性；
- child-only 索引、按 parent 去重、父块正文和 matched child 返回；
- v2 到 v3 的重新分块与 Generation 原子发布；
- REST/MCP 配置输入、响应兼容性和参数校验；
- 临时 Docker pgvector 数据库上的迁移、复合外键、索引和检索集成测试。

所有数据库测试仅使用测试运行期启动的 PostgreSQL + pgvector 容器。
