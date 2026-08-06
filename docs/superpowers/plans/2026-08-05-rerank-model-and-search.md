# 琅嬛 Rerank 模型与检索重排 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为琅嬛交付从 Rerank Provider/Model 配置、连接测试、Generation 快照到 REST/MCP/Web 检索重排与结构化日志的完整闭环。

**Architecture:** 新增独立 `ports/rerank` 与 `rerank_compatible` HTTP adapter；Rerank 模型身份、config hash、候选数和失败策略作为 active Generation 的可选不可变快照。Search 在 Vector/FTS + RRF + parent 聚合后调用一次 Rerank，保留 RRF score 并新增 `rerank_score/ranking_stage`；配置一致的多知识库全局只调用一次，不一致则在模型调用前返回冲突。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL 17 + pgvector + zhparser、golang-migrate、`log/slog`、Redis/asynq、React 19、TypeScript 7、Vite、TanStack Router/Query、React Hook Form、Zod、Tailwind CSS v4、Radix/shadcn、Vitest、Biome。

## Global Constraints

- 必读设计规格：`docs/superpowers/specs/2026-08-05-rerank-model-and-search-design.md`。
- Domain 不依赖外部包；application 不持有 `*gorm.DB`；Repository 是 GORM 薄封装，Row 与领域模型手动转换。
- 所有数据库和外部 HTTP I/O 透传 `context.Context`；所有 Workspace 资源查询显式校验 `workspace_id`。
- 自动化数据库测试只允许使用测试期临时启动并销毁的 `langhuan-test-postgres:pg17` 容器，禁止读取 `config.yaml` DSN 或连接本机长期运行数据库。
- 首个 adapter 名为 `rerank_compatible`，wire contract 固定为 `POST /v1/rerank` 兼容形状；不声称它是 OpenAI 标准。
- Rerank 默认关闭；启用后只从 ready active Generation 的不可变快照读取，调用方不能按请求指定模型。
- `rerank_candidate_top_k` 合法范围 `50..200`、默认 50，且不得超过 Model `max_documents`；`final_top_k` 继续限制为 `1..50`。
- 默认 `failure_mode=fallback`；只对远端 timeout、429、5xx、网络不可达和非法响应回退 RRF。权限、配置、凭证解密、快照漂移、context cancel 和多库配置冲突始终 fail closed。
- `score` 保持 RRF score；成功重排新增 `rerank_score`，`ranking_stage` 固定为 `rrf | rerank | rrf_fallback`。不得把模型分数显示为概率或百分比。
- 日志禁止记录 query/query hash、候选正文、FAQ 问答、向量、Provider 请求/响应 body、API key、cookie、credential、Authorization 或 custom headers。
- Web 复用现有 AppShell、shadcn/Radix、TanStack Query、共享 axios client、RHF + Zod、Tailwind 语义 token；禁止 `any`、组件内 `fetch`、普通 DOM 中的 UUID/config hash。
- 每个任务严格测试先行；每个任务只提交自身文件，使用中文 Conventional Commit。

---

## 文件结构与职责

| 路径 | 职责 |
|---|---|
| `internal/adapters/providerutil/` | Embedding 与 Rerank 共用的 strict JSON、typed map 和安全 HTTP client 构造。 |
| `internal/ports/rerank/` | Application 使用方定义的 Client、Factory、Registry 和输入输出。 |
| `internal/adapters/rerank/` | Registry、稳定 Provider error、结果校验。 |
| `internal/adapters/rerank/compatible/` | `/v1/rerank` typed config、HTTP、retry、body cap 和 index 映射。 |
| `internal/domain/value/rerank.go` | failure mode、ranking stage 和校验。 |
| `internal/domain/model/index_generation.go` | 可选 `RerankSnapshot`。 |
| `internal/application/service/rerank_client_resolver.go` | 解密、构造 client、计算并返回 runtime config hash。 |
| `internal/application/service/search_rerank.go` | private rerank 文本、调用、fallback 和稳定排序。 |
| `internal/infrastructure/migrate/migrations/000014_*` | Generation Rerank 快照列、约束、FK 和索引。 |
| `internal/application/requestmeta/` | request ID、transport、principal kind 的 context 元数据。 |
| `web/src/features/models/` | capability-aware Provider 和 Embedding/Rerank 模型表单。 |
| `web/src/features/index-generations/` | Generation Rerank 选择、只读摘要和 API schema。 |
| `web/src/features/retrieval/` | Rerank/RRF/fallback 结果展示。 |

## 跨任务接口锁定

后续任务必须使用以下名称，若实现中确需改名，先同步修改本计划所有消费点。

```go
// internal/ports/rerank/rerank.go
type Document struct { ID, Text string }
type RerankInput struct { Query string; Documents []Document; TopN int }
type RerankItem struct { DocumentID string; Score float64 }
type RerankResult struct { Items []RerankItem }
type Client interface { Rerank(context.Context, RerankInput) (*RerankResult, error) }

// internal/domain/model/index_generation.go
type RerankSnapshot struct {
    ModelID, ProviderID uuid.UUID
    ModelName, ModelConfigHash string
    CandidateTopK int
    FailureMode value.RerankFailureMode
}

// internal/application/service/rerank_client_resolver.go
type ResolvedRerankClient struct {
    Client rerankport.Client
    ModelID, ProviderID uuid.UUID
    ProviderKey, ModelName, ModelConfigHash string
    MaxDocuments, MaxQueryChars, MaxDocumentChars int
}
type RerankClientResolver interface {
    Resolve(context.Context, uuid.UUID, uuid.UUID) (*ResolvedRerankClient, error)
}
```

---

### Task 1: 提取 Embedding/Rerank 共用的 Provider 边界

**Files:**

- Create: `internal/adapters/providerutil/config.go`
- Create: `internal/adapters/providerutil/config_test.go`
- Modify: `internal/adapters/embedding/ark/factory.go`
- Modify: `internal/adapters/embedding/dashscope/factory.go`
- Modify: `internal/adapters/embedding/ollama/factory.go`
- Modify: `internal/adapters/embedding/openai/factory.go`
- Modify: `internal/adapters/embedding/tencentcloud/factory.go`
- Delete after imports migrate: `internal/adapters/embedding/internal/factoryutil/config.go`
- Delete after tests migrate: `internal/adapters/embedding/internal/factoryutil/config_test.go`

**Interfaces:**

- Consumes: 现有 `internal/adapters/httpclient.NewPublicHTTPSClient` 与 `NewTrustedClient`。
- Produces: `providerutil.DecodeStrict`、`DecodeMap`、`ToMap`、`ToJSON`、`ValidateTimeout`、`ValidateBatchSize`、`ValidateEmbeddingModel`、`NewHTTPClient`，供 Tasks 2–4 使用。

- [x] **Step 1: 先复制现有测试并写跨 capability 可导入的失败测试。**

```go
package providerutil_test

func TestDecodeStrictRejectsUnknownField(t *testing.T) {
    var target struct{ Timeout int `json:"timeout"` }
    err := providerutil.DecodeStrict(
        json.RawMessage(`{"timeout":30,"secret":"leak"}`),
        &target,
        domainerrors.ErrInvalidProviderConfig,
    )
    if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
        t.Fatalf("error = %v", err)
    }
}

func TestNewHTTPClientRejectsWorkspacePrivateEndpoint(t *testing.T) {
    _, err := providerutil.NewHTTPClient(
        value.ModelScopeWorkspace,
        "https://127.0.0.1:8443",
        30*time.Second,
        nil,
    )
    if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
        t.Fatalf("error = %v", err)
    }
}
```

- [x] **Step 2: 运行测试，确认新 package 尚不存在。**

Run: `go test ./internal/adapters/providerutil -count=1`

Expected: FAIL，Go 报告 package/files 不存在或 `providerutil` 符号未定义。

- [x] **Step 3: 移动而不改变公共行为，并更新五个 Embedding adapter 的 import。**

`config.go` 保持现有函数签名；package 改为：

```go
package providerutil

func NewHTTPClient(
    scope value.ModelScope,
    baseURL string,
    timeout time.Duration,
    headers map[string]string,
) (*http.Client, error) {
    if scope == value.ModelScopeWorkspace && strings.TrimSpace(baseURL) != "" {
        client, err := httpclient.NewPublicHTTPSClient(httpclient.PublicClientConfig{
            BaseURL: baseURL,
            Timeout: timeout,
            Headers: headers,
        })
        if err != nil {
            return nil, fmt.Errorf("%w: %v", domainerrors.ErrInvalidProviderConfig, err)
        }
        return client, nil
    }
    client, err := httpclient.NewTrustedClient(timeout, headers)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", domainerrors.ErrInvalidProviderConfig, err)
    }
    return client, nil
}
```

所有 `embedding/internal/factoryutil` import 替换为 `internal/adapters/providerutil`，调用名保持不变。迁移完成后删除旧 package，禁止保留两份实现。

- [x] **Step 4: 验证共享 helper 和全部 Embedding adapter 行为未变。**

Run: `go test ./internal/adapters/providerutil ./internal/adapters/embedding/... -count=1`

Expected: PASS；五种 Embedding Provider 的现有 strict config、SSRF、维度与连接构造测试全部通过。

- [x] **Step 5: 提交。**

