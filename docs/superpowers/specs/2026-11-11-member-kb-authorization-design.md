# 成员级知识库授权（KB 级可见性限制）设计规格

- 日期：2026-11-11
- 状态：已实现（2026-11-11；实现与 §3 决策一致，§2.4 缺口全部补齐并附测试；
  集成覆盖 PG+SQLite 双方言；e2e-web 全栈验证）
- 版本目标：v1.6.0（候选；v1.5.0 已被 MCP 写能力占用）
- 性质：领域值对象语义扩展 + 一张绑定表 + 一个 REST 端点 + 前端成员管理扩展。
  资源校验网络（`ResourceAccess`）已全线就位，绝大多数资源入口**零改动**。

## 1. 目标与问题

**真实问题**：琅嬛当前对人类用户的权限边界是 workspace 级——workspace 的任何
member 都能访问该 workspace 下**全部知识库与全部文档**。当 workspace 承载多个
知识域（产品手册 / 法务合同 / 会议纪要）且成员不应对等可见时，唯一的解法是拆
workspace，而拆分重复承担模型配置、检索设置、成员管理，成本远超需求本身。

**程序化主体无此问题**：API Key 的 KB 绑定集合（`workspace_api_key.KnowledgeBaseIDs`）
已上线且全链路强制。本规格把**同等粒度的可见性控制带给人类成员**。

**验收判据**：admin 把某 member 限定为「仅指定 2 个知识库」后——
1. 该 member 的 KB 列表、检索选择器只出现这 2 个库；
2. 直达 URL / 手打 ID 访问未授权库一律 404（不泄漏存在性）；
3. `kb_scope=all` 的存量成员与 admin/owner 行为零变化；
4. MCP 与 API Key 行为零变化。

## 2. 现状与已验证事实（探索结论，2026-11-11）

### 2.1 双主体模型

| 主体 | 凭证 | 资源边界 |
|---|---|---|
| Session 用户 | cookie（密码 / OIDC） | workspace 级：membership 角色（owner/admin/member）|
| API Key（含 MCP） | Bearer | workspace + KB 绑定集合 + 操作 scope |

### 2.2 协议中立抽象已存在

`value.ResourceAccess`（`internal/domain/value/auth_context.go:61`）：

```go
type ResourceAccess struct {
    WorkspaceID             uuid.UUID
    Unrestricted            bool        // Session 用户今天恒为 true
    AllowedKnowledgeBaseIDs []uuid.UUID // API Key 绑定集合
}
func (a ResourceAccess) AllowsKnowledgeBase(id uuid.UUID) bool
```

全部资源入口已经过它校验（middleware + service 双层），这是本规格成本可控的
根本原因。

### 2.3 已带 Access 校验的入口（零改动）

| 能力 | 证据 |
|---|---|
| KB list / get / summary | `service/knowledge_base.go:100,262-271`（list 按 allowedIDs 过滤）、`knowledge_base_summary.go:84-89` |
| 文档 get / delete / list（按 ID 寻址） | `service/document.go:44,93` |
| 文档 chunk 列表 | `service/document_chunks.go:51-83` |
| FAQ CRUD | `service/faq_document.go:38-106` |
| 文件树 | `service/file_tree.go:28-44` |
| Chunk revision | `service/chunk_revision.go:61-82`（`GetWithAccess`/`ListWithAccess`） |
| 多库检索 | `service/multi_knowledge_search.go:152-157` 逐 KB 校验 |
| 检索回放 | `http/search_handler.go:226` 传 `Access` |
| Job 状态 | 服务层已按绑定集合映射 404（`http/router.go:453-459` 注释与实现） |
| MCP 全部 24 工具 | `interfaces/mcp/*` 逐工具 `AllowsKnowledgeBase` |

### 2.4 缺 Access 校验的入口（本规格必须补齐）

| 能力 | 证据 | 说明 |
|---|---|---|
| 单库检索 | `service/search.go:27-33`（`SearchInput` 无 Access 字段）、`http/search_handler.go:89` | API Key 由 middleware `RequireKnowledgeBaseForAPIKey` 拦截；Session 用户今天恒 Unrestricted 所以无洞，但 member 受限后此路径必须补 |
| 文档资产（列表 / 内容） | `service/document_asset.go` 无 Access；路由 `http/router.go:317-318` 为 Session-only member 组 | 受限 member 可列出未授权库文档的资产 |
| Index generation（列表 / 创建 / 激活 / reindex） | `service/index_generation.go` 无 Access；`http/router.go:334-335,505-510` | 列表是 member 组 |
| KB source / web source（service 层） | 未在 grep 中确认带 Access | 路由层对 API Key 有 middleware 拦截；Session-only 路径需逐 route 审计补齐 |

