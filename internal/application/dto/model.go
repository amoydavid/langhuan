package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelProviderSummary identifies the connection behind a model.
type ModelProviderSummary struct {
	ID          uuid.UUID         `json:"id"`
	Scope       value.ModelScope  `json:"scope"`
	WorkspaceID *uuid.UUID        `json:"workspace_id,omitempty"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Provider    string            `json:"provider"`
	Status      value.ModelStatus `json:"status"`
}

// Model is the API representation of a configured model.
type Model struct {
	ID             uuid.UUID            `json:"id"`
	ProviderID     uuid.UUID            `json:"provider_id"`
	Provider       ModelProviderSummary `json:"provider"`
	Name           string               `json:"name"`
	DisplayName    string               `json:"display_name"`
	Description    string               `json:"description"`
	Type           value.ModelType      `json:"type"`
	ModelName      string               `json:"model_name"`
	Dimensions     int                  `json:"dimensions"`
	Parameters     map[string]any       `json:"parameters"`
	Status         value.ModelStatus    `json:"status"`
	ReferenceCount int64                `json:"reference_count"`
	Available      bool                 `json:"available"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

// ConnectionTestResult is the non-persistent result of one live Embedding call.
type ConnectionTestResult struct {
	OK         bool  `json:"ok"`
	Dimensions int   `json:"dimensions"`
	DurationMS int64 `json:"duration_ms"`
}

// ModelFromResolved builds a model DTO with its Provider and reference state.
func ModelFromResolved(resolved *model.ResolvedModel, referenceCount int64) *Model {
	if resolved == nil || resolved.Model == nil || resolved.Provider == nil {
		return nil
	}
	item, provider := resolved.Model, resolved.Provider
	dimensions := 0
	if item.Dimensions != nil {
		dimensions = *item.Dimensions
	}
	parameters := make(map[string]any, len(item.Parameters))
	for key, value := range item.Parameters {
		parameters[key] = value
	}
	var workspaceID *uuid.UUID
	if provider.WorkspaceID != nil {
		value := *provider.WorkspaceID
		workspaceID = &value
	}
	return &Model{
		ID: item.ID, ProviderID: item.ProviderID,
		Provider: ModelProviderSummary{ID: provider.ID, Scope: provider.Scope, WorkspaceID: workspaceID, Name: provider.Name, DisplayName: provider.DisplayName, Provider: provider.Provider, Status: provider.Status},
		Name:     item.Name, DisplayName: item.DisplayName, Description: item.Description,
		Type: item.Type, ModelName: item.ModelName, Dimensions: dimensions, Parameters: parameters,
		Status: item.Status, ReferenceCount: referenceCount,
		Available: item.Status == value.ModelStatusActive && provider.Status == value.ModelStatusActive,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}
