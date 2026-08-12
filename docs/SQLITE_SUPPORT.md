# 琅嬛 SQLite 支持设计规格（CGO_ENABLED=0，全量端到端）

本规格定义琅嬛在保持 `CGO_ENABLED=0` 的前提下新增 SQLite 数据库后端的完整实施计划。目标是提供一个"单二进制 + 单数据库文件 + 零外部依赖"的开发 / 演示单机部署形态，与现有 PostgreSQL 生产路径并存，PG 路径全程零回归是硬约束。

SQLite 模式的定位：

- **开发 / 演示 / 单机轻量部署**，替代 PG 用于本地开发、Demo、小团队试用。
- 接受单进程、写串行（单写锁）、数据量小（单库建议 < 数万条 embedding）。
- 多租户隔离由应用层显式携带 `workspace_id` 保证，不依赖 PG 的 RLS / advisory lock。
- Redis 在 SQLite 模式下可禁用，三个依赖（队列 / 登录限流 / OIDC state）全部本地化。

不在本规格范围：

- SQLite 作为多副本生产级部署形态。
- SQLite 下追求与 pgvector HNSW 同等的 ANN 性能（演示场景走 sqlite-vec 暴力扫描，召回率为精确解 100%）。
- 启用 SQLite 的行级安全（RLS）或事务级 GUC（`set_config`）。

## 1. 技术选型

选型已经过源码与上游仓库验证，各层之间自洽：

| 层 | 选型 | 依据 |
|---|---|---|
| GORM 驱动 | `github.com/glebarez/sqlite`（基于 modernc.org/sqlite） | gorm.io 官方点名的纯 Go 驱动；API 与 `gorm.io/driver/sqlite` drop-in 兼容；`TranslateError` 已映射 `SQLITE_CONSTRAINT_UNIQUE→gorm.ErrDuplicatedKey` 等 |
| 底层 SQL 驱动 | `modernc.org/sqlite` v1.56.0 | transpiled C 到 Go，纯 Go 编译；内嵌 SQLite 3.53.3；FTS5 / JSON1 / CTE / UPSERT / 部分索引全支持 |
| 向量扩展 | `modernc.org/sqlite/vec`（内置 sqlite-vec v0.1.9） | 官方子包，一行空导入通过 `sqlite3_auto_extension` 自动给每个新连接注册；`distance_metric=cosine`；演示数据量毫秒级暴力扫描 |
| 迁移器 | `github.com/golang-migrate/migrate/v4/database/sqlite`（注意是 `sqlite`，不是 `sqlite3`） | golang-migrate 官方纯 Go SQLite 包（PR #555 合入已 5 年，含于 v4.19.1），底层就是 modernc.org/sqlite；与 `database/sqlite3`（cgo，mattn）区分清楚 |
| 全文检索 | FTS5 + `github.com/go-ego/gse`（应用层分词）+ BM25 | modernc 无法注册 FTS5 tokenizer，必须应用层分词；gse 是纯 Go 中文分词事实标准（2.8k★，Apache-2.0，支持 `//go:embed` 词典） |
| 队列 | 内存 JobQueue（`ports/queue.JobQueue` 接口仅 `Enqueue` 一个方法） | 单机模式 HTTP + Worker 同进程，channel 天然契合 |
| 限流 / OIDC state | 内存实现（`sync.Map` + TTL / 固定窗口计数器） | 单进程语义更准；各约 1h 工作量 |
| 方言分发 | `db.Dialect` 接口 + 每个 SQL 点显式分发 | 改动局部、可测、不污染上层；符合 AGENTS.md 5.7 |

### 1.1 关键自洽点

- 驱动统一在 modernc 栈：glebarez/sqlite（GORM）→ modernc.org/sqlite（底层）→ modernc.org/sqlite/vec（向量扩展）。三者同源 transpiled，无集成摩擦。
- 迁移器走 modernc：`database/sqlite` 包 import 的就是 modernc.org/sqlite，与上面完全配套，不需要 ncruces 或第三方 hack。
- golang-migrate 的 `database/sqlite3`（mattn，cgo）**绝不 import**，否则破坏 `CGO_ENABLED=0`。

## 2. 现状与耦合面（调研结论）

### 2.1 架构干净的切入点

所有 PG 耦合收敛在两个目录：

- `internal/infrastructure/db/`：GORM 连接、Repository、Row 模型、方言 SQL。
- `internal/infrastructure/migrate/`：golang-migrate 与 23 套迁移 SQL。

`application` / `domain` / `interfaces` / `ports` 层几乎只通过 Repository 接口访问数据，零 PG 直接 import。`config.DatabaseConfig.Driver` 字段已经存在但从未被消费，`db.Open` 永远走 `postgres.Open`。

### 2.2 PG 强耦合清单

