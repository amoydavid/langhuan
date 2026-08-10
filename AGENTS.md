# 琅嬛 开发指南（AGENTS.md）

本文件面向参与本项目开发的 AI Agent 与开发者，用于在每次接手任务前快速对齐项目背景、开发规范与关键文档位置。修改代码前请先阅读本指南，并根据需要深入阅读 `docs/` 目录下的专题文档。

## 1. 项目定位

**琅嬛**是一个独立的知识转化与检索服务，位于 RAG 工程中的知识处理层：

- 负责把 `pdf/docx/markdown/txt/csv/xlsx` 等输入转成可检索、可向量化、可追溯的结构。
- 通过 **REST** 与 **MCP over HTTP** 对外提供导入、状态查询、检索和删除能力。
- **不生成 LLM 答案、不编排 Chat/Agent、首版不实现图查询**。

项目采用单一进程入口 `cmd/langhuan`，同一二进制同时承载 REST、MCP 和 asynq worker。

## 2. 必读文档速查

| 文档 | 说明 |
|------|------|
| `ROADMAP.md` | 版本路线、验收标准、暂不进入首版的能力。 |
| `docs/ARCHITECTURE.md` | 系统上下文、分层架构、核心数据流、状态机、数据模型、REST/MCP 设计。 |
| **`docs/DATABASE_GUIDELINES.md`** | **数据库开发文档**。涵盖 Repository 分层、双层模型、GORM 用法、JSON 字段、迁移、事务、向量检索等数据库开发规范。 |
| `config.example.yaml` | 运行时配置示例。 |

**任何涉及数据库表、Repository、迁移、事务、GORM 查询的开发任务，必须首先阅读 `docs/DATABASE_GUIDELINES.md`。**

## 3. 技术基线

- **Go 1.26**
- **Gin**：REST 与 MCP HTTP 入口
- **GORM + PostgreSQL + pgvector**：数据持久化与向量检索
- **asynq + Redis**：异步任务队列
- **golang-migrate**：数据库迁移
- **MinerU Cloud**：首版 PDF 解析
- **YAML 配置**：运行配置从 `config.yaml` 加载，环境变量不作为主配置入口
- **依赖注入**：通过 `internal/infrastructure/di` 或构造函数显式注入，避免全局状态
- **GIT**: 使用 `Conventional Commits` 规范提交，主题、内容使用中文为主

## 4. 项目结构

```text
cmd/langhuan/              # 单一进程入口
internal/
  interfaces/              # HTTP/MCP/Worker 适配入口
    http/                  # Gin handler、路由、中间件
    mcp/                   # MCP tools
    worker/                # asynq 任务 handler
  application/             # 应用服务与流水线编排
    service/               # 用例服务
    pipeline/              # 解析 -> 资产归档 -> 分块 -> 索引
    dto/                   # 跨层数据传输对象
  domain/                  # 领域模型与领域错误
    model/                 # 纯 struct，无 ORM/HTTP/MCP 依赖
    value/                 # 值对象
    errors/                # 领域错误
  ports/                   # 领域端口（接口）
    parser/                # 文档解析
    storage/               # 对象存储
    embedding/             # Embedding 客户端
    index/                 # 向量/全文索引
    queue/                 # 异步队列
  adapters/                # 端口实现
    parser/                # MinerU Cloud、Markdown、TXT、CSV、XLSX、DOCX
    storage/               # OSS / Local
    embedding/             # OpenAI-compatible
    index/                 # pgvector、PostgreSQL FTS
    queue/                 # asynq
  infrastructure/          # 基础设施封装
    config/                # 配置加载
    db/                    # GORM 连接、Repository 实现、Row 模型
    migrate/               # 数据库迁移（golang-migrate）
    logger/                # 日志
docs/                      # 架构与开发文档
web/                       # Web 前端
```

## 5. 开发铁律

### 5.0 第一性原理（所有工作的通用要求）

本项目的所有工作——包括需求分析、架构设计、代码实现、测试、排障、评审与文档维护——都必须以**第一性原理**为起点。不要因为“现有代码就是这样”“惯例如此”或“未来可能需要”而直接复用结论；先回到问题本身，拆解目标、约束、事实与假设，再推导出当前最小且可靠的方案。

