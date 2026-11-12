package value

import (
	"fmt"
	"math"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// SourceAnchor 描述内容在原始文档中的可追溯位置。所有人类可读坐标从 1 开始。
//
// PDF 物理页锚点（C03，schema_version=2）：Page 用于单块锚点，
// PageStart/PageEnd 用于多块合并后的页范围（1-based 物理页）；
// BBox 是块在页面坐标系中的外接框（MinerU 原样保留，合并后丢弃）。
// 缺页锚点的 PDF 块仍按诚实降级处理（可定位到来源类型，不可定位页）。
type SourceAnchor struct {
	SourceType     string
	OffsetStart    *int
	OffsetEnd      *int
	LineStart      *int
	LineEnd        *int
	Sheet          string
	HeaderRow      *int
	RowStart       *int
	RowEnd         *int
	ColumnStart    *int
	ColumnEnd      *int
	ParagraphStart *int
	ParagraphEnd   *int
	TableIndex     *int
	SchemaVersion  *int
	Page           *int
	PageStart      *int
	PageEnd        *int
	BBox           *[4]float64
}

// SourceAnchorSchemaVersionV2 是 PDF 页锚点引入的 schema 版本。
const SourceAnchorSchemaVersionV2 = 2

// HasPDFPageAnchor 报告锚点是否携带 PDF 物理页信息（v2）。
func (a SourceAnchor) HasPDFPageAnchor() bool {
	return a.Page != nil || (a.PageStart != nil && a.PageEnd != nil)
}

// Validate 校验来源锚点中的范围与坐标。
func (a SourceAnchor) Validate() error {
	for name, coordinate := range map[string]*int{
		"offset_start": a.OffsetStart, "offset_end": a.OffsetEnd,
	} {
		if coordinate != nil && *coordinate < 0 {
			return fmt.Errorf("%w: source_anchor.%s 不能为负数", domainerrors.ErrValidation, name)
		}
	}
	for name, coordinate := range map[string]*int{
		"line_start": a.LineStart, "line_end": a.LineEnd,
		"header_row": a.HeaderRow, "row_start": a.RowStart, "row_end": a.RowEnd,
		"column_start": a.ColumnStart, "column_end": a.ColumnEnd,
		"paragraph_start": a.ParagraphStart, "paragraph_end": a.ParagraphEnd,
		"table_index": a.TableIndex,
	} {
		if coordinate != nil && *coordinate <= 0 {
			return fmt.Errorf("%w: source_anchor.%s 必须大于 0", domainerrors.ErrValidation, name)
		}
	}
	for _, pair := range []struct {
		name       string
		start, end *int
	}{
		{name: "offset", start: a.OffsetStart, end: a.OffsetEnd},
		{name: "line", start: a.LineStart, end: a.LineEnd},
		{name: "row", start: a.RowStart, end: a.RowEnd},
		{name: "column", start: a.ColumnStart, end: a.ColumnEnd},
		{name: "paragraph", start: a.ParagraphStart, end: a.ParagraphEnd},
	} {
		if pair.start != nil && pair.end != nil && *pair.end < *pair.start {
			return fmt.Errorf("%w: source_anchor.%s_end 不能小于 start", domainerrors.ErrValidation, pair.name)
		}
	}
	for name, coordinate := range map[string]*int{
		"page": a.Page, "page_start": a.PageStart, "page_end": a.PageEnd,
	} {
		if coordinate != nil && *coordinate < 1 {
			return fmt.Errorf("%w: source_anchor.%s 必须从 1 开始", domainerrors.ErrValidation, name)
		}
	}
	if a.PageStart != nil && a.PageEnd != nil && *a.PageEnd < *a.PageStart {
		return fmt.Errorf("%w: source_anchor.page_end 不能小于 page_start", domainerrors.ErrValidation)
	}
	if a.SchemaVersion != nil && *a.SchemaVersion != SourceAnchorSchemaVersionV2 &&
		*a.SchemaVersion != 1 {
		return fmt.Errorf("%w: source_anchor.schema_version 不支持", domainerrors.ErrValidation)
	}
	if a.BBox != nil {
		box := *a.BBox
		for _, coordinate := range box {
			if !isFiniteFloat(coordinate) || coordinate < 0 {
				return fmt.Errorf("%w: source_anchor.bbox 坐标必须为非负有限数", domainerrors.ErrValidation)
			}
		}
		if box[0] > box[2] || box[1] > box[3] {
			return fmt.Errorf("%w: source_anchor.bbox 顺序无效", domainerrors.ErrValidation)
		}
	}
	return nil
}

// isFiniteFloat 报告浮点数非 NaN/Inf。
func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// MergeSourceAnchors 合并来自相同文档结构位置的两个连续范围。
func MergeSourceAnchors(left, right SourceAnchor) (SourceAnchor, error) {
	if err := left.Validate(); err != nil {
		return SourceAnchor{}, err
	}
	if err := right.Validate(); err != nil {
		return SourceAnchor{}, err
	}
	if strings.TrimSpace(left.SourceType) != strings.TrimSpace(right.SourceType) ||
		left.Sheet != right.Sheet || !equalInt(left.HeaderRow, right.HeaderRow) || !equalInt(left.TableIndex, right.TableIndex) ||
		!equalInt(left.SchemaVersion, right.SchemaVersion) {
		return SourceAnchor{}, fmt.Errorf("%w: 不能合并不同来源结构的锚点", domainerrors.ErrValidation)
	}
	if !anchorsContiguous(left, right) {
		return SourceAnchor{}, fmt.Errorf("%w: 不能合并不连续的来源锚点", domainerrors.ErrValidation)
	}
	merged := SourceAnchor{
		SourceType:     left.SourceType,
		OffsetStart:    minPointer(left.OffsetStart, right.OffsetStart),
		OffsetEnd:      maxPointer(left.OffsetEnd, right.OffsetEnd),
		LineStart:      minPointer(left.LineStart, right.LineStart),
		LineEnd:        maxPointer(left.LineEnd, right.LineEnd),
		Sheet:          left.Sheet,
		HeaderRow:      cloneInt(left.HeaderRow),
		RowStart:       minPointer(left.RowStart, right.RowStart),
		RowEnd:         maxPointer(left.RowEnd, right.RowEnd),
		ColumnStart:    minPointer(left.ColumnStart, right.ColumnStart),
		ColumnEnd:      maxPointer(left.ColumnEnd, right.ColumnEnd),
		ParagraphStart: minPointer(left.ParagraphStart, right.ParagraphStart),
		ParagraphEnd:   maxPointer(left.ParagraphEnd, right.ParagraphEnd),
		TableIndex:     cloneInt(left.TableIndex),
		SchemaVersion:  cloneInt(left.SchemaVersion),
	}
	// PDF v2：合并页范围；跨页合并后 Page/BBox 单块信息不再成立，按合同丢弃。
	if left.HasPDFPageAnchor() || right.HasPDFPageAnchor() {
		merged.Page = nil
		merged.BBox = nil
		merged.PageStart = minPointer(pageOf(left), pageOf(right))
		merged.PageEnd = maxPointer(endPageOf(left), endPageOf(right))
	}
	return merged, nil
}

// pageOf 返回锚点的起始物理页（单块取 Page，范围取 PageStart；缺失为 nil）。
func pageOf(anchor SourceAnchor) *int {
	if anchor.Page != nil {
		return cloneInt(anchor.Page)
	}
	return cloneInt(anchor.PageStart)
}

// endPageOf 返回锚点的结束物理页（范围取 PageEnd，单块取 Page）。
func endPageOf(anchor SourceAnchor) *int {
	if anchor.PageEnd != nil {
		return cloneInt(anchor.PageEnd)
	}
	return cloneInt(anchor.Page)
}

func anchorsContiguous(left, right SourceAnchor) bool {
	if left.RowStart != nil && left.RowEnd != nil && right.RowStart != nil && right.RowEnd != nil {
		return *right.RowStart >= *left.RowStart && *right.RowStart <= *left.RowEnd+1
	}
	if left.OffsetStart != nil && left.OffsetEnd != nil && right.OffsetStart != nil && right.OffsetEnd != nil {
		return *right.OffsetStart >= *left.OffsetStart
	}
	if left.ParagraphStart != nil && left.ParagraphEnd != nil && right.ParagraphStart != nil && right.ParagraphEnd != nil {
		return *right.ParagraphStart >= *left.ParagraphStart
	}
	if left.LineStart != nil && left.LineEnd != nil && right.LineStart != nil && right.LineEnd != nil {
		return *right.LineStart >= *left.LineStart
	}
	return true
}

func equalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func minPointer(left, right *int) *int {
	if left == nil {
		return cloneInt(right)
	}
	if right == nil || *left <= *right {
		return cloneInt(left)
	}
	return cloneInt(right)
}

func maxPointer(left, right *int) *int {
	if left == nil {
		return cloneInt(right)
	}
	if right == nil || *left >= *right {
		return cloneInt(left)
	}
	return cloneInt(right)
}
