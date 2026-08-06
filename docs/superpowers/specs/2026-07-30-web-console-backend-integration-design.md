# 琅嬛 Web Console 真实后端对接设计规格

## 1. 背景

当前 `web/` 来自 shadcn-admin 模板，已经具备 React 19、TanStack Router、TanStack Query、React Hook Form、Zod、Tailwind CSS、Radix UI、Zustand、Vitest Browser 和 Biome 等前端基础设施，也有一套成熟的可折叠响应式侧边栏、固定 Header、主题与布局配置能力。

但目前页面仍是模板演示状态，尚未形成琅嬛管理台：

- 登录使用延迟两秒后写入 `mock-access-token` 的模拟流程，没有调用 `/api/v1/auth/login`。
- `auth-store` 把模拟 token 写入可由 JavaScript 读取的 Cookie，与后端 HttpOnly session 模型冲突。
- 登录后的路由没有以 `/api/v1/auth/me` 为依据的真实守卫。
- Dashboard、Users、Tasks、Apps、Chats、Clerk、Billing 等页面和数据均为模板内容。
- TeamSwitcher、NavUser、ProfileDropdown 和 CommandMenu 读取静态示例数据。
- 没有统一 Axios API client、稳定错误 envelope 解析或 Vite 开发代理。
- 知识库、文档、Job、成员与邀请尚无真实页面。
- `package.json` 仍残留 ESLint/Prettier 命令，与项目已经采用 Biome 的事实不一致。

后端 v0.2.1 已经提供 email/password 用户、PostgreSQL session、Workspace membership、owner/admin/member 角色、邀请注册、Workspace slug 路由、知识库创建与详情、文档上传与详情、Job 详情等能力。Web Console 本轮应从模板演示页转变为能覆盖这些真实接口的管理台。

现有后端缺少知识库列表、文档列表、邀请列表、首用户初始化状态以及成员用户展示信息。严格只对接现有接口会导致资源创建后无法重新发现、邀请无法回看、成员只能显示 UUID，因此本规格同时纳入支撑正常管理台所必需的最小后端读取接口。

## 2. 目标

本次设计交付以下能力：

- 使用后端 HttpOnly session 完成登录、登出、当前身份恢复和路由守卫。
- 提供首个平台管理员初始化，以及邀请查看、接受、注册并自动进入 Workspace 的流程。
- 使用显式 Workspace slug 前端路由，使资源页面可以刷新、收藏和分享。
- 覆盖当前后端已有的 Workspace、成员、邀请、知识库、文档和 Job 业务接口。
- 增加 KB、文档、邀请列表、bootstrap status 和成员用户摘要等最小后端能力。
- 修正邀请公开链接生成规则，使本地 Vite 代理和生产反向代理都能得到可用链接。
- 统一 REST HTTP 接口为 `/api/v1/*`，并与 SPA 路由、`/mcp` 协议入口严格隔离。
- 复用现有 AppSidebar 的折叠、Tooltip、Dropdown、移动端 Sheet、布局 variant 和 Cookie 偏好体验。
- 统一使用固定顶栏：左侧面包屑，右侧搜索和系统按钮。
- 用 TanStack Query 管理全部服务端状态，用 React Hook Form + Zod 管理表单。
- 移除 mock token、静态示例业务数据和与琅嬛无关的模板入口。
- 为主要角色、深层路由、异步状态轮询和错误边界提供测试。

## 3. 非目标

本次明确不做：

- Workspace API Token 的签发、吊销和鉴权。
- MCP 客户端或 MCP 调试台；`/mcp` 是协议入口，不是 Web Console 页面。
- OIDC、SSO、GitHub/Facebook 登录、OTP 登录。
- 邮件密码重置或用户自助密码重置；当前只提供 platform_admin 手动重置。
- Workspace 编辑、删除、归档。
- 用户全局列表、用户删除或平台管理员角色管理。
- 知识库编辑、删除，文档删除或重试。
- 后端全文搜索或语义检索 UI；检索能力属于后续 ROADMAP 版本。
- API 返回数据的通用分页协议；本轮列表按早期规模返回稳定排序的完整数组。
- Web 静态资源嵌入 Go 二进制；单二进制打包仍属于 v0.7.0。
- 重写 `components/ui/` 或替换现有 Sidebar 基础组件。

## 4. 核心产品与架构决策

### 4.1 HTTP 命名空间严格隔离

服务器公开路径划分为三个互不重叠的命名空间：

```text
/                         仅用于 SPA 页面与静态资源
/api/v1/*                 所有 REST JSON、multipart 与健康检查接口
/mcp                      仅用于 MCP over HTTP 协议入口
```

规范如下：

- 所有 REST 接口，包括公开认证、邀请读取、管理操作和健康检查，统一注册在 `/api/v1` 下；不得在根路径保留 `/auth/*`、`/invitations/*`、`/admin/*`、`/healthz` 等兼容别名。
- Gin 装配入口先创建 `api := router.Group("/api/v1")`，REST handler 只能注册到该 group 或其子 group。
- 未匹配的 `/api/v1/*` 必须返回 API JSON `404`，不得返回 SPA 的 `index.html`。
- `/mcp` 及其后续协议子路径由 MCP handler 独占，不得进入 SPA fallback，也不复用 REST `/api/v1` group。
- SPA fallback 只处理 `/api/v1/*` 与 `/mcp` 之外的页面路由；未来托管前端静态资源时，深层 SPA URL 仍应返回前端入口文件。
- Web Console 的统一 API client 只以 `/api/v1` 为 base URL 发起请求。Vite 开发服务器仅把 `/api` 代理到 Go 后端；Web Console 当前不代理或调用 `/mcp`。
- 生产反向代理必须先保留 `/api/v1/*` 与 `/mcp`，再应用 SPA fallback，避免协议请求被前端页面吞掉。

### 4.2 Workspace 显式进入前端路由

所有 Workspace 业务页面使用可读 slug：

```text
/workspaces/$workspaceSlug/...
```

知识库路径使用简短的 `kb`，不使用较长的 `knowledge-bases`：

```text
/workspaces/$workspaceSlug/kb
/workspaces/$workspaceSlug/kb/$kbId
```

