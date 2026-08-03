package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Redis       RedisConfig       `yaml:"redis"`
	Log         LogConfig         `yaml:"log"`
	Storage     StorageConfig     `yaml:"storage"`
	Ingest      IngestConfig      `yaml:"ingest"`
	Auth        AuthConfig        `yaml:"auth"`
	Credentials CredentialsConfig `yaml:"credentials"`
	Retrieval   RetrievalConfig   `yaml:"retrieval"`
	APIKey      APIKeyConfig      `yaml:"api_key"`
	MCP         MCPConfig         `yaml:"mcp"`
	Search      SearchConfig      `yaml:"search"`
}

type ServerConfig struct {
	HTTPAddr  string `yaml:"http_addr"`
	BaseURL   string `yaml:"base_url"`
	RunHTTP   bool   `yaml:"run_http"`
	RunWorker bool   `yaml:"run_worker"`
}

// APIKeyConfig 描述 Workspace API Key 的生命周期与限流参数。
type APIKeyConfig struct {
	DefaultLifetimeSeconds       int `yaml:"default_lifetime_seconds"`
	MaxLifetimeSeconds           int `yaml:"max_lifetime_seconds"`
	LastUsedTouchIntervalSeconds int `yaml:"last_used_touch_interval_seconds"`
	ActiveLimit                  int `yaml:"active_limit"`
}

// MCPConfig 描述 MCP transport 层的程序化访问约束。
type MCPConfig struct {
	InlineIngestMaxFileSizeBytes int64 `yaml:"inline_ingest_max_file_size_bytes"`
}

// SearchConfig 描述多知识库检索的并发与合并参数。
type SearchConfig struct {
	MultiKnowledgeBaseLimit int `yaml:"multi_knowledge_base_limit"`
	MultiConcurrency        int `yaml:"multi_concurrency"`
	MultiMergeRRFK          int `yaml:"multi_merge_rrf_k"`
}

type DatabaseConfig struct {
	Driver      string `yaml:"driver"`
	DSN         string `yaml:"dsn"`
	AutoMigrate bool   `yaml:"auto_migrate"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type StorageConfig struct {
	RawDocumentDir string `yaml:"raw_document_dir"`
}

type IngestConfig struct {
	MaxFileSizeBytes int64    `yaml:"max_file_size_bytes"`
	AllowedFileTypes []string `yaml:"allowed_file_types"`
}

// RetrievalConfig controls bounded cleanup of rebuildable search projections.
type RetrievalConfig struct {
	FailedStagingRetention     time.Duration `yaml:"failed_staging_retention"`
	RetiredGenerationRetention time.Duration `yaml:"retired_generation_retention"`
	CleanupBatchSize           int           `yaml:"cleanup_batch_size"`
}

// CredentialsConfig 描述持久化敏感凭证所使用的主密钥。
type CredentialsConfig struct {
	EncryptionKey string `yaml:"encryption_key"`
}

// DecodeEncryptionKey 解码并校验 AES-256 所需的 32-byte key。
func (c CredentialsConfig) DecodeEncryptionKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(c.EncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("credentials.encryption_key 必须是 Base64 编码的 32 字节密钥")
	}
	return key, nil
}

// AuthConfig 汇总认证相关配置：会话、密码哈希、登录限流、邀请。
type AuthConfig struct {
	Session    SessionConfig    `yaml:"session"`
	Password   PasswordConfig   `yaml:"password"`
	RateLimit  RateLimitConfig  `yaml:"rate_limit"`
	Invitation InvitationConfig `yaml:"invitation"`
}

// SessionConfig 描述会话 cookie 相关参数。
type SessionConfig struct {
	CookieName      string `yaml:"cookie_name"`
	LifetimeSeconds int    `yaml:"lifetime_seconds"`
	SecureCookie    bool   `yaml:"secure_cookie"`
	Domain          string `yaml:"domain"`
}

// PasswordConfig 描述 argon2id 哈希参数。
type PasswordConfig struct {
	Argon2MemoryKiB   uint32 `yaml:"argon2_memory_kib"`
	Argon2Iterations  uint32 `yaml:"argon2_iterations"`
	Argon2Parallelism uint8  `yaml:"argon2_parallelism"`
}

// RateLimitConfig 描述登录失败限流参数。
type RateLimitConfig struct {
	LoginMaxAttempts   int `yaml:"login_max_attempts"`
	LoginWindowSeconds int `yaml:"login_window_seconds"`
}

// InvitationConfig 描述邀请 token 的有效期。
type InvitationConfig struct {
	LifetimeSeconds int `yaml:"lifetime_seconds"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = "config.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	cfg := defaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析 YAML 配置失败: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			HTTPAddr:  ":8080",
			BaseURL:   "http://127.0.0.1:8080",
			RunHTTP:   true,
			RunWorker: true,
		},
		Database: DatabaseConfig{Driver: "postgres", AutoMigrate: true},
		Redis:    RedisConfig{Addr: "127.0.0.1:6379"},
		Log:      LogConfig{Level: "info"},
		Storage:  StorageConfig{RawDocumentDir: "./data/raw-documents"},
		Ingest: IngestConfig{
			MaxFileSizeBytes: 50 * 1024 * 1024,
			AllowedFileTypes: []string{
				"markdown",
				"md",
				"txt",
				"csv",
				"xlsx",
				"docx",
			},
		},
		Auth: defaultAuthConfig(),
		Retrieval: RetrievalConfig{
			FailedStagingRetention:     24 * time.Hour,
			RetiredGenerationRetention: 168 * time.Hour,
			CleanupBatchSize:           1000,
		},
		APIKey: defaultAPIKeyConfig(),
		MCP: MCPConfig{
			InlineIngestMaxFileSizeBytes: 8 * 1024 * 1024,
		},
		Search: defaultSearchConfig(),
	}
}

