package mcp

import "testing"

func TestDocumentOutlineOf(t *testing.T) {
	markdown := "# 标题一\n\n正文。\n\n## 小节甲\n\n正文。\n\n## 小节乙\n\n### 子目\n\n# 标题二\n"
	entries := documentOutlineOf(markdown)
	if len(entries) != 5 {
		t.Fatalf("entries = %#v", entries)
	}
	if got := entries[0]; len(got.Path) != 1 || got.Path[0] != "标题一" || got.Line != 1 {
		t.Fatalf("entry0 = %#v", got)
	}
	if got := entries[1]; len(got.Path) != 2 || got.Path[1] != "小节甲" || got.Line != 5 {
		t.Fatalf("entry1 = %#v", got)
	}
	if got := entries[3]; len(got.Path) != 3 || got.Path[2] != "子目" {
		t.Fatalf("entry3 = %#v", got)
	}
	if got := entries[4]; len(got.Path) != 1 || got.Path[0] != "标题二" {
		t.Fatalf("entry4 = %#v", got)
	}

	// 无标题结构的纯文本：outline 为空。
	if entries := documentOutlineOf("第一段。\n\n第二段。\n"); len(entries) != 0 {
		t.Fatalf("纯文本 outline 应为空：%#v", entries)
	}

	// 非标题行（列表/代码围栏）不误判。
	if entries := documentOutlineOf("- # 不是标题\n```\n## 围栏内\n```\n"); len(entries) != 0 {
		t.Fatalf("非标题行不应进入 outline：%#v", entries)
	}
}
