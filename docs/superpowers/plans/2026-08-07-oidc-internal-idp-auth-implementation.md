# 琅嬛 OIDC 接入（内部 IdP、OIDC-first）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不破坏既有 password session 与 API Key/MCP 程序化访问的前提下，接入企业内部受控 IdP 的 OIDC Authorization Code 登录，支持 JIT 建号、email 合并、OIDC 邀请接受、已登录绑定，并提供独立的 `auth.password.enabled` 运行期开关以实现 OIDC-first 形态。

**Architecture:** 新增 `external_identities` 表（`(issuer, subject) → user`）。OIDC 登录成功后复用现有 session 模型（建 session 行 + 写 httpOnly cookie），`SessionAuth` 中间件零改动。state 安全用 Redis 存储 + 浏览器 nonce cookie 双绑、Lua compare-and-delete 一次性消费。所有建 user 路径（password 首注册、OIDC JIT、OIDC 邀请接受新建 user）共享同一把 PostgreSQL advisory transaction lock，保证 bootstrap 首管理员唯一性。provider/state store/port 接口由 application 定义、adapter 实现，全程构造函数注入，禁止包级全局。

**Tech Stack:** Go 1.26、Gin、GORM、PostgreSQL + golang-migrate、Redis（go-redis）、`github.com/coreos/go-oidc/v3/oidc` + `golang.org/x/oauth2`、crypto/rand、miniredis、httptest；前端 React 19 + TanStack Query/Router + React Hook Form + Zod + Tailwind/shadcn + Vitest。

**Spec:** `docs/superpowers/specs/2026-08-07-oidc-internal-idp-auth-design.md`。本计划任务编号与 spec 章节交叉引用，每个 Task 标注对应的 spec 节，实现前必须先读对应章节。

## Global Constraints

- 严守 AGENTS.md 5.1 / 5.8 / 5.10：domain 不依赖 HTTP/GORM/OIDC SDK；接口定义在使用方；禁止 `config.Current()`、包级全局 `var`；数据库测试只用临时 docker pgvector 容器（`LANGHUAN_TEST_DATABASE_DSN`），严禁连 `config.yaml` 的库。
- OIDC 路径产出的 session 与 password session 完全同构，`SessionAuth` 中间件、`value/auth_context.go` 的 `PrincipalUser` 不改动。
- API Key / MCP / Bearer 程序化访问路径完全不动。
- `oidc.enabled=false` 时系统行为与现状完全一致；OIDC 装配是条件挂载（`deps.OIDC == nil` 时不挂路由）。
- `password.enabled` 默认 true（向后兼容）；用 `defaultConfig()` 设默认值，不引入 `*bool`。
- `oidc.enabled=true` 但配置不全 → 启动 fail fast；`!password.enabled && !oidc.enabled` → 拒绝启动。
- OIDC discovery 采用 lazy（构造时不发网络请求，首次 AuthCodeURL/Exchange 才 discovery）；IdP 不可达不阻止琅嬛启动。
- 日志严禁出现 `sub` / `email` / `id_token` / `access_token` / `refresh_token` / `raw_profile` 明文；只记 `provider=oidc, user_id=..., action=...`。
- `raw_profile` 只保存 whitelist claims（`email`/`email_verified`/`preferred_username`/`name`/`picture`），上限 16 KiB，禁止保存完整 token 或 groups。
- invitation_token 进入 Redis state 前先算 sha256，明文不写 Redis、不入日志。
- 全程 Red → Green → Refactor；每个 Task 完成后单独提交（Conventional Commits，中文主题）。

## 路由契约（spec §11.1）