```bash
git add internal/adapters/providerutil internal/adapters/embedding
git commit -m "refactor(model): 提取共享 Provider 边界"
```

### Task 2: 定义 Rerank Port、Registry 和稳定错误

**Files:**

- Create: `internal/ports/rerank/rerank.go`
- Create: `internal/ports/rerank/factory.go`
- Create: `internal/adapters/rerank/registry.go`
- Create: `internal/adapters/rerank/registry_test.go`
- Create: `internal/adapters/rerank/provider_error.go`
- Create: `internal/adapters/rerank/provider_error_test.go`
- Modify: `internal/domain/errors/errors.go`

**Interfaces:**

- Consumes: `value.ModelScope`、`value.ModelTypeRerank`。
- Produces: `rerankport.Client`、`Factory`、`FactoryRegistry`，以及可被 `errors.Is` 判断的稳定 Rerank 错误。

- [x] **Step 1: 写失败的 Port/Registry/error 测试。**

```go
type fakeFactory struct{ provider string }

func (f fakeFactory) Provider() string { return f.provider }
func (f fakeFactory) CredentialFields() []string { return []string{"api_key"} }
func (f fakeFactory) DecodeProvider(rerankport.ProviderDecodeInput) (map[string]any, []byte, error) {
    return map[string]any{}, []byte(`{"api_key":"secret"}`), nil
}
func (f fakeFactory) DecodeModel(rerankport.ModelDecodeInput) (map[string]any, error) {
    return map[string]any{"max_documents": 100}, nil
}
func (f fakeFactory) NewClient(context.Context, rerankport.ClientInput) (rerankport.Client, error) {
    return nil, nil
}

func TestRegistryNormalizesProviderAndRejectsDuplicates(t *testing.T) {
    registry, err := rerankadapter.NewRegistry(fakeFactory{provider: " RERANK_COMPATIBLE "})
    if err != nil { t.Fatal(err) }
    if _, err := registry.Factory("rerank_compatible"); err != nil { t.Fatal(err) }
    if _, err := rerankadapter.NewRegistry(
        fakeFactory{provider: "rerank_compatible"},
        fakeFactory{provider: "RERANK_COMPATIBLE"},
    ); err == nil { t.Fatal("duplicate must fail") }
}

func TestProviderErrorMapsWithoutUpstreamBody(t *testing.T) {
    err := rerankadapter.NewProviderError("rerank_compatible", rerankadapter.ProviderErrorRateLimited)
    if !errors.Is(err, domainerrors.ErrRerankRateLimited) { t.Fatal(err) }
    if strings.Contains(err.Error(), "upstream secret body") { t.Fatal(err) }
}
```

- [x] **Step 2: 运行测试，确认合同尚未实现。**

Run: `go test ./internal/ports/rerank ./internal/adapters/rerank -count=1`

Expected: FAIL，缺少 package、接口和领域错误。

- [x] **Step 3: 实现最小 Port、Registry 和 typed error。**

```go
// ports/rerank/factory.go
type ProviderDecodeInput struct {
    Scope value.ModelScope
    Config, Credentials json.RawMessage
}
type ModelDecodeInput struct {
    ModelName string
    Parameters json.RawMessage
}
type ClientInput struct {
    ProviderID uuid.UUID
    Scope value.ModelScope
    Config map[string]any
    CredentialsJSON []byte
    ModelName string
    Parameters map[string]any
}
type Factory interface {
    Provider() string
    CredentialFields() []string
    DecodeProvider(ProviderDecodeInput) (map[string]any, []byte, error)
    DecodeModel(ModelDecodeInput) (map[string]any, error)
    NewClient(context.Context, ClientInput) (Client, error)
}
type FactoryRegistry interface {
    Factory(string) (Factory, error)
}
```

在 `domain/errors` 新增：

```go
ErrInvalidRerankResponse       = stderrors.New("供应商返回了无效重排结果")
ErrRerankUnavailable           = stderrors.New("重排服务暂时不可用")
ErrRerankRateLimited           = stderrors.New("重排服务请求过于频繁")
ErrRerankInputTooLarge         = stderrors.New("重排输入超过模型限制")
ErrRerankConfigurationConflict = stderrors.New("所选知识库的重排配置不一致")
ErrRerankSnapshotMismatch      = stderrors.New("重排模型配置与索引快照不一致")
```

`ProviderError` 只保存 `Provider` 和稳定 `Kind`；禁止保存原始 error/body。Registry 使用小写 trim key，nil、空 key、重复 key 均构造失败。

- [x] **Step 4: 验证 Port、Registry 和 errors.Is 映射。**

Run: `go test ./internal/ports/rerank ./internal/adapters/rerank ./internal/domain/errors -count=1`

Expected: PASS；error 字符串只包含 Provider key 与稳定分类。

- [x] **Step 5: 提交。**

```bash
git add internal/ports/rerank internal/adapters/rerank internal/domain/errors
git commit -m "feat(rerank): 定义重排端口与稳定错误"
```

### Task 3: 实现 `rerank_compatible` Provider 和 HTTP Client

**Files:**

- Create: `internal/adapters/rerank/compatible/factory.go`
- Create: `internal/adapters/rerank/compatible/factory_test.go`
- Create: `internal/adapters/rerank/compatible/client.go`
- Create: `internal/adapters/rerank/compatible/client_test.go`
- Modify: `internal/adapters/httpclient/ssrf_test.go`（仅在新增共享 header/redirect 合同需要覆盖时）

**Interfaces:**

- Consumes: Tasks 1–2 的 `providerutil` 与 `rerankport.Factory`。
- Produces: `compatible.NewFactory()`，供 Task 4 Provider/model lifecycle 与 runtime 装配使用。

- [x] **Step 1: 写 typed config、wire、retry 与安全失败测试。**

```go
func TestFactoryDecodesProviderAndModel(t *testing.T) {
    factory := compatible.NewFactory()
    config, credentials, err := factory.DecodeProvider(rerankport.ProviderDecodeInput{
        Scope: value.ModelScopeWorkspace,
        Config: json.RawMessage(`{"base_url":"https://rerank.example.com","endpoint_path":"/v1/rerank","timeout_seconds":30,"retry_times":2}`),
        Credentials: json.RawMessage(`{"api_key":"secret"}`),
    })
    if err != nil || config["endpoint_path"] != "/v1/rerank" || len(credentials) == 0 { t.Fatalf("%#v %s %v", config, credentials, err) }
    parameters, err := factory.DecodeModel(rerankport.ModelDecodeInput{
        ModelName: "BAAI/bge-reranker-v2-m3",
        Parameters: json.RawMessage(`{"max_documents":100,"max_query_chars":4096,"max_document_chars":8192}`),
    })
    if err != nil || parameters["max_documents"] != float64(100) { t.Fatalf("%#v %v", parameters, err) }
}

func TestClientRestoresIDsAndRejectsDuplicateIndexes(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/v1/rerank" || r.Header.Get("Authorization") != "Bearer secret" { t.Fatalf("%s %#v", r.URL.Path, r.Header) }
        _, _ = io.WriteString(w, `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.3}]}`)
    }))
    defer server.Close()
    client := compatibleTestClient(t, server.URL)
    got, err := client.Rerank(context.Background(), rerankport.RerankInput{
        Query: "query",
        Documents: []rerankport.Document{{ID:"a", Text:"A"}, {ID:"b", Text:"B"}},
        TopN: 2,
    })
    if err != nil || got.Items[0].DocumentID != "b" { t.Fatalf("%#v %v", got, err) }
}
```

另写表驱动用例覆盖 Workspace 私网 URL、endpoint path 含 scheme/query/`..`、reserved headers、401 不重试、429/500 重试、2 MiB body cap、invalid JSON、重复/越界/缺失 index、NaN/Inf、context cancel。

- [x] **Step 2: 运行 adapter 测试，确认 factory/client 尚未定义。**

Run: `go test ./internal/adapters/rerank/compatible -count=1`

Expected: FAIL，`compatible.NewFactory` 和 client 不存在。

- [x] **Step 3: 实现精确 wire contract 和有限重试。**

```go
type ProviderConfig struct {
    BaseURL string `json:"base_url"`
    EndpointPath string `json:"endpoint_path"`
    TimeoutSeconds int `json:"timeout_seconds"`
    RetryTimes int `json:"retry_times"`
}
type Credentials struct {
    APIKey string `json:"api_key"`
    CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}
type ModelParameters struct {
    MaxDocuments int `json:"max_documents"`
    MaxQueryChars int `json:"max_query_chars"`
    MaxDocumentChars int `json:"max_document_chars"`
}
type requestBody struct {
    Model string `json:"model"`
    Query string `json:"query"`
    Documents []string `json:"documents"`
    TopN int `json:"top_n"`
    ReturnDocuments bool `json:"return_documents"`
}
type responseBody struct {
    Results []struct {
        Index int `json:"index"`
        RelevanceScore float64 `json:"relevance_score"`
    } `json:"results"`
}
```

默认值固定为 `endpoint_path=/v1/rerank`、timeout 30、retry 2、max documents 100、max query 4096、max document 8192。Workspace 用 `providerutil.NewHTTPClient` 的公网 HTTPS policy；platform 使用 trusted client。client 对每次请求新建 body，429/5xx/瞬时网络错误按 `100ms, 200ms, 400ms` 上限退避，任何等待都 `select` context。响应通过 `io.LimitReader(resp.Body, 2<<20+1)` 限制，验证全量唯一 index 后映射回 opaque ID。

