package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// DocumentChunkSet is one deterministic complete chunking result.
type DocumentChunkSet struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	Strategy           value.ChunkStrategy
	ChunkerVersion     int
	ChunkingConfig     map[string]any
	ConfigHash         string
	Status             value.ChunkSetStatus
	ChunkCount         int64
	ErrorClass         string
	ErrorMessage       string
	CreatedAt          time.Time
	ReadyAt            *time.Time
	ArchivedAt         *time.Time
}
