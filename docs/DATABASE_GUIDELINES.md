# 数据库操作指南

本指南规定琅嬛当前知识处理数据模型的分层、租户边界、GORM 写法、迁移与检索约束。新增表、Repository、事务或原生 SQL 前必须先通读。

## 1. 数据访问边界

```text
interfaces / application service
        │  只依赖使用方定义的 Repository / Store 接口
        ▼
domain model + application transaction contract
        │
        ▼
infrastructure/db
        │  Row + codec + GORM
        ▼
PostgreSQL + pgvector
```

- `domain` 与 `application` 不得持有或导入 `*gorm.DB`。
- 接口定义在使用方；`infrastructure/db` 只实现，不把 GORM 类型泄漏到上层。
- Repository 是持久化薄封装：负责查询、Row/领域模型转换和数据库错误映射，不推进业务状态机或调用外部服务。
- 跨表业务单元使用 application 定义的最小 `XxxTx` / `XxxStore` 合同；基础设施在 Workspace transaction 中构造 tx-bound 实现。

## 2. Workspace transaction 与 RLS-ready 合同

所有租户业务资源直接保存 `workspace_id uuid NOT NULL`。业务读写必须显式携带 Workspace，并通过 `WorkspaceTxRunner.WithinWorkspace`：

```go
err := runner.WithinWorkspace(ctx, workspaceID, func(tx *gorm.DB) error {
    // 此处所有查询必须使用传入的 tx。
    return tx.WithContext(ctx).
        Where("workspace_id = ? AND id = ?", workspaceID, id).
        First(&row).Error
})
```

Runner 在事务开始时执行：

```sql
SELECT set_config('app.workspace_id', $1, true);
```

第三个参数为 `true`，配置只在当前事务有效，不会泄漏到连接池中的下一次请求。即使查询已经带 `workspace_id`，也不能绕过该 transaction-local 上下文；前者是当前隔离条件，后者是未来 RLS policy 的数据库上下文。

当前迁移尚未执行 `ENABLE ROW LEVEL SECURITY`。正式启用 RLS 必须另做迁移和负向测试，并满足：

- 普通应用角色无 `BYPASSRLS`，表同时 `ENABLE` 与 `FORCE ROW LEVEL SECURITY`；
- policy 使用 `NULLIF(current_setting('app.workspace_id', true), '')::uuid`；
- HTTP、worker、MCP、cleanup 和维护入口均进入 `WithinWorkspace`；
- migration role 与应用 role 分离。

## 3. 直接租户键与复合外键

每张租户表必须具有：

```sql
workspace_id uuid NOT NULL,
UNIQUE (workspace_id, id)
```

下级资源不得只靠多层 JOIN 推导租户。外键要携带完整 lineage，例如：

```sql
FOREIGN KEY (workspace_id, knowledge_base_id, document_id, document_revision_id)
REFERENCES document_revisions
  (workspace_id, knowledge_base_id, document_id, id)
```

`documents.kind` 是不可变业务类型 `file|faq|web`；`document_revisions.kind` 冗余保存相同值并由包含 `kind` 的复合外键保证一致。File Tree 的 file node 也以常量 `document_kind='file'` 的复合外键阻止关联 FAQ/Web。

## 4. 事实层与检索投影

知识处理事实层按以下 lineage 保存：

```text
documents
  └─ document_revisions
       ├─ faq_revision_contents + faq_revision_questions
       └─ document_chunk_sets
            └─ chunks
                 └─ chunk_revisions
```

- Document 保存稳定身份、不可变 kind、当前 title/source/status 和 active Revision 指针。
- `file_type`、原始文件名、raw storage key、hash、解析产物属于 DocumentRevision；重新分块不创建新 Revision。
- FAQ 的问题集合与一个回答作为完整 Revision 原子保存；FAQ 固定生成一个 `strategy=faq` Chunk。
- Chunk 保存来源事实；人工编辑追加 ChunkRevision，不覆盖 `source_content`。
- `file_tree_nodes` 是独立组织投影，只组织 File Document，不承载对象存储路径、内容版本或权限继承。

检索层由以下表构成：

```text
knowledge_base_index_generations
  └─ retrieval_entries
```

Generation 保存不可变的模型、分块和检索配置快照。KnowledgeBase 只有一个 active Generation；inactive Generation 构建完成后通过 compare-and-swap 激活。

RetrievalEntry 是可重建投影，不是 Chunk 权威内容：

