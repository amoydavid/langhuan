package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// FileTreeService manages the virtual File-only tree without changing content versions.
type FileTreeService struct {
	store FileTreeStore
}

// NewFileTreeService creates a File tree service.
func NewFileTreeService(store FileTreeStore) *FileTreeService { return &FileTreeService{store: store} }

type CreateFileTreeFolderInput struct {
	WorkspaceID, KnowledgeBaseID, ParentID uuid.UUID
	Name                                   string
}

type UpdateFileTreeNodeInput struct {
	WorkspaceID, KnowledgeBaseID, NodeID uuid.UUID
	Name                                 *string
	ParentID                             *uuid.UUID
}

type MoveFileTreeNodeInput struct {
	WorkspaceID, KnowledgeBaseID, NodeID, NewParentID uuid.UUID
}

func (s *FileTreeService) List(ctx context.Context, workspaceID, knowledgeBaseID uuid.UUID) (*dto.FileTree, error) {
	var nodes []*model.FileTreeNode
	err := s.store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, tx FileTreeTx) error {
		var err error
		nodes, err = tx.ListFileTreeNodes(txCtx, knowledgeBaseID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return buildFileTreeDTO(workspaceID, knowledgeBaseID, nodes)
}

func (s *FileTreeService) CreateFolder(ctx context.Context, input CreateFileTreeFolderInput) (*dto.FileTreeNode, error) {
	name, err := normalizeFileTreeName(input.Name)
	if err != nil {
		return nil, err
	}
	var created *model.FileTreeNode
	err = s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx FileTreeTx) error {
		parent, err := tx.GetFileTreeNodeForUpdate(txCtx, input.ParentID)
		if err != nil {
			return err
		}
		if !fileTreeParentMatches(parent, input.WorkspaceID, input.KnowledgeBaseID) {
			return domainerrors.ErrNotFound
		}
		created, err = model.NewFileTreeNode(model.NewFileTreeNodeInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			ParentID: &input.ParentID, NodeType: value.FileTreeNodeFolder, Name: name,
		})
		if err != nil {
			return err
		}
		return tx.CreateFileTreeNode(txCtx, created)
	})
	if err != nil {
		return nil, err
	}
	tree, err := s.List(ctx, input.WorkspaceID, input.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if result := findFileTreeNodeDTO(tree.Root, created.ID); result != nil {
		return result, nil
	}
	return nil, domainerrors.ErrNotFound
}

func (s *FileTreeService) Move(ctx context.Context, input MoveFileTreeNodeInput) error {
	return s.Update(ctx, UpdateFileTreeNodeInput{
		WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		NodeID: input.NodeID, ParentID: &input.NewParentID,
	})
}

func (s *FileTreeService) Update(ctx context.Context, input UpdateFileTreeNodeInput) error {
	if input.Name == nil && input.ParentID == nil {
		return fmt.Errorf("%w: name 与 parent_id 至少提供一个", domainerrors.ErrValidation)
	}
	var normalizedName *string
	if input.Name != nil {
		name, err := normalizeFileTreeName(*input.Name)
		if err != nil {
			return err
		}
		normalizedName = &name
	}
	return s.store.WithinWorkspace(ctx, input.WorkspaceID, func(txCtx context.Context, tx FileTreeTx) error {
		current, err := tx.GetFileTreeNodeForUpdate(txCtx, input.NodeID)
		if err != nil {
			return err
		}
		if current.KnowledgeBaseID != input.KnowledgeBaseID || current.WorkspaceID != input.WorkspaceID {
			return domainerrors.ErrNotFound
		}
		if current.NodeType == value.FileTreeNodeRoot {
			return fmt.Errorf("%w: root 节点不可修改", domainerrors.ErrValidation)
		}
		updated := *current
		if input.ParentID != nil {
			parent, err := tx.GetFileTreeNodeForUpdate(txCtx, *input.ParentID)
			if err != nil {
				return err
			}
			if !fileTreeParentMatches(parent, input.WorkspaceID, input.KnowledgeBaseID) {
				return domainerrors.ErrNotFound
			}
			if updated.NodeType == value.FileTreeNodeFolder {
				cycle, err := tx.WouldCreateCycle(txCtx, updated.ID, parent.ID)
				if err != nil {
					return err
				}
				if cycle {
					return domainerrors.ErrFileTreeCycle
				}
			}
			parentID := parent.ID
			updated.ParentID = &parentID
		}
		var document *model.Document
		if normalizedName != nil {
			updated.Name = *normalizedName
			if updated.NodeType == value.FileTreeNodeFile {
				if updated.DocumentID == nil {
					return fmt.Errorf("%w: File 节点缺少 Document", domainerrors.ErrValidation)
				}
				document, err = tx.GetDocumentForUpdate(txCtx, *updated.DocumentID)
				if err != nil {
					return err
				}
				if document.WorkspaceID != input.WorkspaceID || document.KnowledgeBaseID != input.KnowledgeBaseID || document.Kind != value.DocumentKindFile {
					return domainerrors.ErrNotFound
				}
				documentCopy := *document
				documentCopy.Title = *normalizedName
				documentCopy.UpdatedAt = time.Now().UTC()
				document = &documentCopy
			}
		}
		updated.UpdatedAt = time.Now().UTC()
		return tx.SaveFileTreeNode(txCtx, &updated, document)
	})
}