> 实施第一步：**逐 route 审计**——凡 Session 可达且服务层未带 Access 的资源读/
> 写路径，统一补 `Access` 入参与 `AllowsKnowledgeBase`（或 `validateResourceAccess`）
> 校验，模式照抄 `document.go` / `multi_knowledge_search.go`。

### 2.5 关键表结构事实

- `workspace_memberships`（`000002_multi_tenant_auth.up.sql`）：`UNIQUE (workspace_id, user_id)`，**无** `UNIQUE (id, workspace_id)`——绑定表复合 FK 需先补该约束。
- `knowledge_bases` 已有 `UNIQUE (workspace_id, id)`（`000005_knowledge_retrieval_v2.up.sql:36`）——复合 FK 模式与 API Key 绑定表（`000010_workspace_api_keys.up.sql`）完全同构，可直接复用。
- 迁移双方言：PG 走 `migrations/`（下一个版本号 **000028**），SQLite 走 `migrations_sqlite/` 合并式终态 schema（下一个版本号 **000006**）。

## 3. 设计决策

### 3.1 语义模型（裁决记录）

**D1 授权载体**：membership 行新增 `kb_scope`（`all` | `restricted`），受限集合
存新表 `workspace_member_knowledge_bases`。

- `all` 为默认值：存量行自动 `all`，升级零行为变化；
- 空集合语义清晰：`restricted` + 0 行 = 看不到任何库（合法状态，UI 明示）；
- 与 `workspace_api_token_knowledge_bases` 同构，工程模式直接复用。

**D2 角色交互**：`restricted` **仅对 member 生效**；admin/owner 隐含全量
（管理面必须看到全部库）。`ResourceAccess()` 一处集中裁决。REST 层拒绝为
admin/owner 设置 restricted（400），杜绝「设置了但不生效」的困惑态。

**D3 语义开关唯一落点**：`AuthContext.ResourceAccess()`。Session 主体：
`role ≥ admin` 或 `kb_scope = all` → `Unrestricted`；否则返回授权集合。
所有已校验入口自动生效。

**D4 受限 member 建库**（`restricted` 下的自我一致性）：member 创建新 KB 时
**自动把新库写入其授权集合**（「我建的库我可见」）。否则受限 member 建库后
自己都看不到，语义荒谬。落点：`KnowledgeBaseService.Create` 受限路径同事务
追加 grant。

**D5 授权管理权限级**：admin+（与邀请管理同级，低于 changeRole 的 owner 级）——
授权是可见性管理，不是权力转移。Session-only，不进 progGroup（成员管理本就
不在程序化合同内）。

**D6 邀请接受**：新成员一律 `all`，入库后再由 admin 收紧。邀请表不加字段。

**D7 失败安全方向**：`AuthContext.KBScope` 零值（""）按 `all` 处理——旧会话 /
未迁移路径行为回退到现状，而不是把人锁死；显式 `restricted` 永远强制。

### 3.2 数据模型

**迁移 000028（PG）**：

```sql
-- up
ALTER TABLE workspace_memberships
    ADD COLUMN kb_scope text NOT NULL DEFAULT 'all',
    ADD CONSTRAINT workspace_memberships_kb_scope_check
        CHECK (kb_scope IN ('all','restricted'));

-- 绑定表复合 FK 的引用目标（对齐 000010 的 API Key 模式）
ALTER TABLE workspace_memberships
    ADD CONSTRAINT workspace_memberships_id_workspace_id_key UNIQUE (id, workspace_id);

CREATE TABLE workspace_member_knowledge_bases (
    membership_id     uuid NOT NULL,
    workspace_id      uuid NOT NULL,
    knowledge_base_id uuid NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (membership_id, knowledge_base_id),
    CONSTRAINT member_kbs_membership_fk FOREIGN KEY (membership_id, workspace_id)
        REFERENCES workspace_memberships(id, workspace_id) ON DELETE CASCADE,
    CONSTRAINT member_kbs_knowledge_base_fk FOREIGN KEY (workspace_id, knowledge_base_id)
        REFERENCES knowledge_bases(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX member_kbs_workspace_kb_idx
    ON workspace_member_knowledge_bases(workspace_id, knowledge_base_id, membership_id);
```

down 对称回滚（drop table / drop 两个约束）。