- `search_content` 用于 Embedding 与 FTS；`content` 是召回后返回正文。
- 普通 File/Web 通常两者来自 ChunkRevision 的 embedding content / content。
- FAQ 的 `search_content` 只含所有问题，`content` 只含回答，因此问题可召回、答案独有词不会被索引。
- RetrievalEntry 不保存权威 Document title；检索结果在同一 Workspace 上下文读取当前 Document，并对 File 读取当前 file node 名称。

## 5. 领域模型与 Row 双层分离

- 领域模型位于 `internal/domain/model/`，不写 GORM/JSON tag。
- Row 按职责位于 `internal/infrastructure/db/*_rows.go`，例如 `document_rows.go`、`chunk_rows.go`、`retrieval_rows.go`。
- 通过显式 `toRow` / `fromRow` codec 转换；Repository 对外只返回领域模型或 application 定义的投影。
- 不创建模糊的 `utils.go`、`common.go` 或通用大 Row 文件。

JSONB 使用项目 `JSONMap`，nil 写入规范化为 `{}`。需要稳定 hash 的配置先转成确定性 JSON；不得把凭证、显示字段、时间或统计计入 Generation 配置指纹。

## 6. 查询与错误处理

普通 CRUD 使用 GORM 方法链，所有 I/O 都透传 `context.Context`：

```go
err := tx.WithContext(ctx).
    Where("workspace_id = ? AND knowledge_base_id = ?", workspaceID, kbID).
    Order("created_at DESC, id DESC").
    Find(&rows).Error
```

- 单条查询用 `First`，列表用 `Find`，批量写入用 `CreateInBatches`。
- `gorm.ErrRecordNotFound` 在 Repository 映射为 `domainerrors.ErrNotFound`。
- 其它错误每层只包装一次上下文，并保留 `%w` 错误链。
- 唯一键、复合外键等数据库冲突通过统一翻译函数映射为稳定领域错误。
- pgvector、FTS、递归 CTE、`FOR UPDATE SKIP LOCKED` 等使用 GORM `Raw` / `Exec` / `clause.Expr`，不另开驱动连接。

## 7. halfvec、FTS 与 HNSW

`retrieval_entries` 同行保存 `fts_document tsvector`、`embedding halfvec` 与 `dimension`。支持维度固定为 798、1024、2048、3584。

每个维度都有部分 HNSW 表达式索引。查询 SQL 必须和迁移中的表达式完全一致，例如 1024 维：

```sql
ORDER BY (embedding::halfvec(1024)) <=> (?::halfvec(1024))
```

并同时限定：

```sql
WHERE dimension = 1024 AND state = 'published'
```

不得把维度作为任意字符串插入 SQL；代码只在四条固定 SQL 中选择。集成测试使用 `EXPLAIN` 与 `enable_seqscan=off` 证明命中对应 HNSW 索引。

FTS 在 staging 时由 `to_tsvector(config, search_content)` 生成并保存。查询只匹配保存的 `fts_document`，不能从返回用 `content` 临时重建，否则会破坏 FAQ 的“索引问题、返回回答”语义。

### 7.1 中文全文检索（zhparser）

默认 `fts_config` 为 `zhparser`（迁移 000011 引入），对中文按词典切词（如「人工智能驱动的知识管理系统」→ `人工智能/驱动/知识/管理系统`）。迁移前的默认 `simple` 分词把整句中文当作单个 token，中文关键词查询无法命中，因此：

- **扩展与配置由迁移 000011 注册**：迁移在 `public` schema 中执行 `CREATE EXTENSION zhparser`，并幂等创建 `public.zhparser` text search configuration（词性映射 `n,v,a,i,e,l` → `simple`）。扩展本体不在 SQL 层分发，由部署/测试镜像预装（测试见 `docker/postgres-test/Dockerfile`，`make test-image` 构建 `langhuan-test-postgres:pg17`）。
- **`fts_config` 按 generation 存储**（`RetrievalConfig["fts_config"]`）：写入 `to_tsvector(config, search_content)` 与查询 `plainto_tsquery(config, ?)` 使用同一配置，写入/查询天然自洽。
- **旧 generation 不自动重建**：迁移只注册分词能力，不重算已有 `fts_document`。旧 generation 继续使用其快照中的分词器（如 `simple`）；要享受中文分词，需重建对应 generation（重跑索引 pipeline 或新建 generation）。切换分词器后必须重建 generation，否则写入与查询配置不一致。
- **回滚保留分词对象**：000011 的 down migration 不删除 `public.zhparser` 配置或 zhparser extension。Generation 快照可能长期保存 `fts_config=zhparser`；删除对象会让这些快照在写入和查询时失效。需要卸载扩展时，必须先迁移或清理所有引用它的 generation，再由运维显式处理。
- **生产部署**：生产数据库镜像必须预装 zhparser（参考 `docker/postgres-test/Dockerfile` 的构建步骤），否则迁移 000011 的 `CREATE EXTENSION` 会失败。
- **手动安装**：Ubuntu 24 服务器与 macOS 本机的手动安装步骤见 **7.2**。
- **可选调优**：zhparser 支持 `multi_short` 等多粒度切分、数据库级自定义词表（`zhprs_custom_word`），可按检索效果需要启用；默认保持迁移中的最小映射。