| Method | Path | 权限 | 说明 |
|---|---|---|---|
| GET | `/api/v1/auth/oidc/login` | 公开 | 普通登录 / 邀请接受发起；query 带 `next`、`invitation_token` |
| GET | `/api/v1/auth/oidc/callback` | 公开 | IdP 回调；query 带 `code`/`state` 或 `error`/`state` |
| POST | `/api/v1/auth/oidc/bind/start` | SessionAuth | 已登录绑定发起 |
| GET | `/api/v1/auth/external-identities` | SessionAuth | 当前 user 外部身份非敏感摘要 |
| GET | `/api/v1/auth/bootstrap-status` | 公开 | 扩展返回 `initialized`/`password_enabled`/`oidc_enabled` |

仅当 `deps.OIDC != nil` 时挂载前三条 OIDC 路由。`POST /auth/login` 在 `password.enabled=false` 时返回 `403 password_login_disabled`；`POST /auth/register` 在 `password.enabled=false` 时返回 `403 password_registration_disabled`。

---

### Task 1: 配置、迁移与领域持久化合同

> Spec: §7 数据模型、§12 配置、§4.2 bootstrap 锁。

**Files:**
- Create: `internal/domain/model/external_identity.go`
- Create: `internal/domain/model/external_identity_test.go`
- Modify: `internal/domain/model/user.go`
- Modify: `internal/domain/model/user_test.go`
- Create: `internal/infrastructure/migrate/migrations/000019_external_identities.up.sql`
- Create: `internal/infrastructure/migrate/migrations/000019_external_identities.down.sql`
- Modify: `internal/infrastructure/db/models.go`
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/infrastructure/config/config_test.go`
- Modify: `config.example.yaml`

**Interfaces:**
- 领域模型 `ExternalIdentity{ID, UserID, Issuer, Subject, Email, EmailVerified, RawProfile, LastAuthAt, CreatedAt, UpdatedAt}`，`NewExternalIdentity` 校验 issuer/subject/email 非空、UserID 非 Nil。
- `User` 新增 `NewProvisionalUser(email, nickname)`（`password_hash=""`）与 `HasPassword() bool`。
- `PasswordConfig` 增 `Enabled bool`；`AuthConfig` 增 `OIDC OIDCConfig{Enabled, Issuer, ClientID, ClientSecret, RedirectURL, Scopes, RequireEmailVerified, StateTTLSeconds, HTTPTimeoutSeconds}`。
- 迁移建 `external_identities` 表，`(issuer, subject)` 唯一，`user_id` 索引，三个 nonempty CHECK 约束。

- [x] 写 `ExternalIdentity` 表驱动测试：合法构造、issuer/subject/email 空 → `ErrValidation`、UserID 为 Nil → `ErrValidation`。
- [x] 写 `User` 测试：`NewProvisionalUser` 产生 `password_hash=""` 且 `HasPassword()==false`；既有 `NewUser` 的 user `HasPassword()==true`。
- [x] 写 config 测试：旧配置无 `password.enabled` 字段 → 加载后默认 true；`!password.enabled && !oidc.enabled` → 报错；`oidc.enabled=true` 缺字段 → 报错；issuer/redirect_url 非 HTTPS（非 loopback）→ 报错；`redirect_url.Path != /api/v1/auth/oidc/callback` → 报错。
- [x] 运行上述测试确认 RED（`go test ./internal/domain/model ./internal/infrastructure/config -count=1`）。
- [x] 实现 `ExternalIdentity` 领域模型与 `User` 扩展；迁移 SQL（含 down）；`ExternalIdentityRow` + codec（不放业务规则）；`defaultAuthConfig` 设 `Password.Enabled=true`、`OIDC.RequireEmailVerified=true`；`validateAuth` 增量（spec §12.3）；`config.example.yaml` 增 `auth.oidc` 块与 `auth.password.enabled`。
- [x] 运行 `gofmt -w .`、`go test ./internal/domain/model ./internal/infrastructure/config -count=1`、`go vet ./...`、`git diff --check`。

### Task 2: OIDC port 与事务 runner 合同

> Spec: §8 Port、§9.1 OIDCAuthTxRunner。

**Files:**
- Create: `internal/ports/auth/oidc.go`

**Interfaces:**
- `OIDCProvider{ AuthCodeURL(state, oidcNonce, codeChallenge string) string; Exchange(ctx, code, codeVerifier, expectedNonce string) (*OIDCProfile, error) }`。
- `OIDCProfile{Subject, Email, EmailVerified, PreferredUsername, Name, Picture, RawProfile}`。
- `OIDCStateStore{ Issue(ctx, payload) (state string, err error); Consume(ctx, state, nonce string) (*OIDCStatePayload, error) }`。
- `OIDCStatePayload{Next, BrowserNonce, OIDCNonce, PKCEVerifier, InvitationTokenHash, BindActorID, BindSessionID}`。
- `OIDCAuthTxRunner{ WithinOIDCAuth(ctx, fn func(tx OIDCAuthTx) error) error }`（由 application 定义、infrastructure/db 实现）。
- `OIDCAuthTx` 薄持久化接口（`AcquireBootstrapLock`、`CountUsers`、`FindIdentityByIssuerSubject`、`FindUserByID`、`FindUserByEmail`、`CreateUser`、`CreateIdentity`、`UpdateIdentityAuth`、`CreateSession`、`TouchLastLogin`、`FindActiveSession`、`FindPendingInvitationForUpdate`、`CreateMembership`、`MarkInvitationAccepted`）。

- [x] 确认接口签名与 spec §8 / §9.1 完全一致；`OIDCAuthTx` 方法命名与既有 repository 风格对齐（返回领域错误，`gorm.ErrRecordNotFound` 由实现层映射为 `ErrNotFound`）。
- [x] 在接口文件顶部注释写明：业务分支留在 service，runner 只建事务、提供 tx-bound 薄持久化；`AcquireBootstrapLock` 用 `pg_advisory_xact_lock(hashtextextended('langhuan:auth-bootstrap', 0))`。
- [x] 运行 `go vet ./internal/ports/...`、`gofmt`。

### Task 3: OIDC adapter（provider + Redis state store）

> Spec: §10 Adapter、§4.5 state 双绑。

**Files:**
- Modify: `go.mod` / `go.sum`（引入 `github.com/coreos/go-oidc/v3/oidc`、`golang.org/x/oauth2`）
- Create: `internal/adapters/auth/oidc/provider.go`
- Create: `internal/adapters/auth/oidc/provider_test.go`
- Create: `internal/adapters/auth/oidc/state_store_redis.go`
- Create: `internal/adapters/auth/oidc/state_store_redis_test.go`

**Interfaces:**
- `NewProvider(ctx, cfg config.OIDCConfig) (*Provider, error)`：`cfg.Enabled=false` 返回 `(nil, nil)`；配置非法返回 error；**不发起 discovery**（lazy），记下 issuer/credentials。
- `AuthCodeURL` 拼带 `nonce`/`code_challenge`/`openid profile email` 的授权 URL。
- `Exchange`：`oauth2.Exchange(code_verifier)` → `idTokenVerifier.Verify` → 校验 id_token `nonce` → `profileFromIDToken` → 可选 UserInfo whitelist 合并（UserInfo `sub` 不一致拒绝）。
- `state_store_redis`：`Issue` 生成 ≥32 字节随机 state，`SET oidc:state:<state> <payload> EX <ttl>`；`Consume` 用 Lua compare-and-delete（只有 nonce 匹配才删），常量时间比较 nonce。

- [x] 写 `provider_test`：用 `httptest.Server` 伪装 IdP（`.well-known/openid-configuration` + token + JWKS + UserInfo），覆盖验签成功 / 篡改失败 / id_token 缺失 / nonce 不匹配 / PKCE verifier 错误 / UserInfo sub 不一致 / UserInfo whitelist 合并。
- [x] 写 `state_store_redis_test`：用 `miniredis`（参考 `redis_rate_limiter_test.go` 的 `newMiniRateLimiter` 风格），覆盖 Issue/Consume 正常、state 过期、state 不存在、browser nonce 不匹配（不删 state）、一次性消费（同 state 第二次 Consume 失败）、并发多个 state 动态 nonce cookie 互不覆盖。
- [x] 运行测试确认 RED。
- [x] 实现 provider 与 state store；state 消费用 GETDEL + nonce 校验 + 不匹配回写（miniredis Lua 不支持 DEL 持久化，改用 GETDEL 保持测试与生产一致）；所有 HTTP 调用用 `http_timeout_seconds` 超时。
- [x] 运行 `go test ./internal/adapters/auth/oidc/... -count=1`、`go vet`。

### Task 4: OIDCLoginService（登录/JIT/合并/绑定）

> Spec: §6.1/§6.2/§6.3/§6.5、§9.1。

**Files:**
- Create: `internal/application/service/oidc_login.go`
- Create: `internal/application/service/oidc_login_test.go`

**Interfaces:**
- `OIDCLoginService` 依赖 `OIDCProvider`、`OIDCStateStore`、`OIDCAuthTxRunner`、`ExternalIdentityReader`、`config.SessionConfig`、`config.OIDCConfig`（取 issuer/require_email_verified）。
- `BeginLogin(ctx, next, invitationToken string, actorUserID, sessionID uuid.UUID) (authURL, browserNonce, state string, err error)`：生成 browser nonce / OIDC nonce / PKCE verifier，`sanitizeNextPath`，invitationToken 存 hash。
- `ConsumeAndExchange(ctx, code, state, browserNonce string) (*OIDCStatePayload, *OIDCProfile, error)`。
- `LoginOrProvision(ctx, profile, userAgent, ipAddr) (*model.Session, error)`：事务内 identity 命中复用 / email 命中合并 / JIT 建号（空库首用户持锁后 `count==0` 授 platform_admin）。
- `BindIdentity(ctx, actorUserID, profile) error`：事务内 `FindActiveSession` 确认 BindSessionID 仍属 BindActorID，`(issuer,sub)` 已绑别人 → `ErrConflict`，否则 `AttachIdentity`。
- `ListIdentities(ctx, userID) ([]*model.ExternalIdentity, error)`。

- [x] 写 `LoginOrProvision` 表驱动测试：`(issuer,sub)` 已绑 → 复用刷新；sub 未绑 email 命中 → 合并建 identity；都未命中 → JIT；空库首用户 → 唯一 platform_admin；sub/email 缺失或 `require_email_verified=true` 未满足 → `ErrUnauthorized`。
- [x] 写 `BindIdentity` 测试：未绑且 session 一致 → 成功；已绑别人 → `ErrConflict`；已绑自己 → 幂等；回调 session 撤销/切换 → `ErrUnauthorized`。
- [x] 写 `BeginLogin`/`ConsumeAndExchange` 测试：`next` 拒绝 `//`/绝对 URL/控制字符；invitationToken 存的是 hash 非明文；state payload 含 nonce/verifier。
- [x] 运行测试确认 RED。
- [x] 实现 service：事务伪代码严格遵循 spec §9.1（持锁 → 锁内 CountUsers → 决定 admin → Create）；email 合并/JIT 同事务提交 session，避免认证成功但持久化不完整。
- [x] 运行 `go test ./internal/application/service -run OIDC -count=1`、`go vet`。

