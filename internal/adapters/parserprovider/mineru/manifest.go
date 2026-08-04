package mineru

import (
	"fmt"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// buildParsedDocument 把 MinerU 产出的 Markdown 组装成 ParsedDocument。
// MinerU Markdown 通常已是结构化文本，这里生成一个简单的 manifest：
// 把整篇 Markdown 作为一个 paragraph block，anchor 指向文档级别。
func buildParsedDocument(markdown, modelVersion string) (*parserport.ParsedDocument, error) {
	markdown = trimWhitespace(markdown)
	if markdown == "" {
		return nil, fmt.Errorf("MinerU 返回空 Markdown")
	}

	blocks := buildBlocks(markdown)
	manifest := model.ParseManifest{
		Version:       model.CurrentParseManifestVersion,
		Parser:        "pdf",
		ParserVersion: 1,
		Blocks:        blocks,
		Warnings:      nil,
	}

	return &parserport.ParsedDocument{
		Markdown: markdown,
		Manifest: manifest,
	}, nil
}

func buildBlocks(markdown string) []model.ParsedBlock {
	return []model.ParsedBlock{
		{
			Sequence:        0,
			Kind:            model.BlockKindParagraph,
			NormalizedStart: 0,
			NormalizedEnd:   len(markdown),
			SourceAnchor: value.SourceAnchor{
				SourceType: "pdf",
			},
		},
	}
}

func trimWhitespace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
