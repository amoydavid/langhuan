# 琅嬛多能力 Provider 与模型管理 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让一条 Provider 连接可显式承载多个模型 capability，并交付模型/连接双视图、SiliconFlow Embedding+Rerank、多模型创建、连接详情和移动端等价体验。

**Architecture:** 用显式 `ProviderDescriptorRegistry` 作为 Provider 管理事实源，Descriptor 规范化共享 config/credentials，按 capability 精确路由到现有 Embedding/Rerank Factory。数据库现有 `model_providers 1:N models` 不变；API 增加 capabilities/counts 与管理型全局模型查询；Web 以 URL search params 驱动模型/连接双视图，并按 `model.type` 渲染类型专属表单。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL 17、React 19、TypeScript 7、TanStack Router/Query、React Hook Form、Zod、Tailwind CSS v4、shadcn/Radix、Vitest Browser、Biome。

## Global Constraints

- 权威设计：`docs/superpowers/specs/2026-08-06-multi-capability-provider-model-management-design.md`。
- Provider 是共享 Endpoint/credential/scope/status 的连接；Model 是其下实例；Model type 创建后不可变。
- capability 只能来自服务端 descriptor，不能由用户勾选或前端根据 provider key 推断。
- SiliconFlow key 固定为 `siliconflow`，capabilities 固定为 `[embedding, rerank]`。
- 不新增数据库关系或动态 JSON Schema 表单引擎，不开放 LLM，不改变 Rerank 检索算法。
- Workspace/平台权限、404 隐藏语义、Generation snapshot 和 credential 加密合同保持不变。
- Provider/Model 普通 DOM 不显示 UUID、config hash、credential 或 raw config JSON。
- 自动化数据库测试只使用测试期临时 Docker PostgreSQL；外部模型测试只使用 `httptest`/fake server。
- 所有生产行为严格测试先行；完成前运行 Go/Web 全量验证。

---

## 文件结构与职责

| 路径 | 职责 |
|---|---|
| `internal/application/service/provider_descriptor.go` | 显式 Provider descriptor、registry、capability lookup 与启动校验。 |
| `internal/adapters/siliconflow/` | SiliconFlow 共享连接 codec、Embedding/Rerank 薄 Factory。 |
| `internal/application/dto/model_provider.go` | Provider capabilities 与 model counts API。 |
| `internal/infrastructure/db/model_repository.go` | Provider count 聚合与全局管理模型查询。 |
| `internal/interfaces/http/model_handler.go` | 管理型全局模型 filters；selectable 查询保持严格合同。 |
| `web/src/features/models/model-service-page.tsx` | 模型/连接双视图页面容器。 |
| `web/src/features/models/components/model-catalog.tsx` | 桌面模型表格、移动模型卡片与单行测试状态。 |
| `web/src/features/models/components/provider-catalog.tsx` | 连接列表、capability/count/status。 |
| `web/src/features/models/components/model-editor.tsx` | 多能力类型选择与按 Model type 分派表单。 |
| `web/src/features/models/components/provider-connection-settings.tsx` | 可读连接配置、凭证和危险操作。 |
| `web/src/features/models/search-params.ts` | 双视图与详情筛选的 typed URL state。 |

## 跨任务接口锁定

```go
type ProviderDescriptor struct {
    Key              string
    Capabilities     []value.ProviderCapability
    CredentialFields []string
    DecodeProvider   func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error)
}

type ProviderModelCounts struct {
    Total     int64 `json:"total"`
    Active    int64 `json:"active"`
    Embedding int64 `json:"embedding"`
    Rerank    int64 `json:"rerank"`
}

// dto.ModelProvider
Capabilities []value.ProviderCapability `json:"capabilities"`
ModelCounts  ProviderModelCounts        `json:"model_counts"`

type ModelListFilter struct {
    Type       *value.ModelType
    Status     *value.ModelStatus
    Scope      *value.ModelScope
    ProviderID *uuid.UUID
    Query      string
}
```

