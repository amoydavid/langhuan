package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchRun 是一次检索运行的元数据快照，不保存原始 query、正文、向量或凭证。
type SearchRun struct {
	ID                        uuid.UUID
	WorkspaceID               uuid.UUID
	RequestedScope            value.SearchScope
	QueryHash                 string
	QueryChars                int
	VectorTopK                int
	KeywordTopK               int
	FinalTopK                 int
	RetrievalStatus           value.RetrievalStatus
	FailureClass              string
	RankingStage              value.RankingStage
	ResultCount               int
	RequestID                 string
	Transport                 string
	PrincipalKind             string
	CreatedAt                 time.Time
	CompletedAt               *time.Time
	ExpiresAt                 time.Time
	ReplayOfID                *uuid.UUID
	Generations               []SearchRunGeneration
	EffectiveKnowledgeBaseIDs []uuid.UUID
}

// SearchRunGeneration 是一次检索运行记录的一个 Generation 快照。
type SearchRunGeneration struct {
	ID                    uuid.UUID
	WorkspaceID           uuid.UUID
	SearchRunID           uuid.UUID
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
	RerankSnapshot        *RerankSnapshot
}

// SearchRunCompletion 是完成一个 running SearchRun 所需的终态信息。
type SearchRunCompletion struct {
	Status         value.RetrievalStatus
	FailureClass   string
	RankingStage   value.RankingStage
	ResultCount    int
	EffectiveKBIDs []uuid.UUID
	Generations    []SearchRunGeneration
}

// Validate 校验 SearchRun 的不变量。
func (r *SearchRun) Validate() error {
	if r == nil {
		return fmt.Errorf("%w: SearchRun 为空", domainerrors.ErrValidation)
	}
	if r.ID == uuid.Nil {
		return fmt.Errorf("%w: SearchRun ID 为空", domainerrors.ErrValidation)
	}
	if r.WorkspaceID == uuid.Nil {
		return fmt.Errorf("%w: SearchRun WorkspaceID 为空", domainerrors.ErrValidation)
	}
	if err := r.RequestedScope.Validate(); err != nil {
		return err
	}
	if err := r.RetrievalStatus.Validate(); err != nil {
		return err
	}
	if r.RetrievalStatus == value.RetrievalStatusFailed && r.FailureClass == "" {
		return fmt.Errorf("%w: failed SearchRun 必须有 failure_class", domainerrors.ErrValidation)
	}
	if r.RetrievalStatus != value.RetrievalStatusFailed && r.FailureClass != "" {
		return fmt.Errorf("%w: 非 failed SearchRun 不能有 failure_class", domainerrors.ErrValidation)
	}
	if r.QueryHash == "" {
		return fmt.Errorf("%w: SearchRun QueryHash 为空", domainerrors.ErrValidation)
	}
	if r.QueryChars < 0 {
		return fmt.Errorf("%w: SearchRun QueryChars 为负", domainerrors.ErrValidation)
	}
	if r.VectorTopK <= 0 || r.KeywordTopK <= 0 || r.FinalTopK <= 0 {
		return fmt.Errorf("%w: SearchRun topK 必须为正", domainerrors.ErrValidation)
	}
	if r.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: SearchRun ExpiresAt 为空", domainerrors.ErrValidation)
	}
	for i := range r.Generations {
		if err := r.Generations[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate 校验 SearchRunGeneration 的不变量。
func (g *SearchRunGeneration) Validate() error {
	if g == nil {
		return fmt.Errorf("%w: SearchRunGeneration 为空", domainerrors.ErrValidation)
	}
	if g.WorkspaceID == uuid.Nil || g.SearchRunID == uuid.Nil ||
		g.KnowledgeBaseID == uuid.Nil || g.GenerationID == uuid.Nil {
		return fmt.Errorf("%w: SearchRunGeneration lineage 为空", domainerrors.ErrValidation)
	}
	if g.EmbeddingModelID == uuid.Nil || g.ProviderID == uuid.Nil {
		return fmt.Errorf("%w: SearchRunGeneration embedding/provider 为空", domainerrors.ErrValidation)
	}
	if g.ModelName == "" || g.ModelConfigHash == "" || g.GenerationConfigHash == "" ||
		g.RetrievalConfigHash == "" {
		return fmt.Errorf("%w: SearchRunGeneration hash/name 为空", domainerrors.ErrValidation)
	}
	if g.EmbeddingDimension <= 0 {
		return fmt.Errorf("%w: SearchRunGeneration 维度必须为正", domainerrors.ErrValidation)
	}
	return nil
}