执行任务时必须遵循以下顺序：

1. **明确目标**：说明要解决的真实问题、验收标准，以及明确不在范围内的内容。
2. **区分事实与假设**：通过代码、文档、测试或可复现实验验证关键前提；无法验证的内容要显式标注为假设。
3. **拆解约束**：识别业务规则、数据边界、依赖关系、安全与性能要求，并确认它们的优先级。
4. **从约束推导方案**：优先选择满足目标的最简单实现，只引入能降低当前复杂度或风险的抽象；不得为了迎合既有形式或预留未经证实的未来需求而增加复杂性。
5. **用证据闭环**：通过针对性的测试、检查或运行结果验证结论；若结果与预期不符，回到事实和约束重新推导，而不是用临时补丁掩盖问题。

在代码评审、故障处理和方案取舍中，应能回答“这个结论基于哪些事实和不可违背的约束”。当既有实现与第一性原理推导出的正确性、安全性或可维护性冲突时，应记录原因并修正既有实现。

### 5.1 分层依赖

- `domain` **不依赖任何外部包**（HTTP、MCP、数据库、第三方 SDK）。
- `application` 依赖 `domain` 和 `ports`，编排业务流。
- `interfaces` 依赖 `application`，只做协议转换。
- `adapters` 实现 `ports`，封装第三方系统。

### 5.2 数据库访问

- **领域层与 Application 层不得直接持有 `*gorm.DB`**。
- 数据库访问统一通过 `domain` 中定义的 Repository 接口，由 `internal/infrastructure/db` 实现。
- 每个 Repository 只持有 `db *gorm.DB` 字段，由构造函数注入。
- Repository 是 GORM 的薄封装，只负责持久化，不放置业务规则。
- 领域模型与 GORM Row 必须双层分离，通过 `toRow` / `fromRow` 手动转换。

> 详见 **`docs/DATABASE_GUIDELINES.md`**。

### 5.3 错误处理

- GORM 的 `gorm.ErrRecordNotFound` 必须在 Repository 层映射为领域错误 `domainerrors.ErrNotFound`。
- 其它错误使用 `fmt.Errorf("中文描述: %w", err)` 包装，保留错误链。
- HTTP 层把领域错误映射为合适的 HTTP 状态码，不允许把底层错误直接返回给调用方。

### 5.4 事务

- 跨多表原子操作使用 `db.Transaction`，事务边界应放在能感知完整业务单元的位置（通常是 application service）。
- 事务内部必须始终使用传入的 `tx`，禁止使用外层 `db`。

### 5.5 异步任务

- 耗时操作（MinerU 解析、索引构建）必须入队 asynq，HTTP 接口不等待完成。
- worker handler 只负责解码任务和调用 application service，不直接访问数据库或第三方服务。
- 任务必须幂等：执行前检查状态，`completed/deleted` 直接跳过。

### 5.6 Workspace 隔离

- 所有业务资源归属于 `workspace`。
- v0.2.0 起不再保留非 workspace 的 HTTP 入口。
- 查询/操作必须显式校验 `workspace_id`，未来通过 workspace API token 做统一鉴权。

### 5.7 本项目中的 DDD 实践：避免盲目复杂化

DDD 是工具，不是目标。本项目鼓励**有边界的分层**，但反对为分层而分层。请按以下原则执行：

- **不要为未来可能永远用不上的能力提前抽象**。只为当前真实会替换的实现定义 port（如 MinerU Cloud parser、OpenAI-compatible embedding、OSS/local storage）。不要给“未来可能支持的图数据库、多向量库、多种队列”预留学生口。
- **domain 保持干净，但不必“充血”**。Go 适合用纯 struct 描述领域概念，业务约束放在 application service 或独立的领域函数里。不要为了让模型“面向对象”而硬造方法。
- **Repository 是薄封装**。它只做领域模型与 Row 的转换、以及标准 CRUD。不要把业务规则、状态机推进、外部调用放进 repository。
- **HTTP handler / MCP tool 只做协议转换**。参数校验、响应组装可以留在这里；业务决策交给 application service。
- **异步任务 handler 只负责解码和转发**。worker 不直接调用 parser、storage、embedding 等适配器，而是通过 application service 编排。
- **不要轻易引入事件溯源、CQRS、进程内事件总线**。当前项目用 PostgreSQL 状态机和 asynq 任务链足够表达流程；只有出现真正的跨聚合、跨服务事件需求时，再考虑事件机制。
- **简单业务允许简单实现**。如果某个功能只是“查一行记录并返回”，不要强行拆成 service + repository + domain model + port + adapter 五层。分层是手段，可读性和可维护性才是目的。

