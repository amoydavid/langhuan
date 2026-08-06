# Workspace 检索策略与全局 Rerank Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Rerank 配置提升为 Workspace 级 Search Profile，仅 Workspace owner/admin 可配置，并让单库、多库搜索统一使用该策略。

**Architecture:** 新增一 Workspace 一行的 `workspace_search_settings` 持久化模型与 Workspace-scoped service/API。SearchService 与 MultiKnowledgeSearchService 在查询开始解析该策略；各 KB 仍按自己的 Embedding Generation 召回，候选全局合并后只调用一次共同 Rerank。旧 Generation Rerank 快照保留兼容，但不再参与搜索决策。

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL migrations, React 19, TanStack Query, React Hook Form, Zod, Tailwind/shadcn, Vitest.

## Global Constraints

- Domain 不依赖 HTTP/GORM；Repository 只做 Row/领域转换。
- 所有 Workspace 数据库访问必须显式携带 workspace_id 并运行在 Workspace transaction 内。
- 权限由 SessionAuth + RequireWorkspace + RequireWorkspaceRole(value.RoleAdmin) 保护写接口。
- 搜索运行时不得记录 API key、完整 query 或候选正文。
- 数据库测试只能使用测试期间临时 Docker PostgreSQL/pgvector。
- 代码先写失败测试，确认 RED 后再写生产实现。

---

### Task 1: 规格、迁移与领域持久化合同

**Files:**
- Create: `internal/domain/model/workspace_search_settings.go`
- Create: `internal/application/dto/workspace_search_settings.go`
- Create: `internal/infrastructure/db/workspace_search_settings_rows.go`
- Create: `internal/infrastructure/db/workspace_search_settings_repository.go`
- Create: `internal/infrastructure/migrate/migrations/000015_workspace_search_settings.up.sql`
- Create: `internal/infrastructure/migrate/migrations/000015_workspace_search_settings.down.sql`
- Modify: `internal/infrastructure/db/models.go`
- Modify: `internal/application/service/model_repository.go`
- Test: `internal/domain/model/workspace_search_settings_test.go`

**Interfaces:**
- Domain model exposes `WorkspaceSearchSettings{WorkspaceID, Rerank, UpdatedBy, CreatedAt, UpdatedAt}`.
- Repository exposes `Get(ctx, workspaceID)` and `Upsert(ctx, settings)`.
- `nil` Rerank means disabled; enabled values reuse `model.RerankSnapshot` validation.

- [ ] Write table-driven domain tests for disabled settings, valid enabled settings, missing model IDs, invalid candidate top-k, and invalid failure mode.
- [ ] Run `go test ./internal/domain/model -run WorkspaceSearchSettings -count=1`; confirm RED before production code.
- [ ] Implement model, DTO, Row codec, repository and SQL migration with one-row-per-workspace primary key and snapshot shape CHECK constraints.
- [ ] Extend model/provider reference-count queries to include `workspace_search_settings.rerank_model_id` / provider ID.
- [ ] Run `gofmt`, focused domain tests, and `git diff --check`.

### Task 2: Workspace Search Profile service

**Files:**
- Create: `internal/application/service/workspace_search_settings.go`
- Create: `internal/application/service/workspace_search_settings_test.go`
- Modify: `internal/application/service/rerank_client_resolver.go`

**Interfaces:**
- `WorkspaceSearchSettingsRepository` is consumed by the service.
- `WorkspaceSearchSettingsService.Get(ctx, workspaceID) (*dto.WorkspaceSearchSettings, error)` returns disabled defaults when no row exists.
- `WorkspaceSearchSettingsService.Update(ctx, workspaceID, actorRole, input) (*dto.WorkspaceSearchSettings, error)` rejects roles below admin and atomically validates the selected visible active rerank model.
- `SearchProfileResolver.Resolve(ctx, workspaceID) (*model.RerankSnapshot, error)` is the read-only runtime contract used by search.

- [ ] Write failing service tests for member rejection, disabled update, enabled update snapshot fields, model type/status rejection, and missing-row defaults.
- [ ] Run the focused tests and verify the expected RED failures.
- [ ] Implement input validation, model resolution/config hash snapshotting, repository upsert, and DTO conversion.
- [ ] Extract shared rerank snapshot construction/hash logic so Generation compatibility and Search Settings use the same validation.
- [ ] Run focused service tests and `go vet` on changed packages.

### Task 3: HTTP routes and dependency injection

**Files:**
- Create: `internal/interfaces/http/workspace_search_settings_handler.go`
- Create: `internal/interfaces/http/workspace_search_settings_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/dependencies.go` or the file containing `Dependencies`
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- `GET /api/v1/workspaces/:workspace_slug/search-settings` is Session member+ read-only.
- `PUT /api/v1/workspaces/:workspace_slug/search-settings` is Session admin/owner only.
- Request shape: `{ "rerank": { "enabled": false } }` or enabled plus `model_id`, `candidate_top_k`, `failure_mode`.