HTTP API 继续使用后端现有的 `/knowledge-bases` 命名。前端页面路径与 API 路径不要求字面一致，两者分别服务于用户导航与协议稳定性。

### 4.3 服务端身份是唯一事实来源

身份状态以 `GET /api/v1/auth/me` 为唯一事实来源：

- 浏览器不读取后端 session Cookie。
- 不在 Zustand、localStorage 或普通 Cookie 中保存 access token/session ID。
- Axios 请求使用 `withCredentials: true`。
- Query Cache 可以缓存 `/api/v1/auth/me` 响应，但后端仍是最终授权边界。
- 最近使用的 Workspace slug 可以写入 localStorage，只用于导航偏好，并且每次必须与 `/api/v1/auth/me` 响应中的 `workspaces` 核对。

### 4.4 Workspace 权限不会被 platform_admin 绕过

platform_admin 是平台级身份，只能执行平台级操作，例如创建 Workspace、重置用户密码、按已知 ID 撤销邀请。访问 Workspace 资源时仍必须拥有该 Workspace 的 membership。

前端必须把 `is_platform_admin` 与当前 Workspace 的 `role` 分开展示和判断，不能把平台管理员误显示成所有 Workspace 的 owner。

### 4.5 前端权限提示不替代后端授权

前端根据 `/api/v1/auth/me` 中当前 Workspace role 隐藏或禁用不可执行操作，减少误操作；所有请求仍由后端中间件和 service 做最终校验。

跨 Workspace 的不存在与无权限继续使用同一 `404` UI，不向用户区分具体原因。

### 4.6 保持简单的最小列表接口

本轮不引入通用 pagination、BFF 聚合 Dashboard 或复杂搜索接口。列表接口使用稳定排序数组，满足当前管理台的资源发现需求。数据规模增长后再统一设计分页协议。

## 5. 总体架构

```text
Browser
  │
  ├── TanStack Router
  │     ├── public routes
  │     ├── authenticated routes
  │     └── workspace-scoped routes
  │
  ├── TanStack Query
  │     ├── /api/v1/auth/me
  │     ├── workspace / KB / document / job
  │     └── membership / invitation
  │
  ├── React Hook Form + Zod
  │
  └── Axios client (withCredentials)
          │
          ├── 开发：Vite proxy
          └── 生产：同源反向代理路径
                  │
                  ▼
              Gin REST API
                  │
                  ▼
        application service → repository
```

开发期通过 Vite 代理保持浏览器同源语义：

```text
Browser
  └── Vite :5173
       ├── 页面与静态资源
       └── proxy /api
            └── Go Backend :8080
```

约束：

- 不为本地开发增加通配或反射式宽松 CORS。
- `VITE_API_BASE_URL` 默认值为 `/api/v1`；若显式配置，也必须以 `/api/v1` 结尾。
- `VITE_DEV_PROXY_TARGET` 配置 Vite 开发代理目标，默认可指向本地 Go 服务。
- Vite 只代理 `/api`；`/mcp` 保留给未来 MCP 客户端，不属于 Web Console 开发代理范围。
- v0.6.0 独立构建部署时，应由同一公开 origin 的反向代理先转发 `/api/v1/*` 和 `/mcp`，再处理 SPA fallback。
- 真正跨 origin 的 credentialed CORS 与 Cookie 策略不在本次范围内。

## 6. 前端路由设计

### 6.1 路由树

```text
/                                      根据 /api/v1/auth/me 执行入口跳转
├── /sign-in                           登录
├── /setup                             初始化首个平台管理员
├── /invitations/$token                查看并接受邀请
│
├── /workspaces                        选择 Workspace
├── /workspaces/new                    创建 Workspace，仅 platform_admin
│
└── /workspaces/$workspaceSlug         Workspace 布局与概览
    ├── /                              Workspace 概览
    ├── /kb                            知识库列表
    ├── /kb/new                        创建知识库
    ├── /kb/$kbId                      知识库详情、文档列表
    ├── /kb/$kbId/documents/new        上传文档
    ├── /documents/$documentId         文档详情与处理状态
    ├── /jobs/$jobId                   异步任务详情
    ├── /members                       成员列表与角色管理
    └── /invitations                   邀请列表、创建与撤销
```

### 6.2 入口与重定向规则

- `/` 首先读取 `/api/v1/auth/me`：
  - `401`：跳转 `/sign-in`。
  - 有合法的最近 Workspace：跳转其 `/kb`。
  - 只有一个 Workspace：跳转该 Workspace `/kb`。
  - 有多个 Workspace 且无最近偏好：跳转 `/workspaces`。
  - 无 Workspace：跳转 `/workspaces` 展示对应空状态。
- 未登录访问受保护页面时，跳转 `/sign-in?redirect=<站内路径>`。
- 登录成功后仅接受以 `/` 开头且不以 `//` 开头的站内 redirect；不接受完整 URL 或协议相对 URL。
- 已登录访问 `/sign-in` 时执行与 `/` 相同的 Workspace 选择规则。
- `/setup` 先读取 bootstrap status：
  - 未初始化：展示首管理员表单。
  - 已初始化且未登录：跳转 `/sign-in`。
  - 已登录：按正常入口规则跳转。
- `/invitations/$token` 是公开路由。邀请有效时展示锁定 email、Workspace 名称和角色；提交注册成功后直接进入对应 Workspace。
- URL 中 Workspace 不存在或当前用户不是成员时，统一展示“不存在或无权访问”，并提供返回 Workspace 选择页的入口。

### 6.3 深层资源路由

深层页面不能依赖前一页传入的 React state。刷新以下 URL 必须能通过 URL 参数自行恢复：

```text
/workspaces/acme/kb/<kb-id>
/workspaces/acme/documents/<document-id>
/workspaces/acme/jobs/<job-id>
```

面包屑中的资源名称由页面 query 返回值提供。加载期间使用 Skeleton；加载失败时不得回退显示原始 UUID 作为名称。

## 7. 固定顶栏设计

所有已登录页面统一复用现有 `Header` 组件的 `fixed` 模式、滚动阴影和半透明背景。顶栏由认证布局统一渲染，feature 页面不再分别复制 Header 组合。

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│ [侧栏开关] │ 华东研发 / 知识库 / 产品手册    [搜索 ⌘K] [主题] [外观] [用户] │
└─────────────────────────────────────────────────────────────────────────────┘
```

固定顺序：

1. 左侧 `SidebarTrigger`。
2. 纵向 Separator。
3. 路由面包屑。
4. 弹性留白。
5. Search。
6. ThemeSwitch。
7. ConfigDrawer。
8. ProfileDropdown 或 NavUser 对应的顶栏用户入口。

具体规则：

- Search 必须靠右，不再用 `me-auto` 把搜索按钮放到左侧。
- 桌面端显示完整面包屑；移动端显示当前页名称和一个可展开的上级路径菜单。
- 面包屑可点击返回可访问的上级资源页面。
- 用户入口显示真实昵称/邮箱，不使用模板头像与示例用户。
- Header 固定时，Main 是唯一内容滚动区；Header 和 Sidebar 不随内容滚出视口。

### 7.1 Command Menu 搜索范围

现有 Search 和 CommandMenu 交互继续保留。本期只搜索：

- 当前用户可访问的导航页面。
- `/api/v1/auth/me` 返回的 Workspace。
- 当前 Workspace 已经由 KB 列表 Query 加载的知识库。
- 主题切换命令。

它不是后端全文搜索，不显示“搜索文档内容”之类超出真实能力的文案。

## 8. 侧边栏复用设计

### 8.1 保留现有组件骨架

不重写 Sidebar 基础体验，继续使用：

```text
AppSidebar
├── SidebarHeader
│   └── WorkspaceSwitcher
├── SidebarContent
│   ├── NavGroup「Workspace」
│   └── NavGroup「平台管理」
├── SidebarFooter
│   └── NavUser
└── SidebarRail
```

所有后续已登录页面原型统一使用下面的 AppShell。示例身份同时拥有当前 Workspace `owner` 与 `platform_admin`，因此能展示全部条件导航；实际页面必须按权限隐藏“邀请”和“平台管理”。左右区域之间的竖向分隔线同时代表现有 `SidebarRail` 的悬浮点击区域。

```text
┌────────────────────────┬────────────────────────────────────────────────────────────────────────────┐
│ [琅] 华东研发        ↕ │ [侧栏] │ 华东研发 / 当前页面          [搜索 ⌘K] [主题] [外观] [用户]       │
│      owner              ├────────────────────────────────────────────────────────────────────────────┤
│                         │                                                                            │
│ Workspace               │ Main 内容区                                                                │
│   概览                  │                                                                            │
│   知识库                │                                                                            │
│   成员                  │                                                                            │
│   邀请                  │                                                                            │
│                         │                                                                            │
│ 平台管理                │                                                                            │
│   Workspace             │                                                                            │
│   创建 Workspace        │                                                                            │
│                         │                                                                            │
│ [张] 张三            ↕ │                                                                            │
│      zhang@example.com  │                                                                            │
└────────────────────────┴────────────────────────────────────────────────────────────────────────────┘
```

必须保留：

- `inset / floating / sidebar` 三种外观。
- `icon / offcanvas / none` 三种折叠方式，默认 `inset + icon`。
- 桌面折叠动画。
- 折叠状态图标 Tooltip。
- 折叠状态的子菜单 Dropdown。
- 移动端 Sheet。
- `SidebarRail` 展开/收起体验。
- Sidebar、布局、主题和方向偏好的 Cookie 持久化。
- ConfigDrawer 中已有的主题、Sidebar variant、折叠方式与布局选择。

`components/ui/sidebar.tsx` 原则上不修改。业务变化应集中在 AppSidebar、WorkspaceSwitcher、NavGroup 数据和 NavUser。

### 8.2 WorkspaceSwitcher

现有 TeamSwitcher 改造为 WorkspaceSwitcher，保留大尺寸触发器、Dropdown 宽度、移动端方向和折叠行为。

```text
┌────────────────────────┐
│ [琅] 华东研发        ↕ │
│      owner              │
├────────────────────────┤
│ Workspaces              │
│ ✓ 华东研发       owner  │
│   产品中心       member │
│   技术委员会     admin  │
├────────────────────────┤
│   查看全部 Workspace    │
│ + 创建 Workspace        │ 仅 platform_admin
└────────────────────────┘
```

- 当前 Workspace 由 URL `$workspaceSlug` 决定，禁止继续使用局部 `useState` 作为事实来源。
- Workspace 图标以名称前一至两个字符作为 fallback，不要求后端提供 logo。
- 第二行显示当前 role，不显示模板中的 plan。
- 切换 Workspace 后进入目标 `/workspaces/$slug/kb`，不保留另一个 Workspace 的资源子路径。
- 折叠状态只显示当前 Workspace 图标，通过现有 Tooltip/Dropdown 暴露完整信息。
- “创建 Workspace”只对 platform_admin 显示。

### 8.3 NavGroup

继续复用现有分组、Badge、子菜单、折叠 Dropdown 和激活视觉。导航数据按当前用户和 Workspace 动态生成：

```text
Workspace
  概览
  知识库
  成员
  邀请              admin/owner 显示

平台管理            platform_admin 显示
  Workspace
  创建 Workspace
```

现有 `checkIsActive` 主要按静态第一段路径判断，不能可靠区分动态 Workspace 和 KB/文档子路由。应使用 TanStack Router 匹配能力或显式的动态前缀规则修正，但保留 NavGroup 的 DOM 结构与视觉行为。

### 8.4 NavUser

继续复用现有 Footer 触发器和 Dropdown：

- 用户昵称和邮箱来自 `/api/v1/auth/me`。
- Avatar fallback 使用昵称前一至两个字符。
- 移除 Upgrade、Billing、Notifications 等模板入口。
- 保留外观设置入口和退出登录。
- 退出调用 `/api/v1/auth/logout`；成功后清空全部 Query Cache 并跳转登录。

## 9. 页面设计

### 9.1 登录页

```text
┌──────────────────────────────────────────────────────────────────────┐
│                                                                      │
│                         琅 嬛                                        │
│                    知识转化与检索服务                                │
│                                                                      │
│                 ┌──────────────────────────────┐                     │
│                 │ 登录                         │                     │
│                 │                              │                     │
│                 │ 邮箱                         │                     │
│                 │ [ name@example.com        ]  │                     │
│                 │                              │                     │
│                 │ 密码                         │                     │
│                 │ [ •••••••••••••••••      ]  │                     │
│                 │                              │                     │
│                 │ [          登录           ]  │                     │
│                 │                              │                     │
│                 │ 普通用户需通过 Workspace     │                     │
│                 │ 邀请链接完成注册。            │                     │
│                 └──────────────────────────────┘                     │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

