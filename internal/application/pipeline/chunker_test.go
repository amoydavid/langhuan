package pipeline

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestChunkerRejectsFAQWithTypedValidationError(t *testing.T) {
	input := chunkerInput("FAQ", "回答", []model.ParsedBlock{{
		Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: len("回答"),
		SourceAnchor: value.SourceAnchor{SourceType: "faq"},
	}})
	input.Kind = value.DocumentKindFAQ

	_, _, err := NewChunker().Chunk(input, value.DefaultChunkingConfig())
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("Chunk error = %v, want ErrValidation", err)
	}
}

func TestCurrentStandardChunkerVersionIsThree(t *testing.T) {
	if CurrentStandardChunkerVersion != 3 {
		t.Fatalf("CurrentStandardChunkerVersion = %d, want 3", CurrentStandardChunkerVersion)
	}
}

func TestChunkerUsesUnicodeRunesAndOverlap(t *testing.T) {
	input := chunkerInput("中文.txt", "甲乙丙丁戊己庚辛", []model.ParsedBlock{{
		Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: len("甲乙丙丁戊己庚辛"),
		SourceAnchor: textAnchor(0, 8),
	}})
	chunks, revisions, err := NewChunker().Chunk(input, value.ChunkingConfig{ChunkSize: 4, ChunkOverlap: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"甲乙丙丁", "丁戊己庚", "庚辛"}
	if got := chunkContents(chunks); !reflect.DeepEqual(got, want) {
		t.Fatalf("contents = %#v, want %#v", got, want)
	}
	for index, chunk := range chunks {
		if chunk.Sequence != index || len([]rune(chunk.Content)) > 4 {
			t.Fatalf("chunk %d = %#v", index, chunk)
		}
		if revisions[index].ChunkID != chunk.ID {
			t.Fatalf("revision chunk_id = %s, want %s", revisions[index].ChunkID, chunk.ID)
		}
	}
}

func TestChunkerKeepsHeadingBoundaryAndBuildsContext(t *testing.T) {
	markdown := "# 指南\n\n第一节正文\n\n# 附录\n\n第二节正文"
	blocks := []model.ParsedBlock{
		{Sequence: 0, Kind: model.BlockKindHeading, NormalizedStart: 0, NormalizedEnd: len("# 指南"), HeadingPath: []string{"指南"}, SourceAnchor: textAnchor(0, 4)},
		{Sequence: 1, Kind: model.BlockKindParagraph, NormalizedStart: len("# 指南\n\n"), NormalizedEnd: len("# 指南\n\n第一节正文"), HeadingPath: []string{"指南"}, SourceAnchor: textAnchor(6, 11)},
		{Sequence: 2, Kind: model.BlockKindHeading, NormalizedStart: len("# 指南\n\n第一节正文\n\n"), NormalizedEnd: len("# 指南\n\n第一节正文\n\n# 附录"), HeadingPath: []string{"附录"}, SourceAnchor: textAnchor(13, 17)},
		{Sequence: 3, Kind: model.BlockKindParagraph, NormalizedStart: len("# 指南\n\n第一节正文\n\n# 附录\n\n"), NormalizedEnd: len(markdown), HeadingPath: []string{"附录"}, SourceAnchor: textAnchor(19, 24)},
	}
	input := chunkerInput("指南", markdown, blocks)
	chunks, _, err := NewChunker().Chunk(input, value.ChunkingConfig{ChunkSize: 64, ChunkOverlap: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Content != "# 指南\n\n第一节正文" || chunks[1].Content != "# 附录\n\n第二节正文" {
		t.Fatalf("chunks = %#v", chunkContents(chunks))
	}
	if chunks[0].ContextHeader != "指南" || chunks[1].ContextHeader != "指南 > 附录" {
		t.Fatalf("headers = %q / %q", chunks[0].ContextHeader, chunks[1].ContextHeader)
	}
	if chunks[1].EmbeddingContent != chunks[1].ContextHeader+"\n\n"+chunks[1].Content {
		t.Fatalf("embedding content = %q", chunks[1].EmbeddingContent)
	}
}

func TestChunkerDoesNotSkipContentInsideOversizedHeading(t *testing.T) {
	headingRunes := make([]rune, 600)
	for index := range headingRunes {
		headingRunes[index] = rune(0x4E00 + index)
	}
	heading := string(headingRunes)
	input := chunkerInput("超长标题", heading, []model.ParsedBlock{{
		Sequence: 0, Kind: model.BlockKindHeading,
		NormalizedStart: 0, NormalizedEnd: len(heading),
		HeadingPath: []string{"超长标题"}, SourceAnchor: textAnchor(0, len(headingRunes)),
	}})

	chunks, _, err := NewChunker().Chunk(input, value.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 80})
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range headingRunes {
		covered := false
		for _, chunk := range chunks {
			if strings.ContainsRune(chunk.Content, want) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("heading rune %d was not emitted", index)
		}
	}
}

func TestChunkerRepeatsTableHeaderAndMarksOversizedRow(t *testing.T) {
	header := "| 名称 | 值 |\n| --- | --- |"
	row1 := "| A | 1 |"
	row2 := "| 很长很长很长很长很长 | 2 |"
	markdown := header + "\n\n" + row1 + "\n\n" + row2
	headerRow, rowTwo, rowThree, columnOne, columnTwo := 1, 2, 3, 1, 2
	blocks := []model.ParsedBlock{
		{Sequence: 0, Kind: model.BlockKindTableHeader, NormalizedStart: 0, NormalizedEnd: len(header), Metadata: map[string]any{"table_id": "t1"}, SourceAnchor: value.SourceAnchor{SourceType: "csv", Sheet: "CSV", HeaderRow: &headerRow, ColumnStart: &columnOne, ColumnEnd: &columnTwo}},
		{Sequence: 1, Kind: model.BlockKindTableRow, NormalizedStart: len(header + "\n\n"), NormalizedEnd: len(header + "\n\n" + row1), Metadata: map[string]any{"table_id": "t1"}, SourceAnchor: value.SourceAnchor{SourceType: "csv", Sheet: "CSV", HeaderRow: &headerRow, RowStart: &rowTwo, RowEnd: &rowTwo, ColumnStart: &columnOne, ColumnEnd: &columnTwo}},
		{Sequence: 2, Kind: model.BlockKindTableRow, NormalizedStart: len(header + "\n\n" + row1 + "\n\n"), NormalizedEnd: len(markdown), Metadata: map[string]any{"table_id": "t1"}, SourceAnchor: value.SourceAnchor{SourceType: "csv", Sheet: "CSV", HeaderRow: &headerRow, RowStart: &rowThree, RowEnd: &rowThree, ColumnStart: &columnOne, ColumnEnd: &columnTwo}},
	}
	chunks, _, err := NewChunker().Chunk(chunkerInput("表格", markdown, blocks), value.ChunkingConfig{ChunkSize: 36, ChunkOverlap: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %#v", chunkContents(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk.Content) < len(header) || chunk.Content[:len(header)] != header {
			t.Fatalf("table header missing: %q", chunk.Content)
		}
	}
	if chunks[1].Metadata["oversized"] != true {
		t.Fatalf("oversized metadata = %#v", chunks[1].Metadata)
	}
	if chunks[1].SourceAnchor.RowStart == nil || *chunks[1].SourceAnchor.RowStart != 3 {
		t.Fatalf("anchor = %#v", chunks[1].SourceAnchor)
	}
}

func TestChunkerIsDeterministicForSemanticFields(t *testing.T) {
	markdown := "第一句。第二句。第三句。第四句。"
	input := chunkerInput("文档", markdown, []model.ParsedBlock{{
		Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: len(markdown), SourceAnchor: textAnchor(0, len([]rune(markdown))),
	}})
	first, _, err := NewChunker().Chunk(input, value.ChunkingConfig{ChunkSize: 8, ChunkOverlap: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := NewChunker().Chunk(input, value.ChunkingConfig{ChunkSize: 8, ChunkOverlap: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("chunk counts = %d / %d", len(first), len(second))
	}
	for index := range first {
		left, right := first[index], second[index]
		if left.Sequence != right.Sequence || left.Content != right.Content || left.ContextHeader != right.ContextHeader ||
			left.EmbeddingContent != right.EmbeddingContent || !reflect.DeepEqual(left.SourceAnchor, right.SourceAnchor) || !reflect.DeepEqual(left.Metadata, right.Metadata) {
			t.Fatalf("chunk %d differs: %#v / %#v", index, left, right)
		}
	}
}

func chunkerInput(title, markdown string, blocks []model.ParsedBlock) ChunkInput {
	return ChunkInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), DocumentID: uuid.New(),
		DocumentRevisionID: uuid.New(), ChunkSetID: uuid.New(), Kind: value.DocumentKindFile,
		Title: title, Markdown: markdown,
		Manifest: model.ParseManifest{
			Version: model.CurrentParseManifestVersion, Parser: "text", ParserVersion: 1, Blocks: blocks,
		},
	}
}

func textAnchor(start, end int) value.SourceAnchor {
	return value.SourceAnchor{SourceType: "txt", OffsetStart: &start, OffsetEnd: &end}
}

func chunkContents(chunks []*model.Chunk) []string {
	result := make([]string, len(chunks))
	for index, chunk := range chunks {
		result[index] = chunk.Content
	}
	return result
}