### Task 5: InvitationService.AcceptOIDC 与 password 开关

> Spec: §6.4、§9.2/§9.3、§6.6。

**Files:**
- Modify: `internal/application/service/invitation.go`
- Modify: `internal/application/service/invitation_test.go`
- Modify: `internal/application/service/auth.go`
- Modify: `internal/application/service/auth_test.go`
- Modify: `internal/application/service/user.go`（`RegisterFirstUser` 在 `password.enabled=false` 返回 `password_registration_disabled`）

**Interfaces:**
- `InvitationService.AcceptOIDC(ctx, invitationTokenHash, profile, userAgent, ipAddr) (*model.Session, error)`：token hash 格式校验（64 位小写 hex）；`FindPendingByTokenHash` 快速失败；email 匹配 `InvitedEmail`；事务内 `FOR UPDATE` 重读 invitation，按 issuer+sub/email 决定复用/合并/JIT 建 user（JIT 持 bootstrap lock），建 membership，标记 accepted，建 session。
- `AuthService.Login` 开头检查 `passwordEnabled`（构造函数注入），false → `password_login_disabled`；`!user.HasPassword()` → 统一 `invalidLoginError`。
- `InvitationService.Accept`（既有 password 邀请）在 `password.enabled=false` 返回 `password_registration_disabled`。
- `RegisterFirstUser` 在 `password.enabled=false` 返回 `password_registration_disabled`。