- [x] **Step 4: 运行完整安全与协议测试。**

Run: `go test ./internal/adapters/rerank/compatible ./internal/adapters/httpclient -count=1`

Expected: PASS；测试捕获的错误和日志字符串不含 `secret`、query 或文档正文。

- [x] **Step 5: 提交。**

```bash
git add internal/adapters/rerank/compatible internal/adapters/httpclient
git commit -m "feat(rerank): 实现兼容重排 Provider"
```

### Task 4: 通用化 Provider/Model CRUD 与连接测试

**Files:**

- Modify: `internal/application/service/provider_factory_resolver.go`
- Modify: `internal/application/service/model_provider.go`
- Modify: `internal/application/service/model.go`
- Modify: `internal/application/service/model_connection.go`
- Modify: `internal/application/service/model_repository.go`
- Modify: `internal/application/dto/model.go`
- Modify: `internal/interfaces/http/model_provider_handler.go`
- Modify: `internal/interfaces/http/model_handler.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/application/service/model_provider_test.go`
- Modify: `internal/application/service/model_test.go`
- Modify: `internal/application/service/model_connection_test_test.go`
- Modify: `internal/interfaces/http/model_routes_test.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `cmd/langhuan/main_test.go`

**Interfaces:**

- Consumes: Task 3 `rerank_compatible` factory。
- Produces: capability-aware Provider options、`type=rerank` Model CRUD/test、nullable dimensions、直接按类型列可选模型 API。

- [x] **Step 1: 写 service 和 HTTP 失败测试。**

```go
func TestModelServiceCreatesRerankWithoutDimensions(t *testing.T) {
    service := NewModelService(providers, models, embeddingRegistry, rerankRegistry)
    got, err := service.CreateWorkspace(ctx, workspaceID, CreateModelInput{
        ProviderID: providerID, ActorID: actorID,
        Name: "bge_reranker", DisplayName: "BGE Reranker",
        Type: value.ModelTypeRerank, ModelName: "BAAI/bge-reranker-v2-m3",
        Parameters: json.RawMessage(`{"max_documents":100,"max_query_chars":4096,"max_document_chars":8192}`),
    })
    if err != nil || got.Type != value.ModelTypeRerank || got.Dimensions != nil { t.Fatalf("%#v %v", got, err) }
}

func TestProviderOptionsExposeCapabilities(t *testing.T) {
    rec := request(t, http.MethodGet, "/api/v1/workspaces/acme/model-providers/options", nil)
    assertJSON(t, rec, http.StatusOK, `{"providers":[{"key":"openai","capabilities":["embedding"]},{"key":"rerank_compatible","capabilities":["rerank"]}]}`)
}

func TestConnectionTestRerankReturnsResultCount(t *testing.T) {
    got, err := service.TestWorkspace(ctx, workspaceID, rerankModelID)
    if err != nil || got.Type != value.ModelTypeRerank || got.ResultCount == nil || *got.ResultCount != 2 || got.Dimensions != nil { t.Fatalf("%#v %v", got, err) }
}
```

再覆盖：Rerank 传 dimensions 被拒绝、Embedding 缺 dimensions 被拒绝、LLM 继续 unsupported、`GET /models?type=rerank&active=true` 只返回当前 Workspace 可见 active 项。

- [x] **Step 2: 运行聚焦测试，确认现有 service 只接受 Embedding。**

Run: `go test ./internal/application/service ./internal/interfaces/http ./cmd/langhuan -run 'Rerank|ProviderOptions|SelectableModels' -count=1`

Expected: FAIL，`CreateModelInput.Dimensions` 仍是 int、resolver 没有 Rerank registry、options 仍是 string array。

- [x] **Step 3: 实现 capability 路由和 nullable DTO。**

锁定构造函数：

```go
func NewModelService(
    providers ModelProviderRepository,
    models ModelRepository,
    embeddings embeddingport.FactoryRegistry,
    reranks rerankport.FactoryRegistry,
) *ModelService

func NewModelConnectionTestService(
    models ModelRepository,
    cipher embeddingport.CredentialCipher,
    embeddings embeddingport.FactoryRegistry,
    reranks rerankport.FactoryRegistry,
) *ModelConnectionTestService

func NewProviderFactoryResolver(
    embeddings embeddingport.FactoryRegistry,
    reranks rerankport.FactoryRegistry,
    parsers *ParserRegistryAdapter,
    supported ...ProviderFactoryInfo,
) *ProviderFactoryResolver
```

`CreateModelInput.Dimensions`、`UpdateModelInput.Dimensions` 和 `dto.Model.Dimensions` 改为 `*int`。Model service 对 type switch：Embedding 调 embedding factory 并要求 dimensions；Rerank 调 rerank factory 并要求 nil；LLM 返回 `ErrUnsupportedModelType`。连接测试 Rerank 固定传两个文档，验证数量/ID/有限 score，不断言语义顺序。

Provider options DTO：

```go
type providerOption struct {
    Key string `json:"key"`
    Capabilities []string `json:"capabilities"`
}
type providerOptionsResponse struct {
    Providers []providerOption `json:"providers"`
}
```

新增 `GET /api/v1/workspaces/:workspace_slug/models?type=&active=` handler，调用既有 `ModelRepository.ListVisible`；type 必须是 `embedding|rerank`，active 默认 false。`main.go` 构造并注入 `rerankadapter.NewRegistry(compatible.NewFactory())`。

- [x] **Step 4: 验证模型生命周期和既有 Embedding/Parser 回归。**

Run: `go test ./internal/application/service ./internal/interfaces/http ./cmd/langhuan -count=1`

Expected: PASS；OpenAI/ARK/Ollama/DashScope/TencentCloud/MinerU 现有管理测试不回归，Rerank CRUD/test 和类型列表通过。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/application/dto internal/interfaces/http cmd/langhuan
git commit -m "feat(model): 支持 Rerank 模型生命周期"
```

### Task 5: 持久化 Generation Rerank 快照与引用约束

**Files:**

- Create: `internal/domain/value/rerank.go`
- Create: `internal/domain/value/rerank_test.go`
- Create: `internal/infrastructure/migrate/migrations/000014_rerank_generation_snapshot.up.sql`
- Create: `internal/infrastructure/migrate/migrations/000014_rerank_generation_snapshot.down.sql`
- Create: `internal/infrastructure/migrate/migrate_v014_rerank_integration_test.go`
- Modify: `internal/domain/model/index_generation.go`
- Modify: `internal/domain/model/index_generation_test.go`
- Modify: `internal/infrastructure/db/knowledge_rows.go`
- Modify: `internal/infrastructure/db/knowledge_v2_codec.go`
- Modify: `internal/infrastructure/db/knowledge_v2_codec_test.go`
- Modify: `internal/infrastructure/db/model_repository.go`
- Modify: `internal/infrastructure/db/model_provider_repository.go`
- Modify: `internal/infrastructure/db/model_repository_integration_test.go`
- Modify: `internal/infrastructure/db/model_provider_repository_integration_test.go`

**Interfaces:**

- Consumes: Task 4 nullable Rerank Model。
- Produces: `value.RerankFailureMode`、`value.RankingStage`、`model.RerankSnapshot`、Row codec、Generation/Provider 引用统计。

- [x] **Step 1: 写领域和真实 PostgreSQL 失败测试。**

```go
func TestRerankSnapshotValidation(t *testing.T) {
    snapshot := &model.RerankSnapshot{
        ModelID: uuid.New(), ProviderID: uuid.New(), ModelName: "rerank",
        ModelConfigHash: "hash", CandidateTopK: 50,
        FailureMode: value.RerankFailureFallback,
    }
    if err := snapshot.Validate(); err != nil { t.Fatal(err) }
    snapshot.CandidateTopK = 49
    if !errors.Is(snapshot.Validate(), domainerrors.ErrValidation) { t.Fatal("want validation") }
}

func TestV014RejectsPartialRerankSnapshot(t *testing.T) {
    db := integrationDatabase(t)
    result := db.Exec(`UPDATE knowledge_base_index_generations SET rerank_model_id = ? WHERE id = ?`, modelID, generationID)
    if result.Error == nil { t.Fatal("partial snapshot must fail") }
}

func TestModelReferenceCountIncludesEmbeddingAndRerank(t *testing.T) {
    seedGeneration(t, db, embeddingModelID, rerankModelID)
    if got := countGenerationReferences(t, repo, rerankModelID); got != 1 { t.Fatalf("got %d", got) }
}
```

- [x] **Step 2: 构建测试镜像并运行，确认列和领域类型不存在。**

Run: `make test-image && go test -tags=integration -p 1 ./internal/infrastructure/migrate ./internal/infrastructure/db ./internal/domain/model ./internal/domain/value -run 'V014|RerankSnapshot|ReferenceCount' -count=1`

Expected: FAIL，迁移列、snapshot 或新引用统计未定义。

- [x] **Step 3: 实现迁移、领域快照和 codec。**

Up migration 核心 SQL：

