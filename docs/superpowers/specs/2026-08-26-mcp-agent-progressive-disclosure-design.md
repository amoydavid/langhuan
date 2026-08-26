# v1.2.0 MCP Agent 工效：检索结果渐进披露 与 发现类工具 设计规格

- 日期：2026-08-26
- 状态：待评审
- 版本目标：v1.2.0
- 前置结论：检索质量已闭环（hybrid+rerank 达到"足够好"，分块参数/语义分块/上下文头生成/繁简归一化四条路均已实验排除，见 `RETRIEVAL_BENCHMARK.md` §4.4–§4.7）。本版本不做检索算法，做**检索的消费工效**。

## 1. 背景与问题

琅嬛的 MCP 面向 agent 提供知识检索，但当前 `knowledge_search` 的返回是"胖结果"：

- 每个命中携带完整父块正文（≤4096 字）+ 全部命中子块正文（384 字 × N），top 10 一次调用约 4~5 万字符，一次搜索即消耗 agent 上下文窗口的 10%+；
- `matched_children[].content` 与父块正文存在**构造性冗余**（chunker 的父子装配就是子块内容按序拼接成父块，见 `chunker.go` `mergeChildDraftContents`），纯粹是重复传输；
- agent 无法发现环境：7 个 MCP 工具里没有任何 list/browse 能力，agent 不知道 workspace 里有哪些知识库、库里有哪些文档、文档长什么结构，只能依赖系统提示词硬塞 ID。

业界先例支持渐进披露：Claude Code 的 Grep/Read 两段式（先返回 file:line + 匹配行，按需 Read 全文）是该模式在最高频 agent 场景的验证；MCP 官方与社区共识是"先轻量摘要、按需取全量、服务端过滤优于 agent 翻页"。

## 2. 目标 / 非目标

### 目标

1. `knowledge_search`（REST 与 MCP）新增响应档位 `detail: full | lean`，两档对称契约（见 §4）。
2. **`matched_children[].content` 字段从合同中无条件移除**（两档均不返回子块正文；元数据保留）。
3. MCP 新增三个发现/阅读类只读工具：`knowledge_base_list`、`document_list`、`document_get`（含文档大纲）。
4. web console 检索测试视图适配子块正文移除。
5. 评测客户端同步并验证指标逐位不变。

### 非目标

- 不改检索算法、索引、分块、RRF/rerank（检索侧已闭环）。
- 不做 token 预算上下文装配（RAG 应用开发者人格的需求，暂缓，待拉力证据）。
- 不做实体/关系写入（已搁置：外部写入破坏"索引是文档的确定性函数"不变量）。
- 不做分页游标协议改造（检索结果条数由 `final_top_k` 控制，服务端收敛优于 agent 翻页；`document_list` 用简单 page/page_size）。
- 不改 chunk_get（钻取层现成）。

## 3. 设计决策

### D1：`matched_children` 去 content，无条件

- 事实依据：子块正文构造上是父块正文的子串（父子装配即拼接 + 滑窗重叠去重），`full` 档下为纯冗余；`lean` 档的命中证据由独立的 `evidence` 字段承载（D3），不需要藏在 matched_children 里。
- 保留字段：`chunk_id`、`chunk_revision_id`、`role`、`source_anchor`、`score`、`vector_score`、`keyword_score`——它们承担"命中位置标记、钻取句柄、通道归因"职责，体积小。
- 这是 REST/MCP 响应的**破坏性变更**，随 v1.2.0 发布说明处理（见 §6）。

### D2：两档对称契约

| | 返回正文的单元 | 降级为句柄/元数据的单元 |
|---|---|---|
| `detail: full` | 父块（`content` = 完整父块正文，阅读单元） | 子块（`matched_children` 元数据，无正文） |
| `detail: lean` | 最佳命中子块（`evidence`，"为什么命中"的即时证据） | 父块（`chunk_id` 保留作钻取句柄，`content` 不返回） |

每档只返回自己层的正文，另一层降级为句柄。行粒度**两档一致**：一行 = 一个父块（flat 以自身为一行），排序语义完全不变（RRF 父块聚合照旧），只是投影不同。

### D3：lean 档的 `evidence` 定义

- `evidence` = 该父块下**综合得分最高的命中子块**完整信息：`chunk_id`、`chunk_revision_id`、`role`、`content`（子块正文，≤384 字）、`source_anchor`、`score`、`vector_score`、`keyword_score`。
- 其余命中子块仍以元数据形式列在 `matched_children`（数量、位置、通道分数可见，正文不重复）。
- 理由：纯 ID 清单会逼 agent 盲钻（两段式退化成三段式）；Grep 先例的精髓是返回匹配行本身。384 字证据让多数查询一轮结束，需要完整上下文时经 `chunk_get(chunk_id)`（父块 ID）钻取。

