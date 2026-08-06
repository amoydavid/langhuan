package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestMoveFolderRejectsDescendant(t *testing.T) {
	t.Parallel()

	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	rootID, parentID, childID := uuid.New(), uuid.New(), uuid.New()
	store := &fakeFileTreeStore{nodes: map[uuid.UUID]*model.FileTreeNode{
		rootID:   {ID: rootID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, NodeType: value.FileTreeNodeRoot},
		parentID: {ID: parentID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, ParentID: &rootID, NodeType: value.FileTreeNodeFolder, Name: "parent"},
		childID:  {ID: childID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, ParentID: &parentID, NodeType: value.FileTreeNodeFolder, Name: "child"},
	}, cycles: map[[2]uuid.UUID]bool{{parentID, childID}: true}}
	service := NewFileTreeService(store)

	err := service.Move(context.Background(), MoveFileTreeNodeInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
		NodeID: parentID, NewParentID: childID,
	})
	if !errors.Is(err, domainerrors.ErrFileTreeCycle) {
		t.Fatalf("error = %v, want file tree cycle", err)
	}
}

func TestFileTreeListGivesRootAReadableName(t *testing.T) {
	t.Parallel()

	workspaceID, knowledgeBaseID, rootID := uuid.New(), uuid.New(), uuid.New()
	store := &fakeFileTreeStore{nodes: map[uuid.UUID]*model.FileTreeNode{
		rootID: {
			ID: rootID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID,
			NodeType: value.FileTreeNodeRoot,
		},
	}}

	tree, err := NewFileTreeService(store).List(context.Background(), value.ResourceAccess{WorkspaceID: workspaceID, Unrestricted: true}, knowledgeBaseID)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Root == nil || tree.Root.Name != "文件" {
		t.Fatalf("root = %#v, want readable name", tree.Root)
	}
}

func TestFileTreeRenameFileUpdatesDocumentWithoutContentMutation(t *testing.T) {
	t.Parallel()

	workspaceID, knowledgeBaseID := uuid.New(), uuid.New()
	rootID, fileID, documentID := uuid.New(), uuid.New(), uuid.New()
	document := &model.Document{ID: documentID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Kind: value.DocumentKindFile, Title: "old.pdf"}
	store := &fakeFileTreeStore{
		nodes: map[uuid.UUID]*model.FileTreeNode{
			rootID: {ID: rootID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, NodeType: value.FileTreeNodeRoot},
			fileID: {ID: fileID, WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, ParentID: &rootID, NodeType: value.FileTreeNodeFile, Name: "old.pdf", DocumentID: &documentID},
		},
		documents: map[uuid.UUID]*model.Document{documentID: document},
	}
	service := NewFileTreeService(store)

	name := "new.pdf"
	if err := service.Update(context.Background(), UpdateFileTreeNodeInput{
		WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, NodeID: fileID, Name: &name,
	}); err != nil {
		t.Fatal(err)
	}
	if store.nodes[fileID].Name != name || store.documents[documentID].Title != name {
		t.Fatalf("node/document = %#v / %#v", store.nodes[fileID], store.documents[documentID])
	}
}

func TestFileTreeListRejectsUnboundKnowledgeBase(t *testing.T) {
	workspaceID, allowedKB, otherKB := uuid.New(), uuid.New(), uuid.New()
	store := &fakeFileTreeStore{}
	access := value.ResourceAccess{WorkspaceID: workspaceID, AllowedKnowledgeBaseIDs: []uuid.UUID{allowedKB}}
	_, err := NewFileTreeService(store).List(context.Background(), access, otherKB)
	if !errors.Is(err, domainerrors.ErrNotFound) {
		t.Fatalf("List() error = %v, want ErrNotFound", err)
	}
}

type fakeFileTreeStore struct {
	nodes     map[uuid.UUID]*model.FileTreeNode
	documents map[uuid.UUID]*model.Document
	cycles    map[[2]uuid.UUID]bool
}

func (s *fakeFileTreeStore) WithinWorkspace(ctx context.Context, _ uuid.UUID, fn func(context.Context, FileTreeTx) error) error {
	return fn(ctx, s)
}

func (s *fakeFileTreeStore) ListFileTreeNodes(_ context.Context, knowledgeBaseID uuid.UUID) ([]*model.FileTreeNode, error) {
	result := make([]*model.FileTreeNode, 0)
	for _, node := range s.nodes {
		if node.KnowledgeBaseID == knowledgeBaseID {
			result = append(result, node)
		}
	}
	return result, nil
}

func (s *fakeFileTreeStore) GetFileTreeNodeForUpdate(_ context.Context, id uuid.UUID) (*model.FileTreeNode, error) {
	node := s.nodes[id]
	if node == nil {
		return nil, domainerrors.ErrNotFound
	}
	return node, nil
}

func (s *fakeFileTreeStore) GetDocumentForUpdate(_ context.Context, id uuid.UUID) (*model.Document, error) {
	document := s.documents[id]
	if document == nil {
		return nil, domainerrors.ErrNotFound
	}
	return document, nil
}

func (s *fakeFileTreeStore) WouldCreateCycle(_ context.Context, nodeID, newParentID uuid.UUID) (bool, error) {
	return s.cycles[[2]uuid.UUID{nodeID, newParentID}], nil
}

func (s *fakeFileTreeStore) HasFileTreeChildren(_ context.Context, id uuid.UUID) (bool, error) {
	for _, node := range s.nodes {
		if node.ParentID != nil && *node.ParentID == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeFileTreeStore) CreateFileTreeNode(_ context.Context, node *model.FileTreeNode) error {
	s.nodes[node.ID] = node
	return nil
}

func (s *fakeFileTreeStore) SaveFileTreeNode(_ context.Context, node *model.FileTreeNode, document *model.Document) error {
	s.nodes[node.ID] = node
	if document != nil {
		s.documents[document.ID] = document
	}
	return nil
}

func (s *fakeFileTreeStore) DeleteFileTreeNode(_ context.Context, id uuid.UUID) error {
	delete(s.nodes, id)
	return nil
}