不显示第三方登录、OTP、忘记密码或普通自由注册入口。

### 9.2 首管理员初始化页

`/setup` 使用独立文案说明这是首次部署初始化，而不是普通注册。表单字段为 email、nickname、password、confirm_password。成功后提示管理员使用新凭据登录；不假设首注册会自动创建 session。

### 9.3 邀请接受页

页面先显示：

- Workspace 名称。
- 锁定的 email。
- 将获得的 role。
- 邀请过期时间。

email 字段只读，用户填写 nickname、password、confirm_password。无效、过期、已接受或已撤销邀请统一展示“邀请不存在或已失效”。成功响应会设置 session Cookie，前端刷新 `/api/v1/auth/me` 并进入邀请所属 Workspace。

### 9.4 Workspace 选择页

展示 `/api/v1/auth/me` 响应中的 `workspaces`，每项包含 name、slug、role。点击进入其 `/kb`。

- platform_admin 无 Workspace 时：主操作为“创建 Workspace”。
- 普通用户无 Workspace 时：提示联系 Workspace 管理员发送邀请，不显示创建按钮。
- platform_admin 有 Workspace 时仍显示创建按钮。

### 9.5 Workspace 概览

概览只使用已有或本轮新增的真实数据，不设计虚假业务指标：

- Workspace 名称、slug、当前角色。
- KB 数量和最近创建的 KB，来自 KB 列表。
- 成员数量，来自成员列表。
- “创建知识库”“上传文档”“管理成员”等按角色显示的快捷入口。

不展示文档总数，因为当前最小接口只按 KB 列出文档，前端不应为一个总数对每个 KB 发起 N 次请求。

### 9.6 知识库列表

```text
┌────────────────────────┬────────────────────────────────────────────────────────────────────────────┐
│ [琅] 华东研发        ↕ │ [侧栏] │ 华东研发 / 知识库              [搜索 ⌘K] [主题] [外观] [用户]   │
│      owner              ├────────────────────────────────────────────────────────────────────────────┤
│                         │                                                                            │
│ Workspace               │ 知识库                                                   [新建知识库]      │
│   概览                  │ 将同类文档组织在一起，统一执行解析、分块和检索。                            │
│ ● 知识库                │                                                                            │
│   成员                  │ [搜索名称……]                                            共 3 个知识库     │
│   邀请                  │                                                                            │
│                         │ ┌────────────────────────────────────────────────────────────────────────┐ │
│ 平台管理                │ │ 产品手册                                                            → │ │
│   Workspace             │ │ 面向产品与售后团队的正式手册                                           │ │
│   创建 Workspace        │ │ 维度 1536 · 分块 800/120 · 2026-07-30 创建                            │ │
│                         │ ├────────────────────────────────────────────────────────────────────────┤ │
│ [张] 张三            ↕ │ │ 技术规范                                                            → │ │
│      zhang@example.com  │ │ API、架构和部署规范 · 维度 1536 · 分块 1000/150                      │ │
│                         │ └────────────────────────────────────────────────────────────────────────┘ │
└────────────────────────┴────────────────────────────────────────────────────────────────────────────┘
```

列表搜索只在已加载数据中按名称与描述过滤，不能称为后端搜索。空状态明确提供创建第一个 KB 的入口。

### 9.7 创建知识库

字段严格映射当前接口：

- name：必填。
- description：可选。
- embedding_dimension：必填且大于 0。
- chunk_size：可选高级设置。
- chunk_overlap：可选高级设置。
- metadata：可选 JSON 对象，高级设置。

创建成功后刷新 KB 列表并进入新 KB 详情。

### 9.8 知识库详情与文档列表

```text
┌────────────────────────┬────────────────────────────────────────────────────────────────────────────┐
│ [琅] 华东研发        ↕ │ [侧栏] │ 华东研发 / 知识库 / 产品手册 [搜索 ⌘K] [主题] [外观] [用户]   │
│      owner              ├────────────────────────────────────────────────────────────────────────────┤
│                         │                                                                            │
│ Workspace               │ 产品手册                                                   [上传文档]      │
│   概览                  │ 面向产品与售后团队的正式手册                                                │
│ ● 知识库                │                                                                            │
│   成员                  │ [ Embedding 1536 ] [ 分块 800 ] [ 重叠 120 ] [ 07-30 创建 ]               │
│   邀请                  │                                                                            │
│                         │ 文档                                                       [筛选状态 ▾]     │
│ 平台管理                │ ┌────────────────────────────────────────────────────────────────────────┐ │
│   Workspace             │ │ 文件                              状态             更新时间          │ │
│   创建 Workspace        │ ├────────────────────────────────────────────────────────────────────────┤ │
│                         │ │ 产品使用说明.md                   ● 已完成         10:42        →    │ │
│ [张] 张三            ↕ │ │ 安装手册.docx                      ◌ 解析中         10:40        →    │ │
│      zhang@example.com  │ │ 参数表.xlsx                        ! 失败           10:31        →    │ │
│                         │ └────────────────────────────────────────────────────────────────────────┘ │
└────────────────────────┴────────────────────────────────────────────────────────────────────────────┘
```

状态颜色必须同时配合文字和图标，不能只依赖颜色。

### 9.9 上传文档

上传使用独立路由，继续处于第 8.1 节的同一 AppShell 内。下面只展开其 Main 内容区，侧边栏、固定 Header 和底部 NavUser 不发生变化。字段映射 multipart 接口：