### 7.2 手动安装 pgvector + zhparser（Ubuntu 24 / macOS）

适用场景：未使用项目自建测试镜像（`docker/postgres-test/Dockerfile`）的数据库环境，如 Ubuntu 24 生产服务器、mac 本机开发。以 **PostgreSQL 17** 为例（pgvector v0.8.6 支持 PG13+，zhparser 支持 PG 9.2+，与项目验证版本一致）。

**通用验证**（两种平台装完后都执行，`<your-db>` 换成实际库名）：

1. 安装完成后，先确认 PostgreSQL 已发现扩展控制文件。此时 text search configuration 尚未创建，不能直接调用 `to_tsvector('zhparser', ...)`：

```sql
SELECT name, default_version
FROM pg_available_extensions
WHERE name IN ('vector', 'zhparser');
```

预期同时返回 `vector` 和 `zhparser`。若缺少任意一行，先修复对应扩展安装，不要继续迁移。

2. 对目标数据库执行琅嬛迁移，至少应用到 000011。迁移会在 `public` schema 中创建 extension 和 `public.zhparser` 配置：

```bash
go run ./cmd/langhuan migrate
```

3. 迁移成功后再验证对象 namespace 和中文分词：

```sql
SELECT extname, extnamespace::regnamespace
FROM pg_extension
WHERE extname IN ('vector', 'zhparser');

SELECT cfgname, cfgnamespace::regnamespace
FROM pg_ts_config
WHERE cfgname = 'zhparser' AND cfgnamespace = 'public'::regnamespace;

-- 预期包含 '人工智能':1；simple 分词会把整句中文当作单个 token。
SELECT to_tsvector('public.zhparser'::regconfig, '人工智能驱动的知识管理系统');
```

分词配置的词性映射 `n,v,a,i,e,l` 由迁移 000011 创建，无需手动建。

#### Ubuntu 24.04（PostgreSQL 通过 apt 安装）

1. **安装 PostgreSQL 17**：Ubuntu 自带仓库只到 PG16，需用 PGDG 官方仓库：

   ```bash
   sudo apt install -y postgresql-common
   sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y
   sudo apt install -y postgresql-17 postgresql-server-dev-17
   ```

2. **安装 pgvector**：PGDG 提供现成包，无需编译：

   ```bash
   sudo apt install -y postgresql-17-pgvector
   ```

   若需源码编译（与 Dockerfile 一致），备选：

   ```bash
   sudo apt install -y build-essential git
   git clone --depth 1 --branch v0.8.6 https://github.com/pgvector/pgvector.git
   cd pgvector && make && sudo make install
   ```

3. **编译安装 zhparser**（无现成包，需 SCWS；github 源码包是 autotools 工程，`configure` 需 `autoreconf` 生成）：

   ```bash
   sudo apt install -y build-essential autoconf automake libtool git curl
   # SCWS 1.2.3
   cd /tmp && curl -fsSL -o scws.tar.gz \
     https://github.com/hightman/scws/archive/refs/tags/1.2.3.tar.gz
   mkdir -p scws && tar -xzf scws.tar.gz --strip-components=1 -C scws
   cd scws && autoreconf -i && ./configure --prefix=/usr/local \
     && make && sudo make install
   sudo ldconfig
   # zhparser v2.3（SCWS_HOME 默认 /usr/local，与上面一致，无需传参；
   # pg_config 不在默认 PATH，需显式加入）
   export PATH="/usr/lib/postgresql/17/bin:$PATH"
   cd /tmp && git clone --depth 1 --branch v2.3 https://github.com/amutu/zhparser.git
   cd zhparser && make && sudo make install
   ```

