package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New 创建结构化 JSON logger。
// level 为空或非法时回退到 info。
// 默认开启脱敏（redact=true），对 authorization/bearer/api_key/token/password/secret/dsn
// 等敏感字段做 mask，防止 API key、OIDC client_secret、数据库 DSN 等意外泄漏到日志。
func New(level string) *slog.Logger {
	return NewWithOpts(level, WithRedact())
}

// Option 配置 logger 行为。
type Option func(*loggerConfig)

type loggerConfig struct {
	redact bool
}

// WithRedact 启用敏感字段脱敏。无参数时默认启用；传入 false 可关闭（本地 debug）。
func WithRedact() Option {
	return func(c *loggerConfig) { c.redact = true }
}

// NewWithOpts 创建可配置的 logger。
func NewWithOpts(level string, opts ...Option) *slog.Logger {
	cfg := loggerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	handlerOpts := &slog.HandlerOptions{Level: slogLevel}
	if cfg.redact {
		handlerOpts.ReplaceAttr = redactAttr
	}
	handler := slog.NewJSONHandler(os.Stdout, handlerOpts)
	return slog.New(handler)
}

// sensitiveFieldNames 是需要脱敏的字段名（不区分大小写）。
var sensitiveFieldNames = []string{
	"authorization", "bearer", "api_key", "apikey", "token",
	"password", "passwd", "secret", "client_secret", "dsn",
}

// sensitiveValuePrefixes 是值内容含此前缀即视为敏感的标记。
var sensitiveValuePrefixes = []string{
	"Bearer ", "lhk_", // Workspace API key 前缀
}

// redactAttr 是 slog ReplaceAttr 回调，对敏感字段做 mask。
func redactAttr(groups []string, a slog.Attr) slog.Attr {
	// 只处理字符串 value 的字段；非字符串（如 nested group）原样返回。
	if a.Value.Kind() != slog.KindString {
		return a
	}
	key := strings.ToLower(a.Key)
	for _, name := range sensitiveFieldNames {
		if key == name || strings.Contains(key, name) {
			return slog.String(a.Key, "***")
		}
	}
	// 值内容匹配（Bearer/lhk_ 前缀）。
	val := a.Value.String()
	for _, prefix := range sensitiveValuePrefixes {
		if strings.HasPrefix(val, prefix) {
			return slog.String(a.Key, "***")
		}
	}
	return a
}