```text
产品手册 / 上传文档

┌─────────────────────────────────────────────────────┐
│                                                     │
│        将文件拖到这里，或点击选择文件               │
│        当前服务支持：按后端实际允许格式展示          │
│                                                     │
└─────────────────────────────────────────────────────┘

标题              [ 默认取文件名，可修改              ]
来源类型          [ upload                         ▾ ]
去重              [✓] 复用当前知识库中的相同文件
Metadata JSON      [ 可选，高级设置                    ]

                         [取消] [上传并查看处理状态]
```

请求规则：

- 文件字段名固定为 `file`。
- `title`、`source_type` 和 `metadata` 放入 FormData。
- `dedupe` 放在 query string。
- 不手工设置 multipart boundary，由浏览器处理。
- 上传成功进入文档详情，并保留响应中的 Job 链接。
- `deduped=true` 时提示“已复用已有文档”，不显示成新任务。
- `413` 显示文件超过服务端限制，不把它误报为普通网络失败。

### 9.10 文档详情与 Job 详情

文档详情展示：标题、文件类型、内容类型、大小、状态、SHA256、创建/更新时间、错误信息和 normalized markdown（有内容时）。原始 storage key 只作为高级技术信息展示，不提供无法工作的下载按钮。

Job 详情展示：type、status、attempts、external_job_id、错误、创建/更新时间和经过格式化的 payload。

轮询规则：

```text
pending / parsing_submitted / parsing / parsed / indexing
  → 前台页面每 2 秒刷新
  → 连续稳定后退避到 5 秒
  → completed / failed / deleted 时停止

页面进入后台
  → 停止轮询
页面重新可见
  → 立即刷新一次，再恢复轮询
```

Job 以其自身 JobStatus 终态判断停止。页面卸载后必须清除 timer，不启动脱离组件生命周期的 goroutine 式无限轮询。

### 9.11 成员管理

```text
┌────────────────────────┬────────────────────────────────────────────────────────────────────────────┐
│ [琅] 华东研发        ↕ │ [侧栏] │ 华东研发 / 成员                [搜索 ⌘K] [主题] [外观] [用户]   │
│      owner              ├────────────────────────────────────────────────────────────────────────────┤
│                         │                                                                            │
│ Workspace               │ 成员                                                       [发出邀请]      │
│   概览                  │ 角色决定 Workspace 内可以执行的操作。                                        │
│   知识库                │                                                                            │
│ ● 成员                  │ ┌────────────────────────────────────────────────────────────────────────┐ │
│   邀请                  │ │ 用户                         角色          加入时间           操作    │ │
│                         │ ├────────────────────────────────────────────────────────────────────────┤ │
│ 平台管理                │ │ 张三                         owner         07-29              ···     │ │
│   Workspace             │ │ zhang@example.com                                                     │ │
│   创建 Workspace        │ │                                                                        │ │
│                         │ │ 李四                         admin         07-30              ···     │ │
│ [张] 张三            ↕ │ │ li@example.com                                                        │ │
│      zhang@example.com  │ └────────────────────────────────────────────────────────────────────────┘ │
└────────────────────────┴────────────────────────────────────────────────────────────────────────────┘
```

- 所有成员可查看成员列表。
- 只有 owner 显示调整角色与移除操作。
- platform_admin 在自己可见的成员行上额外显示“重置密码”。
- 最后一名 owner 的降级/移除确认框说明约束；后端 `409` 时保留页面并刷新成员列表。
- platform_admin 重置密码成功后提示目标用户的全部旧 session 已失效。

### 9.12 邀请管理

- admin/owner 可访问邀请页并创建邀请。
- admin 只能邀请 admin/member；owner 可以邀请 owner/admin/member。
- 创建成功后明文 token 只存在于一次性 `invite_url` 响应中。页面立即提供复制按钮，并明确“关闭后无法再次查看完整链接”。
- 列表显示 pending/accepted/expired/revoked 状态；只有 pending 显示撤销操作。
- Workspace owner 可以撤销该 Workspace 内的邀请；admin 只能撤销自己创建的邀请，最终仍以后端返回为准。
- platform_admin 在已知邀请记录上可以使用全局撤销接口，但本期不增加全局邀请搜索页面。

## 10. 角色与页面能力矩阵

| 能力 | platform_admin | owner | admin | member |
|---|---:|---:|---:|---:|
| 创建 Workspace | 是 | 否 | 否 | 否 |
| 进入 Workspace | 仍需 membership | 是 | 是 | 是 |
| 查看 Workspace/KB/文档/Job | 取得 membership 后 | 是 | 是 | 是 |
| 创建 KB、上传文档 | 取得 membership 后 | 是 | 是 | 是 |
| 查看成员 | 取得 membership 后 | 是 | 是 | 是 |
| 创建 member/admin 邀请 | 取得 membership 后 | 是 | 是 | 否 |
| 创建 owner 邀请 | 取得 membership 后 | 是 | 否 | 否 |
| 修改角色、移除成员 | 否 | 是 | 否 | 否 |
| 重置用户密码 | 是 | 否 | 否 | 否 |
| 撤销任意已知邀请 | 是 | 否 | 否 | 否 |

当前真实后端把创建 KB、上传文档放在 `member+` 路由下，因此前端也允许 member 执行这两项操作，不擅自按后续 ROADMAP 文案把 member 改成只读。

## 11. HTTP API 对接清单

### 11.1 目标接口与 UI 映射

