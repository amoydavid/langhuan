//go:build integration

// Package testsupport provides infrastructure shared by integration tests.
//
// 数据库隔离铁律（AGENTS 5.10）：集成测试使用的 PostgreSQL 必须是测试运行期
// 临时拉起的 docker 容器，一次测试运行即弃，严禁连接 config.yaml 的开发库或
// 本机长期运行的 PostgreSQL（包括默认的 localhost:5432/langhuan）。
package testsupport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// 测试专用镜像：pgvector/pg17 + zhparser 中文分词（见 docker/postgres-test/Dockerfile，
	// 用 make test-image 构建）。必须是测试期间临时拉起的 docker 容器，严禁使用
	// config.yaml 的开发库或本机长期运行的 PostgreSQL（AGENTS.md 5.10）。
	testPostgresImage      = "langhuan-test-postgres:pg17"
	postgresDSNEnv         = "LANGHUAN_TEST_DATABASE_DSN"
	postgresRunIDEnv       = "LANGHUAN_TEST_RUN_ID"
	automaticDatabase      = "langhuan_test"
	postgresSetupTimeout   = 3 * time.Minute
	postgresCleanupTimeout = 30 * time.Second
)

// Migrator upgrades a database at the supplied DSN to the current schema.
type Migrator func(context.Context, string) error

type postgresServerConfig struct {
	external bool
	dsn      string
	runID    string
}

// PostgresServer owns or attaches to one disposable PostgreSQL service.
// Ordinary tests clone an isolated database from its migrated template.
type PostgresServer struct {
	adminDSN      string
	templateName  string
	templateReady bool
	container     testcontainers.Container
}

var (
	defaultPostgresMu sync.RWMutex
	defaultPostgres   *PostgresServer
)

// RunPostgresTestMain prepares a disposable PostgreSQL service for one test
// package, optionally migrates a reusable template, runs the package tests,
// and tears down an automatically started container afterwards.
func RunPostgresTestMain(m *testing.M, migrator Migrator) int {
	ctx, cancel := context.WithTimeout(context.Background(), postgresSetupTimeout)
	server, err := startPostgresServer(ctx)
	if err == nil && migrator != nil {
		err = server.ensureTemplate(ctx, migrator)
	}
	cancel()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "准备测试 PostgreSQL 失败: %v\n", err)
		if server != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), postgresCleanupTimeout)
			_ = server.close(cleanupCtx)
			cleanupCancel()
		}
		return 1
	}

	setDefaultPostgres(server)
	code := m.Run()
	setDefaultPostgres(nil)

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), postgresCleanupTimeout)
	defer cleanupCancel()
	if err := server.close(cleanupCtx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "清理测试 PostgreSQL 失败: %v\n", err)
		if code == 0 {
			return 1
		}
	}
	return code
}

// NewMigratedPostgres clones a unique database from the package's migrated
// template and removes it when the calling test finishes.
func NewMigratedPostgres(t testing.TB) string {
	t.Helper()
	server := requireDefaultPostgres(t)
	if !server.templateReady {
		t.Fatal("测试 PostgreSQL 未初始化迁移模板")
	}
	return server.newTestDatabase(t, server.templateName)
}

// NewEmptyPostgres creates a unique empty database from template0 and removes
// it when the calling test finishes. Migration tests use this helper so they
// can exercise exact up/down version transitions themselves.
func NewEmptyPostgres(t testing.TB) string {
	t.Helper()
	return requireDefaultPostgres(t).newTestDatabase(t, "template0")
}

func postgresServerConfigFromEnv(getenv func(string) string) (postgresServerConfig, error) {
	dsn := strings.TrimSpace(getenv(postgresDSNEnv))
	runID := strings.TrimSpace(getenv(postgresRunIDEnv))
	if (dsn == "") != (runID == "") {
		return postgresServerConfig{}, fmt.Errorf("%s 与 %s 必须同时设置", postgresDSNEnv, postgresRunIDEnv)
	}
	if dsn == "" {
		return postgresServerConfig{}, nil
	}
	databaseName, err := databaseNameFromDSN(dsn)
	if err != nil {
		return postgresServerConfig{}, err
	}
	if !strings.HasPrefix(databaseName, automaticDatabase) {
		return postgresServerConfig{}, fmt.Errorf("测试数据库名必须以 %s 开头", automaticDatabase)
	}
	return postgresServerConfig{external: true, dsn: dsn, runID: runID}, nil
}

