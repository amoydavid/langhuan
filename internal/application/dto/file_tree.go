package dto

import (
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// FileTreeNode is one safe virtual-tree node returned by the API.
type FileTreeNode struct {
	ID         uuid.UUID              `json:"id"`
	ParentID   *uuid.UUID             `json:"parent_id"`
	NodeType   value.FileTreeNodeType `json:"node_type"`
	Name       string                 `json:"name"`
	DocumentID *uuid.UUID             `json:"document_id"`
	Path       string                 `json:"path"`
	Children   []*FileTreeNode        `json:"children"`
}

// FileTree is a KnowledgeBase's single rooted virtual File tree.
type FileTree struct {
	WorkspaceID     uuid.UUID     `json:"workspace_id"`
	KnowledgeBaseID uuid.UUID     `json:"knowledge_base_id"`
	Root            *FileTreeNode `json:"root"`
}
