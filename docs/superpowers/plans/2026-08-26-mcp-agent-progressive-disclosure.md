# MCP Agent 工效：检索渐进披露与发现类工具 Implementation Plan

> **For agentic workers:** Steps use checkbox (`- [ ]`) syntax for tracking. 设计依据：`docs/superpowers/specs/2026-08-26-mcp-agent-progressive-disclosure-design.md`（三轮评审定稿版）。

**Goal:** `knowledge_search` 新增 `detail: full|lean` 响应档位（双协议默认 full，lean 显式 opt-in）；`matched_children[].content` 从契约无条件移除（内部保留供 evidence 装配）；MCP 新增 `knowledge_base_list` / `document_list` / `document_get` 三个只读工具。

**Tech Stack:** Go 1.26、Gin、mcp-go、React 19 + TS + Zod + Vitest。

## Global Constraints

- 不改检索算法/索引/分块：本特性只动**响应投影层**与协议面。若实现中检索内部逻辑被改动（超出投影），必须补跑 `make eval` 对比（spec D6）。
- 行粒度与排序语义两档完全一致：投影发生在最终截断之后。
- `MatchedChild.Content` 以 `json:"-"` 实现契约移除（内部保留：lean 的 `evidence` 装配与 rerank 的 searchContent 通道互不依赖）。
- MCP 工具全部只读、走 typed tool + 现有 scope 过滤（`scopeToolFilter`）。
- 数据库测试仅用临时 docker 容器（AGENTS 5.10）；前端过 `pnpm check`/`pnpm test`/`pnpm build`。

## 文件结构

| 路径 | 职责 |
|---|---|
| `internal/domain/value/search_detail.go` | `SearchResultDetail` 枚举 + 解析/校验 |
| `internal/application/dto/search.go` | `MatchedChild.Content`→`json:"-"`；新增 `MatchedEvidence`、`SearchResult.Evidence`；投影函数 `ProjectSearchDetail` |
| `internal/application/service/{search,multi_knowledge_search}.go` | 输入加 `Detail`；装配时挂 Evidence；截断后投影 |
| `internal/interfaces/http/search_handler.go` + `openapi_routes.go` | `detail` 参数与契约更新 |
| `internal/interfaces/mcp/{tools,contracts,adapters,server}.go` | search 参数 + 3 新工具 + Dependencies |
| `web/src/features/retrieval/{schemas.ts,retrieval-test.tsx}` | 子块行改元数据展示 |
| `cmd/langhuan-eval/run.go` + 新单测 | `ranksOf` 去子块正文拼接 + 恒等表驱动测试 |

## Tasks

- [x] T1 domain：`value/search_detail.go`——`SearchResultDetail`（`full`/`lean`），空值归一为 full，非法值返回 ErrValidation；表驱动单测
- [x] T2 dto：`MatchedChild.Content` 加 `json:"-"`（注释说明内部用途）；新增 `MatchedEvidence`（= MatchedChild 字段 + Content）；`SearchResult.Evidence *MatchedEvidence json:"evidence,omitempty"`；`ProjectSearchDetail(results, detail)`：lean→保留 Evidence、`Content=""`；full→`Evidence=nil`（Content 保留）；单测
- [x] T3 service/search.go：`SearchInput.Detail`；`SearchResultFromEvidence` 同时装配 `Evidence`（复用 matchedChildFromEvidence 数据）；分组/排序/rerank/截断后调用投影；现有 search 测试补两档断言
- [x] T4 service/multi：`MultiKnowledgeSearchInput.Detail` 穿线；`groupMultiSearchResults` 合并时保留得分较高的 Evidence；全局排序+截断后投影；测试补断言
- [x] T5 REST：`searchRequest.Detail *string` 严格 JSON 解码 + 枚举校验（非法→400 validation_error）；`openapi_routes.go` 请求/响应说明更新
- [x] T6 MCP search：`knowledgeSearchInput.Detail`（默认 full，描述引导 agent 用 lean）；contracts 输出 schema 同步
- [x] T7 MCP 发现工具：`knowledge_base_list`（ScopeKnowledgeBasesRead，复用 `KnowledgeBaseService.List`，按 API Key 绑定收敛）、`document_list`（ScopeDocumentsRead，`DocumentService.List` 分页）、`document_get`（ScopeDocumentsRead，详情 + `NormalizedMarkdown` + `outline` 自 ParseManifest heading 块，`max_chars` 默认 50000 截断）；`scopeToolFilter` 注册
- [x] T8 web：`schemas.ts` 去 `matched_children[].content`；`retrieval-test.tsx` 子块行改元数据（锚点/分数/ID 链接）；vitest 更新
- [x] T9 langhuan-eval：`ranksOf` 移除子块正文拼接；新增表驱动单测证明带/不带子块正文命中排名逐位一致（spec D6 硬验收）
- [x] T10 验收：`go test ./...`（含集成）、`go vet`、`gofmt`、`pnpm check/test/build`；lean top10 序列化长度 ≤15000 字符且 ≤full 的 1/3 断言；同 query 两档 chunk_id 顺序与分数一致断言
- [x] T11 提交推送（中文 Conventional Commit，PR 说明标注 eval 回归"不适用"理由）

## 验收标准（对照 spec §7）

1. `ranksOf` 恒等单测（秒级，替代评测跑批）
2. 同 query `detail=full` 与 `detail=lean`：相同 chunk_id 顺序与分数
3. lean top10 响应 ≤15,000 字符且 ≤full 的 1/3（实测 ~13.7k vs ~41k+）
4. `matched_children[].content` 不出现在任何 JSON 序列化输出（REST/MCP/eval 客户端）
5. MCP 工具 7→10，新工具只读 + scope 过滤正确