| HTTP 接口 | UI 入口 |
|---|---|
| `GET /api/v1/healthz` | API 连接诊断；不单独建立业务页面 |
| `GET /api/v1/auth/bootstrap-status` | `/setup` 入口判断 |
| `POST /api/v1/auth/register` | `/setup` 或 `/invitations/$token` |
| `POST /api/v1/auth/login` | `/sign-in` |
| `POST /api/v1/auth/logout` | NavUser/用户菜单 |
| `GET /api/v1/auth/me` | 所有认证路由、WorkspaceSwitcher、NavUser |
| `GET /api/v1/invitations/:token` | 邀请接受页 |
| `POST /api/v1/workspaces` | `/workspaces/new` |
| `GET /api/v1/workspaces/:slug` | Workspace Layout/概览 |
| `GET /api/v1/workspaces/:slug/members` | 成员页 |
| `PATCH /api/v1/workspaces/:slug/members/:user_id` | 成员角色操作 |
| `DELETE /api/v1/workspaces/:slug/members/:user_id` | 移除成员 |
| `POST /api/v1/workspaces/:slug/invitations` | 发出邀请 |
| `GET /api/v1/workspaces/:slug/invitations` | 邀请列表 |
| `DELETE /api/v1/workspaces/:slug/invitations/:invitation_id` | Workspace 邀请撤销 |
| `DELETE /api/v1/invitations/:invitation_id` | platform_admin 撤销已知邀请 |
| `POST /api/v1/admin/users/:user_id/password-reset` | 成员行 platform_admin 操作 |
| `POST /api/v1/workspaces/:slug/knowledge-bases` | 新建 KB |
| `GET /api/v1/workspaces/:slug/knowledge-bases` | KB 列表 |
| `GET /api/v1/workspaces/:slug/knowledge-bases/:id` | KB 详情 |
| `POST /api/v1/workspaces/:slug/knowledge-bases/:id/documents` | 文档上传 |
| `GET /api/v1/workspaces/:slug/knowledge-bases/:id/documents` | KB 文档列表 |
| `GET /api/v1/workspaces/:slug/documents/:id` | 文档详情/轮询 |
| `GET /api/v1/workspaces/:slug/jobs/:id` | Job 详情/轮询 |

SPA 邀请页仍是 `/invitations/$token`，但它读取数据时调用 `GET /api/v1/invitations/:token`。页面路由不因 REST 前缀迁移而改变。

`/mcp` 不映射 Web UI，也不添加 `/api/v1` 前缀。它继续作为 MCP over HTTP 协议入口。

### 11.2 新增 bootstrap status

```http
GET /api/v1/auth/bootstrap-status
```

公开接口，响应只暴露是否已有用户：

```json
{
  "initialized": true
}
```

不返回用户数量、首用户邮箱或其他可枚举信息。

### 11.3 新增 KB 列表

```http
GET /api/v1/workspaces/:workspace_slug/knowledge-bases
```

权限：member+。响应为 `KnowledgeBase[]`，按 `created_at DESC, id DESC` 排序。Repository 查询必须显式包含 `workspace_id`。

### 11.4 新增文档列表

```http
GET /api/v1/workspaces/:workspace_slug/knowledge-bases/:kb_id/documents
```

权限：member+。响应为 `Document[]`，按 `created_at DESC, id DESC` 排序。Repository 必须同时校验 Workspace 与 KB 归属，跨 Workspace 或 KB 不匹配返回 `404`。

### 11.5 新增邀请列表

```http
GET /api/v1/workspaces/:workspace_slug/invitations
```

权限：admin+。响应为管理视图数组：

```json
[
  {
    "id": "uuid",
    "workspace_id": "uuid",
    "invited_email": "user@example.com",
    "role": "member",
    "token_prefix": "AbCd1234",
    "status": "pending",
    "expires_at": "2026-08-06T10:00:00Z",
    "accepted_at": null,
    "revoked_at": null,
    "created_by": "uuid",
    "created_at": "2026-07-30T10:00:00Z"
  }
]
```

`status` 是服务端根据字段与当前时间计算的稳定枚举：`pending | accepted | expired | revoked`。排序为 pending 优先，然后 `created_at DESC, id DESC`。响应不包含 `token_hash` 或完整明文 token。

### 11.6 扩展成员列表响应

保留现有数组结构和 membership 字段，补充嵌套的非敏感用户摘要：

```json
{
  "id": "membership-uuid",
  "workspace_id": "workspace-uuid",
  "user_id": "user-uuid",
  "role": "admin",
  "user": {
    "email": "user@example.com",
    "nickname": "张三"
  },
  "created_at": "2026-07-30T10:00:00Z",
  "updated_at": "2026-07-30T10:00:00Z"
}
```

应通过一次关联查询或批量查询装配用户摘要，禁止 handler 对成员逐条调用 user repository 形成 N+1。不得暴露 `password_hash`、last login IP、session 或其他认证信息。

### 11.7 修正邀请公开地址生成

当前邀请 handler 固定拼接 `https://<request Host>/invitations/<token>`，在本地 Vite HTTP 代理下会生成协议错误的链接。`server` 配置增加：

```yaml
server:
  public_base_url: "" # 生产建议显式设置，例如 https://langhuan.example.com
```

生成规则：

- `public_base_url` 非空时，校验它是合法的绝对 `http`/`https` URL，去掉尾部 `/` 后拼接邀请路径。
- 配置为空时，使用当前请求的真实 scheme 与 Host；TLS 请求使用 `https`，普通请求使用 `http`，不得强制改成 `https`。
- 不在未配置可信代理的前提下直接信任任意 `X-Forwarded-*` 请求头。
- Vite 开发代理保留浏览器请求 Host 时，应得到 `http://localhost:5173/invitations/<token>`。
- 创建响应仍只在 `invite_url` 中出现一次完整明文 token；列表接口绝不返回完整链接。

## 12. 前端数据模型与 Query 设计

### 12.1 Query keys

```text
['bootstrap-status']
['me']
['workspace', workspaceSlug]
['knowledge-bases', workspaceSlug]
['knowledge-base', workspaceSlug, kbId]
['documents', workspaceSlug, kbId]
['document', workspaceSlug, documentId]
['job', workspaceSlug, jobId]
['members', workspaceSlug]
['invitations', workspaceSlug]
```

任何 Workspace 资源 key 都必须包含 `workspaceSlug`，避免切换 Workspace 后缓存串租户。

### 12.2 Mutation invalidation

- 初始化用户：刷新 bootstrap status，跳转登录。
- 登录：刷新 `['me']`，执行安全 redirect。
- 创建 Workspace：刷新 `['me']`，进入新 Workspace。
- 创建 KB：刷新当前 Workspace KB 列表，进入新 KB。
- 上传文档：刷新当前 KB 文档列表，进入文档详情。
- 修改/移除成员：刷新当前成员列表和 `['me']`。
- 创建/撤销邀请：刷新当前邀请列表。
- 重置密码：不修改当前成员数据；提示目标用户旧 session 已失效。
- 登出：后端成功后清空整个 Query Cache。

