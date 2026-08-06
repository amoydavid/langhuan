# Provider 泛化与模型目录快速配置 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Provider 成为可扩展的供应商连接，提供可选模型目录快速填充，并修正开发库中的 SiliconFlow 连接数据。

**Architecture:** Provider descriptor 只声明供应商能力和共享配置解码器，能力名称采用注册式 identifier；可选 `ModelCatalog` 负责把各供应商模型列表归一化为统一目录项。HTTP/Application 层负责权限、凭证解密、类型可运行性校验和日志脱敏；Web 目录弹窗只填充表单，不直接创建业务模型。

**Tech Stack:** Go 1.26、Gin、GORM/PostgreSQL、React 19、TypeScript、TanStack Query、React Hook Form、Zod、Vitest。

## Global Constraints

- 领域层不依赖 GORM/HTTP/SDK；数据库访问只通过 Repository；数据库修改遵循 `docs/DATABASE_GUIDELINES.md`。
- Provider key 是稳定小写标识；展示名与模型类型不得写入 provider key。
- 目录调用必须透传 context、使用配置超时并清洗日志；不得记录 API key、Authorization、完整 query 或 raw credentials。
- 目录不自动写入 `models`；必须由用户选择后走现有创建模型流程。
- 测试数据库只能使用测试期间临时 Docker PostgreSQL；开发库修正只针对 `config.yaml` 的本地 DSN。
- Web 继续使用 TanStack Query、axios、RHF + Zod、Tailwind/shadcn，禁止 `any`。

---

### Task 1: 泛化 Provider capability 和 descriptor

**Files:**
- Modify: `internal/domain/value/provider_capability.go`
- Modify: `internal/application/service/provider_descriptor.go`
- Modify: `internal/application/service/provider_factory_resolver.go`
- Test: `internal/application/service/provider_descriptor_test.go`, `internal/domain/value/model_value_test.go`

**Interfaces:**
- `ProviderCapability` 接受合法 ASCII identifier；descriptor 不再使用 embedding/rerank/parser 固定 switch。
- `SupportsModelType` 使用 `CapabilityFromModelType` 和 descriptor 精确匹配。

- [ ] 先增加表驱动测试：`llm`、`asr` 等合法能力可注册；空格、大写、非法符号和空值拒绝；现有 embedding/rerank 行为不变。
- [ ] 删除 descriptor 的能力白名单，增加 identifier 规范化和稳定排序；保留重复 key/重复 capability/nil decoder 校验。
- [ ] 更新 resolver/服务错误映射，使未知能力可展示但没有对应 Model Factory 时创建失败为稳定 unsupported model type。
- [ ] 运行 `go test ./internal/domain/value ./internal/application/service -run 'ProviderDescriptor|ModelType' -count=1`。

### Task 2: 定义可选 ModelCatalog 端口并接入 descriptor

**Files:**
- Create: `internal/application/service/model_catalog.go`
- Modify: `internal/application/service/provider_descriptor.go`
- Modify: `internal/application/service/model_provider.go`
- Test: `internal/application/service/model_catalog_test.go`

**Interfaces:**
- `ModelCatalogInput` 包含 `Scope`、规范化 config、解密 credentials、可选 `ModelType` 和 query。
- `ModelCatalogItem` 包含 id、display name、description、可选 type/dimensions、parameters、available。
- `ProviderDescriptor.ListModels` 可为 nil；无实现返回 `ErrCatalogUnavailable`。

- [ ] 写 fake catalog 测试 context 取消、query 传递、错误清洗和未知类型返回。
- [ ] 把可选 `ListModels` 绑定到 descriptor；Embedding/Rerank 单能力 descriptor 默认 nil，SiliconFlow descriptor 绑定统一 catalog。
- [ ] ModelProviderService 增加按 visible/platform provider 读取、解密、调用 catalog 的 application 方法；输出结构不含 config/credentials。
- [ ] 运行 `go test ./internal/application/service -run 'ModelCatalog|Provider' -count=1`。

### Task 3: 实现 Provider 模型目录 HTTP 合同

**Files:**
- Modify: `internal/interfaces/http/model_provider_handler.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/model_routes_test.go`
- Test: `internal/interfaces/http/model_provider_routes_test.go`

**Interfaces:**
- `GET /api/v1/admin/model-providers/:provider_id/model-catalog?type=all|embedding|rerank&q=`。
- `GET /api/v1/workspaces/:workspace_slug/model-providers/:provider_id/model-catalog?type=all|embedding|rerank&q=`。
- Response includes `provider`, `items`, `source`, `fetched_at`; errors use `catalog_unavailable` or existing auth/validation codes。