### D4：默认值按协议面区分

- **MCP 默认 `lean`**：agent 是 MCP 的主用户，旧行为（胖结果）本来就是待修正项。
- **REST 默认 `full`**：现有调用方（console、评测、用户脚本）行为不变，显式传参才切换。
- MCP 请求 `detail: "full"` 可显式取回父块正文。

### D5：发现/阅读类工具复用现有 service，只补 MCP 协议面

REST 已有等价端点（console 在用：知识库列表/文档列表/文档详情）。MCP 三个新工具全部为只读包装：

- `knowledge_base_list`：当前主体可见的知识库（API Key 场景 = 绑定的知识库集合），返回 `id/name/description/document_count/updated_at`。scope：`knowledge_bases:read`。
- `document_list`：库内文档列表（分页），返回 `id/name/kind/status/error_message/updated_at`。scope：`documents:read`。
- `document_get`：文档详情 + 全文（`NormalizedMarkdown`，`max_chars` 上限默认 50000，超出截断并标记 `truncated`）+ `outline`。scope：`documents:read`。
- `outline` 从 `ParseManifest` 的 heading 块序列化生成：`[{path: [标题路径], anchor}]`；无标题结构的文档（如纯 TXT）`outline` 为空数组，前端/agent 以 `content` 为准。第一版不做 大纲→chunk 映射（钻取路径仍是 search → chunk_get）。

### D6：评测客户端同步（单测证明，不跑评测）

`cmd/langhuan-eval/run.go` 的 `ranksOf` 当前把 `item.Content + MatchedChildren[].Content` 拼接做重叠判定。子块正文 ⊆ 父块正文 ⇒ 移除后 bigram 集合不变 ⇒ 指标数学上不变。实现时删除该拼接，**以 `ranksOf` 的表驱动单测证明恒等**（同一批结果，带/不带子块正文的命中排名完全一致）——纯函数测试，秒级。本特性不触碰 chunker/RRF/默认检索参数/embedding 链路，按 AGENTS.md 评测回归规则在 PR 中标注"不适用"；若实现过程中检索内部逻辑被意外改动（超出投影层），必须回到本节补跑 `make eval` 对比。

### D7：已否决的备选

- ~~瘦结果返回纯 ID 清单~~：逼 agent 盲钻，往返退化（见 D3）。
- ~~lean 档行粒度改子块级~~：破坏父块级排序语义，且同父多子块命中会产生重复行。
- ~~`matched_children.content` 按 detail 条件保留~~：两份正文来源增加合同复杂度；无条件移除 + `evidence` 承载更干净。
- ~~新增"expand parent"专用工具~~：`chunk_get` 按 ID 可取任意角色 chunk，已覆盖。

## 4. 接口合同

### 4.1 REST

`POST /api/v1/workspaces/:slug/knowledge-bases/:id/search` 请求体新增可选字段：

```json
{ "query": "...", "vector_top_k": 50, "keyword_top_k": 50, "final_top_k": 10,
  "detail": "lean" }
```

- `detail` 缺省 `full`；非法值返回 `validation_error`。
- 多库检索端点同样支持。

响应 `SearchResult`（两档共用结构，字段按档位出现）：

```json
{
  "chunk_id": "父块或 flat 的 chunk ID",
  "chunk_revision_id": "...",
  "document_id": "...", "document_kind": "file", "document_name": "...",
  "content": "仅 detail=full：完整父块正文",
  "evidence": {
    "chunk_id": "...", "chunk_revision_id": "...", "role": "child",
    "content": "仅 detail=lean：最佳命中子块正文",
    "source_anchor": {}, "score": 0, "vector_score": 0, "keyword_score": 0
  },
  "score": 0, "vector_score": 0, "keyword_score": 0, "rerank_score": 0,
  "ranking_stage": "...", "metadata": {},
  "matched_children": [
    { "chunk_id": "...", "chunk_revision_id": "...", "role": "child",
      "source_anchor": {}, "score": 0, "vector_score": 0, "keyword_score": 0 }
  ],
  "knowledge_base_id": "...", "knowledge_base_name": "...",
  "document_revision_id": "...", "index_generation_id": "...", "citation": {}
}
```

`SearchRun`/`search_id`/归因头等运行元数据合同不变（v0.9 血缘不受影响）。

### 4.2 MCP