```sql
ALTER TABLE knowledge_base_index_generations
  ADD COLUMN rerank_model_id uuid REFERENCES models(id) ON DELETE RESTRICT,
  ADD COLUMN rerank_provider_id uuid REFERENCES model_providers(id) ON DELETE RESTRICT,
  ADD COLUMN rerank_model_name text,
  ADD COLUMN rerank_model_config_hash text,
  ADD COLUMN rerank_config jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE knowledge_base_index_generations
  ADD CONSTRAINT index_generations_rerank_config_object_check
    CHECK (jsonb_typeof(rerank_config) = 'object'),
  ADD CONSTRAINT index_generations_rerank_snapshot_shape_check CHECK (
    (rerank_model_id IS NULL AND rerank_provider_id IS NULL AND rerank_model_name IS NULL
      AND rerank_model_config_hash IS NULL AND rerank_config = '{}'::jsonb)
    OR
    (rerank_model_id IS NOT NULL AND rerank_provider_id IS NOT NULL
      AND btrim(rerank_model_name) <> '' AND btrim(rerank_model_config_hash) <> ''
      AND rerank_config ? 'candidate_top_k' AND rerank_config ? 'failure_mode')
  );

CREATE INDEX idx_index_generations_rerank_model_id
  ON knowledge_base_index_generations (rerank_model_id) WHERE rerank_model_id IS NOT NULL;
CREATE INDEX idx_index_generations_rerank_provider_id
  ON knowledge_base_index_generations (rerank_provider_id) WHERE rerank_provider_id IS NOT NULL;
```

Down migration 按 `indexes -> constraints -> columns` 逆序删除。`RerankSnapshot.Validate` 校验 UUID、trim name/hash、candidate `50..200`、failure `fallback|fail`。Row 将 snapshot 映射成四列 + `{"candidate_top_k":N,"failure_mode":"..."}`。

Repository 接口改名并统一语义：

```go
CountGenerationReferences(context.Context, uuid.UUID) (int64, error)
CountProviderGenerationReferences(context.Context, uuid.UUID) (int64, error)
```

模型计数 SQL 使用 `embedding_model_id = ? OR rerank_model_id = ?`；Provider 计数使用 `provider_id = ? OR rerank_provider_id = ?`。Model parameters 或 Provider config semantic change 且 count > 0 时返回 `ErrImmutableModelField`；credential-only update 允许。

- [x] **Step 4: 验证 migration、FK、CHECK、codec 和引用不可变性。**

Run: `go test ./internal/domain/model ./internal/domain/value -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/migrate ./internal/infrastructure/db -run 'V014|Rerank|GenerationReferences' -count=1`

Expected: PASS；既有 Generation 读出 `Rerank=nil`，partial snapshot 和删除被引用模型/Provider 均失败。

- [x] **Step 5: 提交。**

```bash
git add internal/domain internal/infrastructure/migrate internal/infrastructure/db
git commit -m "feat(db): 持久化 Generation 重排快照"
```

### Task 6: 扩展 Generation 创建、继承、关闭和 API 合同

**Files:**

- Modify: `internal/application/service/index_generation.go`
- Modify: `internal/application/service/knowledge_base_generation.go`
- Modify: `internal/application/service/config_hash.go`
- Modify: `internal/application/service/index_generation_test.go`
- Modify: `internal/application/service/config_hash_test.go`
- Modify: `internal/application/dto/index_generation.go`
- Modify: `internal/application/dto/index_generation_test.go`
- Modify: `internal/interfaces/http/index_generation_handler.go`
- Modify: `internal/interfaces/http/index_generation_handler_test.go`
- Modify: `internal/infrastructure/db/index_generation_store.go`
- Modify: `internal/infrastructure/db/index_generation_store_integration_test.go`

**Interfaces:**

- Consumes: Task 5 `RerankSnapshot` 和 Row。
- Produces: `RerankSelection` 请求语义、同事务 typed model selection、Generation DTO `rerank`。

- [x] **Step 1: 写继承/关闭/启用和事务选择失败测试。**

```go
func TestCreateGenerationInheritsRerankWhenSelectionOmitted(t *testing.T) {
    base := readyGenerationWithRerank(t, rerankModelID, 50, value.RerankFailureFallback)
    got := createGeneration(t, service, base, CreateIndexGenerationInput{WorkspaceID: wsID, KnowledgeBaseID: kbID, ActorRole: value.RoleAdmin})
    if got.Rerank == nil || got.Rerank.ModelID != rerankModelID { t.Fatalf("%#v", got.Rerank) }
}

func TestCreateGenerationExplicitlyDisablesRerank(t *testing.T) {
    input := CreateIndexGenerationInput{
        WorkspaceID: wsID, KnowledgeBaseID: kbID, ActorRole: value.RoleAdmin,
        Rerank: &RerankSelection{Enabled: false},
    }
    got := createGeneration(t, service, readyGenerationWithRerank(t, rerankModelID, 50, value.RerankFailureFallback), input)
    if got.Rerank != nil { t.Fatalf("%#v", got.Rerank) }
}

func TestCreateGenerationRejectsCandidateAboveModelLimit(t *testing.T) {
    input := enabledRerankSelection(rerankModelID, 200, value.RerankFailureFallback)
    tx := generationTxWithRerankMaxDocuments(100)
    _, err := service.Create(ctx, createInput(input, tx))
    if !errors.Is(err, domainerrors.ErrValidation) { t.Fatal(err) }
}
```

HTTP 测试覆盖省略、`{"enabled":false}`、enabled 缺字段、candidate 49/201、非法 failure mode、跨 Workspace/非 rerank model。

- [x] **Step 2: 运行测试，确认 Generation 尚无 Rerank input/output。**

Run: `go test ./internal/application/service ./internal/application/dto ./internal/interfaces/http -run 'Generation.*Rerank|CandidateAbove' -count=1`

Expected: FAIL，`RerankSelection`、DTO 字段和 tx resolver 不存在。

- [x] **Step 3: 实现显式三态输入并在 Generation transaction 内解析模型。**

```go
type RerankSelection struct {
    Enabled bool `json:"enabled"`
    ModelID uuid.UUID `json:"model_id"`
    CandidateTopK int `json:"candidate_top_k"`
    FailureMode value.RerankFailureMode `json:"failure_mode"`
}

type CreateIndexGenerationInput struct {
    WorkspaceID, KnowledgeBaseID uuid.UUID
    EmbeddingModelID uuid.UUID
    ChunkingConfig *value.ChunkingConfig
    RetrievalConfig *RetrievalConfig
    Rerank *RerankSelection
    ActorRole value.WorkspaceRole
}
```

在 `IndexGenerationTx` 新增：

```go
ResolveSelectableModel(context.Context, uuid.UUID, value.ModelType) (*model.ResolvedModel, error)
```

DB 实现必须使用 transaction 的 `tx.db` + `FOR SHARE`，校验 Workspace 可见、Provider/Model active、精确 ModelType。Embedding 和 Rerank 都在同一 transaction 解析后再写 Generation/Job，禁止回到外层 repository。

`rerank=nil` 从 base snapshot 取得 model ID/config，然后重新解析当前模型并重算 hash；disabled 写 nil；enabled 校验 Model parameters 的 `max_documents >= candidate_top_k`。初始 KnowledgeBase Generation 固定 `Rerank=nil`。总 `config_hash` 加入 normalized rerank snapshot；credentials 排除。

DTO：

```go
type IndexGenerationRerank struct {
    ModelID uuid.UUID `json:"model_id"`
    ProviderID uuid.UUID `json:"provider_id"`
    ModelName string `json:"model_name"`
    CandidateTopK int `json:"candidate_top_k"`
    FailureMode value.RerankFailureMode `json:"failure_mode"`
}
// IndexGeneration.Rerank *IndexGenerationRerank `json:"rerank,omitempty"`
```

DTO 的可读名称固定使用 Generation 已保存的 `ModelName`，与现有 Embedding Generation 展示规则一致；不查询可变的 Model `display_name`，普通 DTO 也不返回 config hash。

- [x] **Step 4: 验证 Generation 服务、HTTP strict JSON 与数据库事务。**

Run: `go test ./internal/application/service ./internal/application/dto ./internal/interfaces/http -run 'Generation|Rerank' -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'Generation.*Rerank' -count=1`

Expected: PASS；inherit/disable/enable 和 typed lock 都通过，跨租户统一 not found/model not visible。

- [x] **Step 5: 提交。**

```bash
git add internal/application internal/interfaces/http internal/infrastructure/db
git commit -m "feat(generation): 固化重排模型配置"
```

### Task 7: 实现 Rerank Resolver 并校验 Embedding/Rerank config hash

**Files:**

- Create: `internal/application/service/rerank_client_resolver.go`
- Create: `internal/application/service/rerank_client_resolver_test.go`
- Modify: `internal/application/service/embedding_client_resolver.go`
- Modify: `internal/application/service/embedding_client_resolver_test.go`
- Modify: `internal/application/service/config_hash.go`
- Modify: `internal/application/service/config_hash_test.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `cmd/langhuan/main_test.go`

**Interfaces:**

- Consumes: Tasks 3–6 的 factory、model repo 和 Generation hash。
- Produces: `ResolvedRerankClient` 与增强后的 `ResolvedEmbeddingClient.ModelConfigHash`，供 Tasks 8–10。

- [x] **Step 1: 写 resolver 和漂移失败测试。**

```go
func TestRerankResolverBuildsVisibleActiveClientAndHash(t *testing.T) {
    resolver := NewRerankClientResolver(models, cipher, rerankRegistry)
    got, err := resolver.Resolve(ctx, workspaceID, rerankModelID)
    if err != nil { t.Fatal(err) }
    if got.Client == nil || got.ModelID != rerankModelID || got.MaxDocuments != 100 || got.ModelConfigHash == "" { t.Fatalf("%#v", got) }
}

