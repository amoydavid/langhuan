package http

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

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
		{http.MethodPost, "/api/v1/auth/login", true, true},
		{http.MethodPost, "/api/v1/workspaces", true, true},
		{http.MethodPost, "/api/v1/workspaces/{workspace_slug}/knowledge-bases", true, true},
		{http.MethodGet, "/api/v1/workspaces/{workspace_slug}/knowledge-bases", true, false},
		{http.MethodDelete, "/api/v1/workspaces/{workspace_slug}/documents/{document_id}", false, false},
		{http.MethodPost, "/api/v1/workspaces/{workspace_slug}/knowledge-bases/{id}/documents", true, true},
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
	pi := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/members")
	if pi == nil || pi.Get == nil {
		t.Fatal("成员列表路由未注册")
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
	roleProp := items.Value.Properties["role"]
	if roleProp == nil || roleProp.Value == nil {
		t.Fatal("membership schema 缺少 role 字段")
	}
	if len(roleProp.Value.Enum) == 0 {
		t.Errorf("role 字段应带 enum 枚举值，实际为空（type=%v）", roleProp.Value.Type)
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
	login := spec.Paths.Find("/api/v1/auth/login").Post
	if login.Security != nil {
		t.Errorf("public 路由不应有 security，实际 %v", login.Security)
	}
	me := spec.Paths.Find("/api/v1/auth/me").Get
	if me.Security == nil || len(*me.Security) == 0 {
		t.Errorf("session 路由应有 security requirement")
	}
	kb := spec.Paths.Find("/api/v1/workspaces/{workspace_slug}/knowledge-bases").Post
	if kb.Security == nil || len(*kb.Security) != 2 {
		t.Errorf("bearer-or-session 路由应有 2 条 OR requirement，实际 %d", lenOfSec(kb.Security))
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
