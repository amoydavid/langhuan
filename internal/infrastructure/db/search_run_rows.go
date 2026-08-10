package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// SearchRunRow 是 search_runs 表的 GORM Row 模型。
type SearchRunRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null"`
	RequestedScope  string    `gorm:"not null"`
	QueryHash       string    `gorm:"not null"`
	QueryChars      int       `gorm:"not null"`
	VectorTopK      int       `gorm:"not null"`
	KeywordTopK     int       `gorm:"not null"`
	FinalTopK       int       `gorm:"not null"`
	RetrievalStatus string    `gorm:"not null"`
	FailureClass    string    `gorm:"not null;default:''"`
	RankingStage    string    `gorm:"not null;default:''"`
	ResultCount     int       `gorm:"not null;default:0"`
	RequestID       string    `gorm:"not null;default:''"`
	Transport       string    `gorm:"not null;default:''"`
	PrincipalKind   string    `gorm:"not null;default:''"`
	CreatedAt       time.Time
	CompletedAt     *time.Time
	ExpiresAt       time.Time
	ReplayOfID      *uuid.UUID `gorm:"type:uuid"`
}

func (SearchRunRow) TableName() string { return "search_runs" }

// SearchRunGenerationRow 是 search_run_generations 表的 GORM Row 模型。
type SearchRunGenerationRow struct {
	ID                    uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID           uuid.UUID `gorm:"type:uuid;not null"`
	SearchRunID           uuid.UUID `gorm:"type:uuid;not null"`
	KnowledgeBaseID       uuid.UUID `gorm:"type:uuid;not null"`
	GenerationID          uuid.UUID `gorm:"type:uuid;not null"`
	SourceContentVersion  int64
	IndexedContentVersion int64
	GenerationConfigHash  string
	EmbeddingModelID      uuid.UUID `gorm:"type:uuid;not null"`
	ProviderID            uuid.UUID `gorm:"type:uuid;not null"`
	ModelName             string
	ModelConfigHash       string
	EmbeddingDimension    int
	RetrievalConfigHash   string
	RerankSnapshot        JSONMap `gorm:"type:jsonb"`
}

func (SearchRunGenerationRow) TableName() string { return "search_run_generations" }

func searchRunToRow(run *model.SearchRun) *SearchRunRow {
	row := &SearchRunRow{
		ID:              run.ID,
		WorkspaceID:     run.WorkspaceID,
		RequestedScope:  string(run.RequestedScope),
		QueryHash:       run.QueryHash,
		QueryChars:      run.QueryChars,
		VectorTopK:      run.VectorTopK,
		KeywordTopK:     run.KeywordTopK,
		FinalTopK:       run.FinalTopK,
		RetrievalStatus: string(run.RetrievalStatus),
		FailureClass:    run.FailureClass,
		RankingStage:    string(run.RankingStage),
		ResultCount:     run.ResultCount,
		RequestID:       run.RequestID,
		Transport:       run.Transport,
		PrincipalKind:   run.PrincipalKind,
		CreatedAt:       run.CreatedAt,
		CompletedAt:     run.CompletedAt,
		ExpiresAt:       run.ExpiresAt,
		ReplayOfID:      run.ReplayOfID,
	}
	return row
}

func searchRunFromRow(row *SearchRunRow, generations []model.SearchRunGeneration) *model.SearchRun {
	return &model.SearchRun{
		ID:                  row.ID,
		WorkspaceID:         row.WorkspaceID,
		RequestedScope:      value.SearchScope(row.RequestedScope),
		QueryHash:           row.QueryHash,
		QueryChars:          row.QueryChars,
		VectorTopK:          row.VectorTopK,
		KeywordTopK:         row.KeywordTopK,
		FinalTopK:           row.FinalTopK,
		RetrievalStatus:     value.RetrievalStatus(row.RetrievalStatus),
		FailureClass:        row.FailureClass,
		RankingStage:        value.RankingStage(row.RankingStage),
		ResultCount:         row.ResultCount,
		RequestID:           row.RequestID,
		Transport:           row.Transport,
		PrincipalKind:       row.PrincipalKind,
		CreatedAt:           row.CreatedAt,
		CompletedAt:         row.CompletedAt,
		ExpiresAt:           row.ExpiresAt,
		ReplayOfID:          row.ReplayOfID,
		Generations:         generations,
	}
}

