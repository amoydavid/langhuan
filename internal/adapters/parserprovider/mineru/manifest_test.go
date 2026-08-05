package mineru

import (
	"context"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
)

func TestBuildParsedDocumentReparsesMinerUMarkdown(t *testing.T) {
	parsed, err := buildParsedDocument(context.Background(), "# 安装\n\n正文\n\n| 名称 | 值 |\n| --- | --- |\n| A | 1 |", "v1")
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