```ts
export const modelServiceSearchSchema = z.object({
  view: z.enum(['models', 'connections']).catch('models'),
  type: z.enum(['all', 'embedding', 'rerank']).catch('all'),
  capability: z.enum(['all', 'embedding', 'rerank', 'parser']).catch('all'),
  status: z.enum(['all', 'active', 'disabled']).catch('all'),
  scope: z.enum(['all', 'workspace', 'platform']).catch('all'),
  q: z.string().max(100).catch(''),
})
```

---

### Task 1: 建立显式 Provider Descriptor 与多 capability 路由

**Files:**

- Create: `internal/application/service/provider_descriptor.go`
- Create: `internal/application/service/provider_descriptor_test.go`
- Modify: `internal/application/service/provider_factory_resolver.go`
- Modify: `internal/application/service/provider_factory_resolver_test.go`
- Modify: `internal/application/service/model.go`
- Modify: `internal/application/service/model_test.go`
- Modify: `internal/application/service/model_provider.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `cmd/langhuan/main_test.go`

**Interfaces:**

- Produces `ProviderDescriptorRegistry.Descriptor(key)` and `SupportsModelType(key,type)`。
- Existing Provider decoders are wrapped as single-capability descriptors; model creation checks descriptor before Factory lookup。

- [ ] **Step 1: 写 descriptor 合并、重复和 capability 拒绝测试。**

```go
func TestProviderDescriptorRegistrySupportsMultipleCapabilities(t *testing.T) {
    registry, err := NewProviderDescriptorRegistry(ProviderDescriptor{
        Key: "siliconflow",
        Capabilities: []value.ProviderCapability{value.CapabilityRerank, value.CapabilityEmbedding},
        CredentialFields: []string{"api_key"},
        DecodeProvider: fakeProviderDecoder,
    })
    if err != nil { t.Fatal(err) }
    got, err := registry.Descriptor(" SILICONFLOW ")
    if err != nil { t.Fatal(err) }
    if diff := cmp.Diff([]value.ProviderCapability{value.CapabilityEmbedding, value.CapabilityRerank}, got.Capabilities); diff != "" { t.Fatal(diff) }
    if !registry.SupportsModelType("siliconflow", value.ModelTypeEmbedding) || !registry.SupportsModelType("siliconflow", value.ModelTypeRerank) { t.Fatal(got) }
}

func TestModelCreateRejectsTypeOutsideProviderCapabilities(t *testing.T) {
    svc := modelServiceWithDescriptors(t, embeddingOnlyDescriptor("openai"))
    _, err := svc.CreatePlatform(ctx, CreateModelInput{ProviderID: providerID, Type: value.ModelTypeRerank})
    if !errors.Is(err, domainerrors.ErrUnsupportedModelType) { t.Fatal(err) }
}
```

- [ ] **Step 2: 运行聚焦测试并确认按预期失败。**

Run: `go test ./internal/application/service ./cmd/langhuan -run 'ProviderDescriptor|OutsideProviderCapabilities' -count=1`

Expected: FAIL，descriptor registry/constructor 尚不存在，ModelService 尚未校验 capability。

- [ ] **Step 3: 实现 descriptor registry 并替换 first-hit resolver。**

实现归一化 key、capability 去重稳定排序、nil decoder/空 capability/重复 key 启动失败。`ProviderFactoryResolver.Resolve` 只从 descriptor registry 返回统一管理视图；`ModelService.decodeModelParameters` 先调用 `SupportsModelType`，再按精确 type 获取 Factory。现有 resolver 不再按 embedding→rerank→parser 顺序猜测。

- [ ] **Step 4: 运行 service/cmd 回归。**

Run: `go test ./internal/application/service ./cmd/langhuan -run 'Provider|Model' -count=1`

Expected: PASS；现有 openai/ark/ollama/dashscope/tencentcloud/rerank_compatible/mineru options 不变且 capability 精确。

- [ ] **Step 5: 提交。**

```bash
git add internal/application/service cmd/langhuan
git commit -m "refactor(provider): 使用显式多能力描述符"
```

### Task 2: 增加 SiliconFlow 共享连接与双能力 Factory

**Files:**

- Create: `internal/adapters/siliconflow/config.go`
- Create: `internal/adapters/siliconflow/config_test.go`
- Create: `internal/adapters/siliconflow/embedding_factory.go`
- Create: `internal/adapters/siliconflow/embedding_factory_test.go`
- Create: `internal/adapters/siliconflow/rerank_factory.go`
- Create: `internal/adapters/siliconflow/rerank_factory_test.go`
- Modify: `internal/adapters/embedding/openai/factory.go`
- Modify: `internal/adapters/embedding/openai/factory_test.go`
- Modify: `internal/adapters/rerank/compatible/factory.go`
- Modify: `internal/adapters/rerank/compatible/client.go`
- Modify: `internal/adapters/rerank/compatible/factory_test.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `cmd/langhuan/main_test.go`