- [x] 写 `AcceptOIDC` 测试：email 匹配 → 事务一致建 user/membership/identity/标记 accepted/session；email 不匹配 → `ErrForbidden`；invitation 已接受 → `ErrConflict`；不存在/过期 → `ErrNotFound`；identity 与 email 命中不同 user → `ErrConflict`。
- [x] 写 `AuthService.Login` 测试：`password.enabled=false` → `password_login_disabled`；provisional user 密码登录 → 统一失败错误（防枚举）。
- [x] 写 password 开关测试：`Accept` / `RegisterFirstUser` 在 `password.enabled=false` 均返回 `password_registration_disabled`。
- [x] 运行测试确认 RED。
- [x] 实现 AcceptOIDC（复用 §9.1 同一 `OIDCAuthTxRunner`）；password 开关在 service 层强制（非仅 handler/前端）。
- [x] 运行 `go test ./internal/application/service -count=1`、`go vet`。

### Task 6: Repository 实现与 OIDCAuthTxRunner

> Spec: §9.1 OIDCAuthTx、§4.2 advisory lock 清单、§7.1 唯一约束。

**Files:**
- Create: `internal/infrastructure/db/external_identity_repository.go`
- Create: `internal/infrastructure/db/external_identity_repository_integration_test.go`
- Modify: `internal/infrastructure/db/invitation_repository.go`（`OIDCAuthTxRunner` 实现放此处或独立文件）
- Create: `internal/infrastructure/db/oidc_auth_tx_runner.go`
- Create: `internal/infrastructure/db/oidc_auth_tx_runner_integration_test.go`

