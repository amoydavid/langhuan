package parser

import (
	"fmt"
	"strings"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// TableRecord is one logical source row before Markdown normalization.
type TableRecord struct {
	Number    int
	LineStart int
	LineEnd   int
	Cells     []string
}

// TableInput describes one independent source table.
type TableInput struct {
	SourceType  string
	Sheet       string
	TableIndex  *int
	TableID     string
	HeadingPath []string
	Metadata    map[string]any
	Header      TableRecord
	Rows        []TableRecord
}

// AppendTable emits one header block and one block per data row.
func AppendTable(builder *ManifestBuilder, input TableInput) error {
	if builder == nil {
		return fmt.Errorf("table builder is nil")
	}
	if strings.TrimSpace(input.SourceType) == "" || strings.TrimSpace(input.TableID) == "" || input.Header.Number <= 0 {
		return fmt.Errorf("table source, id and header row are required")
	}
	columnCount := len(input.Header.Cells)
	for _, row := range input.Rows {
		if len(row.Cells) > columnCount {
			columnCount = len(row.Cells)
		}
	}
	if columnCount == 0 {
		return fmt.Errorf("table must contain at least one column")
	}

	headerCells := padCells(input.Header.Cells, columnCount)
	for column := len(input.Header.Cells); column < columnCount; column++ {
		headerCells[column] = fmt.Sprintf("Column %d", column+1)
	}
	metadata := mergeTableMetadata(input.Metadata, input.TableID)
	headerRow := input.Header.Number
	columnStart, columnEnd := 1, columnCount
	headerAnchor := value.SourceAnchor{
		SourceType:  input.SourceType,
		Sheet:       input.Sheet,
		TableIndex:  cloneTableInt(input.TableIndex),
		HeaderRow:   &headerRow,
		ColumnStart: &columnStart,
		ColumnEnd:   &columnEnd,
	}
	setLineRange(&headerAnchor, input.Header.LineStart, input.Header.LineEnd)
	builder.Append(
		model.BlockKindTableHeader,
		renderHeader(headerCells),
		input.HeadingPath,
		headerAnchor,
		metadata,
	)

	for _, row := range input.Rows {
		if row.Number <= 0 {
			return fmt.Errorf("table row number must be positive")
		}
		rowNumber := row.Number
		anchor := value.SourceAnchor{
			SourceType:  input.SourceType,
			Sheet:       input.Sheet,
			TableIndex:  cloneTableInt(input.TableIndex),
			HeaderRow:   intPointer(headerRow),
			RowStart:    &rowNumber,
			RowEnd:      intPointer(rowNumber),
			ColumnStart: intPointer(columnStart),
			ColumnEnd:   intPointer(columnEnd),
		}
		setLineRange(&anchor, row.LineStart, row.LineEnd)
		builder.Append(
			model.BlockKindTableRow,
			renderRow(padCells(row.Cells, columnCount)),
			input.HeadingPath,
			anchor,
			metadata,
		)
	}
	return nil
}

func renderHeader(cells []string) string {
	separator := make([]string, len(cells))
	for index := range separator {
		separator[index] = "---"
	}
	return renderRow(cells) + "\n" + renderRow(separator)
}

func renderRow(cells []string) string {
	normalized := make([]string, len(cells))
	for index, cell := range cells {
		cell = strings.ReplaceAll(cell, "\r\n", "\n")
		cell = strings.ReplaceAll(cell, "\r", "\n")
		cell = strings.ReplaceAll(cell, "|", "\\|")
		normalized[index] = strings.ReplaceAll(cell, "\n", "<br>")
	}
	return "| " + strings.Join(normalized, " | ") + " |"
}

func padCells(cells []string, count int) []string {
	result := make([]string, count)
	copy(result, cells)
	return result
}

func mergeTableMetadata(metadata map[string]any, tableID string) map[string]any {
	result := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		result[key] = value
	}
	result["table_id"] = tableID
	return result
}

func setLineRange(anchor *value.SourceAnchor, start, end int) {
	if start > 0 {
		anchor.LineStart = intPointer(start)
	}
	if end > 0 {
		anchor.LineEnd = intPointer(end)
	}
}

func intPointer(number int) *int {
	copy := number
	return &copy
}

func cloneTableInt(number *int) *int {
	if number == nil {
		return nil
	}
	return intPointer(*number)
}
