package db

import (
	"time"

	"github.com/google/uuid"
)

// FileTreeNodeRow maps only the virtual File tree shape and File Document reference.
type FileTreeNodeRow struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	WorkspaceID     uuid.UUID `gorm:"type:uuid;not null;index"`
	KnowledgeBaseID uuid.UUID `gorm:"type:uuid;not null;index"`
	ParentID        *uuid.UUID
	NodeType        string
	Name            string
	DocumentID      *uuid.UUID
	DocumentKind    *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (FileTreeNodeRow) TableName() string { return "file_tree_nodes" }