**Interfaces:**
- `ExternalIdentityRepository` 实现 `FindIdentityByIssuerSubject` / `ListByUserID` / `AttachIdentity` / `CreateProvisionalUserWithIdentity`（事务）/ `UpdateIdentityAuth`。
- `OIDCAuthTxRunner.WithinOIDCAuth` 开 `db.Transaction`，把 tx 包成 `OIDCAuthTx` 传入 fn；`AcquireBootstrapLock` 执行 `pg_advisory_xact_lock(hashtextextended('langhuan:auth-bootstrap', 0))`。
- 既有 `InvitationRepository.AcceptRegistration`（password 路径）不持锁、不改。

- [x] 写集成测试：迁移 000019 从空库执行成功；`(issuer, subject)` 重复插入失败；`CreateProvisionalUserWithIdentity` 中途失败回滚；`AttachIdentity` 正常。
- [x] 写 `OIDCAuthTxRunner` 集成测试：`WithinOIDCAuth` 内 panic 不泄漏（GORM 回滚）；`AcquireBootstrapLock` 在并发两事务中串行化（用 goroutine + channel 验证持锁期间另一事务阻塞）。
- [x] 写 **bootstrap advisory lock 并发矩阵**（spec §15.3）：空库下两两并发（password 首注册 × OIDC JIT、OIDC JIT × OIDC 邀请接受新建、password 首注册 × OIDC 邀请接受新建、三路 `errgroup` 并发），断言只产生一个 bootstrap platform_admin；已初始化库（count>0）下 JIT 与邀请接受均建普通用户。
- [x] 运行测试确认 RED（连临时 docker pgvector 容器）。
- [x] 实现 repository 与 runner；`gorm.ErrRecordNotFound` 映射 `ErrNotFound`；`RowsAffected==0` 映射 `ErrConflict`。
- [x] 运行 `go test -tags=integration ./internal/infrastructure/db -count=1`（先 `make test-image`）、`go vet`。

