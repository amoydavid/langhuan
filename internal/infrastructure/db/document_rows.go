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
	ContentHash *string
	// KbSourceID 是产生该文档的内容源（kb_sources.id，迁移 000025）；上传文档为 NULL。
	KbSourceID *uuid.UUID `gorm:"type:uuid"`
	// SourceMeta 是 web 页面文档的抓取运行态（documents.source_meta jsonb，
	// 键名合同见 ports/source）；非 web 文档保持空对象。
	SourceMeta       JSONMap `gorm:"type:jsonb"`
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
