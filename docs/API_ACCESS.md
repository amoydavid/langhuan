# 琅嬛 程序化访问接入指南 (v0.6.0)

琅嬛 v0.6.0 起提供 **Workspace API Key**，让外部程序通过 REST 与 MCP 访问批准的能力，无需浏览器 Session。本文说明如何从全局服务地址派生端点、创建与使用 API Key。

## 1. 服务地址

所有公开地址都从全局 `server.base_url` 派生，该值在 `config.yaml` 中配置：

| 用途 | 地址 |
|---|---|
| Web Console | `${base_url}/` |
| REST API | `${base_url}/api/v1` |
| MCP | `${base_url}/mcp` |

**生产部署必须使用 HTTPS**（例如 `https://langhuan.example.com`），可含部署前缀（如 `https://example.com/langhuan`，此时 REST 为 `/langhuan/api/v1`、MCP 为 `/langhuan/mcp`）。不得包含 userinfo、query 或 fragment。

## 2. 创建 Workspace API Key

在 Web Console（owner/admin）的「API Key」页面创建：

- 绑定一个或多个知识库（创建后不可修改范围）。
- 选择 scope（无隐式继承）：`knowledge_bases:read`、`knowledge_bases:write`、`documents:read`、`documents:write`、`search:read`。
- 选择有效期：默认 90 天，或自定义 1..365 天，或不限期。

创建响应返回一次性明文：

```json
{
  "api_key": "lhk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "item": { "id": "uuid", "name": "检索 Agent", "token_prefix": "lhk_xxxxxxxx", "...": "..." },
  "base_url": "https://langhuan.example.com",
  "rest_base_url": "https://langhuan.example.com/api/v1",
  "mcp_url": "https://langhuan.example.com/mcp"
}
```

明文格式固定为 `lhk_` + 43 字符 Base64URL，总长 47 个 ASCII 字符。创建后可随时在详情页「重新获取」（reveal），不依赖创建时的唯一机会。

## 3. 使用 API Key

所有程序化请求都通过 TLS 上的 `Authorization: Bearer <api-key>` 头携带：

```bash
curl -X POST https://langhuan.example.com/api/v1/workspaces/acme/search \
  -H "Authorization: Bearer lhk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"knowledge_base_ids":["<kb-product>","<kb-faq>"],"query":"如何重置密码？"}'
```

**优先级规则**：存在 `Authorization` 头时，Bearer 是唯一权威凭证；无效 Bearer 直接返回 `401`，不回退浏览器 Cookie。API Key 不得出现在 URL、query 或 multipart 字段中。

## 4. REST 能力与 scope

| REST | Scope | 说明 |
|---|---|---|
| `POST /workspaces/:slug/knowledge-bases` | `knowledge_bases:write` | 创建知识库（新库原子加入 key 范围） |
| `GET /workspaces/:slug/knowledge-bases` | `knowledge_bases:read` | 列出 API Key 已绑定知识库 |
| `GET/PATCH /workspaces/:slug/knowledge-bases/:id` | `knowledge_bases:read/write` | 读取或更新已绑定知识库 |
| `GET /workspaces/:slug/knowledge-bases/:id/summary` | `knowledge_bases:read` | 知识库摘要 |
| `POST /workspaces/:slug/knowledge-bases/:id/documents` | `documents:write` | 导入文档 |
| `POST /workspaces/:slug/knowledge-bases/:id/documents/text` | `documents:write` | 导入 Markdown 文本 |
| `GET /workspaces/:slug/knowledge-bases/:id/documents?kind=file\|faq\|web` | `documents:read` | 按 kind 列出文档 |
| `GET/POST/PATCH/DELETE /workspaces/:slug/knowledge-bases/:id/file-tree/...` | `documents:read/write` | 文件树读写 |
| `POST /workspaces/:slug/knowledge-bases/:id/documents/faq` | `documents:write` | 创建 FAQ |
| `GET/PUT /workspaces/:slug/knowledge-bases/:id/documents/:document_id/faq` | `documents:read/write` | FAQ 读写；URL 必须携带 KB ID |
| `GET /workspaces/:slug/knowledge-bases/:id/documents/:document_id/chunks` | `documents:read` | 文档分块 |
| `GET /workspaces/:slug/models?type=embedding&status=active&scope=platform` | `knowledge_bases:write` | Bearer 仅可读取平台 active embedding 模型 |
| `GET /workspaces/:slug/documents/:id` | Session-only | 既有文档状态入口；不在本次 KB-qualified Bearer 迁移范围 |
| `GET /workspaces/:slug/jobs/:id` | Session-only | 既有 Job 状态入口；不在本次 KB-qualified Bearer 迁移范围 |
| `DELETE /workspaces/:slug/documents/:id` | Session-only | 既有软删除入口；不在本次 KB-qualified Bearer 迁移范围 |
| `GET /workspaces/:slug/knowledge-bases/:id/chunks/:chunk_id` | `documents:read` | 获取 Chunk |
| `POST /workspaces/:slug/knowledge-bases/:id/search` | `search:read` | 单库检索 |
| `POST /workspaces/:slug/search` | `search:read` | 多库检索（按 Embedding 模型分组） |

