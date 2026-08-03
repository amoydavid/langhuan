// Package docx parses the supported read-only subset of WordprocessingML.
package docx

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	parseradapter "github.com/dajee/langhuan/internal/adapters/parser"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// Parser parses DOCX body structures without external dependencies.
type Parser struct{}

var _ parserport.DocumentParser = (*Parser)(nil)

// New creates a DOCX parser.
func New() *Parser { return &Parser{} }

// Supports reports whether fileType is docx.
func (p *Parser) Supports(fileType string) bool {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".") == "docx"
}

// Parse extracts headings, paragraphs, lists and tables from a DOCX body.
func (p *Parser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	if !p.Supports(input.FileType) {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, input.FileType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parts, err := readParts(input.Content)
	if err != nil {
		return nil, err
	}
	styles, err := parseStyles(parts["word/styles.xml"])
	if err != nil {
		return nil, fmt.Errorf("%w: DOCX styles XML 无效", parserport.ErrInvalidDocument)
	}
	if err := validateOptionalXML(parts["word/numbering.xml"]); err != nil {
		return nil, fmt.Errorf("%w: DOCX numbering XML 无效", parserport.ErrInvalidDocument)
	}
	nodes, err := parseDocument(ctx, parts["word/document.xml"])
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%w: DOCX document XML 无效", parserport.ErrInvalidDocument)
	}

	renderer := documentRenderer{
		builder:      parseradapter.NewManifestBuilder("docx"),
		styles:       styles,
		warningCodes: make(map[string]struct{}),
	}
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if node.paragraph != nil {
			renderer.addParagraph(*node.paragraph)
			continue
		}
		if node.table != nil {
			if err := renderer.addTable(*node.table); err != nil {
				return nil, err
			}
		}
	}
	renderer.flushList()
	if renderer.blockCount == 0 {
		return nil, fmt.Errorf("%w: DOCX 没有可解析正文", parserport.ErrEmptyDocument)
	}
	return renderer.builder.Build()
}

type headingEntry struct {
	level int
	title string
}

type documentRenderer struct {
	builder      *parseradapter.ManifestBuilder
	styles       map[string]int
	headings     []headingEntry
	pendingList  []paragraphData
	listPath     []string
	warningCodes map[string]struct{}
	blockCount   int
}

func (r *documentRenderer) addParagraph(paragraph paragraphData) {
	anchor := paragraphAnchor(paragraph.number, paragraph.number)
	r.addWarnings(paragraph.warnings, anchor)
	text := strings.TrimSpace(paragraph.text)
	if paragraph.numID != nil && text != "" {
		if len(r.pendingList) > 0 && *r.pendingList[len(r.pendingList)-1].numID != *paragraph.numID {
			r.flushList()
		}
		if len(r.pendingList) == 0 {
			r.listPath = r.headingPath()
		}
		r.pendingList = append(r.pendingList, paragraph)
		return
	}
	r.flushList()
	if text == "" {
		return
	}
	level := 0
	if paragraph.outlineLevel != nil && *paragraph.outlineLevel >= 0 && *paragraph.outlineLevel < 6 {
		level = *paragraph.outlineLevel + 1
	} else {
		level = r.styles[paragraph.styleID]
	}
	if level > 0 {
		title := escapeInline(text)
		r.updateHeading(level, text)
		r.builder.Append(
			model.BlockKindHeading,
			strings.Repeat("#", level)+" "+title,
			r.headingPath(),
			anchor,
			nil,
		)
		r.blockCount++
		return
	}
	r.builder.Append(model.BlockKindParagraph, escapeParagraph(text), r.headingPath(), anchor, nil)
	r.blockCount++
}

func (r *documentRenderer) flushList() {
	if len(r.pendingList) == 0 {
		return
	}
	lines := make([]string, len(r.pendingList))
	for index, paragraph := range r.pendingList {
		level := paragraph.listLevel
		if level < 0 {
			level = 0
		}
		lines[index] = strings.Repeat("  ", level) + "- " + escapeInline(strings.TrimSpace(paragraph.text))
	}
	start := r.pendingList[0].number
	end := r.pendingList[len(r.pendingList)-1].number
	r.builder.Append(model.BlockKindList, strings.Join(lines, "\n"), r.listPath, paragraphAnchor(start, end), nil)
	r.blockCount++
	r.pendingList = nil
	r.listPath = nil
}

