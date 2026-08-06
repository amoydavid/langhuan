# 琅嬛知识处理与检索数据模型 v2 设计

**日期：** 2026-07-30

**状态：** 已确认

**范围：** KnowledgeBase、File/FAQ/Web Document、文件树、Chunk、向量/全文索引、Job 与 DocumentAsset

**不在范围：** 用户、会话、Workspace 成员、邀请、Workspace API Token、Model Provider 与 Model 的既有授权和管理合同

## 1. 背景

琅嬛当前尚未部署到生产环境，知识处理链路的数据可以执行一次开发期破坏性重建。现有模型适合 v0.2-v0.3 的流水线骨架，但不足以支撑长期 SaaS 运行：

- `documents` 同时保存稳定文档身份、原始文件信息和某次解析结果，无法表达不可变版本。
- `chunks` 在重建时整批删除再插入，Chunk 身份不稳定，也不能支持人工编辑历史。
- `chunk_embeddings` 以 `chunk_id` 为唯一主键，无法让新旧模型索引同时存在并原子切换。
- `chunk_embeddings` 与 `chunk_keywords` 分开写入，存在向量和全文投影版本不一致的风险。
- `knowledge_bases.embedding_model_id` 只能表达用户选择，不能证明当前可查询向量确实由该模型生成。
- 下级资源通过多层 JOIN 推导 Workspace，无法直接建立简单、可靠的 PostgreSQL RLS policy。
- 当前 Go `ChunkEmbeddingRow` 没有映射数据库的非空 `dimension` 字段，真实索引写入前必须消除该合同漂移。

WeKnora 的表结构证明了 Chunk 编辑、历史版本、索引状态和检索宽表的实际价值，但其无约束租户冗余、通用多态 Embedding、数值 flags、前后 Chunk 指针和 VectorStore 提前抽象不适合直接复制到琅嬛。

## 2. 目标

本设计建立以下长期合同：

1. Workspace 是唯一 SaaS 租户边界，所有租户业务表直接保存 `workspace_id NOT NULL`。
2. 文档解析结果不可变并可回滚；重新上传、重新解析与重新分块具有明确语义。
3. Chunk 支持独立编辑、启用、停用和历史审计，不覆盖解析器原始产物。
4. 一个知识库只有一套当前生效的检索 generation；全量重建通过双缓冲完成。
5. 向量与全文索引属于同一个可重建检索投影，并能原子发布。
6. 换模型、换分块策略、单文档替换和单 Chunk 编辑都不会产生半新半旧的搜索结果。
7. 数据模型为 PostgreSQL RLS 做好准备，但本次不执行 `ENABLE ROW LEVEL SECURITY`。
8. 保留现有授权表和 Model/Provider 数据，不引入外部向量数据库抽象。
9. Document 具有不可变的 `file/faq/web` 类型；采集渠道与业务类型分离。
10. FAQ 固定索引问题、返回答案；答案不进入向量或全文索引。
11. File Document 在知识库内以虚拟文件树组织，目录变化不改变内容版本、对象存储键或检索 generation。
12. 为未来 Web 抓取保留稳定 URL 身份与按抓取生成 Revision 的合同，但本次不实现 crawler。

## 3. 非目标

- 本次不启用 RLS policy，不改变当前 session、membership、role 或 platform admin 行为。
- 不实现图查询、Chunk 关系图、自动标签或外部 VectorStore。
- 不自动把旧 Chunk 人工编辑映射到新的分块边界。
- 不提供同时查询多个 active Embedding 模型的产品能力。
- 不为旧 KnowledgeBase、Document、Chunk 或向量数据编写回填程序。
- 不在 metadata 中保存 API key、完整第三方响应或完整用户原文副本。
- 不实现 Web crawler、抓取调度、网页快照详情表或 URL canonicalization 网络探测。
- 不把 FAQ 答案拼入 Embedding/FTS 输入，也不允许通过通用 Chunk 编辑接口修改 FAQ。
- 不让文件树路径承担对象存储路径、文档版本或访问控制语义。

## 4. 核心设计决策

### 4.1 事实层与检索投影分离

事实层负责身份、版本、来源和审计：

```text
knowledge_bases
documents
document_revisions
faq_revision_contents
faq_revision_questions
document_chunk_sets
chunks
chunk_revisions
file_tree_nodes
document_assets
jobs
```

检索层负责可见性、向量、全文和原子切换：

```text
knowledge_base_index_generations
retrieval_entries
```

`retrieval_entries` 可以随时删除并从事实层重建。任何业务 API 都不能把 RetrievalEntry 当作 Chunk 的权威内容来源。

### 4.2 Workspace 直接归属

每张租户表都包含：

```sql
id uuid PRIMARY KEY,
workspace_id uuid NOT NULL,
UNIQUE (workspace_id, id)
```

共享主键的一对一表 `faq_revision_contents` 以 `document_revision_id` 充当 `id`，仍提供 `UNIQUE (workspace_id, document_revision_id)`；不因为一对一关系省略直接 tenant key。

下级表继续保存必要的 `knowledge_base_id`、`document_id` 等 lineage 字段，但冗余值必须由复合外键约束，不能仅依靠 application 保持一致。

### 4.3 指针决定生效状态

