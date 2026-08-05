package pipeline

import (
	"reflect"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestSelectChunkingStrategiesUsesHeadingThenFallbacksForStructuredDocuments(t *testing.T) {
	manifest := model.ParseManifest{Blocks: []model.ParsedBlock{{Kind: model.BlockKindHeading}}}
	got := selectChunkingStrategies(manifest, value.DefaultChunkingConfig())
	want := []value.ChunkingStrategy{
		value.ChunkingStrategyHeading,
		value.ChunkingStrategyHeuristic,
		value.ChunkingStrategyRecursive,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strategies = %#v, want %#v", got, want)
	}
}

func TestSelectChunkingStrategiesHonorsExplicitRecursive(t *testing.T) {
	config := value.DefaultChunkingConfig()
	config.Strategy = value.ChunkingStrategyRecursive
	got := selectChunkingStrategies(model.ParseManifest{}, config)
	if !reflect.DeepEqual(got, []value.ChunkingStrategy{value.ChunkingStrategyRecursive}) {
		t.Fatalf("strategies = %#v", got)
	}
}

func TestChunkerHonorsExplicitRecursiveStrategy(t *testing.T) {
	markdown := "# 第一章\n\n第一节正文\n\n# 第二章\n\n第二节正文"
	blocks := []model.ParsedBlock{
		{Sequence: 0, Kind: model.BlockKindHeading, NormalizedStart: 0, NormalizedEnd: len("# 第一章"), HeadingPath: []string{"第一章"}, SourceAnchor: textAnchor(0, 4)},
		{Sequence: 1, Kind: model.BlockKindParagraph, NormalizedStart: len("# 第一章\n\n"), NormalizedEnd: len("# 第一章\n\n第一节正文"), HeadingPath: []string{"第一章"}, SourceAnchor: textAnchor(6, 11)},
		{Sequence: 2, Kind: model.BlockKindHeading, NormalizedStart: len("# 第一章\n\n第一节正文\n\n"), NormalizedEnd: len("# 第一章\n\n第一节正文\n\n# 第二章"), HeadingPath: []string{"第二章"}, SourceAnchor: textAnchor(13, 17)},
		{Sequence: 3, Kind: model.BlockKindParagraph, NormalizedStart: len("# 第一章\n\n第一节正文\n\n# 第二章\n\n"), NormalizedEnd: len(markdown), HeadingPath: []string{"第二章"}, SourceAnchor: textAnchor(19, 24)},
	}
	config := value.ChunkingConfig{Strategy: value.ChunkingStrategyRecursive, ChunkSize: 64, ChunkOverlap: 8}
	chunks, _, err := NewChunker().Chunk(chunkerInput("指南", markdown, blocks), config)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].Content != markdown {
		t.Fatalf("recursive chunks = %#v", chunkContents(chunks))
	}
}

func TestChunkerHeuristicStrategyFindsUnmarkedSectionBoundary(t *testing.T) {
	markdown := "第一章 安装\n\n安装正文。\n\n第二章 配置\n\n配置正文。"
	blocks := []model.ParsedBlock{
		{Sequence: 0, Kind: model.BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: len("第一章 安装"), SourceAnchor: textAnchor(0, 5)},
		{Sequence: 1, Kind: model.BlockKindParagraph, NormalizedStart: len("第一章 安装\n\n"), NormalizedEnd: len("第一章 安装\n\n安装正文。"), SourceAnchor: textAnchor(7, 12)},
		{Sequence: 2, Kind: model.BlockKindParagraph, NormalizedStart: len("第一章 安装\n\n安装正文。\n\n"), NormalizedEnd: len("第一章 安装\n\n安装正文。\n\n第二章 配置"), SourceAnchor: textAnchor(14, 19)},
		{Sequence: 3, Kind: model.BlockKindParagraph, NormalizedStart: len("第一章 安装\n\n安装正文。\n\n第二章 配置\n\n"), NormalizedEnd: len(markdown), SourceAnchor: textAnchor(21, 26)},
	}
	config := value.ChunkingConfig{Strategy: value.ChunkingStrategyHeuristic, ChunkSize: 64, ChunkOverlap: 8}
	chunks, _, err := NewChunker().Chunk(chunkerInput("指南", markdown, blocks), config)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Content != "第一章 安装\n\n安装正文。" || chunks[1].Content != "第二章 配置\n\n配置正文。" {
		t.Fatalf("heuristic chunks = %#v", chunkContents(chunks))
	}
}

func TestHeuristicSectionBoundaryMarkers(t *testing.T) {
	tests := []struct {
		name, content string
		want          bool
	}{
		{name: "中文编号章节", content: "第 2 章 配置", want: true},
		{name: "数字编号章节", content: "2.3 配置", want: true},
		{name: "全大写标题", content: "DEPLOYMENT CONFIGURATION", want: true},
		{name: "分页符", content: "\f下一页", want: true},
		{name: "分隔线", content: "-----", want: true},
		{name: "普通正文", content: "服务会在启动后读取配置文件。", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isHeuristicSectionBoundary(test.content); got != test.want {
				t.Fatalf("isHeuristicSectionBoundary(%q) = %v, want %v", test.content, got, test.want)
			}
		})
	}
}
