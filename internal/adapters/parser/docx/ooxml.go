package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

const (
	maxZIPEntries    = 10_000
	maxExpandedSize  = int64(512 << 20)
	maxSingleXMLSize = int64(128 << 20)
)

type paragraphData struct {
	text         string
	styleID      string
	outlineLevel *int
	numID        *int
	listLevel    int
	number       int
	warnings     map[string]struct{}
}

type tableData struct {
	number   int
	rows     []tableRowData
	warnings map[string]struct{}
}

type tableRowData struct {
	number int
	cells  []string
}

type bodyNode struct {
	paragraph *paragraphData
	table     *tableData
}

func readParts(content []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("%w: DOCX 不是有效 ZIP", parserport.ErrInvalidDocument)
	}
	if len(reader.File) > maxZIPEntries {
		return nil, fmt.Errorf("%w: DOCX ZIP entry 过多", parserport.ErrParseLimitExceeded)
	}
	var total uint64
	files := make(map[string]*zip.File, 3)
	for _, file := range reader.File {
		total += file.UncompressedSize64
		if total > uint64(maxExpandedSize) {
			return nil, fmt.Errorf("%w: DOCX 解压总大小超限", parserport.ErrParseLimitExceeded)
		}
		if strings.HasSuffix(strings.ToLower(file.Name), ".xml") && file.UncompressedSize64 > uint64(maxSingleXMLSize) {
			return nil, fmt.Errorf("%w: DOCX XML part 超限", parserport.ErrParseLimitExceeded)
		}
		switch file.Name {
		case "word/document.xml", "word/styles.xml", "word/numbering.xml":
			files[file.Name] = file
		}
	}
	if files["word/document.xml"] == nil {
		return nil, fmt.Errorf("%w: DOCX 缺少 word/document.xml", parserport.ErrInvalidDocument)
	}
	parts := make(map[string][]byte, len(files))
	for name, file := range files {
		opened, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: 无法读取 DOCX part", parserport.ErrInvalidDocument)
		}
		data, readErr := io.ReadAll(io.LimitReader(opened, maxSingleXMLSize+1))
		closeErr := opened.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("%w: 无法读取 DOCX part", parserport.ErrInvalidDocument)
		}
		if int64(len(data)) > maxSingleXMLSize {
			return nil, fmt.Errorf("%w: DOCX XML part 实际大小超限", parserport.ErrParseLimitExceeded)
		}
		parts[name] = data
	}
	return parts, nil
}

func parseStyles(data []byte) (map[string]int, error) {
	levels := make(map[string]int)
	if len(data) == 0 {
		return levels, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return levels, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "style" {
			continue
		}
		styleID := attr(start, "styleId")
		name, outline, err := parseStyle(decoder, start)
		if err != nil {
			return nil, err
		}
		level := headingLevel(styleID + " " + name)
		if outline != nil && *outline >= 0 && *outline < 6 {
			level = *outline + 1
		}
		if styleID != "" && level > 0 {
			levels[styleID] = level
		}
	}
}

func parseStyle(decoder *xml.Decoder, start xml.StartElement) (string, *int, error) {
	var name string
	var outline *int
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "name":
				name = attr(typed, "val")
			case "outlineLvl":
				outline = intAttr(typed, "val")
			}
		case xml.EndElement:
			if typed.Name == start.Name {
				return name, outline, nil
			}
		}
	}
}

var (
	headingPattern        = regexp.MustCompile(`(?i)heading\s*([1-6])`)
	chineseHeadingPattern = regexp.MustCompile(`标题\s*([1-6])`)
)

func headingLevel(name string) int {
	for _, pattern := range []*regexp.Regexp{headingPattern, chineseHeadingPattern} {
		match := pattern.FindStringSubmatch(name)
		if len(match) == 2 {
			level, _ := strconv.Atoi(match[1])
			return level
		}
	}
	return 0
}

func parseDocument(ctx context.Context, data []byte) ([]bodyNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			return nil, fmt.Errorf("missing body")
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if ok && start.Name.Local == "body" {
			return parseBody(ctx, decoder, start)
		}
	}
}

func parseBody(ctx context.Context, decoder *xml.Decoder, body xml.StartElement) ([]bodyNode, error) {
	nodes := make([]bodyNode, 0)
	paragraphNumber := 0
	tableNumber := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "p":
				paragraphNumber++
				paragraph, err := parseParagraph(decoder, typed)
				if err != nil {
					return nil, err
				}
				paragraph.number = paragraphNumber
				nodes = append(nodes, bodyNode{paragraph: &paragraph})
			case "tbl":
				tableNumber++
				table, err := parseTable(ctx, decoder, typed)
				if err != nil {
					return nil, err
				}
				table.number = tableNumber
				nodes = append(nodes, bodyNode{table: &table})
			default:
				if err := decoder.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if typed.Name == body.Name {
				return nodes, nil
			}
		}
	}
}

