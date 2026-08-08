package db

import (
	"time"

	"github.com/google/uuid"
)

// DocumentRow maps stable Document identity; revision-local content is not stored here.
type DocumentRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID uuid.UUID `gorm:"type:uuid;not null;index"`
	Kind            string
	Title           string
	SourceType      string
	SourceURI       *string
	ExternalID      *string
	// ContentHash 是文档正文（normalized markdown）的稳定哈希，可空。
	ContentHash      *string
	Status           string
	ActiveRevisionID *uuid.UUID `gorm:"type:uuid"`
	Metadata         JSONMap    `gorm:"type:jsonb"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

func (DocumentRow) TableName() string { return "documents" }

// DocumentRevisionRow maps immutable acquisition and parse facts.
type DocumentRevisionRow struct {
	ID                   uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID          uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID      uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID           uuid.UUID `gorm:"type:uuid;not null;index"`
	Kind                 string
	RevisionNo           int64
	RevisionReason       string
	OriginalFilename     *string
	FileType             *string
	ContentType          *string
	RawStorageKey        *string
	SHA256               *string
	SizeBytes            int64
	NormalizedMarkdown   *string
	ParseManifest        *JSONMap `gorm:"type:jsonb"`
	ParserRawMarkdownKey *string
	ProcessingVersion    int
	Status               string
	ErrorClass           string
	ErrorMessage         string
	CreatedBy            *uuid.UUID `gorm:"type:uuid"`
	CreatedAt            time.Time
	CompletedAt          *time.Time
}

func (DocumentRevisionRow) TableName() string { return "document_revisions" }

// DocumentAssetRow maps an archived asset discovered in one DocumentRevision.
type DocumentAssetRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null;index"`
	OriginalRef        string
	StorageKey         string
	PublicURL          string
	MimeType           string
	SHA256             string
	SizeBytes          int64
	Metadata           JSONMap `gorm:"type:jsonb"`
	CreatedAt          time.Time
}

func (DocumentAssetRow) TableName() string { return "document_assets" }
