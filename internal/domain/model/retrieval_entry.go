package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// RetrievalEntry is a rebuildable FTS/vector projection row.
type RetrievalEntry struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	IndexGenerationID  uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	ChunkID            uuid.UUID
	ChunkRevisionID    uuid.UUID
	State              value.RetrievalEntryState
	SearchContent      string
	Content            string
	SourceAnchor       value.SourceAnchor
	Metadata           map[string]any
	FTSDocument        string
	Embedding          string
	Dimension          *int
	CreatedAt          time.Time
	PublishedAt        *time.Time
	RetiredAt          *time.Time
}