// defaultAPIKeyConfig 返回 Workspace API Key 的默认生命周期与上限。
func defaultAPIKeyConfig() APIKeyConfig {
	return APIKeyConfig{
		DefaultLifetimeSeconds:       7776000,  // 90 天
		MaxLifetimeSeconds:           31536000, // 365 天
		LastUsedTouchIntervalSeconds: 300,      // 5 分钟
		ActiveLimit:                  100,
	}
}

// defaultSearchConfig 返回多知识库检索的默认参数。
func defaultSearchConfig() SearchConfig {
	return SearchConfig{
		MultiKnowledgeBaseLimit: 20,
		MultiConcurrency:        4,
		MultiMergeRRFK:          60,
	}
}

// defaultAuthConfig 返回规格规定的认证默认值。
func defaultAuthConfig() AuthConfig {
	return AuthConfig{
		Session: SessionConfig{
			CookieName:      "langhuan_session",
			LifetimeSeconds: 604800,
			SecureCookie:    true,
			Domain:          "",
		},
		Password: PasswordConfig{
			Argon2MemoryKiB:   65536,
			Argon2Iterations:  3,
			Argon2Parallelism: 2,
		},
		RateLimit: RateLimitConfig{
			LoginMaxAttempts:   5,
			LoginWindowSeconds: 900,
		},
		Invitation: InvitationConfig{
			LifetimeSeconds: 604800,
		},
	}
}

func (c *Config) applyDefaults() {
	if c.Server.HTTPAddr == "" {
		c.Server.HTTPAddr = ":8080"
	}
	if c.Database.Driver == "" {
		c.Database.Driver = "postgres"
	}
	if c.Redis.Addr == "" {
		c.Redis.Addr = "127.0.0.1:6379"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Storage.RawDocumentDir == "" {
		c.Storage.RawDocumentDir = "./data/raw-documents"
	}
	if c.Ingest.MaxFileSizeBytes == 0 {
		c.Ingest.MaxFileSizeBytes = 50 * 1024 * 1024
	}
	if len(c.Ingest.AllowedFileTypes) == 0 {
		c.Ingest.AllowedFileTypes = []string{"markdown", "md", "txt", "csv", "xlsx", "docx"}
	}
	// MCP 内联导入上限不得大于全局导入上限；当用户调低全局上限但未显式
	// 配置 MCP 时，把 MCP 内联上限收敛到全局上限，避免默认 8 MiB 与较小
	// 全局上限冲突。
	if c.MCP.InlineIngestMaxFileSizeBytes == 0 || c.MCP.InlineIngestMaxFileSizeBytes > c.Ingest.MaxFileSizeBytes {
		c.MCP.InlineIngestMaxFileSizeBytes = c.Ingest.MaxFileSizeBytes
	}
}

// 注：认证默认值在 defaultConfig() 中提供。yaml.Unmarshal 在 auth 块缺失时
// 会保留 defaultConfig 设置的值，因此此处无需为 auth 字段回填默认值；
// validate() 会拒绝所有非正认证参数。

func (c *Config) validate() error {
	baseURL, err := normalizeBaseURL(c.Server.BaseURL)
	if err != nil {
		return err
	}
	c.Server.BaseURL = baseURL
	if c.Database.DSN == "" {
		return errors.New("database.dsn 不能为空")
	}
	if c.Redis.Addr == "" {
		return errors.New("redis.addr 不能为空")
	}
	if c.Storage.RawDocumentDir == "" {
		return errors.New("storage.raw_document_dir 不能为空")
	}
	if c.Ingest.MaxFileSizeBytes <= 0 {
		return errors.New("ingest.max_file_size_bytes 必须大于 0")
	}
	if c.Retrieval.FailedStagingRetention <= 0 {
		return errors.New("retrieval.failed_staging_retention 必须大于 0")
	}
	if c.Retrieval.RetiredGenerationRetention <= 0 {
		return errors.New("retrieval.retired_generation_retention 必须大于 0")
	}
	if c.Retrieval.CleanupBatchSize < 1 || c.Retrieval.CleanupBatchSize > 10000 {
		return errors.New("retrieval.cleanup_batch_size 必须在 1 到 10000 之间")
	}
	if err := c.validateAuth(); err != nil {
		return err
	}
	if err := c.validateAPIKey(); err != nil {
		return err
	}
	if err := c.validateSearch(); err != nil {
		return err
	}
	if err := c.validateMCP(); err != nil {
		return err
	}
	if _, err := c.Credentials.DecodeEncryptionKey(); err != nil {
		return err
	}
	return nil
}

func normalizeBaseURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("server.base_url 不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("server.base_url 无效: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("server.base_url 必须是绝对 http/https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errors.New("server.base_url 不得包含用户信息、query 或 fragment")
	}
	return strings.TrimRight(raw, "/"), nil
}

// validateAPIKey 校验 Workspace API Key 的生命周期与上限参数。
func (c *Config) validateAPIKey() error {
	if c.APIKey.DefaultLifetimeSeconds <= 0 {
		return errors.New("api_key.default_lifetime_seconds 必须大于 0")
	}
	if c.APIKey.MaxLifetimeSeconds < c.APIKey.DefaultLifetimeSeconds {
		return errors.New("api_key.max_lifetime_seconds 不能小于 default_lifetime_seconds")
	}
	if c.APIKey.LastUsedTouchIntervalSeconds < 0 {
		return errors.New("api_key.last_used_touch_interval_seconds 不能为负")
	}
	if c.APIKey.ActiveLimit <= 0 {
		return errors.New("api_key.active_limit 必须大于 0")
	}
	return nil
}

// validateSearch 校验多知识库检索参数。
func (c *Config) validateSearch() error {
	if c.Search.MultiKnowledgeBaseLimit < 1 {
		return errors.New("search.multi_knowledge_base_limit 必须大于等于 1")
	}
	if c.Search.MultiConcurrency < 1 {
		return errors.New("search.multi_concurrency 必须大于等于 1")
	}
	if c.Search.MultiMergeRRFK <= 0 {
		return errors.New("search.multi_merge_rrf_k 必须大于 0")
	}
	return nil
}

// validateMCP 校验 MCP transport 参数。
func (c *Config) validateMCP() error {
	if c.MCP.InlineIngestMaxFileSizeBytes <= 0 {
		return errors.New("mcp.inline_ingest_max_file_size_bytes 必须大于 0")
	}
	if c.MCP.InlineIngestMaxFileSizeBytes > c.Ingest.MaxFileSizeBytes {
		return errors.New("mcp.inline_ingest_max_file_size_bytes 不能超过 ingest.max_file_size_bytes")
	}
	return nil
}

// validateAuth 校验所有认证参数为正数。
func (c *Config) validateAuth() error {
	if c.Auth.Session.LifetimeSeconds <= 0 {
		return errors.New("auth.session.lifetime_seconds 必须大于 0")
	}
	if c.Auth.Password.Argon2MemoryKiB == 0 {
		return errors.New("auth.password.argon2_memory_kib 必须大于 0")
	}
	if c.Auth.Password.Argon2Iterations == 0 {
		return errors.New("auth.password.argon2_iterations 必须大于 0")
	}
	if c.Auth.Password.Argon2Parallelism == 0 {
		return errors.New("auth.password.argon2_parallelism 必须大于 0")
	}
	if c.Auth.RateLimit.LoginMaxAttempts <= 0 {
		return errors.New("auth.rate_limit.login_max_attempts 必须大于 0")
	}
	if c.Auth.RateLimit.LoginWindowSeconds <= 0 {
		return errors.New("auth.rate_limit.login_window_seconds 必须大于 0")
	}
	if c.Auth.Invitation.LifetimeSeconds <= 0 {
		return errors.New("auth.invitation.lifetime_seconds 必须大于 0")
	}
	return nil
}
