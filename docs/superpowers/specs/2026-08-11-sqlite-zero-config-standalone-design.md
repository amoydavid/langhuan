# SQLite 零配置单机模式设计规格

**日期：** 2026-08-11

**状态：** 已确认，待实施

**目标版本：** v1.0.0 前

**上游调研：** `docs/SQLITE_SUPPORT.md`

## 1. 目标与验收标准

本设计为琅嬛增加纯 Go SQLite 单机运行形态，同时保留现有 PostgreSQL + Redis 生产形态。真实目标不是“增加一个数据库 driver”，而是让体验用户拿到发布二进制后直接运行，不准备 YAML、PostgreSQL 或 Redis，也能进入 Web Console 并使用完整知识处理与检索主链路。

零配置启动必须满足：

1. 用户直接执行 `langhuan`，当前目录没有 `config.yaml` 时，服务使用内建 standalone profile。
2. 默认持久数据位于当前用户主目录的 `~/.langhuan-data/`：
   - SQLite：`~/.langhuan-data/langhuan.db`
   - 原始文档与资产：`~/.langhuan-data/raw-documents/`
   - 自动生成的凭证主密钥：`~/.langhuan-data/credential.key`
3. 默认不连接 PostgreSQL、Redis、S3 或其它本地基础设施；外部 Embedding/Parser Provider 只在用户配置并实际调用时访问。
4. HTTP、MCP、内存 worker 与 Web Console 同进程启动，数据库迁移自动完成。
5. `CGO_ENABLED=0` 的 release build 和测试通过。
6. PostgreSQL + Redis 显式配置路径保持行为和性能特征不变，现有集成测试零回归。
7. SQLite 下 File、FAQ、Web、Chunk、Generation、向量检索、中文全文检索、RRF、SearchRun、回放、认证、API Key 与同步任务的数据库合同可用。
8. 所有 SQLite 查询显式携带 `workspace_id`；不以缺失 RLS/GUC 为理由降低租户隔离。

### 1.1 不在范围内

- 把 SQLite 定位为多副本、高并发生产数据库。
- 在 SQLite 中复刻 pgvector HNSW 的近似索引性能。
- SQLite RLS、事务 GUC、advisory lock 或 PostgreSQL 扩展兼容层。
- 在 PostgreSQL 和 SQLite 数据文件之间自动转换或在线迁移数据。
- 持久化内存队列；standalone 进程退出时尚未执行的队列项允许丢失，仅由已有 source cleanup/force latch 补偿路径和用户触发的 retry/reindex 恢复可重建任务。
- 改变领域模型、HTTP/MCP 业务合同或引入新的检索 port。

## 2. 已确认的产品合同

### 2.1 配置选择优先级

启动配置来源固定为以下顺序，不做模糊回退：

1. **显式 `-config <path>`**：严格读取指定 YAML。文件不存在、不可读、解析失败或校验失败时立即退出。
2. **未显式传 `-config` 且当前目录存在 `config.yaml`**：继续读取该文件，兼容现有部署。
3. **未显式传 `-config` 且 `config.yaml` 明确不存在**：使用内建 standalone profile。

程序必须区分 flag 的默认值和用户是否显式传参，不能把“默认字符串为 `config.yaml`”当成显式选择。可通过 `FlagSet.Visit` 或返回结构化 `ConfigSelection{Path, Explicit}` 实现。

显式配置错误绝不回退 standalone；默认 `config.yaml` 的 `Stat` 若返回权限、I/O 或其它非 `IsNotExist` 错误也必须失败。否则拼错生产配置路径或暂时不可读的生产配置可能静默启动一个新的空 SQLite 实例。

### 2.2 Standalone profile

内建 standalone profile 至少包含：

```text
server.http_addr        = 127.0.0.1:8080
server.base_url         = http://127.0.0.1:8080
server.run_http         = true
server.run_worker       = true
database.driver         = sqlite
database.dsn            = ~/.langhuan-data/langhuan.db（运行期展开为绝对 file DSN）
database.auto_migrate   = true
redis.enabled           = false
storage.driver          = local
storage.raw_document_dir= ~/.langhuan-data/raw-documents
auth.session.secure_cookie = false
auth.password.enabled   = true
auth.oidc.enabled       = false
```

其余 ingest、queue、retrieval、search、observability 等参数复用现有安全默认值。localhost 明文 HTTP 下必须关闭 Secure Cookie，否则注册/登录后的浏览器会话不可用。显式 YAML 配置继续保留当前生产默认与严格校验，不因 standalone profile 放宽。

