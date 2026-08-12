package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigSelectionExplicitWins(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "my.yaml")
	if err := os.WriteFile(explicit, []byte("database:\n  driver: postgres\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sel, err := resolveConfigSelection(explicit, true,
		filepath.Join(t.TempDir(), "config.yaml"),
		filepath.Join(t.TempDir(), "config.yaml"),
		dir,
		func(string) (string, error) { t.Fatal("显式存在时不应调用 generator"); return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if !sel.Explicit || sel.Path != explicit {
		t.Fatalf("sel = %+v", sel)
	}
}

func TestResolveConfigSelectionExplicitMissingFails(t *testing.T) {
	_, err := resolveConfigSelection("/nonexistent/explicit.yaml", true,
		"", "", "", func(string) (string, error) { return "", nil })
	if err == nil {
		t.Fatal("显式配置不存在应失败")
	}
}

func TestResolveConfigSelectionCwdSecond(t *testing.T) {
	cwdDir := t.TempDir()
	cwdConfig := filepath.Join(cwdDir, "config.yaml")
	if err := os.WriteFile(cwdConfig, []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sel, err := resolveConfigSelection("", false, cwdConfig,
		filepath.Join(t.TempDir(), "config.yaml"), t.TempDir(),
		func(string) (string, error) { t.Fatal("cwd 存在时不应生成"); return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if sel.Explicit || sel.Path != cwdConfig {
		t.Fatalf("sel = %+v", sel)
	}
}

func TestResolveConfigSelectionDataDirThird(t *testing.T) {
	dataDir := t.TempDir()
	dataDirConfig := filepath.Join(dataDir, "config.yaml")
	if err := os.WriteFile(dataDirConfig, []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sel, err := resolveConfigSelection("", false,
		filepath.Join(t.TempDir(), "config.yaml"), // cwd 不存在
		dataDirConfig, dataDir,
		func(string) (string, error) { t.Fatal("dataDir config 存在时不应生成"); return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if sel.Path != dataDirConfig {
		t.Fatalf("sel = %+v", sel)
	}
}

func TestResolveConfigSelectionNoneExistGenerates(t *testing.T) {
	dataDir := t.TempDir()
	called := false
	gen := func(d string) (string, error) {
		called = true
		if d != dataDir {
			t.Fatalf("generator 收到 dataDir = %q, want %q", d, dataDir)
		}
		p := filepath.Join(d, "config.yaml")
		return p, os.WriteFile(p, []byte("gen\n"), 0o600)
	}
	sel, err := resolveConfigSelection("", false,
		filepath.Join(t.TempDir(), "config.yaml"), // cwd 不存在
		filepath.Join(dataDir, "config.yaml"),     // dataDir config 不存在
		dataDir, gen)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("无任何 config 时应调用 generator")
	}
	if sel.Path != filepath.Join(dataDir, "config.yaml") {
		t.Fatalf("sel.Path = %q", sel.Path)
	}
}

func TestResolveConfigSelectionCorruptDataDirConfigDoesNotRegen(t *testing.T) {
	// 第3层 config 已存在（即使损坏）→ 命中第3层，generator 不调用。
	// 损坏由后续 config.Load 报错，探测阶段只判存在性。
	dataDir := t.TempDir()
	dataDirConfig := filepath.Join(dataDir, "config.yaml")
	if err := os.WriteFile(dataDirConfig, []byte(":\n  bad yaml: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	sel, err := resolveConfigSelection("", false,
		filepath.Join(t.TempDir(), "config.yaml"),
		dataDirConfig, dataDir,
		func(string) (string, error) { t.Fatal("损坏 config 不应触发生成"); return "", nil })
	if err != nil {
		t.Fatalf("损坏 config 存在时应命中第3层返回路径，got err: %v", err)
	}
	if sel.Path != dataDirConfig {
		t.Fatalf("应命中第3层，sel.Path = %q", sel.Path)
	}
}
