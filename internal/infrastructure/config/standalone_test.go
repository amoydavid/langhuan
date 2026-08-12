package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeKeyEnsure 返回一个 keyEnsure，它在 dir 下生成一个有效的 credential.key
// （模拟 datadir.Dir.EnsureCredentialKey 的行为，避免本包依赖 datadir）。
func fakeKeyEnsure(dir string) func() ([]byte, error) {
	return func() ([]byte, error) {
		key := make([]byte, 32)
		for i := range key {
			key[i] = byte(i)
		}
		path := filepath.Join(dir, "credential.key")
		if raw, err := os.ReadFile(path); err == nil {
			return decodeBase64Key(strings.TrimSpace(string(raw)))
		}
		if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
			return nil, err
		}
		return key, nil
	}
}

func TestStandaloneProfileContainsExpectedFields(t *testing.T) {
	t.Parallel()
	c := StandaloneProfile("/home/u/.langhuan-data")
	if c.Database.Driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", c.Database.Driver)
	}
	if c.Redis.Enabled {
		t.Fatal("redis.enabled 应为 false")
	}
	if c.Auth.Session.SecureCookie {
		t.Fatal("localhost 明文 HTTP 下 secure_cookie 应为 false")
	}
	if c.Auth.OIDC.Enabled {
		t.Fatal("oidc.enabled 应为 false")
	}
	if c.Credentials.EncryptionKey != "" {
		t.Fatal("standalone profile 不应内联 encryption_key（密钥只通过 encryption_key_file 引用）")
	}
	if c.Credentials.EncryptionKeyFile != "/home/u/.langhuan-data/credential.key" {
		t.Fatalf("encryption_key_file = %q", c.Credentials.EncryptionKeyFile)
	}
	if !strings.Contains(c.Database.DSN, "langhuan.db") {
		t.Fatalf("dsn = %q", c.Database.DSN)
	}
}

func TestStandaloneProfilePassesValidate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := StandaloneProfile(dir)
	// 写出 credential.key 让 encryption_key_file 可解析
	zeroKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if err := os.WriteFile(filepath.Join(dir, "credential.key"),
		[]byte(zeroKey), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.validate(); err != nil {
		t.Fatalf("standalone profile 应通过 validate: %v", err)
	}
}

func TestMaterializeStandaloneWritesYAMLWithKeyRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath, err := MaterializeStandalone(dir, fakeKeyEnsure(dir))
	if err != nil {
		t.Fatalf("MaterializeStandalone 失败: %v", err)
	}
	if cfgPath != filepath.Join(dir, "config.yaml") {
		t.Fatalf("cfgPath = %q", cfgPath)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config 权限 = %o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	checks := []struct{ name, want string }{
		{"driver sqlite", "driver: sqlite"},
		{"redis disabled", "enabled: false"},
		{"key file ref", filepath.Join(dir, "credential.key")},
		{"header comment", "自动生成"},
	}
	for _, ch := range checks {
		if !strings.Contains(content, ch.want) {
			t.Fatalf("config 内容缺少 %q\n---\n%s", ch.want, content)
		}
	}
	// encryption_key 应为空字符串（密钥不内联，只通过 encryption_key_file 引用）
	if !strings.Contains(content, "encryption_key: \"\"") {
		t.Fatalf("config 的 encryption_key 应为空，密钥只通过 encryption_key_file 引用:\n%s", content)
	}
}

func TestMaterializeStandaloneDoesNotOverwriteExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// 预置一个用户编辑过的 config
	if err := os.WriteFile(cfgPath, []byte("# user customized\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := MaterializeStandalone(dir, fakeKeyEnsure(dir))
	if err != nil {
		t.Fatalf("已存在 config 时应直接返回，got err: %v", err)
	}
	if got != cfgPath {
		t.Fatalf("path = %q", got)
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "user customized") {
		t.Fatal("已存在的 config 不应被覆盖")
	}
}

func TestMaterializeStandaloneGeneratedConfigLoadsBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath, err := MaterializeStandalone(dir, fakeKeyEnsure(dir))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("生成的 config 应能 Load 回来: %v", err)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("Load 后 driver = %q", cfg.Database.Driver)
	}
	if cfg.Redis.Enabled {
		t.Fatal("Load 后 redis.enabled 应为 false")
	}
}

// === Task 6: §2.4.3 删除组合矩阵 ===

func TestDeletionMatrixConfigRebuildableKeyNot(t *testing.T) {
	setup := func(t *testing.T) (dir, cfgPath, keyPath string) {
		dir = t.TempDir()
		keyPath = filepath.Join(dir, "credential.key")
		cfgPath, err := MaterializeStandalone(dir, fakeKeyEnsure(dir))
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	t.Run("only config deleted reuses key", func(t *testing.T) {
		dir, cfgPath, keyPath := setup(t)
		keyBefore, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(cfgPath); err != nil {
			t.Fatal(err)
		}
		// 再次 materialize：应复用已有 key，不重新生成
		if _, err := MaterializeStandalone(dir, fakeKeyEnsure(dir)); err != nil {
			t.Fatal(err)
		}
		keyAfter, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(keyBefore) != string(keyAfter) {
			t.Fatal("仅删 config.yaml 后 credential.key 必须复用，绝不重新生成")
		}
	})

	t.Run("only key deleted load_fails_fast", func(t *testing.T) {
		_, cfgPath, keyPath := setup(t)
		if err := os.Remove(keyPath); err != nil {
			t.Fatal(err)
		}
		// 已存在的 config 指向被删的 key → Load 时 ResolveEncryptionKey 读文件失败
		_, err := Load(cfgPath)
		if err == nil {
			t.Fatal("删 key 后 Load 应 fail-fast")
		}
	})

	t.Run("both deleted equals fresh env", func(t *testing.T) {
		dir, cfgPath, keyPath := setup(t)
		_ = cfgPath
		if err := os.Remove(filepath.Join(dir, "config.yaml")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(keyPath); err != nil {
			t.Fatal(err)
		}
		// 重新 materialize：全新生成
		if _, err := MaterializeStandalone(dir, fakeKeyEnsure(dir)); err != nil {
			t.Fatalf("全新环境应能重新生成: %v", err)
		}
		if _, err := os.Stat(keyPath); err != nil {
			t.Fatal("全新环境应生成 credential.key")
		}
		if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
			t.Fatal("全新环境应生成 config.yaml")
		}
	})
}
