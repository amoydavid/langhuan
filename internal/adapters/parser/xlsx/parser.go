// Package xlsx parses OOXML workbooks into independent Markdown tables.
package xlsx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"

	parseradapter "github.com/dajee/langhuan/internal/adapters/parser"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
	"github.com/xuri/excelize/v2"
)

const (
	maxZIPEntries    = 10_000
	maxExpandedSize  = int64(512 << 20)
	maxSingleXMLSize = int64(128 << 20)
)

// Parser parses XLSX workbooks without external I/O.
type Parser struct{}

var _ parserport.DocumentParser = (*Parser)(nil)

// New creates an XLSX parser.
func New() *Parser { return &Parser{} }

// Supports reports whether fileType is xlsx.
func (p *Parser) Supports(fileType string) bool {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".") == "xlsx"
}

// Parse converts each non-empty workbook sheet into a heading and table.
func (p *Parser) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	if !p.Supports(input.FileType) {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, input.FileType)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateArchive(input.Content); err != nil {
		return nil, err
	}
	book, err := excelize.OpenReader(bytes.NewReader(input.Content), excelize.Options{
		RawCellValue:      false,
		UnzipSizeLimit:    maxExpandedSize,
		UnzipXMLSizeLimit: maxSingleXMLSize,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: 无法打开 XLSX", parserport.ErrInvalidDocument)
	}
	defer book.Close()

	builder := parseradapter.NewManifestBuilder("xlsx")
	nonEmptySheets := 0
	for sheetIndex, sheet := range book.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		visible, err := book.GetSheetVisible(sheet)
		if err != nil {
			return nil, fmt.Errorf("%w: 无法读取 sheet 状态", parserport.ErrInvalidDocument)
		}
		rows, err := book.GetRows(sheet, excelize.Options{RawCellValue: false})
		if err != nil {
			return nil, fmt.Errorf("%w: 无法读取 sheet", parserport.ErrInvalidDocument)
		}
		if err := resolveFormulaValues(ctx, book, sheet, rows); err != nil {
			return nil, err
		}
		headerIndex, lastIndex := nonEmptyRange(rows)
		if headerIndex < 0 {
			continue
		}

		nonEmptySheets++
		hidden := !visible
		metadata := map[string]any{"hidden": hidden}
		anchor := value.SourceAnchor{SourceType: "xlsx", Sheet: sheet}
		headingPath := []string{sheet}
		builder.Append(model.BlockKindHeading, "## "+sheet, headingPath, anchor, metadata)

		header := parseradapter.TableRecord{Number: headerIndex + 1, Cells: append([]string(nil), rows[headerIndex]...)}
		dataRows := make([]parseradapter.TableRecord, 0, lastIndex-headerIndex)
		for rowIndex := headerIndex + 1; rowIndex <= lastIndex; rowIndex++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			dataRows = append(dataRows, parseradapter.TableRecord{
				Number: rowIndex + 1,
				Cells:  append([]string(nil), rows[rowIndex]...),
			})
		}
		if err := parseradapter.AppendTable(builder, parseradapter.TableInput{
			SourceType:  "xlsx",
			Sheet:       sheet,
			TableID:     fmt.Sprintf("xlsx-sheet-%d", sheetIndex),
			HeadingPath: headingPath,
			Metadata:    metadata,
			Header:      header,
			Rows:        dataRows,
		}); err != nil {
			return nil, fmt.Errorf("%w: XLSX 表格无效", parserport.ErrInvalidDocument)
		}
	}
	if nonEmptySheets == 0 {
		return nil, fmt.Errorf("%w: XLSX 没有非空 sheet", parserport.ErrEmptyDocument)
	}
	return builder.Build()
}

func validateArchive(content []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return fmt.Errorf("%w: XLSX 不是有效 ZIP", parserport.ErrInvalidDocument)
	}
	if len(archive.File) > maxZIPEntries {
		return fmt.Errorf("%w: XLSX ZIP entry 过多", parserport.ErrParseLimitExceeded)
	}
	var total uint64
	for _, file := range archive.File {
		total += file.UncompressedSize64
		if total > uint64(maxExpandedSize) {
			return fmt.Errorf("%w: XLSX 解压总大小超限", parserport.ErrParseLimitExceeded)
		}
		if strings.HasSuffix(strings.ToLower(file.Name), ".xml") && file.UncompressedSize64 > uint64(maxSingleXMLSize) {
			return fmt.Errorf("%w: XLSX XML part 超限", parserport.ErrParseLimitExceeded)
		}
	}
	return nil
}

func resolveFormulaValues(ctx context.Context, book *excelize.File, sheet string, rows [][]string) error {
	for rowIndex := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		for columnIndex := range rows[rowIndex] {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				return fmt.Errorf("%w: XLSX 单元格坐标无效", parserport.ErrInvalidDocument)
			}
			formula, err := book.GetCellFormula(sheet, cell)
			if err != nil {
				return fmt.Errorf("%w: 无法读取 XLSX 公式", parserport.ErrInvalidDocument)
			}
			if formula == "" {
				continue
			}
			current := strings.TrimSpace(rows[rowIndex][columnIndex])
			if current != "" && current != formula && current != "="+formula {
				// Prefer the workbook's cached/formatted display value when present.
				continue
			}
			calculated, err := book.CalcCellValue(sheet, cell)
			if err == nil {
				rows[rowIndex][columnIndex] = calculated
			}
		}
	}
	return nil
}

func nonEmptyRange(rows [][]string) (int, int) {
	first, last := -1, -1
	for index, row := range rows {
		if rowEmpty(row) {
			continue
		}
		if first < 0 {
			first = index
		}
		last = index
	}
	return first, last
}

func rowEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
