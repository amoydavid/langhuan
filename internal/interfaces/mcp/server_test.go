package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewServerRegistersSixTools(t *testing.T) {
	srv := NewServer(minimalDeps())
	require.NotNil(t, srv)
	require.NotNil(t, srv.Handler())

	resp := srv.MCP().HandleMessage(context.Background(), json.RawMessage(`{
		"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}
	}`))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok, "response type = %T", resp)
	result, ok := jsonResp.Result.(mcplib.ListToolsResult)
	require.True(t, ok, "result type = %T", jsonResp.Result)
	names := toolNames(result.Tools)
	require.ElementsMatch(t, []string{
		"knowledge_base_create", "document_ingest", "document_status",
		"knowledge_search", "document_delete", "chunk_get",
	}, names)
}

func TestScopeToolFilterHidesToolsOutsideScope(t *testing.T) {
	all := []mcplib.Tool{
		{Name: "knowledge_base_create"},
		{Name: "document_ingest"},
		{Name: "document_status"},
		{Name: "knowledge_search"},
		{Name: "document_delete"},
		{Name: "chunk_get"},
	}
	// 只读 key：documents:read + search:read。
	auth := value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsRead, value.ScopeSearchRead},
		[]uuid.UUID{uuid.New()})
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	filtered := scopeToolFilter(ctx, all)
	require.ElementsMatch(t, []string{"document_status", "knowledge_search", "chunk_get"}, toolNames(filtered))
}

func TestScopeToolFilterReturnsAllForSession(t *testing.T) {
	all := []mcplib.Tool{{Name: "knowledge_base_create"}, {Name: "document_delete"}}
	auth := value.AuthContext{PrincipalKind: value.PrincipalUser}
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	filtered := scopeToolFilter(ctx, all)
	require.Len(t, filtered, 2)
}

