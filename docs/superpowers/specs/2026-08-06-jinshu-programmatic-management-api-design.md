# Jinshu 管理面程序化 API 开放设计

> 状态：已获用户批准，待实现
> 日期：2026-08-06
> 范围：仅修改 langhuan；jinshu 只作为 API 消费方，不在本仓库实现。

## 1. 目标

为 jinshu 管理面提供一组稳定、可审计、按 Workspace 与 KnowledgeBase 隔离的 Bearer API，同时保留现有浏览器 Session 调用语义。交付内容包括：

- 新增 `knowledge_bases:read` scope。
- 将知识库、文档、FAQ、文件树、摘要、任务、Chunk 只读和必要写操作开放到现有 `progGroup`。
- 新增 Markdown 文本内容导入，复用现有异步解析、分块、Embedding 与索引流水线。
- 为文档列表增加 `kind=file|faq|web` 过滤。
- 开放受限的 Embedding 模型列表，供 jinshu 建库表单选择模型。
- 统一 URL lineage：所有业务 API 带 `workspace_slug`；所有 KnowledgeBase 相关资源同时带 `knowledge_base_id`。

## 2. 范围与非目标

### 2.1 本次范围

所有新增或迁移的接口仍位于 `/api/v1/workspaces/:workspace_slug`。Session 与 Bearer 共用 handler、DTO 和 application service；Bearer 由 `SessionOrAPIKeyAuth` 识别，并由 scope middleware 与 service 级 `ResourceAccess` 双重约束。

FAQ 查询和更新的 URL 从现有的：

```text
/workspaces/:workspace_slug/documents/:document_id/faq
```

统一为：

```text
/workspaces/:workspace_slug/knowledge-bases/:knowledge_base_id/documents/:document_id/faq
```

旧 FAQ 路径不再注册，避免同一业务资源存在缺少 KnowledgeBase lineage 的入口。

### 2.2 明确不做

| 项 | 原因 |
|---|---|
| 删除 KnowledgeBase | 涉及 Generation、RetrievalEntry、FileTree 级联，生命周期仍由 langhuan Web Console 管理 |
| Chunk 修订编辑/启停 | 仍是 Session admin 高级操作 |
| Generation 创建/激活 | 仍是 Session admin 高级操作 |
| Search Settings 写入 | 仍限 Session owner/admin |
| HTML 文本解析 | 当前在线编辑合同只支持 Markdown；jinshu 负责 HTML→Markdown 转换 |
| jinshu 代码或跨仓库联调 | 本 worktree 只包含 langhuan 代码合同、测试和文档 |
| 数据库迁移 | 本次只增加查询过滤、鉴权和 HTTP 能力，不改变 schema |

## 3. 统一访问模型

### 3.1 URL lineage

- `workspace_slug` 由 `RequireWorkspace` 解析为 `AuthContext.WorkspaceID`。
- 任何 KnowledgeBase 资源 URL 必须再携带 `knowledge_base_id`（路由参数名为 `id`）。
- Document-only 状态接口（例如既有 `GET /documents/:document_id`）不在本次迁移范围；本次新增/开放的文档子资源必须在 URL 中携带 KnowledgeBase ID。
- Application input 对数据库业务操作始终显式保存 `WorkspaceID`；KnowledgeBase 子资源额外保存 `KnowledgeBaseID`，Repository 查询使用完整 lineage。

### 3.2 主体与越界

Session 主体继续按 workspace membership 和 role 授权，`ResourceAccess.Unrestricted=true`。Bearer API Key 主体携带固定 workspace、scopes 和绑定的 `KnowledgeBaseIDs`，不继承创建者的 role。

访问判定顺序：

1. `SessionOrAPIKeyAuth`：Bearer 缺失/无效返回 `401 unauthorized`；存在 Authorization 时不回退 Cookie。
2. `RequireWorkspace`：跨 workspace 统一 `404 not_found`。
3. `RequireWorkspaceRole(member)`：保持工作区成员边界。
4. `RequireScopeForAPIKey(scope)`：scope 不足返回 `403 insufficient_scope`；Session 主体直接通过。
5. 带 `:id` 的 KnowledgeBase 路由执行 `RequireKnowledgeBaseForAPIKey("id")`，未绑定统一 `404`。
6. Service 使用 `ResourceAccess` 对列表和 document-only 关联资源做最终检查，避免未来新增入口绕过 middleware。

越界不能通过错误消息、空结果差异或底层错误泄漏资源存在性。

## 4. HTTP 合同

以下路径省略公共前缀 `/api/v1/workspaces/{workspace_slug}`。

