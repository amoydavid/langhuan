package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testEncryptionKey = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc="

func TestV060DefaultsAndNormalization(t *testing.T) {
	cfg := defaultConfig()
	if cfg.APIKey.DefaultLifetimeSeconds != 7776000 {
		t.Fatalf("api_key.default_lifetime_seconds = %d, want 7776000", cfg.APIKey.DefaultLifetimeSeconds)
	}
	if cfg.APIKey.MaxLifetimeSeconds != 31536000 {
		t.Fatalf("api_key.max_lifetime_seconds = %d, want 31536000", cfg.APIKey.MaxLifetimeSeconds)
	}
	if cfg.APIKey.LastUsedTouchIntervalSeconds != 300 {
		t.Fatalf("api_key.last_used_touch_interval_seconds = %d, want 300", cfg.APIKey.LastUsedTouchIntervalSeconds)
	}
	if cfg.APIKey.ActiveLimit != 100 {
		t.Fatalf("api_key.active_limit = %d, want 100", cfg.APIKey.ActiveLimit)
	}
	if cfg.MCP.InlineIngestMaxFileSizeBytes != 8388608 {
		t.Fatalf("mcp.inline_ingest_max_file_size_bytes = %d, want 8388608", cfg.MCP.InlineIngestMaxFileSizeBytes)
	}
	if cfg.Search.MultiKnowledgeBaseLimit != 20 || cfg.Search.MultiConcurrency != 4 || cfg.Search.MultiMergeRRFK != 60 {
		t.Fatalf("search defaults = %#v", cfg.Search)
	}

	// 完整默认配置应能通过校验（base_url 默认本地安全值）。
	cfg.Database.DSN = "postgres://unused-in-config-unit-test"
	cfg.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32))
	cfg.Server.BaseURL = "https://example.com/langhuan///"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	if cfg.Server.BaseURL != "https://example.com/langhuan" {
		t.Fatalf("base_url = %q, want normalized", cfg.Server.BaseURL)
	}

	cfg.Server.BaseURL = ""
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "server.base_url 不能为空") {
		t.Fatalf("validate() empty base_url error = %v", err)
	}
}

func TestRetrievalRetentionDefaultsAndValidation(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Retrieval.FailedStagingRetention != 24*time.Hour ||
		cfg.Retrieval.RetiredGenerationRetention != 168*time.Hour ||
		cfg.Retrieval.CleanupBatchSize != 1000 {
		t.Fatalf("retrieval defaults = %#v", cfg.Retrieval)
	}
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Credentials.EncryptionKey = testEncryptionKey
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "failed retention", mutate: func(cfg *Config) { cfg.Retrieval.FailedStagingRetention = 0 }},
		{name: "retired retention", mutate: func(cfg *Config) { cfg.Retrieval.RetiredGenerationRetention = -time.Hour }},
		{name: "batch zero", mutate: func(cfg *Config) { cfg.Retrieval.CleanupBatchSize = 0 }},
		{name: "batch too large", mutate: func(cfg *Config) { cfg.Retrieval.CleanupBatchSize = 10001 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cfg
			test.mutate(&candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("validate error = nil")
			}
		})
	}
}

func TestLoadRetrievalRetentionFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
retrieval:
  failed_staging_retention: 12h
  retired_generation_retention: 240h
  cleanup_batch_size: 321
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retrieval.FailedStagingRetention != 12*time.Hour ||
		cfg.Retrieval.RetiredGenerationRetention != 240*time.Hour ||
		cfg.Retrieval.CleanupBatchSize != 321 {
		t.Fatalf("retrieval config = %#v", cfg.Retrieval)
	}
}

func appendTestCredentials(content []byte) []byte {
	return append(content, []byte("\ncredentials:\n  encryption_key: \""+testEncryptionKey+"\"\n")...)
}

func TestCredentialsEncryptionKeyMustDecodeTo32Bytes(t *testing.T) {
	t.Parallel()

	valid := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Credentials.EncryptionKey = valid
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	decoded, err := cfg.Credentials.DecodeEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, bytes.Repeat([]byte{7}, 32)) {
		t.Fatalf("decoded key = %x", decoded)
	}

	tests := []string{
		"",
		"not-base64",
		base64.StdEncoding.EncodeToString([]byte("short")),
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31)),
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 33)),
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
			cfg.Credentials.EncryptionKey = raw
			err := cfg.validate()
			if err == nil {
				t.Fatal("expected invalid encryption key error")
			}
			if strings.Contains(err.Error(), raw) && raw != "" {
				t.Fatalf("error leaked raw key: %v", err)
			}
		})
	}
}

func TestLoadConfigFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
server:
  http_addr: ":18080"
  base_url: "https://langhuan.example.com/console/"
  run_http: true
  run_worker: false
database:
  driver: postgres
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
redis:
  addr: "127.0.0.1:6379"
  password: ""
  db: 0
log:
  level: debug
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.HTTPAddr != ":18080" {
		t.Fatalf("HTTPAddr = %q", cfg.Server.HTTPAddr)
	}
	if cfg.Server.BaseURL != "https://langhuan.example.com/console" {
		t.Fatalf("BaseURL = %q", cfg.Server.BaseURL)
	}
	if cfg.Server.RunWorker {
		t.Fatal("RunWorker should be false")
	}
	if cfg.Database.Driver != "postgres" {
		t.Fatalf("database driver = %q", cfg.Database.Driver)
	}
	if !cfg.Database.AutoMigrate {
		t.Fatal("database.auto_migrate should be true")
	}
	if cfg.Redis.Addr != "127.0.0.1:6379" {
		t.Fatalf("redis addr = %q", cfg.Redis.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("log level = %q", cfg.Log.Level)
	}
}

func TestConfigNormalizesBaseURL(t *testing.T) {
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Server.BaseURL = "https://langhuan.example.com/console///"
	cfg.Credentials.EncryptionKey = testEncryptionKey

	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.BaseURL != "https://langhuan.example.com/console" {
		t.Fatalf("base_url = %q", cfg.Server.BaseURL)
	}
}

func TestConfigRequiresBaseURL(t *testing.T) {
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Credentials.EncryptionKey = testEncryptionKey
	cfg.Server.BaseURL = ""
	if err := cfg.validate(); err == nil {
		t.Fatal("empty base_url should be rejected")
	}
}

func TestConfigRejectsInvalidBaseURL(t *testing.T) {
	tests := []string{
		"/relative",
		"ftp://langhuan.example.com",
		"https://user:pass@langhuan.example.com",
		"https://langhuan.example.com?source=config",
		"https://langhuan.example.com#fragment",
		"https:///missing-host",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
			cfg.Server.BaseURL = raw
			if err := cfg.validate(); err == nil {
				t.Fatalf("validate() accepted invalid base_url %q", raw)
			}
		})
	}
}

func TestLoadConfigUsesDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
`))
	if err := os.WriteFile("config.yaml", content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if cfg.Server.HTTPAddr != ":8080" {
		t.Fatalf("default HTTPAddr = %q", cfg.Server.HTTPAddr)
	}
	if !cfg.Server.RunHTTP {
		t.Fatal("server.run_http should default to true")
	}
	if !cfg.Server.RunWorker {
		t.Fatal("server.run_worker should default to true")
	}
	if cfg.Database.DSN != "postgres://localhost:5432/langhuan?sslmode=disable" {
		t.Fatalf("database dsn = %q", cfg.Database.DSN)
	}
	if !cfg.Database.AutoMigrate {
		t.Fatal("database.auto_migrate should default to true")
	}
	if cfg.Storage.RawDocumentDir != "./data/raw-documents" {
		t.Fatalf("storage.raw_document_dir default = %q", cfg.Storage.RawDocumentDir)
	}
	if cfg.Ingest.MaxFileSizeBytes != 50*1024*1024 {
		t.Fatalf("ingest.max_file_size_bytes default = %d", cfg.Ingest.MaxFileSizeBytes)
	}
	wantTypes := []string{"pdf", "markdown", "md", "txt", "csv", "xlsx", "docx"}
	if len(cfg.Ingest.AllowedFileTypes) != len(wantTypes) {
		t.Fatalf("ingest.allowed_file_types default length = %d", len(cfg.Ingest.AllowedFileTypes))
	}
	for i, want := range wantTypes {
		if cfg.Ingest.AllowedFileTypes[i] != want {
			t.Fatalf("ingest.allowed_file_types[%d] = %q, want %q", i, cfg.Ingest.AllowedFileTypes[i], want)
		}
	}
}

func TestDefaultConfigAllowedFileTypes(t *testing.T) {
	want := []string{"pdf", "markdown", "md", "txt", "csv", "xlsx", "docx"}
	got := defaultConfig().Ingest.AllowedFileTypes
	if len(got) != len(want) {
		t.Fatalf("allowed file types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("allowed file types = %v, want %v", got, want)
		}
	}
}

func TestLoadStorageAndIngestConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
storage:
  raw_document_dir: "./tmp/raw"
ingest:
  max_file_size_bytes: 1024
  allowed_file_types:
    - pdf
    - txt
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.RawDocumentDir != "./tmp/raw" {
		t.Fatalf("storage.raw_document_dir = %q", cfg.Storage.RawDocumentDir)
	}
	if cfg.Ingest.MaxFileSizeBytes != 1024 {
		t.Fatalf("ingest.max_file_size_bytes = %d", cfg.Ingest.MaxFileSizeBytes)
	}
	wantTypes := []string{"pdf", "txt"}
	if len(cfg.Ingest.AllowedFileTypes) != len(wantTypes) {
		t.Fatalf("ingest.allowed_file_types length = %d", len(cfg.Ingest.AllowedFileTypes))
	}
	for i, want := range wantTypes {
		if cfg.Ingest.AllowedFileTypes[i] != want {
			t.Fatalf("ingest.allowed_file_types[%d] = %q, want %q", i, cfg.Ingest.AllowedFileTypes[i], want)
		}
	}
}

func TestLoadConfigAllowsDisablingAutoMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
  auto_migrate: false
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.AutoMigrate {
		t.Fatal("database.auto_migrate should respect explicit false")
	}
}

func TestLoadConfigDoesNotReadEnvironmentOverrides(t *testing.T) {
	t.Setenv("LANGHUAN_DATABASE_DSN", "postgres://should-not-be-used")
	t.Setenv("DATABASE_DSN", "postgres://should-not-be-used")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.DSN != "postgres://localhost:5432/langhuan?sslmode=disable" {
		t.Fatalf("YAML dsn should win, got %q", cfg.Database.DSN)
	}
}

func TestLoadConfigRejectsMissingDatabaseDSN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  http_addr: ':8080'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected missing database dsn error")
	}
}

func TestLoadConfigRejectsNegativeMaxFileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
ingest:
  max_file_size_bytes: -1
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected negative ingest.max_file_size_bytes error")
	}
}

func TestLoadConfigPopulatesAuthDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.Session.CookieName != "langhuan_session" {
		t.Fatalf("session.cookie_name default = %q", cfg.Auth.Session.CookieName)
	}
	if cfg.Auth.Session.LifetimeSeconds != 604800 {
		t.Fatalf("session.lifetime_seconds default = %d", cfg.Auth.Session.LifetimeSeconds)
	}
	if !cfg.Auth.Session.SecureCookie {
		t.Fatal("session.secure_cookie default should be true")
	}
	if cfg.Auth.Session.Domain != "" {
		t.Fatalf("session.domain default = %q", cfg.Auth.Session.Domain)
	}
	if cfg.Auth.Password.Argon2MemoryKiB != 65536 {
		t.Fatalf("password.argon2_memory_kib default = %d", cfg.Auth.Password.Argon2MemoryKiB)
	}
	if cfg.Auth.Password.Argon2Iterations != 3 {
		t.Fatalf("password.argon2_iterations default = %d", cfg.Auth.Password.Argon2Iterations)
	}
	if cfg.Auth.Password.Argon2Parallelism != 2 {
		t.Fatalf("password.argon2_parallelism default = %d", cfg.Auth.Password.Argon2Parallelism)
	}
	if cfg.Auth.RateLimit.LoginMaxAttempts != 5 {
		t.Fatalf("rate_limit.login_max_attempts default = %d", cfg.Auth.RateLimit.LoginMaxAttempts)
	}
	if cfg.Auth.RateLimit.LoginWindowSeconds != 900 {
		t.Fatalf("rate_limit.login_window_seconds default = %d", cfg.Auth.RateLimit.LoginWindowSeconds)
	}
	if cfg.Auth.Invitation.LifetimeSeconds != 604800 {
		t.Fatalf("invitation.lifetime_seconds default = %d", cfg.Auth.Invitation.LifetimeSeconds)
	}
}

func TestLoadConfigRespectsExplicitAuthValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
auth:
  session:
    cookie_name: "custom_session"
    lifetime_seconds: 3600
    secure_cookie: false
    domain: "example.com"
  password:
    argon2_memory_kib: 32768
    argon2_iterations: 2
    argon2_parallelism: 1
  rate_limit:
    login_max_attempts: 3
    login_window_seconds: 600
  invitation:
    lifetime_seconds: 86400
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.Session.CookieName != "custom_session" {
		t.Fatalf("cookie_name = %q", cfg.Auth.Session.CookieName)
	}
	if cfg.Auth.Session.LifetimeSeconds != 3600 {
		t.Fatalf("lifetime_seconds = %d", cfg.Auth.Session.LifetimeSeconds)
	}
	if cfg.Auth.Session.SecureCookie {
		t.Fatal("secure_cookie should be false")
	}
	if cfg.Auth.Session.Domain != "example.com" {
		t.Fatalf("domain = %q", cfg.Auth.Session.Domain)
	}
	if cfg.Auth.Password.Argon2MemoryKiB != 32768 {
		t.Fatalf("memory_kib = %d", cfg.Auth.Password.Argon2MemoryKiB)
	}
	if cfg.Auth.Invitation.LifetimeSeconds != 86400 {
		t.Fatalf("invitation lifetime = %d", cfg.Auth.Invitation.LifetimeSeconds)
	}
}

// TestLoadConfigAuthPartialOverrideRetainsDefaults 钉住 yaml.v3 的部分合并行为：
// 仅覆盖 auth.session.lifetime_seconds 时，其它安全敏感默认值必须保留，
// 防止未来回归把默认值悄悄清零。
func TestLoadConfigAuthPartialOverrideRetainsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
auth:
  session:
    lifetime_seconds: 3600
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// 覆盖项生效。
	if cfg.Auth.Session.LifetimeSeconds != 3600 {
		t.Fatalf("session.lifetime_seconds = %d, want 3600", cfg.Auth.Session.LifetimeSeconds)
	}
	// 其余认证默认值必须保留。
	if cfg.Auth.Session.CookieName != "langhuan_session" {
		t.Fatalf("session.cookie_name = %q, want %q", cfg.Auth.Session.CookieName, "langhuan_session")
	}
	if !cfg.Auth.Session.SecureCookie {
		t.Fatal("session.secure_cookie default should be retained as true")
	}
	if cfg.Auth.Session.Domain != "" {
		t.Fatalf("session.domain = %q, want empty", cfg.Auth.Session.Domain)
	}
	if cfg.Auth.Password.Argon2MemoryKiB != 65536 {
		t.Fatalf("password.argon2_memory_kib = %d, want 65536", cfg.Auth.Password.Argon2MemoryKiB)
	}
	if cfg.Auth.Password.Argon2Iterations != 3 {
		t.Fatalf("password.argon2_iterations = %d, want 3", cfg.Auth.Password.Argon2Iterations)
	}
	if cfg.Auth.Password.Argon2Parallelism != 2 {
		t.Fatalf("password.argon2_parallelism = %d, want 2", cfg.Auth.Password.Argon2Parallelism)
	}
	if cfg.Auth.RateLimit.LoginMaxAttempts != 5 {
		t.Fatalf("rate_limit.login_max_attempts = %d, want 5", cfg.Auth.RateLimit.LoginMaxAttempts)
	}
	if cfg.Auth.RateLimit.LoginWindowSeconds != 900 {
		t.Fatalf("rate_limit.login_window_seconds = %d, want 900", cfg.Auth.RateLimit.LoginWindowSeconds)
	}
	if cfg.Auth.Invitation.LifetimeSeconds != 604800 {
		t.Fatalf("invitation.lifetime_seconds = %d, want 604800", cfg.Auth.Invitation.LifetimeSeconds)
	}
}

func TestLoadConfigRejectsNonPositiveAuthParams(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	base := `
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
`
	cases := []struct {
		name     string
		authYAML string
	}{
		{name: "session lifetime", authYAML: "auth:\n  session:\n    lifetime_seconds: 0\n"},
		{name: "argon2 memory", authYAML: "auth:\n  password:\n    argon2_memory_kib: 0\n"},
		{name: "argon2 iterations", authYAML: "auth:\n  password:\n    argon2_iterations: 0\n"},
		{name: "argon2 parallelism", authYAML: "auth:\n  password:\n    argon2_parallelism: 0\n"},
		{name: "login max attempts", authYAML: "auth:\n  rate_limit:\n    login_max_attempts: 0\n"},
		{name: "login window", authYAML: "auth:\n  rate_limit:\n    login_window_seconds: -1\n"},
		{name: "invitation lifetime", authYAML: "auth:\n  invitation:\n    lifetime_seconds: 0\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(base+tt.authYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("expected non-positive auth param error for %s", tt.name)
			}
		})
	}
}

func TestDefaultStorageAndMinerUConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Storage.Driver != "local" {
		t.Fatalf("storage.driver default = %q, want local", cfg.Storage.Driver)
	}
	if cfg.Storage.Assets.MaxCountPerDocument != 500 {
		t.Fatalf("assets.max_count_per_document = %d", cfg.Storage.Assets.MaxCountPerDocument)
	}
	if cfg.Storage.Assets.MaxImageSizeBytes != 10*1024*1024 {
		t.Fatalf("assets.max_image_size_bytes = %d", cfg.Storage.Assets.MaxImageSizeBytes)
	}
	wantMimes := []string{"image/png", "image/jpeg", "image/webp", "image/gif"}
	if len(cfg.Storage.Assets.AllowedMimeTypes) != len(wantMimes) {
		t.Fatalf("assets.allowed_mime_types = %v", cfg.Storage.Assets.AllowedMimeTypes)
	}
	if cfg.MinerU.Enabled {
		t.Fatal("mineru.enabled should default to false")
	}
	if cfg.MinerU.ModelVersion != "vlm" {
		t.Fatalf("mineru.model_version = %q", cfg.MinerU.ModelVersion)
	}
	if cfg.MinerU.PollIntervalSeconds != 10 || cfg.MinerU.MaxPollAttempts != 180 {
		t.Fatalf("mineru poll config = %#v", cfg.MinerU)
	}
}

func TestLoadV070S3AndMinerUConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
storage:
  driver: s3
  raw_document_dir: "./data/raw"
  s3:
    endpoint: "http://127.0.0.1:19000"
    region: "us-east-1"
    bucket: "langhuan-test"
    access_key: "rustfsadmin"
    secret_key: "rustfsadmin"
    force_path_style: true
    public_base_url: "http://127.0.0.1:19000/langhuan-test"
  assets:
    max_count_per_document: 200
    max_image_size_bytes: 5242880
    allowed_mime_types: ["image/png", "image/jpeg"]
mineru:
  enabled: true
  model_version: "pipeline"
  poll_interval_seconds: 15
  max_poll_attempts: 100
  upload_timeout_seconds: 60
  result_download_timeout_seconds: 90
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Storage.Driver != "s3" {
		t.Fatalf("storage.driver = %q", cfg.Storage.Driver)
	}
	if cfg.Storage.S3.Bucket != "langhuan-test" {
		t.Fatalf("storage.s3.bucket = %q", cfg.Storage.S3.Bucket)
	}
	if cfg.Storage.Assets.MaxCountPerDocument != 200 {
		t.Fatalf("assets.max_count_per_document = %d", cfg.Storage.Assets.MaxCountPerDocument)
	}
	if cfg.MinerU.Enabled != true {
		t.Fatal("mineru.enabled = false")
	}
	if cfg.MinerU.ModelVersion != "pipeline" {
		t.Fatalf("mineru.model_version = %q", cfg.MinerU.ModelVersion)
	}
	if cfg.MinerU.UploadTimeoutSeconds != 60 {
		t.Fatalf("mineru.upload_timeout_seconds = %d", cfg.MinerU.UploadTimeoutSeconds)
	}
}

func TestValidateStorageRejectsInvalidDriver(t *testing.T) {
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Credentials.EncryptionKey = testEncryptionKey
	cfg.Storage.Driver = "gcs"
	if err := cfg.validate(); err == nil {
		t.Fatal("expected invalid storage.driver error")
	}
}

func TestValidateStorageS3RequiresCredentials(t *testing.T) {
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Credentials.EncryptionKey = testEncryptionKey
	cfg.Storage.Driver = "s3"
	cfg.Storage.S3.Endpoint = "http://127.0.0.1:19000"
	// bucket/access_key/secret_key 缺失
	if err := cfg.validate(); err == nil {
		t.Fatal("expected missing s3 credentials error")
	}
	cfg.Storage.S3.Bucket = "test"
	cfg.Storage.S3.AccessKey = "ak"
	cfg.Storage.S3.SecretKey = "sk"
	if err := cfg.validate(); err != nil {
		t.Fatalf("valid s3 config rejected: %v", err)
	}
}

func TestAuthOIDCDefaults(t *testing.T) {
	cfg := defaultConfig()
	// password.enabled 默认 true（向后兼容）
	if !cfg.Auth.Password.Enabled {
		t.Fatal("auth.password.enabled should default to true for backward compatibility")
	}
	// OIDC 默认关闭
	if cfg.Auth.OIDC.Enabled {
		t.Fatal("auth.oidc.enabled should default to false")
	}
	// require_email_verified 默认 true
	if !cfg.Auth.OIDC.RequireEmailVerified {
		t.Fatal("auth.oidc.require_email_verified should default to true")
	}
	// state_ttl / http_timeout 有合理默认
	if cfg.Auth.OIDC.StateTTLSeconds != 300 {
		t.Fatalf("state_ttl_seconds = %d, want 300", cfg.Auth.OIDC.StateTTLSeconds)
	}
	if cfg.Auth.OIDC.HTTPTimeoutSeconds != 10 {
		t.Fatalf("http_timeout_seconds = %d, want 10", cfg.Auth.OIDC.HTTPTimeoutSeconds)
	}
}

// baseValidOIDCConfig 返回一个能通过 validate 的 OIDC 配置，便于逐项破坏。
func baseValidOIDCConfig() Config {
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	cfg.Credentials.EncryptionKey = testEncryptionKey
	cfg.Auth.Password.Enabled = false
	cfg.Auth.OIDC.Enabled = true
	cfg.Auth.OIDC.Issuer = "https://sso.example.com/realms/corp"
	cfg.Auth.OIDC.ClientID = "langhuan"
	cfg.Auth.OIDC.ClientSecret = "secret"
	cfg.Auth.OIDC.RedirectURL = "https://langhuan.example.com/api/v1/auth/oidc/callback"
	return cfg
}

func TestAuthOIDCValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "both disabled", mutate: func(c *Config) {
			c.Auth.Password.Enabled = false
			c.Auth.OIDC.Enabled = false
		}},
		{name: "oidc enabled missing issuer", mutate: func(c *Config) { c.Auth.OIDC.Issuer = "" }},
		{name: "oidc enabled missing client_id", mutate: func(c *Config) { c.Auth.OIDC.ClientID = "" }},
		{name: "oidc enabled missing client_secret", mutate: func(c *Config) { c.Auth.OIDC.ClientSecret = "" }},
		{name: "oidc enabled missing redirect_url", mutate: func(c *Config) { c.Auth.OIDC.RedirectURL = "" }},
		{name: "oidc issuer with userinfo", mutate: func(c *Config) { c.Auth.OIDC.Issuer = "https://user:pass@sso.example.com" }},
		{name: "oidc issuer not absolute", mutate: func(c *Config) { c.Auth.OIDC.Issuer = "sso.example.com" }},
		{name: "oidc redirect_url wrong path", mutate: func(c *Config) {
			c.Auth.OIDC.RedirectURL = "https://langhuan.example.com/api/v1/auth/oidc/wrong"
		}},
		{name: "oidc redirect_url with userinfo", mutate: func(c *Config) {
			c.Auth.OIDC.RedirectURL = "https://u:p@langhuan.example.com/api/v1/auth/oidc/callback"
		}},
		{name: "state_ttl_seconds zero", mutate: func(c *Config) { c.Auth.OIDC.StateTTLSeconds = 0 }},
		{name: "http_timeout_seconds zero", mutate: func(c *Config) { c.Auth.OIDC.HTTPTimeoutSeconds = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidOIDCConfig()
			// 先确认基线合法
			if err := cfg.validate(); err != nil {
				t.Fatalf("baseline config should be valid: %v", err)
			}
			tt.mutate(&cfg)
			if err := cfg.validate(); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestAuthPasswordEnabledBackwardCompatible(t *testing.T) {
	// 旧 config.yaml 不写 password.enabled 字段时，加载后默认 true。
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if !cfg.Auth.Password.Enabled {
		t.Fatal("legacy config without password.enabled should load as true")
	}
}
