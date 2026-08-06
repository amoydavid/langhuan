# 多能力 Provider 与多模型管理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Provider 连接与具体模型解耦，使一条连接可承载多个 Embedding/Rerank 模型，并交付 SiliconFlow 双能力、管理台双视图、日志和安全闭环。

**Architecture:** 运行时以显式 `ProviderDescriptor` 描述 provider key、capabilities、共享配置解码器和凭证字段；Embedding/Rerank registry 枚举已装配 Factory，装配层按 provider key 聚合 descriptor。Application service 负责能力校验、作用域、状态和筛选，Repository 负责薄持久化；Web 以 TanStack Query 管理服务端状态，以 URL search params 管理目录视图和筛选。

**Tech Stack:** Go 1.26、Gin、GORM/PostgreSQL/pgvector、asynq/Redis、React 19、TypeScript、TanStack Query、React Hook Form、Zod、Tailwind/shadcn、Biome、Vitest。

## Global Constraints

- Domain 不依赖 HTTP、ORM 或第三方 SDK；Repository 只做持久化和 Row/Model 转换。
- 所有资源显式归属 workspace；平台共享连接在 Workspace 只读。
- 耗时测试/索引操作走 asynq，任务幂等；日志不得包含 API key、Authorization header、完整文档或 raw credentials。
- 数据库测试只能连接测试期间临时 Docker PostgreSQL（pgvector + zhparser），不能回退到 `config.yaml` 或本机 5432。
- Web 请求统一使用 TanStack Query 和 axios client；表单统一 RHF + Zod；禁止 `any`；路由生成文件不手改。

---

### Task 1: 建立显式 Provider 能力描述符

**Files:**
- Create/modify: `internal/application/service/provider_descriptor.go`
- Modify: `internal/application/service/provider_factory_resolver.go`
- Test: `internal/application/service/provider_descriptor_test.go`

**Interfaces:**
- Produces `ProviderDescriptorRegistry.Descriptor(provider)`, `Options()`, `SupportsModelType(provider, modelType)`。
- `ProviderDescriptor` contains `Key`, `Capabilities`, `CredentialFields`, `DecodeProvider`。

- [x] Write table-driven tests for empty key, nil decoder, unknown capability, duplicate key, capability deduplication and stable sort.
- [x] Replace first-hit embedding/rerank/parser routing with descriptor lookup; preserve `ErrUnsupportedProvider` mapping.
- [x] Verify with `go test ./internal/application/service -run 'ProviderDescriptor|ProviderFactory' -count=1`.

### Task 2: 让 Factory registry 可枚举并在装配层聚合

**Files:**
- Modify: `internal/adapters/embedding/registry.go`
- Modify: `internal/adapters/rerank/registry.go`
- Modify: `cmd/langhuan/main.go`
- Test: `cmd/langhuan/main_test.go`

**Interfaces:**
- Concrete registries expose `Factories() []embedding.Factory` / `Factories() []rerank.Factory`。
- `buildRuntimeServices` consumes catalog-capable registries；`buildProviderDescriptorRegistry` creates one descriptor per provider key and merges capabilities.

- [x] Add registry enumeration methods returning copies of registered values.
- [x] Build descriptors from actual registered factories instead of assuming every production provider exists; sort descriptor keys before registration.
- [x] Add regression test with only `openai`, `ollama` and `rerank_compatible`; assert no missing `ark` failure.
- [x] Verify with `go test ./cmd/langhuan -run 'TestBuildProviderDescriptorRegistry' -count=1`。

### Task 3: 接入 SiliconFlow 共享连接

**Files:**
- Create: `internal/adapters/siliconflow/config.go`
- Create: `internal/adapters/siliconflow/embedding_factory.go`
- Create: `internal/adapters/siliconflow/rerank_factory.go`
- Modify: `cmd/langhuan/main.go`
- Test: `internal/adapters/siliconflow/*_test.go`, `cmd/langhuan/siliconflow_e2e_test.go`

**Interfaces:**
- Provider key is `siliconflow` for both factories.
- Shared config fields are `base_url`, `embedding_endpoint_path`, `rerank_endpoint_path`, `timeout_seconds`, `retry_times`; credentials contain only `api_key`.

