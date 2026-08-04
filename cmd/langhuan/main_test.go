package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

func TestPrintStartupBanner(t *testing.T) {
	var buf bytes.Buffer
	printStartupBanner(&buf)

	out := buf.String()
	if !strings.Contains(out, "▖") || !strings.Contains(out, "▛▌") {
		t.Fatalf("banner 输出缺少 ASCII art: %q", out)
	}
	if !strings.Contains(out, "Langhuan is ready to serve requests.") {
		t.Fatalf("banner 输出缺少就绪提示: %q", out)
	}
}

func TestConfigPathDefault(t *testing.T) {
	path, err := configPath([]string{"langhuan"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "config.yaml" {
		t.Fatalf("config path = %q", path)
	}
}

func TestConfigPathFromFlag(t *testing.T) {
	path, err := configPath([]string{"langhuan", "-config", "dev.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "dev.yaml" {
		t.Fatalf("config path = %q", path)
	}
}

func TestBuildAppWithoutServersSkipsExternalConnections(t *testing.T) {
	runtime, err := buildApp(context.Background(), &config.Config{
		Server: config.ServerConfig{RunHTTP: false, RunWorker: false},
		Database: config.DatabaseConfig{
			Driver: "postgres",
			DSN:    "postgres://invalid-host:5432/langhuan?sslmode=disable",
		},
		Redis: config.RedisConfig{Addr: "invalid-host:6379"},
		Log:   config.LogConfig{Level: "info"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil {
		t.Fatal("runtime is nil")
	}
	if runtime.httpServer != nil || runtime.workerServer != nil {
		t.Fatal("servers should not be initialized")
	}
}

func TestRuntimeNeedsQueueClientWhenHTTPOrWorkerEnabled(t *testing.T) {
	if !needsQueueClient(&config.Config{Server: config.ServerConfig{RunHTTP: true, RunWorker: false}}) {
		t.Fatal("HTTP runtime should require Redis/asynq client for ingest enqueue")
	}
	if !needsQueueClient(&config.Config{Server: config.ServerConfig{RunWorker: true}}) {
		t.Fatal("worker runtime should require Redis/asynq client")
	}
	if needsWorkerServer(&config.Config{Server: config.ServerConfig{RunHTTP: true, RunWorker: false}}) {
		t.Fatal("HTTP-only runtime should not require worker server")
	}
	if !needsWorkerServer(&config.Config{Server: config.ServerConfig{RunWorker: true}}) {
		t.Fatal("worker runtime should require worker server")
	}
}

func TestBuildRuntimeEmbeddingRegistrySupportsExactlyV031Providers(t *testing.T) {
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"openai", "ark", "ollama", "dashscope", "tencentcloud"} {
		if _, err := registry.Factory(value.ModelTypeEmbedding, provider); err != nil {
			t.Fatalf("provider %s: %v", provider, err)
		}
	}
	if _, err := registry.Factory(value.ModelTypeEmbedding, "qianfan"); !errors.Is(err, domainerrors.ErrUnsupportedProvider) {
		t.Fatalf("qianfan error = %v", err)
	}
}

func TestRuntimeServicesWireModelConfigurationDependencies(t *testing.T) {
	cfg := runtimeServicesConfig(t)
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	deps, err := buildRuntimeServices(nil, cfg, nil, nil, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if deps.modelProviderRepo == nil || deps.modelRepo == nil {
		t.Fatal("model repositories are not wired")
	}
	if deps.modelProviders == nil || deps.models == nil || deps.modelConnectionTests == nil {
		t.Fatal("model services are not wired")
	}
	if deps.embeddingResolver == nil || deps.retrievalRepo == nil || deps.pipeline == nil {
		t.Fatal("retrieval indexing runtime is not wired")
	}

	router := buildHTTPRouter(deps)
	for _, path := range []string{
		"/api/v1/workspaces/acme/model-providers",
		"/api/v1/admin/model-providers",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, path, nil))
		if rec.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("path %s status = %d, want 401, body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestRuntimeServicesWireFAQDocumentDependencies(t *testing.T) {
	cfg := runtimeServicesConfig(t)
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	services, err := buildRuntimeServices(nil, cfg, nil, nil, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if services.faqDocuments == nil {
		t.Fatal("FAQ document service is not wired")
	}
	if services.pipeline == nil {
		t.Fatal("document pipeline is not wired")
	}

	router := buildHTTPRouter(services)
	for _, request := range []struct {
		method string
		path   string
	}{
		{method: stdhttp.MethodPost, path: "/api/v1/workspaces/acme/knowledge-bases/" + uuid.NewString() + "/documents/faq"},
		{method: stdhttp.MethodGet, path: "/api/v1/workspaces/acme/documents/" + uuid.NewString() + "/faq"},
		{method: stdhttp.MethodPut, path: "/api/v1/workspaces/acme/documents/" + uuid.NewString() + "/faq"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(request.method, request.path, nil))
		if recorder.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", request.method, request.path, recorder.Code)
		}
	}
}

func TestRuntimeServicesWireChunkRevisionDependencies(t *testing.T) {
	cfg := runtimeServicesConfig(t)
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	services, err := buildRuntimeServices(nil, cfg, nil, nil, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if services.chunkRevisions == nil || services.chunkRevisionIndexer == nil || services.chunkRevisionStore == nil {
		t.Fatal("ChunkRevision HTTP/index/store dependencies are not wired")
	}
	router := buildHTTPRouter(services)
	path := "/api/v1/workspaces/acme/knowledge-bases/" + uuid.NewString() + "/chunks/" + uuid.NewString()
	for _, request := range []struct {
		method string
		path   string
	}{
		{method: stdhttp.MethodGet, path: path},
		{method: stdhttp.MethodGet, path: path + "/revisions"},
		{method: stdhttp.MethodPost, path: path + "/revisions"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(request.method, request.path, nil))
		if recorder.Code != stdhttp.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", request.method, request.path, recorder.Code)
		}
	}
}

func TestRuntimeServicesWireIndexGenerationDependencies(t *testing.T) {
	cfg := runtimeServicesConfig(t)
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	services, err := buildRuntimeServices(nil, cfg, nil, nil, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if services.indexGenerationStore == nil || services.indexGenerations == nil || services.indexGenerationBuilder == nil {
		t.Fatal("IndexGeneration store/lifecycle/builder dependencies are not wired")
	}
	router := buildHTTPRouter(services)
	path := "/api/v1/workspaces/acme/knowledge-bases/" + uuid.NewString() + "/index-generations"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(stdhttp.MethodGet, path, nil))
	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("GET %s status = %d, want 401", path, recorder.Code)
	}
}

func TestRuntimeServicesWireSearchDependencies(t *testing.T) {
	services := buildTestRuntimeServices(t, runtimeServicesConfig(t))
	if services.search == nil {
		t.Fatal("Search service dependency is not wired")
	}
}

func TestRuntimeServicesWireRetrievalCleanup(t *testing.T) {
	services := buildTestRuntimeServices(t, runtimeServicesConfig(t))
	if services.retrievalCleanup == nil {
		t.Fatal("Retrieval cleanup service dependency is not wired")
	}
}

func TestBuildRuntimeServicesRejectsInvalidCredentialKey(t *testing.T) {
	cfg := runtimeServicesConfig(t)
	cfg.Credentials.EncryptionKey = "invalid"
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildRuntimeServices(nil, cfg, nil, nil, registry, nil); err == nil {
		t.Fatal("expected invalid credential key error")
	}
}

func TestBuildAppHTTPOnlyWiresRuntimeServicesWithQueueClient(t *testing.T) {
	restore := stubRuntimeFactories(t)
	defer restore()

	runtime, err := buildApp(context.Background(), &config.Config{
		Server: config.ServerConfig{RunHTTP: true, RunWorker: false, HTTPAddr: "127.0.0.1:0", BaseURL: "http://127.0.0.1:8080"},
		Database: config.DatabaseConfig{
			Driver:      "postgres",
			DSN:         "postgres://stubbed/langhuan?sslmode=disable",
			AutoMigrate: false,
		},
		Redis: config.RedisConfig{Addr: "127.0.0.1:6379"},
		Storage: config.StorageConfig{
			RawDocumentDir: t.TempDir(),
		},
		Ingest: config.IngestConfig{
			MaxFileSizeBytes: 1024,
		},
		Log:         config.LogConfig{Level: "info"},
		Auth:        validAuthConfig(),
		Credentials: config.CredentialsConfig{EncryptionKey: testEncryptionKey()},
		APIKey: config.APIKeyConfig{
			DefaultLifetimeSeconds: 7776000, MaxLifetimeSeconds: 31536000,
			LastUsedTouchIntervalSeconds: 300, ActiveLimit: 100,
		},
		MCP:    config.MCPConfig{InlineIngestMaxFileSizeBytes: 1024},
		Search: config.SearchConfig{MultiKnowledgeBaseLimit: 20, MultiConcurrency: 4, MultiMergeRRFK: 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil {
		t.Fatal("runtime is nil")
	}
	if runtime.httpServer == nil {
		t.Fatal("http server is nil")
	}
	if runtime.workerServer != nil {
		t.Fatal("worker server should be nil for HTTP-only runtime")
	}
	if runtime.services == nil {
		t.Fatal("runtime services are nil")
	}
	if runtime.jobQueue == nil {
		t.Fatal("job queue is nil")
	}
	if runtime.services.documentIngest == nil {
		t.Fatal("document ingest service is nil")
	}
	if runtime.services.documents == nil {
		t.Fatal("document service is nil")
	}
	if runtime.services.jobs == nil {
		t.Fatal("job service is nil")
	}
	if runtime.services.pipeline == nil {
		t.Fatal("document pipeline is nil")
	}
	// Task 8: the full buildApp path must wire the auth services + repos so the
	// auth/invitation/membership/user handlers and SessionAuth middleware work.
	if runtime.services.auth == nil {
		t.Fatal("auth service is nil")
	}
	if runtime.services.users == nil {
		t.Fatal("user service is nil")
	}
	if runtime.services.invitations == nil {
		t.Fatal("invitation service is nil")
	}
	if runtime.services.memberships == nil {
		t.Fatal("membership service is nil")
	}
}

func TestRuntimeHTTPRouterRegistersWorkspaceRoutes(t *testing.T) {
	// With auth wired into runtimeServices (Task 8), the workspace-scoped group
	// is mounted behind SessionAuth. A request without a session cookie is
	// therefore rejected with 401 by SessionAuth (not 404 as before wiring).
	router := buildHTTPRouter(buildTestRuntimeServices(t, runtimeServicesConfig(t)))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/workspaces/not-a-uuid", nil)

	router.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, stdhttp.StatusUnauthorized, rec.Body.String())
	}
}

func TestRuntimeServicesWireAuthDependencies(t *testing.T) {
	// Task 8: buildRuntimeServices must construct the four auth repos + the
	// four auth services (users/auth/invitations/memberships) alongside the
	// existing resource services. The redisClient is nil here (no-DB stub
	// tests), so the rate limiter is nil; that is fine because these tests
	// never exercise login.
	deps := buildTestRuntimeServices(t, runtimeServicesConfig(t))
	if deps == nil {
		t.Fatal("runtime services are nil")
	}
	if deps.userRepo == nil {
		t.Fatal("user repo is nil")
	}
	if deps.sessionRepo == nil {
		t.Fatal("session repo is nil")
	}
	if deps.membershipRepo == nil {
		t.Fatal("membership repo is nil")
	}
	if deps.invitationRepo == nil {
		t.Fatal("invitation repo is nil")
	}
	if deps.users == nil {
		t.Fatal("user service is nil")
	}
	if deps.auth == nil {
		t.Fatal("auth service is nil")
	}
	if deps.invitations == nil {
		t.Fatal("invitation service is nil")
	}
	if deps.memberships == nil {
		t.Fatal("membership service is nil")
	}
}

func TestRuntimeServicesWirePublicURLs(t *testing.T) {
	cfg := runtimeServicesConfig(t)
	cfg.Server.BaseURL = "https://langhuan.example.com/console"

	deps := buildTestRuntimeServices(t, cfg)
	if deps.publicURLs == nil {
		t.Fatal("publicURLs builder is nil")
	}
	if got := deps.publicURLs.BaseURL(); got != cfg.Server.BaseURL {
		t.Fatalf("publicURLs = %q, want %q", got, cfg.Server.BaseURL)
	}
	if deps.publicURLs.URLs().MCPURL != "https://langhuan.example.com/console/mcp" {
		t.Fatalf("MCPURL = %q", deps.publicURLs.URLs().MCPURL)
	}
}

func TestRuntimeParserSupportsV030FormatsAndRejectsPDF(t *testing.T) {
	registry, err := buildRuntimeParser()
	if err != nil {
		t.Fatal(err)
	}
	for _, fileType := range []string{"markdown", "md", "txt", "csv", "xlsx", "docx"} {
		if !registry.Supports(fileType) {
			t.Fatalf("runtime parser does not support %q", fileType)
		}
	}
	if registry.Supports("pdf") {
		t.Fatal("runtime parser unexpectedly supports pdf")
	}
}

func TestRuntimeServicesWireHTTPDependenciesWithoutQueue(t *testing.T) {
	deps := buildTestRuntimeServices(t, runtimeServicesConfig(t))
	if deps == nil {
		t.Fatal("runtime services are nil")
	}
	if deps.workspaces == nil {
		t.Fatal("workspace service is nil")
	}
	if deps.knowledgeBases == nil {
		t.Fatal("knowledge base service is nil")
	}
	if deps.documents == nil {
		t.Fatal("document service is nil")
	}
	if deps.jobs == nil {
		t.Fatal("job service is nil")
	}
	if deps.documentIngest == nil {
		t.Fatal("document ingest service is nil")
	}
	if deps.pipeline == nil {
		t.Fatal("document pipeline is nil")
	}
	if deps.rawStore == nil {
		t.Fatal("raw document store is nil")
	}

	router := buildHTTPRouter(deps)
	// With auth wired (Task 8), the workspace group is mounted behind SessionAuth.
	// Requests without a session cookie are rejected with 401.
	for _, path := range []string{
		"/api/v1/workspaces/not-a-uuid",
		"/api/v1/workspaces/not-a-uuid/documents/00000000-0000-0000-0000-000000000001",
		"/api/v1/workspaces/not-a-uuid/jobs/00000000-0000-0000-0000-000000000001",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(stdhttp.MethodGet, path, nil)

			router.ServeHTTP(rec, req)

			if rec.Code != stdhttp.StatusUnauthorized {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, stdhttp.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func runtimeServicesConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Server: config.ServerConfig{RunHTTP: true, RunWorker: false, BaseURL: "http://127.0.0.1:8080"},
		Storage: config.StorageConfig{
			RawDocumentDir: t.TempDir(),
		},
		Ingest: config.IngestConfig{
			MaxFileSizeBytes: 1024,
		},
		// Task 8: buildRuntimeServices reads the full Auth block to construct
		// the Argon2 hasher, the rate limiter, and the auth/invitation
		// services. All values must be positive (config.validate rejects
		// non-positive auth params); these mirror the config defaults.
		Auth:        validAuthConfig(),
		Credentials: config.CredentialsConfig{EncryptionKey: testEncryptionKey()},
		// v0.6.0: API Key / MCP / Search 配置必须为正，且 MCP 内联上限不超过
		// 全局导入上限。
		APIKey: config.APIKeyConfig{
			DefaultLifetimeSeconds:       7776000,
			MaxLifetimeSeconds:           31536000,
			LastUsedTouchIntervalSeconds: 300,
			ActiveLimit:                  100,
		},
		MCP:    config.MCPConfig{InlineIngestMaxFileSizeBytes: 1024},
		Search: config.SearchConfig{MultiKnowledgeBaseLimit: 20, MultiConcurrency: 4, MultiMergeRRFK: 60},
	}
}

func testEncryptionKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func buildTestRuntimeServices(t *testing.T, cfg *config.Config) *runtimeServices {
	t.Helper()
	registry, err := buildRuntimeEmbeddingRegistry()
	if err != nil {
		t.Fatal(err)
	}
	services, err := buildRuntimeServices(nil, cfg, nil, nil, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	return services
}

// validAuthConfig returns a positive-valued AuthConfig used by every test that
// drives buildRuntimeServices. NewArgon2Hasher panics on zero iterations, so
// any config reaching buildRuntimeServices must set these fields. The values
// mirror the config defaults (see config.defaultAuthConfig).
func validAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		Session: config.SessionConfig{
			CookieName:      "langhuan_session",
			LifetimeSeconds: 604800,
			SecureCookie:    true,
			Domain:          "",
		},
		Password: config.PasswordConfig{
			Argon2MemoryKiB:   65536,
			Argon2Iterations:  3,
			Argon2Parallelism: 2,
		},
		RateLimit: config.RateLimitConfig{
			LoginMaxAttempts:   5,
			LoginWindowSeconds: 900,
		},
		Invitation: config.InvitationConfig{
			LifetimeSeconds: 604800,
		},
	}
}

func stubRuntimeFactories(t *testing.T) func() {
	t.Helper()
	previousOpenDatabase := openDatabase
	previousNewRedisClient := newRedisClient
	previousPingRedis := pingRedis

	openDatabase = func(string) (*gorm.DB, error) {
		return nil, nil
	}
	newRedisClient = func(*redis.Options) *redis.Client {
		return redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	}
	pingRedis = func(context.Context, *redis.Client) error {
		return nil
	}

	return func() {
		openDatabase = previousOpenDatabase
		newRedisClient = previousNewRedisClient
		pingRedis = previousPingRedis
	}
}

func TestRuntimeRunsMigrationsOnlyWhenEnabled(t *testing.T) {
	if shouldRunMigrations(&config.Config{Database: config.DatabaseConfig{AutoMigrate: false}}) {
		t.Fatal("disabled auto migration should not run migrations")
	}
	if !shouldRunMigrations(&config.Config{Database: config.DatabaseConfig{AutoMigrate: true}}) {
		t.Fatal("enabled auto migration should run migrations")
	}
}