- `knowledge_bases.active_index_generation_id` 决定当前检索 generation。
- `documents.active_revision_id` 决定当前文件与解析版本。
- `chunks.active_revision_id` 决定当前有效 Chunk 内容。
- `file_tree_nodes.parent_id + name` 决定 File Document 当前目录位置与文件树展示名。

不再额外维护 `is_current` 或 `is_active` 布尔列。资源状态描述处理进度，生效关系只由父实体指针决定。

### 4.4 单活双缓冲

一个 KnowledgeBase 对外只有一个 active generation。换模型、换检索参数或全量重建时，后台建立新的 generation；构建期间查询旧 generation，全部完成并通过版本校验后只切换一个指针。

### 4.5 解析版本与分块集合分离

`document_revisions` 表达原始文件和解析产物；`document_chunk_sets` 表达某个解析版本在特定 Chunker 版本和配置下的分块结果。更换分块策略不会伪装成新的文件版本。

### 4.6 Document 类型与采集渠道分离

`documents.kind` 是不可变业务类型，只允许 `file/faq/web`；`source_type` 是 `upload/api/crawler/sync` 等采集渠道。二者正交，不能用 `file_type` 或 `source_type` 代替 `kind`。

`document_revisions.kind` 冗余保存同一类型，并通过包含 `kind` 的复合外键强制等于父 Document。这样 revision 本地 CHECK、未来 RLS 与 worker 查询都不需要 JOIN 才能判断类型。现有 Document 变更类型没有合法更新路径；需要改变类型时创建新 Document。

`file_type` 继续属于 DocumentRevision，因为它描述一次文件版本的解析合同，而不是稳定 Document 身份：

- `kind=file`：`file_type` 必填；
- `kind=faq`：`file_type` 必须为空；
- `kind=web`：`file_type` 必须为空。

Web Document 以规范化 URL 作为 KB 内稳定身份，每次抓取产生新 DocumentRevision。未来可以增加抓取任务、HTTP 响应和网页快照表，不需要移动 `file_type`。

### 4.7 FAQ 是事实类型，不是特殊文件格式

一个 FAQ DocumentRevision 由一组问题和一个回答构成。问题与回答整体版本化；修改任一问题或回答都创建完整的新 DocumentRevision。该 Revision 固定产生一个 `strategy=faq` 的 ChunkSet、一个 Chunk 和一个 system ChunkRevision。

FAQ 的检索文本与返回文本不同：所有问题按 sequence 拼接为 `search_content`，回答保存为 `content`。Embedding 与 FTS 只消费 `search_content`；命中后只把回答作为 evidence 正文返回。

### 4.8 文件树是独立的知识库组织投影

`file_tree_nodes` 只组织 `kind=file` 的 Document。它不承载对象存储键、原始文件名、Revision、检索内容或权限继承。目录重命名/移动、文件重命名不会创建 DocumentRevision、递增 `content_version`、重新索引或让 building Generation stale。

## 5. 实体关系与职责

```text
Workspace
  └─ KnowledgeBase
       ├─ FileTreeNode(root)
       │    └─ FileTreeNode(folder/file)
       ├─ IndexGeneration
       │    └─ RetrievalEntry
       └─ Document
            └─ DocumentRevision
                 ├─ FAQRevisionContent
                 │    └─ FAQRevisionQuestion
                 ├─ DocumentAsset
                 └─ DocumentChunkSet
                      └─ Chunk
                           └─ ChunkRevision
```

### 5.1 `knowledge_bases`

稳定的知识库容器。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uuid | 全局业务 ID |
| `workspace_id` | uuid | RLS tenant key |
| `name` | text | Workspace 内活动名称唯一 |
| `description` | text | 描述 |
| `metadata` | jsonb | 非核心扩展信息 |
| `content_version` | bigint | 每次成功发布内容变化后递增 |
| `active_index_generation_id` | uuid/null | 当前生效检索 generation |
| `file_tree_root_id` | uuid | 该 KB 唯一显式 root 节点 |
| `created_at` | timestamptz | 创建时间 |
| `updated_at` | timestamptz | 更新时间 |
| `deleted_at` | timestamptz/null | 业务恢复窗口 |

Embedding Model、分块配置和检索配置不直接作为当前生效配置保存在 KnowledgeBase；它们属于 generation 的不可变快照。

创建 KnowledgeBase 时先生成 root 与 Generation ID，再在同一事务插入带这两个目标 ID 的 KnowledgeBase、唯一显式 root `file_tree_nodes` 行和空的 ready Generation。两个外键均延迟到提交时校验，因此不需要暂时把非空 `file_tree_root_id` 设为 NULL，知识库也不会处于“存在但没有根节点”的可见状态。

### 5.2 `knowledge_base_index_generations`

