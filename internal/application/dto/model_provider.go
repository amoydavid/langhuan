package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelProvider is the credential-free API representation of a model connection.
type ModelProvider struct {
	ID                    uuid.UUID                  `json:"id"`
	Scope                 value.ModelScope           `json:"scope"`
	WorkspaceID           *uuid.UUID                 `json:"workspace_id,omitempty"`
	Name                  string                     `json:"name"`
	DisplayName           string                     `json:"display_name"`
	Description           string                     `json:"description"`
	Provider              string                     `json:"provider"`
	Config                map[string]any             `json:"config"`
	CredentialsConfigured bool                       `json:"credentials_configured"`
	CredentialFields      []string                   `json:"credential_fields"`
	Capabilities          []value.ProviderCapability `json:"capabilities"`
	ModelCatalog          bool                       `json:"model_catalog"`
	ModelCounts           ProviderModelCounts        `json:"model_counts"`
	Status                value.ModelStatus          `json:"status"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
}

// ProviderModelCounts 汇总一条连接下的模型数量。
type ProviderModelCounts struct {
	Total     int64 `json:"total"`
	Active    int64 `json:"active"`
	Embedding int64 `json:"embedding"`
	Rerank    int64 `json:"rerank"`
}

// ModelProviderFromModel removes encrypted credentials and builds an API DTO.
func ModelProviderFromModel(provider *model.ModelProvider, credentialFields []string, capabilities []value.ProviderCapability, modelCatalog bool, counts ProviderModelCounts) *ModelProvider {
	if provider == nil {
		return nil
	}
	config := make(map[string]any, len(provider.Config))
	for key, value := range provider.Config {
		config[key] = value
	}
	var workspaceID *uuid.UUID
	if provider.WorkspaceID != nil {
		value := *provider.WorkspaceID
		workspaceID = &value
	}
	fields := append([]string(nil), credentialFields...)
	capabilityValues := append([]value.ProviderCapability(nil), capabilities...)
	return &ModelProvider{
		ID: provider.ID, Scope: provider.Scope, WorkspaceID: workspaceID,
		Name: provider.Name, DisplayName: provider.DisplayName, Description: provider.Description,
		Provider: provider.Provider, Config: config,
		CredentialsConfigured: len(fields) > 0 && len(provider.CredentialsCiphertext) > 0,
		CredentialFields:      fields, Capabilities: capabilityValues, ModelCatalog: modelCatalog, ModelCounts: counts, Status: provider.Status,
		CreatedAt: provider.CreatedAt, UpdatedAt: provider.UpdatedAt,
	}
}