- [ ] 增加严格 query 解析，限制 query 长度并把原文只传给 adapter、不写普通日志。
- [ ] 在平台/Workspace 路由分别校验 Provider 可见性、workspace ownership 和 admin 权限；平台共享连接在 Workspace 只读但可读目录。
- [ ] 为未实现 catalog、Provider disabled、workspace 越界和成功目录项补 HTTP 测试。
- [ ] 运行 `go test ./internal/interfaces/http -run 'Model.*Catalog|Provider.*Catalog' -count=1`。

### Task 4: SiliconFlow 目录适配器

**Files:**
- Modify: `internal/adapters/siliconflow/config.go`
- Create: `internal/adapters/siliconflow/model_catalog.go`
- Modify: `cmd/langhuan/main.go`
- Test: `internal/adapters/siliconflow/model_catalog_test.go`, `cmd/langhuan/siliconflow_e2e_test.go`

**Interfaces:**
- 调用 `GET {base_url}/v1/models`，复用同一 Bearer key。
- 归一化结果为 `ModelCatalogItem`；已知模型 metadata 可补类型/维度/默认参数，未知项保留 nil 并由前端手动确认。

- [ ] 用 fake HTTP server 测试路径、授权、超时、非 2xx 脱敏错误、空列表和 malformed JSON。
- [ ] 增加严格上游响应 codec；限制返回数量和单项字段长度，避免外部响应放大。
- [ ] 把 catalog callback 注册到 SiliconFlow descriptor，并保持 embedding/rerank 两个 endpoint 测试通过。
- [ ] 运行 `go test ./internal/adapters/siliconflow ./cmd/langhuan -run 'SiliconFlow' -count=1`。

### Task 5: Web 模型目录快速填充

**Files:**
- Modify: `web/src/features/models/api.ts`
- Modify: `web/src/features/models/queries.ts`
- Create: `web/src/features/models/components/model-catalog-picker.tsx`
- Modify: `web/src/features/models/components/model-editor.tsx`, `model-form.tsx`
- Test: `web/src/features/models/components/model-catalog-picker.test.tsx`, `model-editor.test.tsx`

**Interfaces:**
- Query key 包含 scope、workspace、provider id、type、q；目录请求不会污染已配置模型 query。
- `ModelCatalogItem` 选中后映射为当前 `ModelFormValues`，不自动 submit。

- [ ] 在模型编辑器增加“从 Provider 获取模型”入口和 dialog；支持搜索、类型筛选、刷新、加载、错误、空态、键盘选择。
- [ ] 选中项填充 model name/display name/type/dimensions/parameters；未知 type 置灰；保留手动输入和用户修改。
- [ ] Provider 无 catalog 时隐藏或禁用按钮并给出手动填写说明；编辑已有模型时不允许改变 type。
- [ ] 运行 `pnpm --dir web check`、`pnpm --dir web test -- --run model-catalog-picker`、`pnpm --dir web build`。

### Task 6: 修正 make dev 开发库 SiliconFlow 连接

**Files:**
- No production code file; execute a transaction against DSN from `config.yaml`.
- Record verification: `model_providers` and related `models` rows only。

**Interfaces:**
- Target row: `name='siliconflow' AND provider='openai'`。
- New provider key: `siliconflow`；config keys: `base_url`, `embedding_endpoint_path`, `rerank_endpoint_path`, `timeout_seconds`, `retry_times`。

- [ ] Begin transaction and lock the target row with `FOR UPDATE`; abort unless exactly one row matches.
- [ ] Update only `provider` and normalized config; preserve `credentials_ciphertext`, id, scope, workspace_id and all model rows.
- [ ] Read back through SiliconFlow decoder and verify model/provider foreign-key counts are unchanged.
- [ ] Record the before/after identifiers and non-sensitive config keys; never print credentials.

### Task 7: 全量验证与文档自检

**Files:**
- Modify: `docs/ARCHITECTURE.md`, `docs/API_ACCESS.md`, `ROADMAP.md`
- Create: this spec and plan

- [ ] Run `make test-integration` using temporary Docker PostgreSQL/Redis.
- [ ] Run `go test ./... -count=1`, `go vet ./...`, `pnpm --dir web check`, `pnpm --dir web test`, `pnpm --dir web build`, `git diff --check`。
- [ ] Scan spec/plan for vague placeholders, contradictions, hard-coded vendor assumptions and sensitive data examples。
- [ ] Verify main/dev database state and report exact commits without push/PR。