### 2.3 默认数据目录和权限

使用 `os.UserHomeDir()` 取得用户主目录并通过 `filepath.Join(home, ".langhuan-data")` 构造路径，不手工拼接 `~`。无法解析或写入主目录时返回可行动错误，不回退工作目录或临时目录。

- 数据根目录与 raw document 目录以 `0700` 创建。
- `credential.key` 以 `0600` 创建。
- 新 SQLite 文件在 driver 打开前以 `O_CREATE|O_EXCL`、`0600` 预创建；已有文件权限过宽时尝试收紧，失败则拒绝启动，避免数据库曾以宽权限短暂存在。
- Unix 平台严格执行并校验权限；Windows 没有等价 POSIX mode，只执行可用的用户目录隔离并在文档中说明边界。
- 日志可显示数据目录和数据库路径，但仍通过现有脱敏规则禁止输出 DSN query、密钥或文档内容。

### 2.4 自动凭证主密钥

standalone 首次启动生成 32 个加密随机字节并以 Base64 文本写入 `credential.key`。写入流程必须使用 `O_CREATE|O_EXCL` 和 `0600`，写完后 `fsync` 并关闭。并发首次启动时，创建失败的一方遇到 `EEXIST` 后做有界重读，直到胜出进程写完有效内容；不会覆盖对方文件。若创建者崩溃留下空文件，后续启动按损坏密钥 fail-fast，不能擅自轮换。

后续启动只读取并严格校验已有密钥。文件为空、Base64 非法、解码长度不是 32 字节、权限无法收紧或读取失败时立即退出；绝不能自动生成新密钥覆盖，否则数据库内 Provider、飞书连接和 Workspace API Key 密文将永久不可恢复。

显式 YAML 部署继续要求 `credentials.encryption_key`。自动密钥文件只属于无配置 standalone profile，不改变生产备份与密钥管理合同。

## 3. 技术选型

| 能力 | 选型 | 约束 |
|---|---|---|
| GORM SQLite Dialector | 项目内 `internal/infrastructure/db/sqlitedialect` | 只实现项目使用的 GORM Dialector/错误翻译，底层只打开 modernc driver |
| SQLite SQL driver | `modernc.org/sqlite` v1.56.0 | 业务连接、迁移和 vec 共用唯一 driver/lib |
| Vector functions | `_ "modernc.org/sqlite/vec"` | 内嵌 sqlite-vec v0.1.9，自动注册到新连接 |
| SQLite migration | `github.com/golang-migrate/migrate/v4/database/sqlite` v4.19.1 | 禁止 import cgo 的 `database/sqlite3` |
| 中文分词 | `github.com/go-ego/gse` v1.0.2 | 使用内嵌词典，查询与写入共用同一 tokenizer |
| 全文索引 | SQLite FTS5 + BM25 | token 由注入的 gse adapter 生成 |
| 队列 | 进程内有界优先队列 + worker pool | 实现现有 `queue.JobQueue`，重用现有 worker handler |
| 登录限流/OIDC state | mutex 保护的内存实现 | 单进程 TTL 语义 |

评审已排除 `github.com/glebarez/sqlite`：它会经 `github.com/glebarez/go-sqlite` 注册名为 `sqlite` 的 `database/sql` driver，而 golang-migrate 的纯 Go `database/sqlite` 又 blank-import `modernc.org/sqlite` 并注册同名 driver；同一进程同时导入会在 init 阶段重复注册。项目内 Dialector 参考其 MIT/GORM SQLite clause 行为，但直接使用唯一的 `modernc.org/sqlite` driver。若借鉴第三方实现，必须保留其版权与许可证声明。

项目内 Dialector 的边界固定为实现 GORM `Dialector` 所需的 `Name`、`Initialize`、`Migrator`、`DataTypeOf`、`DefaultValueOf`、`BindVarTo`、`QuoteTo`、`Explain`，注册 SQLite 所需的 INSERT/LIMIT/FOR clause builder，并实现 `SavePoint`、`RollbackTo` 与 `ErrorTranslator`。错误翻译只处理 `*modernc.org/sqlite.Error` 的稳定 extended code：`2067`/`1555` 映射 `gorm.ErrDuplicatedKey`，`787` 映射 `gorm.ErrForeignKeyViolated`，其它错误原样返回。实现前必须先以 compatibility test 验证 GORM CRUD、约束翻译、时间扫描、事务 savepoint、迁移、FTS5 和 vec 函数，不能只以编译通过作为证据。

## 4. 数据库边界与方言设计