func (s *FileTreeService) Delete(ctx context.Context, workspaceID, knowledgeBaseID, nodeID uuid.UUID) error {
	return s.store.WithinWorkspace(ctx, workspaceID, func(txCtx context.Context, tx FileTreeTx) error {
		node, err := tx.GetFileTreeNodeForUpdate(txCtx, nodeID)
		if err != nil {
			return err
		}
		if node.WorkspaceID != workspaceID || node.KnowledgeBaseID != knowledgeBaseID {
			return domainerrors.ErrNotFound
		}
		if node.NodeType != value.FileTreeNodeFolder {
			return fmt.Errorf("%w: 只能删除 folder 节点", domainerrors.ErrValidation)
		}
		hasChildren, err := tx.HasFileTreeChildren(txCtx, nodeID)
		if err != nil {
			return err
		}
		if hasChildren {
			return domainerrors.ErrFileTreeNotEmpty
		}
		return tx.DeleteFileTreeNode(txCtx, nodeID)
	})
}

func fileTreeParentMatches(node *model.FileTreeNode, workspaceID, knowledgeBaseID uuid.UUID) bool {
	return node != nil && node.WorkspaceID == workspaceID && node.KnowledgeBaseID == knowledgeBaseID &&
		(node.NodeType == value.FileTreeNodeRoot || node.NodeType == value.FileTreeNodeFolder)
}

func normalizeFileTreeName(input string) (string, error) {
	name := strings.TrimSpace(input)
	if name == "" || strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		return "", fmt.Errorf("%w: 文件树名称必须是非空单段名称", domainerrors.ErrValidation)
	}
	return name, nil
}

func buildFileTreeDTO(workspaceID, knowledgeBaseID uuid.UUID, nodes []*model.FileTreeNode) (*dto.FileTree, error) {
	items := make(map[uuid.UUID]*dto.FileTreeNode, len(nodes))
	var rootID uuid.UUID
	for _, node := range nodes {
		if node.WorkspaceID != workspaceID || node.KnowledgeBaseID != knowledgeBaseID {
			continue
		}
		items[node.ID] = fileTreeNodeDTO(node, "", make([]*dto.FileTreeNode, 0))
		if node.NodeType == value.FileTreeNodeRoot {
			rootID = node.ID
		}
	}
	root := items[rootID]
	if root == nil {
		return nil, domainerrors.ErrNotFound
	}
	for _, item := range items {
		if item.ParentID != nil {
			parent := items[*item.ParentID]
			if parent == nil {
				return nil, fmt.Errorf("%w: 文件树存在孤立节点", domainerrors.ErrValidation)
			}
			parent.Children = append(parent.Children, item)
		}
	}
	var assignPaths func(*dto.FileTreeNode, string)
	assignPaths = func(node *dto.FileTreeNode, parentPath string) {
		if node.NodeType == value.FileTreeNodeRoot {
			node.Path = "/"
		} else if parentPath == "/" {
			node.Path = "/" + node.Name
		} else {
			node.Path = parentPath + "/" + node.Name
		}
		sort.Slice(node.Children, func(i, j int) bool {
			left, right := node.Children[i], node.Children[j]
			if left.NodeType != right.NodeType {
				return left.NodeType < right.NodeType
			}
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		})
		for _, child := range node.Children {
			assignPaths(child, node.Path)
		}
	}
	assignPaths(root, "")
	return &dto.FileTree{WorkspaceID: workspaceID, KnowledgeBaseID: knowledgeBaseID, Root: root}, nil
}

func fileTreeNodeDTO(node *model.FileTreeNode, path string, children []*dto.FileTreeNode) *dto.FileTreeNode {
	name := node.Name
	if node.NodeType == value.FileTreeNodeRoot && strings.TrimSpace(name) == "" {
		name = "文件"
	}
	return &dto.FileTreeNode{
		ID: node.ID, ParentID: node.ParentID, NodeType: node.NodeType, Name: name,
		DocumentID: node.DocumentID, Path: path, Children: children,
	}
}

func findFileTreeNodeDTO(node *dto.FileTreeNode, id uuid.UUID) *dto.FileTreeNode {
	if node == nil || node.ID == id {
		return node
	}
	for _, child := range node.Children {
		if found := findFileTreeNodeDTO(child, id); found != nil {
			return found
		}
	}
	return nil
}
