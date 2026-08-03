package text

import (
	"context"
	"errors"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func TestParserNormalizesBOMNewlinesAndEscapesMarkdown(t *testing.T) {
	content := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 标题\r\n内容包含 | 和 `code`\r\n\r\n- 列表\r\n")...)
	got, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "txt", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	want := "\\# 标题\n内容包含 \\| 和 \\`code\\`\n\n\\- 列表"
	if got.Markdown != want {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
	}
	if len(got.Manifest.Blocks) != 2 {
		t.Fatalf("blocks = %#v", got.Manifest.Blocks)
	}
	for index, block := range got.Manifest.Blocks {
		if block.Kind != model.BlockKindParagraph {
			t.Fatalf("block %d kind = %q", index, block.Kind)
		}
		if block.Metadata["anchor_granularity"] != "block" {
			t.Fatalf("block %d metadata = %#v", index, block.Metadata)
		}
	}
	assertTextAnchor(t, got.Manifest.Blocks[0], 0, 20, 1, 2)
	assertTextAnchor(t, got.Manifest.Blocks[1], 22, 26, 4, 4)
	if err := got.Manifest.Validate(got.Markdown); err != nil {
		t.Fatalf("manifest is invalid: %v", err)
	}
}

func TestParserUsesBlankLinesAsParagraphBoundaries(t *testing.T) {
	got, err := New().Parse(context.Background(), parserport.ParseInput{
		FileType: ".txt",
		Content:  []byte("第一行\n第二行\n \t\n\n第三段"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Markdown != "第一行\n第二行\n\n第三段" {
		t.Fatalf("Markdown = %q", got.Markdown)
	}
	if len(got.Manifest.Blocks) != 2 {
		t.Fatalf("blocks = %#v", got.Manifest.Blocks)
	}
	if got.Manifest.Blocks[0].Metadata != nil || got.Manifest.Blocks[1].Metadata != nil {
		t.Fatalf("metadata = %#v / %#v", got.Manifest.Blocks[0].Metadata, got.Manifest.Blocks[1].Metadata)
	}
	assertTextAnchor(t, got.Manifest.Blocks[0], 0, 7, 1, 2)
	assertTextAnchor(t, got.Manifest.Blocks[1], 12, 15, 5, 5)
}

func TestParserEscapesThematicSetextAndIndentedCodeLines(t *testing.T) {
	got, err := New().Parse(context.Background(), parserport.ParseInput{
		FileType: "txt",
		Content:  []byte("普通文本\n---\n\n===\n\n    缩进文本\n\t制表符缩进"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "普通文本\n\\---\n\n\\===\n\n&#32;   缩进文本\n&#9;制表符缩进"
	if got.Markdown != want {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
	}
}

func TestParserRejectsInvalidUTF8AndEmptyDocument(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    error
	}{
		{name: "invalid UTF-8", content: []byte{0xff}, want: parserport.ErrInvalidEncoding},
		{name: "empty", content: []byte(" \r\n\t\n"), want: parserport.ErrEmptyDocument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "txt", Content: test.content})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParserRejectsUnsupportedTypeAndCancellation(t *testing.T) {
	p := New()
	if !p.Supports("txt") || p.Supports("markdown") {
		t.Fatal("unexpected Supports results")
	}
	_, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "markdown", Content: []byte("text")})
	if !errors.Is(err, parserport.ErrUnsupportedFileType) {
		t.Fatalf("unsupported error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Parse(ctx, parserport.ParseInput{FileType: "txt", Content: []byte("text")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func assertTextAnchor(t *testing.T, block model.ParsedBlock, offsetStart, offsetEnd, lineStart, lineEnd int) {
	t.Helper()
	anchor := block.SourceAnchor
	if anchor.SourceType != "txt" || anchor.OffsetStart == nil || *anchor.OffsetStart != offsetStart ||
		anchor.OffsetEnd == nil || *anchor.OffsetEnd != offsetEnd || anchor.LineStart == nil || *anchor.LineStart != lineStart ||
		anchor.LineEnd == nil || *anchor.LineEnd != lineEnd {
		t.Fatalf("anchor = %#v, want offsets [%d,%d) lines [%d,%d]", anchor, offsetStart, offsetEnd, lineStart, lineEnd)
	}
}
