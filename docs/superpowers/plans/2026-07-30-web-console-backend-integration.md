# 琅嬛 Web Console 真实后端对接 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把现有 shadcn-admin 模板改造成使用 HttpOnly session、真实 Workspace/知识库/文档/成员/邀请接口的琅嬛管理台，并把全部 REST 接口统一到 `/api/v1/*`。

**Architecture:** Go 端先整理当前过大的 Repository 文件，再以 Gin `/api/v1` group 统一 REST 命名空间，补齐 bootstrap、列表、成员摘要和邀请链接能力；`/mcp` 保持独立。React 端使用一个带 `withCredentials` 的 Axios client、TanStack Query 服务端状态、TanStack Router 文件路由以及 RHF + Zod 表单，认证布局统一复用现有 AppSidebar、固定 Header、移动 Sheet 和布局偏好。

**Tech Stack:** Go 1.26、Gin、GORM/PostgreSQL、Redis/asynq、React 19、TypeScript 7、Vite 8、TanStack Router/Query、Axios、React Hook Form、Zod、Tailwind CSS v4、shadcn/ui、Vitest Browser、Biome。

## Global Constraints

- 开始实现前使用 `superpowers:using-git-worktrees` 创建隔离 worktree；当前主工作树的 `AGENTS.md` 有用户未提交修改，不得带入功能提交。
- 所有行为改动严格执行 Red → Green → Refactor；纯文件拆分、生成文件和机械格式化是例外。
- 所有 REST JSON、multipart 与健康检查接口只允许位于 `/api/v1/*`；不保留 `/auth/*`、`/invitations/*`、`/admin/*`、`/healthz` 根路径兼容 handler。
- `/mcp` 与 `/mcp/*path` 保持独立协议入口，不挂载到 `/api/v1`，也不进入未来 SPA fallback。
- SPA 路由使用 `/workspaces/$workspaceSlug/...`，知识库页面使用短路径 `/workspaces/$workspaceSlug/kb/$kbId`；后端 API 继续使用 `/knowledge-bases`。
- 浏览器只使用 HttpOnly session Cookie；JavaScript、Zustand、localStorage、普通 Cookie、URL、日志和响应体均不得保存 session ID 或 access token。
- 服务端状态只由 TanStack Query 管理；表单统一使用 React Hook Form + Zod；组件不得直接调用裸 Axios/fetch。
- Workspace 资源 Query key 必须包含 `workspaceSlug`；后端 handler、service、repository 必须保留 Workspace UUID 隔离与跨租户统一 `404`。
- 复用现有 SidebarProvider/AppSidebar/NavGroup/NavUser/SidebarRail、折叠 Tooltip、collapsed Dropdown、移动端 Sheet、variant/collapsible Cookie 偏好；不重写 `components/ui/`。
- 已登录页面统一使用固定 Header：左侧 SidebarTrigger + 面包屑，右侧依次是弹性留白、搜索、主题、外观和用户按钮，搜索不得回到左侧。
- 当前 v0.3.0 实际支持 `.md`、`.markdown`、`.txt`、`.csv`、`.xlsx`、`.docx`；PDF 和未知格式必须展示 `415 unsupported_file_type`，不得写入 raw storage/Document/Job。
- 当前文档处理已生成 normalized Markdown、version 1 parse manifest 和 chunks；不得恢复 stub parser，也不得改变 `processing_version=1` 的去重条件、worker payload 或状态机。
- 前端不得新增 `any`，不得手改 `web/src/routeTree.gen.ts`，不得重新引入 ESLint/Prettier；路由树只能由 TanStack Router 插件生成。
- 所有页面提供明确 loading、error、empty 与 disabled 状态；移动端资源表格必须转换为卡片，不依赖横向滚动完成主要操作。
- 界面使用简体中文、深墨蓝/中性色和明确语义色；不引入紫色 AI 渐变或无真实数据来源的统计卡片。
- 当前验证基线：`go test ./... -count=1` 与 `pnpm --dir web build` 通过；`pnpm --dir web test` 需先安装 Playwright Chromium，当前失败原因为浏览器二进制缺失而非测试断言失败。
- 本计划不实现检索台、Workspace API Token、MCP 业务工具、PDF/MinerU、静态资源 `go:embed` 或 SPA server fallback；这些仍按 ROADMAP 后续版本交付。

## Current Update Impact

`39fe8c8` 之后合入的 v0.3.0 多格式解析改变了文档相关实施细节，但不改变已批准的路由、认证、侧边栏和权限设计：

- 上传 UI 必须显示当前五类解析器对应的六个扩展名，并在前端与后端都保留 `415` 错误语义；不能再暗示 PDF 可用。
- Document 详情仍读取现有 DTO 的 `normalized_markdown`，处理完成意味着真实 parse manifest/chunks 已持久化；本次不额外暴露 parse manifest/chunk 浏览 API。
- 文档列表和详情必须保留 `file_type`、`content_type`、`processing_version` 相关持久化语义，不得绕开现有 `DocumentRepository` codec。
- 新增列表方法时要在当前 v0.3.0 schema 上查询，不新增数据库迁移。
- `internal/infrastructure/db/repository.go`（648 行）和 `auth_repository.go`（553 行）已混合多个 Repository；因为本计划必须修改其中的 KB、Document、User、Membership、Invitation，实现行为前先做纯机械职责拆分。
- `ROADMAP.md` 与 `docs/ARCHITECTURE.md` 仍保留根路径 `/auth/*`，ROADMAP 还把 member 描述成只读；最终文档任务必须改成 `/api/v1/*` 和当前后端真实的 member+ 创建 KB/上传权限，同时明确检索台不在本计划内。

## File Responsibility Map

### Backend

- `internal/infrastructure/db/*_repository.go`：每个文件只保存一个 Repository 的类型、构造函数、方法和专属 `toRow/fromRow`。
- `internal/infrastructure/db/repository_errors.go`：保存 `ErrRepositoryNotFound` 与多个 Repository 共用的数据库错误映射。
- `internal/application/service/user.go`、`knowledge_base.go`、`document.go`、`invitation.go`、`membership.go`：用例编排、列表排序、状态计算和批量用户摘要装配。
- `internal/application/dto/invitation.go`、`membership.go`：邀请管理视图、邀请状态和成员 `user` 摘要。
- `internal/interfaces/http/router.go`：唯一 REST/MCP 路由装配点与 API JSON 404 边界。
- `internal/interfaces/http/auth_handler.go`、`knowledge_base.go`、`document.go`、`invitation_handler.go`、`membership_handler.go`：协议解析与 DTO 返回，不直接访问数据库。
- `internal/infrastructure/config/config.go`：`server.public_base_url` 的加载、规范化与校验。
- `cmd/langhuan/main.go`：构造函数依赖与 HTTP config 接线。

### Frontend

- `web/src/lib/api/client.ts`：唯一 Axios 实例与 `/api/v1` base URL 约束。
- `web/src/lib/api/error.ts`：把后端 `{error:{code,message}}` 收窄为 `ApiError`。
- `web/src/lib/query-client.ts`：QueryClient 默认策略和并发 `401` 单次导航协调。
- 各 `web/src/features/<domain>/api.ts`、`queries.ts`、`schemas.ts`、`types.ts`：对应业务域的协议、Query options、mutation 和表单 schema。
- `web/src/components/layout/*`：唯一 AppShell、固定 Header、动态面包屑、WorkspaceSwitcher、NavGroup、NavUser。
- `web/src/routes/**`：只连接 route params/search 与 feature 页面；不直接发请求。
- `web/src/routeTree.gen.ts`：构建时生成并提交，禁止手工编辑。

