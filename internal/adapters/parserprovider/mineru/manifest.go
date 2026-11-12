package mineru

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	markdownparser "github.com/dajee/langhuan/internal/adapters/parser/markdown"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// mineruContentEntry 是 content_list.json 的单条目（仅取页锚点所需字段；
// 未知字段按 MinerU 产物宽容处理，content_list 非契约冻结对象）。
type mineruContentEntry struct {
	// Type 是条目类型（text/image/table/equation 等）；页对齐只消费携带正文的条目。
	Type string `json:"type"`
	// Text 是条目正文（text/equation 条目）；image/table 条目可能缺省。
	Text string `json:"text"`
	// PageIdx 是 0-based 物理页（对齐后 +1 转为合同要求的 1-based）。
	PageIdx *int `json:"page_idx"`
	// BBox 是条目在页面坐标系中的外接框 [x0, y0, x1, y1]（可缺省）。
	BBox []float64 `json:"bbox"`
}

// buildParsedDocument 把 MinerU 产出的 Markdown 重建为结构化 ParsedDocument。
// 复用 Markdown parser 识别标题、表格与代码块，确保 PDF 与其它文档进入
// 同一分块器时拥有一致的结构边界；随后将来源锚点标记为 PDF。
// contentListJSON 是 zip 内 content_list.json 原文（可为空）——非空时按 C03
// 为文本块附加物理页锚点（schema_version=2，1-based）。
//
// 对齐规则（顺序单游标）：MinerU 的 markdown 与 content_list 同序生成，
// 带正文条目与顶层文本块一一对应；块文本与条目文本精确比对（归一化空白），
// 命中才赋页并推进游标；首次失配即停止对齐——后续页码会整体错位，宁可
// 诚实降级（缺页锚点）也不臆造页码，并以解析 warning 披露。
func buildParsedDocument(
	ctx context.Context, markdown, modelVersion, contentListJSON string,
) (*parserport.ParsedDocument, error) {
	markdown = trimWhitespace(markdown)
	if markdown == "" {
		return nil, fmt.Errorf("MinerU 返回空 Markdown")
	}

	parsed, err := markdownparser.New().Parse(ctx, parserport.ParseInput{FileType: "markdown", Content: []byte(markdown)})
	if err != nil {
		return nil, fmt.Errorf("MinerU Markdown 结构化解析失败: %w", err)
	}
	for index := range parsed.Manifest.Blocks {
		parsed.Manifest.Blocks[index].SourceAnchor.SourceType = "pdf"
	}
	applyPageAnchors(&parsed.Manifest, parsed.Markdown, parseMineruContentList(contentListJSON))
	parsed.Manifest.Parser = "pdf"
	parsed.Manifest.ParserVersion = 2
	return parsed, nil
}

// applyPageAnchors 把 content_list 条目的物理页对齐到 manifest 文本块。
// markdown 是 builder 重建文本（块 span 的索引基准）。
func applyPageAnchors(manifest *model.ParseManifest, markdown string, entries []mineruContentEntry) {
	if len(entries) == 0 {
		return
	}
	cursor := 0
	for index := range manifest.Blocks {
		block := &manifest.Blocks[index]
		text := normalizeForMatch(blockText(*block, markdown))
		if text == "" {
			continue
		}
		for cursor < len(entries) && !entryHasText(entries[cursor]) {
			cursor++
		}
		if cursor >= len(entries) {
			return
		}
		entry := entries[cursor]
		if normalizeForMatch(entry.Text) != text {
			manifest.Warnings = append(manifest.Warnings, model.ParseWarning{
				Code:    "page_anchor_misaligned",
				Message: fmt.Sprintf("block %d 与 content_list 条目文本不一致，停止页锚点对齐", index),
			})
			return
		}
		if entry.PageIdx != nil && *entry.PageIdx >= 0 {
			block.SourceAnchor.SchemaVersion = ptrInt(value.SourceAnchorSchemaVersionV2)
			block.SourceAnchor.Page = ptrInt(*entry.PageIdx + 1)
			block.SourceAnchor.BBox = bboxOf(entry)
		}
		cursor++
	}
}

// blockText 取块的精确文本（builder 重建的 Markdown 与块 span 一一对应）。
func blockText(block model.ParsedBlock, markdown string) string {
	if block.NormalizedStart < 0 || block.NormalizedEnd > len(markdown) ||
		block.NormalizedStart > block.NormalizedEnd {
		return ""
	}
	return markdown[block.NormalizedStart:block.NormalizedEnd]
}

// entryHasText 报告条目是否携带可对齐正文。
func entryHasText(entry mineruContentEntry) bool {
	return strings.TrimSpace(entry.Text) != ""
}

// normalizeForMatch 折叠空白便于比对。
func normalizeForMatch(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

// parseMineruContentList 解析 content_list.json；空输入或解析失败返回 nil
// （页锚点降级，不阻断解析）。
func parseMineruContentList(raw string) []mineruContentEntry {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var entries []mineruContentEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}
	return entries
}

// bboxOf 把 MinerU bbox 转为锚点外接框（长度不为 4 或含负值时丢弃）。
func bboxOf(entry mineruContentEntry) *[4]float64 {
	if len(entry.BBox) != 4 {
		return nil
	}
	for _, coordinate := range entry.BBox {
		if coordinate < 0 {
			return nil
		}
	}
	box := [4]float64{entry.BBox[0], entry.BBox[1], entry.BBox[2], entry.BBox[3]}
	return &box
}

func ptrInt(value int) *int {
	copied := value
	return &copied
}

func trimWhitespace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
