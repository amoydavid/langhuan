# MCP 写能力全量补齐（text 直写 / FAQ / 文件树 / Web 内容源）设计规格

- 日期：2026-11-10
- 状态：已评审通过（2026-11-10；范围裁决=全量补齐含 web source CRUD，text 形式裁决=独立 `document_ingest_text` 工具）
- 版本目标：v1.5.0（候选）
- 性质：纯 MCP 接口层扩展——复用全部既有 application service，无新迁移、无新领域模型、不改 REST 行为

## 1. 目标与问题

MCP 当前 10 个工具的写面只有「建库 / 传文件（Base64）/ 往 urls 源加 URL / 重试 / 删除」。REST 已有的四块写能力没有 MCP 出口：

1. **text 直写**（`POST /knowledge-bases/:id/documents/text`）——Agent 把自己生成的内容（笔记、总结、会话沉淀）写进知识库，必须先 Base64 编码伪装成文件，摩擦大且语义失真；
2. **FAQ 问答对**（`POST/PUT .../documents/faq`）——FAQ 是 Agent 记忆的天然形态（问题→答案），当前完全不可写；
3. **文件树**（`POST .../file-tree/folders` 等）——`document_ingest` 已接受 `parent_node_id`/`node_name`，但 MCP 没有任何工具能列出树、拿不到 node ID，该参数形同虚设；
4. **Web 内容源 CRUD**（`POST/PATCH/DELETE .../web-sources`）——只能"往唯一 urls 源追加 URL"，不能建源、改配置、触发同步、删源。

本版本把四块能力全量补进 MCP，工具数 10 → 24。验收判据：一个只持有 MCP 入口和 API Key 的 Agent，不借助 REST，能完成「写入内容（text/FAQ）→ 组织文件树 → 建立/维护网站源 → 触发同步」的完整内容运营闭环。

## 2. 现状与可复用合同（探索结论）

全部 service 现成，本版**零 application 层改动**：

| 能力 | 既有 service | 说明 |
|---|---|---|
| text 直写 | `DocumentIngestService.Ingest`（`service.IngestDocumentInput`） | REST `/documents/text` 即此路径：`FileName=title+".md"`、`ContentType="text/markdown"`、`Reader=strings.NewReader(content)`；幂等键 `IdempotencyKey`+`CallerAPIKeyID` 仅 Bearer 生效 |
| FAQ | `FAQDocumentService.Create/Update/Get` | 单事务建 Document(kind=faq)+ready Revision+FAQRevision+document_index Job；Update 乐观锁（`ActiveRevisionID != BaseRevisionID` → `ErrRevisionConflict`）；FAQ 恰 1 个 chunk（问题做 embedding、答案做内容） |
| 文件树 | `FileTreeService.List/CreateFolder/Update/Move/Delete` | Delete 仅空 folder（`ErrFileTreeNotEmpty`）；Update 重命名 file 节点联动改 Document.Title；DTO 主键是 `id`（非 node_id） |
| Web 源 | `KbSourceService.CreateWebSource/ListWebSources/GetWebSource/UpdateWebSource/SyncWebSource/DeleteWebSource`（+既有 `AddURLToWebSource`） | create/PATCH 成功 best-effort 入队同步；config 为 `map[string]any` 透传，`value.ParseWebSourceConfig` 全量校验（未知键拒绝）；`RejectImmutableWebSourceConfigKeys` 拦不可变键 |

装配零成本：`runtimeServices` 已有 `faqDocuments`/`fileTree`/`kbSources` 字段（`cmd/langhuan/main.go:152-167`），只差 MCP `Dependencies` 接线。

鉴权模型不变：MCP 入口 API-key-only（`APIKeyOnlyAuth`），门控 = scope 检查 + `ResourceAccess.AllowsKnowledgeBase` 绑定集合。REST 写路径上的 `RequireAdminForSession()` 对 Bearer 本来就不适用，MCP 与 REST Bearer 语义天然对齐。

## 3. 工具面（10 → 24）

### 3.1 新工具合同

