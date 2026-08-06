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
	MinerU      MinerUConfig      `yaml:"mineru"`
	Auth        AuthConfig        `yaml:"auth"`
	Credentials CredentialsConfig `yaml:"credentials"`
	Retrieval   RetrievalConfig   `yaml:"retrieval"`
	APIKey      APIKeyConfig      `yaml:"api_key"`
	MCP         MCPConfig         `yaml:"mcp"`
	Search      SearchConfig      `yaml:"search"`
	SourceSync  SourceSyncConfig  `yaml:"source_sync"`
}

// SourceSyncConfig 描述飞书同步 Meta Scheduler 的非敏感运行参数。
type SourceSyncConfig struct {
	// SchedulerIntervalSeconds 是 Meta Scheduler 的扫描周期（秒）。
	// Meta Scheduler 是常驻后台 goroutine，每隔这个周期查一次"到期该同步"
	// 的飞书知识库（next_sync_at <= now）并入队。注意它决定的是到期检查的
	// 频率（定时同步的最大延迟精度），不是同步频率本身——同步频率由每个
	// 知识库的 source_config.cron 决定。调小则到期项更快入队但每周期多一次
	// 数据库扫描；调大则到期项最多多等一个周期才被入队。
	SchedulerIntervalSeconds int `yaml:"scheduler_interval_seconds"`
	// MaxConcurrentPerConnection 是每个飞书应用（source connection）同时
	// 最多运行的同步任务数（pending/running）。超额的到期项排队等待——
	// 同一应用下的某个同步任务完成后会立即续跑同应用队列里的下一项，
	// 无需等下一个扫描周期。这是按应用限流、避免单应用并发拉取触发飞书限流的核心旋钮。
	MaxConcurrentPerConnection int `yaml:"max_concurrent_per_connection"`
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

// StorageConfig 描述原始文件、解析产物与图片资产的存储后端。
// Driver 为 "local" 时使用本地文件系统（RawDocumentDir）；为 "s3" 时使用
// S3-compatible 对象存储（RustFS / MinIO / 阿里云 / 腾讯云等）。
type StorageConfig struct {
	Driver         string              `yaml:"driver"`
	RawDocumentDir string              `yaml:"raw_document_dir"`
	S3             S3Config            `yaml:"s3"`
	Assets         AssetsStorageConfig `yaml:"assets"`
}

// S3Config 描述 S3-compatible 对象存储连接参数。
type S3Config struct {
	Endpoint       string `yaml:"endpoint"`
	Region         string `yaml:"region"`
	Bucket         string `yaml:"bucket"`
	AccessKey      string `yaml:"access_key"`
	SecretKey      string `yaml:"secret_key"`
	ForcePathStyle bool   `yaml:"force_path_style"`
	PublicBaseURL  string `yaml:"public_base_url"`
}

// AssetsStorageConfig 描述解析图片资产的归档限制。
type AssetsStorageConfig struct {
	MaxCountPerDocument int      `yaml:"max_count_per_document"`
	MaxImageSizeBytes   int64    `yaml:"max_image_size_bytes"`
	AllowedMimeTypes    []string `yaml:"allowed_mime_types"`
}

// MinerUConfig 描述 MinerU Cloud PDF 解析的非敏感运行参数。
// MinerU token 属于敏感凭证，保存在 model_providers 表（加密），不写入 YAML。
type MinerUConfig struct {
	Enabled                      bool   `yaml:"enabled"`
	ModelVersion                 string `yaml:"model_version"`
	PollIntervalSeconds          int    `yaml:"poll_interval_seconds"`
	MaxPollAttempts              int    `yaml:"max_poll_attempts"`
	UploadTimeoutSeconds         int    `yaml:"upload_timeout_seconds"`
	ResultDownloadTimeoutSeconds int    `yaml:"result_download_timeout_seconds"`
	MaxZipImageBytes             int64  `yaml:"max_zip_image_bytes"`
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
		Storage: StorageConfig{
			Driver:         "local",
			RawDocumentDir: "./data/raw-documents",
			Assets: AssetsStorageConfig{
				MaxCountPerDocument: 500,
				MaxImageSizeBytes:   10 * 1024 * 1024,
				AllowedMimeTypes:    []string{"image/png", "image/jpeg", "image/webp", "image/gif"},
			},
		},
		Ingest: IngestConfig{
			MaxFileSizeBytes: 50 * 1024 * 1024,
			AllowedFileTypes: []string{
				"pdf",
				"markdown",
				"md",
				"txt",
				"csv",
				"xlsx",
				"docx",
			},
		},
		MinerU: defaultMinerUConfig(),
		Auth:   defaultAuthConfig(),
		Retrieval: RetrievalConfig{
			FailedStagingRetention:     24 * time.Hour,
			RetiredGenerationRetention: 168 * time.Hour,
			CleanupBatchSize:           1000,
		},
		APIKey: defaultAPIKeyConfig(),
		MCP: MCPConfig{
			InlineIngestMaxFileSizeBytes: 8 * 1024 * 1024,
		},
		Search:     defaultSearchConfig(),
		SourceSync: defaultSourceSyncConfig(),
	}
}

