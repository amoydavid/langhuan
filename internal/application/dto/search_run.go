package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchResponse 是应用层统一返回的检索响应，包含运行元数据和结果。
type SearchResponse struct {
	Run     SearchRunSummary `json:"run"`
	Results []*SearchResult  `json:"results"`
}

// SearchRunSummary 是一次检索运行的摘要元数据，用于协议返回。
type SearchRunSummary struct {
	SearchID                  uuid.UUID             `json:"search_id"`
	WorkspaceID               uuid.UUID             `json:"workspace_id,omitempty"`
	RequestedScope            value.SearchScope     `json:"requested_scope"`
	EffectiveScope            value.SearchScope     `json:"effective_scope"`
	EffectiveKnowledgeBaseIDs []uuid.UUID           `json:"effective_knowledge_base_ids,omitempty"`
	GenerationSnapshots       []GenerationSnapshot  `json:"generation_snapshots,omitempty"`
	QueryHash                 string                `json:"query_hash,omitempty"`
	QueryChars                int                   `json:"query_chars,omitempty"`
	VectorTopK                int                   `json:"vector_top_k,omitempty"`
	KeywordTopK               int                   `json:"keyword_top_k,omitempty"`
	FinalTopK                 int                   `json:"final_top_k,omitempty"`
	RetrievalStatus           value.RetrievalStatus `json:"retrieval_status"`
	FailureClass              string                `json:"failure_class,omitempty"`
	RankingStage              value.RankingStage    `json:"ranking_stage,omitempty"`
	ResultCount               int                   `json:"result_count,omitempty"`
	CreatedAt                 time.Time             `json:"created_at,omitempty"`
	CompletedAt               *time.Time            `json:"completed_at,omitempty"`
	ReplayOfID                *uuid.UUID            `json:"replay_of_id,omitempty"`
}

// GenerationSnapshot 是回放和审计所需的 Generation 身份摘要，
// 不包含 query、正文、向量或凭证。
type GenerationSnapshot struct {
	KnowledgeBaseID       uuid.UUID             `json:"knowledge_base_id"`
	GenerationID          uuid.UUID             `json:"generation_id"`
	SourceContentVersion  int64                 `json:"source_content_version,omitempty"`
	IndexedContentVersion int64                 `json:"indexed_content_version,omitempty"`
	GenerationConfigHash  string                `json:"generation_config_hash,omitempty"`
	EmbeddingModelID      uuid.UUID             `json:"embedding_model_id,omitempty"`
	ProviderID            uuid.UUID             `json:"provider_id,omitempty"`
	ModelName             string                `json:"model_name,omitempty"`
	ModelConfigHash       string                `json:"model_config_hash,omitempty"`
	EmbeddingDimension    int                   `json:"embedding_dimension,omitempty"`
	RetrievalConfigHash   string                `json:"retrieval_config_hash,omitempty"`
	RerankSnapshot        *model.RerankSnapshot `json:"rerank_snapshot,omitempty"`
}
