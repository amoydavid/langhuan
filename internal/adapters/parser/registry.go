// Package parser provides shared parser routing and manifest construction.
package parser

import (
	"context"
	"fmt"
	"strings"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// Registration binds one normalized file type to a parser.
type Registration struct {
	FileType string
	Parser   parserport.DocumentParser
}

// Registry routes parsing requests by normalized file type.
type Registry struct {
	parsers map[string]parserport.DocumentParser
}

var _ parserport.DocumentParser = (*Registry)(nil)

// NewRegistry constructs a registry and rejects duplicate normalized types.
func NewRegistry(registrations ...Registration) (*Registry, error) {
	registry := &Registry{parsers: make(map[string]parserport.DocumentParser, len(registrations))}
	for _, registration := range registrations {
		fileType := normalizeFileType(registration.FileType)
		if fileType == "" {
			return nil, fmt.Errorf("register parser: file type is empty")
		}
		if registration.Parser == nil {
			return nil, fmt.Errorf("register parser %q: parser is nil", fileType)
		}
		if !registration.Parser.Supports(fileType) {
			return nil, fmt.Errorf("register parser %q: parser does not support file type", fileType)
		}
		if _, exists := registry.parsers[fileType]; exists {
			return nil, fmt.Errorf("register parser %q: duplicate file type", fileType)
		}
		registry.parsers[fileType] = registration.Parser
	}
	return registry, nil
}

// Supports reports whether a parser is registered for fileType.
func (r *Registry) Supports(fileType string) bool {
	if r == nil {
		return false
	}
	_, ok := r.parsers[normalizeFileType(fileType)]
	return ok
}

// Parse routes input to its registered parser.
func (r *Registry) Parse(ctx context.Context, input parserport.ParseInput) (*parserport.ParsedDocument, error) {
	fileType := normalizeFileType(input.FileType)
	if r == nil {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, fileType)
	}
	registered, ok := r.parsers[fileType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", parserport.ErrUnsupportedFileType, fileType)
	}
	input.FileType = fileType
	return registered.Parse(ctx, input)
}

func normalizeFileType(fileType string) string {
	fileType = strings.ToLower(strings.TrimSpace(fileType))
	fileType = strings.TrimPrefix(fileType, ".")
	switch fileType {
	case "md", "markdown":
		return "markdown"
	default:
		return fileType
	}
}
