// Package text parses UTF-8 plain text into paragraph Markdown blocks.
package text

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	parseradapter "github.com/dajee/langhuan/internal/adapters/parser"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// Parser parses plain UTF-8 text.
type Parser struct{}

var _ parserport.DocumentParser = (*Parser)(nil)

// New creates a plain-text parser.
func New() *Parser {
	return &Parser{}
}

// Supports reports whether fileType is txt.
func (p *Parser) Supports(fileType string) bool {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".") == "txt"
}

// Parse splits plain text at blank lines and escapes Markdown syntax.
func (p *Parser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	if !p.Supports(input.FileType) {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, input.FileType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !utf8.Valid(input.Content) {
		return nil, fmt.Errorf("%w: TXT 必须是 UTF-8", parserport.ErrInvalidEncoding)
	}

	source := normalizeText(input.Content)
	if strings.TrimSpace(string(source)) == "" {
		return nil, fmt.Errorf("%w: TXT 没有可解析内容", parserport.ErrEmptyDocument)
	}

	builder := parseradapter.NewManifestBuilder("text")
	paragraphs := paragraphRanges(source)
	for _, paragraph := range paragraphs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw := string(source[paragraph.start:paragraph.end])
		escaped := escapeMarkdown(raw)
		var metadata map[string]any
		if escaped != raw {
			metadata = map[string]any{"anchor_granularity": "block"}
		}
		builder.Append(
			model.BlockKindParagraph,
			escaped,
			nil,
			sourceAnchor(source, paragraph.start, paragraph.end),
			metadata,
		)
	}
	return builder.Build()
}

type byteRange struct {
	start int
	end   int
}

func normalizeText(content []byte) []byte {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
}

func paragraphRanges(source []byte) []byteRange {
	ranges := make([]byteRange, 0)
	paragraphStart := -1
	paragraphEnd := -1
	for lineStart := 0; lineStart <= len(source); {
		lineEnd := bytes.IndexByte(source[lineStart:], '\n')
		next := len(source) + 1
		if lineEnd < 0 {
			lineEnd = len(source)
		} else {
			lineEnd += lineStart
			next = lineEnd + 1
		}
		if len(bytes.TrimSpace(source[lineStart:lineEnd])) == 0 {
			if paragraphStart >= 0 {
				ranges = append(ranges, byteRange{start: paragraphStart, end: paragraphEnd})
				paragraphStart = -1
			}
		} else {
			if paragraphStart < 0 {
				paragraphStart = lineStart
			}
			paragraphEnd = lineEnd
		}
		if next > len(source) {
			break
		}
		lineStart = next
	}
	if paragraphStart >= 0 {
		ranges = append(ranges, byteRange{start: paragraphStart, end: paragraphEnd})
	}
	return ranges
}

func escapeMarkdown(paragraph string) string {
	lines := strings.Split(paragraph, "\n")
	for index, line := range lines {
		var escaped strings.Builder
		escaped.Grow(len(line))
		for _, character := range line {
			switch character {
			case '\\', '`', '*', '_', '[', ']', '<', '>', '|', '~':
				escaped.WriteRune('\\')
			}
			escaped.WriteRune(character)
		}
		lines[index] = escapeBlockMarker(escaped.String())
	}
	return strings.Join(lines, "\n")
}

func escapeBlockMarker(line string) string {
	if strings.HasPrefix(line, "\t") {
		return "&#9;" + line[1:]
	}
	if strings.HasPrefix(line, "    ") {
		return "&#32;" + line[1:]
	}
	indent := 0
	for indent < len(line) && indent < 3 && line[indent] == ' ' {
		indent++
	}
	rest := line[indent:]
	if isATXHeading(rest) || isListMarker(rest) || isOrderedListMarker(rest) || isSetextOrThematicMarker(rest) {
		return line[:indent] + "\\" + rest
	}
	return line
}

func isSetextOrThematicMarker(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	if strings.Trim(trimmed, "=") == "" {
		return true
	}
	hyphens := 0
	for _, character := range trimmed {
		switch character {
		case '-':
			hyphens++
		case ' ', '\t':
		default:
			return false
		}
	}
	return hyphens >= 3
}

func isATXHeading(line string) bool {
	count := 0
	for count < len(line) && count < 6 && line[count] == '#' {
		count++
	}
	return count > 0 && (count == len(line) || line[count] == ' ' || line[count] == '\t')
}

func isListMarker(line string) bool {
	return len(line) >= 2 && (line[0] == '-' || line[0] == '+') && (line[1] == ' ' || line[1] == '\t')
}

func isOrderedListMarker(line string) bool {
	digits := 0
	for digits < len(line) && digits < 9 && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	return digits > 0 && digits+1 < len(line) && (line[digits] == '.' || line[digits] == ')') &&
		(line[digits+1] == ' ' || line[digits+1] == '\t')
}

func sourceAnchor(source []byte, start, end int) value.SourceAnchor {
	offsetStart := utf8.RuneCount(source[:start])
	offsetEnd := utf8.RuneCount(source[:end])
	lineStart := bytes.Count(source[:start], []byte("\n")) + 1
	lineEnd := bytes.Count(source[:end], []byte("\n")) + 1
	return value.SourceAnchor{
		SourceType:  "txt",
		OffsetStart: &offsetStart,
		OffsetEnd:   &offsetEnd,
		LineStart:   &lineStart,
		LineEnd:     &lineEnd,
	}
}
