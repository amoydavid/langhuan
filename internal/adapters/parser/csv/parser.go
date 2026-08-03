// Package csv parses CSV documents into normalized Markdown tables.
package csv

import (
	"bytes"
	"context"
	encodingcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	parseradapter "github.com/dajee/langhuan/internal/adapters/parser"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// Parser parses UTF-8 CSV input.
type Parser struct{}

var _ parserport.DocumentParser = (*Parser)(nil)

// New creates a CSV parser.
func New() *Parser { return &Parser{} }

// Supports reports whether fileType is csv.
func (p *Parser) Supports(fileType string) bool {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".") == "csv"
}

// Parse converts CSV logical records into a single Markdown table.
func (p *Parser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	if !p.Supports(input.FileType) {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, input.FileType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !utf8.Valid(input.Content) {
		return nil, fmt.Errorf("%w: CSV 必须是 UTF-8", parserport.ErrInvalidEncoding)
	}
	content := bytes.TrimPrefix(input.Content, []byte{0xef, 0xbb, 0xbf})
	reader := encodingcsv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false

	logicalRecord := 0
	var header *parseradapter.TableRecord
	rows := make([]parseradapter.TableRecord, 0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: CSV 结构无效: %v", parserport.ErrInvalidDocument, err)
		}
		logicalRecord++
		lineStart, _ := reader.FieldPos(0)
		lineEnd := lineStart
		for _, field := range record {
			lineEnd += strings.Count(strings.ReplaceAll(field, "\r\n", "\n"), "\n")
		}
		tableRecord := parseradapter.TableRecord{
			Number: logicalRecord, LineStart: lineStart, LineEnd: lineEnd,
			Cells: append([]string(nil), record...),
		}
		if header == nil {
			if recordEmpty(record) {
				continue
			}
			header = &tableRecord
			continue
		}
		rows = append(rows, tableRecord)
	}
	if header == nil {
		return nil, fmt.Errorf("%w: CSV 没有非空记录", parserport.ErrEmptyDocument)
	}

	builder := parseradapter.NewManifestBuilder("csv")
	if err := parseradapter.AppendTable(builder, parseradapter.TableInput{
		SourceType: "csv",
		Sheet:      "CSV",
		TableID:    "table-0",
		Header:     *header,
		Rows:       rows,
	}); err != nil {
		return nil, fmt.Errorf("%w: CSV 表格无效: %v", parserport.ErrInvalidDocument, err)
	}
	return builder.Build()
}

func recordEmpty(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