func TestEmbeddingResolverReturnsGenerationComparableHash(t *testing.T) {
    got, err := resolver.Resolve(ctx, workspaceID, embeddingModelID)
    if err != nil { t.Fatal(err) }
    want, err := modelConfigHash(resolvedModel)
    if err != nil || got.ModelConfigHash != want { t.Fatalf("got=%q want=%q err=%v", got.ModelConfigHash, want, err) }
}

func TestRerankResolverRejectsEmbeddingModel(t *testing.T) {
    _, err := resolver.Resolve(ctx, workspaceID, embeddingModelID)
    if !errors.Is(err, domainerrors.ErrUnsupportedModelType) { t.Fatal(err) }
}
```

- [x] **Step 2: 运行测试，确认 Rerank resolver 和 Embedding hash 字段不存在。**

Run: `go test ./internal/application/service -run 'RerankResolver|EmbeddingResolverReturnsGenerationComparableHash' -count=1`

Expected: FAIL，缺少 resolver/field。

- [x] **Step 3: 实现共享 hash 与 resolver。**

```go
func modelConfigHash(resolved *model.ResolvedModel) (string, error) {
    if resolved == nil || resolved.Model == nil || resolved.Provider == nil {
        return "", fmt.Errorf("%w: 模型快照无效", domainerrors.ErrValidation)
    }
    input := map[string]any{
        "provider": resolved.Provider.Provider,
        "provider_config": resolved.Provider.Config,
        "model_name": resolved.Model.ModelName,
        "parameters": resolved.Model.Parameters,
    }
    if resolved.Model.Type == value.ModelTypeEmbedding {
        if resolved.Model.Dimensions == nil { return "", domainerrors.ErrUnsupportedEmbeddingDimension }
        input["dimensions"] = *resolved.Model.Dimensions
    }
    return CanonicalConfigHash(input)
}
```

`embeddingClientResolver.Resolve` 返回该 hash。`rerankClientResolver.Resolve` 重用可见性/active/解密逻辑，调用 `rerankRegistry.Factory(providerKey).NewClient`，严格解析 `max_documents/max_query_chars/max_document_chars` 为整数并返回锁定结构。credential plaintext 用 `defer clearBytes` 清除。

`main.go` 构造一个 resolver 实例并注入 Search/MultiSearch；不得每次搜索重新创建 registry。

- [x] **Step 4: 验证 resolver、hash 确定性和凭证错误链。**

Run: `go test ./internal/application/service ./cmd/langhuan -run 'Resolver|ConfigHash|Rerank' -count=1`

Expected: PASS；map key 顺序不影响 hash，credential rotation 不改变 hash，Provider config/model parameters 改变 hash。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service cmd/langhuan
git commit -m "feat(rerank): 解析运行时重排客户端"
```

### Task 8: 接入单知识库 Rerank、private 文本与 fallback

**Files:**

- Create: `internal/application/service/search_rerank.go`
- Create: `internal/application/service/search_rerank_test.go`
- Modify: `internal/application/service/search.go`
- Modify: `internal/application/service/search_test.go`
- Modify: `internal/application/dto/search.go`
- Modify: `internal/application/dto/search_test.go`
- Modify: `internal/ports/index/index.go`
- Modify: `internal/infrastructure/db/retrieval_search_repository.go`
- Modify: `internal/infrastructure/db/retrieval_search_integration_test.go`

**Interfaces:**

- Consumes: Task 7 resolvers 与 Task 5 Generation snapshot。
- Produces: 单库 `RRF -> parent grouping -> candidate cap -> rerank -> final cap`，以及 `rerank_score/ranking_stage`。

- [x] **Step 1: 写顺序、FAQ/private text、漂移与 fallback 失败测试。**

```go
func TestSearchReranksAfterParentGrouping(t *testing.T) {
    svc, reranker := searchFixtureWithRerank(t, value.RerankFailureFallback)
    got, err := svc.Search(ctx, SearchInput{WorkspaceID: wsID, KnowledgeBaseID: kbID, Query: "退款"})
    if err != nil { t.Fatal(err) }
    if reranker.Calls != 1 || len(reranker.Input.Documents) != 2 { t.Fatalf("calls=%d docs=%#v", reranker.Calls, reranker.Input.Documents) }
    if got[0].ChunkID != secondParentID || got[0].RerankScore == nil || got[0].RankingStage != value.RankingStageRerank { t.Fatalf("%#v", got) }
}

func TestFAQUsesQuestionsAsRerankText(t *testing.T) {
    candidates := []rankableSearchResult{faqRankable("问题一\n问题二", "答案正文")}
    docs, err := buildRerankDocuments(candidates, 8192)
    if err != nil { t.Fatal(err) }
    if strings.Contains(docs[0].Text, "答案正文") || docs[0].Text != "问题一\n问题二" { t.Fatalf("%q", docs[0].Text) }
}

func TestSearchFallsBackOnlyForRemoteRerankErrors(t *testing.T) {
    svc := searchFixtureRerankError(t, domainerrors.ErrRerankUnavailable, value.RerankFailureFallback)
    got, err := svc.Search(ctx, validSearchInput())
    if err != nil || got[0].RankingStage != value.RankingStageRRFFallback { t.Fatalf("%#v %v", got, err) }
    svc = searchFixtureSnapshotHash(t, "generation-hash", "runtime-hash")
    if _, err := svc.Search(ctx, validSearchInput()); !errors.Is(err, domainerrors.ErrRerankSnapshotMismatch) { t.Fatal(err) }
}
```

Integration test asserts `SearchEvidence.SearchContent` comes from `re.search_content` while `Content` remains parent/answer return content。

- [x] **Step 2: 运行测试，确认当前 Search 在 parent grouping 后直接 final truncate。**

Run: `go test ./internal/application/service ./internal/application/dto -run 'Rerank|FAQUsesQuestions|FallsBack' -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/db -run SearchContent -count=1`

Expected: FAIL，缺少 private SearchContent、rankable intermediate 和新 DTO 字段。

- [x] **Step 3: 实现单库重排算法。**

```go
type rankableSearchResult struct {
    Result *dto.SearchResult
    MatchedSearchContent []string
}

type SearchServiceDeps struct {
    Repository indexport.SearchRepository
    Resolver EmbeddingClientResolver
    RerankResolver RerankClientResolver
    Logger *slog.Logger
}
```

`SearchEvidence` 新增 `SearchContent string`，DB select `re.search_content AS search_content`。构造 Rerank text：FAQ 只拼 questions；File/Web 先按 RRF 顺序去重 matched search content，再用 `\n\n---\n\n` 补 parent content；flat 使用 search content；按 rune 截断，命中片段优先。

在 `Search` 中：

1. active Generation 后立即 resolve/比对 Embedding hash；启用 Rerank 时 resolve/比对五个 snapshot 字段和 hash。
2. 完成 RRF、evidence load、parent grouping，但不 final truncate。
3. 取前 `min(candidate_top_k,len(grouped))`，opaque ID 使用 `candidate-000001` 稳定生成。
4. 调 `Rerank`，按 DocumentID 赋分。
5. 排序 `rerank DESC, RRF DESC, chunk UUID ASC`，再 final truncate。

DTO：

```go
RerankScore *float64 `json:"rerank_score,omitempty"`
RankingStage value.RankingStage `json:"ranking_stage"`
```

Rerank 关闭为 `rrf`。远端 typed error + fallback 为 `rrf_fallback`；`fail` 返回稳定 error。context cancellation 和 snapshot mismatch 永不 fallback。

- [x] **Step 4: 验证单库算法与真实 evidence 读取。**

Run: `go test ./internal/application/service ./internal/application/dto -run 'Search|Rerank|FAQ' -count=1 && go test -tags=integration -p 1 ./internal/infrastructure/db -run 'LoadEvidence|SearchContent' -count=1`