| 方法与路径 | Bearer scope | 响应 | 访问规则 |
|---|---|---|---|
| `GET /knowledge-bases` | `knowledge_bases:read` | `200 []*dto.KnowledgeBase` | Session 全量；Bearer 仅返回绑定 KB |
| `GET /knowledge-bases/{id}` | `knowledge_bases:read` | `200 dto.KnowledgeBase` | 绑定 KB 外统一 404 |
| `PATCH /knowledge-bases/{id}` | `knowledge_bases:write` | `200 dto.KnowledgeBase` | Session owner/admin；Bearer 仅绑定 KB |
| `GET /knowledge-bases/{id}/summary` | `knowledge_bases:read` | `200 dto.KnowledgeBaseSummary` | 绑定 KB 外统一 404 |
| `GET /knowledge-bases/{id}/documents` | `documents:read` | `200 []*dto.Document` | 可选 `kind=file|faq|web` |
| `GET /knowledge-bases/{id}/file-tree` | `documents:read` | `200 dto.FileTree` | 仅 File tree |
| `POST /knowledge-bases/{id}/file-tree/folders` | `documents:write` | `201 dto.FileTreeNode` | 同名、父节点和 KB lineage 校验 |
| `PATCH /knowledge-bases/{id}/file-tree/nodes/{node_id}` | `documents:write` | `204` | rename 同步 File Document title |
| `DELETE /knowledge-bases/{id}/file-tree/nodes/{node_id}` | `documents:write` | `204` | 非空目录 `409 file_tree_conflict` |
| `POST /knowledge-bases/{id}/documents/faq` | `documents:write` | `201 dto.FAQDocument` | Bearer `CreatedBy=nil` |
| `GET /knowledge-bases/{id}/documents/{document_id}/faq` | `documents:read` | `200 dto.FAQDocument` | URL KB 与 Document KB 必须一致 |
| `PUT /knowledge-bases/{id}/documents/{document_id}/faq` | `documents:write` | `202 dto.FAQDocument` | `base_revision_id` 必填，保留乐观并发 |
| `GET /knowledge-bases/{id}/documents/{document_id}/chunks` | `documents:read` | `200 dto.DocumentChunkPage` | 保留 `enabled/cursor/limit` |
| `GET /knowledge-bases/{id}/jobs` | `documents:read` | `200 dto.JobSummaryPage` | 保留 `document_id/status/cursor/limit` |
| `POST /knowledge-bases/{id}/documents/text` | `documents:write` | `201 service.IngestDocumentResult` | Markdown、非空、大小受配置限制 |
| `GET /models` | `knowledge_bases:write` | `200 []*dto.Model` | Bearer 强制 embedding + active + platform |

### 4.1 文本导入请求

```json
{
  "title": "排障手册：登录失败",
  "content": "# 登录失败\n\n## 原因\n…",
  "content_type": "markdown",
  "parent_node_id": "00000000-0000-0000-0000-000000000003"
}
```

handler 将请求转换为现有 `service.IngestDocumentInput`：

```go
service.IngestDocumentInput{
    WorkspaceID:     auth.WorkspaceID,
    KnowledgeBaseID: knowledgeBaseID,
    Title:           strings.TrimSpace(req.Title),
    FileName:        strings.TrimSpace(req.Title) + ".md",
    ContentType:     "text/markdown",
    SourceType:      "api",
    Reader:          strings.NewReader(req.Content),
    SizeBytes:       int64(len([]byte(req.Content))),
    Dedupe:          false,
    ParentNodeID:    req.ParentNodeID,
    NodeName:        strings.TrimSpace(req.Title),
}
```

`content_type` 省略或不是 `markdown` 都返回 `400 validation_error`；`content` 为空、title 为空、parent UUID 无效或内容字节数超过 `ingest.max_file_size_bytes` 同样返回 `400`。成功响应复用文件导入的 `document/job/deduped` 结构，异步状态初始为 processing/pending。

### 4.2 文档 kind 过滤

`GET /knowledge-bases/{id}/documents` 的 `kind` 缺省返回全部；只接受 `file`、`faq`、`web`。其它值不进入 service 或数据库，直接返回 `400 validation_error`。Repository 条件必须同时包含 `workspace_id`、`knowledge_base_id` 和 `deleted_at IS NULL`。

### 4.3 模型列表

Session 继续兼容现有 `type=embedding|rerank`、`active` 和管理目录参数。Bearer 请求必须满足：

```text
type=embedding&status=active&scope=platform
```

服务端强制构造 `ModelListFilter{Type: embedding, Status: active, Scope: platform}`；Bearer 传入 `type=rerank`、`status=disabled`、`scope=workspace`、`management=true` 或 `type=all` 均返回 `400 validation_error`。响应不返回 Provider config 或 credentials。

## 5. 分层改造合同

### 5.1 KnowledgeBase

