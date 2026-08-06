package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// WorkspaceSearchSettings 是 Workspace 默认查询阶段策略的 API 表示。
type WorkspaceSearchSettings struct {
	WorkspaceID uuid.UUID                      `json:"workspace_id"`
	Rerank      *WorkspaceSearchSettingsRerank `json:"rerank"`
	UpdatedAt   time.Time                      `json:"updated_at"`
}

// WorkspaceSearchSettingsRerank 是可配置的 Rerank 运行参数；不暴露 config hash。
type WorkspaceSearchSettingsRerank struct {
	ModelID       uuid.UUID               `json:"model_id"`
	ProviderID    uuid.UUID               `json:"provider_id"`
	ModelName     string                  `json:"model_name"`
	CandidateTopK int                     `json:"candidate_top_k"`
	FailureMode   value.RerankFailureMode `json:"failure_mode"`
}

// WorkspaceSearchSettingsFromModel 构造 API DTO。
func WorkspaceSearchSettingsFromModel(settings *model.WorkspaceSearchSettings) *WorkspaceSearchSettings {
	if settings == nil {
		return nil
	}
	dto := &WorkspaceSearchSettings{WorkspaceID: settings.WorkspaceID, UpdatedAt: settings.UpdatedAt}
	if settings.Rerank != nil {
		dto.Rerank = &WorkspaceSearchSettingsRerank{
			ModelID: settings.Rerank.ModelID, ProviderID: settings.Rerank.ProviderID,
			ModelName: settings.Rerank.ModelName, CandidateTopK: settings.Rerank.CandidateTopK,
			FailureMode: settings.Rerank.FailureMode,
		}
	}
	return dto
}
