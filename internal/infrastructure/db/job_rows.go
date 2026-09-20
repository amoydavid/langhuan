package db

import (
	"time"

	"github.com/google/uuid"
)

// JobRow maps one document-revision or index-generation asynchronous task.
type JobRow struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID        uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID    uuid.UUID `gorm:"type:uuid;not null;index"`
	DocumentID         *uuid.UUID
	DocumentRevisionID *uuid.UUID
	IndexGenerationID  *uuid.UUID
	SourceConnectionID *uuid.UUID
	// KbSourceID 是源级任务目标（迁移 000025 第五分支）；KB 级任务为 NULL。
	KbSourceID    *uuid.UUID `gorm:"type:uuid"`
	Type          string
	Status        string
	Attempts      int
	ExternalJobID string
	Payload       JSONMap `gorm:"type:jsonb"`
	ErrorClass    string
	ErrorMessage  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (JobRow) TableName() string { return "jobs" }