func startPostgresServer(ctx context.Context) (*PostgresServer, error) {
	config, err := postgresServerConfigFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	if config.external {
		adminDSN, err := withDatabaseName(config.dsn, "postgres")
		if err != nil {
			return nil, err
		}
		if err := pingPostgres(ctx, adminDSN); err != nil {
			return nil, fmt.Errorf("连接外部临时 PostgreSQL 失败: %w", err)
		}
		return newPostgresServer(adminDSN, config.runID, nil), nil
	}
	if err := ensureTestPostgresImage(ctx); err != nil {
		return nil, err
	}

	container, err := postgres.Run(ctx, testPostgresImage,
		postgres.WithDatabase(automaticDatabase),
		postgres.WithUsername("langhuan"),
		postgres.WithPassword("langhuan"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("启动测试 PostgreSQL 容器失败: %w", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(container, testcontainers.StopContext(ctx))
		return nil, fmt.Errorf("获取测试 PostgreSQL 连接串失败: %w", err)
	}
	adminDSN, err := withDatabaseName(dsn, "postgres")
	if err != nil {
		_ = testcontainers.TerminateContainer(container, testcontainers.StopContext(ctx))
		return nil, err
	}
	return newPostgresServer(adminDSN, uuid.NewString(), container), nil
}

func ensureTestPostgresImage(ctx context.Context) error {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return fmt.Errorf("连接 Docker 失败: %w", err)
	}
	defer provider.Close()
	images, err := provider.ListImages(ctx)
	if err != nil {
		return fmt.Errorf("列出 Docker 镜像失败: %w", err)
	}
	return validateTestPostgresImage(images)
}

func validateTestPostgresImage(images []testcontainers.ImageInfo) error {
	for _, image := range images {
		if image.Name == testPostgresImage {
			return nil
		}
	}
	return fmt.Errorf("测试 PostgreSQL 镜像 %s 不存在，请先执行 make test-image", testPostgresImage)
}

func newPostgresServer(adminDSN, runID string, container testcontainers.Container) *PostgresServer {
	digest := sha256.Sum256([]byte(runID))
	return &PostgresServer{
		adminDSN:     adminDSN,
		templateName: "langhuan_test_template_" + fmt.Sprintf("%x", digest[:6]),
		container:    container,
	}
}

func (server *PostgresServer) ensureTemplate(ctx context.Context, migrator Migrator) error {
	err := server.withTemplateLock(ctx, func(conn *sql.Conn) error {
		exists, err := databaseExists(ctx, conn, server.templateName)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := conn.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(server.templateName)+" TEMPLATE template0"); err != nil {
				return fmt.Errorf("创建迁移模板数据库失败: %w", err)
			}
		}
		templateDSN, err := withDatabaseName(server.adminDSN, server.templateName)
		if err != nil {
			return err
		}
		if err := migrator(ctx, templateDSN); err != nil {
			return fmt.Errorf("迁移测试模板数据库失败: %w", err)
		}
		return nil
	})
	if err == nil {
		server.templateReady = true
	}
	return err
}

func (server *PostgresServer) newTestDatabase(t testing.TB, templateName string) string {
	t.Helper()
	databaseName := "langhuan_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), postgresSetupTimeout)
	dsn, err := server.createDatabase(ctx, databaseName, templateName)
	cancel()
	if err != nil {
		t.Fatalf("创建隔离测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), postgresCleanupTimeout)
		defer cleanupCancel()
		if err := server.dropDatabase(cleanupCtx, databaseName); err != nil {
			t.Errorf("删除隔离测试数据库失败: %v", err)
		}
	})
	return dsn
}

func (server *PostgresServer) createDatabase(ctx context.Context, databaseName, templateName string) (string, error) {
	err := server.withTemplateLock(ctx, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx,
			"CREATE DATABASE "+pq.QuoteIdentifier(databaseName)+" TEMPLATE "+pq.QuoteIdentifier(templateName),
		)
		if err != nil {
			return fmt.Errorf("从模板 %s 创建数据库失败: %w", templateName, err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return withDatabaseName(server.adminDSN, databaseName)
}

func (server *PostgresServer) dropDatabase(ctx context.Context, databaseName string) error {
	database, err := sql.Open("postgres", server.adminDSN)
	if err != nil {
		return fmt.Errorf("打开测试 PostgreSQL 管理连接失败: %w", err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(databaseName)+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("删除数据库 %s 失败: %w", databaseName, err)
	}
	return nil
}

func (server *PostgresServer) withTemplateLock(ctx context.Context, operation func(*sql.Conn) error) error {
	database, err := sql.Open("postgres", server.adminDSN)
	if err != nil {
		return fmt.Errorf("打开测试 PostgreSQL 管理连接失败: %w", err)
	}
	defer database.Close()
	conn, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取测试 PostgreSQL 管理连接失败: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext($1))", server.templateName); err != nil {
		return fmt.Errorf("获取测试数据库模板锁失败: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), postgresCleanupTimeout)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "SELECT pg_advisory_unlock(hashtext($1))", server.templateName)
	}()
	return operation(conn)
}

func (server *PostgresServer) close(ctx context.Context) error {
	if server.container == nil {
		return nil
	}
	if err := testcontainers.TerminateContainer(server.container, testcontainers.StopContext(ctx)); err != nil {
		return fmt.Errorf("终止测试 PostgreSQL 容器失败: %w", err)
	}
	return nil
}

func databaseExists(ctx context.Context, conn *sql.Conn, databaseName string) (bool, error) {
	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", databaseName).Scan(&exists); err != nil {
		return false, fmt.Errorf("查询测试数据库 %s 失败: %w", databaseName, err)
	}
	return exists, nil
}

func pingPostgres(ctx context.Context, dsn string) error {
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer database.Close()
	return database.PingContext(ctx)
}

func withDatabaseName(dsn, databaseName string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("解析测试 PostgreSQL DSN 失败: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("测试 PostgreSQL DSN 必须使用 postgres URL")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("测试 PostgreSQL DSN 缺少主机")
	}
	parsed.Path = "/" + databaseName
	parsed.RawPath = ""
	return parsed.String(), nil
}

func databaseNameFromDSN(dsn string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("解析测试 PostgreSQL DSN 失败: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("测试 PostgreSQL DSN 必须使用 postgres URL")
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if databaseName == "" || strings.Contains(databaseName, "/") {
		return "", fmt.Errorf("测试 PostgreSQL DSN 缺少合法数据库名")
	}
	return databaseName, nil
}

func setDefaultPostgres(server *PostgresServer) {
	defaultPostgresMu.Lock()
	defer defaultPostgresMu.Unlock()
	defaultPostgres = server
}

func requireDefaultPostgres(t testing.TB) *PostgresServer {
	t.Helper()
	defaultPostgresMu.RLock()
	server := defaultPostgres
	defaultPostgresMu.RUnlock()
	if server == nil {
		t.Fatal("数据库集成测试必须通过 RunPostgresTestMain 运行")
	}
	return server
}
