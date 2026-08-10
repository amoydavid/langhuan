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

func TestV080Defaults(t *testing.T) {
	cfg := defaultConfig()

	// 日志脱敏默认开启。
	if !cfg.Log.Redact {
		t.Fatal("log.redact should default to true")
	}

	// 队列治理默认值。
	if cfg.Queue.Concurrency != 8 {
		t.Fatalf("queue.concurrency = %d, want 8", cfg.Queue.Concurrency)
	}
	if cfg.Queue.Retry.MaxAttempts != 5 || cfg.Queue.Retry.MinBackoffSeconds != 30 || cfg.Queue.Retry.MaxBackoffSeconds != 3600 {
		t.Fatalf("queue.retry defaults = %#v", cfg.Queue.Retry)
	}
	if cfg.Queue.TaskTimeoutSeconds != 1800 {
		t.Fatalf("queue.task_timeout_seconds = %d, want 1800", cfg.Queue.TaskTimeoutSeconds)
	}
	if cfg.Queue.RetentionSeconds != 86400 {
		t.Fatalf("queue.retention_seconds = %d, want 86400", cfg.Queue.RetentionSeconds)
	}

	// embedding 限制。
	if cfg.Embedding.BatchSize != 64 || cfg.Embedding.MaxConcurrency != 4 {
		t.Fatalf("embedding defaults = %#v", cfg.Embedding)
	}

	// chunk 上限。
	if cfg.Ingest.MaxChunksPerDocument != 50000 {
		t.Fatalf("ingest.max_chunks_per_document = %d, want 50000", cfg.Ingest.MaxChunksPerDocument)
	}

	// 可观测性默认值。
	if !cfg.Observability.Metrics.Enabled {
		t.Fatal("observability.metrics.enabled should default to true")
	}
	if cfg.Observability.Metrics.Path != "/metrics" {
		t.Fatalf("observability.metrics.path = %q, want /metrics", cfg.Observability.Metrics.Path)
	}
	if cfg.Observability.Readiness.QueuePendingThreshold != 80 {
		t.Fatalf("readiness.queue_pending_threshold = %d, want 80", cfg.Observability.Readiness.QueuePendingThreshold)
	}
	// traces 默认启用，全采样。
	if !cfg.Observability.Traces.Enabled || cfg.Observability.Traces.SampleRate != 1.0 {
		t.Fatalf("traces defaults = %#v", cfg.Observability.Traces)
	}
	// OTLP 默认关闭，protocol 默认 grpc。
	if cfg.Observability.OTLP.Enabled {
		t.Fatal("otlp.enabled should default to false")
	}
	if cfg.Observability.OTLP.Protocol != "grpc" {
		t.Fatalf("otlp.protocol = %q, want grpc", cfg.Observability.OTLP.Protocol)
	}

	// helper 方法。
	if cfg.Queue.TaskTimeout() != 30*time.Minute {
		t.Fatalf("TaskTimeout = %v, want 30m", cfg.Queue.TaskTimeout())
	}
	if cfg.Queue.Retention() != 24*time.Hour {
		t.Fatalf("Retention = %v, want 24h", cfg.Queue.Retention())
	}
	if cfg.Queue.MinBackoff() != 30*time.Second {
		t.Fatalf("MinBackoff = %v, want 30s", cfg.Queue.MinBackoff())
	}
	if cfg.Queue.MaxBackoff() != time.Hour {
		t.Fatalf("MaxBackoff = %v, want 1h", cfg.Queue.MaxBackoff())
	}
	// MaxRetry = MaxAttempts - 1（asynq 的 MaxRetry 不含首次执行）。
	if cfg.Queue.MaxRetry() != 4 {
		t.Fatalf("MaxRetry = %d, want 4", cfg.Queue.MaxRetry())
	}
}

func TestV080QueueMaxRetryEdgeCases(t *testing.T) {
	// max_attempts=1 表示只执行一次不重试，asynq MaxRetry 应为 0。
	cfg := defaultConfig()
	cfg.Queue.Retry.MaxAttempts = 1
	if cfg.Queue.MaxRetry() != 0 {
		t.Fatalf("MaxRetry for max_attempts=1 = %d, want 0", cfg.Queue.MaxRetry())
	}

	// 零值回退到默认 max_attempts=5 → MaxRetry=4。
	cfg2 := defaultConfig()
	cfg2.Queue.Retry.MaxAttempts = 0
	cfg2.applyDefaults()
	if cfg2.Queue.MaxRetry() != 4 {
		t.Fatalf("MaxRetry after applyDefaults for zero max_attempts = %d, want 4", cfg2.Queue.MaxRetry())
	}
}

func TestV080LoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
log:
  level: debug
  redact: false
queue:
  concurrency: 16
  retry:
    max_attempts: 3
    min_backoff_seconds: 10
    max_backoff_seconds: 600
  task_timeout_seconds: 900
  retention_seconds: 3600
embedding:
  batch_size: 32
  max_concurrency: 2
ingest:
  max_file_size_bytes: 104857600
  max_chunks_per_document: 10000