**判断标准**：新增抽象后，是否让替换实现、写测试、定位 bug 变得更简单？如果没有，就回退到更直接的写法。

### 5.8 Go 后端最佳实践

- **Context 贯穿始终**：所有 I/O（数据库、Redis、HTTP、第三方 API）都接收并透传 `context.Context`，禁止用 `context.Background()` 替代请求上下文；超时和取消要能一路传导到底层调用。
- **error 处理**：
  - 用 `errors.Is` / `errors.As` 判断错误类型，不要用字符串比较或 `==` 比较 error。
  - 每层只包一次上下文信息，避免 `fmt.Errorf("失败: %w", fmt.Errorf("失败: %w", err))` 式的重复包装。
  - 不使用裸 `panic` 处理业务错误；`panic` 只用于真正不可恢复的程序错误，HTTP/worker 入口应有 `recover` 兜底。
- **接口定义在使用方，不在实现方**：`ports` 下的接口由 `application` 层根据需要定义，`adapters` 只需实现，避免接口和实现耦合在一个包里导致循环依赖。
- **依赖注入用构造函数**：`NewXxxService(repo XxxRepository, ...)` 显式传参，禁止用包级全局变量持有 `*gorm.DB`、`*redis.Client` 等状态。
- **并发安全**：
  - goroutine 必须能被 context 取消或有明确退出条件，禁止启动"永不退出"的 goroutine 而不受 worker 生命周期管理。
  - 共享状态用 `sync.Mutex`/`sync.RWMutex` 或 channel 保护，不要假设单线程执行。
  - 使用 `errgroup` 管理一组可能失败的并发任务（如批量 embedding 调用）。
  - 每个 `defer` 匹配的资源释放前查清楚 `close`/`cancel` 会不会重复调用。
- **配置与常量**：不要在业务代码里硬编码超时时间、重试次数、批量大小等数字，统一放到 `config.yaml` 或包内具名常量。
- **日志**：使用结构化日志（key-value 字段），不要用字符串拼接；错误日志附带关键上下文（`document_id`、`workspace_id`、`job_id`），但不能包含敏感数据（见 5.5、9）。
- **测试**：
  - 领域逻辑和 application service 用表驱动测试（table-driven tests）。
  - Repository / 集成 / e2e 测试**必须**连接真实 PostgreSQL（pgvector），且该实例**只能由测试在运行期通过 docker 临时拉起**，严禁复用 `config.yaml` 中的数据库或本机长期运行的实例。详细规则见 **5.10 测试数据库隔离**。
  - 外部依赖（MinerU、embedding API、OSS）通过 `ports` 接口 mock，不在测试里发真实网络请求。
- **命名与可见性**：包内未导出的辅助函数、类型尽量小写，只导出真正需要跨包使用的符号；避免出现"上帝包"（一个包塞满不相关逻辑）。
- **禁止 import cycle 的临时手段**：不要用空 interface（`any`）绕过循环依赖，应通过调整分层或提取公共接口解决。

### 5.9 Go 文件组织与规模控制

Go 的组织边界首先是 package，文件只是同一 package 内便于阅读、定位和协作的单元。拆分文件时以**职责与共同变化原因**为依据，不以追求固定行数为目标：

