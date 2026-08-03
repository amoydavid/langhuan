package parser

import (
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestManifestBuilderRecordsUTF8ByteSpans(t *testing.T) {
	b := NewManifestBuilder("text")
	b.Append(model.BlockKindParagraph, "你好", nil, value.SourceAnchor{SourceType: "txt"}, nil)
	b.Append(model.BlockKindParagraph, "world", nil, value.SourceAnchor{SourceType: "txt"}, nil)

	got, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if got.Markdown != "你好\n\nworld" {
		t.Fatalf("markdown = %q", got.Markdown)
	}
	if got.Manifest.Blocks[1].NormalizedStart != len("你好\n\n") {
		t.Fatalf("span = %#v", got.Manifest.Blocks[1])
	}
	if got.Manifest.Blocks[1].NormalizedEnd != len(got.Markdown) {
		t.Fatalf("span = %#v", got.Manifest.Blocks[1])
	}
	if got.Manifest.Blocks[1].Sequence != 1 {
		t.Fatalf("sequence = %d", got.Manifest.Blocks[1].Sequence)
	}
}

func TestManifestBuilderRejectsEmptyBlock(t *testing.T) {
	b := NewManifestBuilder("text")
	b.Append(model.BlockKindParagraph, "", nil, value.SourceAnchor{SourceType: "txt"}, nil)
	if _, err := b.Build(); err == nil {
		t.Fatal("Build() error = nil, want invalid manifest error")
	}
}
