package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ===== file_tree_list / file_tree_folder_create / file_tree_node_update / file_tree_node_delete =====
//
// 文件树是知识库的内容组织结构（folder/file 两级节点 + 虚拟 root）。file 节点
// 关联文档，删除文档本体走 document_delete（树节点随文档删除联动）。业务规则
// 在 service 层（FileTreeService），本文件只做协议转换。

type fileTreeListInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
}

// fileTreeNodeOutput 是树节点的递归输出形状；root 是虚拟根节点（node_type=root）。
type fileTreeNodeOutput struct {
	ID         string               `json:"id"`
	ParentID   string               `json:"parent_id,omitempty"`
	NodeType   string               `json:"node_type"`
	Name       string               `json:"name"`
	DocumentID string               `json:"document_id,omitempty"`
	Path       string               `json:"path"`
	Children   []fileTreeNodeOutput `json:"children"`
}

type fileTreeListOutput struct {
	KnowledgeBaseID string             `json:"knowledge_base_id"`
	Root            fileTreeNodeOutput `json:"root"`
}

// fileTreeListOutputSchema 是 file_tree_list 的手写输出 schema：节点树是递归
// 结构，jsonschema-go 的 For[T] 不支持自引用类型（cycle detected），这里用
// 标准 $defs/$ref 表达；mcp-go 输出校验器（santhosh-tekuri/jsonschema）完整
// 支持 $ref 解析。
var fileTreeListOutputSchema = json.RawMessage(`{
  "type": "object",
  "description": "文件树：root 为虚拟根节点，children 递归嵌套",
  "properties": {
    "knowledge_base_id": {"type": "string", "description": "知识库 ID"},
    "root": {"$ref": "#/$defs/node"}
  },
  "required": ["knowledge_base_id", "root"],
  "$defs": {
    "node": {
      "type": "object",
      "description": "树节点：node_type=root（虚拟根）/ folder / file（关联文档）",
      "properties": {
        "id": {"type": "string", "description": "节点 ID"},
        "parent_id": {"type": "string", "description": "父节点 ID（根节点省略）"},
        "node_type": {"type": "string", "enum": ["root", "folder", "file"]},
        "name": {"type": "string", "description": "节点名（file 节点为文件名）"},
        "document_id": {"type": "string", "description": "关联文档 ID（仅 file 节点）"},
        "path": {"type": "string", "description": "从根开始的完整路径"},
        "children": {"type": "array", "items": {"$ref": "#/$defs/node"}, "description": "子节点（按 folder 先、名称序）"}
      },
      "required": ["id", "node_type", "name", "path", "children"]
    }
  }
}`)

// fileTreeNodeOf 把 dto.FileTreeNode 递归转换为字符串 ID 的输出形状
// （uuid → string，指针 → omitempty 空串省略）。
func fileTreeNodeOf(node *dto.FileTreeNode) fileTreeNodeOutput {
	if node == nil {
		return fileTreeNodeOutput{Children: []fileTreeNodeOutput{}}
	}
	out := fileTreeNodeOutput{
		ID: node.ID.String(), NodeType: string(node.NodeType),
		Name: node.Name, Path: node.Path,
		Children: make([]fileTreeNodeOutput, 0, len(node.Children)),
	}
	if node.ParentID != nil {
		out.ParentID = node.ParentID.String()
	}
	if node.DocumentID != nil {
		out.DocumentID = node.DocumentID.String()
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, fileTreeNodeOf(child))
	}
	return out
}

type fileTreeFolderCreateInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"目标知识库 ID"`
	ParentID        string `json:"parent_id" jsonschema:"父节点 ID（根目录传根节点的 ID，从 file_tree_list 获取）"`
	Name            string `json:"name" jsonschema:"文件夹名称（同级不能重名）"`
}

type fileTreeFolderCreateOutput struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	NodeType string `json:"node_type"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