| 类别 | 范围 | 难度 |
|---|---|---|
| 配置 + 入口 | `DatabaseConfig.Driver` 已存在但被忽略；`db.Open` / `migrate.Run` 硬编码 PG driver | 易 |
| 迁移 SQL | 23 套（共 1673 行，其中 000005 占 706 行）全是 PG DDL：`CREATE EXTENSION` / `halfvec` / `HNSW` / `tsvector` / `GIN` / `plpgsql` 触发器 / `FILTER (WHERE)` / `LATERAL` / `DEFERRABLE` / `gen_random_uuid()` | 高 |
| 向量检索 | `retrieval_search_repository.go:234-260` 写死 4 条 `embedding::halfvec(N) <=> ?::halfvec(N)` 余弦 SQL；`retrieval_repository.go:72-78` 写入 `?::halfvec` + `to_tsvector`；`halfVectorLiteral` 手写序列化 | 极高 |
| 全文检索 | `retrieval_search_repository.go:262-267` 写死 `plainto_tsquery` + `ts_rank_cd` + `@@`；000011 注册 zhparser 扩展与词性映射 | 极高 |
| 多租户隔离 | `workspace_tx.go:36-39` `SELECT set_config('app.workspace_id', ?, true)` 事务级 GUC；`workspace_repository.go:85` 与 `oidc_auth_tx_runner.go:51` 两处 `pg_advisory_xact_lock(hashtextextended(...))` | 高 |
| 行级锁 | 30+ 处 `clause.Locking{Strength:"UPDATE"/"SHARE"}`；2 处 raw `FOR UPDATE SKIP LOCKED`（`retrieval_cleanup_repository.go:123,154`） | 中（SQLite 驱动静默忽略 Locking；SKIP LOCKED 需删除） |
| JSON 操作 | `jsonb_set` / `->>` / `::timestamptz` / `CAST AS jsonb` 散落 ~8 个文件 | 中 |
| 数组类型 | `pq.Array` / `pq.StringArray` / `id = ANY(?::uuid[])` / `type:text[]` 在 4 个文件 | 小 |
| 条件聚合 | `COUNT(*) FILTER (WHERE ...)` 约 5 处 | 小（改 `SUM(CASE WHEN)`） |
| 错误翻译 | `repository_errors.go:8` import `pgconn.PgError` 做 SQLSTATE 双保险 | 小（glebarez TranslateError 已覆盖 SQLite） |
| Redis | asynq 队列 + 登录限流器 + OIDC state store，全部已通过 ports 隔离 | 中（队列 worker handler 签名耦合 `*asynq.Task`） |

队列（asynq + Redis）和对象存储（local / s3）**与 PG 零耦合**，存储层不受影响。

## 3. 分阶段实施计划

### Phase 1：基础设施层（驱动 + 方言抽象 + 配置）

**目标**：让 `db.Open` 和 `migrate.Run` 能按 driver 分流；建立 Dialect 抽象。这是所有后续工作的地基。

#### 1.1.1 新增 Dialect 抽象

新建 `internal/infrastructure/db/dialect.go`：

```go
type Dialect string

const (
    DialectPostgres Dialect = "postgres"
    DialectSQLite   Dialect = "sqlite"
)

// DialectCapabilities 暴露各方言的能力差异，供 repository 做分支决策。
type DialectCapabilities interface {
    Dialect() Dialect
    SupportsAdvisoryLock() bool   // PG=true, SQLite=false
    SupportsSetConfig() bool      // PG=true, SQLite=false（set_config GUC）
    SupportsRowLocking() bool     // PG=true, SQLite=false（FOR UPDATE）
    VectorStorage() VectorStorage // pgvector / sqlite_vec
    FTSStorage() FTSStorage       // tsvector / fts5
}
```

Dialect 由 `*gorm.DB` 的 `Dialector.Name()` 推断（`postgres` / `sqlite`），`NewDialectCapabilities(name)` 工厂构造。Repository 通过构造函数注入 `Dialect` 值，或简单场景在方法内 `db.Dialector.Name()` 判断。

#### 1.1.2 改造 `db.Open`

`internal/infrastructure/db/db.go`（14 行 → ~40 行）按 driver 分流。签名从 `Open(dsn string)` 改为 `Open(cfg config.DatabaseConfig)`，返回 `(*gorm.DB, Dialect, error)`：

```go
func Open(cfg config.DatabaseConfig) (*gorm.DB, Dialect, error) {
    switch cfg.Driver {
    case "postgres", "":
        gormDB, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{TranslateError: true})
        return gormDB, DialectPostgres, err
    case "sqlite":
        dsn := sqliteDSN(cfg.DSN) // 注入 _pragma 与 _txlock 参数
        gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
        if err != nil {
            return nil, "", err
        }
        sqlDB, _ := gormDB.DB()
        sqlDB.SetMaxOpenConns(1) // 避免 SQLITE_BUSY，单写串行
        return gormDB, DialectSQLite, nil
    default:
        return nil, "", fmt.Errorf("不支持的数据库 driver: %s", cfg.Driver)
    }
}
```

