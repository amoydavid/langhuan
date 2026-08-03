package db

import (
	"time"

	"github.com/google/uuid"
)

// RetrievalEntryRow maps the rebuildable FTS/vector projection.
type RetrievalEntryRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null"`
	IndexGenerationID  uuid.UUID `gorm:"type:uuid;not null"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null"`
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null"`
	ChunkSetID         uuid.UUID `gorm:"type:uuid;not null"`
	ChunkID            uuid.UUID `gorm:"type:uuid;not null"`
	ChunkRevisionID    uuid.UUID `gorm:"type:uuid;not null"`
	State              string
	SearchContent      string
	Content            string
	SourceAnchor       JSONMap `gorm:"type:jsonb"`
	Metadata           JSONMap `gorm:"type:jsonb"`
	FTSDocument        string  `gorm:"column:fts_document;type:tsvector"`
	Embedding          *string `gorm:"type:halfvec"`
	Dimension          *int
	CreatedAt          time.Time
	PublishedAt        *time.Time
	RetiredAt          *time.Time
}

func (RetrievalEntryRow) TableName() string { return "retrieval_entries" }