### Task 7: HTTP handler、路由条件挂载与装配

> Spec: §11 HTTP 接口、§13 装配、§11.4 错误码。

**Files:**
- Create: `internal/interfaces/http/oidc_handler.go`
- Create: `internal/interfaces/http/oidc_handler_test.go`
- Modify: `internal/interfaces/http/router.go`
- Modify: `internal/interfaces/http/auth_handler.go`（`bootstrap-status` 扩展返回字段）
- Modify: `cmd/langhuan/main.go`

**Interfaces:**
- `oidc_handler`：`begin` / `beginBind`（POST，SessionAuth）/ `callback` / `listIdentities`。
- `Dependencies` 增 `OIDC OIDCLoginServiceHTTP`（nil 不挂路由）。
- `bootstrap-status` 返回 `{initialized, password_enabled, oidc_enabled}`。
- 回调错误映射：IdP `error=access_denied` → `oidc_access_denied`；其余 → `oidc_provider_error`；统一 `302 /login?oidc_error=<code>`，不透传 `error_description`。
- bind 回调重新认证 session 并比对 `BindActorID`/`BindSessionID`。

- [ ] 写 handler 测试：begin 设动态 nonce cookie + 302；callback 成功建 session cookie + 302 next；callback `error=access_denied` → 302 带 `oidc_error=oidc_access_denied`；callback state 过期/nonce 不匹配 → 302 错误码；bind 未登录 → 401；`listIdentities` 不返回 subject/raw_profile；`bootstrap-status` 三字段正确。
- [ ] 运行测试确认 RED。
- [ ] 实现 handler；router 条件挂载（`if deps.OIDC != nil`）；`main.go` 装配（`NewProvider` → `NewRedisStateStore` → `NewOIDCLoginService`，nil 时不挂）；`AuthService`/`InvitationService`/`UserService` 注入 `passwordEnabled`。
- [ ] 确认 `oidc.enabled=false` 时既有路由与行为零变化（回归）。
- [ ] 运行 `go test ./internal/interfaces/http ./cmd/langhuan -count=1`、`go vet`。

### Task 8: e2e 全链路

> Spec: §15.5。

**Files:**
- Create: `cmd/langhuan/oidc_flow_e2e_test.go`

- [ ] 用 `httptest.Server` 伪装 IdP（含 JWKS 签发），跑全链路：
  - 常规登录 begin → callback → `/auth/me` 返回新 user。
  - 空库首个 OIDC JIT → platform_admin；第二个未知 OIDC 用户无 membership 且非 admin。
  - 邀请接受：建 invitation → 带 token 走 OIDC → 校验 user/identity/membership/`invitation.accepted_at` 事务一致。
  - email 合并：预置 password user → OIDC 同 email 回调 → 只增 identity 不建新 user。
  - 绑定：登录 → bind → `/auth/external-identities` 返回 issuer 摘要。
  - `password.enabled=false`：`/auth/login` → 403；OIDC 仍可用。
- [ ] 运行 e2e（临时 docker DB）；确认通过。

### Task 9: 前端登录/邀请/账号设置 OIDC 入口

> Spec: §16 前端交互。

