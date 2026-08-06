# Integration Test Database Infrastructure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-test PostgreSQL containers and repeated full migrations with a disposable PostgreSQL server, one migrated template database, and isolated per-test database clones.

**Architecture:** Integration package `TestMain` functions own an automatic testcontainers server or attach to the disposable server injected by `make test-integration`. `internal/testsupport` initializes a migration template under an advisory lock, clones a unique database for each ordinary test, and creates blank databases for migration tests.

**Tech Stack:** Go 1.26, `testing`, testcontainers-go, PostgreSQL 16, pgvector, lib/pq, golang-migrate, GNU Make, Docker CLI.

## Global Constraints

- Automated tests must never read `config.yaml` or connect to a long-running local PostgreSQL database.
- Every external DSN must be paired with `LANGHUAN_TEST_RUN_ID` and target a database whose name starts with `langhuan_test`.
- Ordinary tests receive independent databases cloned from a migrated template; migration tests receive independent blank databases.
- Context is passed through every SQL and container operation.
- Production database, migration SQL, repository, and application behavior remain unchanged.
- Feature and defect implementation follows Red -> Green -> Refactor.

---

### Task 1: Disposable PostgreSQL server and database provisioning

**Files:**
- Modify: `internal/testsupport/postgres.go`
- Create: `internal/testsupport/postgres_config_test.go`
- Modify: `internal/testsupport/postgres_integration_test.go`
- Create: `internal/testsupport/postgres_testmain_integration_test.go`

**Interfaces:**
- Produces: `type Migrator func(context.Context, string) error`
- Produces: `func RunPostgresTestMain(m *testing.M, migrator Migrator) int`
- Produces: `func NewMigratedPostgres(t testing.TB) string`
- Produces: `func NewEmptyPostgres(t testing.TB) string`
- Consumes: optional `LANGHUAN_TEST_DATABASE_DSN` and `LANGHUAN_TEST_RUN_ID`.

- [ ] **Step 1: Write failing configuration and isolation tests**

Add `postgres_config_test.go` in package `testsupport` for unexported configuration behavior. Replace the old two-container assertion in the external test package with a same-server, independent-database assertion:

```go
func TestStartPostgresServerRejectsIncompleteExternalConfig(t *testing.T) {
    t.Setenv("LANGHUAN_TEST_DATABASE_DSN", "postgres://langhuan:langhuan@127.0.0.1:5432/langhuan_test?sslmode=disable")
    t.Setenv("LANGHUAN_TEST_RUN_ID", "")
    _, err := startPostgresServer(context.Background())
    if err == nil || !strings.Contains(err.Error(), "必须同时设置") {
        t.Fatalf("startPostgresServer() error = %v", err)
    }
}

func TestNewEmptyPostgresUsesSameServerAndIndependentDatabases(t *testing.T) {
    firstDSN := testsupport.NewEmptyPostgres(t)
    secondDSN := testsupport.NewEmptyPostgres(t)
    firstURL, _ := url.Parse(firstDSN)
    secondURL, _ := url.Parse(secondDSN)
    if firstURL.Host != secondURL.Host || firstURL.Path == secondURL.Path {
        t.Fatalf("first = %s, second = %s", firstDSN, secondDSN)
    }
    first := openPostgres(t, firstDSN)
    second := openPostgres(t, secondDSN)
    _, _ = first.ExecContext(context.Background(), "CREATE TABLE isolation_probe (id integer PRIMARY KEY)")
    if _, err := second.ExecContext(context.Background(), "SELECT id FROM isolation_probe"); err == nil {
        t.Fatal("second database can see first database table")
    }
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test -tags=integration ./internal/testsupport -run 'Test(StartPostgresServer|NewEmptyPostgres)' -count=1`

Expected: compilation fails because `RunPostgresTestMain`, `NewEmptyPostgres`, or the new server implementation does not exist.

- [ ] **Step 3: Implement server lifecycle, configuration guards, advisory locking, template creation, cloning, and cleanup**

Implement `PostgresServer` in `postgres.go` with these concrete rules:

```go
type Migrator func(context.Context, string) error

type PostgresServer struct {
    adminDSN        string
    runID           string
    templateName    string
    container       testcontainers.Container
}

func RunPostgresTestMain(m *testing.M, migrator Migrator) int
func NewMigratedPostgres(t testing.TB) string
func NewEmptyPostgres(t testing.TB) string
func startPostgresServer(ctx context.Context) (*PostgresServer, error)
func (server *PostgresServer) ensureTemplate(ctx context.Context, migrator Migrator) error
func (server *PostgresServer) createDatabase(ctx context.Context, templateName string) (string, error)
func (server *PostgresServer) dropDatabase(ctx context.Context, databaseName string) error
func (server *PostgresServer) close(ctx context.Context) error
```

Use `pq.QuoteIdentifier` for names, `CREATE DATABASE ... TEMPLATE template0` for blank databases, the migrated template name for ordinary databases, and `DROP DATABASE ... WITH (FORCE)` in cleanup. Acquire a session-scoped advisory lock through `*sql.Conn` around template migration and template cloning. Set the package default server before `m.Run` and clear it after cleanup.

- [ ] **Step 4: Run targeted tests and verify GREEN**

Run: `go test -tags=integration ./internal/testsupport -count=1`

Expected: all tests pass; logs show one automatic container for the package and independent database names.

- [ ] **Step 5: Refactor and commit**

Run: `gofmt -w internal/testsupport`

Run: `go test -tags=integration ./internal/testsupport -count=1`

Commit: `git add internal/testsupport && git commit -m "test: share postgres server across integration tests"`

### Task 2: Migrate integration packages to package-owned servers