越界（跨 Workspace、未绑定知识库或其下资源）统一返回 `404`，不泄漏存在性。成员、邀请、Provider、设置、API Key 管理等路由仍为 Session-only；模型选择仅开放上一行所述的 Bearer 精确过滤合同。

FAQ 旧路径 `/workspaces/:slug/documents/:document_id/faq` 已移除；所有新增的知识库子资源 URL 均显式携带 `knowledge-bases/:id`。Bearer scope 不足返回 `403 insufficient_scope`，无效/过期/吊销凭证返回 `401 unauthorized`。

## 5. 分块与检索结果合同

创建知识库或创建 Index Generation 时，`chunking_config` 使用以下六个字段：

```json
{
  "strategy": "auto",
  "enable_parent_child": true,
  "parent_chunk_size": 4096,
  "child_chunk_size": 384,
  "chunk_size": 512,
  "chunk_overlap": 80
}
```

`strategy` 可为 `auto`、`heading`、`heuristic` 或 `recursive`。默认启用父子模式：`parent_chunk_size` 是返回完整上下文的父块上限，`child_chunk_size` 是检索子块上限，`chunk_overlap` 用于相邻父块上下文。关闭 `enable_parent_child` 后，仅产生扁平块，使用 `chunk_size` 和 `chunk_overlap`；不能产生没有父块的 child。

`GET .../chunks/:chunk_id` 的响应包含 `role` 与可选的 `parent_chunk_id`。角色语义如下：

- `parent`：完整上下文，只读，不进入向量或全文检索。
- `child`：必须带 `parent_chunk_id`，是父子模式的检索单元。
- `flat`：没有父块，关闭父子模式后的检索与返回单元。

搜索先召回 child/flat，再按父块聚合。父子模式结果的 `chunk_id`、`content` 和 `source_anchor` 指向父块，`matched_children` 列出实际参与召回的子块；flat 结果以自身作为主结果，并在 `matched_children` 中以 `role: "flat"` 表示。单知识库搜索直接返回结果数组；多知识库搜索在 `results` 数组中返回同一结构：

```json
{
  "chunk_id": "<parent-or-flat-chunk-id>",
  "content": "完整父块正文或 flat 正文",
  "score": 0.031,
  "rerank_score": 0.91,
  "ranking_stage": "rerank",
  "matched_children": [
    {
      "chunk_id": "<matched-child-or-flat-id>",
      "role": "child",
      "content": "实际命中的子块正文",
      "score": 0.031,
      "source_anchor": { "source_type": "markdown" }
    }
  ]
}
```

`score` 始终是 RRF 融合分数；`rerank_score` 仅在 Workspace Search Settings 启用 Rerank 且成功应用时出现；`ranking_stage` 为 `rrf`（未启用重排）、`rerank`（成功重排）或 `rrf_fallback`（重排远端失败后回退到 RRF 顺序）。多知识库可以混用不同 Embedding Generation，各库召回后统一使用 Workspace Search Settings 指定的 Rerank 模型。

Workspace 管理员可通过 `GET /api/v1/workspaces/:workspace_slug/search-settings` 查看默认策略，并通过 `PUT` 更新。PUT 仅接受 Session owner/admin；Bearer API Key 只能执行搜索。未配置策略时默认关闭 Rerank。

## 6. MCP

`/mcp` 只接受 `Authorization: Bearer <api-key>`，不接受浏览器 Cookie。MCP 客户端配置：