一次完整索引代次与不可变配置快照。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id/workspace_id/knowledge_base_id` | uuid | 租户与 KB lineage |
| `base_generation_id` | uuid/null | 构建所基于的 active generation |
| `embedding_model_id` | uuid | 关联现有 `models` |
| `provider_id` | uuid | 非敏感模型快照 |
| `model_name` | text | 真实供应商模型名快照 |
| `embedding_dimension` | integer | 798/1024/2048/3584 |
| `model_config_hash` | text | 语义配置指纹，不含凭证 |
| `chunker_version` | integer | Chunker 合同版本 |
| `chunking_config` | jsonb | typed 分块配置快照 |
| `retrieval_config` | jsonb | typed FTS/topK/RRF 配置快照 |
| `config_hash` | text | 完整 generation 配置指纹 |
| `source_content_version` | bigint | 开始构建时的 KB 内容版本 |
| `indexed_content_version` | bigint | 已纳入投影的内容版本 |
| `status` | text | building/ready/stale/failed/retired |
| 统计字段 | bigint | document/chunk/indexed/failed/manual edit 数量 |
| `manual_edit_disposition` | text | not_applicable/pending/archive_confirmed |
| 错误与时间字段 | text/timestamptz | 构建诊断、激活与退役时间 |

active 状态不存为 status；KnowledgeBase 指针决定哪一个 ready generation 当前生效。

### 5.3 `documents`

稳定的文档身份。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id/workspace_id/knowledge_base_id` | uuid | lineage |
| `kind` | text | 不可变的 file/faq/web |
| `title` | text | 当前展示标题 |
| `source_type` | text | upload/api/crawler/sync 等采集渠道 |
| `source_uri` | text/null | Web 的规范化稳定 URL；File/FAQ 为空 |
| `status` | text | pending/processing/ready/failed/deleting/deleted |
| `active_revision_id` | uuid/null | 当前发布解析版本 |
| `metadata` | jsonb | 文档级扩展信息 |
| `created_at/updated_at/deleted_at` | timestamptz | 生命周期 |

文件 hash、存储键、原始上传文件名、解析 Markdown 和 manifest 移入 revision。`kind` 不允许由更新 API 或 Repository update 改写；类型变化创建新 Document。

Web URL 规范化首版合同固定为：去除 fragment；scheme/host 小写；移除默认端口；空 path 归一为 `/`；保留 query 的键值和顺序，不做网络访问、重定向跟随或站点自定义 canonical 规则。活动 Web Document 使用 `(workspace_id, knowledge_base_id, source_uri)` 部分唯一索引去重。

### 5.4 `document_revisions`

不可变的文件与解析版本。

| 字段 | 类型 | 说明 |
|---|---|---|
| lineage 字段 | uuid | Workspace/KB/Document |
| `kind` | text | 冗余 file/faq/web，并由复合外键约束等于 Document |
| `revision_no` | bigint | Document 内从 1 递增 |
| `revision_reason` | text | ingest/replace/reparse/crawl/edit |
| `original_filename` | text/null | File 原始上传文件名；不随树节点重命名 |
| `file_type/content_type` | text/null | File 解析类型与来源 MIME；FAQ/Web 的 file_type 为空 |
| `raw_storage_key` | text/null | File 或 Web 原始快照对象键 |
| `sha256/size_bytes` | text/bigint | 完整性与查重 |
| `normalized_markdown` | text/null | File/Web 规范化解析正文；FAQ 为空 |
| `parse_manifest` | jsonb/null | File/Web typed versioned manifest；FAQ 为空 |
| `processing_version` | integer | Pipeline 处理版本 |
| `status` | text | pending/parsing/ready/failed |
| `error_class/error_message` | text | 分类且截断的错误 |
| `created_by/created_at/completed_at` | uuid/timestamptz | 审计 |

`rechunk` 不属于 document revision reason；重新分块只创建 ChunkSet。FAQ 使用 `edit` 表达问题/回答整体变更；Web 每次抓取使用 `crawl` 并产生新 Revision。

Document 与 Revision 的 kind equality 使用可引用的唯一键和复合外键：

```sql
UNIQUE (workspace_id, knowledge_base_id, id, kind);

FOREIGN KEY (workspace_id, knowledge_base_id, document_id, kind)
REFERENCES documents (workspace_id, knowledge_base_id, id, kind);
```

Revision 本地 CHECK 强制：File 的 `file_type/original_filename/raw_storage_key` 非空；FAQ 的文件、Markdown、manifest 字段为空；Web 的 `file_type/original_filename` 为空。Web 快照的 `raw_storage_key` 可为空，以允许只保存规范化正文的抓取实现。

### 5.5 `faq_revision_contents` 与 `faq_revision_questions`

`faq_revision_contents` 与 FAQ DocumentRevision 一对一：

| 字段 | 类型 | 说明 |
|---|---|---|
| `workspace_id/knowledge_base_id/document_id` | uuid | 直接 tenant 与 lineage |
| `document_revision_id` | uuid PK | 同时是到 FAQ Revision 的复合外键 |
| `answer` | text | 唯一回答，trim 后非空 |
| `created_at` | timestamptz | 审计 |

`faq_revision_questions` 保存有序问题变体：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | uuid | 问题版本行 ID |
| `workspace_id/knowledge_base_id/document_id/document_revision_id` | uuid | 直接 tenant 与 lineage |
| `sequence` | integer | Revision 内从 0 连续递增 |
| `question` | text | 原始问题，trim 后非空 |
| `normalized_question` | text | 应用层确定性规范化值 |
| `created_at` | timestamptz | 审计 |

每个 FAQ Revision 恰好一行 answer 且至少一个 question。数据库通过一对一 PK、非空 CHECK 和 deferred constraint trigger 在事务提交时保证 question 数量至少为一；Service 仍在入库前验证连续 sequence。`(workspace_id, document_revision_id, sequence)` 与 `(workspace_id, document_revision_id, normalized_question)` 分别唯一。

