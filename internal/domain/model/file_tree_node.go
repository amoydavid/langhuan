package model

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
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
	// ExternalID 记录外部内容源（飞书）的稳定节点标识，供同步对账使用；可空。
	ExternalID string
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
	// DocumentKind 是 file 节点关联文档的类型（documents.kind，FK 复合键的一部分）：
	// 上传/飞书文档为 file，web 页面为 web（spec §3.3 目录投影）。零值按 file 处理。
	DocumentKind value.DocumentKind
	// ExternalID 记录外部内容源（飞书）的稳定节点标识，供同步对账使用；可空。
	ExternalID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewFileTreeNode validates node shape and document ownership.
// file 节点可关联 file（上传/飞书）或 web（网站页面，v1.3.0）两类文档。
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
		if nilOrEmptyUUID(input.ParentID) || nilOrEmptyUUID(input.DocumentID) || name == "" ||
			(input.DocumentKind != value.DocumentKindFile && input.DocumentKind != value.DocumentKindWeb) {
			return nil, fmt.Errorf("%w: file 节点必须关联 File/Web Document 并包含 parent/name", domainerrors.ErrValidation)
		}
	}
	documentKind := input.DocumentKind
	if documentKind == "" {
		documentKind = value.DocumentKindFile
	}
	now := time.Now().UTC()
	return &FileTreeNode{
		ID: id.New(), WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
		ParentID: input.ParentID, NodeType: input.NodeType, Name: name,
		DocumentID: input.DocumentID, DocumentKind: documentKind,
		ExternalID: strings.TrimSpace(input.ExternalID),
		CreatedAt:  now, UpdatedAt: now,
	}, nil
}

func nilOrEmptyUUID(id *uuid.UUID) bool {
	return id == nil || *id == uuid.Nil
}