### 4.1 最小方言抽象

在 `internal/infrastructure/db/dialect.go` 定义：

```go
type Dialect string

const (
    DialectPostgres Dialect = "postgres"
    DialectSQLite   Dialect = "sqlite"
)

func DialectOf(database *gorm.DB) (Dialect, error)
```

不建立覆盖所有 SQL 能力的“大而全” capability framework。当前真实差异只有连接/迁移、JSON/数组/聚合 SQL、锁、向量和 FTS；Repository 在需要处持有 `dialect Dialect` 并选择固定 SQL。没有方言差异的 Repository 不新增字段或构造参数。

固定 SQL 必须放在相应职责文件中，例如：

```text
retrieval_search_repository.go          # 公共编排
retrieval_search_postgres.go            # halfvec/tsvector SQL
retrieval_search_sqlite.go              # vec/FTS5 SQL
```

禁止创建 `utils.go`、万能 SQL builder 或让 application/domain 感知 Dialect。

### 4.2 打开数据库

`db.Open` 改为接收 `config.DatabaseConfig` 并返回数据库与已解析方言：

```go
func Open(cfg config.DatabaseConfig) (*gorm.DB, Dialect, error)
```

PostgreSQL 继续使用 `postgres.Open(cfg.DSN)` 与当前连接池行为。

SQLite DSN 由受控 builder 在用户 DSN 上合并固定参数，不能字符串盲拼：

```text
_pragma=foreign_keys(1)
_pragma=journal_mode(WAL)
_pragma=busy_timeout(5000)
_pragma=synchronous(NORMAL)
_txlock=immediate
_time_format=sqlite
_timezone=UTC
```

连接建立后执行并断言 `PRAGMA foreign_keys=1`、`journal_mode=wal` 和 `busy_timeout>=5000`。SQLite 连接池固定 `SetMaxOpenConns(1)`、`SetMaxIdleConns(1)`，使所有写事务串行并避免连接级 PRAGMA 漂移。该取舍符合单机体验定位；并行读性能不是本版本目标。

错误信息按实际 driver 表述为“连接 SQLite/PostgreSQL 失败”，不再硬编码 PostgreSQL。

### 4.3 迁移分流

`migrate.Run` 改为：

```go
func Run(ctx context.Context, cfg config.DatabaseConfig) error
```

- PostgreSQL：保留 `migrations/` 和 `database/postgres`。
- SQLite：使用独立嵌入目录 `migrations_sqlite/` 和纯 Go `database/sqlite`。
- 两套迁移拥有独立版本历史，不要求版本号一一对应。
- 迁移必须先完成，随后才注册 worker、scheduler 和 HTTP listener。
- SQLite migration 与 GORM 业务连接必须使用相同规范化 DSN，确保 foreign keys、vec 和时间行为一致。

SQLite 当前是全新安装能力，因此迁移从 PostgreSQL 000023 的最终语义出发，不回放 PG 的历史重建与数据回填。建议按职责拆为：

```text
000001_core_schema
000002_knowledge_and_jobs
000003_retrieval_and_search_runs
000004_fts_projection
```

每个 up 都有 down；迁移版本严格递增。拆分目的只是可审查和失败定位，不制造与 PG 历史版本的虚假对应。

## 5. SQLite 最终 Schema 合同

### 5.1 类型映射

| PostgreSQL | SQLite |
|---|---|
| `uuid` | `TEXT`，UUID 仍由 Go 生成 |
| `timestamptz` | SQLite datetime UTC `TEXT`，driver 统一扫描为 `time.Time` |
| `jsonb` | canonical JSON `TEXT` + 必要的 `json_valid/json_type` CHECK |
| `bytea` | `BLOB` |
| `text[]` | canonical JSON array `TEXT` |
| `halfvec` | 独立 `retrieval_embeddings.embedding BLOB` |
| `tsvector` | 独立 FTS5 表 |
| `inet` | `TEXT` |

所有 ID、时间和 JSON codec 必须有双方言 round-trip 测试。SQLite DSN 的 `_time_format=sqlite&_timezone=UTC` 负责普通时间列写入和读取；JSON 内时间仍显式规范化为 UTC RFC3339Nano。不得依赖 `gorm:"type:jsonb"` 在 SQLite 的偶然 affinity 行为作为 schema 合同；迁移 SQL是真实 schema 来源，Row tag 只负责扫描/写入。

### 5.2 约束与 lineage

SQLite 迁移必须保留可表达的业务约束：