SQLite DSN 统一注入 pragma：`?_journal_mode=WAL&_busy_timeout=5000&_foreign=1&_txlock=immediate`。

连接池 `SetMaxOpenConns(1)` 是为了避免多连接并发写撞 `SQLITE_BUSY`；单机场景足够。WAL 模式下多读单写，读不阻塞写、写不阻塞读，但写仍串行。

#### 1.1.3 改造 `migrate.Run`

`internal/infrastructure/migrate/migrate.go` 按 driver 分流：

- PG 走现有 `database/postgres` + `migrations/` 目录（零改动）。
- SQLite 走 `database/sqlite`（纯 Go）+ 新增 `migrations_sqlite/` 目录。
- 新增 embed：`//go:embed migrations_sqlite`。
- 入口签名改为 `Run(ctx, cfg config.DatabaseConfig) error`，内部按 driver 选 source 目录与 database driver。
- SQLite 下迁移连接与业务连接共用时必须串行——迁移在服务启动时跑完后再开业务连接，或全程 `MaxOpenConns(1)`。

#### 1.1.4 配置层

`internal/infrastructure/config/config.go`：

- `validate()` 新增 `validateDatabase()`：driver 限定 `postgres` / `sqlite`；SQLite 时放开 `Redis.Addr` 强制非空（Redis 可选，见 Phase 7）。
- `RedisConfig` 新增 `Enabled bool` 字段（默认 `true` 保持兼容）。
- `config.example.yaml` 新增 SQLite 示例段（注释形式）：

```yaml
# SQLite 单机模式（开发/演示，零外部依赖）
# database:
#   driver: sqlite
#   dsn: "file:langhuan.db?cache=shared"
# redis:
#   enabled: false  # SQLite 单机模式可禁用 Redis，使用内存队列
```

#### 1.1.5 错误翻译

`internal/infrastructure/db/repository_errors.go`：

- PG 的 `pgconn.PgError` 检查保留（对 PG 仍生效）。
- SQLite 下依赖 glebarez 的 `TranslateError`（已映射 `SQLITE_CONSTRAINT_UNIQUE→gorm.ErrDuplicatedKey`），现有 `errors.Is(err, gorm.ErrDuplicatedKey)` 分支自动生效。
- 字符串兜底已覆盖 SQLite 的 `UNIQUE constraint failed`，无需额外改动。

**Phase 1 验收**：`go build ./...` 通过；`config.Database.Driver="postgres"` 时行为与现状完全一致（零回归）。

### Phase 2：SQLite 迁移脚本套件（工作量最大）

**目标**：为 SQLite 写一套等价迁移（`migrations_sqlite/`），覆盖现有 23 个 PG 迁移的表结构，但用 SQLite 友好的 DDL。

#### 1.2.1 规模与策略

现有 23 个迁移共 1673 行 SQL，其中 000005（核心 schema 重建）占 706 行。

策略：**不为每个 PG 迁移写 1:1 对应**，而是按"逻辑版本"合并——SQLite 迁移目录从白纸出发，最终 schema 与 PG 对齐（表 / 列 / 约束 / 索引语义等价，实现方式不同）。起步可只写 1 个 `000001_init_sqlite.up.sql`（合并 000001~000023 的最终状态），后续按需拆分版本。golang-migrate 会记录 SQLite 库的独立版本号，与 PG 库互不影响。

#### 1.2.2 方言映射规则（SQLite 迁移编写指南）

| PG 构造 | SQLite 对应 |
|---|---|
| `uuid DEFAULT gen_random_uuid()` | `TEXT PRIMARY KEY`（ID 由应用层 Go 生成，项目已这么做） |
| `timestamptz DEFAULT now()` | `TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))` |
| `jsonb` + `CHECK (jsonb_typeof(x)='object')` | `TEXT` + `CHECK (json_type(x)='object')` |
| `jsonb_set(k, '{p}', to_jsonb(?::timestamptz))` | `json_set(k, '$.p', ?)`（类型由应用层传字符串） |
| `(config->>'key')::timestamptz <= ?` | `json_extract(config,'$.key') <= ?` |
| `halfvec` + HNSW 部分索引 | 见 Phase 4：`vec0` 虚拟表 |
| `tsvector` + GIN 索引 | 见 Phase 5：FTS5 虚拟表（external content） |
| `text[]` + `cardinality()` + `<@` | `TEXT`（JSON 数组）+ 应用层校验，或改写 `CHECK` |
| `bytea`（密文） | `BLOB` |
| `inet` | `TEXT` |
| `CREATE EXTENSION` | 删除（sqlite-vec 由 Go 空导入注册） |
| `plpgsql` 触发器 + CONSTRAINT TRIGGER | 下沉为应用层校验（repository / service 显式检查），或改写为 SQLite `AFTER ... FOR EACH ROW` 触发器 |
| `DEFERRABLE INITIALLY DEFERRED` 外键 | 删除（SQLite 外键默认立即检查）；靠事务 + 应用层顺序保证 |
| `COUNT(*) FILTER (WHERE cond)` | `SUM(CASE WHEN cond THEN 1 ELSE 0 END)` |
| `pg_advisory_xact_lock` / `set_config` | 删除（见 Phase 6） |
| `gen_random_uuid()` 依赖的 pgcrypto | 删除扩展注册 |