`knowledge_search` input 新增 `detail`（枚举，**默认 lean**），output 同 §4.1 投影。工具描述更新：明确两档语义与"需要完整上下文时用 `chunk_get(chunk_id)` 取父块"的钻取路径。

新增工具（typed tool + `withRawInputSchema/withRawOutputSchema` 现有模式）：

```text
knowledge_base_list  ->  { knowledge_bases: [{id, name, description, document_count, updated_at}] }
document_list        ->  { knowledge_base_id, page?, page_size? }
                          ->  { documents: [{id, name, kind, status, error_message, updated_at}], page, page_size, has_more }
document_get         ->  { knowledge_base_id, document_id, max_chars? }
                          ->  { id, name, kind, status, updated_at, truncated, content, outline: [{path: [], anchor}] }
```

全部标注 `readOnlyHint`。鉴权沿用现有模式：workspace 隔离 + API Key 绑定知识库收敛（`ResourceAccess`）+ 对应 scope。

## 5. 实现落点（分层清单）

| 层 | 变更 |
|---|---|
| `application/dto` | `MatchedChild` 删除 `Content`；`SearchResult` 新增 `Evidence *MatchedEvidence`；`MultiKnowledgeSearchInput`/`SearchInput` 新增 `Detail` |
| `application/service` | search/multi_knowledge_search：装配时按 `Detail` 投影（lean：选最高分子块入 `Evidence`、父块 `Content` 置空；full：现状减子块正文）；父块聚合/去重逻辑只用元数据，不受影响；新增 `document_get`/`document_list`/`kb_list` 的 service 方法（多数为现有方法包装 + outline 生成函数） |
| `interfaces/http` | search handler 透传 `detail`；`openapi_routes.go` 契约更新 |
| `interfaces/mcp` | `tools.go`：search 加参数与描述更新；新增三工具；`schema.go` 类型 |
| `web/` | 检索测试视图：命中子块行改为元数据展示（锚点/分数/ID 链接），移除正文渲染；schema 同步 |
| `cmd/langhuan-eval` | `ranksOf` 移除子块正文拼接（D6） |
| 迁移 | 无 schema 变更，无迁移 |

## 6. 破坏性变更与兼容

- `matched_children[].content` 移除（REST + MCP 双面）。影响方：
  - **web console**：`retrieval-test.tsx` 渲染子块正文 → 本版本同步改为元数据行（父块正文已在上方完整展示，子块正文本就是其子串，信息不丢失）；
  - **langhuan-eval**：拼接移除，指标逐位不变（D6 验收）；
  - **外部 REST 调用方**：v1.2.0 CHANGELOG 显式声明；需要子块正文的场景改用 `detail=lean` 的 `evidence` 或 `chunk_get`。
- MCP `knowledge_search` 默认档位由（事实上的）full 变为 lean：行为变更，写入 CHANGELOG 与工具描述。

## 7. 测试与验收标准

1. **单元**：lean 投影（最佳子块选择按 score、同分按 chunk_id 稳序）、`MatchedChild` 序列化不含 content、outline 生成（有标题/无标题文档）。
2. **集成（临时 docker PG，遵循 5.10）**：detail 两档 E2E；发现类工具的 workspace 隔离与 API Key 绑定收敛；`document_get` 截断。
3. **回归（快验收，不跑评测）**：
   - `ranksOf` 表驱动单测：带/不带子块正文的命中排名逐位一致（D6，秒级）；
   - 集成断言：同一 query 的 `detail=full` 与 `detail=lean` 返回相同的 chunk_id 顺序与分数（投影不改排序）；
   - 负载断言：lean top10 响应体 ≤ 10,000 字符（现状 full 约 45,000）。
4. **前端**：`pnpm check` / `pnpm test` / `pnpm build` 通过；检索测试视图快照更新。
5. **MCP smoke**：工具注册数 7 → 10；schema 校验通过。

## 8. 开放问题（实现时确认）

- `document_get` 的 `max_chars` 默认值（暂定 50000，按真实语料长度分布定）；
- `outline` 是否需要在 v1.3.0 增加"节 → chunk_id 映射"（取决于 agent 实际使用反馈）；
- rerank 开启时 `evidence` 子块选择是否应改用 `rerank_score`（当前子块级 rerank 分数是否存在视实现而定，暂按综合 `score`）。

## 9. 关联文档

- 检索评测结论：`RETRIEVAL_BENCHMARK.md` §4.4–§4.7
- 评测设计：`docs/superpowers/specs/2026-08-24-retrieval-eval-design.md`
- 检索证据血缘（响应合同基础）：`docs/superpowers/specs/2026-08-10-search-evidence-lineage-replay-design.md`