```json
{
  "mcpServers": {
    "langhuan": {
      "type": "http",
      "url": "https://langhuan.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${LANGHUAN_API_KEY}"
      }
    }
  }
}
```

六个工具：

| Tool | Scope | 行为 |
|---|---|---|
| `knowledge_base_create` | `knowledge_bases:write` | 创建知识库并原子加入 key 范围 |
| `document_ingest` | `documents:write` | Base64 内联导入（上限 8 MiB，超限请用 REST multipart） |
| `document_status` | `documents:read` | 文档与可选 Job 状态 |
| `knowledge_search` | `search:read` | 多库检索，结果带知识库来源 |
| `document_delete` | `documents:write` | 软删除文档 |
| `chunk_get` | `documents:read` | 获取 Chunk 内容与活跃修订 |

`tools/list` 只返回当前 key scope 允许的工具；scope 不足直接 call 仍被拒绝。

## 7. 到期、吊销与轮换

- 到期：默认 90 天，可选自定义天数或不限期（`expires_at=null`）。到期后下一次请求返回 `401`。
- 吊销：在 Web Console 吊销，**下一次请求**起失效；已进入服务的请求不会被强制中断。
- 重复吊销返回 `204`（幂等）。
- 轮换顺序：创建新 key → 更新并验证客户端 → 吊销旧 key。

## 8. 稳定错误码

| HTTP | Code | 场景 |
|---:|---|---|
| 400 | `validation_error` | 参数无效 |
| 401 | `unauthorized` | Bearer 缺失/无效/过期/吊销 |
| 403 | `forbidden` | member 访问管理 API |
| 403 | `insufficient_scope` | 合法 key 调用未授权操作 |
| 404 | `not_found` | Workspace/知识库/资源不存在或越界 |
| 409 | `api_key_limit_reached` | 活跃 key 达上限（100） |
| 500 | `api_key_secret_unavailable` | reveal 无法恢复 |
| 500 | `internal_error` | 其它未预期错误 |

MCP 业务错误以 `isError=true` 的结构化结果返回同一份 `{"error":{"code","message","retryable"}}`，不泄漏底层驱动或 Provider 细节。

## 8.1 模型连接与全局模型目录

模型连接管理接口：

- `GET /api/v1/admin/model-providers`：平台连接；`GET /api/v1/workspaces/:workspace_slug/model-providers`：当前工作区可见连接。
- Provider 响应包含 `capabilities`（由服务端 descriptor 生成）和 `model_counts.total/active/embedding/rerank`；不返回凭证明文。
- `GET .../model-providers/options` 返回 descriptor 能力及 `model_catalog` 标志；Provider key 与 capability 不由数据库枚举限制。
- `GET /api/v1/admin/models?type=all|embedding|rerank&status=all|active|disabled&scope=...&q=...` 返回平台模型目录。
- `GET /api/v1/workspaces/:workspace_slug/models?management=true&type=...&status=...&scope=...&q=...` 返回管理目录；不带 `management=true` 且使用 `type=embedding|rerank` 时仍是 Generation 的精确 selectable 合同。
- `GET /api/v1/admin/model-providers/:provider_id/model-catalog?type=embedding|rerank&q=...` 与 Workspace 同路径返回 Provider 上游模型目录；目录仅用于表单快速填充，不会自动创建 `models` 记录。无目录适配器或上游失败返回 `502 catalog_unavailable`，不会返回凭证或完整上游响应。
- `GET /api/v1/workspaces/:workspace_slug/model-providers/:provider_id/model-catalog?type=embedding|rerank&q=...` 遵循 Workspace 可见性规则；Provider 必须 active。

SiliconFlow 使用一个 `provider=siliconflow` 连接，同时承载 Embedding 与 Rerank 模型：默认路径为 `/v1/embeddings` 与 `/v1/rerank`，凭证只保存一份。

## 9. 安全须知

- 生产环境必须使用 HTTPS。
- API Key 明文只在创建/Reveal 响应中短暂出现；数据库只存 SHA-256 hash（鉴权）与 AES-256-GCM 密文（reveal）。普通请求绝不解密密文。
- 日志、错误、Toast、Query Cache 与常驻 DOM 永不包含完整 key、hash 或密文。
- 丢失 `credentials.encryption_key` 会导致历史 key 无法 reveal，但不影响 hash 鉴权。
