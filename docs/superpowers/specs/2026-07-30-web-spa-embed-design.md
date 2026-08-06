# Web SPA 内嵌设计

## 1. 目标

让琅嬛的 Linux 发布物成为同时包含 Go 服务与 Web Console 的单一二进制：`make linux` 每次都先构建当前 `web/` 源码，再把 Vite 的默认输出目录 `web/dist` 内嵌进 `bin/langhuan-linux-amd64`。

开发态保持现状：后端由普通 `go run` / `make dev` 启动，前端由 `make web` 启动 Vite 开发服务器。普通 Go 构建与测试不得依赖预先存在的 `web/dist`。

## 2. 方案选择

采用显式 `web_embed` build tag。

- 带 `web_embed` tag 时，`web/embed.go` 使用 `//go:embed all:dist` 暴露只读 SPA 文件系统。
- 不带该 tag 时，`web/embed_dev.go` 提供空的可选文件系统，HTTP 服务不接管 SPA 路由。
- 不采用参考项目的 `!dev` 默认内嵌方式，因为干净 checkout 没有被 Git 跟踪的 `web/dist`，这会使普通 `go test ./...` 在编译阶段失败。
- 不提交 `web/dist`，避免把带内容 hash 的构建产物长期纳入版本控制。

## 3. 组件与依赖

### 3.1 Web 资源包

`web/` 增加同属 `webspa` package 的 Go 文件：

- `embed.go`：只在 `web_embed` tag 下编译，内嵌 `dist` 全部文件，并将 `dist` 子目录作为 SPA 文件系统暴露。
- `embed_dev.go`：只在非 `web_embed` 构建中编译，暴露 `nil` 文件系统，避免读取磁盘产物或改变开发路由。
- `embed_test.go`：只在 `web_embed` tag 下运行，确认 bundle 至少包含 `index.html`、`assets/` 目录和实际构建资源。

`cmd/langhuan` 只负责把这个可选文件系统传给 HTTP router，不负责静态文件路径判断。

### 3.2 HTTP SPA handler

`internal/interfaces/http` 增加聚焦的 SPA handler，并在 `Dependencies` 中增加可选的 `fs.FS` 依赖。职责如下：

1. 对 `/` 返回 `index.html`。
2. 对 bundle 中存在的文件返回对应静态资源，并沿用 Go 标准库的 Content-Type 与 HEAD 行为。
3. 对不存在的前端路径返回 `index.html`，支持 TanStack Router 深层路由刷新。
4. 不列出目录，不读取内嵌根目录之外的文件。
5. 只为 GET/HEAD 提供 SPA 内容；其它方法保持 404。

若未注入 SPA 文件系统，router 保持当前非 API 路径返回 404 的行为。

### 3.3 协议边界

SPA fallback 不能覆盖已有协议入口：

- `/api/v1` 与 `/api/v1/*` 始终使用现有 JSON 404。
- `/mcp` 与 `/mcp/*` 始终属于 MCP 命名空间；未装配 MCP handler 时也不得返回 SPA。
- 已明确移除且不与前端路由冲突的根级 REST 入口 `/healthz`、`/auth/*` 与 `/admin/*` 继续保持 404，不因 SPA fallback 重新表现为成功页面。`/invitations/:token` 已由 TanStack Router 用作邀请接受页面，因此在内嵌模式下属于 SPA；旧 REST 调用方必须使用 `/api/v1/invitations/:token`。

路由优先级为：已注册 REST/MCP 路由 → 受保护的协议/旧 REST 路径 404 → 静态文件或 SPA fallback。

## 4. 构建流程

Makefile 增加内部前端构建目标：

```text
make linux
  -> pnpm --dir web build
  -> CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags web_embed
  -> bin/langhuan-linux-amd64
```

前端构建失败时立即终止，不允许继续使用旧 `web/dist` 构建二进制。`web` 开发目标和普通 Go 命令不增加 `web_embed` tag。

## 5. 错误处理

- 编译时缺少 `web/dist`：带 `web_embed` tag 的 Go 构建直接失败；`make linux` 的依赖顺序负责先生成它。
- 内嵌 bundle 缺少 `index.html`：embed 测试失败；运行时返回通用 500，不暴露文件系统错误细节。
- 请求不存在的 API/MCP 路径：按各自命名空间返回 404，绝不回退到 SPA。
- 含非法路径段或试图越出 bundle 根目录的请求返回 404；目录本身不列出内容，除 `/` 外的目录路径按前端深层路由回退到 `index.html`。

## 6. 测试与验收

按 Red → Green → Refactor 实施：

1. 先用内存文件系统为 SPA handler 编写失败测试，覆盖首页、静态资源、深层路由、HEAD、非读取方法、路径安全以及 API/MCP 隔离。
2. 实现最小 handler 与依赖注入后，让相关 Go 测试通过。
3. 构建前端，再运行带 `web_embed` tag 的资源测试，证明实际 Vite bundle 被内嵌。
4. 执行 `make linux`，证明前端构建与 Linux 二进制构建形成单一成功链路。
5. 完整门禁：`go test ./... -count=1`、`go vet ./...`、`pnpm --dir web check`、`pnpm --dir web test`、`pnpm --dir web build`、带 tag 的 embed 测试、`make linux`、`git diff --check`。

验收结果应同时证明：普通 Go 测试不需要 `web/dist`，Linux 二进制包含最新构建的 SPA，浏览器深层路由可刷新，REST 与 MCP 命名空间未被 SPA fallback 污染。