// defaultSourceSyncConfig 返回来源同步 Meta Scheduler 的默认运行参数。
func defaultSourceSyncConfig() SourceSyncConfig {
	return SourceSyncConfig{
		SchedulerIntervalSeconds:   60,
		MaxConcurrentPerConnection: 2,
	}
}

// defaultMinerUConfig 返回 MinerU Cloud PDF 解析的默认运行参数。
// 默认禁用：启用 MinerU 前必须先在 model_providers 表写入有效 token。
func defaultMinerUConfig() MinerUConfig {
	return MinerUConfig{
		Enabled:                      false,
		ModelVersion:                 "vlm",
		PollIntervalSeconds:          10,
		MaxPollAttempts:              180,
		UploadTimeoutSeconds:         120,
		ResultDownloadTimeoutSeconds: 120,
		MaxZipImageBytes:             20 * 1024 * 1024, // 20MB
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
	if c.Storage.Driver == "" {
		c.Storage.Driver = "local"
	}
	if c.Storage.Assets.MaxCountPerDocument == 0 {
		c.Storage.Assets.MaxCountPerDocument = 500
	}
	if c.Storage.Assets.MaxImageSizeBytes == 0 {
		c.Storage.Assets.MaxImageSizeBytes = 10 * 1024 * 1024
	}
	if len(c.Storage.Assets.AllowedMimeTypes) == 0 {
		c.Storage.Assets.AllowedMimeTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}
	}
	if c.MinerU.ModelVersion == "" {
		c.MinerU.ModelVersion = "vlm"
	}
	if c.MinerU.PollIntervalSeconds == 0 {
		c.MinerU.PollIntervalSeconds = 10
	}
	if c.MinerU.MaxPollAttempts == 0 {
		c.MinerU.MaxPollAttempts = 180
	}
	if c.MinerU.UploadTimeoutSeconds == 0 {
		c.MinerU.UploadTimeoutSeconds = 120
	}
	if c.MinerU.ResultDownloadTimeoutSeconds == 0 {
		c.MinerU.ResultDownloadTimeoutSeconds = 120
	}
	if c.MinerU.MaxZipImageBytes == 0 {
		c.MinerU.MaxZipImageBytes = 20 * 1024 * 1024
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
	if c.SourceSync.SchedulerIntervalSeconds <= 0 {
		c.SourceSync.SchedulerIntervalSeconds = 60
	}
	if c.SourceSync.MaxConcurrentPerConnection <= 0 {
		c.SourceSync.MaxConcurrentPerConnection = 2
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
	if err := c.validateStorage(); err != nil {
		return err
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
	if err := c.validateSourceSync(); err != nil {
		return err
	}
	if _, err := c.Credentials.DecodeEncryptionKey(); err != nil {
		return err
	}
	return nil
}

// validateSourceSync 校验来源同步 Meta Scheduler 参数。
func (c *Config) validateSourceSync() error {
	if c.SourceSync.SchedulerIntervalSeconds <= 0 {
		return errors.New("source_sync.scheduler_interval_seconds 必须大于 0")
	}
	if c.SourceSync.MaxConcurrentPerConnection <= 0 {
		return errors.New("source_sync.max_concurrent_per_connection 必须大于 0")
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

// validateStorage 校验存储后端配置：driver 必须是 local 或 s3；选择 s3 时
// 必须提供 endpoint/bucket/credentials。资产限制只在显式配置时校验范围。
func (c *Config) validateStorage() error {
	switch c.Storage.Driver {
	case "local":
		// 本地模式只需要 RawDocumentDir（已在主 validate 中校验）。
	case "s3":
		if c.Storage.S3.Endpoint == "" {
			return errors.New("storage.s3.endpoint 不能为空（driver=s3）")
		}
		if c.Storage.S3.Bucket == "" {
			return errors.New("storage.s3.bucket 不能为空（driver=s3）")
		}
		if c.Storage.S3.AccessKey == "" || c.Storage.S3.SecretKey == "" {
			return errors.New("storage.s3.access_key 与 secret_key 不能为空（driver=s3）")
		}
	default:
		return fmt.Errorf("storage.driver 必须是 local 或 s3，当前为 %q", c.Storage.Driver)
	}
	if c.Storage.Assets.MaxCountPerDocument < 0 {
		return errors.New("storage.assets.max_count_per_document 不能为负")
	}
	if c.Storage.Assets.MaxImageSizeBytes < 0 {
		return errors.New("storage.assets.max_image_size_bytes 不能为负")
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