#### 1.2.3 复杂迁移的处理

- **000005（706 行核心 schema）**：工作量大头。需为 `knowledge_bases` / `documents` / `chunks` / `chunk_revisions` / `retrieval_entries` / `file_tree_nodes` 等全套表写 SQLite DDL。建议拆成 2-3 个 SQLite 迁移版本（基础表 / 检索表 / 完整性约束）。
- **000009（回填脚本，130 行）**：PG 专属多 CTE + LATERAL + FILTER。SQLite 是全新库不需要回填历史数据，**直接跳过**。
- **000011（zhparser）**：删除（FTS 走 Phase 5 的 gse + FTS5 方案）。
- **000022（DO 块前置检测）**：SQLite 用 `CREATE UNIQUE INDEX IF NOT EXISTS` 幂等，不需要 DO 块。

#### 1.2.4 Row 模型 GORM tag 适配

现有 `gorm:"type:jsonb"` 在 SQLite 下靠 type affinity 当 TEXT，不报错——**保留不改**（双驱动兼容）。`gorm:"type:halfvec"` / `gorm:"type:tsvector"`（仅 `retrieval_rows.go` 各一处）：这两个字段在 SQLite 下不通过 GORM 读写（走 vec0 / FTS5 虚拟表），需让 Row 结构在 SQLite 下不映射这两列，或拆分 Row 模型。

**Phase 2 验收**：SQLite 库 `migrate.Run` 成功建出全套表；PG 库行为零回归。

### Phase 3：Repository 方言分发（21 个文件的 PG 专属 SQL）

**目标**：把散落在 repository 里的 PG 专属 SQL 改成按 Dialect 分发。改动局部化，每处加 dialect 分支。

#### 1.3.1 改动清单（按文件）

**JSON 操作类（`jsonb_set` / `->>` / `::timestamptz`）**：

- `knowledge_base_repository.go:174,200,203,227,230,250` — `source_config` 的 `next_sync_at` / `sync_cursor` 增删改。
- `source_sync_store.go:98,122,367,406,991,1216` — 同模式 `sync_cursor` / force latch。
- `document_retry_store.go:153` — payload `jsonb_set`。
- `document_chunks_repository.go:119` — `CAST(? AS jsonb)` 改为直接比较（SQLite 下 JSON 相等直接比较 TEXT）。

**数组类（`pq.Array` / `ANY(?::uuid[])`）**：

- `document_publish_store.go:164,281` — `id = ANY(?::uuid[])` 改 `id IN ?`（GORM 自动展开 slice）。
- `index_generation_build_store.go:186,204` — 同上。
- `workspace_api_key_rows.go:21` — `pq.StringArray` / `type:text[]` 改 JSON 存（自定义 Scan / Value，或复用 JSONMap 模式）。
- 移除这 4 个文件对 `github.com/lib/pq` 的 import。

**条件聚合（`COUNT FILTER`）**：

- `knowledge_base_summary_repository.go:43-48`、`model_provider_repository.go:209-211`、`index_generation_store.go:115-116`、`index_generation_stats.go:27-95`（含 LATERAL）、`workspace_readiness_repository.go:53-55`。
- 改 `SUM(CASE WHEN ... THEN 1 ELSE 0 END)`；`index_generation_stats.go` 的 `LATERAL` 改写为子查询 JOIN。

**时间函数**：

- `session_repository.go:39` — `now()` 按 dialect 分发：PG `now()` / SQLite `datetime('now')`。

#### 1.3.2 实现模式

每个含 PG 专属 SQL 的 repository 方法，用 `dialect` 值选 SQL 常量：

```go
func (r *Repository) SomeQuery(ctx context.Context, ...) error {
    var sql string
    if r.dialect == db.DialectSQLite {
        sql = sqliteSomeQuerySQL // json_set / json_extract / SUM(CASE)
    } else {
        sql = pgSomeQuerySQL     // 保留现有 jsonb_set / ->>
    }
    return r.db.WithContext(ctx).Raw(sql, args...).Scan(&dest).Error
}
```

SQL 常量集中在各 repository 文件顶部（PG 版保留，SQLite 版新增）。Dialect 由 repository 构造函数注入。

