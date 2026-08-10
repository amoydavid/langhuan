package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchResponse 是应用层统一返回的检索响应，包含运行元数据和结果。
type SearchResponse struct {
	Run     SearchRunSummary
	Results []*SearchResult
}

// SearchRunSummary 是一次检索运行的摘要元数据，用于协议返回。
type SearchRunSummary struct {
	SearchID                  uuid.UUID
	WorkspaceID               uuid.UUID
	RequestedScope            value.SearchScope
	EffectiveScope            value.SearchScope
	EffectiveKnowledgeBaseIDs []uuid.UUID
	GenerationSnapshots       []GenerationSnapshot
	QueryHash                 string
	QueryChars                int
	VectorTopK                int
	KeywordTopK               int
	FinalTopK                 int
	RetrievalStatus           value.RetrievalStatus
	FailureClass              string
	RankingStage              value.RankingStage
	ResultCount               int
	CreatedAt                 time.Time
	CompletedAt               *time.Time
	ReplayOfID                *uuid.UUID
}

// GenerationSnapshot 是回放和审计所需的 Generation 身份摘要，
// 不包含 query、正文、向量或凭证。
type GenerationSnapshot struct {
	KnowledgeBaseID       uuid.UUID
	GenerationID          uuid.UUID
	SourceContentVersion  int64
	IndexedContentVersion int64
	GenerationConfigHash  string
	EmbeddingModelID      uuid.UUID
	ProviderID            uuid.UUID
	ModelName             string
	ModelConfigHash       string
	EmbeddingDimension    int
	RetrievalConfigHash   string
	RerankSnapshot        *model.RerankSnapshot
}
