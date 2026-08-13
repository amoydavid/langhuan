package datadir

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func newTestDir(t *testing.T) Dir {
	t.Helper()
	d := New(filepath.Join(t.TempDir(), ".langhuan-data"))
	if err := d.Ensure(); err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	return d
}

func TestResolveJoinsHomeDir(t *testing.T) {
	t.Parallel()
	d, err := Resolve(func() (string, error) { return "/tmp/fake-home", nil })
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if got := filepath.Clean(d.Path()); got != "/tmp/fake-home/.langhuan-data" {
		t.Fatalf("Path = %q", got)
	}
}

func TestResolveHomeFailureIsActionable(t *testing.T) {
	t.Parallel()
	_, err := Resolve(func() (string, error) { return "", errors.New("no home") })
	if err == nil {
		t.Fatal("主目录解析失败应返回错误")
	}
}

func TestEnsureCreatesDir(t *testing.T) {
	t.Parallel()
	d := New(filepath.Join(t.TempDir(), ".langhuan-data"))
	if err := d.Ensure(); err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	info, err := os.Stat(d.Path())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("权限 = %o, want 0700", info.Mode().Perm())
	}
}

func TestEnsureIdempotent(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	if err := d.Ensure(); err != nil { // 再次 Ensure
		t.Fatalf("重复 Ensure 失败: %v", err)
	}
}

func TestEnsureRejectsOverPermissiveExistingDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无 POSIX mode，跳过权限收紧测试")
	}
	t.Parallel()
	dir := filepath.Join(t.TempDir(), ".langhuan-data")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 0o755 可被 chmod 收紧到 0700，所以 Ensure 应成功收紧而非拒绝
	d := New(dir)
	if err := d.Ensure(); err != nil {
		t.Fatalf("应能收紧到 0700，got: %v", err)
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("收紧后权限 = %o, want 0700", info.Mode().Perm())
	}
}

func TestEnsureCredentialKeyGeneratesOnFirstRun(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	key, err := d.EnsureCredentialKey()
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	if len(key) != keyLen {
		t.Fatalf("密钥长度 = %d, want %d", len(key), keyLen)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(d.CredentialKeyPath())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("key 权限 = %o, want 0600", info.Mode().Perm())
		}
	}
}

func TestEnsureCredentialKeyReuseStable(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	first, err := d.EnsureCredentialKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.EnsureCredentialKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("复用时密钥必须一致，绝不覆盖")
	}
	// 文件内容应为 Base64，且解码后等于 first
	raw, _ := os.ReadFile(d.CredentialKeyPath())
	decoded, _ := base64.StdEncoding.DecodeString(string(trimSpaceBytes(raw)))
	if !bytes.Equal(decoded, first) {
		t.Fatal("文件内容与返回密钥不一致")
	}
}

func TestEnsureCredentialKeyCorruptFailsFast(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	if err := os.WriteFile(d.CredentialKeyPath(), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureCredentialKey(); err == nil {
		t.Fatal("损坏密钥应 fail-fast")
	}
}

func TestEnsureCredentialKeyEmptyFileFailsFast(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	if err := os.WriteFile(d.CredentialKeyPath(), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureCredentialKey(); err == nil {
		t.Fatal("空密钥文件应 fail-fast")
	}
}

func TestEnsureCredentialKeyWrongLengthFailsFast(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	if err := os.WriteFile(d.CredentialKeyPath(),
		[]byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureCredentialKey(); err == nil {
		t.Fatal("31 字节密钥应 fail-fast")
	}
}

func TestEnsureCredentialKeyConcurrentFirstRun(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	const n = 8
	var wg sync.WaitGroup
	keys := make([][]byte, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			keys[i], errs[i] = d.EnsureCredentialKey()
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d 失败: %v", i, errs[i])
		}
		if !bytes.Equal(keys[0], keys[i]) {
			t.Fatalf("goroutine %d 密钥与 goroutine 0 不一致", i)
		}
	}
}

func TestEnsureCredentialKeyPreservesExistingOnDirReensure(t *testing.T) {
	t.Parallel()
	d := newTestDir(t)
	first, err := d.EnsureCredentialKey()
	if err != nil {
		t.Fatal(err)
	}
	// 模拟仅删 config（保留 key）后再次 Ensure + EnsureCredentialKey
	if err := d.Ensure(); err != nil {
		t.Fatal(err)
	}
	again, err := d.EnsureCredentialKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, again) {
		t.Fatal("保留 key 时复用必须稳定")
	}
}