| 工具 | 输入（JSON 字段） | 输出要点 | scope | annotation |
|---|---|---|---|---|
| `document_ingest_text` | `knowledge_base_id`*, `title`*, `content`*, `parent_node_id`?, `node_name`?, `idempotency_key`? | `{document_id, job_id, status:"pending", deduped:false}`，异步、`document_status` 轮询 | documents:write | — |
| `faq_create` | `knowledge_base_id`*, `title`*, `questions`*(≥1), `answer`* | `{document_id, revision_id, revision_no, status, questions, answer, job_id?}` | documents:write | — |
| `faq_update` | `knowledge_base_id`*, `document_id`*, `base_revision_id`*, `questions`*, `answer`* | 同上；冲突→`revision_conflict` 错误码 | documents:write | — |
| `faq_get` | `knowledge_base_id`*, `document_id`* | 活跃修订的 questions+answer+revision 元信息 | documents:read | ReadOnly |
| `file_tree_list` | `knowledge_base_id`* | 嵌套树（id/parent_id/node_type/name/document_id/path/children），根为虚拟 root | documents:read | ReadOnly |
| `file_tree_folder_create` | `knowledge_base_id`*, `parent_id`*, `name`* | 新节点 `{id, parent_id, node_type:"folder", name, path}` | documents:write | — |
| `file_tree_node_update` | `knowledge_base_id`*, `node_id`*, `name`?, `parent_id`?（至少一项） | 204 语义→`{node_id, updated:true}` | documents:write | — |
| `file_tree_node_delete` | `knowledge_base_id`*, `node_id`* | `{node_id, deleted:true}`；非空 folder→`validation_error`（ErrFileTreeNotEmpty） | documents:write | Destructive |
| `web_source_list` | `knowledge_base_id`* | 源数组：id/name/status/mode/page_count/failed_page_count/next_sync_at/sync_last_result | knowledge-bases:read | ReadOnly |
| `web_source_get` | `knowledge_base_id`*, `source_id`* | 完整源详情含 `config`（供 Agent 学习 config 形状） | knowledge-bases:read | ReadOnly |
| `web_source_create` | `knowledge_base_id`*, `name`*, `config`*(object 透传), `status`?("active"/"disabled") | `{source_id, name, status, job_id?(同步已入队)}`；config 非法→`validation_error` | knowledge-bases:write | — |
| `web_source_update` | `knowledge_base_id`*, `source_id`*, `name`?, `status`?, `config`? | 同上；不可变键（mode/seed_url 等）→`validation_error` | knowledge-bases:write | — |
| `web_source_sync` | `knowledge_base_id`*, `source_id`*, `force`?(默认 false) | `{job_id}`（202 语义：入队即返回） | knowledge-bases:write | — |
| `web_source_delete` | `knowledge_base_id`*, `source_id`*, `pages`?("keep"默认/"remove") | `{source_id, deleted:true}` | knowledge-bases:write | Destructive |

`*` = required。`web_source_update` 的 config 语义严格对齐 REST PATCH（部分键合并、全量重校验），不在 MCP 层另造合并规则。

### 3.2 命名与渐进披露

- 前缀风格与现有 `knowledge_base_*`/`document_*`/`chunk_get` 一致：`faq_*`、`file_tree_*`、`web_source_*`。
- 描述（中文）延续 v1.2.0 渐进披露引导：
  - `file_tree_list`：浏览文件夹/文件树，`document_ingest(_text)` 与 `file_tree_folder_create` 的 `parent_node_id` 从这里获取；
  - `web_source_create`：先 `web_source_get` 参考既有源的 config 形状再创建；
  - `faq_get`：编辑前取当前内容与 `base_revision_id`（也可从 `document_status` 拿修订 ID）。

## 4. 实现要点

### 4.1 契约（`internal/interfaces/mcp/contracts.go`）