4. 执行「通用验证」。zhparser 的 `make` 通过 `pg_config` 定位 PG；多版本并存时确保 `PATH` 中是 17 的 `pg_config`（`/usr/lib/postgresql/17/bin`）。

#### macOS（PostgreSQL 通过 Homebrew 安装）

1. **安装 PostgreSQL 17 与 pgvector**：Homebrew 的 `postgresql@17` **不含** pgvector，需单独安装 `pgvector` formula（它会自动针对当前 `postgresql@17` 编译）：

   ```bash
   brew install postgresql@17 pgvector
   # postgresql@17 是 keg-only，需把 pg_config 加入 PATH
   export PATH="/opt/homebrew/opt/postgresql@17/bin:$PATH"
   ```

2. **编译安装 zhparser**（无 Homebrew formula）：

   ```bash
   brew install scws
   git clone --depth 1 --branch v2.3 https://github.com/amutu/zhparser.git
   cd zhparser && make SCWS_HOME=/opt/homebrew && make SCWS_HOME=/opt/homebrew install
   ```

   `SCWS_HOME=/opt/homebrew` 是必需参数：Homebrew 的 scws 装在 `/opt/homebrew`（Intel Mac 为 `/usr/local`），而 zhparser 默认找 `/usr/local`。

3. 执行「通用验证」。`make install` 写入 `/opt/homebrew/share/postgresql@17` 与 `/opt/homebrew/lib/postgresql@17`；若报 `Operation not permitted`，检查 `/opt/homebrew` 目录写权限（个别机器存在系统级写保护，与 brew 报的权限位无关）。

## 8. 事务与指针切换

跨多表操作必须原子完成，事务内部始终使用传入的 `tx`：

- KB 创建：KnowledgeBase + root node + 空 ready Generation；
- File 导入：Document + 唯一 file node + DocumentRevision + Job；
- FAQ 更新：完整 Revision + answer + 全量 questions + Job；
- 发布：校验 staging 完整性，退役旧投影，发布新投影，切换 Document/Chunk 指针并推进 content version；
- Generation 激活：锁定 KB/candidate/base，校验 base pointer 和 content version，退役旧代并切换唯一 active 指针；
- Document 删除：先退役投影，软删除 Document；File 同事务移除唯一 file node，FAQ/Web 不触碰树。

延迟外键/constraint trigger 用于验证 active 指针、FAQ 完整性、KB root 和 File Document 唯一节点等提交时不变量。

## 9. 删除与有限批量清理

KnowledgeBase/Document 使用 `deleted_at` 提供恢复窗口。软删除 Document 不立即删除 DocumentRevision、raw object 或资产；对象只有在版本不可恢复后才允许删除。

可重建投影由 `RetrievalCleanupService` 按 Workspace 清理：

- staging/failed Entry 使用 `failed_staging_retention`；
- retired Entry 和 retired Generation 使用 `retired_generation_retention`；
- 按时间和 UUID 稳定排序，`FOR UPDATE SKIP LOCKED`，一次 transaction 最多一个 `cleanup_batch_size`；
- active Generation 永不进入物理删除候选；
- Generation 的 `base_generation_id` 外键只在被引用历史代删除时置空该可空列，不得置空 `workspace_id/knowledge_base_id`。

Cleanup 仍然必须进入 `WithinWorkspace`，不允许用无租户的全表后台任务绕过 RLS-ready 边界。

## 10. 迁移与测试

- 迁移位于 `internal/infrastructure/migrate/migrations/`，版本严格递增，每个 up 有对应 down。
- 迁移不访问对象存储，不修改授权、Workspace、用户、session、邀请、API token 或 Model/Provider 合同，除非任务明确要求。
- Repository 约束与原生 SQL 优先使用真实 PostgreSQL 集成测试，不 mock GORM。
- 租户表测试至少覆盖同 Workspace 成功、跨 Workspace/KB 拒绝、空/错误 lineage、并发冲突与 transaction-local `app.workspace_id`。

常用门禁：

```bash
gofmt -w internal cmd
go test ./... -count=1
go test -tags=integration ./... -count=1
go vet ./...
git diff --check
```

一句话总则：领域层不碰 GORM；租户读写都在 Workspace transaction 中；事实层不可变、投影可重建；复合外键守住 lineage；向量查询表达式必须与 HNSW 索引完全一致。