- 每张租户表直接持有 `workspace_id` 和 `(workspace_id,id)` 唯一键。
- Workspace/KB/Document/Revision/ChunkSet/Chunk 的复合外键 lineage。
- Document kind、状态、Job target、Generation 状态、SearchRun 状态等 CHECK。
- active/staging/published、external_id、名称等部分唯一索引。
- `PRAGMA foreign_keys=ON` 是启动硬断言，不是建议项。

PostgreSQL 的 deferred constraint trigger 或 `DEFERRABLE` 不可直接等价时，先判断约束是在单事务内可按安全顺序满足，还是必须补 application/store 显式校验。任何下沉到 Go 的约束都必须有同 Workspace 成功、跨 Workspace/lineage 拒绝和并发场景测试，并在 `docs/DATABASE_GUIDELINES.md` 记录差异。

### 5.3 数组与 JSON

`workspace_api_keys.scopes` 等数组使用稳定 JSON codec，实现 `sql.Scanner`/`driver.Valuer`，两种 driver 返回相同领域值。查询侧优先将现有 `ANY(?::uuid[])` 改为 GORM `IN ?`，该写法双方言均支持。

JSON 局部更新按方言分发：

- PG：`jsonb_set`、`->>`、`-`。
- SQLite：`json_set`、`json_extract`、`json_remove`。

时间字符串统一为 UTC RFC3339Nano 后再写入 JSON，保证 SQLite 文本比较与时间顺序一致。禁止直接比较来源不一、带任意时区偏移的原始字符串。

## 6. Repository 方言分发

实施前对 `internal/infrastructure/db` 全量扫描，建立受测清单。当前已确认至少包括：

- JSON：`knowledge_base_repository.go`、`source_sync_store.go`（含 `jsonb_set`、`LATERAL`、`set_config` 与 force latch/result 更新）、`document_retry_store.go`、`document_chunks_repository.go`。
- 数组：`document_publish_store.go`、`index_generation_build_store.go`、`workspace_api_key_rows.go`。
- 聚合/CTE：`knowledge_base_summary_repository.go`、`model_provider_repository.go`、`index_generation_store.go`、`index_generation_stats.go`、`workspace_readiness_repository.go`。
- PostgreSQL 时间/错误：`session_repository.go`、`invitation_repository.go`、`repository_errors.go`、`retrieval_errors.go`。
- 锁与租户上下文：`workspace_tx.go`、`workspace_repository.go`、`oidc_auth_tx_runner.go`、`retrieval_cleanup_repository.go`。
- 检索：`retrieval_repository.go`、`retrieval_search_repository.go`、`retrieval_rows.go`。

能安全统一的 SQL 直接统一，例如 `SUM(CASE WHEN ...)` 和 `IN ?`；只有语义确实不同的点保留方言分支。所有动态标识符只从固定 allowlist 选择，向量维度继续只允许 798、1024、2048、3584，不把用户输入拼进 SQL。

错误翻译顺序：

1. `errors.Is(err, gorm.ErrRecordNotFound/ErrDuplicatedKey/ErrForeignKeyViolated)`。
2. PostgreSQL 方言才解析 `pgconn.PgError` SQLSTATE。
3. SQLite 只解析可稳定识别的 driver error code；字符串匹配只能作为最后兼容兜底并受测试约束。
4. 对外仍映射领域错误，HTTP 不泄漏底层 SQL 或路径。

## 7. 向量检索

### 7.1 修正原调研方案

`docs/SQLITE_SUPPORT.md` 中“先在全库 vec0 Top-K，再回 `retrieval_entries` 做 Workspace/KB/Generation 过滤并放大 k”不是精确检索：其它租户或 Generation 的近邻可占满候选，过滤后结果不足，放大倍数也不能证明完整性。因此本设计不采用全局 KNN 后过滤。

### 7.2 存储

SQLite 使用普通表保存向量 BLOB：

```sql
CREATE TABLE retrieval_embeddings (
    entry_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    knowledge_base_id TEXT NOT NULL,
    index_generation_id TEXT NOT NULL,
    dimension INTEGER NOT NULL CHECK (dimension IN (798,1024,2048,3584)),
    embedding BLOB NOT NULL,
    FOREIGN KEY (workspace_id, entry_id)
      REFERENCES retrieval_entries(workspace_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_retrieval_embeddings_scope
ON retrieval_embeddings(workspace_id, knowledge_base_id, index_generation_id, dimension);
```

`retrieval_entries` 保存业务投影和 state；embedding 表只保存向量及直接过滤键。写入 staging entry、向量和 FTS 必须在同一事务内完成，失败整体回滚。删除 entry 依靠外键 cascade 清理向量，FTS 由显式 store 同事务删除。