- `MCPIngestInput` 增加 `IdempotencyKey string`、`CallerAPIKeyID *uuid.UUID`（file 模式不传、行为不变）。
- 新增窄接口（全部被现有 service 直接满足，无 adapter）：
  - `MCPFAQService`：`Create/Update/Get`，签名 = `*service.FAQDocumentService` 子集；
  - `MCPFileTreeService`：`List/CreateFolder/Update/Delete`，签名 = `*service.FileTreeService` 子集；
  - `MCPWebSourceService` 扩展：在既有 `AddURLToWebSource` 上追加 Create/List/Get/Update/Sync/Delete 六方法（签名照抄 `http/web_source_handler.go` 的 `WebSourceService` 接口）。
- `toolScopeRequirement` 登记 14 个新工具（读=read scope、写=write scope，逐工具对应 §3.1 表）。

### 4.2 适配器（`adapters.go`）

`documentIngestAdapter.Ingest` 透传两个新字段（`IdempotencyKey` 与 `Dedupe` 互斥由 service 层既有校验兜底）。FAQ/FileTree/WebSource 输入在 handler 直接构造 service input（`Access = auth.ResourceAccess()`、`WorkspaceID = auth.WorkspaceID`），不需要 adapter——遵循"简单业务允许简单实现"。

### 4.3 工具文件（每个资源一个文件）

- `document_ingest_text_tool.go`：`CallerAPIKeyID = auth.PrincipalID`（API Key 主体的 PrincipalID 即 key ID）；`SourceType="mcp"`（与 file 模式一致，标记来源协议）；InlineLimit 复用 file 模式的检查路径。
- `faq_tools.go` / `file_tree_tools.go` / `web_source_tools.go`：统一骨架 `authFromRequest → HasScope → UUID parse → AllowsKnowledgeBase → service → jsonResult`，输出 schema 用 `withRawOutputSchema[T]`（`schema.go` 既有机制，uuid→string 全局映射自动生效）。

### 4.4 注册与装配

- `server.go`：`Dependencies` 加 `FAQDocuments MCPFAQService`、`FileTree MCPFileTreeService`；`registerTools` 追加 14 个注册调用（发现/阅读类之后按资源分组）。
- `cmd/langhuan/main.go` MCP Dependencies 字面量：`FAQDocuments: services.faqDocuments`、`FileTree: services.fileTree`（`WebSources: services.kbSources` 已注入，接口扩展自动生效）。

### 4.5 错误映射（`errors.go mapDomainError`）

补齐领域 sentinel → 稳定错误码：`ErrRevisionConflict` → `revision_conflict`（可重试：重新 `faq_get` 取最新 base_revision_id）；`ErrFileTreeCycle/ErrFileTreeNotEmpty/ErrFileTreeNameConflict` → `validation_error`；web source config 校验错误确认走 `ErrValidation` 通道。未识别错误仍统一 `internal_error`，不泄漏内部细节。

## 5. 测试与验收

- 单测（手写 fake + `value.ContextWithAuthContext` 注入，照 `document_ingest_url_test.go` 模式）：每个工具文件配对 `*_test.go`，覆盖分派/必填校验/scope 拒绝/KB 绑定拒绝/服务错误透传/成功形状。
- `server_test.go`：工具全集断言 10 → 24；`scopeToolFilter` 按新 scope 矩阵过滤；input/output schema 与 struct 声明一致性；annotation（ReadOnly/Destructive）断言。
- 全量：`gofmt` / `go vet ./...` / `go test ./...`（MCP 单测不触数据库）。
- 真机冒烟（standalone SQLite）：MCP JSON-RPC 走通 `tools/list`（24 工具、scope 过滤生效）→ `faq_create` → `faq_update`（错误 base_revision_id 验证 `revision_conflict` 码）→ `file_tree_folder_create` → `document_ingest_text` → `web_source_create/sync`。
- 不动 chunker/RRF/检索参数/embedding 链路，无需 `make eval` 回归（按 ROADMAP 规则声明不适用）。

## 6. 不做

- 不改 REST 行为、不加 application service、无 schema 迁移。
- web/e2e 不扩（MCP 不在浏览器流程内）。
- 发版打 tag、双远程推送、官网 changelog：另行处理。
- `web_source_create` 不做结构化 config 字段（保持 object 透传 + description 引导），若真实调用方反馈易错再评估。