observability:
  metrics:
    enabled: false
    path: /custom-metrics
  traces:
    enabled: false
    sample_rate: 0.5
  readiness:
    queue_pending_threshold: 200
  otlp:
    enabled: true
    endpoint: collector:4317
    protocol: grpc
    insecure: true
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// 显式 redact: false 应生效（不是被默认 true 覆盖）。
	if cfg.Log.Redact {
		t.Fatal("log.redact should be false from yaml")
	}
	if cfg.Queue.Concurrency != 16 || cfg.Queue.Retry.MaxAttempts != 3 ||
		cfg.Queue.Retry.MinBackoffSeconds != 10 || cfg.Queue.Retry.MaxBackoffSeconds != 600 {
		t.Fatalf("queue config = %#v", cfg.Queue)
	}
	if cfg.Queue.TaskTimeoutSeconds != 900 || cfg.Queue.RetentionSeconds != 3600 {
		t.Fatalf("queue timeout/retention = %#v", cfg.Queue)
	}
	if cfg.Embedding.BatchSize != 32 || cfg.Embedding.MaxConcurrency != 2 {
		t.Fatalf("embedding config = %#v", cfg.Embedding)
	}
	if cfg.Ingest.MaxChunksPerDocument != 10000 {
		t.Fatalf("max_chunks_per_document = %d, want 10000", cfg.Ingest.MaxChunksPerDocument)
	}
	if cfg.Observability.Metrics.Enabled {
		t.Fatal("metrics.enabled should be false")
	}
	if cfg.Observability.Metrics.Path != "/custom-metrics" {
		t.Fatalf("metrics.path = %q, want /custom-metrics", cfg.Observability.Metrics.Path)
	}
	if cfg.Observability.Readiness.QueuePendingThreshold != 200 {
		t.Fatalf("readiness threshold = %d, want 200", cfg.Observability.Readiness.QueuePendingThreshold)
	}
	if cfg.Observability.Traces.Enabled || cfg.Observability.Traces.SampleRate != 0.5 {
		t.Fatalf("traces config = %#v", cfg.Observability.Traces)
	}
	if !cfg.Observability.OTLP.Enabled || cfg.Observability.OTLP.Endpoint != "collector:4317" ||
		cfg.Observability.OTLP.Protocol != "grpc" || !cfg.Observability.OTLP.Insecure {
		t.Fatalf("otlp config = %#v", cfg.Observability.OTLP)
	}
	// MaxRetry = 3 - 1 = 2。
	if cfg.Queue.MaxRetry() != 2 {
		t.Fatalf("MaxRetry = %d, want 2", cfg.Queue.MaxRetry())
	}
}

func TestV080LogRedactDefaultTrueWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// 只写 level，不写 redact —— 应保留默认 true。
	content := appendTestCredentials([]byte(`
database:
  dsn: "postgres://localhost:5432/langhuan?sslmode=disable"
log:
  level: info
`))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Log.Redact {
		t.Fatal("log.redact should default to true when omitted")
	}
}

func TestV080QueueValidation(t *testing.T) {
	valid := defaultConfig()
	valid.Database.DSN = "postgres://localhost:5432/langhuan?sslmode=disable"
	valid.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x22}, 32))

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{name: "concurrency zero", mutate: func(c *Config) { c.Queue.Concurrency = 0 }, wantSub: "queue.concurrency"},
		{name: "max_attempts zero", mutate: func(c *Config) { c.Queue.Retry.MaxAttempts = 0 }, wantSub: "queue.retry.max_attempts"},
		{name: "min_backoff zero", mutate: func(c *Config) { c.Queue.Retry.MinBackoffSeconds = 0 }, wantSub: "queue.retry.min_backoff_seconds"},
		{name: "max < min backoff", mutate: func(c *Config) {
			c.Queue.Retry.MinBackoffSeconds = 100
			c.Queue.Retry.MaxBackoffSeconds = 50
		}, wantSub: "queue.retry.max_backoff_seconds"},
		{name: "task_timeout zero", mutate: func(c *Config) { c.Queue.TaskTimeoutSeconds = 0 }, wantSub: "queue.task_timeout_seconds"},
		{name: "retention zero", mutate: func(c *Config) { c.Queue.RetentionSeconds = 0 }, wantSub: "queue.retention_seconds"},
		{name: "otlp enabled no endpoint", mutate: func(c *Config) {
			c.Observability.OTLP.Enabled = true
			c.Observability.OTLP.Endpoint = ""
		}, wantSub: "endpoint"},
		{name: "otlp bad protocol", mutate: func(c *Config) {
			c.Observability.OTLP.Protocol = "weird"
		}, wantSub: "otlp.protocol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid
			tt.mutate(&c)
			err := c.validate()
			if err == nil {
				t.Fatal("validate error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("validate error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestV080ApplyDefaultsFillsZeroQueue(t *testing.T) {
	// 空白 queue 段经 applyDefaults 后应有完整默认值。
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Queue.Concurrency != 8 || cfg.Queue.Retry.MaxAttempts != 5 ||
		cfg.Queue.TaskTimeoutSeconds != 1800 || cfg.Queue.RetentionSeconds != 86400 {
		t.Fatalf("applyDefaults queue = %#v", cfg.Queue)
	}
	if cfg.Embedding.BatchSize != 64 || cfg.Embedding.MaxConcurrency != 4 {
		t.Fatalf("applyDefaults embedding = %#v", cfg.Embedding)
	}
	if cfg.Ingest.MaxChunksPerDocument != 50000 {
		t.Fatalf("applyDefaults max_chunks = %d", cfg.Ingest.MaxChunksPerDocument)
	}
	if cfg.Observability.Metrics.Path != "/metrics" {
		t.Fatalf("applyDefaults metrics path = %q", cfg.Observability.Metrics.Path)
	}
}
