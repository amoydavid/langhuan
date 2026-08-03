package db

import (
	"time"

	"github.com/google/uuid"
)

// DocumentChunkSetRow maps one deterministic complete chunking result.
type DocumentChunkSetRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null;index"`
	Strategy           string
	ChunkerVersion     int
	ChunkingConfig     JSONMap `gorm:"type:jsonb"`
	ConfigHash         string
	Status             string
	ChunkCount         int64
	ErrorClass         string
	ErrorMessage       string
	CreatedAt          time.Time
	ReadyAt            *time.Time
	ArchivedAt         *time.Time
}

func (DocumentChunkSetRow) TableName() string { return "document_chunk_sets" }

// ChunkRow maps stable source lineage and its current effective revision pointer.
type ChunkRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null;index"`
	ChunkSetID         uuid.UUID `gorm:"type:uuid;not null;index"`
	Sequence           int
	SourceContent      string
	SourceAnchor       JSONMap `gorm:"type:jsonb"`
	Metadata           JSONMap `gorm:"type:jsonb"`
	ActiveRevisionID   *uuid.UUID
	CreatedAt          time.Time
}

func (ChunkRow) TableName() string { return "chunks" }

// ChunkRevisionRow maps immutable effective chunk text and edit audit.
type ChunkRevisionRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null;index"`
	ChunkSetID         uuid.UUID `gorm:"type:uuid;not null;index"`
	ChunkID            uuid.UUID `gorm:"type:uuid;not null;index"`
	RevisionNo         int64
	BaseRevisionID     *uuid.UUID
	Content            string
	ContextHeader      string
	EmbeddingContent   string
	Enabled            bool
	Status             string
	EditSource         string
	EditorUserID       *uuid.UUID
	ErrorClass         string
	ErrorMessage       string
	CreatedAt          time.Time
	IndexedAt          *time.Time
}

func (ChunkRevisionRow) TableName() string { return "chunk_revisions" }