**SQLite（migrations_sqlite/000006_member_kb_grants）**：终态合并式——
`workspace_memberships` 加 `kb_scope TEXT NOT NULL DEFAULT 'all' CHECK (...)`；
绑定表 `membership_id TEXT`/`workspace_id TEXT`/`knowledge_base_id TEXT`，
FK 直指单列 PK（SQLite 惯例），workspace_id 列保留用于工作区级索引，
与 API Key 绑定表的 SQLite 版保持同构。

**领域层**：

- `value/kb_scope.go`：`KBScopeAll` / `KBScopeRestricted`、`IsValid()`、
  `Normalize()`（空串→all，落地 D7）。
- `model.Membership` 增加 `KBScope value.KBScope` 与 `KnowledgeBaseIDs []uuid.UUID`
  （聚合加载字段，非行内存储）。
- Repository：membership 读取路径（Get/List）联查或二段查询加载 scope + grants；
  新增 `ReplaceGrants(ctx, membershipID, workspaceID, ids)`（事务内 delete + insert），
  遵循薄封装约束。

### 3.3 唯一行为开关：`ResourceAccess()`

```go
func (a AuthContext) ResourceAccess() ResourceAccess {
    if a.IsAPIKey() {
        return ResourceAccess{WorkspaceID: a.WorkspaceID, AllowedKnowledgeBaseIDs: a.KnowledgeBaseIDs}
    }
    // Session：admin/owner 隐含全量；member 受 kb_scope 约束（D2/D3/D7）
    if a.Role.AtLeast(RoleAdmin) || !a.KBScope.Normalize().IsRestricted() {
        return ResourceAccess{WorkspaceID: a.WorkspaceID, Unrestricted: true}
    }
    return ResourceAccess{WorkspaceID: a.WorkspaceID, AllowedKnowledgeBaseIDs: a.KnowledgeBaseIDs}
}
```

### 3.4 数据装载链

```
SessionAuth(cookie) → RequireWorkspace → membershipResolver.Get(ws, user)
                                             │  dto.Membership 扩展：
                                             │  kb_scope + knowledge_base_ids
                                             ▼
                       authCtx.KBScope / authCtx.KnowledgeBaseIDs 填充
                                             ▼
                       此后所有 handler / service / MCP 走 ResourceAccess()
```

改动仅两处 gin 侧代码：`middleware.go` 的 `RequireWorkspace`（填充新字段）与
`MembershipService.Get`（返回扩展 DTO）。

## 4. REST API 合同

### 4.1 读取（既有端点扩展）

`GET /api/v1/workspaces/:workspace_slug/members` 与单成员查询的 DTO 增加：

```json
{
  "kb_scope": "restricted",
  "knowledge_base_ids": ["<uuid>", "<uuid>"]   // all 时恒为 []
}
```

成员数量为 workspace 内小集合，列表直接携带完整 ID 集合，前端弹窗免二次请求。

### 4.2 写入（新端点）

```
PUT /api/v1/workspaces/:workspace_slug/members/:user_id/kb-grants
Session-only；RequireWorkspaceRole(RoleAdmin)（D5）

body（二选一，互斥）：
  {"kb_scope": "all"}
  {"kb_scope": "restricted", "knowledge_base_ids": ["<uuid>", ...]}

200 → 返回扩展后的 membership DTO

错误映射：
  404 not_found        workspace 或目标成员不存在（统一体，不泄漏）
  400 validation_error kb_scope 非法 / ids 含跨 workspace 或不存在的库 /
                       目标为 admin·owner（D2）/ 两者字段同时出现
  403 forbidden        caller 角色不足 admin
```

服务用例 `MembershipService.SetKBGrants`：一个事务内更新 `kb_scope` +
`ReplaceGrants`（5.4 事务铁律，全程用 tx）。`all` 分支等价于清空绑定行并置
scope——幂等。

### 4.3 建 D4 的 service 落点

`KnowledgeBaseService.Create` 在 `access` 非 Unrestricted 时，创建成功后同事务
将新 KB ID 追加进创建者授权集合（需要 input 增带创建者 UserID）。

## 5. 前端设计（ASCII 原型）

改动集中在 `web/src/features/members/`；KB 列表/检索等页面零改动（后端过滤），
仅新增空态文案分支。KB 多选交互复用 `api-keys` 创建表单已有多选模式。

