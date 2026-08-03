package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// IndexGeneration exposes one immutable index configuration and build report.
type IndexGeneration struct {
	ID                    uuid.UUID                   `json:"id"`
	WorkspaceID           uuid.UUID                   `json:"workspace_id"`
	KnowledgeBaseID       uuid.UUID                   `json:"knowledge_base_id"`
	BaseGenerationID      *uuid.UUID                  `json:"base_generation_id,omitempty"`
	EmbeddingModelID      uuid.UUID                   `json:"embedding_model_id"`
	ProviderID            uuid.UUID                   `json:"provider_id"`
	ModelName             string                      `json:"model_name"`
	DisplayLabel          string                      `json:"display_label"`
	EmbeddingDimension    int                         `json:"embedding_dimension"`
	ChunkerVersion        int                         `json:"chunker_version"`
	ChunkingConfig        map[string]any              `json:"chunking_config"`
	RetrievalConfig       map[string]any              `json:"retrieval_config"`
	ConfigHash            string                      `json:"config_hash"`
	SourceContentVersion  int64                       `json:"source_content_version"`
	IndexedContentVersion int64                       `json:"indexed_content_version"`
	Status                value.IndexGenerationStatus `json:"status"`
	DocumentCount         int64                       `json:"document_count"`
	ChunkCount            int64                       `json:"chunk_count"`
	IndexedCount          int64                       `json:"indexed_count"`
	ManualEditCount       int64                       `json:"manual_edit_count"`
	DisabledChunkCount    int64                       `json:"disabled_chunk_count"`
	ManualEditDisposition value.ManualEditDisposition `json:"manual_edit_disposition"`
	ErrorClass            string                      `json:"error_class,omitempty"`
	ErrorMessage          string                      `json:"error_message,omitempty"`
	CreatedAt             time.Time                   `json:"created_at"`
	ReadyAt               *time.Time                  `json:"ready_at,omitempty"`
	ActivatedAt           *time.Time                  `json:"activated_at,omitempty"`
	RetiredAt             *time.Time                  `json:"retired_at,omitempty"`
}

// IndexGenerationFromModel builds one API DTO.
func IndexGenerationFromModel(generation *model.IndexGeneration) *IndexGeneration {
	if generation == nil {
		return nil
	}
	return &IndexGeneration{
		ID: generation.ID, WorkspaceID: generation.WorkspaceID, KnowledgeBaseID: generation.KnowledgeBaseID,
		BaseGenerationID: generation.BaseGenerationID, EmbeddingModelID: generation.EmbeddingModelID,
		ProviderID: generation.ProviderID, ModelName: generation.ModelName,
		DisplayLabel:       indexGenerationDisplayLabel(generation),
		EmbeddingDimension: generation.EmbeddingDimension, ChunkerVersion: generation.ChunkerVersion,
		ChunkingConfig: cloneDTOMap(generation.ChunkingConfig), RetrievalConfig: cloneDTOMap(generation.RetrievalConfig),
		ConfigHash: generation.ConfigHash, SourceContentVersion: generation.SourceContentVersion,
		IndexedContentVersion: generation.IndexedContentVersion, Status: generation.Status,
		DocumentCount: generation.DocumentCount, ChunkCount: generation.ChunkCount,
		IndexedCount:    generation.IndexedCount,
		ManualEditCount: generation.ManualEditCount, DisabledChunkCount: generation.DisabledChunkCount,
		ManualEditDisposition: generation.ManualEditDisposition,
		ErrorClass:            generation.ErrorClass, ErrorMessage: generation.ErrorMessage,
		CreatedAt: generation.CreatedAt, ReadyAt: generation.ReadyAt,
		ActivatedAt: generation.ActivatedAt, RetiredAt: generation.RetiredAt,
	}
}

func indexGenerationDisplayLabel(generation *model.IndexGeneration) string {
	modelName := strings.TrimSpace(generation.ModelName)
	if modelName == "" {
		modelName = "未命名模型"
	}
	return fmt.Sprintf(
		"%s · %s · %s",
		generation.CreatedAt.Format("2006-01-02 15:04"),
		modelName,
		indexGenerationStatusLabel(generation.Status),
	)
}

func indexGenerationStatusLabel(status value.IndexGenerationStatus) string {
	switch status {
	case value.IndexGenerationBuilding:
		return "构建中"
	case value.IndexGenerationReady:
		return "已就绪"
	case value.IndexGenerationStale:
		return "已过期"
	case value.IndexGenerationFailed:
		return "构建失败"
	case value.IndexGenerationRetired:
		return "已退役"
	default:
		return "状态未知"
	}
}

func cloneDTOMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, item := range input {
		result[key] = item
	}
	return result
}