**Interfaces:**

- Produces provider key `siliconflow` in descriptor, Embedding registry and Rerank registry。
- Shared normalized config: `base_url`, `embedding_endpoint_path`, `rerank_endpoint_path`, `timeout_seconds`, `retry_times`。

- [ ] **Step 1: 写共享 codec、options 和双 wire contract 失败测试。**

```go
func TestDecodeProviderNormalizesSiliconFlowOnce(t *testing.T) {
    config, credentials, err := DecodeProvider(value.ModelScopePlatform,
        json.RawMessage(`{"base_url":"https://api.siliconflow.cn","timeout_seconds":60,"retry_times":2}`),
        json.RawMessage(`{"api_key":"secret"}`))
    if err != nil { t.Fatal(err) }
    if config["embedding_endpoint_path"] != "/v1/embeddings" || config["rerank_endpoint_path"] != "/v1/rerank" { t.Fatal(config) }
    if !bytes.Contains(credentials, []byte(`"api_key"`)) { t.Fatal("credential missing") }
}

func TestSiliconFlowFactoriesShareProviderKey(t *testing.T) {
    if NewEmbeddingFactory().Provider() != "siliconflow" || NewRerankFactory().Provider() != "siliconflow" { t.Fatal() }
}
```

HTTP tests assert Embedding posts to `/v1/embeddings`, Rerank posts to `/v1/rerank`, both send the same Bearer key, and neither error/log leaks payload。

- [ ] **Step 2: 运行测试并确认缺少 package/Factory。**

Run: `go test ./internal/adapters/siliconflow ./cmd/langhuan -run SiliconFlow -count=1`

Expected: FAIL，`internal/adapters/siliconflow` 与注册项不存在。

- [ ] **Step 3: 实现共享 codec 和薄 Factory。**

`siliconflow.DecodeProvider` strict decode 一次。Embedding Factory 把共享 config 投影为 OpenAI-compatible client config；Rerank Factory 投影为 `/v1/rerank` compatible config。为现有 OpenAI/Rerank compatible Factory 增加显式 provider key 构造器，使 typed error/log 使用 `siliconflow`，默认构造器行为保持不变。

- [ ] **Step 4: 验证 adapter、SSRF 和装配。**

Run: `go test ./internal/adapters/siliconflow ./internal/adapters/embedding/openai ./internal/adapters/rerank/compatible ./internal/adapters/httpclient ./cmd/langhuan -count=1`

Expected: PASS；Workspace 私网/redirect 被拒绝，platform fake server 双 endpoint 可用。

- [ ] **Step 5: 提交。**

```bash
git add internal/adapters/siliconflow internal/adapters/embedding/openai internal/adapters/rerank/compatible cmd/langhuan
git commit -m "feat(provider): 支持 SiliconFlow 多能力连接"
```

### Task 3: Provider capabilities/counts 与管理型全局模型 API

**Files:**

- Modify: `internal/application/dto/model_provider.go`
- Modify: `internal/application/dto/model_provider_test.go`
- Modify: `internal/application/service/model_repository.go`
- Modify: `internal/application/service/model_provider.go`
- Modify: `internal/application/service/model_provider_test.go`
- Modify: `internal/application/service/model.go`
- Modify: `internal/application/service/model_rerank_test.go`
- Modify: `internal/infrastructure/db/model_repository.go`
- Modify: `internal/infrastructure/db/model_repository_integration_test.go`
- Modify: `internal/interfaces/http/model_handler.go`
- Modify: `internal/interfaces/http/model_routes_test.go`
- Modify: `internal/interfaces/http/router.go`

