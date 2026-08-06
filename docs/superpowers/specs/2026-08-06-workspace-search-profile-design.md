# Workspace 检索策略与全局 Rerank 设计规格

## 目标

把 Rerank 从 KnowledgeBase 的 Index Generation 配置调整为 Workspace 级检索策略。Workspace owner/admin 才能修改策略；`knowledge_search` 在不同 Embedding 模型分别召回后，使用该 Workspace 策略选择的同一个 Rerank 模型完成全局重排。

## 关键决策

- Provider/Model 页面只负责连接和模型目录，不决定搜索时使用哪个 Rerank。
- Index Generation 继续固化 Embedding 模型和本知识库的 Vector/FTS/RRF 参数；既有 Generation 的 `rerank` 快照保留用于兼容读取，但不再作为搜索决策来源。
- Workspace 只有一个默认 Search Profile。当前不引入多个 profile、请求级任意 model_id 或按知识库 Rerank 覆盖。
- 策略包含：是否启用 Rerank、Rerank 模型快照、candidate_top_k、failure_mode。
- 模型选择必须是当前 Workspace 可见、active、`type=rerank` 的模型，并在保存时固化 model/provider/name/config hash。
- 不同 Embedding 模型的召回结果先在各知识库内做 RRF，再以 rank-based score 全局合并；Rerank 只接收 query 与候选文本，因此不要求 Embedding 空间相同。

## 权限与 API

- `GET /api/v1/workspaces/:workspace_slug/search-settings`：Session member+ 可读当前有效策略。
- `PUT /api/v1/workspaces/:workspace_slug/search-settings`：Session admin/owner 可写；member 返回 403。
- Bearer API Key 只能调用搜索，不能读取或修改策略配置。
- 写入请求支持 `rerank.enabled=false`，或 `enabled=true` 加 `model_id`、`candidate_top_k`、`failure_mode`。关闭时清空模型快照。
- 未创建配置行时返回 disabled 默认策略，不需要为每个 Workspace 预置数据。

## 检索流程

```text
读取 Workspace Search Profile
        │
        ├─ disabled ──> 各 KB 召回 + RRF + merge + Final Top K
        │
        └─ enabled ──> 各 Embedding 快照分组并分别生成 query vector
                         │
                   每 KB Vector + FTS + RRF
                         │
                      全局 merge
                         │
                 parent grouping / evidence
                         │
                 一个共同 Rerank 模型
                         │
                      Final Top K
```

单库和多库均使用同一 Workspace Profile。Rerank 失败时，`fallback` 对可恢复远端错误返回 RRF 并标记 `ranking_stage=rrf_fallback`；`fail` 返回原始错误，不伪装成成功结果。模型不可见、被禁用、配置 hash 漂移属于配置错误，直接拒绝执行。

## 数据模型

新增 `workspace_search_settings` 表，一 Workspace 一行：

- `workspace_id` 主键，级联 Workspace 删除；
- 可空 `rerank_model_id`、`rerank_provider_id`、`rerank_model_name`、`rerank_model_config_hash`；
- `rerank_config jsonb` 保存 `candidate_top_k` 与 `failure_mode`；
- `updated_by`、`created_at`、`updated_at`；
- CHECK 约束保证关闭时快照字段全空，启用时字段完整且 config 合法。

配置更新在 Workspace transaction 中完成，先解析并校验模型，再原子 upsert。模型/Provider 的引用统计必须包含 Search Settings，避免删除或语义修改绕过引用保护。

## 日志

每次搜索记录结构化字段：`workspace_id`、`search_profile`（default）、`rerank_enabled`、`rerank_model_id`、`rerank_provider_id`、`candidate_count`、`duration_ms`、`ranking_stage`、`error_class`/`fallback_reason`。不得记录 API key、完整 query 或候选正文。

## 前端交互

Workspace 管理导航增加“检索策略”。页面只对 owner/admin 显示入口；普通成员不显示入口，直接访问页面时显示无权限状态。

```text
工作区 / 检索策略

┌────────────────────────────────────────────────────┐
│ 默认检索策略                              [已启用] │
├────────────────────────────────────────────────────┤
│ 全局 Rerank                                          │
│ 启用 Rerank        [●]                               │
│ Rerank 模型        [BGE Reranker v2             ▾] │
│ 候选数量           [50]                              │
│ 失败策略           [回退到 RRF                   ▾] │
│                                                      │
│ 该策略会应用于此 Workspace 的单库和多库 knowledge_search │
└────────────────────────────────────── [保存策略] ┘
```

模型选择下拉只请求 active、visible、`type=rerank` 模型，并显示 Provider 与模型名。保存成功后刷新当前策略及搜索相关缓存。

## 验收标准

1. Admin/owner 能启用、修改、关闭 Workspace Rerank；member/API Key 不能修改。
2. 单库搜索使用 Workspace Profile，而不是 Generation 的 Rerank 字段。
3. 多库搜索可混用不同 Embedding 模型，并在全局候选上调用一次 Profile 指定的 Rerank 模型。
4. 多库 Rerank 解析传递真实 Workspace ID；`fail` 错误能返回调用方。
5. Rerank 请求包含原始 query，候选文本不为空且不泄漏敏感数据。
6. 迁移、Repository、HTTP、Service、前端测试覆盖启用/关闭、权限、模型漂移、fallback/fail 和不同 Embedding 分组场景。