`KnowledgeBaseService.List` 与 binder/repository 的 resolved list 增加可选 allowed IDs；Session 传 unrestricted，Bearer 传 `AuthContext.KnowledgeBaseIDs`。`Get` 与 `UpdateBasics` 保持 `WorkspaceID + KnowledgeBaseID` 显式 lineage；更新服务增加主体访问信息，Bearer 使用 scope + binding 判定，Session 使用既有 admin role 判定。

### 5.2 Document

`DocumentQueryService.List` 改为接收 `service.DocumentListFilter{WorkspaceID, KnowledgeBaseID, Kind}`。Document service 在进入 repository 前验证 workspace/KB，repository 将 kind 过滤下推 PostgreSQL。`Get`/`Delete` 已使用 `ResourceAccess`，行为保持不变。

### 5.3 FAQ、FileTree、Summary、Chunks、Jobs

FAQ `Get` 增加 `KnowledgeBaseID`；Update 已有该字段但 handler 必须从 URL 填充并强制校验。FileTree、Summary、Chunks 已携带 workspace+KB，补充 access 绑定判断。FAQ 的 `CreatedBy` 允许 nil，代表 API/system 主体；不能写入 `uuid.Nil` 作为用户。

### 5.4 Repository 与事务

不新增表和迁移。Repository 继续是 GORM 薄封装：所有查询带 workspace/KB 条件，跨表 FAQ 更新、文件树 rename 的事务内部只使用传入 tx。任何 `gorm.ErrRecordNotFound` 继续在 db 层映射为 `domainerrors.ErrNotFound`。

## 6. 错误合同

| HTTP | code | 场景 |
|---:|---|---|
| 400 | `validation_error` | 缺字段、非法 UUID、kind/type/status/scope 非法、Markdown 为空或超限 |
| 401 | `unauthorized` | Bearer 缺失、无效、过期或吊销 |
| 403 | `insufficient_scope` | 合法 API Key 缺少所需 scope |
| 403 | `forbidden` | Session member 执行仍限 admin/owner 的操作 |
| 404 | `not_found` | workspace/KB/document 越界、不存在或 FAQ URL lineage 不一致 |
| 409 | `file_tree_conflict` | 同名节点或删除非空目录 |

HTTP 层只返回稳定错误码，不暴露 Repository、SQL、API key 或文档完整内容。

## 7. 测试设计

### 7.1 单元与 HTTP 测试

- `api_scope_test.go`：新 scope 合法性、排序和重复去重。
- KnowledgeBase/Document/FAQ/Model service 表驱动测试：分别覆盖 Session unrestricted、Bearer allowed、Bearer 越界和 workspace 不匹配。
- handler 测试：路由参数、query/body 校验、scope 错误、错误码和 DTO 复用。

### 7.2 必须新增的 HTTP E2E

新增 `cmd/langhuan/jinshu_management_api_e2e_test.go`（`//go:build integration`），复用 `startV030E2E`、真实路由装配和现有 test support。测试在运行期只连接临时 PostgreSQL（pgvector/zhparser）和 Redis 容器，不能读取 `config.yaml` DSN。

E2E 至少覆盖以下完整链路：

1. Session 创建 workspace/KB 和 API Key；Key 绑定两个 KB，授予 read/write scopes。
2. Bearer 读取 KB 列表只得到绑定 KB；未绑定 KB 的 get/summary/list/file-tree/chunks/jobs 返回 `404`。
3. Bearer 更新绑定 KB 的名称/描述成功；无 `knowledge_bases:write` 返回 `403 insufficient_scope`。
4. Bearer 调用 text ingest：Markdown 成功返回 document/job；空内容、HTML、超限分别返回 `400 validation_error`；等待 worker 至少确认 Job 到达终态。
5. FAQ create/get/update 使用新的带 KB URL；`base_revision_id` 缺失或 URL KB 与 document KB 不一致返回稳定错误。
6. File-tree list/create/rename/delete 通过 Bearer 成功，非空目录删除返回 `409`，rename 后文档 title 同步。
7. `kind=file|faq|web` 过滤只返回对应类型；非法 kind 返回 `400`。
8. Bearer `/models?type=embedding&status=active&scope=platform` 返回可选模型；rerank/disabled/workspace/all 参数均拒绝。
9. Authorization 优先级：无效 Bearer 不回退有效 Session Cookie；吊销 API Key 后所有程序化接口返回 `401`。

## 8. 验收标准

- 所有开放接口 URL 都带 workspace；KnowledgeBase 子资源都带 KB ID，FAQ 旧无 KB 路径不存在。
- Session 既有行为不回归，Bearer 的 scope、绑定范围和稳定错误码全部通过测试。
- 文本导入复用现有异步流水线，无同步解析或重复数据库状态机。
- 文档 kind 过滤下推到 Repository，模型列表 Bearer 过滤由 service 强制执行。
- `go test ./...`、`go test -tags=integration ./...`、`go vet ./...`、`git diff --check` 通过；集成测试只使用临时容器。
