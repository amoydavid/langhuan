package value

import "testing"

func TestMergeSourceAnchorsKeepsTableHeaderSeparate(t *testing.T) {
	header := 2
	row3, row4, row5, row8 := 3, 4, 5, 8
	left := SourceAnchor{SourceType: "xlsx", Sheet: "数据", HeaderRow: &header, RowStart: &row3, RowEnd: &row4}
	right := SourceAnchor{SourceType: "xlsx", Sheet: "数据", HeaderRow: &header, RowStart: &row5, RowEnd: &row8}
	got, err := MergeSourceAnchors(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if got.HeaderRow == nil || *got.HeaderRow != 2 || got.RowStart == nil || *got.RowStart != 3 || got.RowEnd == nil || *got.RowEnd != 8 {
		t.Fatalf("merged anchor = %#v", got)
	}
}

func TestSourceAnchorValidateRejectsNegativeCoordinate(t *testing.T) {
	negative := -1
	if err := (SourceAnchor{SourceType: "txt", OffsetStart: &negative}).Validate(); err == nil {
		t.Fatal("Validate() error = nil, want validation error")
	}
}

func TestSourceAnchorValidateRejectsZeroHumanCoordinate(t *testing.T) {
	zero := 0
	if err := (SourceAnchor{SourceType: "csv", RowStart: &zero}).Validate(); err == nil {
		t.Fatal("Validate() error = nil, want validation error")
	}
}

func TestMergeSourceAnchorsRejectsNonContiguousRows(t *testing.T) {
	header := 1
	row2, row4 := 2, 4
	left := SourceAnchor{SourceType: "csv", Sheet: "CSV", HeaderRow: &header, RowStart: &row2, RowEnd: &row2}
	right := SourceAnchor{SourceType: "csv", Sheet: "CSV", HeaderRow: &header, RowStart: &row4, RowEnd: &row4}
	if _, err := MergeSourceAnchors(left, right); err == nil {
		t.Fatal("MergeSourceAnchors() error = nil, want validation error")
	}
}