**Interfaces:**

- Provider DTO gains `Capabilities []ProviderCapability` and `ModelCounts ProviderModelCounts`。
- `ModelService.ListWorkspaceModels/ListPlatformModels(ctx, filter)` serve management catalogs; selectable methods remain strict。

- [ ] **Step 1: 写聚合 count 与 filter 失败测试。**

```go
func TestProviderDTOIncludesCapabilitiesAndCounts(t *testing.T) {
    got := ModelProviderFromModel(provider, []string{"api_key"}, []value.ProviderCapability{value.CapabilityEmbedding, value.CapabilityRerank}, ProviderModelCounts{Total:5, Active:4, Embedding:3, Rerank:2})
    if got.ModelCounts.Total != 5 || len(got.Capabilities) != 2 { t.Fatalf("%#v", got) }
}

func TestListWorkspaceModelsFiltersWithoutChangingSelectableContract(t *testing.T) {
    got, err := service.ListWorkspaceModels(ctx, workspaceID, ModelListFilter{Type:&rerankType, Status:&activeStatus, Query:"BGE"})
    if err != nil || len(got) != 1 || got[0].Type != value.ModelTypeRerank { t.Fatalf("%#v %v", got, err) }
}
```

Integration tests 使用临时 Docker PostgreSQL，并断言一次 grouped query 返回 total/active/by-type counts，没有 N+1 查询。

- [ ] **Step 2: 运行 service/http tests 并确认合同缺失。**

Run: `go test ./internal/application/dto ./internal/application/service ./internal/interfaces/http -run 'CapabilitiesAndCounts|WorkspaceModelsFilters|AdminModels' -count=1`

Expected: FAIL，DTO fields、repository filter 和 `/admin/models` handler 不存在。

- [ ] **Step 3: 实现 count/filter/route。**

新增 `GET /admin/models` 管理列表；Workspace `GET /models` 在存在 `management=true` 或 `type=all` 时走管理 filter，既有精确 `type=embedding|rerank&active=true` 继续走 selectable 方法。Query 做 trim/max 100，DB 使用参数化 `ILIKE` 匹配可读字段；日志不记录原文。

- [ ] **Step 4: 验证 service、HTTP 与真实 PostgreSQL。**

Run: `go test ./internal/application/dto ./internal/application/service ./internal/interfaces/http -run 'Model|Provider' -count=1 && make test-image && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'Model.*Count|Model.*Filter' -count=1`

Expected: PASS；Workspace/platform scope、type/status/provider/q filters 与 counts 正确，selectable API 回归通过。

- [ ] **Step 5: 提交。**

```bash
git add internal/application internal/infrastructure/db internal/interfaces/http
git commit -m "feat(model): 提供连接能力与模型目录聚合"
```

### Task 4: Web 类型、SiliconFlow 连接表单与 typed URL state

**Files:**

- Create: `web/src/features/models/search-params.ts`
- Create: `web/src/features/models/search-params.test.ts`
- Create: `web/src/features/models/schemas/siliconflow.ts`
- Modify: `web/src/features/models/schemas/index.ts`
- Modify: `web/src/features/models/schemas/schemas.test.ts`
- Modify: `web/src/features/models/types.ts`
- Modify: `web/src/features/models/api.ts`
- Modify: `web/src/features/models/api.test.ts`
- Modify: `web/src/features/models/queries.ts`
- Modify: `web/src/features/models/components/provider-form-data.ts`
- Modify: `web/src/features/models/components/provider-fields.tsx`
- Modify: `web/src/features/models/components/provider-form.tsx`
- Modify: `web/src/routes/_authenticated/admin/models/index.tsx`
- Modify: `web/src/routes/_authenticated/workspaces/$workspaceSlug/models/index.tsx`

**Interfaces:**

- Produces validated `ModelServiceSearch` and API model/provider catalog types。
- Provider form shows read-only capability badges from provider options; SiliconFlow fields map to shared connection config。

- [ ] **Step 1: 写 schema、API path 和路由默认值失败测试。**

