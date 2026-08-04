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
	if err := m.ValidateStructure(); err != nil {
		return err
	}
	if !utf8.ValidString(markdown) {
		return fmt.Errorf("%w: normalized markdown 不是有效 UTF-8", domainerrors.ErrValidation)
	}
	for index, block := range m.Blocks {
		if block.NormalizedEnd > len(markdown) {
			return fmt.Errorf("%w: block %d 超出 markdown 长度", domainerrors.ErrValidation, index)
		}
		if !byteBoundary(markdown, block.NormalizedStart) || !byteBoundary(markdown, block.NormalizedEnd) {
			return fmt.Errorf("%w: block %d span 不在 UTF-8 边界", domainerrors.ErrValidation, index)
		}
	}
	return nil
}

// ValidateStructure 校验 manifest 的结构约束（不依赖 markdown 内容）。
// 供 db codec 等需要独立校验 JSON 解码结果的层复用，保证校验规则单一来源，
// 避免与 Validate(markdown) 各自维护导致漂移。
func (m ParseManifest) ValidateStructure() error {
	if m.Version != CurrentParseManifestVersion {
		return fmt.Errorf("%w: 不支持的 parse manifest version: %d", domainerrors.ErrValidation, m.Version)
	}
	if !IsKnownParser(m.Parser) {
		return fmt.Errorf("%w: 未知 parser: %q", domainerrors.ErrValidation, m.Parser)
	}
	if m.ParserVersion <= 0 {
		return fmt.Errorf("%w: parser_version 必须大于 0", domainerrors.ErrValidation)
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
		if block.NormalizedStart < previousEnd || block.NormalizedEnd <= block.NormalizedStart {
			return fmt.Errorf("%w: block %d span 非法", domainerrors.ErrValidation, index)
		}
		if err := block.SourceAnchor.Validate(); err != nil {
			return fmt.Errorf("block %d source anchor: %w", index, err)
		}
		previousEnd = block.NormalizedEnd
	}
	for index, warning := range m.Warnings {
		if err := warning.SourceAnchor.Validate(); err != nil {
			return fmt.Errorf("warning %d source anchor: %w", index, err)
		}
	}
	return nil
}

// IsKnownParser 判断 parser 名称是否在已知集合内。
// 导出供 codec 等其他层共享同一份白名单，避免多处重复维护导致漂移。
func IsKnownParser(parser string) bool {
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
