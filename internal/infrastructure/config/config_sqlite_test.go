package config

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validBaselineConfig 返回一个能通过 validate 的默认配置，供 SQLite standalone
// 相关改动（Redis Enabled / encryption_key_file）的校验测试复用。
func validBaselineConfig() Config {
	cfg := defaultConfig()
	cfg.Database.DSN = "postgres://unused-in-baseline-test"
	cfg.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32))
	return cfg
}

func TestRedisEnabledDefaultsTrue(t *testing.T) {
	t.Parallel()
	cfg := defaultConfig()
	if !cfg.Redis.Enabled {
		t.Fatal("defaultConfig().Redis.Enabled 应默认 true（向后兼容旧 YAML 未写字段）")
	}
}

func TestRedisDisabledSkipsAddrRequirement(t *testing.T) {
	t.Parallel()
	cfg := validBaselineConfig()
	cfg.Redis.Enabled = false
	cfg.Redis.Addr = ""
	if err := cfg.validate(); err != nil {
		t.Fatalf("redis.enabled=false 时不应要求 addr，但 validate 报错: %v", err)
	}
}

func TestRedisEnabledRequiresAddr(t *testing.T) {
	t.Parallel()
	cfg := validBaselineConfig()
	cfg.Redis.Enabled = true
	cfg.Redis.Addr = ""
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "redis.addr") {
		t.Fatalf("redis.enabled=true 且 addr 为空应报错，got: %v", err)
	}
}

func TestEncryptionKeyFileLoadsFromKeyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "credential.key")
	want := bytes.Repeat([]byte{0x07}, 32)
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(want)), 0o600); err != nil {
		t.Fatal(err)
	}
	c := CredentialsConfig{EncryptionKeyFile: keyPath}
	got, err := c.ResolveEncryptionKey()
	if err != nil {
		t.Fatalf("ResolveEncryptionKey 失败: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("密钥不匹配: got %x want %x", got, want)
	}
}

func TestEncryptionKeyMutexWithEncryptionKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "credential.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))), 0o600); err != nil {
		t.Fatal(err)
	}
	c := CredentialsConfig{
		EncryptionKey:     base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		EncryptionKeyFile: keyPath,
	}
	_, err := c.ResolveEncryptionKey()
	if err == nil || !strings.Contains(err.Error(), "不能同时") {
		t.Fatalf("同时指定两个字段应报互斥错误，got: %v", err)
	}
}

func TestEncryptionKeyFileMissingFailsFast(t *testing.T) {
	t.Parallel()
	c := CredentialsConfig{EncryptionKeyFile: "/nonexistent/path/credential.key"}
	_, err := c.ResolveEncryptionKey()
	if err == nil {
		t.Fatal("key 文件不存在应 fail-fast")
	}
}

func TestEncryptionKeyFileCorruptFailsFast(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "credential.key")
	if err := os.WriteFile(keyPath, []byte("garbage-not-base64-32-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := CredentialsConfig{EncryptionKeyFile: keyPath}
	_, err := c.ResolveEncryptionKey()
	if err == nil {
		t.Fatal("损坏的 key 文件应 fail-fast")
	}
}

func TestEncryptionKeyFileValidates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "credential.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32))), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := validBaselineConfig()
	cfg.Credentials = CredentialsConfig{EncryptionKeyFile: keyPath}
	if err := cfg.validate(); err != nil {
		t.Fatalf("合法 encryption_key_file 应通过 validate: %v", err)
	}
}

// TestEncryptionKeyFileLegacyYAMLStaysEnabled 验证旧 YAML 未写 redis.enabled 时，
// 经 defaultConfig 预填 Enabled:true 后 Unmarshal 不覆盖（bool 零值问题）。
// 由于 defaultConfig 已设 Enabled:true，yaml.Unmarshal 到零值 Enabled 会覆盖为 false。
// 因此 Load 在 Unmarshal 前用 defaultConfig 作为 base，需要确认旧配置仍 Enabled=true。
func TestEncryptionKeyFileLegacyYAMLStaysEnabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// 旧 YAML：未写 redis.enabled，但写了 redis.addr
	content := appendTestCredentials([]byte("redis:\n  addr: localhost:6379\ndatabase:\n  dsn: postgres://x\n"))
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if !cfg.Redis.Enabled {
		t.Fatal("旧 YAML 未写 redis.enabled 时应保持 Enabled=true（向后兼容）")
	}
}
