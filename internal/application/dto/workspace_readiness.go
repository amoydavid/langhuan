package dto

import "github.com/google/uuid"

// ReadinessAction is the next truthful Workspace setup or validation action.
type ReadinessAction string

const (
	ReadinessConfigureProvider     ReadinessAction = "configure_provider"
	ReadinessCreateEmbeddingModel  ReadinessAction = "create_embedding_model"
	ReadinessCreateKnowledgeBase   ReadinessAction = "create_knowledge_base"
	ReadinessAddContent            ReadinessAction = "add_content"
	ReadinessWaitForProcessing     ReadinessAction = "wait_for_processing"
	ReadinessResolveFailedDocument ReadinessAction = "resolve_failed_document"
	ReadinessTestRetrieval         ReadinessAction = "test_retrieval"
	ReadinessNone                  ReadinessAction = "none"
)

// WorkspaceReadinessDocumentCounts summarizes non-deleted Workspace content.
type WorkspaceReadinessDocumentCounts struct {
	Total      int64 `json:"total"`
	Ready      int64 `json:"ready"`
	Processing int64 `json:"processing"`
	Failed     int64 `json:"failed"`
}

// WorkspaceReadiness describes setup facts and a server-selected next action.
type WorkspaceReadiness struct {
	HasActiveProvider            bool                             `json:"has_active_provider"`
	HasSelectableEmbeddingModel  bool                             `json:"has_selectable_embedding_model"`
	KnowledgeBaseCount           int64                            `json:"knowledge_base_count"`
	DocumentCounts               WorkspaceReadinessDocumentCounts `json:"document_counts"`
	SearchableKnowledgeBaseCount int64                            `json:"searchable_knowledge_base_count"`
	RecommendedAction            ReadinessAction                  `json:"recommended_action"`
	RecommendedKnowledgeBaseID   *uuid.UUID                       `json:"recommended_knowledge_base_id"`
	RecommendedKnowledgeBaseName string                           `json:"recommended_knowledge_base_name"`
	RecommendedDocumentID        *uuid.UUID                       `json:"recommended_document_id"`
	RecommendedDocumentName      string                           `json:"recommended_document_name"`
}
