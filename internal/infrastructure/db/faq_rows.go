package db

import (
	"time"

	"github.com/google/uuid"
)

// FAQRevisionContentRow maps the single answer belonging to an FAQ revision.
type FAQRevisionContentRow struct {
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null;index"`
	Kind               string
	Answer             string
	CreatedAt          time.Time
}

func (FAQRevisionContentRow) TableName() string { return "faq_revision_contents" }

// FAQRevisionQuestionRow maps one ordered question variant.
type FAQRevisionQuestionRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentRevisionID uuid.UUID `gorm:"type:uuid;not null;index"`
	Kind               string
	Sequence           int
	Question           string
	NormalizedQuestion string
	CreatedAt          time.Time
}

func (FAQRevisionQuestionRow) TableName() string { return "faq_revision_questions" }
