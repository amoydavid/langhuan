package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewFileTreeNodeOnlyAcceptsFileDocument(t *testing.T) {
	documentID := uuid.New()
	_, err := NewFileTreeNode(NewFileTreeNodeInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), ParentID: uuidPtr(uuid.New()),
		NodeType: value.FileTreeNodeFile, Name: "faq.md", DocumentID: &documentID,
		DocumentKind: value.DocumentKindFAQ,
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("error = %v, want validation", err)
	}
}

func TestNewFileTreeRootHasNoParentOrDocument(t *testing.T) {
	node, err := NewFileTreeNode(NewFileTreeNodeInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), NodeType: value.FileTreeNodeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "" || node.ParentID != nil || node.DocumentID != nil {
		t.Fatalf("root = %#v", node)
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }
