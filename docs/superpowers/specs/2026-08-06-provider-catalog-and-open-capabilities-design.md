# Provider 泛化与模型目录快速配置设计

> 状态：已获用户批准，待实现
> 日期：2026-08-06

## 1. 目标

Provider 表示供应商/平台连接，不表示某一种模型类型。硅基流动、火山方舟、阿里百炼可以在同一连接下提供多种模型能力；智谱、DeepSeek 等未来 Provider 也必须能够按自己的协议接入，而不需要修改核心 Provider 关系或把所有供应商硬编码成 Embedding/Rerank。

本次交付包含：

- Provider descriptor 能力改为注册式、可扩展的字符串能力，不再由固定 switch 限制。
- 增加可选的 Provider 模型目录能力（ModelCatalog）。Provider 没有目录接口时继续支持手动添加模型。
- 模型添加页提供“从 Provider 获取模型”搜索、筛选和快速填充。
- SiliconFlow 提供目录适配器，使用统一的目录响应；目录项可填充模型标识、展示名、类型、维度和默认参数。
- 修正 `make dev` 数据库中展示名“硅基”但 provider key 错误为 `openai` 的连接及其配置结构。

非目标：本次不实现所有国内供应商的真实网络适配器，不把未知模型类型伪装成当前可运行的 Embedding/Rerank，也不把远端目录结果永久写入 `models` 表。

## 2. 三层概念

```text
Provider key       = 供应商/平台身份
  siliconflow / volcengine / bailian / zhipu / deepseek

Provider capability = 琅嬛当前部署已注册的能力
  embedding / rerank / parser / llm / ...

Model type          = 具体模型的运行类型
  当前可运行：embedding、rerank
  未来扩展：llm、asr 等，由对应 codec/client 注册
```

Provider descriptor 只负责声明能力和共享连接解码；模型类型是否真正可创建，仍由对应 Model Factory/codec 决定。这样“供应商能提供什么”和“琅嬛目前能运行什么”不会混为一谈。

### 2.1 Descriptor 合同

```go
type ProviderDescriptor struct {
    Key              string
    Capabilities     []value.ProviderCapability
    CredentialFields []string
    DecodeProvider   func(scope value.ModelScope, config, credentials json.RawMessage) (ProviderDecodeResult, error)
    ListModels       ModelCatalogFunc // 可选
}
```

`ProviderCapability` 只要求符合稳定的 ASCII identifier；未知能力可以被 descriptor 注册、展示和日志记录，但如果当前没有相应 Model Factory，不能被模型创建流程选中。`SupportsModelType` 通过 `CapabilityFromModelType` 与 descriptor 精确匹配，不再只接受两个固定常量。

## 3. ModelCatalog 合同

Provider-specific adapter 实现统一的应用端口：

```go
type ModelCatalogInput struct {
    Scope           value.ModelScope
    Config          map[string]any
    CredentialsJSON []byte
    ModelType       *value.ModelType
    Query           string
}

type ModelCatalogItem struct {
    ID          string
    DisplayName string
    Description string
    Type        *value.ModelType
    Dimensions  *int
    Parameters  map[string]any
    Available   bool
}

type ModelCatalogFunc func(context.Context, ModelCatalogInput) ([]ModelCatalogItem, error)
```

约束：

- `ListModels` 是可选能力；没有实现时返回稳定的 `catalog_unavailable`，前端保留手动填写。
- 目录调用只在用户主动点击时触发，带 context timeout，不能在页面渲染时自动调用外部 API。
- 服务端解密凭证后调用 adapter，日志只能记录 provider/model_type/query 长度/结果数/耗时，不能记录 API key 或完整查询文本。
- 目录结果不自动创建 `models` 行；只有用户点击“使用此模型”并保存后才写入。
- 未知或当前未实现的 `Type` 显示为不可选择，而不是假装可以运行。

### 3.1 HTTP API

```text
GET /api/v1/admin/model-providers/:provider_id/model-catalog
    ?type=all|embedding|rerank
    &q=<search>

GET /api/v1/workspaces/:workspace_slug/model-providers/:provider_id/model-catalog
    ?type=all|embedding|rerank
    &q=<search>
```

响应：

```json
{
  "provider": "siliconflow",
  "items": [
    {
      "id": "BAAI/bge-m3",
      "display_name": "BGE-M3",
      "description": "多语言 Embedding 模型",
      "type": "embedding",
      "dimensions": 1024,
      "parameters": {"batch_size": 32},
      "available": true
    }
  ],
  "source": "provider_api",
  "fetched_at": "2026-08-06T00:00:00Z"
}
```

`type` 过滤只影响目录展示；业务 selectable 接口仍然使用已有的精确 `type + active` 合同。

## 4. SiliconFlow 目录适配

SiliconFlow 的共享连接配置继续使用：

```text
base_url                  https://api.siliconflow.cn
embedding_endpoint_path   /v1/embeddings
rerank_endpoint_path      /v1/rerank
timeout_seconds           60
retry_times               2
```

目录 adapter 调用 `GET {base_url}/v1/models`，使用共享 Bearer API key，并把上游条目归一化为 `ModelCatalogItem`。上游无法确定类型或维度的条目保留 `type=nil`/`dimensions=nil`，由用户在保存前选择并补齐；已知 SiliconFlow 模型可通过本地 metadata 映射默认类型、维度和参数。目录 API 不可用时错误显示为“无法获取模型列表，可手动填写”。

## 5. 前端交互与 ASCII 原型

```text
+------------------------------------------------------------------------+
| 添加模型          连接：硅基流动                         [关闭]       |
|                                                                        |
| [从 Provider 获取模型]                         [手动填写]             |
|                                                                        |
| 目录搜索 [bge____________________] 类型 [全部 v] [刷新]              |
|                                                                        |
| ○ BAAI/bge-m3              Embedding   1024  推荐参数  [使用此模型]  |
| ○ BAAI/bge-reranker-v2     Rerank       —    推荐参数  [使用此模型]  |
| ○ Some/New-Model            未识别      —              不可选择       |
|                                                                        |
| 选中后自动填充，可继续修改：                                           |
| 模型标识 [BAAI/bge-m3________________]                                 |
| 展示名称 [BGE-M3____________________]                                  |
| 类型     [Embedding v]                                                  |
| 维度     [1024____]       参数 [batch_size: 32________________]        |
|                                      [取消] [保存模型]                  |
+------------------------------------------------------------------------+
```

交互规则：

- 默认仍打开手动表单，目录按钮是加速入口，不改变原有保存流程。
- 目录弹窗支持搜索、类型筛选、刷新、键盘选择和错误重试。
- 选中目录项后填充字段，不直接提交；用户可以修改展示名、类型和参数，但服务端最终校验。
- Provider 未实现目录时按钮显示“暂不可用”，同时保留手动填写。
- 目录项的 `type` 不在当前 Factory registry 中时置灰并说明“当前版本暂不支持”。
- 目录 query 使用 TanStack Query，key 包含 scope、workspace、provider、type、q；关闭弹窗不清除已填充表单。

## 6. 开发库数据修正

`make dev` 使用 `postgres://localhost:5432/langhuan`。只针对现有记录 `name='siliconflow' AND provider='openai'` 执行事务：

1. 锁定 Provider 行并确认最多一条。
2. 将 `provider` 更新为 `siliconflow`。
3. 将 config 从 OpenAI 结构转换为 SiliconFlow 结构，保留合法 `base_url` 与 `timeout_seconds`，补齐 endpoint/retry 默认值并移除 `mode`。
4. 不读取、不重写 `credentials_ciphertext`；不改变关联 models、Generation 或 workspace 边界。
5. 重新读取并用 SiliconFlow descriptor 解码验证。

## 7. 验收标准

- Provider descriptor 可注册任意合法 capability identifier，不因未来能力名称而修改核心 switch。
- 当前 Embedding/Rerank 运行链路仍通过精确 Factory 校验；未实现类型不能保存成可运行模型。
- 有 ModelCatalog 的 Provider 显示获取模型入口；目录项能快速填充字段；无目录 Provider 仍能手动添加。
- SiliconFlow 目录和双 endpoint 使用同一加密凭证，日志不泄漏凭证。
- 目录 HTTP API 遵循平台/Workspace 可见性和权限边界，不暴露 raw config/credentials。
- 开发库硅基连接的 `provider='siliconflow'` 且配置可被 SiliconFlow codec 解码，既有模型引用保持不变。

## 8. 设计自检

- 没有把国内供应商名单写入核心类型；供应商通过 descriptor/adapter 注册。
- 目录是可选能力，手动添加始终可用，不依赖每个供应商都实现同一 `/models` 响应。
- 目录结果与业务模型持久化解耦，避免远端变更自动破坏已使用模型。
- “硅基”展示名、`siliconflow` provider key、模型 `type=embedding` 三者语义明确且不互相替代。