- [ ] Add handler tests for admin success, owner success, member PUT 403, malformed UUID/model input 400, and service error mapping.
- [ ] Run the handler tests to verify RED.
- [ ] Implement strict JSON decoding, typed responses, route middleware, and runtime wiring with `db.NewWorkspaceSearchSettingsRepository` and `service.NewWorkspaceSearchSettingsService`.
- [ ] Ensure API Key-authenticated requests cannot reach settings routes.
- [ ] Run `go test ./internal/interfaces/http ./cmd/langhuan -count=1`.

### Task 4: Search execution and rerank correctness

**Files:**
- Modify: `internal/application/service/search.go`
- Modify: `internal/application/service/multi_knowledge_search.go`
- Modify: `internal/application/service/search_rerank.go`
- Modify: `internal/application/service/search_observability.go`
- Modify: `internal/application/service/search_test.go`
- Modify: `internal/application/service/multi_knowledge_search_test.go`
- Modify: `internal/application/service/search_rerank_test.go`
- Create/Modify: `internal/application/service/multi_knowledge_rerank_test.go`

**Interfaces:**
- `SearchServiceDeps` and `NewMultiKnowledgeSearchService` receive `SearchProfileResolver`.
- `applyRerank` accepts the query and sends `rerankport.RerankInput{Query: query, Documents: ..., TopN: ...}`.
- Multi-search `applyMultiKnowledgeRerank(ctx, workspaceID, query, results, snapshot)` returns `([]*dto.SearchResult, error)`.

- [ ] Add failing tests proving query is sent to rerank, single search ignores Generation.Rerank and uses Workspace Profile, multi-search resolves with the real Workspace ID, mixed Embedding groups still produce one global rerank call, fallback returns RRF, and fail returns an error.
- [ ] Run focused tests to confirm RED.
- [ ] Implement profile resolution before embedding, profile snapshot/hash validation, query propagation, real workspace ID propagation, and error-returning multi-rerank.
- [ ] Remove multi-library configuration-conflict planning from the search path; only the single Workspace Profile controls rerank.
- [ ] Add structured logs for profile/model IDs, candidate count, duration, ranking stage, and fallback/error class without query/text/API key.
- [ ] Run all application service tests and `go vet ./internal/application/service/...`.

### Task 5: Frontend Workspace 检索策略页面

**Files:**
- Create: `web/src/features/search-settings/api.ts`
- Create: `web/src/features/search-settings/queries.ts`
- Create: `web/src/features/search-settings/schemas.ts`
- Create: `web/src/features/search-settings/search-settings-form.tsx`
- Create: `web/src/features/search-settings/search-settings-form.test.tsx`
- Create: `web/src/routes/_authenticated/workspaces/$workspaceSlug/search-settings.tsx`
- Modify: workspace navigation/sidebar and locale files under `web/src/lib/i18n/locales/{zh,en}`
- Modify: generated `web/src/routeTree.gen.ts` only through the route generator command

**Interfaces:**
- `GET` query loads settings and active visible rerank models via `selectableModelsQueryOptions(..., 'rerank')` or an equivalent typed query.
- `PUT` mutation invalidates settings and model queries.
- UI hides navigation for members; direct member access renders the existing forbidden state.

- [ ] Add Vitest tests for disabled defaults, enabled form validation, candidate range, admin save payload, and member visibility.
- [ ] Run the frontend focused tests to verify RED.
- [ ] Implement the form with Switch, model Select showing provider/model, candidate top-k, failure mode, explanatory copy, and loading/error states.
- [ ] Add workspace navigation entry gated by owner/admin role and generate the route tree.
- [ ] Update `selectableModelsQueryOptions` to accept model type without weakening the embedding callers.
- [ ] Run `pnpm test`, `pnpm check`, and `pnpm build` in `web`.

### Task 6: Documentation, migration verification, and completion audit

**Files:**
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/API_ACCESS.md`
- Modify: `docs/DATABASE_GUIDELINES.md`
- Modify: `docs/superpowers/specs/2026-08-06-workspace-search-profile-design.md`

- [ ] Document Workspace Search Profile ownership, API contracts, multi-Embedding/global-Rerank flow, and legacy Generation field behavior.
- [ ] Run migration tests from an empty temporary pgvector database and verify up/down ordering.
- [ ] Run `go test ./... -count=1`, `go vet ./...`, `pnpm check`, `pnpm build`, and `git diff --check`.
- [ ] Audit every requirement in the design spec against code, tests, routes, permissions, logs, and runtime wiring; do not claim completion if any item lacks direct evidence.