### 7.3 精确过滤后排序

SQLite 查询先通过业务条件限定候选，再计算余弦距离：

```sql
SELECT re.id AS entry_id,
       vec_distance_cosine(ev.embedding, vec_f32(?)) AS distance
FROM retrieval_entries AS re
JOIN retrieval_embeddings AS ev ON ev.entry_id = re.id
WHERE re.workspace_id = ?
  AND re.knowledge_base_id = ?
  AND re.index_generation_id = ?
  AND re.state = 'published'
  AND ev.dimension = ?
ORDER BY distance ASC, re.id ASC
LIMIT ?;
```

返回 score 为 `1-distance`，与 PG cosine 语义对齐。该方案在限定 scope 内全扫描，召回为精确解；适合文档定义的“小于数万条 embedding”单机边界。测试必须包含两个 Workspace 中更近的干扰向量，证明它不会占用当前 Workspace 的 top-k。

PG 路径保留四条固定 `halfvec(N) <=> halfvec(N)` SQL 和 HNSW 索引表达式，不做性能降级。

## 8. 全文检索

### 8.1 Tokenizer

Tokenizer 只被 SQLite retrieval infrastructure 消费，因此接口定义在使用方 `internal/infrastructure/db`，不把 SQLite 实现细节抬升到 application：

```go
type SearchTokenizer interface {
    Tokens(text string) []string
}
```

SQLite 由 `cmd/langhuan` 注入 gse adapter，构造时调用 `gse.NewEmbed()` 一次加载内嵌词典；PG 不构造该依赖，继续使用 generation 快照中的 `fts_config` 与 zhparser/simple。segmenter 完成构造后只读使用，并用 `go test -race` 验证并发切词路径。

索引文本把规范化、去空 token 以单空格连接。MATCH 查询不能直接拼接原始 query：每个 token 必须双引号包裹、内部 `"` 转义为 `""`，再以 ` AND ` 连接，模拟 PostgreSQL `plainto_tsquery` 的 plain-text AND 语义并阻止 FTS5 操作符注入。无 token 时直接返回空候选。写入和查询使用同一 tokenizer/version；切换分词规则视为索引配置变化，需要重建 Generation。

### 8.2 FTS5 投影

使用独立、可显式维护的 FTS5 表，不采用容易产生 rowid/列名歧义的 external-content 草案：

```sql
CREATE VIRTUAL TABLE retrieval_fts USING fts5(
    entry_id UNINDEXED,
    search_content_tokenized,
    tokenize = 'unicode61 remove_diacritics 2'
);
```

FTS 虚拟表不是租户权威边界。查询必须 JOIN `retrieval_entries` 并在普通表上再次校验 Workspace、KB、Generation 和 published state：

```sql
SELECT re.id AS entry_id, -bm25(retrieval_fts) AS score
FROM retrieval_fts
JOIN retrieval_entries AS re ON re.id = retrieval_fts.entry_id
WHERE retrieval_fts MATCH ?
  AND re.workspace_id = ?
  AND re.knowledge_base_id = ?
  AND re.index_generation_id = ?
  AND re.state = 'published'
ORDER BY bm25(retrieval_fts) ASC, re.id ASC
LIMIT ?;
```

写入、退役、删除与重建由 SQLite retrieval store 在同一事务显式双写；不用 trigger 隐藏行为。集成测试校验 FAQ 仍然只索引问题、返回回答，回答独有词不能命中。

## 9. Workspace 与并发语义

`WorkspaceTxRunner.WithinWorkspace` 两种方言都保留事务边界：

- PG：事务开始后继续执行 `set_config('app.workspace_id', ..., true)`。
- SQLite：不执行 GUC，直接调用 tx-bound store；所有查询仍显式限定 `workspace_id`。

实施时必须审计所有租户表的读写 SQL，至少覆盖 `documents`、`document_revisions`、`chunks`、`retrieval_entries`、`search_runs`、Job 与 source sync。测试通过两 Workspace 同形 ID/相似数据构造负向矩阵，而不是只靠 grep 证明。

PG advisory lock 保留。SQLite 的 workspace 数量上限与 OIDC bootstrap 原子性由 `_txlock=immediate`、单连接和事务内 check-then-write 保证，跳过 advisory SQL。GORM `clause.Locking` 在 SQLite 无行锁语义，但写事务已串行；代码注释必须说明此正确性依据。