**Files:**
- Modify: `web/src/features/auth/api.ts`（增 OIDC 跳转、`listExternalIdentities`、`bootstrap-status` 新字段类型）
- Modify: `web/src/features/auth/queries.ts`
- Modify: `web/src/features/auth/types.ts`
- Modify: `web/src/routes/(auth)/sign-in.tsx`（登录页，spec §16.1 三形态原型）
- Modify: `web/src/routes/(auth)/invitations/$token.tsx`（邀请接受页，spec §16.2 原型；既有文件，按 `password_enabled` 切换表单/SSO 按钮）
- Create: `web/src/routes/_authenticated/settings/account.tsx`（账号设置-外部身份区，spec §16.3 原型）
- Modify: `web/src/routes/_authenticated/settings/index.tsx`（增账号设置入口）
- Modify: locale 文件 `web/src/lib/i18n/locales/{zh,en}`
- Modify: 生成文件 `web/src/routeTree.gen.ts` 仅通过路由生成命令

**Interfaces:**
- `bootstrap-status` 返回 `{initialized, password_enabled, oidc_enabled}`；前端据此决定显示入口。
- `oidc_enabled=true` 显示「用企业 SSO 登录」按钮 → `window.location = /api/v1/auth/oidc/login?next=<returnTo>`。
- `password_enabled=false` 隐藏密码框；邀请页显示「用企业 SSO 接受邀请」。
- 账号设置用 `GET /auth/external-identities` 展示绑定列表 + 「绑定企业 SSO」按钮（POST `/auth/oidc/bind/start`）。

- [ ] 写 Vitest 测试：OIDC-only 形态只显示 SSO 按钮；并存形态密码框 + SSO 按钮；邀请页 OIDC 接受按钮跳正确 URL；账号设置展示 external identities 并触发 bind；bootstrap（`initialized=false`）显示首注册表单（无视 `password_enabled`）。
- [ ] 运行 `pnpm test` 确认 RED。
- [ ] 实现三页面形态切换；`oidc_error` query 解析为 toast 错误文案。
- [ ] 生成路由树；更新 i18n。
- [ ] 运行 `pnpm test`、`pnpm check`、`pnpm build`。

### Task 10: 文档、迁移验证与完成审计

> Spec: §17、§19 验收标准。

**Files:**
- Modify: `docs/API_ACCESS.md`（增 OIDC 登录章节）
- Modify: `docs/ARCHITECTURE.md`（数据模型增 `external_identities`、认证流程增 OIDC）
- Modify: `docs/superpowers/specs/2026-08-07-oidc-internal-idp-auth-design.md`（标注实现 commit）

- [ ] 文档化 OIDC 登录、邀请接受、绑定、break-glass、三种开关组合、错误码。
- [ ] 从空临时 pgvector 库跑全迁移，验证 up/down 顺序与 000019 schema/约束/索引。
- [ ] 运行 `go test ./... -count=1`、`go vet ./...`、`pnpm check`、`pnpm build`、`git diff --check`。
- [ ] 按 spec §19 验收标准逐条审计代码/测试/路由/权限/日志/装配，每条给出直接证据（commit / 测试名 / 文件行）；任一项无证据不得声明完成。
- [ ] 确认日志无 `sub`/`email`/token 明文（grep 测试与生产代码）。

## 依赖与风险提示

- **新依赖**：`coreos/go-oidc/v3` + `golang.org/x/oauth2`，Task 3 引入；确认 license 兼容（Apache-2.0 / BSD-3-Clause，OK）。
- **Task 6 是最大风险点**：bootstrap advisory lock 并发矩阵的集成测试最容易出 race；建议优先实现并反复跑 `-race`。
- **Task 顺序有依赖**：Task 1→2→3→4→5→6→7→8 严格按序（每步依赖前一步接口）；Task 9 前端可与 Task 7/8 并行起步，但 e2e（Task 8）需先通过。
- **回归红线**：每个 Task 完成后跑 `oidc.enabled=false` 的既有测试，确保零回归。
- **工作量**：参考 spec §18 ~9.5 天；Task 6（并发）与 Task 3（adapter httptest IdP）各预留缓冲。
