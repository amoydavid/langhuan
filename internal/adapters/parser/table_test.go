package parser

import (
	"testing"
)

func TestAppendTableEscapesCellsPadsRowsAndBuildsAnchors(t *testing.T) {
	builder := NewManifestBuilder("csv")
	err := AppendTable(builder, TableInput{
		SourceType: "csv",
		Sheet:      "CSV",
		TableID:    "table-0",
		Header:     TableRecord{Number: 2, LineStart: 2, LineEnd: 2, Cells: []string{"名称", "值"}},
		Rows: []TableRecord{
			{Number: 3, LineStart: 3, LineEnd: 4, Cells: []string{"A|B", "两行\n内容"}},
			{Number: 4, LineStart: 5, LineEnd: 5, Cells: []string{"C"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	want := "| 名称 | 值 |\n| --- | --- |\n\n| A\\|B | 两行<br>内容 |\n\n| C |  |"
	if got.Markdown != want {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
	}
	row := got.Manifest.Blocks[1].SourceAnchor
	if row.RowStart == nil || *row.RowStart != 3 || row.RowEnd == nil || *row.RowEnd != 3 ||
		row.HeaderRow == nil || *row.HeaderRow != 2 || row.ColumnEnd == nil || *row.ColumnEnd != 2 ||
		row.LineStart == nil || *row.LineStart != 3 || row.LineEnd == nil || *row.LineEnd != 4 {
		t.Fatalf("row anchor = %#v", row)
	}
	if row.SourceType != "csv" || row.Sheet != "CSV" {
		t.Fatalf("row source = %#v", row)
	}
	if got.Manifest.Blocks[0].Metadata["table_id"] != "table-0" {
		t.Fatalf("header metadata = %#v", got.Manifest.Blocks[0].Metadata)
	}
}