`FOR UPDATE SKIP LOCKED` 清理 SQL 在 SQLite 改为稳定排序的普通 `SELECT ... LIMIT`，仍在 immediate transaction 内完成选择和删除。PG 路径保持 SKIP LOCKED 并发能力。

## 10. Redis 本地化与 worker 复用

### 10.1 配置

`RedisConfig` 增加 `Enabled bool`。YAML 加载兼容规则：在 `yaml.Unmarshal` 前构造 `Enabled: true` 的完整 YAML 默认配置，旧配置未写 `enabled` 时因而保持 `true`；standalone profile 在另一条显式构造路径中设置为 `false`。不使用 `*bool` 把三态复杂度扩散到装配层。`enabled=true` 时继续严格要求 addr 并装配 Redis/asynq；`enabled=false` 时不创建或 ping Redis client。

### 10.2 内存队列

内存队列同时承担：

- `queue.JobQueue.Enqueue`
- worker handler 注册与调度
- 完整 Queue Inspector 管理能力
- 生命周期启动、取消与排空

不重写现有 handler payload。现有 handler 已统一接收 `*asynq.Task`，因此抽出最小注册接口，例如：

```go
type TaskRegistrar interface {
    HandleFunc(pattern string, handler func(context.Context, *asynq.Task) error)
}
```

接口参数必须保留未命名函数类型；若写成 `asynq.HandlerFunc`，其方法签名与 `*asynq.ServeMux.HandleFunc` 不完全一致，mux 不能实现接口。内存 runtime 仍构造 `asynq.NewTask(type,payload)` 调用 handler，从而复用解码、`SkipRetry` 和业务幂等逻辑。Asynq 的 retry metadata context key 是内部实现，不能由 memory adapter 伪造；因此新增 worker-owned `TaskExecutionMetadata` context helper，现有 handler 通过 helper 读取 retry count/max retry，helper 在真实 asynq context 下回退到 `asynq.GetRetryCount/GetMaxRetry`，在 memory context 下读取 adapter 注入的值。任务协议和 payload 不变。

`QueueConfig` 新增 `capacity`，默认 1024，必须大于等于 `concurrency`。容量限制 pending/scheduled/retry/active 的非终态任务总数；completed/dead 只保留轻量 metadata，不占 active capacity，但受 retention 清理。行为合同：

- `TaskID` 在 pending/scheduled/retry/active 以及 terminal metadata retention 期间保持占用，与 asynq 的显式 TaskID 语义一致；terminal metadata 被清理或管理员删除后释放。
- 支持 Delay、MaxRetry、Timeout、Retention。
- 延迟项使用单 scheduler/heap，不为每个任务启动一个 goroutine。
- worker 数由 `queue.concurrency` 控制。
- `errors.Is(err, asynq.SkipRetry)` 立即终止；其它错误按现有指数退避重试。
- context 取消后停止接收，取消 delay scheduler，等待运行中任务到 shutdown deadline。
- completed/failed/pending 计数通过 application 的 Queue Inspector port 暴露；不把 memory 类型泄漏到 service。

内存 inspector 必须完整实现现有 `service.QueueInspectorPort`：`Snapshots` 返回 pending/active/scheduled/retry/dead/processed/failed 统计，`ListDead` 分页返回脱敏 dead metadata，`RetryDead` 把指定 dead task 重新放入可执行队列，`DeleteDead` 删除 metadata 并释放其 `TaskID`。不能只为 readiness 实现计数而让现有队列管理 API 在 standalone 下退化。

### 10.3 内存限流和 OIDC state

新增 mutex 保护的固定窗口 rate limiter，key 仍为规范化 email 的 SHA-256，不保存明文 email。每次访问惰性删除过期记录，并用低频清理避免长期增长。

OIDC 默认关闭，但 SQLite/无 Redis 的显式配置允许开启 OIDC；此时装配内存 state store。`Issue` 使用密码学随机 state/nonce，`Consume` 在同一 mutex 临界区内完成一次性读取与删除，nonce 不匹配遵循现有 Redis 实现合同。清理 goroutine 必须受 app context 控制并可 shutdown。

## 11. Runtime 装配、就绪与关闭

`buildApp` 按数据库和 Redis 配置装配，不以 `RunHTTP/RunWorker` 推断 Redis 必然存在：

```text
config selection
  -> standalone data/key preparation（仅 standalone）
  -> migrate selected database
  -> open selected database
  -> build repositories/services
  -> build asynq runtime OR memory runtime
  -> register same worker handlers
  -> start schedulers/worker/http
```

