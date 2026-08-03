package dto

import (
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// SearchResult is one fused evidence item; Content is the return projection, not search_content.
type SearchResult struct {
	ChunkID         uuid.UUID          `json:"chunk_id"`
	ChunkRevisionID uuid.UUID          `json:"chunk_revision_id"`
	DocumentID      uuid.UUID          `json:"document_id"`
	DocumentKind    value.DocumentKind `json:"document_kind"`
	Content         string             `json:"content"`
	DocumentName    string             `json:"document_name"`
	SourceAnchor    map[string]any     `json:"source_anchor"`
	Score           float64            `json:"score"`
	VectorScore     *float64           `json:"vector_score,omitempty"`
	KeywordScore    *float64           `json:"keyword_score,omitempty"`
	Metadata        map[string]any     `json:"metadata"`
	// KnowledgeBaseID/Name 为多知识库检索的来源归属；单库检索可为零值。
	KnowledgeBaseID   uuid.UUID `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string    `json:"knowledge_base_name,omitempty"`
}

// SearchResultFromEvidence combines current evidence with RRF and branch scores.
func SearchResultFromEvidence(
	evidence indexport.SearchEvidence,
	score float64,
	vectorScore, keywordScore *float64,
) *SearchResult {
	return &SearchResult{
		ChunkID: evidence.ChunkID, ChunkRevisionID: evidence.ChunkRevisionID,
		DocumentID: evidence.DocumentID, DocumentKind: evidence.DocumentKind,
		Content: evidence.Content, DocumentName: evidence.DocumentName,
		SourceAnchor: searchSourceAnchorMap(evidence.SourceAnchor), Score: score,
		VectorScore: vectorScore, KeywordScore: keywordScore,
		Metadata: cloneDTOMap(evidence.Metadata),
	}
}

func searchSourceAnchorMap(anchor value.SourceAnchor) map[string]any {
	return map[string]any{
		"source_type":  anchor.SourceType,
		"offset_start": anchor.OffsetStart, "offset_end": anchor.OffsetEnd,
		"line_start": anchor.LineStart, "line_end": anchor.LineEnd,
		"sheet": anchor.Sheet, "header_row": anchor.HeaderRow,
		"row_start": anchor.RowStart, "row_end": anchor.RowEnd,
		"column_start": anchor.ColumnStart, "column_end": anchor.ColumnEnd,
		"paragraph_start": anchor.ParagraphStart, "paragraph_end": anchor.ParagraphEnd,
		"table_index": anchor.TableIndex,
	}
}