Expected: PASS；Rerank client 每次最多一次，provider 输入没有 UUID/文件名/锚点，API DTO 没有 private text。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/application/dto internal/ports/index internal/infrastructure/db
git commit -m "feat(search): 在混合召回后执行重排"
```

### Task 9: 接入多知识库一致性、REST 与 MCP 合同

**Files:**

- Modify: `internal/application/service/multi_knowledge_search.go`
- Modify: `internal/application/service/multi_knowledge_search_test.go`
- Modify: `internal/interfaces/http/search_handler.go`
- Modify: `internal/interfaces/http/search_handler_test.go`
- Modify: `internal/interfaces/http/errors.go`
- Modify: `internal/interfaces/http/errors_test.go`
- Modify: `internal/interfaces/mcp/contracts.go`
- Modify: `internal/interfaces/mcp/tools.go`
- Modify: `internal/interfaces/mcp/adapters.go`
- Modify: `internal/interfaces/mcp/adapters_test.go`
- Modify: `internal/interfaces/mcp/server_test.go`

**Interfaces:**

- Consumes: Task 8 `search_rerank.go` 共享执行函数和 DTO。
- Produces: 多库 compatibility key、全局一次 Rerank、稳定 HTTP/MCP error/fields。

- [x] **Step 1: 写多库矩阵和协议失败测试。**

```go
func TestMultiSearchRerankCompatibilityMatrix(t *testing.T) {
    tests := []struct {
        name string
        snapshots []*model.RerankSnapshot
        wantCalls int
        wantErr error
    }{
        {name:"all disabled", snapshots:[]*model.RerankSnapshot{nil,nil}, wantCalls:0},
        {name:"same snapshot", snapshots:[]*model.RerankSnapshot{sameRerank(),sameRerank()}, wantCalls:1},
        {name:"mixed enabled", snapshots:[]*model.RerankSnapshot{sameRerank(),nil}, wantErr:domainerrors.ErrRerankConfigurationConflict},
        {name:"different model", snapshots:[]*model.RerankSnapshot{sameRerank(),otherRerank()}, wantErr:domainerrors.ErrRerankConfigurationConflict},
    }
    for _, tt := range tests { t.Run(tt.name, func(t *testing.T) { assertMultiRerank(t, tt.snapshots, tt.wantCalls, tt.wantErr) }) }
}

func TestSearchHTTPMapsRerankConflict(t *testing.T) {
    rec := searchRequestWithError(t, domainerrors.ErrRerankConfigurationConflict)
    assertJSON(t, rec, http.StatusConflict, `{"error":{"code":"rerank_configuration_conflict","message":"所选知识库的重排配置不一致，请统一配置或分开检索"}}`)
}

func TestMCPKnowledgeSearchReturnsRankingStage(t *testing.T) {
    result := callKnowledgeSearch(t, rerankedSearchResults())
    if !strings.Contains(resultJSON(t, result), `"ranking_stage":"rerank"`) { t.Fatal(result) }
}
```

- [x] **Step 2: 运行测试，确认多库没有 Rerank consistency gate。**

Run: `go test ./internal/application/service ./internal/interfaces/http ./internal/interfaces/mcp -run 'Multi.*Rerank|RerankConflict|RankingStage' -count=1`

Expected: FAIL，compatibility key/error mapping/MCP output 未实现。

- [x] **Step 3: 实现严格 compatibility key 和协议映射。**

```go
type rerankCompatibilityKey struct {
    Enabled bool
    ModelID, ProviderID uuid.UUID
    ModelName, ModelConfigHash string
    CandidateTopK int
    FailureMode value.RerankFailureMode
}
```

`loadSnapshots` 后、`embedGroups` 前计算 key；只要不同立即 `ErrRerankConfigurationConflict`，保证没有 Embedding/Rerank 外部调用。完全相同且启用时，现有 per-KB RRF -> global merge -> evidence -> parent grouping 后调用共享 `applyRerank` 一次，再全局 final truncate。结果稳定 tie-break 为 `rerank/RRF DESC, KB UUID ASC, chunk UUID ASC`。

HTTP error mapping按 spec：conflict/snapshot 409、unavailable/rate limited 503、invalid response 502、input too large 400。MCP `knowledge_search` 输入不加 model override；输出 schema/description 增加 optional `rerank_score` 和 required `ranking_stage`，fallback 仍是成功 tool result。

- [x] **Step 4: 验证单库/多库 REST/MCP 一致。**

Run: `go test ./internal/application/service ./internal/interfaces/http ./internal/interfaces/mcp -run 'Search|Rerank|KnowledgeSearch' -count=1`

Expected: PASS；配置冲突发生在任何外部调用前，同快照多库只有一次 Rerank call。

- [x] **Step 5: 提交。**

```bash
git add internal/application/service internal/interfaces/http internal/interfaces/mcp
git commit -m "feat(search): 统一多库重排与协议合同"
```

### Task 10: 增加 Request ID 与检索结构化日志

**Files:**

- Create: `internal/application/requestmeta/context.go`
- Create: `internal/application/requestmeta/context_test.go`
- Create: `internal/interfaces/http/request_id.go`
- Create: `internal/interfaces/http/request_id_test.go`
- Create: `internal/application/service/search_observability_test.go`
- Modify: `internal/application/service/search.go`
- Modify: `internal/application/service/search_rerank.go`
- Modify: `internal/application/service/multi_knowledge_search.go`
- Modify: `internal/application/service/model_connection.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/search_handler.go`
- Modify: `internal/interfaces/mcp/adapters.go`
- Modify: `cmd/langhuan/main.go`
- Modify: `cmd/langhuan/main_test.go`

**Interfaces:**

- Consumes: Tasks 8–9 搜索阶段与 logger dependency。
- Produces: REST/MCP 共享 request ID、一个 terminal search event、fallback Warn 和 Debug Rerank call event。

- [x] **Step 1: 写 middleware 与内存 slog 合同测试。**

```go
func TestRequestIDMiddlewareAcceptsValidAndReplacesInvalid(t *testing.T) {
    router := gin.New()
    router.Use(RequestID())
    router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, requestmeta.From(c.Request.Context()).RequestID) })
    valid := perform(t, router, "valid-id:42")
    if valid.Header().Get("X-Request-ID") != "valid-id:42" { t.Fatal(valid.Header()) }
    invalid := perform(t, router, "contains spaces and / slash")
    if _, err := uuid.Parse(invalid.Header().Get("X-Request-ID")); err != nil { t.Fatal(invalid.Header()) }
}

func TestSearchLogsOneTerminalEventWithoutSensitiveText(t *testing.T) {
    handler, records := captureSlog()
    logger := slog.New(handler)
    svc := observableSearchFixture(logger, "绝密退款问题", "绝密文档正文")
    _, err := svc.Search(requestmeta.With(context.Background(), requestmeta.Meta{RequestID:"req-1",Transport:"rest",PrincipalKind:"user"}), validSearchInput())
    if err != nil { t.Fatal(err) }
    got := records.JSON()
    if records.CountEvent("search.completed") != 1 || records.CountEvent("search.failed") != 0 { t.Fatal(got) }
    for _, secret := range []string{"绝密退款问题","绝密文档正文","api-key"} {
        if strings.Contains(got, secret) { t.Fatalf("leaked %q: %s", secret, got) }
    }
}
```

另写 fallback、fail、empty、多库并发 wall-clock、连接测试事件测试。

- [x] **Step 2: 运行测试，确认 request metadata 和 event 不存在。**

Run: `go test ./internal/application/requestmeta ./internal/application/service ./internal/interfaces/http -run 'RequestID|LogsOneTerminal|Observability' -count=1`

Expected: FAIL，缺少 middleware/context，Search 未注入 logger 或未产生日志。

- [x] **Step 3: 实现 context metadata、middleware 和单一 terminal event。**

```go
type Meta struct {
    RequestID string
    Transport string
    PrincipalKind string
}
func With(ctx context.Context, meta Meta) context.Context
func From(ctx context.Context) Meta
```

`RequestID` 只接受 `^[A-Za-z0-9._:-]{1,64}$`，否则 `id.New().String()`，响应始终回传。REST handler 在调用 service 前补 `transport=rest/principal_kind`；MCP adapter 补 `transport=mcp/api_key`。

`SearchService`、`MultiKnowledgeSearchService`、`ModelConnectionTestService` 构造函数接收 `*slog.Logger`，nil 时仅测试回退 `slog.Default()`。用一个 `searchRunStats` 在 application 收集 counts/durations，顶层 `defer` 根据最终 error 只写一个 `search.completed` 或 `search.failed`。fallback 额外 Warn `search.rerank_fallback`，远端调用写 Debug `rerank.call.completed/failed`。

日志字段严格使用 spec 第 14 节 allowlist；只记录 `query_chars`，不得记录 query/hash。并发阶段 duration 是 wall clock；错误日志只写清洗后的 `error_class`。

- [x] **Step 4: 验证所有日志事件和 HTTP/MCP request ID。**

Run: `go test ./internal/application/requestmeta ./internal/application/service ./internal/interfaces/http ./internal/interfaces/mcp ./cmd/langhuan -run 'RequestID|Observability|SearchLogs|ConnectionTestLogs' -count=1`

Expected: PASS；成功/空/fallback/fail 都只有一个 terminal event，序列化记录无敏感测试字符串。

- [x] **Step 5: 提交。**

```bash
git add internal/application/requestmeta internal/application/service internal/interfaces cmd/langhuan
git commit -m "feat(log): 记录检索重排结构化事件"
```

### Task 11: Web Console 支持 Rerank Provider 与模型管理

**Files:**

- Create: `web/src/features/models/schemas/rerank-compatible.ts`
- Create: `web/src/features/models/components/embedding-model-fields.tsx`
- Create: `web/src/features/models/components/rerank-model-fields.tsx`
- Modify: `web/src/features/models/types.ts`
- Modify: `web/src/features/models/schemas/common.ts`
- Modify: `web/src/features/models/schemas/index.ts`
- Modify: `web/src/features/models/schemas/schemas.test.ts`
- Modify: `web/src/features/models/components/provider-fields.tsx`
- Modify: `web/src/features/models/components/provider-form-data.ts`
- Modify: `web/src/features/models/components/provider-form.tsx`
- Modify: `web/src/features/models/components/provider-form.test.tsx`
- Modify: `web/src/features/models/components/model-form.tsx`
- Modify: `web/src/features/models/components/model-form.test.tsx`
- Modify: `web/src/features/models/components/model-card.tsx`
- Modify: `web/src/features/models/components/model-card.test.tsx`
- Modify: `web/src/features/models/components/model-provider-detail-content.tsx`
- Modify: `web/src/features/models/api.ts`
- Modify: `web/src/features/models/api.test.ts`
- Modify: `web/src/features/models/queries.ts`
- Modify: `web/src/features/models/cache.ts`
- Modify: `web/src/lib/i18n/locales/zh/models.ts`
- Modify: `web/src/lib/i18n/locales/en/models.ts`

**Interfaces:**

- Consumes: Task 4 Provider options、nullable dimensions、typed selectable endpoint。
- Produces: capability-aware连接表单、Rerank 模型 CRUD/test、按 type 的 selectable Query。

- [x] **Step 1: 写 Zod、表单、卡片和 API 失败测试。**

```tsx
it('submits a rerank model without dimensions', async () => {
  const screen = await render(<ModelForm provider={rerankProvider} scope='workspace' workspaceSlug='acme' />)
  await user.type(screen.getByLabelText('模型标识'), 'bge_reranker')
  await user.type(screen.getByLabelText('显示名称'), 'BGE Reranker')
  await user.type(screen.getByLabelText('供应商模型名称'), 'BAAI/bge-reranker-v2-m3')
  await user.click(screen.getByRole('button', { name: '保存模型' }))
  expect(createModelMock).toHaveBeenCalledWith('workspace', rerankProvider.id, expect.objectContaining({
    type: 'rerank',
    model_name: 'BAAI/bge-reranker-v2-m3',
    parameters: { max_documents: 100, max_query_chars: 4096, max_document_chars: 8192 },
  }), 'acme')
  expect(createModelMock.mock.calls[0]?.[2]).not.toHaveProperty('dimensions')
})