迁移应在业务连接和服务启动前完成，避免 SQLite migration connection 与业务连接竞争。若现有测试依赖“先 open 后 migrate”，同步调整测试 helper，不在生产路径保留不必要顺序。内存队列不承诺跨进程持久化；本次只保留现有 source cleanup/force latch 等已有补偿扫描和用户可见的 retry/reindex 恢复入口，不额外发明无法从现有 Job payload 安全重建的通用 startup requeue。进程崩溃时普通内存任务可能需要用户重试，这一边界必须写入 README。

Readiness 使用端口而非 concrete `*asynq.QueueInspector`：

- database 总是 ping。
- Redis 只在 enabled 时 ping。
- queue inspector 在两种后端都可选检查 pending threshold。
- readiness 文案报告 `sqlite` 或 `postgres`，不硬编码 PG。

shutdown 顺序：先停止 HTTP 接收，再停止 scheduler/queue 接收，等待 worker，关闭 asynq/inspector/Redis，最后关闭数据库与 OTel。任何内存 goroutine 必须由 app context 或显式 Close 控制。

启动成功输出：

```text
Langhuan is ready.
Web Console: http://127.0.0.1:8080
Data directory: /absolute/home/.langhuan-data
Database: sqlite
```

不输出 credential key、DSN query 或敏感配置。

## 12. 测试策略

### 12.1 TDD 顺序

实现按可独立闭环的纵向切片推进，每个切片先写失败测试：

1. 配置选择、standalone profile、主目录/密钥权限。
2. SQLite driver、PRAGMA、迁移和最终 schema。
3. 通用 CRUD 与方言 SQL。
4. Workspace/lineage/并发负向测试。
5. 向量精确过滤与 FTS5/gse。
6. 内存 rate limiter、OIDC state、queue 的重试/延迟/去重/关闭。
7. runtime 零配置 smoke 与完整 SQLite E2E。
8. 文档和 release build。

### 12.2 SQLite test support

新增 `internal/testsupport/sqlite.go`，每个测试使用 `t.TempDir()` 内的唯一数据库和密钥。helper 只接受测试生成路径，不读取 `config.yaml`、`~/.langhuan-data` 或环境中的生产 SQLite 文件。

SQLite 不需要 Docker；PostgreSQL 集成测试继续严格使用运行期临时 `langhuan-test-postgres:pg17` 容器。任何修改现有 PG 集成测试 helper 以复用本机数据库的做法都不允许。

### 12.3 必测矩阵

- 配置：缺省文件走 standalone；默认文件存在则读取；显式缺失报错；无静默回退。
- 文件：目录/密钥权限、并发首次生成、损坏密钥 fail-fast、重启密钥稳定。
- 迁移：空 SQLite up、重复 up、down/up、foreign key 开启、所有表/列/索引存在。
- Codec：UUID、UTC time、JSON object/array、BLOB、nullable 字段 round-trip。
- Repository：双方言共享断言；每个专属 SQL 点至少一个 SQLite 真实测试。
- 隔离：两个 Workspace 的 CRUD、向量和 FTS 干扰数据不串租户。
- 检索：四种维度、cosine 顺序、FAQ 语义、中英文、RRF 稳定同分、0 结果。
- 并发：workspace limit、首管理员 bootstrap、重复 enqueue、Generation 激活和 cleanup。
- Queue：Delay、TaskID、Retry、SkipRetry、Timeout、取消、bounded capacity、Inspector。
- Runtime：无 config、无 PG/Redis 时真实二进制启动；register/login/session、创建 Workspace、配置 fake Embedding、导入、worker 处理、检索和优雅关闭。
- 回归：现有 PG 单元/集成/E2E 全部通过，HNSW EXPLAIN 断言保留。

### 12.4 验证命令

```bash
gofmt -w cmd internal
go test ./... -count=1
make test-sqlite
make test-integration
go vet ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 make linux
git diff --check
```

`make test-sqlite` 不要求 Docker；`make test-integration` 继续自动构建并启动临时 pgvector + zhparser 容器。

## 13. 实施拆分

按依赖顺序交付，每步同时跑 PG 回归：

1. **启动合同**：配置选择、standalone profile、data root、credential key、测试。
2. **数据库地基**：依赖、Dialect、Open、migrate 分流、SQLite test support。
3. **最终 schema**：SQLite migrations、codec 与 schema 测试。
4. **通用 Repository**：JSON、数组、聚合、时间、错误翻译。
5. **事务与隔离**：WorkspaceTx、advisory/row lock/SKIP LOCKED 分流与负向矩阵。
6. **检索**：向量 BLOB 精确扫描、gse、FTS5、双方言 RetrievalRepository。
7. **无 Redis runtime**：memory queue/rate limiter/OIDC state、Inspector、readiness/shutdown。
8. **零配置 E2E 与文档**：真实发布二进制 smoke、README、架构/数据库规范/运维文档、ROADMAP。

