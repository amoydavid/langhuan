package model

import (
	"fmt"
	"strings"
	"unicode/utf8"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

const CurrentParseManifestVersion = 1

type BlockKind string

const (
	BlockKindHeading       BlockKind = "heading"
	BlockKindParagraph     BlockKind = "paragraph"
	BlockKindList          BlockKind = "list"
	BlockKindQuote         BlockKind = "quote"
	BlockKindCode          BlockKind = "code"
	BlockKindThematicBreak BlockKind = "thematic_break"
	BlockKindTableHeader   BlockKind = "table_header"
	BlockKindTableRow      BlockKind = "table_row"
)

type ParsedBlock struct {
	Sequence        int
	Kind            BlockKind
	NormalizedStart int
	NormalizedEnd   int
	HeadingPath     []string
	SourceAnchor    value.SourceAnchor
	Metadata        map[string]any
}

type ParseWarning struct {
	Code         string
	Message      string
	SourceAnchor value.SourceAnchor
}

type ParseManifest struct {
	Version       int
	Parser        string
	ParserVersion int
	Blocks        []ParsedBlock
	Warnings      []ParseWarning
}

func (m ParseManifest) Validate(markdown string) error {
	if m.Version != CurrentParseManifestVersion {
		return fmt.Errorf("%w: 不支持的 parse manifest version: %d", domainerrors.ErrValidation, m.Version)
	}
	if !knownParser(m.Parser) {
		return fmt.Errorf("%w: 未知 parser: %q", domainerrors.ErrValidation, m.Parser)
	}
	if m.ParserVersion <= 0 {
		return fmt.Errorf("%w: parser_version 必须大于 0", domainerrors.ErrValidation)
	}
	if !utf8.ValidString(markdown) {
		return fmt.Errorf("%w: normalized markdown 不是有效 UTF-8", domainerrors.ErrValidation)
	}
	if len(m.Blocks) == 0 {
		return fmt.Errorf("%w: parse manifest 至少需要一个 block", domainerrors.ErrValidation)
	}
	previousEnd := 0
	for index, block := range m.Blocks {
		if block.Sequence != index {
			return fmt.Errorf("%w: block sequence 必须从 0 连续递增", domainerrors.ErrValidation)
		}
		if !knownBlockKind(block.Kind) {
			return fmt.Errorf("%w: 未知 block kind: %q", domainerrors.ErrValidation, block.Kind)
		}
		if block.NormalizedStart < previousEnd || block.NormalizedEnd <= block.NormalizedStart || block.NormalizedEnd > len(markdown) {
			return fmt.Errorf("%w: block %d span 非法", domainerrors.ErrValidation, index)
		}
		if !byteBoundary(markdown, block.NormalizedStart) || !byteBoundary(markdown, block.NormalizedEnd) {
			return fmt.Errorf("%w: block %d span 不在 UTF-8 边界", domainerrors.ErrValidation, index)
		}
		if err := block.SourceAnchor.Validate(); err != nil {
			return fmt.Errorf("block %d source anchor: %w", index, err)
		}
		previousEnd = block.NormalizedEnd
	}
	return nil
}

func knownParser(parser string) bool {
	switch strings.TrimSpace(parser) {
	case "markdown", "text", "csv", "xlsx", "docx", "pdf", "stub":
		return true
	default:
		return false
	}
}

func knownBlockKind(kind BlockKind) bool {
	switch kind {
	case BlockKindHeading, BlockKindParagraph, BlockKindList, BlockKindQuote, BlockKindCode, BlockKindThematicBreak, BlockKindTableHeader, BlockKindTableRow:
		return true
	default:
		return false
	}
}

func byteBoundary(text string, offset int) bool {
	return offset == 0 || offset == len(text) || utf8.RuneStart(text[offset])
}