**Phase 3 验收**：所有 repository 单元测试在 PG 下零回归；SQLite 下 CRUD / JSON 操作通过。

### Phase 4：向量检索重写（sqlite-vec）

**目标**：SQLite 下用 `vec0` 虚拟表替代 pgvector halfvec + HNSW，检索语义对齐（cosine + top-k）。

#### 1.4.1 存储改造

- PG：`retrieval_entries.embedding halfvec`（单列，按 dimension 分区建 4 个 HNSW 部分索引）。
- SQLite：**分离存储**——`retrieval_entries` 保留业务列（不含 embedding），embedding 存到 vec0 虚拟表：

```sql
-- 每个维度一张 vec0 表（对应 PG 的 4 个部分索引）
CREATE VIRTUAL TABLE retrieval_embeddings_798 USING vec0(
    entry_id TEXT PRIMARY KEY,
    embedding float[798] distance_metric=cosine
);
-- 同样 1024 / 2048 / 3584
```

按维度分表比统一存 max 维度补零更省空间且语义清晰。支持的维度由 `internal/domain/value/model_type.go:18` `SupportedEmbeddingDimensions` 定义（798 / 1024 / 2048 / 3584）。

#### 1.4.2 写入改造

`retrieval_repository.go:72-78` `StageBatch`：

- PG：`UPDATE retrieval_entries SET embedding = ?::halfvec`。
- SQLite：分两步——先 `INSERT INTO retrieval_embeddings_{dim}(entry_id, embedding) VALUES(?, vec_f32(?))`（JSON 字符串），再由 vec0 索引。
- `halfVectorLiteral`（序列化 `[a,b,c]`）改造：SQLite 用 JSON 格式 `vec_f32('[a,b,c]')`。

#### 1.4.3 查询改造

`retrieval_search_repository.go:234-260` 现有 PG 写法：

```sql
SELECT id AS entry_id, 1 - distance AS score FROM (
  SELECT id, ((embedding::halfvec(1024)) <=> (?::halfvec(1024))) AS distance
  FROM retrieval_entries
  WHERE workspace_id = ? AND knowledge_base_id = ? AND index_generation_id = ?
    AND state = 'published' AND dimension = ?
  ORDER BY (embedding::halfvec(1024)) <=> (?::halfvec(1024)) LIMIT ?
) AS candidates ORDER BY distance ASC, id ASC
```

SQLite：vec0 虚拟表不支持 JOIN 业务表直接过滤，改"先 KNN 查 entry_id，再 JOIN 回 retrieval_entries 过滤"：

```sql
-- 第一步：向量召回（在 vec0 表，不带业务过滤）
SELECT entry_id, distance FROM retrieval_embeddings_1024
WHERE embedding MATCH ? AND k = ?
ORDER BY distance;

-- 第二步：业务过滤（拿 entry_id 列表回查 retrieval_entries）
SELECT id, 1 - <distance> AS score FROM retrieval_entries
WHERE id IN (...) AND workspace_id = ? AND knowledge_base_id = ?
  AND index_generation_id = ? AND state = 'published';
```

vec0 的 `k` 是"召回数"，业务过滤后可能不足 k 个，需适当放大召回数（如 `k * 2~3`）。

#### 1.4.4 依赖

```go
import _ "modernc.org/sqlite/vec" // 空导入，init 自动注册扩展
```

**Phase 4 验收**：向量检索在 SQLite 下返回 cosine top-k，结果与 PG 方案在相同数据上基本一致（暴力扫描召回率 100%，比 HNSW 更精确）。

### Phase 5：全文检索重写（gse + FTS5）

**目标**：SQLite 下用 FTS5 + 应用层 gse 分词替代 PG tsvector + zhparser。

#### 1.5.1 存储改造

- PG：`retrieval_entries.fts_document tsvector`（物化列）+ GIN 索引。
- SQLite：FTS5 external content 虚拟表：

```sql
CREATE VIRTUAL TABLE retrieval_fts USING fts5(
    search_content_tokenized,  -- 应用层 gse 分词后写入
    content='retrieval_entries',
    content_rowid='rowid',     -- 或用 entry_id TEXT
    tokenize='unicode61 remove_diacritics 2'
);
```

#### 1.5.2 分词器（新增 adapter）

新建 `internal/adapters/tokenizer/gse/segmenter.go`：

```go
import "github.com/go-ego/gse"

type Segmenter struct{ seg *gse.Segmenter }

func New() (*Segmenter, error) { /* seg.LoadDictEmbed() */ }

// Tokenize 把中文切成空格分隔的 token，交给 FTS5 unicode61。
func (s *Segmenter) Tokenize(text string) string {
    return strings.Join(s.seg.Cut(text), " ")
}
```