func toolNames(tools []mcplib.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// inputSchemaOf 把 tool 的 inputSchema（RawInputSchema JSON）解析为 map。
func inputSchemaOf(t *testing.T, tool mcplib.Tool) map[string]any {
	t.Helper()
	schema := map[string]any{}
	if len(tool.RawInputSchema) > 0 {
		require.NoError(t, json.Unmarshal(tool.RawInputSchema, &schema))
		return schema
	}
	data, err := json.Marshal(tool.InputSchema)
	if err != nil {
		panic(err)
	}
	require.NoError(t, json.Unmarshal(data, &schema))
	return schema
}

// outputSchemaOf 把 tool 的 outputSchema（RawOutputSchema JSON）解析为 map。
func outputSchemaOf(t *testing.T, tool mcplib.Tool) map[string]any {
	t.Helper()
	schema := map[string]any{}
	if len(tool.RawOutputSchema) > 0 {
		require.NoError(t, json.Unmarshal(tool.RawOutputSchema, &schema))
		return schema
	}
	data, err := json.Marshal(tool.OutputSchema)
	if err != nil {
		panic(err)
	}
	require.NoError(t, json.Unmarshal(data, &schema))
	return schema
}

// property 返回 inputSchema.properties[name]，不存在则返回 nil。
func property(schema map[string]any, name string) map[string]any {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	p, _ := props[name].(map[string]any)
	return p
}

// typeValues 返回 schema 片段的 "type" 值列表。type 可能是单个 string
// （如 "string"）或联合类型数组（如 ["null","object"]，指针/可空字段会产生）。
func typeValues(prop map[string]any) []string {
	switch v := prop["type"].(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// assertPropertyType 断言属性 type 为期望类型（可能是 string 或联合类型数组，
// 如指针字段生成的 ["null","integer"]）。
func assertPropertyType(t *testing.T, schema map[string]any, name, want string) {
	t.Helper()
	p := property(schema, name)
	require.NotNil(t, p, "缺少属性 %s", name)
	switch typ := p["type"].(type) {
	case string:
		require.Equal(t, want, typ, "属性 %s 类型", name)
	case []any:
		require.Contains(t, typ, want, "属性 %s 联合类型", name)
	default:
		t.Fatalf("属性 %s 的 type 字段意外: %#v", name, p["type"])
	}
}

func TestToolInputSchemasMatchStructDeclarations(t *testing.T) {
	srv := NewServer(minimalDeps())
	resp := srv.MCP().HandleMessage(context.Background(), json.RawMessage(`{
		"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}
	}`))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok)
	result, ok := jsonResp.Result.(mcplib.ListToolsResult)
	require.True(t, ok)

	byName := map[string]mcplib.Tool{}
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}

	t.Run("knowledge_base_create", func(t *testing.T) {
		schema := inputSchemaOf(t, byName["knowledge_base_create"])
		require.ElementsMatch(t, []string{"name", "embedding_model_id"}, schema["required"])
		require.Equal(t, "知识库名称", property(schema, "name")["description"])
		assertPropertyType(t, schema, "chunk_size", "integer")
		assertPropertyType(t, schema, "chunk_overlap", "integer")
	})

	t.Run("document_ingest", func(t *testing.T) {
		schema := inputSchemaOf(t, byName["document_ingest"])
		require.ElementsMatch(t, []string{"knowledge_base_id", "file_name", "content_base64"}, schema["required"])
		assertPropertyType(t, schema, "dedupe", "boolean")
		require.Equal(t, "目标知识库 ID", property(schema, "knowledge_base_id")["description"])
	})

	t.Run("document_status", func(t *testing.T) {
		schema := inputSchemaOf(t, byName["document_status"])
		require.ElementsMatch(t, []string{"knowledge_base_id", "document_id"}, schema["required"])
		require.NotContains(t, schema["required"], "job_id")
	})

	t.Run("knowledge_search", func(t *testing.T) {
		schema := inputSchemaOf(t, byName["knowledge_search"])
		require.ElementsMatch(t, []string{"query"}, schema["required"])
		assertPropertyType(t, schema, "knowledge_base_ids", "array")
		assertPropertyType(t, schema, "vector_top_k", "integer")
		require.Equal(t, "string", property(schema, "knowledge_base_ids")["items"].(map[string]any)["type"])
	})

	t.Run("document_delete", func(t *testing.T) {
		schema := inputSchemaOf(t, byName["document_delete"])
		require.ElementsMatch(t, []string{"knowledge_base_id", "document_id"}, schema["required"])
	})

	t.Run("chunk_get", func(t *testing.T) {
		schema := inputSchemaOf(t, byName["chunk_get"])
		require.ElementsMatch(t, []string{"knowledge_base_id", "chunk_id"}, schema["required"])
	})
}

// TestKnowledgeSearchOutputSchemaUUIDsAreString 是 uuid.UUID→string schema 映射的回归测试。
//
// uuid.UUID 底层是 [16]byte，jsonschema-go 默认把它推导成 "type":"array"，但实际
// MarshalJSON 输出字符串，导致 MCP output schema 校验失败。rawOutputSchema 通过
// ForOptions.TypeSchemas 把 uuid.UUID 全局映射为 string。这里锁定：knowledge_search
// 输出 DTO（内嵌 dto.SearchResult）里的所有 uuid.UUID 字段必须声明为 string。
func TestKnowledgeSearchOutputSchemaUUIDsAreString(t *testing.T) {
	srv := NewServer(minimalDeps())
	resp := srv.MCP().HandleMessage(context.Background(), json.RawMessage(`{
		"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}
	}`))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok)
	result, ok := jsonResp.Result.(mcplib.ListToolsResult)
	require.True(t, ok)

	byName := map[string]mcplib.Tool{}
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}

	schema := outputSchemaOf(t, byName["knowledge_search"])

	// results 是数组（指针切片会被推导为 ["null","array"] 联合类型），其 items 是单个检索结果对象。
	resultsProp := property(schema, "results")
	require.NotNil(t, resultsProp, "缺少 results 属性")
	require.Contains(t, typeValues(resultsProp), "array", "results 应为 array")

	items, ok := resultsProp["items"].(map[string]any)
	require.True(t, ok, "results.items 不是 object")
	// results 元素是指针（[]*dto.SearchResult），items 会被推导为 ["null","object"]。
	require.Contains(t, typeValues(items), "object", "results.items 应为 object")

	itemProps, ok := items["properties"].(map[string]any)
	require.True(t, ok, "results.items.properties 不是 object")

	// 这四个字段在 dto.SearchResult 里是 uuid.UUID；修复前会被误判为 array。
	for _, field := range []string{"chunk_id", "chunk_revision_id", "document_id", "knowledge_base_id"} {
		prop, ok := itemProps[field].(map[string]any)
		require.True(t, ok, "缺少 %s 属性", field)
		// 不能是 array（修复前的 bug 表现），也不能带 minItems/maxItems。
		require.NotContains(t, typeValues(prop), "array", "%s 被误判为 array", field)
		require.Nil(t, prop["minItems"], "%s 残留 minItems", field)
		require.Nil(t, prop["maxItems"], "%s 残留 maxItems", field)
		require.Contains(t, typeValues(prop), "string", "%s 应为 string", field)
	}
}

// minimalDeps 返回带 nil 服务适配器的 Dependencies；用于验证工具注册。
func minimalDeps() Dependencies {
	return Dependencies{InlineLimit: 8 * 1024 * 1024}
}

// 占位：dto 被部分 adapter 引用，这里保留 import 以备扩展测试。
var _ = dto.Document{}
