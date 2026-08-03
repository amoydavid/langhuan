package parser

import (
	"context"
	"errors"
	"testing"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

func TestRegistryRoutesNormalizedFileTypesAndMarkdownAlias(t *testing.T) {
	markdown := &registryTestParser{supported: "markdown"}
	registry, err := NewRegistry(
		Registration{FileType: "markdown", Parser: markdown},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Parse(context.Background(), parserport.ParseInput{FileType: ".MD"}); err != nil {
		t.Fatal(err)
	}
	if markdown.got.FileType != "markdown" {
		t.Fatalf("parser file type = %q, want markdown", markdown.got.FileType)
	}
	if !registry.Supports(".markdown") {
		t.Fatal("registry should support .markdown alias")
	}
}

func TestRegistryRejectsDuplicateNormalizedFileType(t *testing.T) {
	_, err := NewRegistry(
		Registration{FileType: "markdown", Parser: &registryTestParser{supported: "markdown"}},
		Registration{FileType: "md", Parser: &registryTestParser{supported: "markdown"}},
	)
	if err == nil {
		t.Fatal("NewRegistry() error = nil, want duplicate registration error")
	}
}

func TestRegistryReturnsTypedUnsupportedFileType(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Parse(context.Background(), parserport.ParseInput{FileType: "pdf"})
	if !errors.Is(err, parserport.ErrUnsupportedFileType) {
		t.Fatalf("Parse() error = %v, want ErrUnsupportedFileType", err)
	}
}

type registryTestParser struct {
	supported string
	got       parserport.ParseInput
}

func (p *registryTestParser) Parse(_ context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	p.got = input
	return &parserport.ParsedDocument{}, nil
}

func (p *registryTestParser) Supports(fileType string) bool {
	return fileType == p.supported
}