词典 `//go:embed` 进二进制（gse 提供 `LoadDictEmbed`），增加几 MB 产物体积可接受。分词器作为端口 `ports/tokenizer.Tokenizer`，PG 路径不用（PG 在 SQL 层 `to_tsvector`），SQLite 路径在应用层调用。

#### 1.5.3 写入改造

`retrieval_repository.go:74`：

- PG：`SET fts_document = to_tsvector(?::regconfig, search_content)`。
- SQLite：应用层先用 gse 分词，再 `INSERT INTO retrieval_fts(rowid, search_content_tokenized) VALUES(?, ?)`。
- 同步策略：`StageBatch` 创建 `retrieval_entries` 后，同事务内同步写入 `retrieval_fts`（不用 trigger，应用层显式管理，避免双写）。

#### 1.5.4 查询改造

`retrieval_search_repository.go:262-267` 现有 PG 写法：

```sql
WITH search_query AS (SELECT plainto_tsquery(?::regconfig, ?) AS value)
SELECT re.id AS entry_id, ts_rank_cd(re.fts_document, search_query.value) AS score
FROM retrieval_entries AS re CROSS JOIN search_query
WHERE ... AND re.fts_document @@ search_query.value
ORDER BY score DESC, re.id ASC LIMIT ?
```

SQLite：

```sql
-- 应用层先 gse.Tokenize(query)，再 MATCH
SELECT re.id AS entry_id FROM retrieval_fts f
JOIN retrieval_entries re ON re.id = f.entry_id
WHERE retrieval_fts MATCH ?  -- 分词后的查询串
  AND re.workspace_id = ? AND re.knowledge_base_id = ?
  AND re.index_generation_id = ? AND re.state = 'published'
ORDER BY rank  -- FTS5 隐藏列，BM25，负值升序即最佳
LIMIT ?
```

`FTSConfig` 概念：PG 区分 `simple` / `zhparser`，SQLite 下统一走 gse，`FTSConfig` 字段在 SQLite 下忽略（或仅 simple/zh 二值控制是否启用 gse）。

#### 1.5.5 FTS 配置错误处理

`retrieval_errors.go` 的 `translateFTSConfigError` 捕获 PG SQLSTATE 42704——SQLite 下不会产生此错误（gse 在应用层，配置错误在 Go 层返回），dialect 分发跳过。

**Phase 5 验收**：中文全文检索在 SQLite 下可用；中英文混合查询召回质量在演示场景可接受。

### Phase 6：多租户与并发模型适配

**目标**：处理 PG 的 `set_config` GUC、advisory lock、行级锁在 SQLite 下的等价物。

#### 1.6.1 WorkspaceTxRunner

`internal/infrastructure/db/workspace_tx.go:36-39` PG 现状：

```sql
SELECT set_config('app.workspace_id', ?, true);
```

SQLite 替代：**no-op**（`set_config` 是 PG 专属）。保留事务包裹（`db.Transaction`），靠应用层每个查询显式带 `workspace_id = ?`（现有查询已这么做，GUC 是兜底）。`WithinWorkspace` 内按 dialect 分发，SQLite 跳过 `Exec("SELECT set_config...")`，直接 `fn(tx)`。

**关键前提**：实现时必须 grep audit 所有 `retrieval_entries` / `documents` / `chunks` 查询，确认显式带 `workspace_id`。若有依赖 RLS / 触发器兜底的查询，需补上显式条件——这是正确性关键。

#### 1.6.2 Advisory Lock（2 处）

- `workspace_repository.go:85` `pg_advisory_xact_lock(hashtextextended('langhuan:workspace-limit', 0))` — 防 workspace 数超限。
- `oidc_auth_tx_runner.go:51` `pg_advisory_xact_lock(hashtextextended(bootstrapLockKey, 0))` — 首管理员 bootstrap 原子性。

SQLite 替代：`MaxOpenConns(1)` + `_txlock=immediate` 已保证写串行化，这两个 advisory lock 在单写锁下天然成立，SQLite 下删除即可（dialect 分发跳过）。

#### 1.6.3 行级锁（`clause.Locking`，30+ 处）

GORM SQLite 驱动**静默忽略** `clause.Locking`（不报错、不加锁）。现有锁点用于防并发重复构建——SQLite 单写锁下，`db.Transaction` + `_txlock=immediate` 已串行化所有写，语义等价。**不需逐一改写**，但在代码注释中标注：SQLite 下并发正确性靠单写锁保证，不依赖行级锁。

#### 1.6.4 FOR UPDATE SKIP LOCKED（2 处 raw SQL）

`retrieval_cleanup_repository.go:123,154` — 批量清理并发安全。SQLite 删除 `FOR UPDATE SKIP LOCKED`，改普通 `SELECT ... LIMIT ?`（单写锁下无需 skip locked）。