- **一个文件只承载一组紧密相关的职责**。当同一文件出现多个可独立演进的 Repository、service、handler、adapter、领域资源或协议入口时，应按资源或能力拆分；不要长期依赖大段分隔注释维持结构。
- **行数只作预警，不作硬门槛**。手写生产代码接近或超过 400 行时，应主动检查是否混合了多个职责；即使不足 400 行，只要修改原因不同也应拆分。生成文件、集中式注册表和以测试数据为主的文件可以例外，但要保证仍可定位和审查。
- **优先在原 package 内拆文件**。例如使用 `document_repository.go`、`document_handler.go`、`mineru_client.go` 等可搜索的职责命名；同一类型的定义、构造函数、方法、专属私有辅助函数以及 `toRow` / `fromRow` 转换应放在一起。
- **测试按被测职责对齐**。生产文件拆分后，对应单元测试和集成测试也应按资源或场景拆分；跨资源端到端流程可以保留独立的 `*_flow_test.go`，不要把所有测试继续堆在一个通用文件中。
- **公共代码必须真的公共**。只有被多个文件稳定复用的错误映射、值类型或小型辅助能力才放入 `errors.go`、`types.go` 等聚焦文件；禁止创建含义模糊的 `utils.go`、`common.go`、`helpers.go` 或 `misc.go` 作为杂物容器。
- **避免反向过度拆分**。不要按单个函数机械建文件，也不要仅为缩短文件就新增子 package、接口或通用基类。只有当一组代码具备清晰所有权、独立依赖方向和稳定对外边界时，才考虑拆成新的 package。
- **结构整理与行为修改分开提交**。纯文件拆分不顺手改查询、事务、错误文本或业务规则；拆分后至少执行 `gofmt`、相关 package 测试、`go test ./...`、`go vet ./...` 和 `git diff --check`，证明行为未改变。

**判断标准**：开发者或 Agent 是否能从文件名直接定位职责，并在不通读无关代码的情况下安全修改和审查？如果不能，应重新划分文件边界。

### 5.10 测试数据库隔离（铁律）

**任何自动化测试——单元测试中涉及数据库的部分、集成测试、e2e 测试——需要访问数据库时，必须使用测试在运行期临时启动的、一次测试运行即弃的 PostgreSQL（含 pgvector 扩展）容器，严禁使用 `config.yaml` / `config.example.yaml` 里配置的数据库，也不得连接本机长期运行的 PostgreSQL 实例（包括默认的 `localhost:5432/langhuan`）。**

动机：测试可能随时清表、重跑迁移、注入异常数据。复用运行配置或开发库会导致真实业务数据被污染或丢失，且使测试结果不可复现。

落地要求：

- **唯一合法来源是临时的 docker 容器**。优先使用 testcontainers（`github.com/testcontainers/testcontainers-go`）在测试进程内启停；若用 `docker-compose`，必须由测试入口负责 `up` / `down`，且容器名、端口、卷与本地开发环境严格隔离。
- **镜像必须带 pgvector 与 zhparser**。当前测试镜像为 `langhuan-test-postgres:pg17`，由 `docker/postgres-test/Dockerfile` 构建；`make test-integration` 会自动执行 `make test-image`，直接运行带 `integration` tag 的单个 package 前则必须先手动执行 `make test-image`。镜像保证 `CREATE EXTENSION vector` / `halfvec` / HNSW 索引和 `CREATE EXTENSION zhparser` 与生产迁移一致。
- **DSN 注入方式**：测试辅助函数从环境变量 `LANGHUAN_TEST_DATABASE_DSN` 读取；该变量只能由临时的 docker 容器提供，**禁止把回退默认值设成 `config.yaml` 里的 DSN 或 `localhost:5432/langhuan`**。未设置该变量时，应直接跳过数据库测试（`t.Skip`）或自动拉起容器，而绝不能落到本地库。
- **迁移跑在测试库内**：测试启动后对临时库执行 `migrate.Run`，验证真实 SQL 行为；测试结束在 `t.Cleanup` 中销毁容器，不留任何持久状态。
- **隔离手段二选一**：单测用例用「容器 + 事务回滚」或「每次唯一 `runID` + 清理」保证互不污染，禁止依赖用例间共享数据。容器级别必须做到一次测试运行结束即销毁。
- **CI 与本地一致**：测试套件不得依赖任何「开发者本机已启动好的 PostgreSQL」。新增/修改涉及数据库的测试时，必须保证在干净环境（仅有 docker）下 `go test ./...` 可直接通过。
- **前端 e2e 同样适用**：`web/` 的 e2e 测试若需要后端数据库，也只能连测试期间临时拉起的 docker 数据库，不得指向 `config.yaml` 的库。

