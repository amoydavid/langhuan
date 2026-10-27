package dto

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

func TestSearchResultMetadataFreshnessRules(t *testing.T) {
	updated := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	fetched := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)

	t.Run("两项俱备时都写入 RFC3339", func(t *testing.T) {
		result := SearchResultFromEvidence(indexport.SearchEvidence{
			EntryID: uuid.New(), ChunkID: uuid.New(), ChunkRevisionID: uuid.New(),
			DocumentID: uuid.New(), DocumentKind: value.DocumentKindWeb,
			DocumentUpdatedAt: &updated, SourceLastFetchAt: &fetched,
		}, uuid.New(), 0.5, nil, nil)
		if result.Metadata["document_updated_at"] != "2026-10-20T08:00:00Z" {
			t.Fatalf("document_updated_at = %v", result.Metadata["document_updated_at"])
		}
		if result.Metadata["source_last_fetch_at"] != "2026-10-27T08:00:00Z" {
			t.Fatalf("source_last_fetch_at = %v", result.Metadata["source_last_fetch_at"])
		}
	})

	t.Run("非 web 文档省略 source_last_fetch_at", func(t *testing.T) {
		result := SearchResultFromEvidence(indexport.SearchEvidence{
			EntryID: uuid.New(), ChunkID: uuid.New(), ChunkRevisionID: uuid.New(),
			DocumentID: uuid.New(), DocumentKind: value.DocumentKindFAQ,
			DocumentUpdatedAt: &updated,
		}, uuid.New(), 0.5, nil, nil)
		if _, ok := result.Metadata["source_last_fetch_at"]; ok {
			t.Fatal("非 web 文档不应有 source_last_fetch_at")
		}
		if result.Metadata["document_updated_at"] != "2026-10-20T08:00:00Z" {
			t.Fatalf("document_updated_at = %v", result.Metadata["document_updated_at"])
		}
	})

	t.Run("不可得时两项都省略且不改写既有 metadata", func(t *testing.T) {
		result := SearchResultFromEvidence(indexport.SearchEvidence{
			EntryID: uuid.New(), ChunkID: uuid.New(), ChunkRevisionID: uuid.New(),
			DocumentID: uuid.New(), DocumentKind: value.DocumentKindFile,
			Metadata: map[string]any{"heading": "安装"},
		}, uuid.New(), 0.5, nil, nil)
		if _, ok := result.Metadata["document_updated_at"]; ok {
			t.Fatal("不可得时不应有 document_updated_at")
		}
		if result.Metadata["heading"] != "安装" {
			t.Fatalf("既有 metadata 被改写: %v", result.Metadata)
		}
	})
}
