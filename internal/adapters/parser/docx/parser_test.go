package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/dajee/langhuan/internal/domain/model"
	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func TestParserExtractsStructureAnchorsAndWarnings(t *testing.T) {
	got, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "docx", Content: structuredFixture(t)})
	if err != nil {
		t.Fatal(err)
	}
	want := "# 指南\n\n正文\n\n- 项目一\n  - 子项目\n\n| 名称 | 值 |\n| --- | --- |\n\n| 琅嬛<br>服务 | 1 |\n\n## 深入\n\n结尾"
	if got.Markdown != want {
		t.Fatalf("Markdown = %q, want %q", got.Markdown, want)
	}
	wantKinds := []model.BlockKind{
		model.BlockKindHeading, model.BlockKindParagraph, model.BlockKindList,
		model.BlockKindTableHeader, model.BlockKindTableRow,
		model.BlockKindHeading, model.BlockKindParagraph,
	}
	if len(got.Manifest.Blocks) != len(wantKinds) {
		t.Fatalf("blocks = %#v", got.Manifest.Blocks)
	}
	for index, kind := range wantKinds {
		if got.Manifest.Blocks[index].Kind != kind {
			t.Fatalf("block %d kind = %q, want %q", index, got.Manifest.Blocks[index].Kind, kind)
		}
	}
	listAnchor := got.Manifest.Blocks[2].SourceAnchor
	if listAnchor.ParagraphStart == nil || *listAnchor.ParagraphStart != 3 || listAnchor.ParagraphEnd == nil || *listAnchor.ParagraphEnd != 4 {
		t.Fatalf("list anchor = %#v", listAnchor)
	}
	tableAnchor := got.Manifest.Blocks[4].SourceAnchor
	if tableAnchor.TableIndex == nil || *tableAnchor.TableIndex != 1 || tableAnchor.HeaderRow == nil || *tableAnchor.HeaderRow != 1 ||
		tableAnchor.RowStart == nil || *tableAnchor.RowStart != 2 || tableAnchor.ColumnEnd == nil || *tableAnchor.ColumnEnd != 2 {
		t.Fatalf("table anchor = %#v", tableAnchor)
	}
	if len(got.Manifest.Warnings) != 1 || got.Manifest.Warnings[0].Code != "unsupported_image" {
		t.Fatalf("warnings = %#v", got.Manifest.Warnings)
	}
	if got.Manifest.Blocks[5].HeadingPath[0] != "指南" || got.Manifest.Blocks[5].HeadingPath[1] != "深入" {
		t.Fatalf("heading path = %#v", got.Manifest.Blocks[5].HeadingPath)
	}
}

func TestParserRejectsBrokenMissingBodyUnsupportedAndCanceled(t *testing.T) {
	p := New()
	if _, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "docx", Content: []byte("bad")}); !errors.Is(err, parserport.ErrInvalidDocument) {
		t.Fatalf("broken error = %v", err)
	}
	if _, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "docx", Content: zipFixture(t, map[string]string{"word/styles.xml": "<styles/>"})}); !errors.Is(err, parserport.ErrInvalidDocument) {
		t.Fatalf("missing body error = %v", err)
	}
	if _, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "docx", Content: zipFixture(t, map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body/></w:document>`})}); !errors.Is(err, parserport.ErrEmptyDocument) {
		t.Fatalf("empty body error = %v", err)
	}
	if _, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "docx", Content: zipFixture(t, map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p>`})}); !errors.Is(err, parserport.ErrInvalidDocument) {
		t.Fatalf("malformed body error = %v", err)
	}
	if _, err := p.Parse(context.Background(), parserport.ParseInput{FileType: "txt", Content: structuredFixture(t)}); !errors.Is(err, parserport.ErrUnsupportedFileType) {
		t.Fatalf("unsupported error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Parse(ctx, parserport.ParseInput{FileType: "docx", Content: structuredFixture(t)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestParserSeparatesAdjacentNumberingDefinitions(t *testing.T) {
	content := zipFixture(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="w"><w:body>
<w:p><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>列表一</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:numId w:val="2"/></w:numPr></w:pPr><w:r><w:t>列表二</w:t></w:r></w:p>
</w:body></w:document>`,
	})
	got, err := New().Parse(context.Background(), parserport.ParseInput{FileType: "docx", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Manifest.Blocks) != 2 || got.Manifest.Blocks[0].Kind != model.BlockKindList || got.Manifest.Blocks[1].Kind != model.BlockKindList {
		t.Fatalf("blocks = %#v", got.Manifest.Blocks)
	}
}

func structuredFixture(t *testing.T) []byte {
	t.Helper()
	return zipFixture(t, map[string]string{
		"word/styles.xml":    `<?xml version="1.0"?><w:styles xmlns:w="w"><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/></w:style></w:styles>`,
		"word/numbering.xml": `<?xml version="1.0"?><w:numbering xmlns:w="w"><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`,
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="w" xmlns:a="a"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>指南</w:t></w:r></w:p>
<w:p><w:r><w:t>正文</w:t></w:r><w:r><w:drawing><a:graphic/></w:drawing></w:r><w:r><w:drawing><a:graphic/></w:drawing></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>项目一</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>子项目</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>名称</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>值</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>琅嬛</w:t></w:r></w:p><w:p><w:r><w:t>服务</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>1</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
<w:p><w:pPr><w:outlineLvl w:val="1"/></w:pPr><w:r><w:t>深入</w:t></w:r></w:p>
<w:p><w:r><w:t>结尾</w:t></w:r></w:p>
</w:body></w:document>`,
	})
}

func zipFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
