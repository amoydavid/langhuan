package parser

import (
	"context"
	"errors"

	"github.com/dajee/langhuan/internal/domain/model"
)

var (
	ErrUnsupportedFileType = errors.New("unsupported file type")
	ErrInvalidEncoding     = errors.New("invalid encoding")
	ErrInvalidDocument     = errors.New("invalid document")
	ErrEmptyDocument       = errors.New("empty document")
	ErrParseLimitExceeded  = errors.New("parse limit exceeded")
)

type ParseInput struct {
	FileType string
	Title    string
	Content  []byte
	Metadata map[string]any
}

type ParsedDocument struct {
	Markdown string
	Manifest model.ParseManifest
}

type DocumentParser interface {
	Parse(ctx context.Context, input ParseInput) (*ParsedDocument, error)
	Supports(fileType string) bool
}