---

### Task 1: 机械拆分过大的 Repository 文件

**Files:**
- Create: `internal/infrastructure/db/repository_errors.go`
- Create: `internal/infrastructure/db/workspace_repository.go`
- Create: `internal/infrastructure/db/knowledge_base_repository.go`
- Create: `internal/infrastructure/db/document_repository.go`
- Create: `internal/infrastructure/db/chunk_repository.go`
- Create: `internal/infrastructure/db/job_repository.go`
- Create: `internal/infrastructure/db/user_repository.go`
- Create: `internal/infrastructure/db/session_repository.go`
- Create: `internal/infrastructure/db/membership_repository.go`
- Create: `internal/infrastructure/db/invitation_repository.go`
- Delete: `internal/infrastructure/db/repository.go`
- Delete: `internal/infrastructure/db/auth_repository.go`
- Split: `internal/infrastructure/db/repository_test.go`
- Split: `internal/infrastructure/db/repository_integration_test.go`

**Interfaces:**
- Consumes: 当前全部 Repository 公共类型和方法签名。
- Produces: 完全相同的导出类型、构造函数、方法与错误语义；后续任务只在对应职责文件追加行为。

- [ ] **Step 1: 记录拆分前行为基线**

Run:

```bash
go test ./internal/infrastructure/db ./internal/application/service ./internal/interfaces/http -count=1
```

Expected: PASS；这是纯结构调整的行为基线。

- [ ] **Step 2: 按类型原样移动生产代码**

使用 `apply_patch` 按下表移动符号，函数体、错误文本、查询条件和事务边界一字不改：

```text
repository_errors.go         ErrRepositoryNotFound, translateDBError
workspace_repository.go      WorkspaceRepository, 构造函数, 全部方法, workspaceToRow/fromRow
knowledge_base_repository.go KnowledgeBaseRepository, 构造函数, 全部方法, knowledgeBaseToRow/fromRow, intFromJSON
document_repository.go       DocumentRepository, 构造函数, 全部方法, documentToRow/fromRow
chunk_repository.go          ChunkRepository, 构造函数, 全部方法, chunkToRow/fromRow
job_repository.go            JobRepository, 构造函数, 全部方法, jobToRow/fromRow
user_repository.go           UserRepository, 构造函数, 全部方法, userToRow/fromRow
session_repository.go        SessionRepository, 构造函数, 全部方法, sessionToRow/fromRow
membership_repository.go     MembershipRepository, 构造函数, 全部方法, membershipToRow/fromRow
invitation_repository.go     InvitationRepository, 构造函数, 全部方法, invitationToRow/fromRow
```

保留现有编译期接口断言，移动到对应 Repository 文件；不要借机修改重复调用、注释、排序或错误消息。

- [ ] **Step 3: 按被测职责拆分测试文件**

把现有测试按函数名前缀机械移动到对应 `*_repository_test.go` 与 `*_repository_integration_test.go`；跨资源 `TestV021AuthFlow` 和 parsing flow 保留在 `repository_flow_test.go`。不改断言。

- [ ] **Step 4: 格式化并证明行为未变**

Run:

```bash
gofmt -w internal/infrastructure/db
go test ./internal/infrastructure/db ./internal/application/service ./internal/interfaces/http -count=1
git diff --check
```

Expected: PASS，且 diff 只有代码移动/import 调整。

- [ ] **Step 5: Commit**

```bash
git add internal/infrastructure/db
git commit -m "refactor(db): 按职责拆分 repository 文件"
```

---

### Task 2: 统一 REST `/api/v1` 命名空间并保护 `/mcp`

