# 集成测试数据库基础设施设计

## 背景与目标

当前 `NewIsolatedPostgres` 每次调用都会启动一个 `pgvector/pgvector:pg16` 容器，随后多数测试再次完整执行 `migrate.Run`。数据库集成测试因此重复支付容器启动和迁移成本。

本次改造必须同时满足：

- 自动化测试只使用测试运行期启动、运行结束即销毁的 PostgreSQL 容器；
- 支持 `LANGHUAN_TEST_DATABASE_DSN` 注入由测试入口预拉的临时容器，不回退到 `config.yaml` 或本机长期数据库；
- 普通集成测试互不共享业务数据，支持 package 与 test 并行；
- 完整迁移只在模板数据库执行一次，普通测试从模板快速克隆独立数据库；
- 迁移升降级测试使用同一临时 PostgreSQL 服务中的空白独立数据库；
- 直接执行 `go test -tags=integration` 时仍能由 testcontainers 自动启动临时容器。

## 方案选择

采用“临时 PostgreSQL 服务 + 模板数据库 + 每测试独立数据库”，不采用 per-test schema 或统一事务回滚作为默认隔离方式。

Schema 隔离需要为每个 schema 重放迁移，无法消除主要成本；统一事务无法覆盖多连接、并发事务、提交时约束、HTTP、worker 和迁移测试。数据库模板克隆保留真实 PostgreSQL 行为，同时让每个测试拥有独立的迁移版本表、表数据和连接池。

## 组件边界

### `internal/testsupport` PostgreSQL 服务器

`PostgresServer` 负责一个临时 PostgreSQL 服务的生命周期：

- 若同时设置 `LANGHUAN_TEST_DATABASE_DSN` 与 `LANGHUAN_TEST_RUN_ID`，使用外部临时容器；
- 否则通过 testcontainers 启动 `pgvector/pgvector:pg16`；
- 外部 DSN 必须是 PostgreSQL URL，且数据库名以 `langhuan_test` 开头，避免误连默认开发库；
- 自动容器由 package `TestMain` 在 `m.Run` 结束后销毁；外部容器由启动它的 Makefile 目标销毁；
- 所有创建、克隆、删除数据库的操作通过管理数据库连接完成，数据库名使用生成的 UUID 并作为 SQL identifier 安全引用。

### 迁移模板

`RunPostgresTestMain` 接收可选迁移函数。需要最新业务 schema 的 package 传入 `migrate.Run`：

1. 使用 `LANGHUAN_TEST_RUN_ID` 派生稳定且合法的模板数据库名；
2. 获取 PostgreSQL advisory lock，避免多个 package 进程同时初始化模板；
3. 模板不存在时从 `template0` 创建；
4. 对模板执行 `migrate.Run`，已是最新版本时允许 `ErrNoChange`；
5. 克隆测试数据库时获取同一把锁，确保模板没有活动迁移连接。

外部单容器模式下，各 package 共用同一模板；自动 fallback 模式下，每个 package 有自己的临时容器和模板。

### 每测试数据库

- `NewMigratedPostgres(t)` 从模板创建唯一数据库，并注册 `t.Cleanup` 执行 `DROP DATABASE ... WITH (FORCE)`；
- `NewEmptyPostgres(t)` 从 `template0` 创建唯一空白数据库，供迁移测试自行执行指定版本；
- 测试打开的 GORM/SQL 连接继续按后进先出的 `t.Cleanup` 先关闭，随后数据库才被删除；`WITH (FORCE)` 为失败路径提供兜底；
- 原有 `newAuthTestDB` 可保留事务回滚，但事务不再承担测试间的唯一隔离职责。

## 测试入口

数据库集成测试涉及五个 package：`cmd/langhuan`、`internal/infrastructure/db`、`internal/infrastructure/migrate`、`internal/interfaces/worker` 和 `internal/testsupport`。

- 需要业务 schema 的前三类业务 package 在 integration build tag 下增加 `TestMain`，调用 `RunPostgresTestMain(m, migrate.Run)`；
- migrate 与 testsupport package 调用无迁移模板的入口，只申请空白数据库；
- 普通 `go test ./...` 不受 integration-only 文件影响。

## Makefile 单容器模式

新增 `make test-integration`：

1. 用 Docker 启动一个随机命名、随机宿主端口的 `pgvector/pgvector:pg16` 容器，数据库名固定为 `langhuan_test`；
2. 等待 `pg_isready`；
3. 注入 `LANGHUAN_TEST_DATABASE_DSN` 与唯一 `LANGHUAN_TEST_RUN_ID`；
4. 执行 `go test -tags=integration ./... -count=1`；
5. 使用 shell trap 无论成功或失败都销毁容器。

这条入口实现整套测试一个容器。开发者直接运行 Go 测试命令时，按 package 自动拉起容器作为兼容兜底。

## 错误处理与安全

- 环境变量只设置其中一个时立即报错，不静默回退；
- 外部 DSN 指向 `langhuan` 等非测试库名时，在建立连接或执行 SQL 前拒绝；
- 模板初始化、数据库创建/删除和容器终止错误保留上下文；
- 不读取 `config.yaml`，不提供 localhost 开发库默认值；Docker Desktop 映射到 `127.0.0.1` 是合法的临时容器入口，因此不能仅凭主机名判断安全性；
- 模板数据库不单独删除，由所属临时容器销毁，避免 package 提前结束影响仍在运行的其它 package。

## 验收标准

- 配置解析测试覆盖无环境变量、缺失配对变量、危险数据库名和合法临时 DSN；
- testsupport 集成测试证明两个测试数据库位于同一 PostgreSQL 服务但数据相互不可见；
- testsupport 集成测试证明模板克隆包含迁移后的表，空白数据库不包含业务表；
- `go test ./... -count=1`、`go test -tags=integration ./... -count=1`、`go vet ./...` 和 `git diff --check` 通过；
- `make test-integration` 实际只启动一个 PostgreSQL 容器并在结束后清理；
- 不修改生产数据库访问路径、迁移 SQL 或业务行为。
