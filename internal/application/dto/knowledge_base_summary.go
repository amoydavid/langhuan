package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// KnowledgeBaseSyncState describes whether the active retrieval projection matches current content.
type KnowledgeBaseSyncState string

const (
	KnowledgeBaseSyncSynced         KnowledgeBaseSyncState = "synced"
	KnowledgeBaseSyncUpdating       KnowledgeBaseSyncState = "updating"
	KnowledgeBaseSyncFailed         KnowledgeBaseSyncState = "failed"
	KnowledgeBaseSyncCandidateReady KnowledgeBaseSyncState = "candidate_ready"
)

// KnowledgeBaseDocumentCounts summarizes all non-deleted content by kind and lifecycle state.
type KnowledgeBaseDocumentCounts struct {
	Total      int64 `json:"total"`
	File       int64 `json:"file"`
	FAQ        int64 `json:"faq"`
	Web        int64 `json:"web"`
	Ready      int64 `json:"ready"`
	Processing int64 `json:"processing"`
	Failed     int64 `json:"failed"`
}

// KnowledgeBaseGenerationSummary is the safe typed projection used by the workbench.
type KnowledgeBaseGenerationSummary struct {
	ID                    uuid.UUID                   `json:"id"`
	DisplayLabel          string                      `json:"display_label"`
	Status                value.IndexGenerationStatus `json:"status"`
	ModelDisplayName      string                      `json:"model_display_name"`
	EmbeddingDimension    int                         `json:"embedding_dimension"`
	ChunkerVersion        int                         `json:"chunker_version"`
	ChunkingConfig        map[string]any              `json:"chunking_config"`
	RetrievalConfig       map[string]any              `json:"retrieval_config"`
	SourceContentVersion  int64                       `json:"source_content_version"`
	IndexedContentVersion int64                       `json:"indexed_content_version"`
	DocumentCount         int64                       `json:"document_count"`
	ChunkCount            int64                       `json:"chunk_count"`
	IndexedCount          int64                       `json:"indexed_count"`
	ManualEditCount       int64                       `json:"manual_edit_count"`
	DisabledChunkCount    int64                       `json:"disabled_chunk_count"`
	ErrorMessage          string                      `json:"error_message,omitempty"`
	CreatedAt             time.Time                   `json:"created_at"`
	ReadyAt               *time.Time                  `json:"ready_at,omitempty"`
	ActivatedAt           *time.Time                  `json:"activated_at,omitempty"`
}

// KnowledgeBaseBlocker is one actionable, safe workbench issue.
type KnowledgeBaseBlocker struct {
	Code                string    `json:"code"`
	ResourceType        string    `json:"resource_type"`
	ResourceID          uuid.UUID `json:"resource_id"`
	ResourceDisplayName string    `json:"resource_display_name"`
	Message             string    `json:"message"`
}

// KnowledgeBaseSummary is the complete workbench overview projection.
type KnowledgeBaseSummary struct {
	KnowledgeBaseID     uuid.UUID                       `json:"knowledge_base_id"`
	KnowledgeBaseName   string                          `json:"knowledge_base_name"`
	ContentVersion      int64                           `json:"content_version"`
	DocumentCounts      KnowledgeBaseDocumentCounts     `json:"document_counts"`
	ActiveGeneration    *KnowledgeBaseGenerationSummary `json:"active_generation"`
	CandidateGeneration *KnowledgeBaseGenerationSummary `json:"candidate_generation"`
	SyncState           KnowledgeBaseSyncState          `json:"sync_state"`
	RecentJobs          []*JobSummary                   `json:"recent_jobs"`
	Blockers            []*KnowledgeBaseBlocker         `json:"blockers"`
}