it('renders rerank card semantics', async () => {
  const screen = await render(<ModelCard model={rerankModel} canManage />)
  await expect.element(screen.getByText('Rerank')).toBeVisible()
  await expect.element(screen.getByText('最大候选 100')).toBeVisible()
  expect(screen.queryByText(/维/)).toBeNull()
})
```

API 测试断言 options parse `{providers:[{key,capabilities}]}`，`listSelectableModels('rerank')` 请求 `/models?type=rerank&active=true`。

- [x] **Step 2: 运行 Web 聚焦测试，确认类型仍只允许 Embedding。**

Run: `pnpm --dir web test -- schemas.test.ts provider-form.test.tsx model-form.test.tsx model-card.test.tsx api.test.ts`

Expected: FAIL，`ModelType` 不接受 rerank、表单仍要求 dimensions、Provider schema 未注册。

- [x] **Step 3: 实现 discriminated schema 和聚焦组件。**

```ts
export type ModelType = 'embedding' | 'rerank'
export type ProviderCapability = 'embedding' | 'rerank' | 'parser'
export type ProviderOption = { key: ProviderKey; capabilities: ProviderCapability[] }

export type Model = {
  id: string
  provider_id: string
  provider: ModelProviderSummary
  name: string
  display_name: string
  description: string
  type: ModelType
  model_name: string
  dimensions?: EmbeddingDimension
  parameters: Record<string, unknown>
  status: ModelStatus
  reference_count: number
  available: boolean
  created_at: string
  updated_at: string
}
```

模型 schema 使用 `z.discriminatedUnion('type', [embeddingModelFormSchema, rerankModelFormSchema])`。Rerank schema 精确限制 max docs `50..200`、query `256..4096`、document `512..32768`。Provider schema 加 `rerank_compatible` 的 base URL/path/timeout/retry/API key/custom headers。`ModelForm` 把类型专属字段拆到两个组件，避免继续增大单文件；Provider capabilities 决定可选类型，只有一种时显示只读类型。

模型卡显示可读类型、模型名、Rerank limits 和“配置快照引用”，不显示 UUID/hash。测试结果按 type 显示“维度”或“返回 2 个排序结果”。mutation 后精确失效 provider models 与对应 type selectable query。

- [x] **Step 4: 验证模型管理、i18n 和 Biome。**

Run: `pnpm --dir web test -- schemas.test.ts provider-form.test.tsx model-form.test.tsx model-card.test.tsx api.test.ts && pnpm --dir web check`

Expected: PASS；中文/英文 key 对齐，无 `any`、裸颜色或组件内 fetch。

- [x] **Step 5: 提交。**

```bash
git add web/src/features/models web/src/lib/i18n/locales/zh/models.ts web/src/lib/i18n/locales/en/models.ts
git commit -m "feat(web): 管理 Rerank 连接与模型"
```

### Task 12: Web Generation 向导支持 Rerank 配置和摘要

**Files:**

- Modify: `web/src/features/index-generations/schemas.ts`
- Modify: `web/src/features/index-generations/schemas.test.ts`
- Modify: `web/src/features/index-generations/types.ts`
- Modify: `web/src/features/index-generations/generation-form-schema.ts`
- Modify: `web/src/features/index-generations/generation-form.tsx`
- Modify: `web/src/features/index-generations/generation-form.test.tsx`
- Modify: `web/src/features/index-generations/generation-list.tsx`
- Modify: `web/src/features/index-generations/generation-list.test.tsx`
- Modify: `web/src/features/index-generations/api.ts`
- Modify: `web/src/features/index-generations/cache.ts`
- Modify: `web/src/lib/i18n/locales/zh/indexGenerations.ts`
- Modify: `web/src/lib/i18n/locales/en/indexGenerations.ts`

**Interfaces:**

- Consumes: Tasks 6、11 的 Generation DTO 和 selectable Rerank query。
- Produces: 第三步启用/关闭选择、candidate/failure validation、Generation 可读摘要。

- [x] **Step 1: 写表单三态和只读摘要失败测试。**

```tsx
it('submits enabled rerank config in retrieval step', async () => {
  const screen = await render(<GenerationForm baseGeneration={base} rerankModels={[rerankModel]} onSubmit={onSubmit} />)
  await goToRetrievalStep(screen)
  await user.click(screen.getByRole('switch', { name: '启用重排' }))
  await user.selectOptions(screen.getByLabelText('Rerank 模型'), rerankModel.id)
  await user.click(screen.getByRole('button', { name: '构建索引版本' }))
  expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
    rerank: { enabled: true, model_id: rerankModel.id, candidate_top_k: 50, failure_mode: 'fallback' },
  }))
})

it('renders disabled and enabled generation summaries', async () => {
  const screen = await render(<GenerationList generations={[withoutRerank, withRerank]} />)
  await expect.element(screen.getByText('Rerank：关闭')).toBeVisible()
  await expect.element(screen.getByText('BAAI/bge-reranker-v2-m3 · 候选 50 · 回退到 RRF')).toBeVisible()
})
```

再覆盖无可选模型的“前往模型”链接、member 只读、开关关闭提交 `{enabled:false}`、candidate 超模型 max 的 Zod error、失败保留草稿。

- [x] **Step 2: 运行测试，确认 Generation schema/form 尚无 Rerank。**

Run: `pnpm --dir web test -- generation-form.test.tsx generation-list.test.tsx schemas.test.ts`

Expected: FAIL，response schema 丢弃/拒绝 rerank，表单无开关和字段。

- [x] **Step 3: 实现 schema、默认值、UI 和 Query invalidation。**

```ts
export const rerankSnapshotSchema = z.object({
  model_id: z.uuid(),
  provider_id: z.uuid(),
  model_name: z.string().min(1),
  candidate_top_k: z.number().int().min(50).max(200),
  failure_mode: z.enum(['fallback', 'fail']),
})

export const generationRerankFormSchema = z.discriminatedUnion('enabled', [
  z.object({ enabled: z.literal(false) }).strict(),
  z.object({
    enabled: z.literal(true),
    model_id: z.uuid(),
    candidate_top_k: z.number().int().min(50).max(200),
    failure_mode: z.enum(['fallback', 'fail']),
  }).strict(),
])
```

`indexGenerationSchema` 加 `rerank: rerankSnapshotSchema.optional()`。表单保持三步，在 retrieval section 使用现有 Switch/Select/FormField；enabled 时显示模型、候选和失败策略，disabled 隐藏但保留本地草稿。提交前 refine `candidate_top_k <= selectedModel.parameters.max_documents`。没有模型时显示原因与 canonical 模型链接。

Generation list/activation dialog只显示快照 `model_name`、candidate/failure；不显示 model/provider UUID/hash。mutation success 精确失效 generation list、KB summary/settings 与 active generation query。

- [x] **Step 4: 验证表单、列表、权限和构建。**

Run: `pnpm --dir web test -- generation-form.test.tsx generation-list.test.tsx schemas.test.ts cache.test.ts && pnpm --dir web check && pnpm --dir web build`

Expected: PASS；desktop/mobile DOM 都有等价状态文本，TypeScript build exit 0。

- [x] **Step 5: 提交。**

```bash
git add web/src/features/index-generations web/src/lib/i18n/locales/zh/indexGenerations.ts web/src/lib/i18n/locales/en/indexGenerations.ts
git commit -m "feat(web): 配置 Generation 重排策略"
```

### Task 13: Web 检索结果、真实 E2E、文档与全量验证

**Files:**

- Modify: `web/src/features/retrieval/schemas.ts`
- Modify: `web/src/features/retrieval/schemas.test.ts`
- Modify: `web/src/features/retrieval/retrieval-test.tsx`
- Modify: `web/src/features/retrieval/retrieval-test.test.tsx`
- Modify: `web/src/lib/i18n/locales/zh/retrieval.ts`
- Modify: `web/src/lib/i18n/locales/en/retrieval.ts`
- Create: `cmd/langhuan/rerank_e2e_test.go`
- Modify: `cmd/langhuan/postgres_testmain_integration_test.go`
- Modify: `cmd/langhuan/main_test.go`
- Modify: `docs/API_ACCESS.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DATABASE_GUIDELINES.md`
- Modify: `ROADMAP.md`

**Interfaces:**

- Consumes: Tasks 1–12 全部合同。
- Produces: 三种 ranking stage 的可访问 UI、REST/MCP/日志真实闭环、同步架构文档和最终验证证据。

- [x] **Step 1: 写 Web 状态测试和临时依赖 E2E。**

```tsx
it('shows rerank as primary and RRF as secondary', async () => {
  const screen = await render(<RetrievalTest {...props} useResults={rerankedResultsStub()} />)
  await expect.element(screen.getByText('Rerank 0.9142')).toBeVisible()
  await expect.element(screen.getByText('RRF 0.0325')).toBeVisible()
  expect(screen.queryByText('91.42%')).toBeNull()
})