### 5.6 `document_chunk_sets`

一个 DocumentRevision 在一套确定分块配置下的完整产物。

| 字段 | 类型 | 说明 |
|---|---|---|
| lineage 字段 | uuid | Workspace/KB/Document/DocumentRevision |
| `chunker_version` | integer | Chunker 行为版本 |
| `strategy` | text | standard/faq |
| `chunking_config` | jsonb | typed 配置快照 |
| `config_hash` | text | 幂等键组成部分 |
| `status` | text | building/ready/failed/archived |
| `chunk_count` | bigint | 完成后数量 |
| 错误与时间字段 | text/timestamptz | 诊断 |

同一 DocumentRevision、strategy、Chunker 版本和 config hash 只有一个 ChunkSet；任务重试复用该行并事务性重建其 Chunk。FAQ 的 strategy 固定为 `faq`，config hash 来自固定版本化 FAQ 合同，不读取 KB 普通 chunking config。

### 5.7 `chunks`

ChunkSet 内稳定的逻辑分块与原始解析来源。

| 字段 | 类型 | 说明 |
|---|---|---|
| lineage 字段 | uuid | Workspace/KB/Document/ChunkSet |
| `sequence` | integer | ChunkSet 内从 0 连续递增 |
| `source_content` | text | 解析器/Chunker 原始内容，不被用户覆盖 |
| `source_anchor` | jsonb | typed 来源锚点 |
| `metadata` | jsonb | oversized、heading/table 等结构信息 |
| `active_revision_id` | uuid/null | 当前有效 ChunkRevision |
| `created_at` | timestamptz | 创建时间 |

相邻 Chunk 由 `(chunk_set_id, sequence)` 推导，不保存 `pre_chunk_id/next_chunk_id`。

FAQ 只允许一个 `sequence=0` Chunk，其内容语义固定为：

```text
source_content =
  Q: question 1
  Q: question 2
  A: answer

system ChunkRevision.content = answer
system ChunkRevision.embedding_content = question 1 + "\n" + question 2
```

### 5.8 `chunk_revisions`

当前展示与检索内容的历史修订。

| 字段 | 类型 | 说明 |
|---|---|---|
| lineage 字段 | uuid | Workspace/KB/Document/Chunk |
| `revision_no` | bigint | Chunk 内从 1 递增 |
| `base_revision_id` | uuid/null | 乐观并发基线 |
| `content` | text | 有效正文 |
| `context_header` | text | 有效上下文头 |
| `embedding_content` | text | 精确记录送入 Embedding 的文本 |
| `enabled` | boolean | 是否应进入检索投影 |
| `status` | text | pending/indexing/ready/failed |
| `edit_source` | text | system/user |
| `editor_user_id` | uuid/null | 人工编辑者 |
| `error_class/error_message` | text | 局部索引失败诊断 |
| `created_at/indexed_at` | timestamptz | 审计 |

初次系统分块也创建 revision 1。人工编辑、停用和重新启用一律追加 revision。

通用 Chunk Revision API 只接受 File/Web Chunk。FAQ Chunk 是其 FAQ Revision 的派生系统事实，任何编辑、启停请求都返回稳定的 `faq_chunk_immutable` 冲突；FAQ 内容变更必须提交完整问题组与回答并创建新 DocumentRevision。

### 5.9 `retrieval_entries`

统一的全文与向量检索投影。

| 字段 | 类型 | 说明 |
|---|---|---|
| lineage 字段 | uuid | Workspace/KB/Generation/Document/DocumentRevision/ChunkSet/Chunk/ChunkRevision |
| `state` | text | staging/published/retired/failed |
| `search_content` | text | Embedding 与 FTS 的唯一输入快照 |
| `content` | text | 命中后返回的 evidence 正文快照 |
| `source_anchor` | jsonb | 证据来源快照 |
| `metadata` | jsonb | 必要的非敏感证据元数据 |
| `fts_document` | tsvector | FTS adapter 生成 |
| `embedding` | halfvec/null | pgvector 数据 |
| `dimension` | integer/null | HNSW 部分索引路由 |
| 时间字段 | timestamptz | 创建、发布、退役 |

只有 `published` 且属于 KB active generation 的行能进入查询。Published 行必须同时具有有效向量、维度与发布时间。

类型映射固定为：

| Document kind | `search_content` | `content` |
|---|---|---|
| File/Web | active ChunkRevision.`embedding_content` | active ChunkRevision.`content` |
| FAQ | 按 question sequence 以换行连接 | FAQ answer |

FTS adapter 与 Embedding client 都只读取 `search_content`。FAQ answer 永远不参与向量或 FTS；`content` 只用于 evidence 返回。RetrievalEntry 不保存权威 `document_title`，候选融合后再按 lineage 读取当前 Document/文件节点名称，避免树节点重命名导致旧快照泄漏。

### 5.10 `file_tree_nodes`

| 字段 | 类型 | 说明 |
|---|---|---|
| `id/workspace_id/knowledge_base_id` | uuid | tenant 与 KB lineage |
| `parent_id` | uuid/null | root 为空，其它节点必填 |
| `node_type` | text | root/folder/file |
| `name` | text | root 固定为空；folder/file trim 后非空 |
| `document_id` | uuid/null | file 必填且只能指向 kind=file；root/folder 为空 |
| `created_at/updated_at` | timestamptz | 组织变更审计 |