**判断标准**：在一台只装了 docker、没有任何预置 PostgreSQL 的机器上，`go test ./...`（及 web e2e）能否干净通过？如果不能，说明测试偷连了本地库，必须修。

## 6. 新增功能的推荐流程

1. **读文档**：
   - 先读 `ROADMAP.md` 确认当前版本范围。
   - 若涉及数据库，必读 `docs/DATABASE_GUIDELINES.md`。
   - 若涉及架构/数据流，参考 `docs/ARCHITECTURE.md`。

2. **定领域模型**：在 `internal/domain/model/` 新增纯 struct，描述业务语义。

3. **定义端口/接口**：在 `internal/ports/` 中新增需要外部系统实现的能力接口。

4. **实现应用服务**：在 `internal/application/service/` 编排领域对象与端口。

5. **实现适配器**：在 `internal/adapters/` 下实现第三方系统交互。

6. **实现基础设施**：
   - Repository 放在 `internal/infrastructure/db/`。
   - 迁移文件放在 `internal/infrastructure/migrate/migrations/`。
   - 配置项加到 `internal/infrastructure/config/` 与 `config.example.yaml`。

7. **接入接口**：
   - REST handler 放在 `internal/interfaces/http/`。
   - MCP tool 放在 `internal/interfaces/mcp/`。
   - worker handler 放在 `internal/interfaces/worker/`。

8. **注册依赖**：在 DI/装配处（如 `internal/infrastructure/di` 或 `cmd/langhuan`）把适配器实现接到端口上。

9. **补测试**：为新增 service、repository、adapter、handler 编写单元或集成测试。

## 7. 代码风格

- Go 代码遵循 `gofmt`、`go vet`。
- 命名：
  - 领域模型：`KnowledgeBase`、`Document`、`Chunk` 等。
  - Repository 接口：`KnowledgeBaseRepository`。
  - 实现：`KnowledgeBaseDBRepository` 或 `KnowledgeBaseRepository`（若只有一个实现）。
  - Row：`KnowledgeBaseRow`。
- 每个导出的函数、类型、包应有简洁注释。
- 禁止在 domain/model 里写 GORM tag、JSON tag 等持久化/序列化细节。
- 配置读取集中化，避免在业务代码中直接读 `os.Getenv`。

## 8. 本地开发常用命令

```bash
# 运行服务（使用默认 config.yaml）
go run ./cmd/langhuan

# 运行测试（涉及数据库的测试会临时拉起 docker 容器，见 5.10；严禁连 config.yaml 的库）
go test ./...

# 集成测试推荐入口：整套测试共享一个临时 pgvector 容器，测试结束自动销毁
make test-integration

# 首次直接运行或测试镜像变化后，先构建本地 pgvector + zhparser 镜像
make test-image

# 再按 package 自动拉起临时容器，适合只测指定 package
go test -tags=integration ./internal/infrastructure/db -count=1

# 格式化与检查
gofmt -w .
go vet ./...

# 数据库迁移（若单独调用迁移子命令）
go run ./cmd/langhuan migrate
```

> 实际可用命令以 `cmd/langhuan` 当前 CLI 实现为准。

## 9. 关键注意事项

- **配置**：使用 `config.yaml`，本地开发默认连接本机 PostgreSQL 的 `langhuan` 数据库。
- **测试数据库**：自动化测试（单元/集成/e2e）需要数据库时，只能用测试期间临时拉起的 docker PostgreSQL 容器，**严禁使用 `config.yaml` 的库或本机 `localhost:5432/langhuan`**（详见 5.10）。
- **迁移**：新增表结构必须写 `internal/infrastructure/migrate/migrations/` 下的 `up/down` SQL，版本号严格递增，优先幂等。
- **向量索引**：`chunk_embeddings.embedding` 使用 `halfvec`，查询侧表达式必须与 `000001_init` 中预建的 HNSW 部分索引完全一致，否则退化为全表扫描。
- **资产归档**：远程图片下载必须使用 SSRF-safe HTTP client。
- **日志**：不得记录 API key、完整用户文档内容等敏感信息。

## 10. 前端最佳实践（`web/`）

`web/` 是基于 shadcn-admin 模板的 React 前端：**React 19 + TypeScript 7 + Vite + TanStack Router/Query + Tailwind CSS v4 + Radix UI + Zustand + React Hook Form + Zod + Biome**。当前主要用作琅嬛的管理台，不承载业务生成逻辑。

