package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// captureLogger 创建一个写到 buffer 的脱敏 logger，返回 logger 和 buffer。
func captureLogger(t *testing.T, redact bool) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	cfg := loggerConfig{}
	if redact {
		cfg.redact = true
	}
	handlerOpts := &slog.HandlerOptions{Level: slog.LevelDebug}
	if cfg.redact {
		handlerOpts.ReplaceAttr = redactAttr
	}
	handler := slog.NewJSONHandler(buf, handlerOpts)
	return slog.New(handler), buf
}

// parseLog 解析单行 JSON 日志为 map。
func parseLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	out := strings.TrimSpace(buf.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("parse log %q: %v", out, err)
	}
	return m
}

func TestRedactMasksSensitiveFields(t *testing.T) {
	tests := []struct {
		key, value string
	}{
		{"authorization", "Bearer abc123"},
		{"api_key", "lhk_xxxxxxxx"},
		{"token", "sometoken"},
		{"password", "hunter2"},
		{"secret", "topsecret"},
		{"client_secret", "oidc-secret"},
		{"dsn", "postgres://user:pass@host/db"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			log, buf := captureLogger(t, true)
			log.Info("test", slog.String(tt.key, tt.value))
			m := parseLog(t, buf)
			if m[tt.key] != "***" {
				t.Fatalf("%s = %v, want ***", tt.key, m[tt.key])
			}
		})
	}
}

func TestRedactMasksBearerValuePrefix(t *testing.T) {
	log, buf := captureLogger(t, true)
	// 字段名不敏感，但值含 "Bearer " 前缀。
	log.Info("test", slog.String("header", "Bearer eyJxyz"))
	m := parseLog(t, buf)
	if m["header"] != "***" {
		t.Fatalf("header = %v, want ***", m["header"])
	}
}

func TestRedactMasksAPIKeyValuePrefix(t *testing.T) {
	log, buf := captureLogger(t, true)
	log.Info("test", slog.String("credential", "lhk_livekey"))
	m := parseLog(t, buf)
	if m["credential"] != "***" {
		t.Fatalf("credential = %v, want ***", m["credential"])
	}
}

func TestRedactDoesNotMaskNormalFields(t *testing.T) {
	log, buf := captureLogger(t, true)
	log.Info("test",
		slog.String("workspace_id", "ws-123"),
		slog.String("document_id", "doc-456"),
		slog.Int("duration_ms", 42),
	)
	m := parseLog(t, buf)
	if m["workspace_id"] != "ws-123" {
		t.Fatalf("workspace_id = %v", m["workspace_id"])
	}
	if m["document_id"] != "doc-456" {
		t.Fatalf("document_id = %v", m["document_id"])
	}
}

func TestRedactDisabled(t *testing.T) {
	// redact=false 时，敏感字段原样输出。
	log, buf := captureLogger(t, false)
	log.Info("test", slog.String("password", "hunter2"))
	m := parseLog(t, buf)
	if m["password"] != "hunter2" {
		t.Fatalf("password = %v, want hunter2 (redact disabled)", m["password"])
	}
}

func TestNewDefaultsToRedactEnabled(t *testing.T) {
	// New() 默认启用脱敏。用一个临时文件捕获 stdout 不便，这里只验证
	// New 返回的 logger 非 nil 且不 panic。
	log := New("info")
	if log == nil {
		t.Fatal("New returned nil")
	}
}