**Files:**
- Create: `cmd/langhuan/postgres_testmain_integration_test.go`
- Create: `internal/infrastructure/db/postgres_testmain_integration_test.go`
- Create: `internal/infrastructure/migrate/postgres_testmain_integration_test.go`
- Create: `internal/interfaces/worker/postgres_testmain_integration_test.go`
- Modify: `cmd/langhuan/v030_e2e_test.go`
- Modify: `cmd/langhuan/model_configuration_e2e_test.go`
- Modify: `internal/infrastructure/db/repository_integration_test.go`
- Modify: `internal/infrastructure/db/repository_flow_test.go`
- Modify: `internal/infrastructure/migrate/migrate_v2_integration_test.go`
- Modify: `internal/interfaces/worker/document_tasks_integration_test.go`

**Interfaces:**
- Consumes: `testsupport.RunPostgresTestMain`, `testsupport.NewMigratedPostgres`, and `testsupport.NewEmptyPostgres` from Task 1.
- Produces: one PostgreSQL server lifecycle per integration-test package when no external DSN is supplied.

- [ ] **Step 1: Add failing package `TestMain` files and switch call sites to the new helpers**

Business-schema packages use:

```go
//go:build integration

func TestMain(m *testing.M) {
    os.Exit(testsupport.RunPostgresTestMain(m, migrate.Run))
}
```

The migration package uses:

```go
//go:build integration

func TestMain(m *testing.M) {
    os.Exit(testsupport.RunPostgresTestMain(m, nil))
}
```

Replace business test calls with `testsupport.NewMigratedPostgres(t)` and migration tests with `testsupport.NewEmptyPostgres(t)`. Remove per-test `migrate.Run` calls and now-unused imports.

- [ ] **Step 2: Run package tests and verify RED where migration/template wiring is incomplete**

Run: `go test -tags=integration ./internal/infrastructure/db ./internal/infrastructure/migrate ./internal/interfaces/worker ./cmd/langhuan -count=1`

Expected before completing all call-site changes: compilation errors or tests reporting that the old per-test helper is unavailable.

- [ ] **Step 3: Complete minimal package migration**

Keep `newAuthTestDB` transaction rollback, but make `openIntegrationTestDB` open `NewMigratedPostgres(t)` without calling `migrate.Run`. Migration tests create blank databases and retain their explicit version-by-version migration logic.

- [ ] **Step 4: Run migrated package tests and verify GREEN**

Run: `go test -tags=integration ./internal/infrastructure/db ./internal/infrastructure/migrate ./internal/interfaces/worker ./cmd/langhuan -count=1`

Expected: all four package groups pass using at most one fallback PostgreSQL container per package.

- [ ] **Step 5: Format and commit**

Run: `gofmt -w cmd/langhuan internal/infrastructure/db internal/infrastructure/migrate internal/interfaces/worker`

Commit: `git add cmd internal && git commit -m "test: provision isolated databases from package templates"`

### Task 3: Add the one-container integration entrypoint

**Files:**
- Modify: `Makefile`
- Modify: `AGENTS.md`

**Interfaces:**
- Produces: `make test-integration`
- Consumes: Docker CLI, `pgvector/pgvector:pg16`, `LANGHUAN_TEST_DATABASE_DSN`, and `LANGHUAN_TEST_RUN_ID`.

- [ ] **Step 1: Add a failing Makefile contract test through dry-run inspection**

Run: `make -n test-integration`

Expected: FAIL with `No rule to make target 'test-integration'`.

- [ ] **Step 2: Implement the Makefile target**

Add a single-shell recipe that generates a unique container name, starts PostgreSQL with a random bound host port, installs a trap before the test command, waits with `pg_isready`, derives the published port with `docker port`, and invokes:

```sh
LANGHUAN_TEST_DATABASE_DSN="postgres://langhuan:langhuan@127.0.0.1:${port}/langhuan_test?sslmode=disable" \
LANGHUAN_TEST_RUN_ID="${container_name}" \
go test -tags=integration ./... -count=1
```

Update AGENTS common commands so the optimized command is discoverable while retaining direct `go test -tags=integration ./... -count=1` as the automatic fallback.

- [ ] **Step 3: Verify the target shape and execute it**

Run: `make -n test-integration`

Expected: the recipe includes one `docker run`, paired environment variables, `go test -tags=integration`, and a cleanup trap.

Run: `make test-integration`

Expected: all integration tests pass and `docker ps` contains no container whose name starts with `langhuan-test-` after completion.

- [ ] **Step 4: Commit**

Commit: `git add Makefile AGENTS.md && git commit -m "test: add single-container integration target"`

### Task 4: Full verification and delivery review

**Files:**
- Verify all files changed in Tasks 1-3.

**Interfaces:**
- Consumes: the complete implementation.
- Produces: evidence that unit, integration, static, formatting, and repository hygiene gates pass.

- [ ] **Step 1: Run unit tests**

Run: `go test ./... -count=1`

Expected: exit 0.

- [ ] **Step 2: Run the optimized integration suite**

Run: `make test-integration`

Expected: exit 0, one PostgreSQL container for the suite, and cleanup after exit.

- [ ] **Step 3: Run static and diff checks**

Run: `go vet ./...`

Run: `git diff --check HEAD~3..HEAD`

Run: `git status --short`

Expected: vet and diff-check exit 0; status is clean.

- [ ] **Step 4: Review the final diff against the design**

Confirm that no production database or migration SQL changed, no helper reads `config.yaml`, migration tests use blank databases, and every ordinary integration test obtains a cloned database.

- [ ] **Step 5: Finish the branch**

Use `superpowers:requesting-code-review`, address findings, rerun affected verification, then use `superpowers:finishing-a-development-branch` to present merge/keep/discard options.