type fileTreeNodeUpdateInput struct {
	KnowledgeBaseID string  `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	NodeID          string  `json:"node_id" jsonschema:"节点 ID（file_tree_list 获取）"`
	Name            *string `json:"name,omitempty" jsonschema:"新名称（重命名；file 节点会联动更新文档标题）"`
	ParentID        *string `json:"parent_id,omitempty" jsonschema:"新父节点 ID（移动）"`
}

type fileTreeNodeDeleteInput struct {
	KnowledgeBaseID string `json:"knowledge_base_id" jsonschema:"知识库 ID"`
	NodeID          string `json:"node_id" jsonschema:"要删除的节点 ID"`
}

type fileTreeNodeMutationOutput struct {
	NodeID  string `json:"node_id"`
	Deleted bool   `json:"deleted"`
	Updated bool   `json:"updated"`
}

func registerFileTreeTools(srv *mcpserver.MCPServer, deps Dependencies) {
	registerFileTreeList(srv, deps)
	registerFileTreeFolderCreate(srv, deps)
	registerFileTreeNodeUpdate(srv, deps)
	registerFileTreeNodeDelete(srv, deps)
}

func registerFileTreeList(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("file_tree_list",
		mcp.WithDescription("列出知识库的完整文件树（文件夹 + 文件，嵌套结构）。"+
			"document_ingest / document_ingest_text 的 parent_node_id、file_tree_folder_create 的 parent_id 都从这里获取；path 字段给出节点的完整路径。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		withRawInputSchema[fileTreeListInput](),
		withRawOutputSchemaFrom(fileTreeListOutputSchema),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in fileTreeListInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsRead) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		tree, err := deps.FileTree.List(ctx, auth.ResourceAccess(), kbID)
		if err != nil {
			return toErrorResult(err), nil
		}
		out := fileTreeListOutput{KnowledgeBaseID: kbID.String(), Root: fileTreeNodeOf(tree.Root)}
		return jsonResult(out), nil
	}))
}

func registerFileTreeFolderCreate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("file_tree_folder_create",
		mcp.WithDescription("在文件树中创建一个文件夹。parent_id 必须是已存在的父节点（根目录用 file_tree_list 中 root 节点的 ID）；同级名称冲突会报错。"),
		withRawInputSchema[fileTreeFolderCreateInput](),
		withRawOutputSchema[fileTreeFolderCreateOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in fileTreeFolderCreateInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		parentID, err := uuid.Parse(in.ParentID)
		if err != nil {
			return toErrorResult(fmt.Errorf("parent_id 必须是有效 UUID")), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		node, err := deps.FileTree.CreateFolder(ctx, service.CreateFileTreeFolderInput{
			WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID,
			ParentID: parentID, Access: auth.ResourceAccess(), Name: in.Name,
		})
		if err != nil {
			return toErrorResult(err), nil
		}
		out := fileTreeFolderCreateOutput{
			ID: node.ID.String(), NodeType: string(node.NodeType),
			Name: node.Name, Path: node.Path,
		}
		if node.ParentID != nil {
			out.ParentID = node.ParentID.String()
		}
		return jsonResult(out), nil
	}))
}

func registerFileTreeNodeUpdate(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("file_tree_node_update",
		mcp.WithDescription("重命名节点或移动节点到新父目录（name / parent_id 至少提供一个，可同时提供）。"+
			"重命名 file 节点会联动更新文档标题；移动到自己的后代节点会因成环被拒绝。"),
		withRawInputSchema[fileTreeNodeUpdateInput](),
		withRawOutputSchema[fileTreeNodeMutationOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in fileTreeNodeUpdateInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		nodeID, err := uuid.Parse(in.NodeID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: node_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if in.Name == nil && in.ParentID == nil {
			return toErrorResult(fmt.Errorf("%w: name 与 parent_id 至少提供一个", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		input := service.UpdateFileTreeNodeInput{
			WorkspaceID: auth.WorkspaceID, KnowledgeBaseID: kbID,
			NodeID: nodeID, Access: auth.ResourceAccess(), Name: in.Name,
		}
		if in.ParentID != nil {
			parsed, err := uuid.Parse(*in.ParentID)
			if err != nil {
				return toErrorResult(fmt.Errorf("%w: parent_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
			}
			input.ParentID = &parsed
		}
		if err := deps.FileTree.Update(ctx, input); err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(fileTreeNodeMutationOutput{NodeID: nodeID.String(), Updated: true}), nil
	}))
}

func registerFileTreeNodeDelete(srv *mcpserver.MCPServer, deps Dependencies) {
	tool := mcp.NewTool("file_tree_node_delete",
		mcp.WithDescription("删除文件树中的空文件夹（必须无子节点；非空会报错，需先移走或删除子内容）。"+
			"file 节点不能通过本工具删除——删除文档本体请用 document_delete（树节点随之清理）。"),
		mcp.WithDestructiveHintAnnotation(true),
		withRawInputSchema[fileTreeNodeDeleteInput](),
		withRawOutputSchema[fileTreeNodeMutationOutput](),
	)
	srv.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, in fileTreeNodeDeleteInput) (*mcp.CallToolResult, error) {
		auth, err := authFromRequest(ctx)
		if err != nil {
			return toErrorResult(err), nil
		}
		if !auth.HasScope(value.ScopeDocumentsWrite) {
			return toErrorResult(domainerrors.ErrInsufficientScope), nil
		}
		kbID, err := uuid.Parse(in.KnowledgeBaseID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: knowledge_base_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		nodeID, err := uuid.Parse(in.NodeID)
		if err != nil {
			return toErrorResult(fmt.Errorf("%w: node_id 必须是有效 UUID", domainerrors.ErrValidation)), nil
		}
		if !auth.ResourceAccess().AllowsKnowledgeBase(kbID) {
			return toErrorResult(domainerrors.ErrNotFound), nil
		}
		if err := deps.FileTree.Delete(ctx, auth.ResourceAccess(), kbID, nodeID); err != nil {
			return toErrorResult(err), nil
		}
		return jsonResult(fileTreeNodeMutationOutput{NodeID: nodeID.String(), Deleted: true}), nil
	}))
}