结构整理与行为修改分开提交。纯拆文件提交至少运行相关 package tests、`go test ./...`、`go vet ./...` 和 `git diff --check`。

## 14. 文档与兼容性

需要同步更新：

- `README.md` / `README.en.md`：第一入口改为直接运行；高级部署再说明 YAML + PG/Redis。
- `config.example.yaml`：保留完整 PostgreSQL 生产示例，并增加 SQLite/Redis disabled 示例。
- `AGENTS.md`：技术基线改为 PG 生产 + SQLite standalone；数据库测试隔离规则补充 `t.TempDir()` SQLite。
- `docs/ARCHITECTURE.md`：数据库多驱动、内存 runtime 与 standalone 数据流。
- `docs/DATABASE_GUIDELINES.md`：方言 SQL、SQLite schema、锁/事务、向量/FTS 规则。
- `docs/operations/backup-restore.md`：standalone 必须一起备份 DB、raw documents 和 `credential.key`；密钥与数据目录整包泄漏不提供静态加密防护。
- `ROADMAP.md`：把零配置 SQLite 纳入 v1.0.0 安装验收基线。

兼容承诺：

- 现有 `config.yaml` 未显式写 `redis.enabled` 时按 true 处理。
- 现有 PostgreSQL driver、DSN、迁移目录、HNSW、zhparser 和 asynq 行为不变。
- `-config` 的严格失败比自动回退优先。
- SQLite 与 PG 数据文件不互相升级；用户切换 driver 时看到的是对应数据库中的独立数据。

## 15. 风险与缓解

| 风险 | 缓解 |
|---|---|
| 两个纯 Go SQLite 包重复注册 `sqlite` driver | 不引入 glebarez/go-sqlite；项目内 GORM Dialector 与 migrate 统一使用 modernc v1.56.0 |
| SQLite migration 漏掉 PG 最终约束 | schema 清单 + 关键约束负向集成测试，不只比较表名 |
| 去掉 GUC 后查询漏 workspace | 两 Workspace 干扰数据测试覆盖 CRUD、检索、cleanup 与 sync |
| 向量先 KNN 后过滤导致召回不足 | 本设计改为 scope 过滤后 `vec_distance_cosine` 精确排序 |
| FTS 双写漂移 | 同事务显式写/删，重建一致性测试与孤儿检查 |
| 用户 query 被解释为 FTS5 语法 | tokenizer 输出逐 token 安全引用并以 AND 组合；特殊符号与空 token 测试 |
| 内存队列重启丢任务 | 明确 standalone 边界；只复用现有可证明安全的补偿与用户重试入口，不声称持久队列或通用自动重建 |
| 自动密钥丢失 | 0600 sidecar、损坏 fail-fast、README/备份文档强提示，绝不自动覆盖 |
| 默认 Secure Cookie 导致 localhost 登录失败 | standalone profile 显式 false；生产 YAML 保持 true |
| 现有 YAML 被静默切到 SQLite | 当前目录存在默认配置即读取；显式配置失败不回退 |

## 16. 第一性原理复核

- **目标事实**：体验用户需要的是可直接使用的产品，不是一个需要手写 SQLite YAML 的 driver demo。因此配置缺失、凭证密钥、Cookie、队列和本地存储都是同一目标的必要组成。
- **正确性约束**：租户过滤必须发生在向量排序之前；“全局 top-k 后过滤”无法由固定放大倍数证明正确，故舍弃。
- **安全约束**：数据库内存在可恢复密文，自动密钥必须稳定持久化并拒绝静默轮换。密钥与数据库分文件至少保留“仅数据库泄漏”时的隔离。
- **兼容约束**：已有 `config.yaml` 是现有生产入口，默认文件存在时继续读取比改变 YAML 默认值更可靠。
- **复杂度约束**：PG/SQLite 的共同业务合同已由 application/domain 隔离；只在真实 SQL 差异处引入 Dialect，避免复制 28 个 Repository 或创建万能 capability abstraction。
- **验证闭环**：SQLite 使用临时文件真实迁移与 E2E，PG 使用临时 Docker；两条路径都以可复现实验而非 mock GORM 或静态 grep 作为完成证据。
