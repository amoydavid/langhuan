// Package version 暴露运行时构建版本，避免在业务代码里硬编码。
//
// Version 优先从 Go build info 读取（包含 VCS 信息或 -ldflags 注入的值），
// 解析失败或开发构建时返回 "dev"，保证调用方始终得到非空字符串。
package version

import (
	"runtime/debug"
)

// Version 返回当前构建版本。
//
// 读取顺序：
//  1. Go build info 中的 module version（vcs tag、`go install` 或
//     `-ldflags` 注入）。
//  2. 任一步骤失败或为空时返回 "dev"。
//
// 该函数永不返回空串；失败也只回退到 "dev"，不向上传播错误，便于在
// MCP server、HTTP healthz 等入口直接调用。
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return "dev"
}