func (r *documentRenderer) addTable(table tableData) error {
	r.flushList()
	tableAnchor := value.SourceAnchor{SourceType: "docx", TableIndex: intPointer(table.number)}
	r.addWarnings(table.warnings, tableAnchor)
	first, last := nonEmptyTableRange(table.rows)
	if first < 0 {
		return nil
	}
	header := parseradapter.TableRecord{Number: table.rows[first].number, Cells: table.rows[first].cells}
	rows := make([]parseradapter.TableRecord, 0, last-first)
	for index := first + 1; index <= last; index++ {
		rows = append(rows, parseradapter.TableRecord{Number: table.rows[index].number, Cells: table.rows[index].cells})
	}
	if err := parseradapter.AppendTable(r.builder, parseradapter.TableInput{
		SourceType:  "docx",
		TableIndex:  intPointer(table.number),
		TableID:     fmt.Sprintf("docx-table-%d", table.number),
		HeadingPath: r.headingPath(),
		Header:      header,
		Rows:        rows,
	}); err != nil {
		return fmt.Errorf("%w: DOCX 表格无效", parserport.ErrInvalidDocument)
	}
	r.blockCount += 1 + len(rows)
	return nil
}

func (r *documentRenderer) updateHeading(level int, title string) {
	for len(r.headings) > 0 && r.headings[len(r.headings)-1].level >= level {
		r.headings = r.headings[:len(r.headings)-1]
	}
	r.headings = append(r.headings, headingEntry{level: level, title: title})
}

func (r *documentRenderer) headingPath() []string {
	path := make([]string, len(r.headings))
	for index, heading := range r.headings {
		path[index] = heading.title
	}
	return path
}

func (r *documentRenderer) addWarnings(codes map[string]struct{}, anchor value.SourceAnchor) {
	ordered := make([]string, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}
	sort.Strings(ordered)
	for _, code := range ordered {
		if _, exists := r.warningCodes[code]; exists {
			continue
		}
		r.warningCodes[code] = struct{}{}
		r.builder.AddWarning(model.ParseWarning{Code: code, Message: warningMessage(code), SourceAnchor: anchor})
	}
}

func warningMessage(code string) string {
	switch code {
	case "unsupported_image":
		return "DOCX 图片未在本版本提取"
	case "unsupported_textbox":
		return "DOCX 文本框未在本版本提取"
	case "unsupported_footnote":
		return "DOCX 脚注未在本版本提取"
	case "unsupported_endnote":
		return "DOCX 尾注未在本版本提取"
	case "unsupported_comment":
		return "DOCX 批注未在本版本提取"
	default:
		return "DOCX 包含未支持内容"
	}
}

func paragraphAnchor(start, end int) value.SourceAnchor {
	return value.SourceAnchor{
		SourceType:     "docx",
		ParagraphStart: intPointer(start),
		ParagraphEnd:   intPointer(end),
	}
}

func nonEmptyTableRange(rows []tableRowData) (int, int) {
	first, last := -1, -1
	for index, row := range rows {
		if tableRowEmpty(row.cells) {
			continue
		}
		if first < 0 {
			first = index
		}
		last = index
	}
	return first, last
}

func tableRowEmpty(cells []string) bool {
	for _, cell := range cells {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func escapeInline(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	replacer := strings.NewReplacer(
		"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_",
		"[", "\\[", "]", "\\]", "<", "\\<", ">", "\\>", "|", "\\|",
	)
	return strings.ReplaceAll(replacer.Replace(text), "\n", "<br>")
}

func escapeParagraph(text string) string {
	escaped := escapeInline(text)
	trimmed := strings.TrimLeft(escaped, " ")
	indent := escaped[:len(escaped)-len(trimmed)]
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "+ ") {
		return indent + "\\" + trimmed
	}
	return escaped
}

func validateOptionalXML(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func intPointer(number int) *int {
	copy := number
	return &copy
}
