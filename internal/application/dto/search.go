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
	RerankScore     *float64           `json:"rerank_score,omitempty"`
	RankingStage    value.RankingStage `json:"ranking_stage"`
	Metadata        map[string]any     `json:"metadata"`
	MatchedChildren []MatchedChild     `json:"matched_children"`
	// KnowledgeBaseID/Name 为多知识库检索的来源归属；单库检索可为零值。
	KnowledgeBaseID   uuid.UUID `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string    `json:"knowledge_base_name,omitempty"`
}

// MatchedChild is one retrievable child or flat chunk that contributed to a result.
type MatchedChild struct {
	ChunkID         uuid.UUID       `json:"chunk_id"`
	ChunkRevisionID uuid.UUID       `json:"chunk_revision_id"`
	Role            value.ChunkRole `json:"role"`
	Content         string          `json:"content"`
	SourceAnchor    map[string]any  `json:"source_anchor"`
	Score           float64         `json:"score"`
	VectorScore     *float64        `json:"vector_score,omitempty"`
	KeywordScore    *float64        `json:"keyword_score,omitempty"`
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
		Metadata:        cloneDTOMap(evidence.Metadata),
		MatchedChildren: []MatchedChild{matchedChildFromEvidence(evidence, score, vectorScore, keywordScore)},
	}
}

func matchedChildFromEvidence(evidence indexport.SearchEvidence, score float64, vectorScore, keywordScore *float64) MatchedChild {
	chunkID, revisionID, content, anchor, role := evidence.MatchedChunkID, evidence.MatchedChunkRevisionID, evidence.MatchedContent, evidence.MatchedSourceAnchor, evidence.MatchedRole
	if chunkID == uuid.Nil {
		chunkID, revisionID, content, anchor, role = evidence.ChunkID, evidence.ChunkRevisionID, evidence.Content, evidence.SourceAnchor, value.ChunkRoleFlat
	}
	return MatchedChild{ChunkID: chunkID, ChunkRevisionID: revisionID, Role: role, Content: content, SourceAnchor: searchSourceAnchorMap(anchor), Score: score, VectorScore: vectorScore, KeywordScore: keywordScore}
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
