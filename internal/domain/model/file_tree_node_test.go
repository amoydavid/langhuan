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

// TestNewFileTreeNodeFolderPreservesExternalID 验证 folder 节点保留并 trim ExternalID。
func TestNewFileTreeNodeFolderPreservesExternalID(t *testing.T) {
	const external = "feishu-folder-token"
	node, err := NewFileTreeNode(NewFileTreeNodeInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		ParentID: uuidPtr(uuid.New()), NodeType: value.FileTreeNodeFolder,
		Name: "目录", ExternalID: "  " + external + "  ",
	})
	if err != nil {
		t.Fatalf("NewFileTreeNode error = %v", err)
	}
	if node.ExternalID != external {
		t.Fatalf("ExternalID = %q, want %q (trimmed)", node.ExternalID, external)
	}
}

// TestNewFileTreeNodeFilePreservesExternalID 验证 file 节点也保留 ExternalID。
func TestNewFileTreeNodeFilePreservesExternalID(t *testing.T) {
	documentID := uuid.New()
	node, err := NewFileTreeNode(NewFileTreeNodeInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(),
		ParentID: uuidPtr(uuid.New()), NodeType: value.FileTreeNodeFile,
		Name: "doc.md", DocumentID: &documentID, DocumentKind: value.DocumentKindFile,
		ExternalID: "docx-abc",
	})
	if err != nil {
		t.Fatalf("NewFileTreeNode error = %v", err)
	}
	if node.ExternalID != "docx-abc" {
		t.Fatalf("ExternalID = %q, want docx-abc", node.ExternalID)
	}
}

// TestNewFileTreeNodeDefaultsExternalIDToEmpty 验证 ExternalID 默认为空（向后兼容）。
func TestNewFileTreeNodeDefaultsExternalIDToEmpty(t *testing.T) {
	node, err := NewFileTreeNode(NewFileTreeNodeInput{
		WorkspaceID: uuid.New(), KnowledgeBaseID: uuid.New(), NodeType: value.FileTreeNodeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.ExternalID != "" {
		t.Fatalf("ExternalID = %q, want empty", node.ExternalID)
	}
}

func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }
