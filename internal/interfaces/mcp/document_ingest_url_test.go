package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 document_ingest 的 url 入口（v1.3.0 spec §9.3）的 MCP 工具行为：
// url 字段提供时分派到 web 源并入端口（documents:write scope、KB 绑定校验不变），
// 与 file_name/content_base64 互斥；url 缺省时走既有文件导入路径，行为零变化。

// fakeMCPIngestService 记录文件导入端口的调用。
type fakeMCPIngestService struct {
	calls int
	input MCPIngestInput
}

func (f *fakeMCPIngestService) Ingest(_ context.Context, input MCPIngestInput) (*MCPIngestResult, error) {
	f.calls++
	f.input = input
	return &MCPIngestResult{DocumentID: uuid.New(), JobID: uuid.New(), Status: "pending"}, nil
}

// fakeMCPWebSourceService 记录 url 并入端口的调用，可注入错误。
type fakeMCPWebSourceService struct {
	calls  int
	wsID   uuid.UUID
	kbID   uuid.UUID
	url    string
	source *model.KbSource
	job    *model.Job
	err    error
}

func (f *fakeMCPWebSourceService) AddURLToWebSource(_ context.Context, workspaceID, kbID uuid.UUID, rawURL string) (*model.KbSource, *model.Job, error) {
	f.calls++
	f.wsID, f.kbID, f.url = workspaceID, kbID, rawURL
	if f.err != nil {
		return nil, nil, f.err
	}
	if f.source == nil {
		f.source = &model.KbSource{ID: uuid.New(), Name: "帮助中心", Status: value.KbSourceStatusActive}
	}
	return f.source, f.job, nil
}

func ingestTestDeps(ingest MCPDocumentIngestService, webSources MCPWebSourceService) Dependencies {
	return Dependencies{
		DocumentIngest: ingest,
		WebSources:     webSources,
		InlineLimit:    8 * 1024 * 1024,
	}
}

func ingestTestAuth(kbID uuid.UUID) value.AuthContext {
	return value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsWrite}, []uuid.UUID{kbID})
}

// callIngestTool 组装带鉴权上下文的 tools/call 请求并返回结果。
func callIngestTool(t *testing.T, deps Dependencies, auth value.AuthContext, arguments string) *mcplib.CallToolResult {
	t.Helper()
	srv := NewServer(deps)
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"document_ingest","arguments":%s}}`, arguments)
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	resp := srv.MCP().HandleMessage(ctx, json.RawMessage(payload))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok, "response type = %T", resp)
	result, ok := jsonResp.Result.(*mcplib.CallToolResult)
	require.True(t, ok, "result type = %T", jsonResp.Result)
	return result
}

// structuredOf 把 CallToolResult 的 StructuredContent 转成 map 便于断言。
func structuredOf(t *testing.T, result *mcplib.CallToolResult) map[string]any {
	t.Helper()
	require.False(t, result.IsError)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

// errorStructuredOf 把 isError 结果的 StructuredContent 解析成 mcpError。
func errorStructuredOf(t *testing.T, result *mcplib.CallToolResult) mcpError {
	t.Helper()
	require.True(t, result.IsError)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var parsed mcpError
	require.NoError(t, json.Unmarshal(data, &parsed))
	return parsed
}

func TestDocumentIngestURLModeDispatchesToWebSource(t *testing.T) {
	kbID := uuid.New()
	ingest := &fakeMCPIngestService{}
	webSources := &fakeMCPWebSourceService{job: &model.Job{ID: uuid.New()}}
	deps := ingestTestDeps(ingest, webSources)

	result := callIngestTool(t, deps, ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"url":"https://example.com/entry"}`, kbID))
	out := structuredOf(t, result)
	require.Equal(t, webSources.source.ID.String(), out["kb_source_id"])
	require.Equal(t, "帮助中心", out["kb_source_name"])
	require.Equal(t, "https://example.com/entry", out["url"])
	require.Equal(t, webSources.job.ID.String(), out["job_id"])
	require.Equal(t, true, out["sync_queued"])

	// 文件导入路径不得被触碰；lineage 与 URL 原样透传。
	require.Zero(t, ingest.calls)
	require.Equal(t, 1, webSources.calls)
	require.Equal(t, kbID, webSources.kbID)
	require.Equal(t, "https://example.com/entry", webSources.url)
}

func TestDocumentIngestURLModeJobOptional(t *testing.T) {
	kbID := uuid.New()
	// 入队 best-effort：job 为 nil 时响应无 job_id 且 sync_queued=false。
	webSources := &fakeMCPWebSourceService{}
	result := callIngestTool(t, ingestTestDeps(&fakeMCPIngestService{}, webSources), ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"url":"https://example.com/entry"}`, kbID))
	out := structuredOf(t, result)
	require.NotContains(t, out, "job_id")
	require.Equal(t, false, out["sync_queued"])
}

func TestDocumentIngestURLMutuallyExclusiveWithFileFields(t *testing.T) {
	kbID := uuid.New()
	ingest := &fakeMCPIngestService{}
	webSources := &fakeMCPWebSourceService{}

	for _, arguments := range []string{
		fmt.Sprintf(`{"knowledge_base_id":%q,"url":"https://example.com/x","content_base64":"aGVsbG8="}`, kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"url":"https://example.com/x","file_name":"a.txt"}`, kbID),
	} {
		result := callIngestTool(t, ingestTestDeps(ingest, webSources), ingestTestAuth(kbID), arguments)
		parsed := errorStructuredOf(t, result)
		require.Equal(t, "validation_error", parsed.Error.Code)
		require.Contains(t, parsed.Error.Message, "不能同时提供")
	}
	require.Zero(t, ingest.calls)
	require.Zero(t, webSources.calls)
}

func TestDocumentIngestFileModeUnchanged(t *testing.T) {
	kbID := uuid.New()
	ingest := &fakeMCPIngestService{}
	webSources := &fakeMCPWebSourceService{}

	// 不带 url 字段：走既有文件导入路径，输出保持 document_id/job_id/status/deduped。
	result := callIngestTool(t, ingestTestDeps(ingest, webSources), ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"file_name":"a.txt","content_base64":"aGVsbG8="}`, kbID))
	out := structuredOf(t, result)
	require.NotEmpty(t, out["document_id"])
	require.NotEmpty(t, out["job_id"])
	require.Equal(t, "pending", out["status"])
	require.Equal(t, false, out["deduped"])
	require.NotContains(t, out, "kb_source_id")
	require.Equal(t, 1, ingest.calls)
	require.Zero(t, webSources.calls)
	require.Equal(t, "a.txt", ingest.input.FileName)
}

func TestDocumentIngestFileModeRequiresFileFields(t *testing.T) {
	kbID := uuid.New()
	ingest := &fakeMCPIngestService{}
	webSources := &fakeMCPWebSourceService{}

	// url 缺省但 file 字段缺失：validation error（schema 不再 required，逐模式显式校验）。
	result := callIngestTool(t, ingestTestDeps(ingest, webSources), ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q}`, kbID))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "validation_error", parsed.Error.Code)
	require.Contains(t, parsed.Error.Message, "file_name")
	require.Zero(t, ingest.calls)
	require.Zero(t, webSources.calls)
}

func TestDocumentIngestURLModePassesServiceErrors(t *testing.T) {
	kbID := uuid.New()
	// 多个 urls 源的 service 错误原样透传为 validation error（含候选清单）。
	svcErr := fmt.Errorf("%w: 目标知识库有 2 个 mode=urls 的 active 网站源，请指定（候选：甲(id-甲)、乙(id-乙)）",
		domainerrors.ErrValidation)
	webSources := &fakeMCPWebSourceService{err: svcErr}
	result := callIngestTool(t, ingestTestDeps(&fakeMCPIngestService{}, webSources), ingestTestAuth(kbID),
		fmt.Sprintf(`{"knowledge_base_id":%q,"url":"https://example.com/x"}`, kbID))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "validation_error", parsed.Error.Code)
	require.Contains(t, parsed.Error.Message, "甲")
	require.Contains(t, parsed.Error.Message, "乙")
}

func TestDocumentIngestURLModeRequiresKBBinding(t *testing.T) {
	kbID := uuid.New()
	ingest := &fakeMCPIngestService{}
	webSources := &fakeMCPWebSourceService{}

	// KB 绑定校验在 url 分派之前，未绑定 KB 返回 not_found，两个端口都不触碰。
	auth := value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsWrite}, []uuid.UUID{uuid.New()})
	result := callIngestTool(t, ingestTestDeps(ingest, webSources), auth,
		fmt.Sprintf(`{"knowledge_base_id":%q,"url":"https://example.com/x"}`, kbID))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "not_found", parsed.Error.Code)
	require.Zero(t, ingest.calls)
	require.Zero(t, webSources.calls)
}

func TestDocumentIngestSchemaDeclaresOptionalURL(t *testing.T) {
	srv := NewServer(minimalDeps())
	resp := srv.MCP().HandleMessage(context.Background(), json.RawMessage(`{
		"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}
	}`))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok)
	result, ok := jsonResp.Result.(mcplib.ListToolsResult)
	require.True(t, ok)

	var ingest *mcplib.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "document_ingest" {
			ingest = &result.Tools[i]
			break
		}
	}
	require.NotNil(t, ingest)

	input := inputSchemaOf(t, *ingest)
	// url 模式不需要 file 字段：required 收敛为 knowledge_base_id。
	require.ElementsMatch(t, []string{"knowledge_base_id"}, input["required"])
	urlProp := property(input, "url")
	require.NotNil(t, urlProp, "缺少 url 属性")
	require.Equal(t, "string", urlProp["type"])

	// 输出 schema 覆盖 file 与 url 两种模式的结果字段（宽松定义）。
	output := outputSchemaOf(t, *ingest)
	for _, field := range []string{"document_id", "job_id", "status", "deduped",
		"kb_source_id", "kb_source_name", "url", "sync_queued"} {
		require.NotNil(t, property(output, field), "输出 schema 缺少 %s", field)
	}
}
