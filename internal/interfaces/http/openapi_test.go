package http

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/config"
)

// TestBuildSpecProducesValidJSON 验证生成的 spec 能被序列化为合法 JSON，
// 并且能用 openapi3.Loader 反向加载回来（结构合法性兜底）。
func TestBuildSpecProducesValidJSON(t *testing.T) {
	spec := buildSpec()
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("序列化 spec 失败: %v", err)
	}
	if !strings.Contains(string(data), "\"openapi\":\"3.0.3\"") {
		t.Errorf("spec 缺少 openapi 版本声明")
	}
	// 反向加载验证结构合法
	loader := openapi3.NewLoader()
	if _, err := loader.LoadFromData(data); err != nil {
		t.Fatalf("生成的 spec 无法被 openapi3 loader 解析: %v", err)
	}
}

// TestKeyRoutesPresent 验证几条代表性路由已被注册到 spec。
func TestKeyRoutesPresent(t *testing.T) {
	spec := buildSpec()
	cases := []struct {
		method  string
		path    string
		hasResp bool // 期望有成功响应
		hasReq  bool // 期望有请求体
	}{
		{http.MethodPost, "/api/v1/workspaces/{workspace_slug}/knowledge-bases", true, true},
		{http.MethodGet, "/api/v1/workspaces/{workspace_slug}/knowledge-bases", true, false},
		{http.MethodPost, "/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/documents", true, true},
		{http.MethodGet, "/api/v1/workspaces/{workspace_slug}/documents/{document_id}", true, false},
		{http.MethodDelete, "/api/v1/workspaces/{workspace_slug}/documents/{document_id}", false, false},
		{http.MethodPost, "/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/search", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+strings.ReplaceAll(tc.path, "/", "_"), func(t *testing.T) {
			pi := spec.Paths.Find(tc.path)
			if pi == nil {
				t.Fatalf("路径未注册: %s", tc.path)
			}
			op := pickOp(pi, tc.method)
			if op == nil {
				t.Fatalf("方法未注册: %s %s", tc.method, tc.path)
			}
			if tc.hasReq && op.RequestBody == nil {
				t.Errorf("期望有请求体，实际无")
			}
			if !tc.hasReq && op.RequestBody != nil {
				t.Errorf("期望无请求体，实际有")
			}
			if tc.hasResp && len(op.Responses.Map()) == 0 {
				t.Errorf("期望有响应，实际无")
			}
		})
	}
}

func TestSessionOnlyRoutesExcluded(t *testing.T) {
	spec := buildSpec()
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodPost, "/api/v1/workspaces"},
		{http.MethodGet, "/api/v1/workspaces/{workspace_slug}/members"},
		{http.MethodGet, "/api/v1/workspaces/{workspace_slug}/search-settings"},
		{http.MethodGet, "/api/v1/admin/model-providers"},
		{http.MethodGet, "/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/index-generations"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+strings.ReplaceAll(tc.path, "/", "_"), func(t *testing.T) {
			path := spec.Paths.Find(tc.path)
			if path == nil {
				return
			}
			if op := pickOp(path, tc.method); op != nil {
				t.Fatalf("Session-only 路由不应出现在 OpenAPI: %s %s", tc.method, tc.path)
			}
		})
	}
}

// TestPathParamSyntaxConverted 验证 Gin 的 :param 语法已转为 {param}。
func TestPathParamSyntaxConverted(t *testing.T) {
	spec := buildSpec()
	for _, key := range spec.Paths.Keys() {
		if strings.Contains(key, ":") {
			t.Errorf("路径 %q 仍含 Gin 风格 :param，未转为 {param}", key)
		}
	}
}

// TestUUIDFieldReflectsAsStringFormat 验证 uuid.UUID 字段被拦截为 string/uuid，
// 而不是反射成空 schema（kin-openapi 的已知坑）。
func TestUUIDFieldReflectsAsStringFormat(t *testing.T) {
	spec := buildSpec()
	pi := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}")
	if pi == nil || pi.Get == nil {
		t.Fatal("知识库查询路由未注册")
	}
	resp := pi.Get.Responses.Status(http.StatusOK)
	if resp == nil {
		t.Fatal("缺少 200 响应")
	}
	media := resp.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatal("缺少 JSON schema")
	}
	idProp := media.Schema.Value.Properties["id"]
	if idProp == nil || idProp.Value == nil {
		t.Fatal("knowledge-base schema 缺少 id 字段")
	}
	if idProp.Value.Type == nil || !idProp.Value.Type.Is("string") {
		t.Errorf("uuid 字段 id 类型应为 string，实际 %v", idProp.Value.Type)
	}
	if idProp.Value.Format != "uuid" {
		t.Errorf("uuid 字段 id format 应为 uuid，实际 %q", idProp.Value.Format)
	}
}

