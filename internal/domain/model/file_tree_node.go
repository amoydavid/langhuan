package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// NewFileTreeNodeInput contains one virtual tree node.
type NewFileTreeNodeInput struct {
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	ParentID        *uuid.UUID
	NodeType        value.FileTreeNodeType
	Name            string
	DocumentID      *uuid.UUID
	DocumentKind    value.DocumentKind
}

// FileTreeNode organizes File Documents without changing content identity.
type FileTreeNode struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	KnowledgeBaseID uuid.UUID
	ParentID        *uuid.UUID
	NodeType        value.FileTreeNodeType
	Name            string
	DocumentID      *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewFileTreeNode validates node shape and File-only document ownership.
func NewFileTreeNode(input NewFileTreeNodeInput) (*FileTreeNode, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil {
		return nil, fmt.Errorf("%w: FileTreeNode lineage 不能为空", domainerrors.ErrValidation)
	}
	if err := input.NodeType.Validate(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	switch input.NodeType {
	case value.FileTreeNodeRoot:
		if input.ParentID != nil || input.DocumentID != nil || name != "" {
			return nil, fmt.Errorf("%w: root 不能有 parent、name 或 document", domainerrors.ErrValidation)
		}
	case value.FileTreeNodeFolder:
		if nilOrEmptyUUID(input.ParentID) || input.DocumentID != nil || name == "" {
			return nil, fmt.Errorf("%w: folder 必须有 parent/name 且不能关联 document", domainerrors.ErrValidation)
		}
	case value.FileTreeNodeFile:
		if nilOrEmptyUUID(input.ParentID) || nilOrEmptyUUID(input.DocumentID) || name == "" || input.DocumentKind != value.DocumentKindFile {
			return nil, fmt.Errorf("%w: file 节点必须关联 File Document 并包含 parent/name", domainerrors.ErrValidation)
		}
	}
	now := time.Now().UTC()
	return &FileTreeNode{
		ID: uuid.New(), WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		ParentID: input.ParentID, NodeType: input.NodeType, Name: name,
		DocumentID: input.DocumentID, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func nilOrEmptyUUID(id *uuid.UUID) bool {
	return id == nil || *id == uuid.Nil
}