```ts
it('defaults model service to the model catalog', () => {
  expect(modelServiceSearchSchema.parse({})).toMatchObject({ view: 'models', type: 'all', status: 'all' })
})

it('builds a SiliconFlow connection with both endpoints', () => {
  expect(toCreateProviderRequest(siliconFlowValues)).toMatchObject({
    provider: 'siliconflow',
    config: { embedding_endpoint_path: '/v1/embeddings', rerank_endpoint_path: '/v1/rerank' },
  })
})
```

- [ ] **Step 2: 运行 Web 聚焦测试并确认缺少 schema/key。**

Run: `pnpm --dir web test -- search-params.test.ts schemas.test.ts api.test.ts provider-form.test.tsx`

Expected: FAIL，SiliconFlow union、search params 和 management API 尚未定义。

- [ ] **Step 3: 实现类型、schema、Query 和 route validation。**

Provider response 解析 capabilities/counts；Model catalog query key 包含 scope/workspace/filters。Provider form 供应商选择项旁显示服务端 capabilities，SiliconFlow advanced fields 使用固定默认 endpoint；不允许用户编辑 capability。

- [ ] **Step 4: 验证 Web schema/API/forms。**

Run: `pnpm --dir web test -- search-params.test.ts schemas.test.ts api.test.ts provider-form.test.tsx && pnpm --dir web check`

Expected: PASS；URL 默认值稳定、SiliconFlow request 精确、凭证切换清除测试通过。

- [ ] **Step 5: 提交。**

```bash
git add web/src/features/models web/src/routes/_authenticated/admin/models/index.tsx web/src/routes/_authenticated/workspaces/\$workspaceSlug/models/index.tsx
git commit -m "feat(web): 建立多能力模型服务合同"
```

### Task 5: 实现模型/连接双视图与响应式目录

**Files:**

- Create: `web/src/features/models/model-service-page.tsx`
- Create: `web/src/features/models/model-service-page.test.tsx`
- Create: `web/src/features/models/components/model-catalog.tsx`
- Create: `web/src/features/models/components/model-catalog.test.tsx`
- Create: `web/src/features/models/components/provider-catalog.tsx`
- Create: `web/src/features/models/components/provider-catalog.test.tsx`
- Modify: `web/src/features/models/model-provider-list-page.tsx`
- Modify: `web/src/features/models/components/provider-card.tsx`
- Modify: `web/src/features/models/components/model-card.tsx`
- Modify: `web/src/lib/i18n/locales/zh/models.ts`
- Modify: `web/src/lib/i18n/locales/en/models.ts`

**Interfaces:**

- `ModelServicePage` consumes route search and canManage, renders model catalog by default and connection catalog on demand。
- Desktop uses tables; mobile uses equivalent cards via responsive containers, no horizontal page overflow。

- [ ] **Step 1: 写双视图、状态与移动信息等价失败测试。**

```tsx
it('shows models by default and preserves filters in navigation', async () => {
  const screen = await render(<ModelServicePage {...props} search={defaultSearch} />)
  await expect.element(screen.getByRole('heading', { name: '模型服务' })).toBeVisible()
  await expect.element(screen.getByText('BGE Reranker v2')).toBeVisible()
  await user.click(screen.getByRole('link', { name: /连接管理/ }))
  expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ search: expect.objectContaining({ view:'connections' }) }))
})

it('distinguishes disabled model from disabled connection', async () => {
  const screen = await render(<ModelCatalog models={[activeModelOnDisabledProvider]} />)
  await expect.element(screen.getByText('连接已停用')).toBeVisible()
})
```

- [ ] **Step 2: 运行组件测试并确认新页面缺失。**

Run: `pnpm --dir web test -- model-service-page.test.tsx model-catalog.test.tsx provider-catalog.test.tsx`

Expected: FAIL，目录组件和双视图不存在。

- [ ] **Step 3: 实现桌面表格、移动卡片、filters 和精确状态。**

连接列表展示 capability/count/credential/status；模型列表展示 type/provider/upstream/type summary/reference/availability。Parser-only 连接显示“不适用”且无添加模型。首次 pending 使用同构 Skeleton，empty/filter-empty/read-only 使用规格文案。