### 10.1 目录约定

- `src/routes/`：TanStack Router 的文件路由，`routeTree.gen.ts` 是自动生成文件，**禁止手改**，改路由结构后需重新跑生成命令。
- `src/features/`：按业务域（feature）组织页面级逻辑，每个 feature 内部可以有自己的 `components/`、`hooks/`、`data/`，避免把所有页面塞进一个大目录。
- `src/components/`：跨 feature 复用的通用组件，`components/ui/` 是 shadcn/ui 生成的基础组件，**尽量不手改**，需要定制时优先包一层而不是直接改源文件。
- `src/context/`、`src/stores/`：全局状态；能用 URL/query param 表达的状态优先不进全局 store，只有真正跨路由共享的状态才放 Zustand。
- `src/lib/`：纯函数工具、API client、公共类型；不放组件。

### 10.2 数据请求

- 服务端状态一律走 **TanStack Query**（`useQuery`/`useMutation`），不要用 `useEffect + useState` 手写请求、loading、error 状态。
- Query key 要能唯一标识请求参数（如 `['documents', workspaceId, documentId]`），避免缓存串数据。
- 变更操作（mutation）成功后用 `queryClient.invalidateQueries` 或乐观更新刷新相关缓存，不要手动拼接 setState 同步多处 UI。
- 网络层统一走 `axios` 封装的 client（`src/lib`），不要在组件里裸调 `fetch`/`axios`，方便统一加鉴权头、错误处理、baseURL。

### 10.3 表单与校验

- 表单统一用 **React Hook Form + Zod**：Zod schema 定义数据形状和校验规则，`@hookform/resolvers` 接入 RHF，不要手写零散的 `if` 校验逻辑。
- 表单 schema 尽量和后端 DTO 字段对齐，减少前后端字段语义漂移。

### 10.4 组件与样式

- 样式统一用 **Tailwind CSS**，需要变体管理时用 `class-variance-authority` + `clsx`/`tailwind-merge`，不要新增 CSS Module 或行内大段 style。
- UI 基础组件优先复用 `components/ui/`（shadcn/ui + Radix），新增通用交互组件前先确认现有组件库是否已有等价实现。
- 组件保持单一职责：展示型组件不直接发请求，请求逻辑放在容器组件或自定义 hook 中（`src/hooks/` 或 feature 内的 `hooks/`）。

### 10.5 类型与代码质量

- **禁止 `any`**：确实无法确定类型时用 `unknown` 并显式收窄，或从后端 OpenAPI/类型定义生成对应类型。
- 类型定义靠近使用位置：feature 内部类型放 feature 目录，跨 feature 共享类型放 `src/lib` 或专门的 `types` 文件。
- 提交前必须通过 `pnpm check`（Biome lint + format）、`tsc -b`（`pnpm build` 会自动跑），不要带着 Biome/TypeScript 报错提交。
- 涉及交互逻辑的改动优先补 `vitest` 测试（`pnpm test`），组件级测试用 `vitest-browser-react`。
- **Lint/Format 用 Biome**：项目已从 ESLint + Prettier 迁移到 Biome（`biome.json`），不要重新引入 ESLint 或 Prettier 配置。Biome 同时负责 lint、format 和 import 排序，一条命令 `pnpm check:fix` 完成全部自动修复。

### 10.6 常用命令

```bash
# 进入 web 目录后执行（本文档命令均省略 cd）
pnpm install
pnpm dev            # 本地开发
pnpm build          # 类型检查 + 生产构建
pnpm check          # Biome lint + format 检查
pnpm check:fix      # Biome 自动修复 lint + format
pnpm test           # 运行测试
```

## 11. 一句话总则

> **后端按 DDD 分层写代码，领域层保持干净，数据库访问必读 `docs/DATABASE_GUIDELINES.md`，异步任务幂等，所有资源归属 workspace，迁移幂等、事务用 tx，**测试数据库只用临时 docker 容器、严禁连 `config.yaml` 的库**；前端数据请求走 TanStack Query，表单走 RHF + Zod，样式走 Tailwind + shadcn/ui，禁止 `any`。**
