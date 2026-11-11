package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 file_tree_* 工具组：嵌套树输出转换、folder 创建输入、
// update 至少一字段校验、delete 错误透传与 scope/绑定拒绝。

// fakeMCPFileTreeService 记录文件树端口调用。
type fakeMCPFileTreeService struct {
	listCalls   int
	listKBID    uuid.UUID
	folderCalls int
	folderInput service.CreateFileTreeFolderInput
	updateCalls int
	updateInput service.UpdateFileTreeNodeInput
	deleteCalls int
	deleteIDs   [2]uuid.UUID
	tree        *dto.FileTree
	folder      *dto.FileTreeNode
	updateErr   error
	deleteErr   error
}

func (f *fakeMCPFileTreeService) List(_ context.Context, access value.ResourceAccess, knowledgeBaseID uuid.UUID) (*dto.FileTree, error) {
	f.listCalls++
	f.listKBID = knowledgeBaseID
	return f.tree, nil
}

func (f *fakeMCPFileTreeService) CreateFolder(_ context.Context, input service.CreateFileTreeFolderInput) (*dto.FileTreeNode, error) {
	f.folderCalls++
	f.folderInput = input
	return f.folder, nil
}

func (f *fakeMCPFileTreeService) Update(_ context.Context, input service.UpdateFileTreeNodeInput) error {
	f.updateCalls++
	f.updateInput = input
	return f.updateErr
}

func (f *fakeMCPFileTreeService) Delete(_ context.Context, access value.ResourceAccess, knowledgeBaseID, nodeID uuid.UUID) error {
	f.deleteCalls++
	f.deleteIDs = [2]uuid.UUID{knowledgeBaseID, nodeID}
	return f.deleteErr
}

func fileTreeTestDeps(tree MCPFileTreeService) Dependencies {
	return Dependencies{FileTree: tree, InlineLimit: 8 * 1024 * 1024}
}

func callTreeTool(t *testing.T, deps Dependencies, auth value.AuthContext, tool, args string) *mcplib.CallToolResult {
	t.Helper()
	srv := NewServer(deps)
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args)
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	resp := srv.MCP().HandleMessage(ctx, json.RawMessage(payload))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok, "response type = %T", resp)
	result, ok := jsonResp.Result.(*mcplib.CallToolResult)
	require.True(t, ok, "result type = %T", jsonResp.Result)
	return result
}

func treeAuth(kbID uuid.UUID) value.AuthContext {
	return value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsRead, value.ScopeDocumentsWrite}, []uuid.UUID{kbID})
}

// treeFixture：root → 文件夹"指南" → 文件"快速开始"。
func treeFixture() *dto.FileTree {
	kbID, folderID, fileID, docID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	parentOfFolder := kbID
	return &dto.FileTree{
		WorkspaceID:     uuid.New(),
		KnowledgeBaseID: kbID,
		Root: &dto.FileTreeNode{
			ID: parentOfFolder, NodeType: value.FileTreeNodeRoot, Name: "", Path: "",
			Children: []*dto.FileTreeNode{{
				ID: folderID, ParentID: &parentOfFolder, NodeType: value.FileTreeNodeFolder,
				Name: "指南", Path: "/指南",
				Children: []*dto.FileTreeNode{{
					ID: fileID, ParentID: &folderID, NodeType: value.FileTreeNodeFile,
					Name: "快速开始", DocumentID: &docID, Path: "/指南/快速开始",
				}},
			}},
		},
	}
}

func TestFileTreeListNestedOutput(t *testing.T) {
	kbID := uuid.New()
	tree := treeFixture()
	tree.KnowledgeBaseID = kbID
	fake := &fakeMCPFileTreeService{tree: tree}

	result := callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_list",
		fmt.Sprintf(`{"knowledge_base_id":%q}`, kbID))
	out := structuredOf(t, result)
	require.Equal(t, kbID.String(), out["knowledge_base_id"])

	root := out["root"].(map[string]any)
	require.Equal(t, "root", root["node_type"])
	require.Equal(t, []any{}, root["children"].([]any)[0:0]) // children 恒非 nil

	children := root["children"].([]any)
	require.Len(t, children, 1)
	folder := children[0].(map[string]any)
	require.Equal(t, "folder", folder["node_type"])
	require.Equal(t, "指南", folder["name"])
	require.Equal(t, "/指南", folder["path"])
	// document_id 仅 file 节点输出。
	require.NotContains(t, folder, "document_id")

	leaf := folder["children"].([]any)[0].(map[string]any)
	require.Equal(t, "file", leaf["node_type"])
	require.NotEmpty(t, leaf["document_id"])
	require.Equal(t, kbID, fake.listKBID)
}