func searchRunGenerationToRow(gen model.SearchRunGeneration) SearchRunGenerationRow {
	row := SearchRunGenerationRow{
		ID:                    gen.ID,
		WorkspaceID:           gen.WorkspaceID,
		SearchRunID:           gen.SearchRunID,
		KnowledgeBaseID:       gen.KnowledgeBaseID,
		GenerationID:          gen.GenerationID,
		SourceContentVersion:  gen.SourceContentVersion,
		IndexedContentVersion: gen.IndexedContentVersion,
		GenerationConfigHash:  gen.GenerationConfigHash,
		EmbeddingModelID:      gen.EmbeddingModelID,
		ProviderID:            gen.ProviderID,
		ModelName:             gen.ModelName,
		ModelConfigHash:       gen.ModelConfigHash,
		EmbeddingDimension:    gen.EmbeddingDimension,
		RetrievalConfigHash:   gen.RetrievalConfigHash,
		RerankSnapshot:        JSONMap{},
	}
	if gen.RerankSnapshot != nil {
		row.RerankSnapshot = JSONMap{
			"model_id":           gen.RerankSnapshot.ModelID,
			"provider_id":        gen.RerankSnapshot.ProviderID,
			"model_name":         gen.RerankSnapshot.ModelName,
			"model_config_hash":  gen.RerankSnapshot.ModelConfigHash,
			"candidate_top_k":    gen.RerankSnapshot.CandidateTopK,
			"failure_mode":       string(gen.RerankSnapshot.FailureMode),
		}
	}
	return row
}

func searchRunGenerationFromRow(row *SearchRunGenerationRow) model.SearchRunGeneration {
	gen := model.SearchRunGeneration{
		ID:                    row.ID,
		WorkspaceID:           row.WorkspaceID,
		SearchRunID:           row.SearchRunID,
		KnowledgeBaseID:       row.KnowledgeBaseID,
		GenerationID:          row.GenerationID,
		SourceContentVersion:  row.SourceContentVersion,
		IndexedContentVersion: row.IndexedContentVersion,
		GenerationConfigHash:  row.GenerationConfigHash,
		EmbeddingModelID:      row.EmbeddingModelID,
		ProviderID:            row.ProviderID,
		ModelName:             row.ModelName,
		ModelConfigHash:       row.ModelConfigHash,
		EmbeddingDimension:    row.EmbeddingDimension,
		RetrievalConfigHash:   row.RetrievalConfigHash,
	}
	if len(row.RerankSnapshot) > 0 {
		candidateTopK, _ := intFromAny(row.RerankSnapshot["candidate_top_k"])
		failureModeRaw, _ := row.RerankSnapshot["failure_mode"].(string)
		modelID := dereferenceUUID(uuidFromAny(row.RerankSnapshot["model_id"]))
		providerID := dereferenceUUID(uuidFromAny(row.RerankSnapshot["provider_id"]))
		modelName, _ := row.RerankSnapshot["model_name"].(string)
		modelConfigHash, _ := row.RerankSnapshot["model_config_hash"].(string)
		if modelID != uuid.Nil && providerID != uuid.Nil && modelName != "" {
			gen.RerankSnapshot = &model.RerankSnapshot{
				ModelID:         modelID,
				ProviderID:      providerID,
				ModelName:       modelName,
				ModelConfigHash: modelConfigHash,
				CandidateTopK:   candidateTopK,
				FailureMode:     value.RerankFailureMode(failureModeRaw),
			}
		}
	}
	return gen
}

// uuidFromAny 把 jsonb 反序列化后的值（string/uuid.UUID）解析为 *uuid.UUID。
func uuidFromAny(value any) *uuid.UUID {
	switch raw := value.(type) {
	case string:
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil
		}
		return &parsed
	case uuid.UUID:
		return &raw
	default:
		return nil
	}
}
