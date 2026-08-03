# 琅嬛管理台

`web/` 是琅嬛的浏览器管理台，使用 React、TanStack Router/Query、Tailwind CSS 与 shadcn/ui 构建，并通过同源的 `/api/v1/*` REST 接口访问后端。

## 本地开发

```bash
pnpm install
pnpm dev
```

提交前运行：

```bash
pnpm check
pnpm test
pnpm build
```

Vite 开发服务器会把 `/api/v1` 与 `/mcp` 代理到本地 Go 服务。路由树由 TanStack Router 自动生成，不要手工编辑 `src/routeTree.gen.ts`。