it('shows a persistent fallback warning', async () => {
  const screen = await render(<RetrievalTest {...props} useResults={fallbackResultsStub()} />)
  await expect.element(screen.getByRole('alert')).toHaveTextContent('重排服务暂时不可用，已按 RRF 融合顺序返回结果')
})
```

```go
//go:build integration

func TestRerankRESTAndMCPFlow(t *testing.T) {
    env := startTemporaryPostgresRedisAndRerankServer(t)
    app := startLanghuan(t, env)
    provider := createRerankProvider(t, app, env.RerankURL)
    rerankModel := createAndTestRerankModel(t, app, provider.ID)
    kb := createIndexedKnowledgeBase(t, app)
    generation := createActivateRerankGeneration(t, app, kb.ID, rerankModel.ID)
    rest := searchREST(t, app, kb.ID, "退款")
    mcp := searchMCP(t, app, kb.ID, "退款")
    if rest[0].RankingStage != value.RankingStageRerank || mcp.Results[0].ChunkID != rest[0].ChunkID { t.Fatalf("rest=%#v mcp=%#v", rest, mcp) }
    env.Rerank.Fail(http.StatusServiceUnavailable)
    fallback := searchREST(t, app, kb.ID, "退款")
    if fallback[0].RankingStage != value.RankingStageRRFFallback { t.Fatalf("%#v generation=%s", fallback, generation.ID) }
    assertLogsDoNotContain(t, app.Logs(), []string{"退款", env.APIKey, "fixture document secret"})
}
```

- [x] **Step 2: 运行聚焦测试，确认 UI/E2E 尚未满足。**

Run: `pnpm --dir web test -- retrieval-test.test.tsx schemas.test.ts && go test -tags=integration -p 1 ./cmd/langhuan -run RerankRESTAndMCPFlow -count=1`

Expected: FAIL，UI 无 ranking stage，E2E helper/后端闭环尚未完成。

- [x] **Step 3: 实现三种检索状态并同步项目文档。**

```ts
export const rankingStageSchema = z.enum(['rrf', 'rerank', 'rrf_fallback'])
export const retrievalResultSchema = z.object({
  // 保留现有 chunk/document/content/source/matched_children 字段
  score: z.number(),
  rerank_score: z.number().optional(),
  ranking_stage: rankingStageSchema,
}).passthrough()
```

实际实现不要用 `.passthrough()` 放宽现有完整 schema；上段只标出新增字段，最终文件继续显式列出所有现有字段。UI：rerank stage 主显 Rerank、次显 RRF；rrf 只显示 RRF；fallback 渲染页面内 `role=alert` warning 且结果仍显示 RRF。排序摘要只显示响应可证明的排序阶段；现有 duration 继续标为客户端总请求耗时，不能写成 Provider Rerank 耗时。保持 SafeMarkdown、来源和 child deep link。

E2E fake server 实现固定 `/v1/rerank`，按包含标记词的候选返回确定性分数，并可切换 503；PostgreSQL/Redis 均测试期创建和销毁。更新：

- `ARCHITECTURE.md`：查询图加入 optional Rerank、Generation snapshot 和多库一致性。
- `DATABASE_GUIDELINES.md`：000014 列、FK、引用和 snapshot codec。
- `API_ACCESS.md`：`rerank_score/ranking_stage`、fallback 与多库冲突。
- `ROADMAP.md`：将“增加 rerank adapter”从未来方向更新为已交付证据，版本归属由实现时当前版本段决定，不虚构新版本号。

- [x] **Step 4: 运行完整验证并保存真实输出。**

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

Expected: 所有命令 exit 0；数据库/Redis/Rerank E2E 只使用测试期临时依赖；测试日志扫描未发现 fixture query、正文或 credential。

- [x] **Step 5: 提交最终闭环。**

```bash
git add web/src/features/retrieval web/src/lib/i18n/locales/zh/retrieval.ts web/src/lib/i18n/locales/en/retrieval.ts cmd/langhuan docs ROADMAP.md
git commit -m "feat(rerank): 完成检索重排闭环"
```

---

## Spec → Plan 覆盖矩阵

| Spec 章节/要求 | 实施任务 | 验证 |
|---|---|---|
| 6–9 Port、Registry、`rerank_compatible` wire、安全 HTTP | Tasks 1–3 | unit + SSRF + retry/body cap tests |
| 8 模型 CRUD、nullable dimensions、连接测试、引用不可变性 | Tasks 4–5 | service/HTTP + PostgreSQL integration |
| 10 Generation schema、三态请求、hash、初始关闭 | Tasks 5–7 | domain/migration/service/HTTP tests |
| 11.1–11.3 单库顺序、private text、score/tie-break | Task 8 | service + DB evidence tests |
| 11.4 多库完全一致或冲突、全局一次调用 | Task 9 | compatibility matrix + call count |
| 12 fallback/fail 与稳定错误 | Tasks 8–9 | typed error + HTTP/MCP tests |
| 13 REST/MCP 与 selectable models | Tasks 4、9 | handler/tool contract tests |
| 14 request ID、阶段耗时、无敏感日志 | Task 10 | in-memory slog + middleware tests |
| 15.2–15.3 Provider/Rerank 模型 UI | Task 11 | Vitest browser + Biome |
| 15.4 Generation ASCII 原型与只读摘要 | Task 12 | form/list/permission tests |
| 15.5–15.7 检索三状态、移动任务等价、Query 缓存 | Tasks 11–13 | UI tests + build |
| 16 安全与资源保护 | Tasks 1、3、8、10、13 | SSRF/input/body/context/log tests |
| 17 测试策略和临时 Docker 铁律 | Tasks 3–13 | unit/integration/E2E commands |
| 18 文件边界与分阶段实施 | Tasks 1–13 | 每任务独立 commit/review gate |
| 19 发布兼容：既有 Generation 关闭、同步 Web | Tasks 5、11–13 | migration + embedded/full build |
| 20 验收标准 | Task 13 | 全量命令与 REST/MCP E2E |

## 实施期间的 Review Gate

每个 Task 提交前都要逐项确认：

1. 只修改该 Task 文件，没有混入无关重构。
2. 失败测试在实现前确实失败，完成后通过。
3. 新 error 可用 `errors.Is/As`，没有字符串比较。
4. Workspace 查询和外部 I/O 都传 context。
5. 日志和错误链没有 query、正文、凭证或第三方 body。
6. 数据库测试使用临时容器；未设置测试环境时 skip/自动拉起，绝不 fallback 本地 DSN。
7. 前端没有 `any`、组件内 fetch、UUID/hash DOM 或裸品牌色。
8. Commit message 使用中文 Conventional Commit。

## Plan 自检记录

- Spec coverage：第 6–20 节均映射到至少一个 Task；Rerank 模型、Generation、单库、多库、日志、Web、E2E 和文档都有验收点。
- Dependency order：共享 helper → Port/adapter → 模型生命周期 → schema/Generation → resolver → 单库 → 多库/协议 → observability → Web → E2E，没有任务消费尚未定义的接口。
- Type consistency：统一使用 `RerankSnapshot`、`RerankSelection`、`ResolvedRerankClient`、`RerankFailureMode`、`RankingStage`、`rerank_score` 和 `ranking_stage`。
- Failure consistency：`fallback` 仅覆盖远端可恢复失败；snapshot/config/auth/context/multi conflict 在 Tasks 8–10 均 fail closed。
- Data consistency：Generation nullable 列采用全空/全非空 CHECK；model/provider 引用统计同时覆盖 Embedding/Rerank。
- Security consistency：Provider payload 只含 wire contract allowlist 字段；日志 allowlist 与 adapter error sanitation 均有测试。
- Test isolation：所有 PostgreSQL/Redis E2E 都显式要求测试期临时容器；第三方 Rerank 只用 `httptest`/fake server。
- 占位标记扫描：未发现未完成标记、替代字符或含糊的省略实现步骤。