- [ ] **Step 4: 验证组件、路由与 i18n。**

Run: `pnpm --dir web test -- model-service-page.test.tsx model-catalog.test.tsx provider-catalog.test.tsx model-pages.test.tsx && pnpm --dir web check && pnpm --dir web build`

Expected: PASS；desktop/mobile DOM 任务等价，普通 DOM 无 UUID/hash。

- [ ] **Step 5: 提交。**

```bash
git add web/src/features/models web/src/lib/i18n/locales web/src/routes
git commit -m "feat(web): 提供模型与连接双视图"
```

### Task 6: Provider 详情模型优先、多能力编辑器与单行测试

**Files:**

- Create: `web/src/features/models/components/model-editor.tsx`
- Create: `web/src/features/models/components/model-editor.test.tsx`
- Create: `web/src/features/models/components/provider-connection-settings.tsx`
- Create: `web/src/features/models/components/provider-connection-settings.test.tsx`
- Modify: `web/src/features/models/model-provider-detail-page.tsx`
- Modify: `web/src/features/models/components/model-provider-detail-content.tsx`
- Modify: `web/src/features/models/components/model-form.tsx`
- Modify: `web/src/features/models/components/model-form.test.tsx`
- Modify: `web/src/features/models/components/rerank-model-form.tsx`
- Modify: `web/src/features/models/components/model-card.tsx`
- Modify: `web/src/routes/_authenticated/admin/models/$providerId.tsx`
- Modify: `web/src/routes/_authenticated/workspaces/$workspaceSlug/models/$providerId.tsx`
- Modify: `web/src/lib/i18n/locales/zh/models.ts`
- Modify: `web/src/lib/i18n/locales/en/models.ts`

**Interfaces:**

- Provider detail defaults `tab=models`; connection tab renders allowlisted readable config and separate credential rotation。
- Model editor selects supported type on create and dispatches by `model.type` on edit; test state keyed by model ID。

- [ ] **Step 1: 写类型选择、只读共享、单行测试和焦点失败测试。**

```tsx
it('offers both model types for SiliconFlow and dispatches selected schema', async () => {
  const screen = await render(<ModelEditor provider={siliconFlowProvider} {...props} />)
  await expect.element(screen.getByRole('radio', { name: /Embedding/ })).toBeVisible()
  await expect.element(screen.getByRole('radio', { name: /Rerank/ })).toBeVisible()
  await user.click(screen.getByRole('radio', { name: /Rerank/ }))
  await expect.element(screen.getByLabelText('最大候选文档数')).toBeVisible()
})

it('edits by model type instead of provider key', async () => {
  const screen = await render(<ModelEditor provider={siliconFlowProvider} model={rerankModel} {...props} />)
  await expect.element(screen.getByText('Rerank')).toBeVisible()
  await expect.element(screen.getByRole('radio')).not.toBeInTheDocument()
})
```

- [ ] **Step 2: 运行测试并确认 provider-key 分支失败。**

Run: `pnpm --dir web test -- model-editor.test.tsx model-form.test.tsx model-card.test.tsx model-pages.test.tsx`

Expected: FAIL，当前 `ModelForm` 仍用 provider key 分支，test result 仍是 Provider 公共横幅。

- [ ] **Step 3: 实现模型优先详情与交互状态。**

详情默认模型 tab；连接设置使用可读 allowlist；轮换凭证与一般编辑分离。测试 mutation state/result 用 `Record<modelId,...>`，只锁单行。创建成功关闭 Dialog/Sheet、精确失效 queries、聚焦新行；移动使用全高 Sheet 和 sticky footer。

- [ ] **Step 4: 验证详情、权限、焦点和 build。**

Run: `pnpm --dir web test -- model-editor.test.tsx provider-connection-settings.test.tsx model-form.test.tsx model-card.test.tsx model-pages.test.tsx && pnpm --dir web check && pnpm --dir web build`

Expected: PASS；平台共享只读隐藏写操作，单/多 capability 类型行为正确，Rerank test 不显示 null 维。

