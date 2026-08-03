package dto

import "github.com/google/uuid"

// DocumentChunkPage is the effective ChunkSet for the current Document revision
// under the KnowledgeBase active Generation configuration.
type DocumentChunkPage struct {
	GenerationID       uuid.UUID `json:"generation_id"`
	DocumentRevisionID uuid.UUID `json:"document_revision_id"`
	ChunkSetID         uuid.UUID `json:"chunk_set_id"`
	Items              []*Chunk  `json:"items"`
	NextCursor         *string   `json:"next_cursor"`
}