规则：

- 每个 KB 恰好一个显式 root；root 名称固定为空字符串且不参加兄弟命名空间。
- file 与 folder 共用大小写不敏感的兄弟命名空间：`(workspace_id, knowledge_base_id, parent_id, lower(name))` 唯一。
- 每个活动 File Document 恰好一个 file node；FAQ/Web 没有节点。
- parent/child 与 Document 使用包含 Workspace/KB 的复合外键；Document 复合键包含 `kind`，file node 以常量 kind=`file` 的 CHECK + 复合外键阻止关联 FAQ/Web。
- folder 移动在同一 Workspace 事务中使用 recursive CTE 检测后代，移动到自身或后代返回 `file_tree_cycle` 409。
- 非空 folder 删除返回 `file_tree_not_empty` 409，不做隐式递归删除。
- 兄弟冲突返回 `file_tree_name_conflict` 409，不自动生成 `(1)` 后缀。
- 文件节点名是文件树查询的权威展示名；重命名 file node 时同事务同步 `documents.title`。FAQ/Web 的 `documents.title` 本身是权威展示名。
- 检索结果先按 RetrievalEntry 取候选，再 JOIN 当前 Document 和可选 file node；File 优先返回 node name，并校验它与镜像 title 一致。

KnowledgeBase 的 `file_tree_root_id` 以 deferred 复合外键指向同 KB 节点，并由 deferred constraint trigger 同时验证该节点 `node_type=root`。每个 File Document 恰好一个活动 file node 由创建/删除事务和 deferred constraint trigger 在提交时共同保证，不能先提交“孤立 File Document”再补节点。

### 5.11 `document_assets` 与 `jobs`

`document_assets` 直接保存 Workspace/KB/Document/DocumentRevision lineage；资产属于具体解析版本。

`jobs` 直接保存 Workspace 和 KB，并按任务类型关联 Document、DocumentRevision 或 IndexGeneration。Job 类型使用明确列与 CHECK 约束，不引入任意 `target_type/target_id` 多态外键。

## 6. 数据库约束

### 6.1 复合外键

典型约束：

```sql
FOREIGN KEY (workspace_id, knowledge_base_id)
REFERENCES knowledge_bases (workspace_id, id);

FOREIGN KEY (workspace_id, knowledge_base_id, document_id)
REFERENCES documents (workspace_id, knowledge_base_id, id);

FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
REFERENCES document_revisions (workspace_id, knowledge_base_id, document_id, id);
```

RetrievalEntry 必须分别约束 Generation、DocumentRevision、ChunkSet、Chunk 和 ChunkRevision 的同租户 lineage。无法仅通过单列 UUID 外键建立投影记录。

FAQ content/question 必须引用包含 `kind=faq` 的 Revision 可引用键；file node 必须引用包含 `kind=file` 的 Document 可引用键。`file_tree_nodes.parent_id` 引用 `(workspace_id, knowledge_base_id, id)`，不允许把其它 KB 的节点设为 parent。

### 6.2 唯一约束

- 活动 KnowledgeBase 名称：`(workspace_id, lower(name)) WHERE deleted_at IS NULL`。
- 每个 KB 一个 root：`(workspace_id, knowledge_base_id) WHERE node_type='root'`。
- 每个 File Document 一个 file node：`(workspace_id, knowledge_base_id, document_id) WHERE node_type='file'`。
- file/folder 共用兄弟命名空间：`(workspace_id, knowledge_base_id, parent_id, lower(name)) WHERE node_type IN ('folder','file')`。
- 活动 Web URL：`(workspace_id, knowledge_base_id, source_uri) WHERE kind='web' AND deleted_at IS NULL`。
- Document revision：`(workspace_id, document_id, revision_no)`。
- FAQ question sequence：`(workspace_id, document_revision_id, sequence)`。
- FAQ normalized question：`(workspace_id, document_revision_id, normalized_question)`。
- ChunkSet 幂等键：`(workspace_id, document_revision_id, strategy, chunker_version, config_hash)`。
- Chunk sequence：`(workspace_id, chunk_set_id, sequence)`。
- Chunk revision：`(workspace_id, chunk_id, revision_no)`。
- 每个 Generation/Chunk 仅一个 staging 和一个 published RetrievalEntry，分别使用部分唯一索引。
- 每个 KB 同时只允许一个 `building` generation，使用部分唯一索引约束；构建完成的 `ready` 候选由 Service 在锁定 KB 后串行激活或退役，active generation 仍只由 KB 指针表达。

### 6.3 CHECK 约束

- 所有 version、sequence、count 非负，revision number 从 1 开始。
- JSON 配置和 metadata 必须是 JSON object。
- 所有状态、reason、edit source 和 manual edit disposition 使用可读 text + CHECK。
- Document/Revision kind 只允许 `file/faq/web` 且复合 FK 保证相等。
- File/FAQ/Web 的 `file_type`、文件字段、FAQ 字段组合满足第 5.4 节类型 CHECK。
- FAQ answer/question trim 后非空；question sequence 非负；提交时至少一个 question。
- file tree 的 root/parent/document/name 组合满足 node type CHECK。
- `enabled=true` 的 ChunkRevision content 去空白后必须非空。
- `edit_source=user` 必须有 editor；`system` 必须没有 editor。
- RetrievalEntry 只有在 embedding、dimension、FTS 和发布时间齐全时才能 published。
- Generation 的 indexed/failed count 不能超过 chunk count。

