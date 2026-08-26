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
	// Content 为完整父块正文，仅 detail=full 返回；lean 档投影后置空。
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
	// Evidence 为该父块下得分最高的命中子块（含正文），仅 detail=lean 返回；
	// full 档投影后置空。装配阶段两档都填充，投影在最终截断后执行。
	Evidence *MatchedEvidence `json:"evidence,omitempty"`
	// KnowledgeBaseID/Name 为多知识库检索的来源归属；单库检索可为零值。
	KnowledgeBaseID   uuid.UUID `json:"knowledge_base_id,omitempty"`
	KnowledgeBaseName string    `json:"knowledge_base_name,omitempty"`
	// Evidence lineage：证据来自的 Document Revision 和 Index Generation。
	DocumentRevisionID uuid.UUID   `json:"document_revision_id,omitempty"`
	IndexGenerationID  uuid.UUID   `json:"index_generation_id,omitempty"`
	Citation           CitationRef `json:"citation"`
}

// MatchedChild is one retrievable child or flat chunk that contributed to a result.
// Content 是父块正文的子串（构造性冗余），v1.2.0 起不再进入 API 契约；
// 字段保留供服务内部装配 lean 档 Evidence 使用。
type MatchedChild struct {
	ChunkID         uuid.UUID       `json:"chunk_id"`
	ChunkRevisionID uuid.UUID       `json:"chunk_revision_id"`
	Role            value.ChunkRole `json:"role"`
	Content         string          `json:"-"`
	SourceAnchor    map[string]any  `json:"source_anchor"`
	Score           float64         `json:"score"`
	VectorScore     *float64        `json:"vector_score,omitempty"`
	KeywordScore    *float64        `json:"keyword_score,omitempty"`
}

// MatchedEvidence 是 lean 档返回的命中证据：最佳命中子块的完整信息。
type MatchedEvidence struct {
	ChunkID         uuid.UUID       `json:"chunk_id"`
	ChunkRevisionID uuid.UUID       `json:"chunk_revision_id"`
	Role            value.ChunkRole `json:"role"`
	Content         string          `json:"content"`
	SourceAnchor    map[string]any  `json:"source_anchor"`
	Score           float64         `json:"score"`
	VectorScore     *float64        `json:"vector_score,omitempty"`
	KeywordScore    *float64        `json:"keyword_score,omitempty"`
}

// ProjectSearchDetail 按响应档位投影检索结果（在最终排序与截断之后调用）：
// full 保留父块正文、去除 Evidence；lean 保留 Evidence（最佳命中子块正文）、
// 父块正文置空（chunk_id 仍可作为钻取句柄）。两档不改变行序与元数据。
func ProjectSearchDetail(results []*SearchResult, detail value.SearchResultDetail) {
	if detail == value.SearchDetailLean {
		for _, result := range results {
			if result == nil {
				continue
			}
			result.Content = ""
		}
		return
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		result.Evidence = nil
	}
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
	child := matchedChildFromEvidence(evidence, score, vectorScore, keywordScore)
	return &SearchResult{
		ChunkID: evidence.ChunkID, ChunkRevisionID: evidence.ChunkRevisionID,
		DocumentID: evidence.DocumentID, DocumentKind: evidence.DocumentKind,
		Content: evidence.Content, DocumentName: evidence.DocumentName,
		SourceAnchor: anchorMap, Score: score,
		VectorScore: vectorScore, KeywordScore: keywordScore,
		Metadata:           cloneDTOMap(evidence.Metadata),
		MatchedChildren:    []MatchedChild{child},
		Evidence:           MatchedEvidenceOf(child),
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

// MatchedEvidenceOf 把命中子块升格为 lean 档证据（字段同构直接转换）。
// 分组归并并按得分排序后，服务用 MatchedChildren[0] 重建 Evidence。
func MatchedEvidenceOf(child MatchedChild) *MatchedEvidence {
	evidence := MatchedEvidence(child)
	return &evidence
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
