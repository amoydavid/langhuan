package model

import (
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestParseManifestValidateAcceptsOrderedSpans(t *testing.T) {
	manifest := ParseManifest{
		Version:       CurrentParseManifestVersion,
		Parser:        "text",
		ParserVersion: 1,
		Blocks: []ParsedBlock{{
			Sequence:        0,
			Kind:            BlockKindParagraph,
			NormalizedStart: 0,
			NormalizedEnd:   len("你好"),
			SourceAnchor:    value.SourceAnchor{SourceType: "txt"},
		}},
	}
	if err := manifest.Validate("你好"); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestParseManifestValidateRejectsOverlappingSpans(t *testing.T) {
	manifest := ParseManifest{
		Version:       CurrentParseManifestVersion,
		Parser:        "text",
		ParserVersion: 1,
		Blocks: []ParsedBlock{
			{Sequence: 0, Kind: BlockKindParagraph, NormalizedStart: 0, NormalizedEnd: 4},
			{Sequence: 1, Kind: BlockKindParagraph, NormalizedStart: 3, NormalizedEnd: 5},
		},
	}
	if err := manifest.Validate("abcde"); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("Validate() error = %v, want validation", err)
	}
}

func TestParseManifestValidateRejectsUnknownVersion(t *testing.T) {
	manifest := ParseManifest{Version: 2, Parser: "text", ParserVersion: 1}
	if err := manifest.Validate(""); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("Validate() error = %v, want validation", err)
	}
}
