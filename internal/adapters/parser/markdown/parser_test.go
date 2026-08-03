package markdown

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func TestParserBuildsHeadingPathAndTable(t *testing.T) {
	source := "# 指南\n\n正文\n\n| 名称 | 值 |\n| --- | --- |\n| A | 1 |\n"
	got, err := New().Parse(context.Background(), parserport.ParseInput{
		FileType: "markdown",
		Content:  []byte(source),
	})
	if err != nil {
		t.Fatal(err)
	}

	wantMarkdown := "# 指南\n\n正文\n\n| 名称 | 值 |\n| --- | --- |\n\n| A | 1 |"
	if got.Markdown != wantMarkdown {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, wantMarkdown)
	}
	assertKinds(t, got.Manifest.Blocks,
		model.BlockKindHeading,
		model.BlockKindParagraph,
		model.BlockKindTableHeader,
		model.BlockKindTableRow,
	)
	for index, want := range [][]string{{"指南"}, {"指南"}, {"指南"}, {"指南"}} {
		if !slices.Equal(got.Manifest.Blocks[index].HeadingPath, want) {
			t.Fatalf("block %d heading path = %#v, want %#v", index, got.Manifest.Blocks[index].HeadingPath, want)
		}
	}
	if got.Manifest.Blocks[2].Metadata["table_id"] != "table-2" || got.Manifest.Blocks[3].Metadata["table_id"] != "table-2" {
		t.Fatalf("table metadata = %#v / %#v", got.Manifest.Blocks[2].Metadata, got.Manifest.Blocks[3].Metadata)
	}
	assertAnchor(t, got.Manifest.Blocks[0], 0, 4, 1, 1)
	assertAnchor(t, got.Manifest.Blocks[1], 6, 8, 3, 3)
	assertAnchor(t, got.Manifest.Blocks[2], 10, 34, 5, 6)
	assertAnchor(t, got.Manifest.Blocks[3], 35, 44, 7, 7)
}

func TestParserComplexGolden(t *testing.T) {
	source, err := os.ReadFile("testdata/complex.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "md", Content: source})
	if err != nil {
		t.Fatal(err)
	}

	wantMarkdown := strings.Replace(strings.TrimSuffix(string(source), "\n"), "| --- | --- |\n| 琅嬛 | 1 |", "| --- | --- |\n\n| 琅嬛 | 1 |", 1)
	if got.Markdown != wantMarkdown {
		t.Fatalf("Markdown mismatch\n--- got ---\n%s\n--- want ---\n%s", got.Markdown, wantMarkdown)
	}
	assertKinds(t, got.Manifest.Blocks,
		model.BlockKindHeading,
		model.BlockKindParagraph,
		model.BlockKindHeading,
		model.BlockKindList,
		model.BlockKindQuote,
		model.BlockKindCode,
		model.BlockKindThematicBreak,
		model.BlockKindTableHeader,
		model.BlockKindTableRow,
		model.BlockKindHeading,
		model.BlockKindParagraph,
		model.BlockKindHeading,
		model.BlockKindParagraph,
	)
	wantPaths := [][]string{
		{"指南"}, {"指南"}, {"指南", "基础"}, {"指南", "基础"},
		{"指南", "基础"}, {"指南", "基础"}, {"指南", "基础"},
		{"指南", "基础"}, {"指南", "基础"}, {"指南", "基础", "深入"},
		{"指南", "基础", "深入"}, {"指南", "附录"}, {"指南", "附录"},
	}
	for index, want := range wantPaths {
		if !slices.Equal(got.Manifest.Blocks[index].HeadingPath, want) {
			t.Fatalf("block %d heading path = %#v, want %#v", index, got.Manifest.Blocks[index].HeadingPath, want)
		}
	}
	if !strings.Contains(got.Markdown, "![架构图](images/arch.png)") {
		t.Fatal("Markdown image syntax was not preserved")
	}
	if got.Manifest.Blocks[5].SourceAnchor.LineStart == nil || *got.Manifest.Blocks[5].SourceAnchor.LineStart != 13 ||
		got.Manifest.Blocks[5].SourceAnchor.LineEnd == nil || *got.Manifest.Blocks[5].SourceAnchor.LineEnd != 16 {
		t.Fatalf("code anchor = %#v", got.Manifest.Blocks[5].SourceAnchor)
	}
	if got.Manifest.Blocks[7].Metadata["table_id"] != "table-7" || got.Manifest.Blocks[8].Metadata["table_id"] != "table-7" {
		t.Fatalf("table metadata = %#v / %#v", got.Manifest.Blocks[7].Metadata, got.Manifest.Blocks[8].Metadata)
	}
	if err := got.Manifest.Validate(got.Markdown); err != nil {
		t.Fatalf("manifest is invalid: %v", err)
	}
}

func TestParserNormalizesBOMAndNewlines(t *testing.T) {
	got, err := New().Parse(context.Background(), parserport.ParseInput{
		FileType: ".markdown",
		Content:  append([]byte{0xef, 0xbb, 0xbf}, []byte("# 标题\r\n\r正文\r\n")...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Markdown != "# 标题\n\n正文" {
		t.Fatalf("Markdown = %q", got.Markdown)
	}
	assertAnchor(t, got.Manifest.Blocks[0], 0, 4, 1, 1)
	assertAnchor(t, got.Manifest.Blocks[1], 6, 8, 3, 3)
}

func TestParserRejectsInvalidUTF8AndEmptyDocument(t *testing.T) {
	for name, content := range map[string][]byte{
		"invalid UTF-8": {0xff},
		"empty":         []byte(" \r\n\t\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "markdown", Content: content})
			want := parserport.ErrInvalidEncoding
			if name == "empty" {
				want = parserport.ErrEmptyDocument
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
}

func TestParserRejectsUnsupportedTypeAndCancellation(t *testing.T) {
	p := New()
	if !p.Supports("markdown") || !p.Supports("md") || p.Supports("txt") {
		t.Fatalf("unexpected Supports results")
	}
	_, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "txt", Content: []byte("text")})
	if !errors.Is(err, parserport.ErrUnsupportedFileType) {
		t.Fatalf("unsupported error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Parse(ctx, parserport.ParseInput{FileType: "markdown", Content: []byte("text")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func assertKinds(t *testing.T, blocks []model.ParsedBlock, want ...model.BlockKind) {
	t.Helper()
	if len(blocks) != len(want) {
		t.Fatalf("block count = %d, want %d: %#v", len(blocks), len(want), blocks)
	}
	for index, kind := range want {
		if blocks[index].Kind != kind {
			t.Fatalf("block %d kind = %q, want %q", index, blocks[index].Kind, kind)
		}
	}
}

func assertAnchor(t *testing.T, block model.ParsedBlock, offsetStart, offsetEnd, lineStart, lineEnd int) {
	t.Helper()
	anchor := block.SourceAnchor
	if anchor.SourceType != "markdown" || anchor.OffsetStart == nil || *anchor.OffsetStart != offsetStart ||
		anchor.OffsetEnd == nil || *anchor.OffsetEnd != offsetEnd || anchor.LineStart == nil || *anchor.LineStart != lineStart ||
		anchor.LineEnd == nil || *anchor.LineEnd != lineEnd {
		t.Fatalf("anchor = %#v, want offsets [%d,%d) lines [%d,%d]", anchor, offsetStart, offsetEnd, lineStart, lineEnd)
	}
}
