package parser

import (
	"strings"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

const parserVersion = 1

type pendingBlock struct {
	kind        model.BlockKind
	content     string
	headingPath []string
	anchor      value.SourceAnchor
	metadata    map[string]any
}

// ManifestBuilder builds normalized Markdown and matching byte spans.
type ManifestBuilder struct {
	parser   string
	blocks   []pendingBlock
	warnings []model.ParseWarning
}

// NewManifestBuilder creates a version 1 manifest builder for parserName.
func NewManifestBuilder(parserName string) *ManifestBuilder {
	return &ManifestBuilder{parser: strings.TrimSpace(parserName)}
}

// Append adds a normalized Markdown block. Blocks are separated by two LF bytes.
func (b *ManifestBuilder) Append(
	kind model.BlockKind,
	content string,
	headingPath []string,
	anchor value.SourceAnchor,
	metadata map[string]any,
) {
	b.blocks = append(b.blocks, pendingBlock{
		kind:        kind,
		content:     content,
		headingPath: append([]string(nil), headingPath...),
		anchor:      anchor,
		metadata:    copyMetadata(metadata),
	})
}

// AddWarning records a non-fatal parser warning.
func (b *ManifestBuilder) AddWarning(warning model.ParseWarning) {
	b.warnings = append(b.warnings, warning)
}

// Build returns normalized Markdown and a validated parse manifest.
func (b *ManifestBuilder) Build() (*parserport.ParsedDocument, error) {
	var markdown strings.Builder
	blocks := make([]model.ParsedBlock, 0, len(b.blocks))
	for sequence, block := range b.blocks {
		if sequence > 0 {
			markdown.WriteString("\n\n")
		}
		start := markdown.Len()
		markdown.WriteString(block.content)
		blocks = append(blocks, model.ParsedBlock{
			Sequence:        sequence,
			Kind:            block.kind,
			NormalizedStart: start,
			NormalizedEnd:   markdown.Len(),
			HeadingPath:     append([]string(nil), block.headingPath...),
			SourceAnchor:    block.anchor,
			Metadata:        copyMetadata(block.metadata),
		})
	}
	result := &parserport.ParsedDocument{
		Markdown: markdown.String(),
		Manifest: model.ParseManifest{
			Version:       model.CurrentParseManifestVersion,
			Parser:        b.parser,
			ParserVersion: parserVersion,
			Blocks:        blocks,
			Warnings:      append([]model.ParseWarning(nil), b.warnings...),
		},
	}
	if err := result.Manifest.Validate(result.Markdown); err != nil {
		return nil, err
	}
	return result, nil
}

func copyMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}
