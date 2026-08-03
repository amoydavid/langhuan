package version

import (
	"strings"
	"testing"
)

func TestBuildVersionNeverEmpty(t *testing.T) {
	got := Version()
	if got == "" {
		t.Fatal("Version() returned empty string")
	}
	// 单元测试运行在未注入版本的开发构建中，预期回退到 dev。
	if got != "dev" {
		t.Fatalf("Version() = %q, want %q in untagged test build", got, "dev")
	}
}

func TestBuildVersionIsSafeForDisplay(t *testing.T) {
	got := Version()
	// 版本字符串进入 MCP server name 和日志，禁止包含控制字符或换行。
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("Version() contains control characters: %q", got)
	}
}
