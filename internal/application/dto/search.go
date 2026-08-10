package dto

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
	indexport "github.com/dajee/langhuan/internal/ports/index"
)

// EvidenceContentSHA256 对返回的 content 按 UTF-8 字节计算 SHA-256，输出小写十六进制字符串。
// 该指纹只覆盖 API 返回的 content，与 DocumentRevision.SHA256（原始资产指纹）不同。
func EvidenceContentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

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
	// Evidence lineage：证据来自的 Document Revision 和 Index Generation。
	DocumentRevisionID uuid.UUID   `json:"document_revision_id,omitempty"`
	IndexGenerationID  uuid.UUID   `json:"index_generation_id,omitempty"`
	Citation           CitationRef `json:"citation"`
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

// CitationRef 是证据的可验证引用，包含 Revision lineage、来源锚点、内容指纹和可用状态。
// ContentSHA256 对 API 返回的 content 字段按 UTF-8 字节计算 SHA-256，输出小写十六进制字符串；
// 它与 DocumentRevision.SHA256（原始资产指纹）不同，不得混用。
type CitationRef struct {
	DocumentRevisionID uuid.UUID            `json:"document_revision_id"`
	ChunkRevisionID    uuid.UUID            `json:"chunk_revision_id"`
	SourceAnchor       map[string]any       `json:"source_anchor"`
	ContentSHA256      string               `json:"content_sha256"`
	Status             value.CitationStatus `json:"status"`
}

// SearchResultFromEvidence combines current evidence with RRF, branch scores and Generation lineage.
func SearchResultFromEvidence(
	evidence indexport.SearchEvidence,
	generationID uuid.UUID,
	score float64,
	vectorScore, keywordScore *float64,
) *SearchResult {
	anchorMap := searchSourceAnchorMap(evidence.SourceAnchor)
	return &SearchResult{
		ChunkID: evidence.ChunkID, ChunkRevisionID: evidence.ChunkRevisionID,
		DocumentID: evidence.DocumentID, DocumentKind: evidence.DocumentKind,
		Content: evidence.Content, DocumentName: evidence.DocumentName,
		SourceAnchor: anchorMap, Score: score,
		VectorScore: vectorScore, KeywordScore: keywordScore,
		Metadata:           cloneDTOMap(evidence.Metadata),
		MatchedChildren:    []MatchedChild{matchedChildFromEvidence(evidence, score, vectorScore, keywordScore)},
		DocumentRevisionID: evidence.DocumentRevisionID,
		IndexGenerationID:  generationID,
		Citation: CitationRef{
			DocumentRevisionID: evidence.DocumentRevisionID,
			ChunkRevisionID:    evidence.ChunkRevisionID,
			SourceAnchor:       anchorMap,
			ContentSHA256:      EvidenceContentSHA256(evidence.Content),
			Status:             value.CitationStatusValid,
		},
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
