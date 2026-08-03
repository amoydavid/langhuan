package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// EmbeddingModelSummary describes the model bound to a KnowledgeBase.
type EmbeddingModelSummary struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	DisplayName         string    `json:"display_name"`
	Provider            string    `json:"provider"`
	ProviderDisplayName string    `json:"provider_display_name"`
	Dimensions          int       `json:"dimensions"`
	Available           bool      `json:"available"`
}

// ChunkingConfig is the API representation of the standard chunker settings.
type ChunkingConfig struct {
	ChunkSize    int `json:"chunk_size"`
	ChunkOverlap int `json:"chunk_overlap"`
}

// KnowledgeBase is the API representation of a resolved KnowledgeBase.
type KnowledgeBase struct {
	ID                      uuid.UUID             `json:"id"`
	WorkspaceID             uuid.UUID             `json:"workspace_id"`
	Name                    string                `json:"name"`
	Description             string                `json:"description"`
	EmbeddingModelID        uuid.UUID             `json:"embedding_model_id"`
	EmbeddingModel          EmbeddingModelSummary `json:"embedding_model"`
	ChunkingConfig          ChunkingConfig        `json:"chunking_config"`
	RetrievalConfig         map[string]any        `json:"retrieval_config"`
	ContentVersion          int64                 `json:"content_version"`
	ActiveIndexGenerationID *uuid.UUID            `json:"active_index_generation_id"`
	FileTreeRootID          uuid.UUID             `json:"file_tree_root_id"`
	Metadata                map[string]any        `json:"metadata"`
	CreatedAt               time.Time             `json:"created_at"`
	UpdatedAt               time.Time             `json:"updated_at"`
}

// KnowledgeBaseFromResolved builds an API DTO including current availability.
func KnowledgeBaseFromResolved(resolved *model.ResolvedKnowledgeBase) *KnowledgeBase {
	if resolved == nil || resolved.KnowledgeBase == nil || resolved.EmbeddingModel == nil || resolved.EmbeddingModel.Model == nil || resolved.EmbeddingModel.Provider == nil {
		return nil
	}
	kb, item, provider := resolved.KnowledgeBase, resolved.EmbeddingModel.Model, resolved.EmbeddingModel.Provider
	dimensions := 0
	if item.Dimensions != nil {
		dimensions = *item.Dimensions
	}
	metadata := make(map[string]any, len(kb.Metadata))
	for key, value := range kb.Metadata {
		metadata[key] = value
	}
	return &KnowledgeBase{
		ID: kb.ID, WorkspaceID: kb.WorkspaceID, Name: kb.Name, Description: kb.Description,
		EmbeddingModelID: kb.EmbeddingModelID,
		EmbeddingModel: EmbeddingModelSummary{
			ID: item.ID, Name: item.Name, DisplayName: item.DisplayName,
			Provider: provider.Provider, ProviderDisplayName: provider.DisplayName,
			Dimensions: dimensions,
			Available:  item.Type == value.ModelTypeEmbedding && item.Status == value.ModelStatusActive && provider.Status == value.ModelStatusActive && value.IsSupportedEmbeddingDimension(dimensions),
		},
		ChunkingConfig: ChunkingConfig{
			ChunkSize: kb.ChunkingConfig.ChunkSize, ChunkOverlap: kb.ChunkingConfig.ChunkOverlap,
		},
		RetrievalConfig:         cloneDTOMap(resolved.RetrievalConfig),
		ContentVersion:          kb.ContentVersion,
		ActiveIndexGenerationID: kb.ActiveIndexGenerationID, FileTreeRootID: kb.FileTreeRootID, Metadata: metadata,
		CreatedAt: kb.CreatedAt, UpdatedAt: kb.UpdatedAt,
	}
}