### 原型 1：成员列表（admin/owner 视角，新增列 + 行操作）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 成员                                                                          │
│ 管理工作区成员与他们的知识库访问范围                        [ + 邀请成员 ]      │
├──────────────────────────────────────────────────────────────────────────────┤
│ 用户                     角色         知识库范围      加入时间        操作     │
├──────────────────────────────────────────────────────────────────────────────┤
│ wei@langhuan.dev        owner       全部           2026-09-01     ⋮        │
│ 刘纬 (你)                                                                    │
│ agent@langhuan.dev      admin       全部           2026-09-03     ⋮        │
│ alice@corp.com          member      3 个知识库      2026-10-12     ⋮        │
│ bob@corp.com            member      全部           2026-10-20     ⋮        │
│ carol@corp.com          member      0 个知识库      2026-11-01     ⋮        │
└──────────────────────────────────────────────────────────────────────────────┘

member 行 ⋮ 菜单（admin/owner 行不出现「知识库授权」）：
            ┌────────────────────────┐
            │ 调整角色                │
            │ 知识库授权…      ← 新增 │
            │ ────────────────────── │
            │ 移除成员                │
            └────────────────────────┘
```

### 原型 2：知识库授权弹窗（radio + 多选列表）

```
┌────────────────────────────────────────────────────────────────────┐
│ 知识库授权                                                     ✕  │
│ 配置 alice@corp.com 可以访问哪些知识库                              │
├────────────────────────────────────────────────────────────────────┤
│ 访问范围                                                           │
│ (●) 全部知识库                                                     │
│     可访问工作区内当前与未来的所有知识库                             │
│ ( ) 仅指定知识库                                                    │
│     只能访问下方勾选的知识库                                        │
│                                                                    │
│ ── 选择「仅指定知识库」时展开 ────────────────────────────────────  │
│                                                    已选 2 / 5      │
│ ┌──────────────────────────────────────────────────────────────┐  │
│ │ [x] 产品手册                128 文档     上次同步 2 小时前     │  │
│ │ [x] 内部 FAQ                 42 文档     上次同步 3 天前      │  │
│ │ [ ] 研发 wiki               356 文档     同步中…              │  │
│ │ [ ] 法务合同库               18 文档     上次同步 1 个月前     │  │
│ │ [ ] 会议纪要                640 文档     上次同步 2 小时前     │  │
│ └──────────────────────────────────────────────────────────────┘  │
│ ⚠ 一个都不勾选 = 该成员将看不到任何知识库                            │
├────────────────────────────────────────────────────────────────────┤
│ 目标成员角色为 member 时才可保存（admin/owner 隐含全部）             │
│                                              [ 取消 ]  [ 保存 ]    │
└────────────────────────────────────────────────────────────────────┘
```

### 原型 3：受限 member 视角——知识库列表（列表接口已过滤，前端零逻辑）

```
┌──────────────────────────────────────────────────────────────────────┐
│ 知识库                                     [ 搜索… ]  [ + 新建知识库 ] │
├──────────────────────────────────────────────────────────────────────┤
│ 名称           文档数    状态     来源      更新时间                    │
│ ──────────────────────────────────────────────────────────────────── │
│ 产品手册        128      就绪    上传      2 小时前                    │
│ 内部 FAQ         42      就绪    上传      3 天前                      │
│                                                                      │
│   （研发 wiki / 法务合同库 / 会议纪要 不渲染——未授权，接口已过滤）      │
└──────────────────────────────────────────────────────────────────────┘

空态（restricted 且授权集合为空）：
┌──────────────────────────────────────────────────────────────────────┐
│                                                                      │
│                        还没有可访问的知识库                            │
│              请联系工作区管理员为你配置知识库访问范围                    │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### 原型 4：受限 member 视角——检索页知识库选择器

```
┌────────────────────────────────────────────────────────────────────┐
│ 检索                                                                │
│ 知识库范围  [ 产品手册 ✕ ] [ 内部 FAQ ✕ ] [ + ]  （上限 5 个）        │
│             ↑ 选择器只出现授权库；未授权库不可见亦不可选              │
│                                                                    │
│ 查询  [ 如何配置同步频率？                        ]   [ 检索 ]       │
├────────────────────────────────────────────────────────────────────┤
│ 结果                                                                │
│ 1. 产品手册 › 第 3 章 数据同步                            0.92      │
│ 2. 内部 FAQ › 同步频率说明                                0.88      │
└────────────────────────────────────────────────────────────────────┘

绕过 UI 手打未授权 kb_id 调 API：
  POST /workspaces/:slug/knowledge-bases/<未授权id>/search → 404 not_found
  （与「库不存在」同一响应体，不泄漏存在性——既有 404 语义自动继承）
```

### 强制链全景