- [x] Implement strict config/credential decoding with SSRF-safe base URL and relative endpoint validation.
- [x] Project normalized config into OpenAI-compatible embedding and rerank transports.
- [x] Register both factories and verify fake server receives `/v1/embeddings` and `/v1/rerank` with one Bearer credential.
- [x] Verify with `go test ./internal/adapters/siliconflow ./cmd/langhuan -run SiliconFlow -count=1`。

### Task 4: 扩展模型服务、DTO、查询和数据库聚合

**Files:**
- Modify: `internal/application/service/model_service.go` and related service files
- Modify: `internal/application/dto/*model*go`, `internal/application/dto/*provider*go`
- Modify: `internal/infrastructure/db/*model*repository*.go`
- Modify: `internal/interfaces/http/*model*go`
- Test: service, HTTP and integration repository tests

**Interfaces:**
- Management catalog filters: `type`, `status`, `scope`, `provider_id`, `q`。
- Provider DTO includes capabilities, credential configured, and grouped model counts.

- [x] Validate provider descriptor capability before model create/update.
- [x] Add parameterized filters and one grouped count query; batch-load providers to avoid N+1.
- [x] Preserve exact selectable contract for active embedding/rerank models.
- [x] Verify with `go test -tags=integration -p 1 ./internal/infrastructure/db -run 'ModelRepositoryManagedFilterAndProviderCounts' -count=1`。

### Task 5: 实现管理台双视图和类型路由编辑器

**Files:**
- Modify: `web/src/features/models/**`
- Modify: `web/src/features/model-providers/**`
- Modify: `web/src/routes/**` (except generated route tree)
- Test: corresponding Vitest component/page tests

**Interfaces:**
- URL search schema: `view`, `type`, `capability`, `status`, `scope`, `q`。
- Model editor chooses form by `model.type`; provider options supply capability badges.

- [x] Implement “全部模型 / 连接管理” tabs, desktop table, mobile cards, and filterable URL state.
- [x] Implement connection detail tabs, safe shared-config summary, capability badges and model counts.
- [x] Implement SiliconFlow create-time type selection and edit-time type routing; preserve per-model test result state.
- [x] Verify with `pnpm --dir web check`, `pnpm --dir web test`, `pnpm --dir web build`。

### Task 6: 日志、安全、文档与端到端验收

**Files:**
- Modify: `docs/ARCHITECTURE.md`, `docs/API_ACCESS.md`, `ROADMAP.md`
- Create: `docs/superpowers/specs/2026-08-06-multi-capability-provider-models-design.md`
- Create: `docs/superpowers/plans/2026-08-06-multi-capability-provider-models.md`
- Test: `cmd/langhuan/*_e2e_test.go`, full repository test suite

**Interfaces:**
- Structured logs carry provider/model/workspace/request/job context without sensitive payloads.
- Acceptance is the checklist in the companion spec section 6.

- [x] Audit event names, fields and redaction rules for provider/model lifecycle and rerank execution.
- [x] Update architecture, API access and roadmap contracts; include the ASCII prototypes and interaction states in the spec.
- [x] Run `make test-integration` with temporary Docker services.
- [x] Run `go test ./... -count=1`, `go vet ./...`, `pnpm --dir web check`, `pnpm --dir web test`, `pnpm --dir web build`, `git diff --check`.
- [x] Commit the implementation with Conventional Commits message `fix(runtime): 按已注册能力构造 Provider 描述符`.

## Plan 自检记录

- Spec coverage: six tasks cover domain descriptor, registry/assembly, SiliconFlow transport, service/API/database, Web UX, logging/security/docs and every acceptance criterion.
- Placeholder scan: every task names concrete files, interfaces, implementation actions and verification commands; no vague future-work marker remains.
- Type consistency: `Factories()` return types match concrete registry methods; `ProviderDescriptorRegistry` methods and URL filter names match the API/Web contract.
- Verification evidence: `make test-integration` exit 0; `go test ./... -count=1` exit 0; `go vet ./...` exit 0; Web check/test/build exit 0; `git diff --check` exit 0.
