package db

import (
	"time"

	"github.com/google/uuid"
)

// KnowledgeBaseRow maps the tenant-local knowledge base control row.
type KnowledgeBaseRow struct {
	ID                      uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID             uuid.UUID `gorm:"type:uuid;not null;index"`
	Name                    string
	Description             string
	Metadata                JSONMap `gorm:"type:jsonb"`
	ContentVersion          int64
	ActiveIndexGenerationID *uuid.UUID `gorm:"type:uuid"`
	FileTreeRootID          uuid.UUID  `gorm:"type:uuid;not null"`
	SourceType              string
	SourceConfig            JSONMap    `gorm:"type:jsonb"`
	SourceConnectionID      *uuid.UUID `gorm:"type:uuid"`
	CreatedAt               time.Time
	UpdatedAt               time.Time
	DeletedAt               *time.Time
}

func (KnowledgeBaseRow) TableName() string { return "knowledge_bases" }

// IndexGenerationRow maps one immutable retrieval-index configuration snapshot.
type IndexGenerationRow struct {
	ID                    uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID           uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID       uuid.UUID `gorm:"type:uuid;not null;index"`
	BaseGenerationID      *uuid.UUID
	EmbeddingModelID      uuid.UUID `gorm:"type:uuid;not null"`
	ProviderID            uuid.UUID `gorm:"type:uuid;not null"`
	ModelName             string
	EmbeddingDimension    int
	ModelConfigHash       string
	ChunkerVersion        int
	ChunkingConfig        JSONMap `gorm:"type:jsonb"`
	RetrievalConfig       JSONMap `gorm:"type:jsonb"`
	ConfigHash            string
	SourceContentVersion  int64
	IndexedContentVersion int64
	Status                string
	DocumentCount         int64
	ChunkCount            int64
	IndexedCount          int64
	ManualEditCount       int64
	DisabledChunkCount    int64
	ManualEditDisposition string
	ErrorClass            string
	ErrorMessage          string
	CreatedAt             time.Time
	ReadyAt               *time.Time
	ActivatedAt           *time.Time
	RetiredAt             *time.Time
	RerankModelID         *uuid.UUID `gorm:"type:uuid"`
	RerankProviderID      *uuid.UUID `gorm:"type:uuid"`
	RerankModelName       *string
	RerankModelConfigHash *string
	RerankConfig          JSONMap `gorm:"type:jsonb"`
}

func (IndexGenerationRow) TableName() string { return "knowledge_base_index_generations" }