- [ ] **Step 5: 提交。**

```bash
git add web/src/features/models web/src/routes web/src/lib/i18n/locales
git commit -m "feat(web): 重构多模型连接详情"
```

### Task 7: E2E、项目文档与全量验证

**Files:**

- Create: `cmd/langhuan/siliconflow_e2e_test.go`
- Modify: `cmd/langhuan/postgres_testmain_integration_test.go`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/API_ACCESS.md`
- Modify: `ROADMAP.md`
- Modify: `web/src/features/models/model-pages.test.tsx`

**Interfaces:**

- Consumes Tasks 1–6 and proves one fake SiliconFlow Provider serves both Embedding and Rerank models through REST/Web contracts。

- [ ] **Step 1: 写真实双 endpoint E2E 失败测试。**

```go
//go:build integration
func TestSiliconFlowProviderSupportsEmbeddingAndRerankModels(t *testing.T) {
    env := startTemporaryModelManagementEnv(t)
    provider := createSiliconFlowProvider(t, env.API)
    embedding := createEmbeddingModel(t, env.API, provider.ID)
    rerank := createRerankModel(t, env.API, provider.ID)
    if testModel(t, env.API, embedding.ID).Type != value.ModelTypeEmbedding { t.Fatal() }
    if testModel(t, env.API, rerank.ID).Type != value.ModelTypeRerank { t.Fatal() }
    got := listProvider(t, env.API, provider.ID)
    if got.ModelCounts.Total != 2 || len(got.Capabilities) != 2 { t.Fatalf("%#v", got) }
}
```

- [ ] **Step 2: 运行 E2E 并确认 helper/合同缺失。**

Run: `go test -tags=integration -p 1 ./cmd/langhuan -run SiliconFlowProviderSupports -count=1`

Expected: FAIL，E2E helper 或最终聚合合同尚未接入。

- [ ] **Step 3: 完成 fake server、浏览器合同和文档。**

Fake server 同时实现 `/v1/embeddings` 与 `/v1/rerank`，记录两类请求使用同一 Bearer key，并拒绝 secret 出现在日志。更新 Architecture 的 descriptor/factory 图、API_ACCESS 的 filters/capabilities/counts 和 Roadmap 已交付证据。

- [ ] **Step 4: 运行完整验证。**

Run:

```bash
gofmt -w .
go test ./...
go vet ./...
make test-integration
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
git diff --check
```

Expected: 全部 exit 0；数据库只使用临时容器，外部模型只使用 fake server，日志扫描无 credential/query/document。

- [ ] **Step 5: 提交最终闭环。**

```bash
git add cmd/langhuan docs ROADMAP.md web/src/features/models
git commit -m "feat(model): 完成多能力模型服务闭环"
```

---

## Spec → Plan 覆盖矩阵

| Spec 要求 | Tasks |
|---|---|
| 显式 descriptor、多 capability、禁止 first-hit | 1 |
| SiliconFlow shared config/credential + Embedding/Rerank | 2、7 |
| Provider capabilities/counts、管理模型 filters | 3 |
| URL 双视图与 Query 归属 | 4、5 |
| 桌面表格、移动卡片、真实状态 | 5 |
| 模型优先详情、按 type 表单、单行测试 | 6 |
| 权限、共享只读、Parser-only | 3、5、6 |
| Loading/empty/error/focus/aria-live | 5、6 |
| E2E、临时依赖、文档与全量验证 | 7 |

## Plan 自检

- 顺序：descriptor → 双能力 adapter → API aggregate → Web contract → catalogs → detail/editor → E2E。
- 类型：统一使用 `ProviderDescriptor`、`ProviderModelCounts`、`ModelListFilter`、`ModelServiceSearch`。
- 事实源：capability 始终来自 descriptor；Model editor 始终来自 Model type/用户显式选择。
- 安全：共享 credential 只解码一次；日志/DOM 不出现 credential、raw config、UUID/hash。
- 数据库：不新增 relation/migration；聚合查询使用临时 Docker 集成测试。
- 无省略步骤：每个任务包含 RED、失败验证、GREEN、通过验证和提交 gate。