## 7. 发布状态机

### 7.1 初次导入

```text
DocumentRevision pending
  -> parsing
  -> ready
  -> ChunkSet building
  -> Chunks + system ChunkRevision
  -> ChunkSet ready
  -> RetrievalEntry staging
  -> atomic publish
```

发布事务锁定 KB、Document 和目标 generation，校验当前版本，切换 Document/Chunk 指针，发布 RetrievalEntry，并同时推进 `knowledge_bases.content_version` 与 active generation 的 `indexed_content_version`。

File 导入请求增加：

```text
parent_node_id optional，默认 KB root
node_name optional，默认安全规范化的 original_filename
```

格式验证与 raw object 写入成功后，一个 Workspace transaction 锁定 parent，校验其为 root/folder，并原子创建 File Document、file node、DocumentRevision 与 Parse Job。任一数据库写入失败都不留下半个可见文档；raw object 由补偿清理任务回收。

同名冲突返回 409，不自动改名。`dedupe=true` 命中同一 KB 内可复用的 File Revision 时返回已有 Document/node，不创建第二个文件树入口。

### 7.2 FAQ 创建与修改

创建 FAQ 时，一个 Workspace transaction 写入 FAQ Document、DocumentRevision、answer、全部 questions 和 Job/发布状态。FAQ 不进入 parser；worker 直接构建固定 FAQ ChunkSet/Chunk/Revision，生成 `search_content=questions` 的投影并走与 File 相同的单文档原子发布流程。

修改 FAQ 时客户端提交完整问题数组、完整回答和 `base_revision_id`。Service 锁定 Document 并校验基线，创建新的完整 Revision；旧 Revision、answer、questions、ChunkSet 和 RetrievalEntry 保留审计。发布失败不影响当前 active Revision；并发修改只有一个能发布。

### 7.3 单 Chunk 编辑

1. 读取 active ChunkRevision 作为 `base_revision_id`。
2. 创建 pending revision，转 indexing 并生成向量/FTS。
3. 写 staging RetrievalEntry。
4. 发布事务再次校验 active revision 未变化。
5. 退役旧 entry，发布新 entry，切换 Chunk 指针并推进内容版本。

并发编辑只允许一个成功；另一个返回稳定的 revision conflict，不做最后写入者静默覆盖。

停用是 `enabled=false` 的新 revision，发布时仅退役旧 entry。重新启用创建新 revision 并重新索引。

如果 Chunk 所属 Document `kind=faq`，在创建 ChunkRevision 前返回 `faq_chunk_immutable`；FAQ 不经过此状态机。

### 7.4 单文档替换或重解析

新 DocumentRevision、ChunkSet 和 RetrievalEntry 全部完成后，在一个事务中退役该文档的旧 entries、发布新 entries、切换 Document 指针并推进版本。查询只能看到完整旧文档或完整新文档。

### 7.5 全库重建

Generation 记录 `base_generation_id` 与 `source_content_version`。只换模型或检索参数时复用 File/Web/FAQ 当前 ChunkSet/ChunkRevision；改变普通分块配置时只为 File/Web active DocumentRevision 生成新 ChunkSet，FAQ 始终复用固定 `strategy=faq` 的单 Chunk 产物。

激活前必须满足：

```text
base_generation_id == knowledge_base.active_index_generation_id
source_content_version == knowledge_base.content_version
generation.status == ready
```

不满足时 generation 进入 stale，不能覆盖并发内容变化。激活事务只切换 KB 指针和少量状态，不批量更新全部 RetrievalEntry。

文件树重命名/移动不改变 `content_version`，因此不会让 building Generation stale；搜索在候选检索后解析当前名称。FAQ 内容修改属于内容变化，按正常单文档发布递增版本。

### 7.6 分块策略与人工编辑

新分块策略不自动迁移人工编辑。构建报告统计人工修改和停用数量；存在人工编辑时 `manual_edit_disposition=pending`，管理员显式确认归档后才变为 `archive_confirmed` 并允许激活。旧 ChunkSet、Revision 和 Generation 在保留期内可审计和整体回滚。

FAQ system Chunk 不计入人工编辑统计，也不因普通 chunking config 改变而归档。

### 7.7 文件树操作

- 创建 folder：锁定 parent，校验同 KB 且为 root/folder，再检查共享兄弟命名空间。
- rename/move：锁定目标、旧 parent、新 parent；folder move 使用 recursive CTE 排除自身及后代；file rename 同事务同步 `documents.title`。
- 删除 folder：锁定节点并以同一事务检查无 child；非空返回 409。
- 删除 File Document：先让 RetrievalEntry 退出查询，再删除/归档唯一 file node；不得留下指向 deleted Document 的活动节点。
- 上述纯树操作不触碰 Revision、ChunkSet、ChunkRevision、Generation 或对象存储 key。

## 8. 索引与查询

### 8.1 范围索引

