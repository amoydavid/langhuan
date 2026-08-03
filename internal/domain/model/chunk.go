package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

type Chunk struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	Sequence           int
	SourceContent      string
	ActiveRevisionID   *uuid.UUID
	Content            string
	EmbeddingContent   string
	ContextHeader      string
	SourceAnchor       value.SourceAnchor
	Metadata           map[string]any
	CreatedAt          time.Time
}
