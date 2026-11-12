package mineru

import (
	"context"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestBuildParsedDocumentReparsesMinerUMarkdown(t *testing.T) {
	parsed, err := buildParsedDocument(context.Background(), "# 安装\n\n正文\n\n| 名称 | 值 |\n| --- | --- |\n| A | 1 |", "v1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Manifest.Blocks) < 3 || parsed.Manifest.Blocks[0].Kind != model.BlockKindHeading {
		t.Fatalf("manifest=%#v", parsed.Manifest)
	}
	for _, block := range parsed.Manifest.Blocks {
		if block.SourceAnchor.SourceType != "pdf" {
			t.Fatalf("anchor=%#v", block.SourceAnchor)
		}
	}
}

func TestBuildParsedDocumentAttachesPDFPageAnchors(t *testing.T) {
	markdown := "# 安装\n\n第一步：下载安装包。\n\n第二步：运行安装程序。"
	contentList := `[{"type":"text","text":"# 安装","text_level":1,"page_idx":0},
		{"type":"text","text":"第一步：下载安装包。","page_idx":0},
		{"type":"image","img_path":"images/1.jpg","page_idx":1},
		{"type":"text","text":"第二步：运行安装程序。","page_idx":1},
		{"type":"table","table_body":"<table></table>","page_idx":2}]`
	parsed, err := buildParsedDocument(context.Background(), markdown, "v1", contentList)
	if err != nil {
		t.Fatal(err)
	}
	blocks := parsed.Manifest.Blocks
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	type expectation struct {
		page int
		kind model.BlockKind
	}
	expected := []expectation{{page: 1, kind: model.BlockKindHeading}, {page: 1}, {page: 2}}
	for index, want := range expected {
		got := blocks[index].SourceAnchor
		if got.Page == nil || *got.Page != want.page {
			t.Fatalf("block %d page = %v, want %d", index, got.Page, want.page)
		}
		if got.SchemaVersion == nil || *got.SchemaVersion != 2 {
			t.Fatalf("block %d schema_version = %v, want 2", index, got.SchemaVersion)
		}
		if got.SourceType != "pdf" {
			t.Fatalf("block %d source_type = %s", index, got.SourceType)
		}
		if want.kind != "" && blocks[index].Kind != want.kind {
			t.Fatalf("block %d kind = %s", index, blocks[index].Kind)
		}
	}
	// 合并跨页块：页范围聚合（1..2），单块 Page/BBox 被丢弃。
	merged, err := value.MergeSourceAnchors(blocks[0].SourceAnchor, blocks[2].SourceAnchor)
	if err != nil {
		t.Fatal(err)
	}
	if merged.PageStart == nil || *merged.PageStart != 1 || merged.PageEnd == nil || *merged.PageEnd != 2 {
		t.Fatalf("merged page range = %v..%v, want 1..2", merged.PageStart, merged.PageEnd)
	}
	if merged.Page != nil || merged.BBox != nil {
		t.Fatalf("merged anchor should drop single-page/bbox fields: %#v", merged)
	}
}

func TestBuildParsedDocumentStopsAlignmentOnMismatch(t *testing.T) {
	markdown := "# 标题\n\n内容与条目不一致的正文。"
	contentList := `[{"type":"text","text":"完全不同的文本","page_idx":0},
		{"type":"text","text":"后续条目","page_idx":5}]`
	parsed, err := buildParsedDocument(context.Background(), markdown, "v1", contentList)
	if err != nil {
		t.Fatal(err)
	}
	for index, block := range parsed.Manifest.Blocks {
		if block.SourceAnchor.Page != nil {
			t.Fatalf("block %d should not carry page anchor after mismatch: %#v", index, block.SourceAnchor)
		}
	}
	found := false
	for _, warning := range parsed.Manifest.Warnings {
		if warning.Code == "page_anchor_misaligned" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected page_anchor_misaligned warning")
	}
}

func TestBuildParsedDocumentInvalidPageIdxDropsAnchor(t *testing.T) {
	markdown := "# 标题\n\n正文"
	contentList := `[{"type":"text","text":"# 标题","page_idx":0},
		{"type":"text","text":"正文","page_idx":-3}]`
	parsed, err := buildParsedDocument(context.Background(), markdown, "v1", contentList)
	if err != nil {
		t.Fatal(err)
	}
	last := parsed.Manifest.Blocks[len(parsed.Manifest.Blocks)-1]
	if last.SourceAnchor.Page != nil {
		t.Fatalf("negative page_idx must be dropped: %#v", last.SourceAnchor)
	}
}
