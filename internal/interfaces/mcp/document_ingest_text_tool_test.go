package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 document_ingest_text 工具行为：必填校验、InlineLimit、
// parent_node_id 解析、幂等键透传（CallerAPIKeyID 取自 API Key 主体）、
// scope/KB 绑定拒绝与输出形状。

// callTextTool 组装带鉴权上下文的 document_ingest_text 调用。
func callTextTool(t *testing.T, deps Dependencies, auth value.AuthContext, args string) *mcplib.CallToolResult {
	t.Helper()
	srv := NewServer(deps)
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"document_ingest_text","arguments":%s}}`, args)
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	resp := srv.MCP().HandleMessage(ctx, json.RawMessage(payload))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok, "response type = %T", resp)
	result, ok := jsonResp.Result.(*mcplib.CallToolResult)
	require.True(t, ok, "result type = %T", jsonResp.Result)
	return result
}

func TestDocumentIngestTextPassesServiceInput(t *testing.T) {
	kbID := uuid.New()
	parentID := uuid.New()
	ingest := &fakeMCPIngestService{}
	deps := ingestTestDeps(ingest, nil)

	result := callTextTool(t, deps, ingestTestAuth(kbID), fmt.Sprintf(`{
		"knowledge_base_id":%q,"title":"会话纪要","content":"# 纪要\n要点",
		"parent_node_id":%q,"idempotency_key":"abc123"
	}`, kbID, parentID))
	out := structuredOf(t, result)
	require.NotEmpty(t, out["document_id"])
	require.NotEmpty(t, out["job_id"])
	require.Equal(t, "pending", out["status"])
	require.Equal(t, false, out["deduped"])

	require.Equal(t, 1, ingest.calls)
	in := ingest.input
	require.Equal(t, kbID, in.KnowledgeBaseID)
	require.Equal(t, "会话纪要.md", in.FileName)
	require.Equal(t, "text/markdown", in.ContentType)
	require.Equal(t, "会话纪要", in.Title)
	require.Equal(t, "会话纪要", in.NodeName) // node_name 缺省回落标题
	require.NotNil(t, in.ParentNodeID)
	require.Equal(t, parentID, *in.ParentNodeID)
	require.Equal(t, "abc123", in.IdempotencyKey)
	require.NotNil(t, in.CallerAPIKeyID) // API Key 主体 → PrincipalID
	content, err := io.ReadAll(in.Reader)
	require.NoError(t, err)
	require.Equal(t, "# 纪要\n要点", string(content))
	require.Equal(t, int64(len("# 纪要\n要点")), in.SizeBytes)
}

func TestDocumentIngestTextValidatesRequiredFields(t *testing.T) {
	kbID := uuid.New()
	cases := []struct {
		name string
		args string
	}{
		{"title 为空", fmt.Sprintf(`{"knowledge_base_id":%q,"title":"  ","content":"x"}`, kbID)},
		{"content 为空", fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","content":""}`, kbID)},
		{"parent_node_id 非法", fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","content":"x","parent_node_id":"nope"}`, kbID)},
		{"knowledge_base_id 非法", `{"knowledge_base_id":"nope","title":"t","content":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ingest := &fakeMCPIngestService{}
			result := callTextTool(t, ingestTestDeps(ingest, nil), ingestTestAuth(kbID), tc.args)
			parsed := errorStructuredOf(t, result)
			require.Equal(t, "validation_error", parsed.Error.Code)
			require.Zero(t, ingest.calls)
		})
	}
}

func TestDocumentIngestTextRejectsOversizeContent(t *testing.T) {
	kbID := uuid.New()
	deps := ingestTestDeps(&fakeMCPIngestService{}, nil)
	deps.InlineLimit = 8
	result := callTextTool(t, deps, ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","content":"%s"}`, kbID, strings.Repeat("a", 16)))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "internal_error", parsed.Error.Code) // 超限错误未包领域 sentinel，走 internal 兜底
}

func TestDocumentIngestTextScopeAndBindingDenied(t *testing.T) {
	kbID := uuid.New()
	// 无 documents:write scope：ToolFilter 在 tools/call 层直接拒绝（tool not found）。
	noScope := value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsRead}, []uuid.UUID{kbID})
	assertToolFilterDenial(t, callMCPToolRaw(ingestTestDeps(&fakeMCPIngestService{}, nil), noScope,
		"document_ingest_text", fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","content":"x"}`, kbID)))

	// KB 不在绑定集合：工具可见（有 write scope），handler 层拒绝。
	unbound := ingestTestAuth(uuid.New())
	result := callTextTool(t, ingestTestDeps(&fakeMCPIngestService{}, nil), unbound,
		fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","content":"x"}`, kbID))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "not_found", parsed.Error.Code)
}

func TestDocumentIngestTextServiceErrorPassthrough(t *testing.T) {
	kbID := uuid.New()
	ingest := &errIngestService{err: fmt.Errorf("%w: 幂等键冲突", domainerrors.ErrValidation)}
	result := callTextTool(t, ingestTestDeps(ingest, nil), ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","content":"x"}`, kbID))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "validation_error", parsed.Error.Code)
}

// errIngestService 恒定返回注入错误。
type errIngestService struct{ err error }

func (e *errIngestService) Ingest(context.Context, MCPIngestInput) (*MCPIngestResult, error) {
	return nil, e.err
}
