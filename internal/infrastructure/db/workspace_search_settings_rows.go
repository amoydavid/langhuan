package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// WorkspaceSearchSettingsRow 是 Workspace 默认检索策略的持久化行。
type WorkspaceSearchSettingsRow struct {
	WorkspaceID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	RerankModelID         *uuid.UUID `gorm:"type:uuid"`
	RerankProviderID      *uuid.UUID `gorm:"type:uuid"`
	RerankModelName       *string
	RerankModelConfigHash *string
	RerankConfig          JSONMap    `gorm:"type:jsonb;not null;default:'{}'"`
	UpdatedBy             *uuid.UUID `gorm:"type:uuid"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (WorkspaceSearchSettingsRow) TableName() string { return "workspace_search_settings" }

func workspaceSearchSettingsToRow(settings *model.WorkspaceSearchSettings) (*WorkspaceSearchSettingsRow, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	row := &WorkspaceSearchSettingsRow{
		WorkspaceID: settings.WorkspaceID, RerankConfig: nil,
		CreatedAt: settings.CreatedAt, UpdatedAt: settings.UpdatedAt,
	}
	if settings.UpdatedBy != uuid.Nil {
		updatedBy := settings.UpdatedBy
		row.UpdatedBy = &updatedBy
	}
	if settings.Rerank == nil {
		return row, nil
	}
	r := settings.Rerank
	modelID, providerID := r.ModelID, r.ProviderID
	modelName, configHash := r.ModelName, r.ModelConfigHash
	row.RerankModelID, row.RerankProviderID = &modelID, &providerID
	row.RerankModelName, row.RerankModelConfigHash = &modelName, &configHash
	row.RerankConfig = JSONMap{"candidate_top_k": r.CandidateTopK, "failure_mode": string(r.FailureMode)}
	return row, nil
}

func workspaceSearchSettingsFromRow(row *WorkspaceSearchSettingsRow) (*model.WorkspaceSearchSettings, error) {
	if row == nil {
		return nil, fmt.Errorf("%w: Workspace Search Settings 行为空", domainerrors.ErrValidation)
	}
	settings := &model.WorkspaceSearchSettings{
		WorkspaceID: row.WorkspaceID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.UpdatedBy != nil {
		settings.UpdatedBy = *row.UpdatedBy
	}
	if row.RerankModelID == nil && row.RerankProviderID == nil && row.RerankModelName == nil && row.RerankModelConfigHash == nil {
		return settings, nil
	}
	if row.RerankModelID == nil || row.RerankProviderID == nil || row.RerankModelName == nil || row.RerankModelConfigHash == nil {
		return nil, fmt.Errorf("%w: Workspace Rerank 快照字段不完整", domainerrors.ErrValidation)
	}
	topK, ok := workspaceSearchIntFromJSON(row.RerankConfig["candidate_top_k"])
	if !ok {
		return nil, fmt.Errorf("%w: Workspace Rerank candidate_top_k 无效", domainerrors.ErrValidation)
	}
	failureMode, ok := row.RerankConfig["failure_mode"].(string)
	if !ok {
		return nil, fmt.Errorf("%w: Workspace Rerank failure_mode 无效", domainerrors.ErrValidation)
	}
	settings.Rerank = &model.RerankSnapshot{
		ModelID: *row.RerankModelID, ProviderID: *row.RerankProviderID,
		ModelName: *row.RerankModelName, ModelConfigHash: *row.RerankModelConfigHash,
		CandidateTopK: topK, FailureMode: value.RerankFailureMode(failureMode),
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return settings, nil
}

func workspaceSearchIntFromJSON(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), value == float64(int(value))
	default:
		return 0, false
	}
}
