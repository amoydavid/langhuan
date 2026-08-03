// Package markdown parses UTF-8 Markdown into normalized Markdown and a source manifest.
package markdown

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
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type headingEntry struct {
	level int
	title string
}

// Parser parses Markdown with Goldmark's GFM extensions.
type Parser struct {
	markdown goldmark.Markdown
}

var _ parserport.DocumentParser = (*Parser)(nil)

// New creates a Markdown parser.
func New() *Parser {
	return &Parser{markdown: goldmark.New(goldmark.WithExtensions(extension.GFM))}
}

// Supports reports whether fileType is a Markdown alias.
func (p *Parser) Supports(fileType string) bool {
	switch normalizeFileType(fileType) {
	case "markdown", "md":
		return true
	default:
		return false
	}
}

// Parse parses one Markdown document without performing external I/O.
func (p *Parser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	if !p.Supports(input.FileType) {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, input.FileType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !utf8.Valid(input.Content) {
		return nil, fmt.Errorf("%w: Markdown 必须是 UTF-8", parserport.ErrInvalidEncoding)
	}

	source := normalizeText(input.Content)
	if strings.TrimSpace(string(source)) == "" {
		return nil, fmt.Errorf("%w: Markdown 没有可解析内容", parserport.ErrEmptyDocument)
	}

	document := p.markdown.Parser().Parse(text.NewReader(source))
	topLevel := topLevelNodes(document)
	if len(topLevel) == 0 {
		return nil, fmt.Errorf("%w: Markdown 没有可解析内容", parserport.ErrEmptyDocument)
	}

	builder := parseradapter.NewManifestBuilder("markdown")
	headings := make([]headingEntry, 0, 6)
	sequence := 0
	for index, node := range topLevel {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := lineStart(source, node.Pos())
		limit := len(source)
		if index+1 < len(topLevel) {
			limit = lineStart(source, topLevel[index+1].Pos())
		}
		end := contentEnd(source, start, limit)
		if end <= start {
			continue
		}

		if table, ok := node.(*extensionast.Table); ok {
			added, err := appendTable(ctx, builder, table, source, start, end, headingPath(headings), sequence)
			if err != nil {
				return nil, err
			}
			sequence += added
			continue
		}

		kind := blockKind(node)
		if heading, ok := node.(*ast.Heading); ok {
			title := strings.TrimSpace(string(heading.Text(source)))
			headings = updateHeadingPath(headings, heading.Level, title)
		}
		builder.Append(kind, string(source[start:end]), headingPath(headings), sourceAnchor(source, start, end), nil)
		sequence++
	}

	if sequence == 0 {
		return nil, fmt.Errorf("%w: Markdown 没有可解析内容", parserport.ErrEmptyDocument)
	}
	return builder.Build()
}

func appendTable(
	ctx context.Context,
	builder *parseradapter.ManifestBuilder,
	table *extensionast.Table,
	source []byte,
	tableStart, tableEnd int,
	headingPath []string,
	sequence int,
) (int, error) {
	children := make([]ast.Node, 0, table.ChildCount())
	for child := table.FirstChild(); child != nil; child = child.NextSibling() {
		children = append(children, child)
	}
	if len(children) == 0 {
		return 0, nil
	}

	tableID := fmt.Sprintf("table-%d", sequence)
	for index, child := range children {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		start := tableStart
		if index > 0 {
			start = lineStart(source, child.Pos())
		}
		end := tableEnd
		if index+1 < len(children) {
			end = lineStart(source, children[index+1].Pos())
		}
		end = contentEnd(source, start, end)
		if end <= start {
			continue
		}
		kind := model.BlockKindTableRow
		if index == 0 {
			kind = model.BlockKindTableHeader
		}
		builder.Append(kind, string(source[start:end]), headingPath, sourceAnchor(source, start, end), map[string]any{"table_id": tableID})
	}
	return len(children), nil
}

func normalizeFileType(fileType string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".")
}

func normalizeText(content []byte) []byte {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
}

func topLevelNodes(document ast.Node) []ast.Node {
	nodes := make([]ast.Node, 0, document.ChildCount())
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		nodes = append(nodes, node)
	}
	return nodes
}

func blockKind(node ast.Node) model.BlockKind {
	switch node.(type) {
	case *ast.Heading:
		return model.BlockKindHeading
	case *ast.List:
		return model.BlockKindList
	case *ast.Blockquote:
		return model.BlockKindQuote
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		return model.BlockKindCode
	case *ast.ThematicBreak:
		return model.BlockKindThematicBreak
	default:
		return model.BlockKindParagraph
	}
}

func updateHeadingPath(stack []headingEntry, level int, title string) []headingEntry {
	for len(stack) > 0 && stack[len(stack)-1].level >= level {
		stack = stack[:len(stack)-1]
	}
	return append(stack, headingEntry{level: level, title: title})
}

func headingPath(stack []headingEntry) []string {
	path := make([]string, len(stack))
	for index, heading := range stack {
		path[index] = heading.title
	}
	return path
}

func lineStart(source []byte, position int) int {
	if position < 0 {
		return 0
	}
	if position > len(source) {
		position = len(source)
	}
	if previous := bytes.LastIndexByte(source[:position], '\n'); previous >= 0 {
		return previous + 1
	}
	return 0
}

func contentEnd(source []byte, start, limit int) int {
	if start < 0 {
		start = 0
	}
	if limit > len(source) {
		limit = len(source)
	}
	end := limit
	for end > start {
		lineEnd := end
		if source[lineEnd-1] == '\n' {
			lineEnd--
		}
		lineStart := lineStart(source, lineEnd)
		if len(bytes.TrimSpace(source[lineStart:lineEnd])) > 0 {
			return lineEnd
		}
		end = lineStart
	}
	return start
}

func sourceAnchor(source []byte, start, end int) value.SourceAnchor {
	offsetStart := utf8.RuneCount(source[:start])
	offsetEnd := utf8.RuneCount(source[:end])
	lineStart := bytes.Count(source[:start], []byte("\n")) + 1
	lineEnd := bytes.Count(source[:end], []byte("\n")) + 1
	return value.SourceAnchor{
		SourceType:  "markdown",
		OffsetStart: &offsetStart,
		OffsetEnd:   &offsetEnd,
		LineStart:   &lineStart,
		LineEnd:     &lineEnd,
	}
}
