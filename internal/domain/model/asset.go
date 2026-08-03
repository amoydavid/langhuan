package model

import (
	"time"

	"github.com/google/uuid"
)

type Asset struct {
	ID          uuid.UUID
	DocumentID  uuid.UUID
	OriginalRef string
	StorageKey  string
	PublicURL   string
	MimeType    string
	SHA256      string
	SizeBytes   int64
	Metadata    map[string]any
	CreatedAt   time.Time
}