### 12.3 服务端状态与客户端状态边界

TanStack Query 管理：用户、Workspace、KB、文档、Job、成员、邀请、bootstrap status。

组件本地状态或轻量 context 管理：Dialog 是否打开、表格本地筛选、上传拖拽状态、当前主题与布局偏好。

不再需要保存 access token 的 auth-store。若保留 Zustand，只能用于真正跨路由且无法由 URL/Query 表达的纯客户端 UI 状态。

## 13. API client 与错误处理

### 13.1 Axios client

统一 client：

```text
baseURL: import.meta.env.VITE_API_BASE_URL || '/api/v1'
withCredentials: true
timeout: 使用具名配置
```

`VITE_API_BASE_URL` 的值必须以 `/api/v1` 结尾；feature API 模块只传入 `/auth/me`、`/workspaces/...` 等相对于该 base URL 的路径，最终请求不得逃逸到 `/api/v1` 之外。组件和 route 文件禁止直接调用裸 Axios/fetch。上传仍通过统一 client 发送 FormData。

### 13.2 错误 envelope

后端格式为：

```json
{
  "error": {
    "code": "validation_error",
    "message": "..."
  }
}
```

当前模板读取 `response.data.title`，必须改为类型安全地解析 `error.code` 和 `error.message`。

### 13.3 HTTP 状态处理

- `400 validation_error`：优先映射到字段或表单级错误。
- `401 unauthorized`：清空认证相关缓存，单次导航到登录，并保留安全站内 redirect。
- `403 forbidden`：保留当前页面，提示权限不足并刷新 `/api/v1/auth/me`。
- Workspace `404`：显示“不存在或无权访问”。
- 普通资源 `404`：显示资源不存在，提供返回上级资源入口。
- `409 conflict`：展示 slug 冲突、重复邀请、最后一个 owner 等真实业务约束。
- `413`：展示文件大小超限。
- `429 rate_limited`：登录按钮暂时禁用并提示稍后重试；没有可靠 Retry-After 时不显示伪造倒计时。
- `500 internal_error`：保留用户输入，显示通用错误；仅路由初始化失败进入整页错误边界。

并发请求同时返回 `401` 时，只允许触发一次清理和导航，避免重复 toast/导航循环。

## 14. 前端文件与组件边界

```text
web/src/
├── lib/api/
│   ├── client.ts
│   └── error.ts
│
├── features/
│   ├── auth/
│   │   ├── api.ts
│   │   ├── queries.ts
│   │   ├── schemas.ts
│   │   └── components/
│   ├── workspaces/
│   ├── knowledge-bases/
│   ├── documents/
│   ├── members/
│   └── invitations/
│
├── components/layout/
│   ├── authenticated-layout.tsx
│   ├── app-header.tsx
│   ├── app-breadcrumbs.tsx
│   ├── app-sidebar.tsx
│   ├── workspace-switcher.tsx
│   ├── nav-group.tsx
│   └── nav-user.tsx
│
└── routes/
    └── 按第 6 节 TanStack 文件路由组织
```

约束：

- API client 只在 `src/lib/api` 创建。
- DTO、schema、query options 和 mutation 靠近对应 feature。
- 展示组件不直接请求网络。
- 表单统一使用 React Hook Form + Zod。
- 跨 feature 复用类型才进入 `src/lib`，不创建模糊 `utils.ts`/`common.ts` 杂物文件。
- `components/ui` 尽量不修改，业务定制通过组合完成。
- `routeTree.gen.ts` 只由 TanStack Router 插件生成，禁止手改。

## 15. 模板清理与保留

### 15.1 清理

- mock access token、模拟用户和模拟登录延迟。
- Shadcn Admin、Acme、示例用户等静态数据。
- 电商 Dashboard 的营收、销售、订阅卡片。
- Tasks、Apps、Chats、Clerk 等路由与导航入口。
- Billing、Upgrade、Notifications、OTP、Forgot Password 等无后端能力入口。
- GitHub/Facebook 登录按钮。
- 不再使用的 Clerk 依赖。
- `package.json` 中 ESLint/Prettier 残留命令。

### 15.2 保留

- ThemeProvider、FontProvider、DirectionProvider。
- LayoutProvider 和 ConfigDrawer。
- SearchProvider 和 CommandMenu，改为真实动态导航数据。
- AppSidebar、NavGroup、NavUser 的交互基础。
- shadcn/ui 和 Radix 基础组件。
- data-table、confirm dialog、password input 等仍有业务用途的通用组件。
- NavigationProgress、Toaster、Router/Query devtools。

### 15.3 脚本

`package.json` 应提供并实际通过：

```text
pnpm check
pnpm check:fix
pnpm test
pnpm build
```

Biome 负责 lint、format 和 import 排序，不重新引入 ESLint 或 Prettier。

## 16. 后端实现边界

新增接口仍遵循项目既有分层：

```text
HTTP Handler
  → Application Service
    → Repository interface
      → GORM Repository
```

- application/domain 不持有 `*gorm.DB`。
- Repository 查询全部使用 `WithContext(ctx)`。
- Workspace 资源查询必须显式带 `workspace_id`。
- `gorm.ErrRecordNotFound` 在 Repository 映射为领域 NotFound。
- 成员用户信息通过聚焦的查询 DTO 或批量查询装配，不制造 N+1。
- Handler 只读 AuthContext，不自行重新解释 Workspace 权限。
- REST 路由统一挂载到 `router.Group("/api/v1")`；根路径不注册 REST 兼容别名。
- API `NoRoute` 处理必须先识别 `/api/v1/*` 并返回统一 JSON `404`，不得落入未来的 SPA fallback。
- `/mcp` 保持独立注册，不受 REST 路由分组调整影响。
- 不修改 worker、队列 payload 和文档处理状态机。
- 所有后端功能和缺陷修复按 Red → Green → Refactor 执行。

## 17. 视觉与响应式约束

视觉方向为“现代知识档案馆”：克制、安静、偏信息密集，保留 shadcn 的可访问性基础，但不沿用通用电商 Dashboard 内容。