**Phase 6 验收**：workspace 隔离在 SQLite 下正确（查询结果不串 workspace）；并发写不冲突（靠单写锁串行化）。

### Phase 7：Redis 全部本地化

**目标**：SQLite 单机模式配套去掉 Redis 依赖，实现"单二进制 + 单 .db 文件"零外部部署。三个依赖都已通过 ports 接口隔离，零 `application` / `domain` 层改动。

#### 1.7.1 内存 JobQueue（工作量最大）

新建 `internal/adapters/queue/memory/queue.go`，实现 `ports/queue.JobQueue`（接口仅 `Enqueue` 一个方法）：

```go
type Queue struct {
    mu      sync.Mutex
    pending map[string]*task   // TaskID 去重
    handler map[string]Handler // type → handler
    wg      sync.WaitGroup
}

func (q *Queue) Enqueue(ctx context.Context, job JobRequest) (*JobHandle, error)
```

**Worker handler 重写**：现有 7 个 handler 签名是 `func(ctx, *asynq.Task) error`，需定义一个本地 Task 接口（`{Type, Payload, RetryCount, ID}`），改写 handler 注册（`main.go:370-420`）和 payload 解码。

重试 / 退避：复用 `cmd/langhuan/asynq_runtime.go:92-107` 的 `retryDelayFunc` 算法（纯函数，不依赖 asynq）。Inspector：内存版维护计数器返回 pending / dead 统计，死信列表可选持久化（单机模式重启丢失可接受）。

#### 1.7.2 内存限流器

新建 `internal/adapters/auth/memory_rate_limiter.go`，实现 `ports/auth.RateLimiter`（`IsBlocked` / `RecordFailure` / `Reset` 三方法）：

```go
type MemoryRateLimiter struct {
    mu        sync.Mutex
    counts    map[string]int
    firstSeen map[string]time.Time
}
```

逻辑直接照搬 `redis_rate_limiter.go`（固定窗口失败计数器，key = sha256(email)）。

#### 1.7.3 内存 OIDC state store

新建 `internal/adapters/auth/oidc/state_store_memory.go`，实现 `ports/auth.OIDCStateStore`（`Issue` / `Consume`）。`sync.Map` + TTL 过期清理 goroutine，或 `map[string]entry{payload, expireAt}` + `sync.Mutex`。`GETDEL` 原子性由 mutex 保证，nonce 不匹配回写逻辑照搬。

#### 1.7.4 装配分流

`cmd/langhuan/main.go` 的 `buildApp` 按 `cfg.Redis.Enabled` 分流：

```go
if cfg.Redis.Enabled {
    // 现有 Redis 装配（asynq + redis client + 限流器 + state store）
} else {
    app.jobQueue = memory.NewQueue(handlerRegistry, cfg.Queue)
    app.queueInspector = memory.NewInspector()
    limiter = auth.NewMemoryRateLimiter(cfg.Auth.RateLimit)
    stateStore = oidc.NewMemoryStateStore(cfg.Auth.OIDC.StateTTLSeconds)
}
```

限流器 / state store 复用现有 `if redisClient != nil` 守卫模式（`main.go:494, 520`）。

#### 1.7.5 readiness 检查

`cmd/langhuan/readiness.go` 已有 `if r.redis != nil` 守卫，SQLite 模式下 `redis=nil` 自动跳过 Redis ping。

**Phase 7 验收**：`redis.enabled=false` 时服务正常启动，队列任务正常调度，登录限流正常，OIDC 登录正常。

### Phase 8：测试基础设施

#### 1.8.1 SQLite 集成测试基础

新建 `internal/testsupport/sqlite.go`：用 `file:{tempdir}/langhuan_test.db?cache=shared` 或内存库，跑 SQLite 迁移，返回 `*gorm.DB`。不需要 docker（SQLite 是文件 / 内存），比 PG 测试更轻、更快。DSN 从环境变量 `LANGHUAN_TEST_SQLITE_PATH` 读（可选，默认用 `t.TempDir()`）。

#### 1.8.2 测试矩阵策略

现有 PG 集成测试（30+ `*_integration_test.go`）**保留不动**，继续跑 PG。为 SQLite 新增一套并行测试，或用 `t.Run("postgres", ...)` + `t.Run("sqlite", ...)` 参数化（推荐后者，共享断言，覆盖双方言）。检索相关测试（`retrieval_search_integration_test.go`、`migrate_v011_zhparser_integration_test.go`）需 SQLite 专属断言（gse 分词结果 vs zhparser 分词结果会有差异）。

#### 1.8.3 测试数据库隔离（遵守 AGENTS.md 5.10）

SQLite 测试库用 `t.TempDir()` 每次唯一路径，测试结束自动清理。**严禁**复用 `config.yaml` 的库（SQLite 下也不复用生产 .db 文件）。

#### 1.8.4 Makefile