**Files:**
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/router_test.go`
- Modify: `internal/interfaces/http/auth_handler_test.go`
- Modify: `internal/interfaces/http/invitation_handler_test.go`
- Modify: `internal/interfaces/http/user_handler_test.go`
- Modify: `internal/interfaces/http/auth_handler.go`
- Modify: `internal/application/dto/invitation.go`
- Modify: `internal/application/service/membership.go`
- Modify: `internal/application/service/user.go`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- Consumes: 当前 `Dependencies` 与全部 handler/middleware。
- Produces: `GET /api/v1/healthz`、`/api/v1/auth/*`、`/api/v1/invitations/*`、`/api/v1/admin/*`、`/api/v1/workspaces/*`；`/mcp` 不变。

- [ ] **Step 1: 写失败的命名空间测试**

在 `router_test.go` 增加完整依赖 router 的路由枚举和 JSON 404 测试：

```go
func TestAllRESTRoutesUseAPIV1(t *testing.T) {
    router := newFullyWiredTestRouter(t)
    for _, route := range router.Routes() {
        if strings.HasPrefix(route.Path, "/mcp") {
            continue
        }
        if !strings.HasPrefix(route.Path, "/api/v1/") {
            t.Errorf("REST route escaped /api/v1: %s %s", route.Method, route.Path)
        }
    }
}

func TestUnknownAPIRouteReturnsJSON404(t *testing.T) {
    router := NewRouter(Dependencies{})
    rec := httptest.NewRecorder()
    router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil))
    if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
        t.Fatalf("status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
    }
}
```

把现有健康、登录、登出、me、注册、公开邀请和密码重置测试的目标路径改为 `/api/v1/...`，并新增表驱动测试确认历史根路径不再命中 API handler。

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/interfaces/http -run 'Test(AllRESTRoutesUseAPIV1|UnknownAPIRouteReturnsJSON404|Healthz|Auth|Invitation|AdminPassword)' -count=1
```

Expected: FAIL；当前 `/healthz`、`/auth/*`、公开邀请和密码重置仍在根路径。

- [ ] **Step 3: 最小化重组 Gin routes**

在 `NewRouter` 中先创建 `api := router.Group("/api/v1")`，之后公开、认证、platform admin 与 workspace groups 都从 `api` 派生；MCP 仍直接注册到 `router`：

```go
api := router.Group("/api/v1")
api.GET("/healthz", healthz)

router.Any("/mcp", gin.WrapH(deps.MCPHandler))
router.Any("/mcp/*path", gin.WrapH(deps.MCPHandler))

router.NoRoute(func(c *gin.Context) {
    if c.Request.URL.Path == "/api/v1" || strings.HasPrefix(c.Request.URL.Path, "/api/v1/") {
        writeError(c, http.StatusNotFound, "not_found", "接口不存在")
        return
    }
    c.Status(http.StatusNotFound)
})
```

所有子 route 传相对路径，例如 `api.POST("/auth/login", ...)`、`api.Group("/workspaces/:workspace_slug")`；不注册兼容别名。

同步修改 handler/service/DTO/main 中的路由注释。运行 `rg -n '/auth/|GET /invitations|POST /admin|/healthz' internal cmd --glob '*.go'`，结果只能是测试旧路由不再注册的断言或带 `/api/v1` 的完整路径。

- [ ] **Step 4: 验证 API 与 MCP 边界**

Run:

```bash
gofmt -w internal/interfaces/http internal/application/dto/invitation.go internal/application/service/membership.go internal/application/service/user.go cmd/langhuan/main.go
go test ./internal/interfaces/http ./internal/application/service ./cmd/langhuan -count=1
go test ./internal/interfaces/mcp -count=1
```

Expected: PASS；`/api/v1/unknown` 是 JSON 404，`/mcp` 测试仍通过。

- [ ] **Step 5: Commit**

```bash
git add internal/interfaces/http internal/application/dto/invitation.go internal/application/service/membership.go internal/application/service/user.go cmd/langhuan/main.go
git commit -m "refactor(http): 统一 REST API v1 路由"
```

---

### Task 3: 提供公开 bootstrap status

**Files:**
- Modify: `internal/application/service/user.go`
- Modify: `internal/application/service/user_test.go`
- Modify: `internal/interfaces/http/auth_handler.go`
- Modify: `internal/interfaces/http/auth_handler_test.go`
- Modify: `internal/interfaces/http/router.go`

**Interfaces:**
- Consumes: `UserRepository.Count(ctx)`。
- Produces: `UserService.IsInitialized(ctx) (bool, error)` 与公开 `GET /api/v1/auth/bootstrap-status -> {"initialized":boolean}`。

- [ ] **Step 1: 写 service 与 handler 失败测试**

```go
func TestUserServiceIsInitialized(t *testing.T) {
    repo := &fakeUserRepository{count: 1}
    svc := NewUserService(repo, fakeHasher{})
    initialized, err := svc.IsInitialized(context.Background())
    if err != nil || !initialized {
        t.Fatalf("initialized=%v err=%v", initialized, err)
    }
}
```

handler 测试覆盖零用户、已有用户、Count 错误映射、无 Cookie 可访问，并断言响应不含 count/email。

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/application/service ./internal/interfaces/http -run 'Test(UserServiceIsInitialized|BootstrapStatus)' -count=1
```

Expected: FAIL，方法和路由尚不存在。

- [ ] **Step 3: 实现最小用例与 handler**

```go
func (s *UserService) IsInitialized(ctx context.Context) (bool, error) {
    count, err := s.repo.Count(ctx)
    if err != nil {
        return false, fmt.Errorf("统计用户失败: %w", err)
    }
    return count > 0, nil
}

func (h authHandler) bootstrapStatus(c *gin.Context) {
    initialized, err := h.users.IsInitialized(c.Request.Context())
    if err != nil {
        writeServiceError(c, err)
        return
    }
    c.JSON(http.StatusOK, gin.H{"initialized": initialized})
}
```

把方法加入 handler-side `UserService` interface，并在任何 session middleware 之前注册公开路由。

- [ ] **Step 4: 验证并 Commit**

Run:

```bash
gofmt -w internal/application/service/user.go internal/interfaces/http
go test ./internal/application/service ./internal/interfaces/http -count=1
```

Expected: PASS。

```bash
git add internal/application/service/user.go internal/application/service/user_test.go internal/interfaces/http
git commit -m "feat(auth): 添加初始化状态接口"
```

---

### Task 4: 生成代理安全的邀请公开 URL

**Files:**
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/config_test.go`
- Modify: `config.example.yaml`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/invitation_handler.go`
- Modify: `internal/interfaces/http/invitation_handler_test.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `cmd/langhuan/main_test.go`

**Interfaces:**
- Consumes: 请求的真实 TLS 状态与 Host，不信任未配置的 `X-Forwarded-*`。
- Produces: `ServerConfig.PublicBaseURL string`、`Dependencies.PublicBaseURL string` 和稳定的 `<base>/invitations/<token>`。

- [ ] **Step 1: 写失败的配置与 URL 测试**

覆盖：空配置允许；`https://langhuan.example.com/` 规范化为无尾斜线；拒绝相对 URL、`ftp`、userinfo、query、fragment；显式 base 优先；普通请求生成 `http://`；TLS 请求生成 `https://`；恶意 `X-Forwarded-Proto` 不生效。

```go
func TestInvitationURLUsesPlainHTTPRequestScheme(t *testing.T) {
    req := httptest.NewRequest(http.MethodPost, "http://localhost:5173/api/v1/workspaces/acme/invitations", body)
    req.Host = "localhost:5173"
    got := createInvitation(t, NewRouter(deps), req).InviteURL
    if !strings.HasPrefix(got, "http://localhost:5173/invitations/") {
        t.Fatalf("invite_url=%q", got)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/infrastructure/config ./internal/interfaces/http ./cmd/langhuan -run 'Test.*PublicBaseURL|TestInvitationURL' -count=1
```

Expected: FAIL；当前固定使用 `https://<Host>`。

- [ ] **Step 3: 实现配置规范化与 URL helper**

`ServerConfig` 增加字段：

```go
PublicBaseURL string `yaml:"public_base_url"`
```

config validation 用 `net/url` 校验绝对 `http/https`、Host 非空且无 userinfo/query/fragment，并用 `strings.TrimRight(raw, "/")` 保存规范值。邀请 handler 使用：

```go
func (h invitationHandler) inviteURL(c *gin.Context, token string) string {
    base := h.publicBaseURL
    if base == "" {
        scheme := "http"
        if c.Request.TLS != nil {
            scheme = "https"
        }
        base = scheme + "://" + c.Request.Host
    }
    return strings.TrimRight(base, "/") + "/invitations/" + url.PathEscape(token)
}
```

通过 `runtimeServices`/`Dependencies` 把 `cfg.Server.PublicBaseURL` 注入 handler；不要读取环境变量或 `X-Forwarded-*`。

- [ ] **Step 4: 更新示例配置并验证**

```yaml
server:
  public_base_url: "" # 生产建议显式设置，例如 https://langhuan.example.com
```

Run:

```bash
gofmt -w internal/infrastructure/config internal/interfaces/http cmd/langhuan
go test ./internal/infrastructure/config ./internal/interfaces/http ./cmd/langhuan -count=1
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add config.example.yaml internal/infrastructure/config internal/interfaces/http cmd/langhuan
git commit -m "fix(invitation): 生成可配置的公开邀请地址"
```

---

### Task 5: 增加 KB 与文档列表接口

**Files:**
- Modify: `internal/infrastructure/db/knowledge_base_repository.go`
- Modify: `internal/infrastructure/db/knowledge_base_repository_test.go`
- Modify: `internal/infrastructure/db/knowledge_base_repository_integration_test.go`
- Modify: `internal/infrastructure/db/document_repository.go`
- Modify: `internal/infrastructure/db/document_repository_test.go`
- Modify: `internal/infrastructure/db/document_repository_integration_test.go`
- Modify: `internal/application/service/knowledge_base.go`
- Modify: `internal/application/service/knowledge_base_test.go`
- Modify: `internal/application/service/document.go`
- Modify: `internal/application/service/document_test.go`
- Modify: `internal/interfaces/http/knowledge_base.go`
- Modify: `internal/interfaces/http/document.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/router_test.go`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- Produces: `KnowledgeBaseService.List(ctx, workspaceID) ([]*dto.KnowledgeBase, error)`。
- Produces: `DocumentService.List(ctx, workspaceID, knowledgeBaseID) ([]*dto.Document, error)`。
- Produces: `GET /api/v1/workspaces/:workspace_slug/knowledge-bases` 与 `GET .../knowledge-bases/:id/documents`。

- [ ] **Step 1: 写 Repository 排序与隔离失败测试**

真实 PostgreSQL integration fixtures 创建两个 Workspace、两个 KB 和交错时间的 Documents，断言：

```go
list, err := kbRepo.List(ctx, workspaceA)
// only workspaceA; ORDER BY created_at DESC, id DESC

docs, err := docRepo.List(ctx, workspaceA, kbA)
// only kbA and workspaceA; preserve v0.3 Document codec fields
```

覆盖 KB 不属于 Workspace 时 service 返回 `ErrNotFound`，即使该 KB 没有文档也不能错误返回空数组。

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
go test ./internal/infrastructure/db ./internal/application/service -run 'Test(KnowledgeBase|Document).*List' -count=1
```

Expected: FAIL，List 方法尚不存在。

- [ ] **Step 3: 实现薄 Repository 与 service 编排**

接口签名：

```go
List(ctx context.Context, workspaceID uuid.UUID) ([]*model.KnowledgeBase, error)
List(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) ([]*model.Document, error)
```

查询必须 `WithContext(ctx)`、显式 workspace 条件，并使用 `Order("created_at DESC, id DESC")`。`DocumentService` 同时注入 `KnowledgeBaseReader`，先 `Get(workspaceID, knowledgeBaseID)` 确认归属，再查询文档；Document repository 仍用 join 带 workspace 条件，形成纵深隔离。

- [ ] **Step 4: 写 handler 失败测试并接入路由**

扩展 handler-side interfaces，新增 `list` handlers；无数据返回 `[]` 而非 `null`。用 member session 测试 `200`、稳定数组、跨 Workspace `404` 和无 Cookie `401`。

Run:

```bash
go test ./internal/interfaces/http -run 'Test(ListWorkspaceKnowledgeBases|ListKnowledgeBaseDocuments)' -count=1
```

Expected before route implementation: FAIL；实现 route 后 PASS。

- [ ] **Step 5: 更新 runtime 接线并运行相关套件**

```go
documents := service.NewDocumentService(documentRepo, kbRepo)
```

Run:

```bash
gofmt -w internal/infrastructure/db internal/application/service internal/interfaces/http cmd/langhuan
go test ./internal/infrastructure/db ./internal/application/service ./internal/interfaces/http ./cmd/langhuan -count=1
```

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/infrastructure/db internal/application/service internal/interfaces/http cmd/langhuan
git commit -m "feat(api): 添加知识库与文档列表"
```

---

### Task 6: 增加邀请列表与成员用户摘要

**Files:**
- Modify: `internal/infrastructure/db/invitation_repository.go`
- Modify: `internal/infrastructure/db/invitation_repository_integration_test.go`
- Modify: `internal/infrastructure/db/user_repository.go`
- Modify: `internal/infrastructure/db/user_repository_integration_test.go`
- Modify: `internal/application/dto/invitation.go`
- Modify: `internal/application/dto/membership.go`
- Modify: `internal/application/service/invitation.go`
- Modify: `internal/application/service/invitation_test.go`
- Modify: `internal/application/service/membership.go`
- Modify: `internal/application/service/membership_test.go`
- Modify: `internal/interfaces/http/invitation_handler.go`
- Modify: `internal/interfaces/http/invitation_handler_test.go`
- Modify: `internal/interfaces/http/membership_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- Produces: `InvitationService.List(ctx, workspaceID, actorRole) ([]*dto.InvitationListItem, error)`。
- Produces: `MembershipUserRepository.ListByIDs(ctx, ids) ([]*model.User, error)`。
- Produces: `dto.Membership.User *dto.MembershipUserSummary` 与 `GET /api/v1/workspaces/:slug/invitations`。

- [ ] **Step 1: 写邀请状态/排序失败测试**

定义稳定状态：

```go
type InvitationStatus string

const (
    InvitationStatusPending  InvitationStatus = "pending"
    InvitationStatusAccepted InvitationStatus = "accepted"
    InvitationStatusExpired  InvitationStatus = "expired"
    InvitationStatusRevoked  InvitationStatus = "revoked"
)
```

测试 precedence 为 accepted → revoked → expired → pending，输出 pending 优先，其余和同组按 `created_at DESC, id DESC`；member 调用返回 `ErrForbidden`；DTO 不包含 `TokenHash` 或明文 token。

- [ ] **Step 2: 写成员批量富化失败测试**

fake user repository 记录调用次数与 ID：

```go
members, err := svc.List(ctx, workspaceID)
if err != nil || users.listByIDsCalls != 1 {
    t.Fatalf("members=%v calls=%d err=%v", members, users.listByIDsCalls, err)
}
if members[0].User.Email != "a@example.com" || members[0].User.Nickname != "张三" {
    t.Fatalf("user=%+v", members[0].User)
}
```

同时断言序列化结果没有 `password_hash`、last login、session 字段。

- [ ] **Step 3: 运行测试确认失败**

Run:

```bash
go test ./internal/application/service ./internal/infrastructure/db -run 'Test(InvitationList|MembershipListIncludesUsers|UserRepositoryListByIDs)' -count=1
```

Expected: FAIL。

- [ ] **Step 4: 实现 Repository、DTO 与 service**

`InvitationRepository.ListByWorkspace` 用 `WithContext` + `workspace_id`，先按 `created_at DESC, id DESC` 取完整早期规模数组；application service 使用 `now func() time.Time`（生产为 `time.Now`）计算状态并稳定分组。空列表必须序列化为 `[]`。`UserRepository.ListByIDs` 使用单次 `WHERE id IN ?`，空 ID 直接返回空 slice。

`InvitationService` 增加未导出的 `now func() time.Time` 字段，`NewInvitationService` 固定设置为 `time.Now`；同 package 测试覆盖固定时间，不新增生产构造参数。管理 DTO 精确为：

```go
type InvitationListItem struct {
    ID           uuid.UUID        `json:"id"`
    WorkspaceID  uuid.UUID        `json:"workspace_id"`
    InvitedEmail string           `json:"invited_email"`
    Role         value.WorkspaceRole `json:"role"`
    TokenPrefix  string           `json:"token_prefix"`
    Status       InvitationStatus `json:"status"`
    ExpiresAt    time.Time        `json:"expires_at"`
    AcceptedAt   *time.Time       `json:"accepted_at"`
    RevokedAt    *time.Time       `json:"revoked_at"`
    CreatedBy    uuid.UUID        `json:"created_by"`
    CreatedAt    time.Time        `json:"created_at"`
}

type MembershipUserSummary struct {
    Email    string `json:"email"`
    Nickname string `json:"nickname"`
}

type Membership struct {
    ID          uuid.UUID              `json:"id"`
    WorkspaceID uuid.UUID              `json:"workspace_id"`
    UserID      uuid.UUID              `json:"user_id"`
    Role        value.WorkspaceRole    `json:"role"`
    User        *MembershipUserSummary `json:"user"`
    CreatedAt   time.Time              `json:"created_at"`
    UpdatedAt   time.Time              `json:"updated_at"`
}
```

`MembershipService` 构造函数改为：

```go
func NewMembershipService(repo MembershipRepository, users MembershipUserRepository) *MembershipService
```

只有 workspace `List` 批量富化；`ListForUser` 保持现有轻量路径，避免 `/auth/me` 做无用用户查询。

- [ ] **Step 5: 接入 handler、路由和 runtime**

邀请 list handler 从 `AuthContext` 传 `WorkspaceID` 与 `Role`；路由放在 admin+ group。更新 `cmd/langhuan/main.go` 为 `service.NewMembershipService(membershipRepo, userRepo)`。

Run:

```bash
gofmt -w internal/infrastructure/db internal/application internal/interfaces/http cmd/langhuan
go test ./internal/infrastructure/db ./internal/application/service ./internal/interfaces/http ./cmd/langhuan -count=1
```

Expected: PASS，并且成员 handler 测试只看到非敏感 `user` 摘要。

- [ ] **Step 6: Commit**

```bash
git add internal/infrastructure/db internal/application internal/interfaces/http cmd/langhuan
git commit -m "feat(api): 添加邀请列表与成员摘要"
```

---

### Task 7: 建立前端 API、Query 与质量基础设施

**Files:**
- Create: `web/src/lib/api/client.ts`
- Create: `web/src/lib/api/client.test.ts`
- Create: `web/src/lib/api/error.ts`
- Create: `web/src/lib/api/error.test.ts`
- Create: `web/src/lib/query-client.ts`
- Create: `web/src/lib/query-client.test.ts`
- Modify: `web/src/main.tsx`
- Modify: `web/vite.config.ts`
- Modify: `web/package.json`
- Modify: `web/pnpm-lock.yaml`

**Interfaces:**
- Produces: `apiClient: AxiosInstance`、`ApiError`、`parseApiError(error: unknown): ApiError`、`queryClient`。
- Produces: `setUnauthorizedHandler(handler)`、`handleUnauthorizedOnce()`、`resetUnauthorizedNavigation()`，由 `main.tsx` 在 router 创建后接线，避免 query-client/router 循环依赖。
- Produces: `VITE_API_BASE_URL` 默认 `/api/v1`，`VITE_DEV_PROXY_TARGET` 默认 `http://127.0.0.1:8080`。

- [ ] **Step 1: 补齐本机浏览器测试依赖**

Run:

```bash
pnpm --dir web exec playwright install chromium
pnpm --dir web test
```

Expected: Chromium 安装成功；现有 21 个测试文件开始实际执行。若出现现有断言失败，先记录并修复基线，不把浏览器缺失误报成代码失败。

- [ ] **Step 2: 写 API client 与错误解析失败测试**

```ts
expect(apiClient.defaults.baseURL).toBe('/api/v1')
expect(apiClient.defaults.withCredentials).toBe(true)
expect(() => resolveApiBaseURL('/api')).toThrow()

const error = parseApiError(
  new AxiosError('', 'ERR_BAD_REQUEST', undefined, undefined, {
    status: 409,
    data: { error: { code: 'conflict', message: 'slug 已存在' } },
  } as AxiosResponse)
)
expect(error).toMatchObject({ status: 409, code: 'conflict', message: 'slug 已存在' })
```

覆盖 unknown error、无 envelope 的 500、`/api/v1/` 尾斜线规范化和绝对 `https://host/api/v1`。

`ApiError` 的稳定合同为：

```ts
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
  ) {
    super(message)
  }
}
```

Run:

```bash
pnpm --dir web test -- src/lib/api/client.test.ts src/lib/api/error.test.ts
```

Expected: FAIL，模块尚不存在。

- [ ] **Step 3: 实现唯一 Axios client**

```ts
export function resolveApiBaseURL(raw = import.meta.env.VITE_API_BASE_URL) {
  const value = (raw || '/api/v1').replace(/\/+$/, '')
  if (!value.endsWith('/api/v1')) throw new Error('VITE_API_BASE_URL 必须以 /api/v1 结尾')
  return value
}

export const apiClient = axios.create({
  baseURL: resolveApiBaseURL(),
  withCredentials: true,
  timeout: 15_000,
})
```

response interceptor 只把错误转换为 `ApiError`，不在网络层做业务 toast 或导航。

- [ ] **Step 4: 抽出 QueryClient 并测试统一状态策略**

把 `main.tsx` 的 QueryClient 配置移入 `lib/query-client.ts`。实现模块级单次 401 gate：并发 unauthorized 只清 cache、toast 和导航一次；登录成功调用 `resetUnauthorizedNavigation()`。保留 401/403 不重试，移除 mock auth-store reset。

错误策略测试还必须覆盖：403 保留当前页面、提示权限不足并 invalidate `['me']`；普通 404 由 feature 空/错误页面处理；409/413/415/429 交给 mutation 表单映射；500 mutation 不清表单，只有 route 初始化 query 进入整页 error boundary。禁止在 Axios interceptor 中重复 toast。

- [ ] **Step 5: 配置 Vite 与 Biome scripts**

```ts
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  return {
    server: {
      proxy: {
        '/api': {
          target: env.VITE_DEV_PROXY_TARGET || 'http://127.0.0.1:8080',
          changeOrigin: false,
        },
      },
    },
  }
})
```

`package.json` scripts 改为：

```json
{
  "check": "biome check .",
  "check:fix": "biome check --write .",
  "test": "vitest run --browser.headless",
  "build": "tsc -b && vite build"
}
```

删除 ESLint/Prettier 残留 scripts，不增加对应依赖。

- [ ] **Step 6: 验证并 Commit**

Run:

```bash
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

Expected: 全部 PASS。

```bash
git add web/package.json web/pnpm-lock.yaml web/vite.config.ts web/src/lib web/src/main.tsx
git commit -m "feat(web): 建立真实 API 与 Query 基础设施"
```

---

### Task 8: 实现登录、setup、邀请接受与路由守卫

**Files:**
- Create: `web/src/features/auth/types.ts`
- Create: `web/src/features/auth/api.ts`
- Create: `web/src/features/auth/queries.ts`
- Create: `web/src/features/auth/schemas.ts`
- Create: `web/src/features/auth/navigation.ts`
- Create: `web/src/features/auth/navigation.test.ts`
- Create: `web/src/features/auth/components/setup-form.tsx`
- Create: `web/src/features/auth/components/setup-form.test.tsx`
- Create: `web/src/features/auth/components/invitation-registration-form.tsx`
- Create: `web/src/features/auth/components/invitation-registration-form.test.tsx`
- Modify: `web/src/features/auth/sign-in/components/user-auth-form.tsx`
- Modify: `web/src/features/auth/sign-in/components/user-auth-form.test.tsx`
- Modify: `web/src/features/auth/sign-in/index.tsx`
- Create: `web/src/routes/index.tsx`
- Create: `web/src/routes/(auth)/setup.tsx`
- Create: `web/src/routes/(auth)/invitations/$token.tsx`
- Modify: `web/src/routes/(auth)/sign-in.tsx`
- Modify: `web/src/routes/_authenticated/route.tsx`
- Delete: `web/src/routes/_authenticated/index.tsx`
- Delete: `web/src/stores/auth-store.ts`
- Delete: `web/src/stores/auth-store.test.ts`

**Interfaces:**
- Produces: `meQueryOptions()`、`bootstrapStatusQueryOptions()`、`publicInvitationQueryOptions(token)`。
- Produces: `safeRedirect(raw: string | undefined): string | undefined` 与 `chooseWorkspaceEntry(me, recentSlug)`。

- [ ] **Step 1: 写认证类型、redirect 与入口选择测试**

定义 `Role = 'owner' | 'admin' | 'member'`、`AuthenticatedUser`、`WorkspaceSummary`、`MeResponse`、`BootstrapStatus` 和 `PublicInvitation`。测试：

```ts
expect(safeRedirect('/workspaces/acme/kb')).toBe('/workspaces/acme/kb')
expect(safeRedirect('//evil.example')).toBeUndefined()
expect(safeRedirect('https://evil.example')).toBeUndefined()
expect(chooseWorkspaceEntry(meWithOneWorkspace, undefined)).toBe('/workspaces/acme/kb')
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --dir web test -- src/features/auth/navigation.test.ts`

Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现 API/query options 与真实登录**

API 模块只传 base-relative 路径：

```ts
apiClient.post('/auth/login', input)
apiClient.post('/auth/logout')
apiClient.get<MeResponse>('/auth/me')
apiClient.get<BootstrapStatus>('/auth/bootstrap-status')
apiClient.post('/auth/register', input)
apiClient.get<PublicInvitation>(`/invitations/${encodeURIComponent(token)}`)
```

登录表单使用 RHF + Zod，成功后 invalidate/refetch `['me']`、reset 401 gate、执行安全 redirect 或 workspace 入口；删除 sleep、mock token、社交登录和 forgot-password 链接。429 只提示稍后重试，不伪造倒计时。

- [ ] **Step 4: 实现 setup 与邀请注册表单**

setup 提交 `{email,nickname,password}`，成功刷新 `['bootstrap-status']` 后去 `/sign-in`。邀请页面先加载公开 DTO，锁定 email，提交 `{email,nickname,password,invitation_token}`；成功 Cookie 由后端设置，刷新 `['me']` 后进入邀请 Workspace。两个表单都有 `confirm_password` 的 Zod refine。

- [ ] **Step 5: 实现文件路由守卫**

`/_authenticated` 的 `beforeLoad` 使用 `context.queryClient.ensureQueryData(meQueryOptions())`，401 时 redirect 到 `/sign-in?redirect=<安全站内路径>`。`/`、`/sign-in` 根据 me 选择入口；`/setup` 根据 bootstrap status 决定表单或跳转；公开邀请无 Cookie 也可访问。路由文件只组合 query/feature，不直接 Axios。

- [ ] **Step 6: 生成路由树、验证并 Commit**

Run:

```bash
pnpm --dir web build
pnpm --dir web check
pnpm --dir web test
```

Expected: PASS，`routeTree.gen.ts` 由插件更新，源码中没有 `mock-access-token` 或 auth-store import。

```bash
git add web/src web/package.json web/pnpm-lock.yaml
git commit -m "feat(web): 接入真实 session 认证流程"
```

---

### Task 9: 构建统一 AppShell、固定顶栏与动态侧边栏

**Files:**
- Create: `web/src/components/layout/app-header.tsx`
- Create: `web/src/components/layout/app-header.test.tsx`
- Create: `web/src/components/layout/app-breadcrumbs.tsx`
- Create: `web/src/components/layout/workspace-switcher.tsx`
- Create: `web/src/components/layout/workspace-switcher.test.tsx`
- Modify: `web/src/components/layout/authenticated-layout.tsx`
- Modify: `web/src/components/layout/app-sidebar.tsx`
- Modify: `web/src/components/layout/nav-group.tsx`
- Create: `web/src/components/layout/nav-group.test.tsx`
- Modify: `web/src/components/layout/nav-user.tsx`
- Modify: `web/src/components/profile-dropdown.tsx`
- Modify: `web/src/components/sign-out-dialog.tsx`
- Modify: `web/src/components/command-menu.tsx`
- Modify: `web/src/components/layout/types.ts`
- Delete: `web/src/components/layout/team-switcher.tsx`
- Delete: `web/src/components/layout/data/sidebar-data.ts`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/route.tsx`

**Interfaces:**
- Consumes: `MeResponse` 与 URL `workspaceSlug`。
- Produces: 唯一已登录 AppShell、URL 驱动的 WorkspaceSwitcher、动态 breadcrumbs/nav 与真实 logout。

- [ ] **Step 1: 写 Header DOM 顺序与 Sidebar 激活测试**

断言 Header 使用 `fixed`，DOM 顺序是 trigger/breadcrumbs → `flex-1` → Search → ThemeSwitch → ConfigDrawer → ProfileDropdown。`isNavActive` 对 `/workspaces/acme/kb/123` 激活 KB，对 `/members` 激活成员；移动导航点击后关闭 Sheet。

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
pnpm --dir web test -- src/components/layout/app-header.test.tsx src/components/layout/nav-group.test.tsx
```

Expected: FAIL，新 shell 尚不存在。

- [ ] **Step 3: 实现 AppHeader 与 breadcrumbs**

`AuthenticatedLayout` 在 `SidebarInset` 内始终渲染 `<AppHeader fixed />` 和 `<main id="content">`。本任务定义通用 `RouteBreadcrumb` match metadata 合同，不直接 import 尚未创建的业务 feature；Task 10/11 的 route loader 填入 Workspace、KB、Document、Job 名称。加载时 Skeleton，加载失败不显示裸 UUID。

- [ ] **Step 4: 把 TeamSwitcher 改为 URL 驱动 WorkspaceSwitcher**

Workspace 数据来自 `meQueryOptions`；当前项取 route param，不存局部 active state。点击后导航到 `/workspaces/$slug/kb` 并写 `langhuan:last-workspace-slug`；只有 platform_admin 显示“创建 Workspace”。保留 Dropdown、快捷序号、移动端 side 选择和 SidebarMenu 结构。

- [ ] **Step 5: 数据化 NavGroup、NavUser 与 CommandMenu**

导航组只保留概览、知识库、成员、邀请；成员页 member+ 可见，邀请页 admin+ 可见。保留 icon/offcanvas/none collapsed 行为、Tooltip、collapsed Dropdown、Rail。NavUser 与顶部 ProfileDropdown 都读取 me 的 nickname/email，外观入口导航到 `/settings/appearance`；退出调用真实 mutation，成功清空整个 Query cache 并跳转 `/sign-in`。CommandMenu 只搜索导航、me workspaces、已缓存 KB 和主题命令，不发后端全文检索。

- [ ] **Step 6: 验证并 Commit**

Run:

```bash
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

Expected: PASS；侧栏 Cookie、移动 Sheet、RTL、theme/layout tests 保持通过。

```bash
git add web/src
git commit -m "feat(web): 建立 workspace 管理台应用壳"
```

---

### Task 10: 实现 Workspace 与知识库页面

**Files:**
- Create: `web/src/features/workspaces/types.ts`
- Create: `web/src/features/workspaces/api.ts`
- Create: `web/src/features/workspaces/queries.ts`
- Create: `web/src/features/workspaces/schemas.ts`
- Create: `web/src/features/workspaces/components/workspace-form.tsx`
- Create: `web/src/features/workspaces/components/workspace-form.test.tsx`
- Create: `web/src/features/workspaces/workspace-picker.tsx`
- Create: `web/src/features/workspaces/workspace-overview.tsx`
- Create: `web/src/features/knowledge-bases/types.ts`
- Create: `web/src/features/knowledge-bases/api.ts`
- Create: `web/src/features/knowledge-bases/queries.ts`
- Create: `web/src/features/knowledge-bases/schemas.ts`
- Create: `web/src/features/knowledge-bases/components/knowledge-base-form.tsx`
- Create: `web/src/features/knowledge-bases/components/knowledge-base-form.test.tsx`
- Create: `web/src/features/knowledge-bases/knowledge-base-list.tsx`
- Create: `web/src/features/knowledge-bases/knowledge-base-detail.tsx`
- Create: `web/src/routes/_authenticated/workspaces/index.tsx`
- Create: `web/src/routes/_authenticated/workspaces/new.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/index.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/kb/index.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/kb/new.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/kb/$kbId/index.tsx`

**Interfaces:**
- Produces: spec 中全部 workspace/KB query keys 和 mutation invalidation。
- Consumes: `POST/GET /workspaces...` 与 KB list/create/get endpoints。

- [ ] **Step 1: 写 schema、权限和 invalidation 失败测试**

Workspace schema 校验 name/slug；KB schema 校验 name、正整数 embedding dimension、`chunk_size > 0`、`0 <= chunk_overlap < chunk_size`。断言非 platform_admin 不显示创建 Workspace，member 仍显示创建 KB（匹配当前真实 member+ route），mutation 后刷新 `['me']` 或 `['knowledge-bases', slug]`。

Run:

```bash
pnpm --dir web test -- src/features/workspaces/components/workspace-form.test.tsx src/features/knowledge-bases/components/knowledge-base-form.test.tsx
```

Expected: FAIL，feature 尚不存在。

- [ ] **Step 2: 实现 workspace feature 与路由**

`/workspaces` 展示 `me.workspaces` 与角色；无 Workspace 时 platform_admin 显示创建入口，普通用户显示联系管理员。创建成功进入 `/workspaces/$slug/kb`。Workspace layout 通过 `workspaceQueryOptions(slug)` 验证 membership，404 显示“不存在或无权访问”。概览只显示真实资源入口，不造统计数字。

- [ ] **Step 3: 实现知识库列表、创建与详情**

列表稳定使用 query 结果，空状态有创建动作；详情显示 name、description、embedding dimension、chunking config、metadata、创建/更新时间。本任务只交付 KB 元数据区域，Task 11 再把独立的 DocumentList 挂到同一路由，不创建空占位组件。URL 中始终使用 `kb`，API 中始终使用 `knowledge-bases`。

- [ ] **Step 4: 运行测试与构建**

Run:

```bash
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

Expected: PASS；直接加载 KB 深层路由能仅凭 params 恢复。

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -m "feat(web): 实现 workspace 与知识库管理"
```

---

### Task 11: 实现 v0.3 文档上传、详情与 Job 轮询

**Files:**
- Create: `web/src/features/documents/types.ts`
- Create: `web/src/features/documents/api.ts`
- Create: `web/src/features/documents/queries.ts`
- Create: `web/src/features/documents/schemas.ts`
- Create: `web/src/features/documents/polling.ts`
- Create: `web/src/features/documents/polling.test.ts`
- Create: `web/src/features/documents/components/document-upload-form.tsx`
- Create: `web/src/features/documents/components/document-upload-form.test.tsx`
- Create: `web/src/features/documents/document-list.tsx`
- Create: `web/src/features/documents/document-detail.tsx`
- Create: `web/src/features/documents/job-detail.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/kb/$kbId/documents/new.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/documents/$documentId.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/jobs/$jobId.tsx`
- Modify: `web/src/features/knowledge-bases/knowledge-base-detail.tsx`

**Interfaces:**
- Produces: `documentQueryOptions`、`documentsQueryOptions`、`jobQueryOptions`、`uploadDocument` 与可见性安全的 polling interval。
- Consumes: v0.3 Document/Job DTO 和 ingest `{document,job,deduped}`。

前端状态类型必须与 Go 枚举逐字一致：

```ts
type DocumentStatus =
  | 'pending' | 'parsing_submitted' | 'parsing' | 'parsed' | 'indexing'
  | 'completed' | 'failed' | 'deleting' | 'deleted'
type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
```

- [ ] **Step 1: 写 v0.3 上传合同失败测试**

表单 `accept` 必须是：

```text
.md,.markdown,.txt,.csv,.xlsx,.docx
```

测试 FormData 包含 `file/title/source_type/metadata`，`dedupe` 只在 query string；不手工写 Content-Type boundary；`deduped=true` 显示复用提示；`413` 与 `415 unsupported_file_type` 使用不同中文错误。

- [ ] **Step 2: 写轮询状态机失败测试**

```ts
expect(documentPollInterval({ status: 'pending', stableCount: 0, visible: true })).toBe(2000)
expect(documentPollInterval({ status: 'parsing', stableCount: 3, visible: true })).toBe(5000)
expect(documentPollInterval({ status: 'completed', stableCount: 0, visible: true })).toBe(false)
expect(documentPollInterval({ status: 'failed', stableCount: 0, visible: true })).toBe(false)
expect(documentPollInterval({ status: 'parsing', stableCount: 0, visible: false })).toBe(false)
```

Job terminal states 为 `succeeded|failed|cancelled`。

- [ ] **Step 3: 运行测试确认失败**

Run:

```bash
pnpm --dir web test -- src/features/documents/polling.test.ts src/features/documents/components/document-upload-form.test.tsx
```

Expected: FAIL，文档 feature 尚不存在。

- [ ] **Step 4: 实现 API、上传表单和文档列表**

上传成功刷新 `['documents', slug, kbId]`，导航到 Document 详情，并提供响应 Job 的链接。Metadata textarea 只能接受 JSON object。文件选择时默认 title 为文件名，但允许修改；source_type 默认 `upload`。

- [ ] **Step 5: 实现详情与生命周期安全轮询**

Document 展示 title/file_type/content_type/size/status/SHA256/timestamps/error/normalized Markdown；raw storage key 只在高级信息中展示且不伪造下载。页面 hidden 时停轮询，visible 时立即 invalidate/refetch，组件卸载由 TanStack Query 自动取消，不创建裸 interval/goroutine 式后台任务。Job payload 用安全 JSON formatter 展示。

- [ ] **Step 6: 验证并 Commit**

Run:

```bash
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

Expected: PASS；文档、Job 深层 URL 刷新不依赖前页 React state。

```bash
git add web/src
git commit -m "feat(web): 实现多格式文档处理界面"
```

---

### Task 12: 实现成员与邀请管理

**Files:**
- Create: `web/src/features/members/types.ts`
- Create: `web/src/features/members/api.ts`
- Create: `web/src/features/members/queries.ts`
- Create: `web/src/features/members/schemas.ts`
- Create: `web/src/features/members/components/member-actions.tsx`
- Create: `web/src/features/members/components/member-actions.test.tsx`
- Create: `web/src/features/members/member-list.tsx`
- Create: `web/src/features/invitations/types.ts`
- Create: `web/src/features/invitations/api.ts`
- Create: `web/src/features/invitations/queries.ts`
- Create: `web/src/features/invitations/schemas.ts`
- Create: `web/src/features/invitations/components/invitation-form.tsx`
- Create: `web/src/features/invitations/components/invitation-form.test.tsx`
- Create: `web/src/features/invitations/invitation-list.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/members.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/invitations.tsx`
- Modify: `web/src/features/workspaces/workspace-overview.tsx`

**Interfaces:**
- Consumes: enriched member DTO、invitation management DTO、role matrix 与 password reset endpoint。
- Produces: role-aware member actions、one-time invite URL 复制和 query invalidation。

- [ ] **Step 1: 写角色矩阵与表单失败测试**

覆盖：member 只看成员且无邀请入口；admin 可邀请 member/admin、只能撤销自己创建的 pending；owner 可邀请 owner/admin/member、改角色、移除成员；platform_admin 只有在拥有 membership 时进入 Workspace，但成员行可显示密码重置。最后 owner 的 409 保留真实约束文案。

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
pnpm --dir web test -- src/features/members/components/member-actions.test.tsx src/features/invitations/components/invitation-form.test.tsx
```

Expected: FAIL，业务组件尚不存在。

- [ ] **Step 3: 实现成员列表与操作**

显示 `user.nickname`、`user.email`、role、joined time；owner role mutation/delete 成功后刷新 `['members', slug]` 与 `['me']`。platform_admin 重置密码用 RHF + Zod dialog，成功只 toast，不篡改成员数据。

- [ ] **Step 4: 实现邀请列表与一次性链接**

邀请表显示 pending/accepted/expired/revoked，只有 pending 显示撤销。创建成功 modal 立即展示 `invite_url` 和复制按钮，并明确关闭后无法再次获取完整链接；列表永远只显示 `token_prefix`，不能拼造旧链接。

成员与邀请桌面表格在移动端改为资源卡片。Workspace 概览在本任务接入 `['members', slug]`，显示真实成员数量；不得为了文档总数对每个 KB 发起 N 次查询。

- [ ] **Step 5: 验证并 Commit**

Run:

```bash
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

Expected: PASS。

```bash
git add web/src
git commit -m "feat(web): 实现成员与邀请管理"
```

---

### Task 13: 清理模板、同步文档并做全链路验收

**Files:**
- Delete directory: `web/src/routes/_authenticated/apps/`
- Delete directory: `web/src/routes/_authenticated/chats/`
- Delete directory: `web/src/routes/_authenticated/tasks/`
- Delete directory: `web/src/routes/_authenticated/users/`
- Delete directory: `web/src/routes/_authenticated/help-center/`
- Delete directory: `web/src/routes/_authenticated/errors/`
- Delete directory: `web/src/routes/clerk/`
- Delete: `web/src/routes/(auth)/forgot-password.tsx`
- Delete: `web/src/routes/(auth)/otp.tsx`
- Delete: `web/src/routes/(auth)/sign-in-2.tsx`
- Delete: `web/src/routes/(auth)/sign-up.tsx`
- Delete directory: `web/src/features/apps/`
- Delete directory: `web/src/features/chats/`
- Delete directory: `web/src/features/dashboard/`
- Delete directory: `web/src/features/tasks/`
- Delete directory: `web/src/features/users/`
- Delete directory: `web/src/features/auth/forgot-password/`
- Delete directory: `web/src/features/auth/otp/`
- Delete directory: `web/src/features/auth/sign-up/`
- Delete directory: `web/src/features/settings/account/`
- Delete directory: `web/src/features/settings/display/`
- Delete directory: `web/src/features/settings/notifications/`
- Delete directory: `web/src/features/settings/profile/`
- Delete: `web/src/routes/_authenticated/settings/account.tsx`
- Delete: `web/src/routes/_authenticated/settings/display.tsx`
- Delete: `web/src/routes/_authenticated/settings/notifications.tsx`
- Modify: `web/src/routes/_authenticated/settings/index.tsx`
- Modify: `web/src/routes/_authenticated/settings/route.tsx`
- Modify: `web/src/features/settings/index.tsx`
- Modify: `web/src/features/settings/components/sidebar-nav.tsx`
- Keep: `web/src/features/settings/appearance/`
- Keep: `web/src/routes/_authenticated/settings/appearance.tsx`
- Modify: `web/src/components/command-menu.tsx`
- Delete: `web/src/lib/handle-server-error.ts`
- Delete: `web/src/lib/handle-server-error.test.ts`
- Modify: `web/package.json`
- Modify: `web/pnpm-lock.yaml`
- Create: `cmd/langhuan/web_console_api_e2e_test.go`
- Modify: `ROADMAP.md`
- Modify: `docs/ARCHITECTURE.md`

**Interfaces:**
- Produces: 无模板业务入口的最终 route tree、真实 API 集成 flow 测试和与当前实现一致的文档。

- [ ] **Step 1: 写后端真实流程 integration test**

文件使用现有 integration build tag，在隔离 PostgreSQL + Redis/asynq 环境覆盖：

```text
bootstrap false
→ 注册首个平台管理员
→ bootstrap true
→ 登录取得 HttpOnly Cookie
→ 创建 Workspace
→ 创建 KB
→ 上传 Markdown 并等待 Document completed / Job succeeded
→ 创建邀请并读取邀请公开信息
→ 接受邀请并取得新用户 Cookie
→ member 读取 KB/Document，跨 Workspace 得到统一 404
→ owner 调整成员角色
→ 登出后 me 返回 401
```

额外断言 `/api/v1/unknown` 为 JSON 404、历史根 REST 路径无 API 响应、`/mcp` 仍是 MCP handler、PDF 415 且无存储/数据库/队列副作用。

Run:

```bash
go test -tags=integration ./cmd/langhuan -run TestWebConsoleAPIFlow -count=1
```

Expected: 新测试进入真实执行；若暴露前述任务的集成缺口，先回到对应任务补最小修复并重跑，最终必须 PASS，不能以 “no tests to run” 结束。

- [ ] **Step 2: 删除模板路由与依赖**

删除所有无后端能力的 Dashboard/Tasks/Apps/Chats/Users/Clerk/OTP/Forgot Password/Billing/Notifications 入口和数据；移除 `@clerk/react`。Settings index 重定向到 `/settings/appearance`，settings sidebar 只保留外观项。保留 Theme/Font/Direction/Layout providers、ConfigDrawer、Search、appearance 页面、错误边界与通用 UI。运行构建生成新的 `routeTree.gen.ts`，再用 `rg` 确认无 `mock-access-token|Shadcn Admin|Acme Inc|@clerk/react`。

- [ ] **Step 3: 同步 ROADMAP 与架构文档**

明确写入：

```text
SPA:  /
REST: /api/v1/*
MCP:  /mcp
```

把所有根路径 `/auth/*`、`/healthz` 示例迁移到 `/api/v1/*`；v0.6 当前管理台范围说明 member 可按真实 member+ route 创建 KB/上传文档，检索台仍等待 v0.5 search API；v0.7 fallback 排除项改为 `/api/v1/*` 与 `/mcp`。记录 v0.3 当前支持格式与 PDF 非目标。

- [ ] **Step 4: 运行完整静态与单元验证**

Run:

```bash
gofmt -w .
go test ./... -count=1
go vet ./...

pnpm --dir web check
pnpm --dir web test
pnpm --dir web build

git diff --check
```

Expected: 全部 PASS；Vitest 输出零 unhandled errors。

- [ ] **Step 5: 运行 integration 验证**

Run:

```bash
go test -tags=integration ./... -count=1
```

Expected: PASS；需要的 PostgreSQL/Redis 地址沿用仓库现有 integration harness，不把凭据写入仓库。

- [ ] **Step 6: 浏览器人工 smoke test**

启动 Go server 与 `pnpm --dir web dev`，在桌面和移动 viewport 逐项验证：登录、Workspace 切换、KB 深层刷新、支持格式上传、Document/Job 轮询、邀请接受、成员权限、侧栏 icon/offcanvas/none、collapsed Dropdown、Rail、移动 Sheet、固定 Header 和右对齐搜索。完成后停止后台进程并用端口检查证明服务已退出。

- [ ] **Step 7: Commit**

```bash
git add ROADMAP.md docs/ARCHITECTURE.md cmd/langhuan/web_console_api_e2e_test.go web
git commit -m "feat(web): 完成管理台真实后端对接"
```

- [ ] **Step 8: 最终提交审计**

Run:

```bash
git status --short
git log --oneline --decorate -15
git diff main...HEAD --check
```

Expected: 功能 worktree 干净；提交按 Repository 拆分、API namespace、后端读取能力、前端基础、认证、AppShell、业务页面、清理验收分层，未包含主工作树原有 `AGENTS.md` 修改。

## Spec Coverage Matrix

| Spec requirement | Implemented by |
|---|---|
| `/api/v1/*`、JSON 404、`/mcp` 隔离 | Task 2、Task 13 |
| bootstrap status、真实 session 登录/登出/me | Task 3、Task 8 |
| 邀请公开地址与公开接受流程 | Task 4、Task 8、Task 12 |
| KB/Document/Invitation 列表与成员摘要 | Task 5、Task 6 |
| Axios、error envelope、Query keys、并发 401 | Task 7、Task 8 |
| 显式 Workspace 与短 `kb` 文件路由 | Task 8、Task 9、Task 10、Task 11、Task 12 |
| 复用侧边栏、固定顶栏、右对齐搜索、breadcrumbs | Task 9 |
| Workspace/KB 页面与权限 | Task 10 |
| v0.3 支持格式、上传、Document/Job 状态和轮询 | Task 11 |
| owner/admin/member/platform_admin 操作矩阵 | Task 6、Task 9、Task 10、Task 12 |
| 模板清理、响应式、深层刷新、端到端验收 | Task 13 |