RetrievalEntry 至少建立：

```text
(workspace_id, knowledge_base_id, index_generation_id, state)
(workspace_id, index_generation_id, document_id, state)
(workspace_id, index_generation_id, chunk_id, state)
```

所有搜索先解析 KB active generation，再同时限定 Workspace、KB、Generation 和 published state。

### 8.2 FTS

`fts_document` 是普通 `tsvector` 列，由 typed FTS adapter 生成，不使用写死 tokenizer 的 generated column：

```sql
CREATE INDEX idx_retrieval_entries_fts
ON retrieval_entries USING gin (fts_document)
WHERE state = 'published';
```

这允许后续改进中文分词而不污染事实层。

FTS adapter 的输入固定为 RetrievalEntry.`search_content`，不得从 `content` 或 FAQ answer 重建。测试必须用答案中独有、问题中不存在的词证明 FAQ answer 无法通过 FTS 命中。

### 8.3 HNSW

保留 `halfvec` 和 798/1024/2048/3584 四个部分索引。索引与查询表达式必须完全一致：

```sql
CREATE INDEX idx_retrieval_entries_hnsw_1024
ON retrieval_entries
USING hnsw ((embedding::halfvec(1024)) halfvec_cosine_ops)
WITH (m = 16, ef_construction = 64)
WHERE dimension = 1024 AND state = 'published';
```

首版不做物理分区。RetrievalEntry 是可重建投影；真实规模证明单表过滤不足时，再按 dimension + Workspace hash 分区。

### 8.4 混合检索

Vector topK 和 FTS topK 在同一个 active generation 内独立取候选，由 application 使用 RRF 融合。向量输入与 FTS 都使用 `search_content`，融合后的 evidence 正文使用 `content`。

候选阶段只返回 lineage、score 与必要投影字段；evidence 组装阶段在同一 Workspace 上下文批量读取当前 Document，并 LEFT JOIN 当前 file node。File 的展示名取节点名，FAQ/Web 取 `documents.title`。结果包含当前名称、SourceAnchor、metadata 和分路 score，不生成 LLM 答案。

## 9. RLS-ready 合同

本次只完成直接 tenant key、复合约束和事务边界，不启用 policy。`faq_revision_contents`、`faq_revision_questions` 与 `file_tree_nodes` 和其它租户业务表一样直接保存 `workspace_id NOT NULL`，不能通过 JOIN 间接推导租户。

未来 application 使用如下抽象进入租户事务：

```go
WithinWorkspace(ctx, workspaceID, fn)
```

Infrastructure 的未来实现等价于：

```sql
BEGIN;
SELECT set_config('app.workspace_id', $1, true);
-- tx-bound repositories only
COMMIT;
```

正式启用时，每张租户表使用相同的 `USING` 与 `WITH CHECK`：

```sql
workspace_id = NULLIF(current_setting('app.workspace_id', true), '')::uuid
```

并同时执行 `ENABLE ROW LEVEL SECURITY` 与 `FORCE ROW LEVEL SECURITY`。普通应用角色不得拥有 `BYPASSRLS`；迁移和维护使用独立数据库角色。

Service 定义自己所需的最小事务接口，application 不持有 `*gorm.DB`，也不通过 context 偷藏数据库句柄。所有事务内 Repository 必须绑定同一个 `tx`。

Worker 内部 payload 携带 `workspace_id + job_id`；handler 先进入 Workspace transaction，再按复合键读取 Job。外部请求不能直接构造可信 worker payload。

## 10. 删除、保留与配额

- KnowledgeBase 和 Document 使用 `deleted_at` 提供有限恢复窗口。
- file_tree_nodes 不使用独立软删除语义；File Document 删除事务负责先移除活动节点，Document 恢复时必须重新校验原 parent/name 是否可用。
- 删除、停用或版本切换必须立即让对应 RetrievalEntry 退出查询。
- 失败 staging 默认保留 24 小时，retired generation 默认保留 7 天；两个期限都从 YAML `retrieval` 配置读取，不能在业务代码中硬编码。
- RetrievalEntry、失败 staging 和 retired generation 在保留期后物理删除，避免 HNSW 膨胀。
- 历史 Revision 和对象存储文件遵循 Workspace 保留策略；对象只有在版本不可恢复后删除。
- SaaS 配额分别统计原始对象、历史文本和 active/retired 向量，不把 soft-deleted 重型数据永久排除在计费之外。

## 10.1 权限

- Workspace member 可以读取 KnowledgeBase、Document、Chunk、Revision、Generation 状态并执行 search，继续保留当前上传文档能力。
- Workspace member 可以创建 FAQ、上传 File、创建/重命名/移动/删除自己可操作 KB 内的文件树节点；角色边界沿用现有 Document 写权限，不新增授权表。
- Chunk 人工编辑、启用、停用、创建/激活 Generation 和确认归档人工编辑要求 Workspace admin 或 owner。
- platform admin 不获得绕过 Workspace 上下文的业务查询路径；操作具体 Workspace 资源时仍进入该 Workspace 的事务上下文。
- RLS 启用后权限判断仍由现有 application/middleware 完成，RLS 只负责租户行隔离，不替代角色授权。

## 10.2 配置指纹

