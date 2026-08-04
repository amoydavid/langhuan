package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestDocumentRowMappingPreservesStableIdentityFields(t *testing.T) {
	now := time.Date(2026, 6, 17, 11, 0, 0, 0, time.UTC)
	activeRevisionID := uuid.New()
	doc := &model.Document{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		Kind: value.DocumentKindWeb, Title: "guide", SourceType: "crawl",
		SourceURI: "https://example.com/guide", Status: value.DocumentStatusPending,
		ActiveRevisionID: &activeRevisionID, Metadata: map[string]any{"source": "unit"},
		CreatedAt: now, UpdatedAt: now,
	}

	row, err := documentToRow(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := documentFromRow(row)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != doc.ID || got.WorkspaceID != doc.WorkspaceID || got.KnowledgeBaseID != doc.KnowledgeBaseID {
		t.Fatalf("identity not preserved: %#v", got)
	}
	if got.Kind != doc.Kind || got.SourceURI != doc.SourceURI || got.ActiveRevisionID == nil || *got.ActiveRevisionID != activeRevisionID {
		t.Fatalf("stable fields = %#v", got)
	}
	if !reflect.DeepEqual(got.Metadata, doc.Metadata) {
		t.Fatalf("metadata = %#v", got.Metadata)
	}
}

func TestParseManifestJSONMapRoundTrip(t *testing.T) {
	want := model.ParseManifest{
		Version:       model.CurrentParseManifestVersion,
		Parser:        "text",
		ParserVersion: 1,
		Blocks: []model.ParsedBlock{{
			Sequence:        0,
			Kind:            model.BlockKindParagraph,
			NormalizedStart: 0,
			NormalizedEnd:   4,
			SourceAnchor:    value.SourceAnchor{SourceType: "txt"},
		}},
	}
	raw, err := parseManifestToJSONMap(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseManifestFromJSONMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}

func TestParseManifestFromEmptyJSONMapKeepsDocumentUnparsed(t *testing.T) {
	got, err := parseManifestFromJSONMap(JSONMap{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, model.ParseManifest{}) {
		t.Fatalf("manifest = %#v, want zero value", got)
	}
}

// TestParseManifestPDFParserCodecRoundTrip 确保 MinerU PDF 解析产出的
// parser="pdf" manifest 能通过 codec 白名单（回归 KB：曾因 codec 白名单
// 漏掉 "pdf" 导致 CompleteParse 失败）。
func TestParseManifestPDFParserCodecRoundTrip(t *testing.T) {
	want := model.ParseManifest{
		Version:       model.CurrentParseManifestVersion,
		Parser:        "pdf",
		ParserVersion: 1,
		Blocks: []model.ParsedBlock{{
			Sequence:        0,
			Kind:            model.BlockKindParagraph,
			NormalizedStart: 0,
			NormalizedEnd:   3,
			SourceAnchor:    value.SourceAnchor{SourceType: "pdf"},
		}},
	}
	raw, err := parseManifestToJSONMap(want)
	if err != nil {
		t.Fatalf("encode pdf manifest failed: %v", err)
	}
	got, err := parseManifestFromJSONMap(raw)
	if err != nil {
		t.Fatalf("decode pdf manifest failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}

func TestParseManifestFromJSONMapRejectsUnknownVersion(t *testing.T) {
	_, err := parseManifestFromJSONMap(JSONMap{
		"version":        99,
		"parser":         "text",
		"parser_version": 1,
		"blocks": []any{map[string]any{
			"sequence":         0,
			"kind":             "paragraph",
			"normalized_start": 0,
			"normalized_end":   1,
			"source_anchor":    map[string]any{"source_type": "txt"},
		}},
	})
	if err == nil {
		t.Fatal("parseManifestFromJSONMap() error = nil, want unknown version error")
	}
}

func TestDocumentStatusRowMappingPreservesCompletedStatus(t *testing.T) {
	row := &DocumentRow{
		ID: uuid.New(), WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		Kind: string(value.DocumentKindFile), Title: "done.md", SourceType: "upload",
		Status: string(value.DocumentStatusCompleted), Metadata: JSONMap{},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}

	got, err := documentFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != value.DocumentStatusCompleted {
		t.Fatalf("status = %q, want %q", got.Status, value.DocumentStatusCompleted)
	}
}
