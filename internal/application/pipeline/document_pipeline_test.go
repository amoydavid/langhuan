package pipeline

import (
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func parsedTestDocument(markdown string) *parserport.ParsedDocument {
	if markdown == "" {
		markdown = "parsed"
	}
	return &parserport.ParsedDocument{
		Markdown: markdown,
		Manifest: model.ParseManifest{
			Version:       model.CurrentParseManifestVersion,
			Parser:        "stub",
			ParserVersion: 1,
			Blocks: []model.ParsedBlock{{
				Sequence:        0,
				Kind:            model.BlockKindParagraph,
				NormalizedStart: 0,
				NormalizedEnd:   len(markdown),
				SourceAnchor:    value.SourceAnchor{SourceType: "stub"},
			}},
		},
	}
}