`model_config_hash` 与 `config_hash` 都使用 RFC 8785 风格的确定性 JSON 编码结果计算 SHA-256 小写十六进制。实现使用项目内显式 canonical encoder，禁止直接依赖 Go map 的遍历顺序。凭证、显示名称、创建时间和运行时统计不进入哈希。

## 11. 迁移策略

新增 `000005_knowledge_retrieval_v2.up.sql/down.sql`。Up 按依赖顺序删除现有知识处理表，并创建 `knowledge_bases`、`knowledge_base_index_generations`、`documents`、`document_revisions`、`faq_revision_contents`、`faq_revision_questions`、`document_chunk_sets`、`chunks`、`chunk_revisions`、`file_tree_nodes`、`document_assets`、`jobs` 与 `retrieval_entries`；不修改且不删除 Workspace、授权、用户、会话、邀请、API Token、ModelProvider 和 Model 数据。

这是开发期破坏性迁移，不回填旧 KB 数据。Down 可以恢复旧版空结构，但不能恢复已删除的数据。旧对象存储文件由单独开发清理命令处理，SQL migration 不访问文件系统。

## 12. 测试与验收

### 12.1 Migration/Repository

- `000005 up/down/up` 在真实 PostgreSQL + pgvector 通过。
- 授权表和 Model 表数据在 Up 后完全保留。
- 所有租户列非空，跨 Workspace/KB/Document lineage 写入被数据库拒绝。
- 所有 JSON、状态、revision、计数和发布 CHECK 生效。
- Document/Revision kind 不一致、FAQ 关联非 FAQ Revision、file node 关联 FAQ/Web Document 均被数据库拒绝。
- 每个 KB 只有一个 root；跨 KB parent、大小写兄弟重名、一个 File Document 两个节点均被拒绝。
- FAQ 缺 answer、零 question、重复 sequence 或重复 normalized question 均被拒绝。
- Repository 全部显式接受 Workspace 并透传 Context。

### 12.2 并发与发布

- 同一 base revision 的两个并发编辑只能发布一个。
- 重解析失败不影响旧 DocumentRevision。
- 新 generation 构建期间旧 generation 继续返回完整结果。
- 构建期间内容变化使 generation stale。
- generation 切换前后都不存在混合结果。
- Chunk 停用立即退出检索，重新启用后重新索引。
- 分块配置变化且存在人工编辑时，未确认不能激活。
- Worker 重试不产生重复 Revision、ChunkSet 或 RetrievalEntry。
- FAQ 创建/修改原子发布；失败或并发冲突保留完整旧答案。
- 普通 chunking config 变化不重分 FAQ；仅换模型时 File/FAQ 都复用 ChunkSet。
- 文件树 rename/move 不递增 content version，不使 building Generation stale，也不移动对象存储 key。
- folder 不能移动到自身/后代，非空 folder 不能删除，冲突稳定映射为 409。

### 12.3 检索

- 四种维度写入、查询与 HNSW 表达式一致。
- FTS 只返回 active generation 的 published 行。
- Vector/FTS 候选始终限定 Workspace 和 KB。
- FAQ 只用问题命中；答案独有词不能命中，返回正文等于答案。
- File rename 后无需重建投影，search 立即返回当前节点名。
- 删除 Document 会清理或退役其投影与资产引用。

### 12.4 质量门禁

```bash
gofmt -w .
go test ./... -count=1
go test -tags=integration ./... -count=1
go vet ./...
git diff --check
pnpm --dir web check
pnpm --dir web test
pnpm --dir web build
```

只有涉及前端或 API 合同的阶段才要求局部前端门禁，但最终合并前执行完整套件。

## 13. WeKnora 取舍

采用：

- `source_content` 与有效编辑内容分离。
- Chunk Revision、编辑人、启停和索引状态。
- 检索正文、FTS 与向量相邻的查询投影。
- 每张 SaaS 租户表直接保存 tenant key。
- 按向量维度建立部分 HNSW 索引。
- 将 FAQ 的搜索文本和 evidence 文本显式分离。
- 使用独立文件树表达知识库组织，不把对象存储路径当目录。

不采用：

- `VARCHAR(36)` ID、独立整数 tenant ID 和无复合外键保护的重复 lineage。
- `pre_chunk_id/next_chunk_id`、关系 Chunk JSON 和图查询预留。
- 数字 flags/status、单列 tag ID 和外部整数 seq ID。
- 通用 `source_id/source_type` Embedding 多态表。
- VectorStore 注册表和外部向量库提前抽象。
- 重型向量与历史数据无限软删除。
- 按相似度自动迁移跨分块策略的人工编辑。

## 14. 实施切片

实施按八个可独立审查的切片推进：

1. v2 migration、领域类型和 Row/codec。
2. Workspace transaction boundary 与 Repository。
3. KnowledgeBase root 与 File Tree CRUD/移动约束。
4. File DocumentRevision + ChunkSet 流水线。
5. FAQ 完整版本、固定单 Chunk 与发布流水线。
6. Generation + RetrievalEntry + Embedding/FTS adapter。
7. Chunk 编辑、启停、并发发布和人工编辑冲突确认。
8. Search 当前名称解析、清理、运行指标、文档同步与最终回归。

RLS policy 正式启用属于后续独立设计与迁移；本设计保证启用时无需再次重构 tenant key 或资源 lineage。
