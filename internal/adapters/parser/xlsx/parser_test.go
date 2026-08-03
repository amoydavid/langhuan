package xlsx

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
	"github.com/xuri/excelize/v2"
)

func TestParserProducesIndependentSheetsFormattedValuesAndMergedCells(t *testing.T) {
	content := workbookFixture(t)
	got, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "xlsx", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	want := "## 一月\n\n| 名称 | 值 |\n| --- | --- |\n\n| 格式化 | 1.50 |\n\n| 合并 |  |\n\n| 公式 | 3 |\n\n## 隐藏数据\n\n| 键 | 内容 |\n| --- | --- |\n\n| hidden | 是 |"
	if got.Markdown != want {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
	}
	if len(got.Manifest.Blocks) != 8 {
		t.Fatalf("blocks = %#v", got.Manifest.Blocks)
	}
	if got.Manifest.Blocks[0].Kind != model.BlockKindHeading || got.Manifest.Blocks[5].Kind != model.BlockKindHeading {
		t.Fatalf("heading blocks = %#v / %#v", got.Manifest.Blocks[0], got.Manifest.Blocks[5])
	}
	if got.Manifest.Blocks[0].SourceAnchor.Sheet != "一月" || got.Manifest.Blocks[5].SourceAnchor.Sheet != "隐藏数据" {
		t.Fatalf("sheet order = %#v / %#v", got.Manifest.Blocks[0].SourceAnchor, got.Manifest.Blocks[5].SourceAnchor)
	}
	if got.Manifest.Blocks[5].Metadata["hidden"] != true || got.Manifest.Blocks[6].Metadata["hidden"] != true {
		t.Fatalf("hidden metadata = %#v / %#v", got.Manifest.Blocks[5].Metadata, got.Manifest.Blocks[6].Metadata)
	}
	row := got.Manifest.Blocks[3].SourceAnchor
	if row.HeaderRow == nil || *row.HeaderRow != 1 || row.RowStart == nil || *row.RowStart != 3 || row.ColumnEnd == nil || *row.ColumnEnd != 2 {
		t.Fatalf("merged row anchor = %#v", row)
	}
}

func TestParserRejectsBrokenEmptyUnsupportedAndCanceled(t *testing.T) {
	if _, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "xlsx", Content: []byte("bad")}); !errors.Is(err, parserport.ErrInvalidDocument) {
		t.Fatalf("broken error = %v", err)
	}
	empty := emptyWorkbookFixture(t)
	if _, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "xlsx", Content: empty}); !errors.Is(err, parserport.ErrEmptyDocument) {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "csv", Content: empty}); !errors.Is(err, parserport.ErrUnsupportedFileType) {
		t.Fatalf("unsupported error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Parse(ctx, parserport.ParseInput{FileType: "xlsx", Content: empty}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func workbookFixture(t *testing.T) []byte {
	t.Helper()
	book := excelize.NewFile()
	t.Cleanup(func() { _ = book.Close() })
	if err := book.SetSheetName("Sheet1", "一月"); err != nil {
		t.Fatal(err)
	}
	for cell, value := range map[string]any{"A1": "名称", "B1": "值", "A2": "格式化", "B2": 1.5, "A3": "合并", "A4": "公式"} {
		if err := book.SetCellValue("一月", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	style, err := book.NewStyle(&excelize.Style{NumFmt: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := book.SetCellStyle("一月", "B2", "B2", style); err != nil {
		t.Fatal(err)
	}
	if err := book.MergeCell("一月", "A3", "B3"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetCellFormula("一月", "B4", "=SUM(1,2)"); err != nil {
		t.Fatal(err)
	}
	if _, err := book.NewSheet("隐藏数据"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetVisible("隐藏数据", false); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("隐藏数据", "A1", &[]any{"键", "内容"}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("隐藏数据", "A2", &[]any{"hidden", "是"}); err != nil {
		t.Fatal(err)
	}
	if _, err := book.NewSheet("空表"); err != nil {
		t.Fatal(err)
	}
	buffer, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), buffer.Bytes()...)
}

func emptyWorkbookFixture(t *testing.T) []byte {
	t.Helper()
	book := excelize.NewFile()
	defer book.Close()
	var buffer bytes.Buffer
	if err := book.Write(&buffer); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