func TestFileTreeFolderCreate(t *testing.T) {
	kbID, parentID := uuid.New(), uuid.New()
	nodeID := uuid.New()
	fake := &fakeMCPFileTreeService{folder: &dto.FileTreeNode{
		ID: nodeID, ParentID: &parentID, NodeType: value.FileTreeNodeFolder,
		Name: "新目录", Path: "/新目录", Children: []*dto.FileTreeNode{},
	}}

	result := callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_folder_create",
		fmt.Sprintf(`{"knowledge_base_id":%q,"parent_id":%q,"name":"新目录"}`, kbID, parentID))
	out := structuredOf(t, result)
	require.Equal(t, nodeID.String(), out["id"])
	require.Equal(t, "folder", out["node_type"])
	require.Equal(t, "/新目录", out["path"])

	require.Equal(t, 1, fake.folderCalls)
	require.Equal(t, parentID, fake.folderInput.ParentID)
	require.Equal(t, "新目录", fake.folderInput.Name)
	require.Equal(t, kbID, fake.folderInput.KnowledgeBaseID)
}

func TestFileTreeNodeUpdateRequiresAtLeastOneField(t *testing.T) {
	kbID, nodeID := uuid.New(), uuid.New()
	fake := &fakeMCPFileTreeService{}

	parsed := errorStructuredOf(t, callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_node_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q}`, kbID, nodeID)))
	require.Equal(t, "validation_error", parsed.Error.Code)
	require.Zero(t, fake.updateCalls)

	// 只改名。
	newName := "改名"
	structuredOf(t, callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_node_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q,"name":"改名"}`, kbID, nodeID)))
	require.Equal(t, 1, fake.updateCalls)
	require.NotNil(t, fake.updateInput.Name)
	require.Equal(t, newName, *fake.updateInput.Name)
	require.Nil(t, fake.updateInput.ParentID)

	// 非法 parent_id。
	parsed = errorStructuredOf(t, callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_node_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q,"parent_id":"bad"}`, kbID, nodeID)))
	require.Equal(t, "validation_error", parsed.Error.Code)
}

func TestFileTreeNodeDeleteErrorPassthrough(t *testing.T) {
	kbID, nodeID := uuid.New(), uuid.New()
	fake := &fakeMCPFileTreeService{deleteErr: domainerrors.ErrFileTreeNotEmpty}

	parsed := errorStructuredOf(t, callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_node_delete",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q}`, kbID, nodeID)))
	require.Equal(t, "validation_error", parsed.Error.Code)
	require.Equal(t, [2]uuid.UUID{kbID, nodeID}, fake.deleteIDs)

	// 成功路径。
	fake.deleteErr = nil
	out := structuredOf(t, callTreeTool(t, fileTreeTestDeps(fake), treeAuth(kbID), "file_tree_node_delete",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q}`, kbID, nodeID)))
	require.Equal(t, true, out["deleted"])
}

func TestFileTreeWriteScopeDenied(t *testing.T) {
	kbID := uuid.New()
	tree := treeFixture()
	tree.KnowledgeBaseID = kbID
	fake := &fakeMCPFileTreeService{tree: tree}
	readOnly := value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsRead}, []uuid.UUID{kbID})

	// 读放行。
	structuredOf(t, callTreeTool(t, fileTreeTestDeps(fake), readOnly, "file_tree_list",
		fmt.Sprintf(`{"knowledge_base_id":%q}`, kbID)))
	require.Equal(t, 1, fake.listCalls)

	// 写拒绝：ToolFilter 在 tools/call 层直接拒绝（参数需先过各自 schema）。
	assertToolFilterDenial(t, callMCPToolRaw(fileTreeTestDeps(fake), readOnly, "file_tree_folder_create",
		fmt.Sprintf(`{"knowledge_base_id":%q,"parent_id":%q,"name":"x"}`, kbID, uuid.New())))
	assertToolFilterDenial(t, callMCPToolRaw(fileTreeTestDeps(fake), readOnly, "file_tree_node_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q,"name":"x"}`, kbID, uuid.New())))
	assertToolFilterDenial(t, callMCPToolRaw(fileTreeTestDeps(fake), readOnly, "file_tree_node_delete",
		fmt.Sprintf(`{"knowledge_base_id":%q,"node_id":%q}`, kbID, uuid.New())))
}