// TestEnumValuesPopulated 验证 value.* enum 字段反射后带枚举值。
func TestEnumValuesPopulated(t *testing.T) {
	spec := buildSpec()
	pi := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/models")
	if pi == nil || pi.Get == nil {
		t.Fatal("模型列表路由未注册")
	}
	resp := pi.Get.Responses.Status(http.StatusOK)
	if resp == nil {
		t.Fatal("缺少 200 响应")
	}
	media := resp.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		t.Fatal("缺少 JSON schema")
	}
	items := media.Schema.Value.Items
	if items == nil || items.Value == nil {
		t.Fatal("响应应为 array")
	}
	statusProp := items.Value.Properties["status"]
	if statusProp == nil || statusProp.Value == nil {
		t.Fatal("model schema 缺少 status 字段")
	}
	if len(statusProp.Value.Enum) == 0 {
		t.Errorf("status 字段应带 enum 枚举值，实际为空（type=%v）", statusProp.Value.Type)
	}
}

// TestRequiredComputed 验证非指针、无 omitempty 的字段进入 required。
func TestRequiredComputed(t *testing.T) {
	b := newSpecBuilder()
	ref, err := b.schemaRef(reflect.TypeOf(createKnowledgeBaseRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{}
	for _, r := range ref.Value.Required {
		required[r] = true
	}
	if !required["name"] {
		t.Errorf("name 应为 required，当前 required=%v", ref.Value.Required)
	}
}

// TestSecurityRequirements 验证鉴权方式映射。
func TestSecurityRequirements(t *testing.T) {
	spec := buildSpec()
	if spec.Paths.Find("/api/v1/auth/login") != nil || spec.Paths.Find("/api/v1/auth/me") != nil {
		t.Errorf("认证路由不应进入 API Key OpenAPI 文档")
	}
	kb := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/knowledge-bases").Post
	if kb.Security == nil || len(*kb.Security) != 2 {
		t.Errorf("bearer-or-session 路由应有 2 条 OR requirement，实际 %d", lenOfSec(kb.Security))
	} else {
		if _, ok := (*kb.Security)[0][secBearer]; !ok {
			t.Errorf("程序化路由的第一 security requirement 应为 BearerAuth，实际 %v", *kb.Security)
		}
		if _, ok := (*kb.Security)[1][secSessionCookie]; !ok {
			t.Errorf("程序化路由的第二 security requirement 应为 SessionCookie，实际 %v", *kb.Security)
		}
	}
}

func TestProgrammaticRoutesExposeScopesAndLineageParameters(t *testing.T) {
	spec := buildSpec()
	path := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/documents/{document_id}/faq")
	if path == nil || path.Get == nil {
		t.Fatal("带知识库 lineage 的 FAQ GET 未声明")
	}
	if _, old := spec.Paths.Map()["/api/v1/workspaces/{workspace_slug}/documents/{document_id}/faq"]; old {
		t.Fatal("旧 FAQ 路径不应出现在 OpenAPI")
	}
	if got := path.Get.Extensions["x-langhuan-required-scopes"]; got == nil {
		t.Fatal("FAQ GET 缺少 required scopes extension")
	} else {
		scopes, ok := got.([]value.APIScope)
		if !ok || len(scopes) != 1 || scopes[0] != value.ScopeDocumentsRead {
			t.Fatalf("FAQ GET scopes = %#v, want documents:read", got)
		}
	}
	if path.Get.Security == nil || len(*path.Get.Security) != 2 {
		t.Fatalf("FAQ GET security = %#v, want Session/Bearer OR", path.Get.Security)
	}
	seen := map[string]bool{}
	for _, p := range path.Get.Parameters {
		seen[p.Value.Name] = true
	}
	for _, name := range []string{"workspace_slug", "id", "document_id"} {
		if !seen[name] {
			t.Errorf("FAQ GET 缺少 path parameter %q", name)
		}
	}
	docs := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/documents")
	if docs == nil || docs.Get == nil {
		t.Fatal("文档列表未声明")
	}
	if len(docs.Get.Parameters) == 0 {
		t.Fatal("文档列表缺少 kind/query 或 path 参数")
	}
	var kindEnum []interface{}
	for _, parameter := range docs.Get.Parameters {
		if parameter.Value.Name == "kind" {
			kindEnum = parameter.Value.Schema.Value.Enum
		}
	}
	if len(kindEnum) != 3 {
		t.Fatalf("kind enum = %#v", kindEnum)
	}
}

func TestSearchRoutesExposeRequiredScope(t *testing.T) {
	spec := buildSpec()
	for _, path := range []string{
		"/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/search",
		"/api/v1/workspaces/{workspace_slug}/search",
	} {
		pi := spec.Paths.Find(path)
		if pi == nil || pi.Post == nil {
			t.Fatalf("检索路由未声明: %s", path)
		}
		got := pi.Post.Extensions["x-langhuan-required-scopes"]
		scopes, ok := got.([]value.APIScope)
		if !ok || len(scopes) != 1 || scopes[0] != value.ScopeSearchRead {
			t.Fatalf("%s scopes = %#v, want search:read", path, got)
		}
	}
}

func TestEveryBearerOrSessionOperationDeclaresRequiredScope(t *testing.T) {
	spec := buildSpec()
	for pathName, pathItem := range spec.Paths.Map() {
		for method, operation := range map[string]*openapi3.Operation{
			http.MethodGet: pathItem.Get, http.MethodPost: pathItem.Post, http.MethodPut: pathItem.Put,
			http.MethodPatch: pathItem.Patch, http.MethodDelete: pathItem.Delete,
		} {
			if operation == nil || operation.Security == nil || len(*operation.Security) != 2 {
				continue
			}
			if operation.Extensions["x-langhuan-required-scopes"] == nil {
				t.Errorf("%s %s 缺少 x-langhuan-required-scopes", method, pathName)
			}
		}
	}
}

func TestOpenAPIPublishesOnlyAPIKeyOperations(t *testing.T) {
	spec := buildSpec()
	operationCount := 0
	for pathName, pathItem := range spec.Paths.Map() {
		for method, operation := range map[string]*openapi3.Operation{
			http.MethodGet: pathItem.Get, http.MethodPost: pathItem.Post, http.MethodPut: pathItem.Put,
			http.MethodPatch: pathItem.Patch, http.MethodDelete: pathItem.Delete,
		} {
			if operation == nil {
				continue
			}
			operationCount++
			if operation.Security == nil || len(*operation.Security) != 2 {
				t.Errorf("%s %s 必须声明 Bearer 或 Session security", method, pathName)
				continue
			}
			if _, ok := (*operation.Security)[0][secBearer]; !ok {
				t.Errorf("%s %s 第一 security requirement 应为 BearerAuth", method, pathName)
			}
			if _, ok := (*operation.Security)[1][secSessionCookie]; !ok {
				t.Errorf("%s %s 第二 security requirement 应为 SessionCookie", method, pathName)
			}
			if operation.Extensions["x-langhuan-required-scopes"] == nil {
				t.Errorf("%s %s 缺少 required scopes extension", method, pathName)
			}
		}
	}
	if operationCount == 0 {
		t.Fatal("OpenAPI 至少应包含一条 API Key operation")
	}
}

// TestMultipartUploadRoute 验证文件上传路由标记为 multipart/form-data。
func TestMultipartUploadRoute(t *testing.T) {
	spec := buildSpec()
	pi := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/documents")
	if pi == nil || pi.Post == nil {
		t.Fatal("文档上传路由未注册")
	}
	if pi.Post.RequestBody == nil {
		t.Fatal("上传路由应有请求体")
	}
	if _, ok := pi.Post.RequestBody.Value.Content["multipart/form-data"]; !ok {
		t.Errorf("上传路由应为 multipart/form-data，实际 content-types: %v", contentKeys(pi.Post.RequestBody.Value.Content))
	}
}

// TestDocsEndpointsRegistered 验证 /openapi.json 与 /docs 已注册到 router。
func TestDocsEndpointsRegistered(t *testing.T) {
	deps := Dependencies{
		Auth:          &fakeAuthService{},
		SessionConfig: config.SessionConfig{CookieName: "session"},
	}
	router := NewRouter(deps)
	routes := router.Routes()
	want := map[string]bool{"/api/v1/openapi.json": false, "/api/v1/docs": false}
	for _, r := range routes {
		if r.Method == http.MethodGet {
			if _, ok := want[r.Path]; ok {
				want[r.Path] = true
			}
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("文档端点 %s 未注册", path)
		}
	}
}

// --- helpers ---

func pickOp(pi *openapi3.PathItem, method string) *openapi3.Operation {
	switch method {
	case http.MethodGet:
		return pi.Get
	case http.MethodPost:
		return pi.Post
	case http.MethodPut:
		return pi.Put
	case http.MethodPatch:
		return pi.Patch
	case http.MethodDelete:
		return pi.Delete
	}
	return nil
}

func lenOfSec(s *openapi3.SecurityRequirements) int {
	if s == nil {
		return 0
	}
	return len(*s)
}

func contentKeys(c openapi3.Content) []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