func parseParagraph(decoder *xml.Decoder, start xml.StartElement) (paragraphData, error) {
	paragraph := paragraphData{warnings: make(map[string]struct{})}
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return paragraphData{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				var content string
				if err := decoder.DecodeElement(&content, &typed); err != nil {
					return paragraphData{}, err
				}
				text.WriteString(content)
			case "tab":
				text.WriteByte('\t')
			case "br", "cr":
				text.WriteByte('\n')
			case "pStyle":
				paragraph.styleID = attr(typed, "val")
			case "outlineLvl":
				paragraph.outlineLevel = intAttr(typed, "val")
			case "numId":
				paragraph.numID = intAttr(typed, "val")
			case "ilvl":
				if level := intAttr(typed, "val"); level != nil && *level >= 0 {
					paragraph.listLevel = *level
				}
			case "drawing", "pict":
				paragraph.warnings["unsupported_image"] = struct{}{}
				if err := decoder.Skip(); err != nil {
					return paragraphData{}, err
				}
			case "txbxContent":
				paragraph.warnings["unsupported_textbox"] = struct{}{}
				if err := decoder.Skip(); err != nil {
					return paragraphData{}, err
				}
			case "footnoteReference":
				paragraph.warnings["unsupported_footnote"] = struct{}{}
			case "endnoteReference":
				paragraph.warnings["unsupported_endnote"] = struct{}{}
			case "commentReference", "commentRangeStart", "commentRangeEnd":
				paragraph.warnings["unsupported_comment"] = struct{}{}
			}
		case xml.EndElement:
			if typed.Name == start.Name {
				paragraph.text = text.String()
				return paragraph, nil
			}
		}
	}
}

func parseTable(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (tableData, error) {
	table := tableData{warnings: make(map[string]struct{})}
	for {
		if err := ctx.Err(); err != nil {
			return tableData{}, err
		}
		token, err := decoder.Token()
		if err != nil {
			return tableData{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "tr" {
				if err := decoder.Skip(); err != nil {
					return tableData{}, err
				}
				continue
			}
			row, warnings, err := parseTableRow(ctx, decoder, typed)
			if err != nil {
				return tableData{}, err
			}
			row.number = len(table.rows) + 1
			table.rows = append(table.rows, row)
			mergeWarnings(table.warnings, warnings)
		case xml.EndElement:
			if typed.Name == start.Name {
				return table, nil
			}
		}
	}
}

func parseTableRow(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (tableRowData, map[string]struct{}, error) {
	row := tableRowData{}
	warnings := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return tableRowData{}, nil, err
		}
		token, err := decoder.Token()
		if err != nil {
			return tableRowData{}, nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "tc" {
				if err := decoder.Skip(); err != nil {
					return tableRowData{}, nil, err
				}
				continue
			}
			cell, cellWarnings, err := parseTableCell(ctx, decoder, typed)
			if err != nil {
				return tableRowData{}, nil, err
			}
			row.cells = append(row.cells, cell)
			mergeWarnings(warnings, cellWarnings)
		case xml.EndElement:
			if typed.Name == start.Name {
				return row, warnings, nil
			}
		}
	}
}

func parseTableCell(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (string, map[string]struct{}, error) {
	paragraphs := make([]string, 0)
	warnings := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		token, err := decoder.Token()
		if err != nil {
			return "", nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "p" {
				if err := decoder.Skip(); err != nil {
					return "", nil, err
				}
				continue
			}
			paragraph, err := parseParagraph(decoder, typed)
			if err != nil {
				return "", nil, err
			}
			paragraphs = append(paragraphs, strings.TrimSpace(paragraph.text))
			mergeWarnings(warnings, paragraph.warnings)
		case xml.EndElement:
			if typed.Name == start.Name {
				return strings.Join(paragraphs, "\n"), warnings, nil
			}
		}
	}
}

func attr(start xml.StartElement, local string) string {
	for _, attribute := range start.Attr {
		if attribute.Name.Local == local {
			return attribute.Value
		}
	}
	return ""
}

func intAttr(start xml.StartElement, local string) *int {
	value, err := strconv.Atoi(attr(start, local))
	if err != nil {
		return nil
	}
	return &value
}

func mergeWarnings(destination, source map[string]struct{}) {
	for code := range source {
		destination[code] = struct{}{}
	}
}
