package model

import (
	"time"

	"github.com/google/uuid"
)

type Asset struct {
	ID                 uuid.UUID
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	OriginalRef        string
	StorageKey         string
	PublicURL          string
	MimeType           string
	SHA256             string
	SizeBytes          int64
	Metadata           map[string]any
	CreatedAt          time.Time
}