新增 `test-sqlite` target：`go test -tags=integration,sqlite ./...`，走 SQLite 路径。`make test-integration` 仍跑 PG（零回归）。

**Phase 8 验收**：`make test-sqlite` 在无 docker 环境下干净通过。

### Phase 9：文档与配置示例

#### 1.9.1 文档

- `AGENTS.md` 第 3 节技术基线：补充 SQLite 支持说明（PG 为生产推荐，SQLite 为单机 / 开发）。
- `docs/ARCHITECTURE.md`：新增"数据库多驱动"章节，说明 dialect 分发设计。
- `docs/DATABASE_GUIDELINES.md`：新增 SQLite 迁移与方言分发规范。
- `ROADMAP.md`：新增版本项（如 v0.10.0 SQLite 支持）。
- `config.example.yaml`：SQLite + 无 Redis 的完整配置示例（注释段）。

#### 1.9.2 README

新增"快速开始（SQLite 单机版）"章节：单二进制 + 单 .db 文件启动指南。标注 SQLite 模式的能力边界（数据量建议 < 数万条；并发写串行；检索为暴力扫描）。

## 4. 风险与缓解

| 风险 | 概率 | 缓解 |
|---|---|---|
| sqlite-vec v0.1.x pre-v1，API 可能 breaking | 中 | 锁定 `modernc.org/sqlite` 版本，不直接依赖 asg017 上游 |
| `workspace_tx` 删除 GUC 后有查询遗漏 `workspace_id` 导致串数据 | 高 | 实现时必须 grep audit 所有 `retrieval_entries` / `documents` / `chunks` 查询，确认显式带 `workspace_id` |
| glebarez/sqlite 锁定旧版 modernc（v1.28），与 `/vec` 要求的新版冲突 | 中 | 根 go.mod 间接提升 modernc 到 v1.56.0，验证编译 |
| FTS5 + gse 召回质量与 zhparser 有差异 | 中 | 演示场景可接受；记录在 README 能力边界 |
| 000005 核心迁移（706 行）SQLite 重写工作量大 | 高 | 可分 2-3 个 SQLite 迁移版本渐进交付 |
| 内存队列重启丢任务 | 低 | 单机演示可接受；文档标注 |

## 5. PR 拆分（按依赖顺序）

1. **PR1（Phase 1）**：Dialect 抽象 + `db.Open` / `migrate.Run` 分流 + 配置 — 地基，PG 零回归。
2. **PR2（Phase 2）**：SQLite 迁移脚本套件 — 可独立验证（建表成功）。
3. **PR3（Phase 3）**：Repository 方言分发（21 文件）— CRUD 层，PG 零回归。
4. **PR4（Phase 6）**：多租户 / 并发适配 — `workspace_tx` + advisory lock + SKIP LOCKED。
5. **PR5（Phase 4 + 5）**：向量 + FTS 检索重写 — 核心能力，可合并一个 PR。
6. **PR6（Phase 7）**：Redis 本地化 — 独立于数据库改造，可并行。
7. **PR7（Phase 8 + 9）**：测试基础设施 + 文档。

总工作量估算：约 8-12 个工作日（000005 迁移 + 检索重写 + worker 重写是大头）。PG 路径全程零回归是硬约束，每个 PR 都跑现有 PG 测试套件验证。

## 6. 第一性原理核验

回到本规格的目标、约束与事实：

- **目标**：让琅嬛有一个零外部依赖（无 PG、无 Redis）的单机部署形态，定位为开发 / 演示。
- **约束**：`CGO_ENABLED=0`（已是现状）；PG 路径零回归；不引入未经证实需要的抽象（AGENTS.md 5.7）。
- **事实**：
  - 现有架构已把 PG 耦合收敛在 `db/` + `migrate/`，上层通过 Repository 接口隔离——多驱动切换的代价主要在基础设施层，不在业务层。
  - modernc.org/sqlite 官方子包内置 sqlite-vec，让 CGO_ENABLED=0 下的向量检索有等价物，不需降级。
  - golang-migrate 官方已有纯 Go SQLite 包，迁移器不用换工具。
  - 三个 Redis 依赖全部通过 ports 隔离，本地化不触碰业务层。
- **方案推导**：基于上述事实，最小可靠方案是"PG 路径零改动 + SQLite 作为并列 dialect + Redis 可选本地化"。不为 SQLite 提前抽象新的 port（检索仍走现有 `indexport.SearchRepository`，只是 SQLite 侧用 vec0 / FTS5 实现）；不引入事件总线 / CQRS 等超出当前需要的机制。

当既有 PG 实现与本规格推导的正确性 / 可维护性冲突时（如 `set_config` GUC 在 SQLite 下无对应物），本规格选择记录原因并修正既有实现的适用边界（SQLite 下 no-op + 应用层显式 workspace_id），而非用临时补丁掩盖。