- 主要语言为简体中文。
- 主色使用深墨蓝/中性色，状态和危险操作使用明确语义色。
- 标题可使用具有文献感的中文字体，正文/表格使用高可读中文无衬线字体，并提供可靠 fallback。
- 不使用紫色 AI 渐变和无业务意义的大数字卡片。
- 深色模式、RTL 和布局配置继续工作。
- 状态必须同时使用文字、图标和颜色。
- 移动端 Sidebar 使用现有 Sheet。
- 移动端表格转换为资源卡片，不依赖横向滚动完成主要操作。
- 页面主操作保持在内容标题右侧；顶栏只放全局系统操作。

## 18. 测试策略

### 18.1 前端单元与组件测试

- API client 的默认 base URL 是 `/api/v1`，显式配置也不能绕开该版本前缀。
- API client 始终携带 credentials。
- 正确解析 `{error:{code,message}}`。
- 多个并发 `401` 只触发一次退出导航。
- redirect 只接受安全站内路径。
- `/setup` 在未初始化/已初始化状态下行为正确。
- 邀请信息加载、锁定 email、提交注册和无效邀请状态。
- WorkspaceSwitcher 当前值来自 URL，不来自局部 state。
- Workspace 切换后 Query key 不串租户。
- 动态 NavGroup 在深层 KB/文档路由保持正确激活状态。
- 侧边栏折叠 Tooltip、子菜单 Dropdown 与移动 Sheet 回归。
- 固定 Header DOM 顺序为面包屑、弹性留白、搜索、系统按钮。
- member/admin/owner/platform_admin 的可见操作符合矩阵。
- KB 创建表单校验与 mutation invalidation。
- 文档上传 FormData、dedupe query 和 `413` 映射。
- `deduped=true` 显示复用提示。
- Document/Job 轮询在终态、页面隐藏和卸载时停止。
- 最后一名 owner 的 `409` 展示真实约束。
- 登出调用真实接口并清空 Query Cache。

### 18.2 后端单元与集成测试

- 路由表中不存在 `/api/v1` 之外的 REST handler，历史根路径不提供兼容 API 响应。
- 未知 `/api/v1/*` 返回统一 API JSON `404`，不得返回 HTML。
- `/mcp` 的注册与协议响应不受 REST 路由迁移影响，也不得进入 SPA fallback。
- bootstrap status 在零用户/已有用户时正确，且不泄露用户信息。
- KB 列表只返回当前 Workspace，排序稳定。
- 文档列表同时校验 Workspace 与 KB，排序稳定。
- 邀请列表权限、状态计算与排序正确，不暴露 token hash。
- 成员列表返回 email/nickname，不暴露敏感字段。
- 成员查询不存在 N+1。
- 邀请地址在配置 public base URL、本地 HTTP 请求和 TLS 请求下生成正确；非法配置启动失败。
- 跨 Workspace 访问继续统一 `404`。
- 当前 v0.2.1 认证、worker 和文档导入回归测试保持通过。

### 18.3 端到端路径

使用隔离 PostgreSQL/Redis 测试环境验证：

```text
首次初始化管理员
  → 登录
  → 创建 Workspace
  → 创建知识库
  → 上传文档
  → 查看 Document/Job 状态
  → 邀请普通用户
  → 普通用户接受邀请
  → 切换账号验证角色权限
  → 调整成员角色
  → 登出
```

必须额外验证：

- 直接刷新 KB、Document、Job 深层路由。
- `/api/v1/unknown` 返回 JSON `404`；未来启用 SPA 托管后，合法 SPA 深层路由仍返回前端入口内容。
- `/mcp` 仍由 MCP handler 响应，不返回前端入口内容。
- 用户访问无 membership 的 Workspace 得到统一 404 页面。
- 移动端打开/关闭 Sidebar 后可正常导航。
- 上传和轮询流程不需要手工复制任何资源 UUID。

## 19. 验收标准

- 当前后端认证、Workspace、成员、邀请、KB、文档和 Job 业务接口均有可到达的真实 UI。
- 所有 REST 接口只注册在 `/api/v1/*`；SPA 页面只占用非 API 路径，`/mcp` 保持独立协议入口。
- `/api/v1/healthz` 可用于连接诊断；`/mcp` 被明确保留为协议入口而非 UI。
- KB、文档、邀请列表、bootstrap status 和成员用户摘要接口完成并受正确权限保护。
- 本地 Vite 代理生成可直接打开的 HTTP 邀请链接，生产可通过 `server.public_base_url` 生成稳定公开链接。
- 页面刷新、直接打开深层 URL、浏览器前进后退和 Workspace 切换正常。
- 不存在 mock token、模拟登录、模板示例业务数据或 Clerk 入口。
- 侧边栏保留现有折叠、Tooltip、Dropdown、移动 Sheet、Rail 和外观配置体验。
- 顶栏在所有已登录页面固定，左侧是面包屑，右侧是搜索、主题、外观和用户按钮。
- Search/CommandMenu 不伪装成后端全文检索。
- 前端权限展示与后端真实角色能力一致，platform_admin 不绕过 Workspace membership。
- 所有错误、加载、空状态、上传状态和异步处理状态均有明确界面。
- session ID 不进入 JavaScript 可读存储、URL、日志或响应体。
- 代码不新增 `any`，不手改 `routeTree.gen.ts`，不重新引入 ESLint/Prettier。
- 以下检查全部通过：

```text
go test ./... -count=1
go test -tags=integration ./... -count=1
go vet ./...
git diff --check

pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

## 20. 实施顺序建议

本规格只定义设计，不代替实施计划。后续实施计划应按以下依赖顺序拆解：

1. 将全部 REST handler 迁移到 `/api/v1` group，保留独立 `/mcp`，补齐命名空间与 JSON `404` 测试。
2. 后端最小读取接口与测试。
3. 前端 API client、错误模型、开发代理和认证 Query。
4. 登录、setup、邀请接受与认证路由守卫。
5. 固定 Header、动态面包屑、AppSidebar 数据化与 WorkspaceSwitcher。
6. Workspace、KB 列表/创建/详情。
7. 文档上传、详情与 Job 轮询。
8. 成员、邀请和 platform_admin 操作。
9. 模板清理、CommandMenu、响应式和完整验收。