```
请求 ─► SessionAuth(cookie) ─► RequireWorkspace ──────────► handler ─► service
                                    │                                       │
     membership.Get(ws,user) ───────┤                        ResourceAccess │
     role + kb_scope + grant_ids    │                 ┌────────────────────┤
                                    │                 │ role ≥ admin → Unrestricted
                                    │                 │ kb_scope=all  → Unrestricted
                                    │                 │ restricted    → AllowedIDs
                                    │                 └────────────────────┤
                                    │                                      ▼
                                    │                    AllowsKnowledgeBase(kbID)?
                                    │                      ├── 是 → 放行
                                    │                      └── 否 → ErrNotFound → 404
                                    │
MCP/API Key：APIKeyOnlyAuth（Authenticate 时已填绑定集合）→ 同一 ResourceAccess → 行为不变
```

## 6. MCP / API Key 影响面

**零改动**。API Key 主体在 `Authenticate` 时已填充 `KnowledgeBaseIDs`
（`service/api_key.go:325-331`），`ResourceAccess()` 的 API Key 分支原样保留；
本规格只改 Session 分支。MCP 门控（scope + 绑定集合）不受影响。
`workspace_api_token_knowledge_bases` 表与绑定管理 UI 不动。

## 7. 边界与安全考量

- **404 语义不变**：所有越界路径沿用统一 `not_found` 体，不泄漏 KB 存在性
  （`middleware.go`、`document.go:93`、`multi_knowledge_search.go:154-157` 既有约定）。
- **检索热路径不进入**：KB 过滤发生在检索入口（选择器 + service 校验），
  向量/FTS 召回与 RRF 融合零改动——**本规格不触发 `make eval` 回归义务**
  （不涉及 chunker / RRF / 默认检索参数 / embedding 链路）。
- **健康页 / insights**：insights 与健康信号是 admin+ 运营视角
  （`http/router.go:583-596`），不受成员授权影响，保持全库口径。
- **自我锁定**：admin/owner 隐含全量（D2），不存在把自己锁出管理面的路径；
  owner 转移等既有 lastOwnerConflict 防护不涉及。
- **KB 软删**：`knowledge_bases` 为软删（DeletedAt），授权集合中的死 ID 无副作用
  （列表天然不渲染软删库）；硬删由 FK CASCADE 清理。
- **日志**：不记录授权集合全文，仅记 `membership_id` / `user_id` / 目标 KB 数量。

## 8. 测试计划

1. **domain value 表驱动单测**：`KBScope` 归一/校验；`ResourceAccess()` 语义矩阵
   （主体 × 角色 × scope × grant 集合 → Unrestricted / IDs / 拒绝），含 D7 零值用例。
2. **service 集成测试**（临时 pgvector 容器，铁律 5.10）：grant 读写与事务性、
   受限 member 的 KB list 过滤、document get 越界 404、**单库检索越界 404**（§2.4 补点）、
   受限建库自动授权（D4）、admin/owner 全量回归。
3. **handler 测试**：路由权限矩阵（caller 角色 × 目标角色 × body 合法性 × 404 一致体）。
4. **SQLite 集成**：000006 迁移 up/down + 受限查询基线（dialect 分流不感知——上层无 SQL 差异）。
5. **e2e（web）**：全栈冒烟追加一条——admin 限制 member → member 登录只见授权库 →
   直达未授权库 URL 得 404。

## 9. 非目标

- **文档级 ACL**：检索召回以 chunk→document 进入打分路径，文档级过滤会进入热
  路径并与 hybrid top-k 语义耦合；「部分文档可见」类需求由检索期 metadata
  filter 承载（另立规格），不动权限模型。
- 邀请时预设授权（D6：入库后收紧）。
- KB 详情页「可访问成员」反向视图。
- 成员管理暴露到 MCP / API Key 合同。
- KB 之外的资源级授权（model provider、source connection 等保持 workspace 级）。

## 10. 验收标准（对照 §1 判据）

1. admin 将 alice 设为「仅指定 2 库」→ alice 的 KB 列表 / 检索选择器只见 2 库；
   直达 URL 与手打 ID 均得 404（响应体与「库不存在」一致）。
2. alice 受限期间新建的库自动出现在其列表中（D4）。
3. `kb_scope=all` 成员与全部 admin/owner 的行为与升级前完全一致（回归零变化）。
4. MCP 24 工具与全部 API Key 行为零变化（既有绑定表路径不动）。
5. §2.4 缺口清单全部补齐 Access 校验并有对应测试。
6. 干净环境（仅 docker）`go test ./...` 与 `make e2e-web` 通过；SQLite standalone
   模式功能等价。
7. `pnpm check` / `pnpm build` 通过；新增交互有 vitest 覆盖。
