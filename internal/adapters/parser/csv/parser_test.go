package csv

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func TestParserPreservesQuotedFieldsAndLogicalAnchors(t *testing.T) {
	source := ",\n名称,说明\nA,\"含,逗号\"\nB,\"两行\n内容\"\nC\nD,x,extra\n"
	got, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "csv", Content: []byte(source)})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"| 名称 | 说明 | Column 3 |\n| --- | --- | --- |",
		"| A | 含,逗号 |  |",
		"| B | 两行<br>内容 |  |",
		"| C |  |  |",
		"| D | x | extra |",
	}, "\n\n")
	if got.Markdown != want {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
	}
	if len(got.Manifest.Blocks) != 5 || got.Manifest.Blocks[0].Kind != model.BlockKindTableHeader {
		t.Fatalf("blocks = %#v", got.Manifest.Blocks)
	}
	for i, wantRow := range []int{3, 4, 5, 6} {
		block := got.Manifest.Blocks[i+1]
		if block.Kind != model.BlockKindTableRow || block.SourceAnchor.RowStart == nil || *block.SourceAnchor.RowStart != wantRow {
			t.Fatalf("block %d = %#v", i+1, block)
		}
	}
	multiline := got.Manifest.Blocks[2].SourceAnchor
	if multiline.LineStart == nil || *multiline.LineStart != 4 || multiline.LineEnd == nil || *multiline.LineEnd != 5 {
		t.Fatalf("multiline anchor = %#v", multiline)
	}
	if got.Manifest.Blocks[0].SourceAnchor.HeaderRow == nil || *got.Manifest.Blocks[0].SourceAnchor.HeaderRow != 2 {
		t.Fatalf("header anchor = %#v", got.Manifest.Blocks[0].SourceAnchor)
	}
}

func TestParserRejectsInvalidEmptyUnsupportedAndCanceled(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		typeName string
		content  []byte
		want     error
	}{
		{name: "invalid utf8", ctx: context.Background(), typeName: "csv", content: []byte{0xff}, want: parserport.ErrInvalidEncoding},
		{name: "invalid csv", ctx: context.Background(), typeName: "csv", content: []byte("a,b\n\"broken"), want: parserport.ErrInvalidDocument},
		{name: "empty", ctx: context.Background(), typeName: "csv", content: []byte(",\n , \n"), want: parserport.ErrEmptyDocument},
		{name: "unsupported", ctx: context.Background(), typeName: "txt", content: []byte("a"), want: parserport.ErrUnsupportedFileType},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name     string
		ctx      context.Context
		typeName string
		content  []byte
		want     error
	}{
		name: "canceled", ctx: ctx, typeName: "csv", content: []byte("a\n1"), want: context.Canceled,
	})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New().Parse(test.ctx, parserport.ParseInput{FileType: test.typeName, Content: test.content})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
