package mineru

import (
	"context"
	"fmt"

	markdownparser "github.com/dajee/langhuan/internal/adapters/parser/markdown"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// buildParsedDocument 把 MinerU 产出的 Markdown 重建为结构化 ParsedDocument。
// 复用 Markdown parser 识别标题、表格与代码块，确保 PDF 与其它文档进入
// 同一分块器时拥有一致的结构边界；随后将来源锚点标记为 PDF。
func buildParsedDocument(ctx context.Context, markdown, modelVersion string) (*parserport.ParsedDocument, error) {
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
	parsed.Manifest.Parser = "pdf"
	parsed.Manifest.ParserVersion = 1
	return parsed, nil
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
