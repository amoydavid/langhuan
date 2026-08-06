package http

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3gen"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/value"
)

// 文档：本包其余代码（handler/router/middleware）负责运行时行为；
// openapi_spec.go + openapi_routes.go + openapi_ui.go 只负责"文档生成"。
//
// 设计原则：Go 类型是 schema 的唯一来源。本文件只配置反射规则（uuid 拦截、
// required 计算、enum 值表），不逐字段手写 schema；openapi_routes.go 只声明
// "路由→类型"的绑定关系。新增/修改 struct 字段时文档自动更新，零漂移。

const (
	// securityScheme 常量：在 SecuritySchemes 里注册的 key。
	secSessionCookie = "SessionCookie" // 浏览器登录态，cookie 传递
	secBearer        = "BearerAuth"    // 程序化 API Key，Authorization: Bearer
)

// opSec 标记一条路由的鉴权方式，用于生成 operation 的 security 字段。
type opSec int

const (
	secPublic           opSec = iota // 无鉴权
	secSession                       // 仅 Session cookie
	secSessionAdmin                  // Session + 平台管理员
	secSessionMember                 // Session + workspace member+
	secSessionAdminRole              // Session + workspace admin+
	secSessionOwner                  // Session + workspace owner
	secBearerOrSession               // Bearer API Key 或 Session（程序化入口）
)

// specBuilder 聚集 spec 组装过程中的共享状态。
type specBuilder struct {
	doc     *openapi3.T
	gen     *openapi3gen.Generator
	schemas openapi3.Schemas // 复用 schema，避免同一 struct 反射多次
}

// newSpecBuilder 创建构建器并装配 Info / SecuritySchemes / 通用错误响应。
func newSpecBuilder() *specBuilder {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "琅嬛 API",
			Description: "琅嬛知识转化与检索服务。所有资源归属于 workspace；检索与导入通过 REST 与 MCP over HTTP 提供。",
			Version:     "0.x",
		},
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			SecuritySchemes: openapi3.SecuritySchemes{
				secSessionCookie: &openapi3.SecuritySchemeRef{
					Value: &openapi3.SecurityScheme{
						Type: "apiKey", In: "cookie", Name: "session",
						Description: "浏览器登录态，由 /api/v1/auth/login 写入的 session cookie。",
					},
				},
				secBearer: &openapi3.SecuritySchemeRef{
					Value: &openapi3.SecurityScheme{
						Type:         "http",
						Scheme:       "bearer",
						BearerFormat: "API Key",
						Description:  "程序化 API Key，Authorization: Bearer <plaintext>。",
					},
				},
			},
		},
	}
	return &specBuilder{
		doc:     doc,
		schemas: openapi3.Schemas{},
		gen:     newGenerator(),
	}
}

// newGenerator 配置反射器：反射全部导出字段 + uuid 拦截 + enum 补值。
// 不用 CreateComponentSchemas（会让 schema 以 $ref 形式分散到 components，
// 且 openapi3gen 对无 Properties 的自定义类型收集不全，导致 ref 无法解析）。
// 改为全 inline + ThrowErrorOnCycle：真实 dto 无循环引用，故不会触发 cycle error，
// 也不会产生任何无根 $ref，spec 完全自包含。代价是同一类型在多路由重复 inline，
// 对 81 路由的文档 UI 体积完全可接受。
func newGenerator() *openapi3gen.Generator {
	return openapi3gen.NewGenerator(
		openapi3gen.UseAllExportedFields(),
		openapi3gen.SchemaCustomizer(schemaCustomizer),
	)
}

// schemaCustomizer 是逐字段的反射钩子，处理 openapi3gen 默认行为无法覆盖的三类情况：
//   - uuid.UUID 默认反射得空 schema（底层 [16]byte 被当 struct），强制改为 string/uuid；
//   - value.* 的 string-enum 默认只得到 type:string，补 enum 枚举值；
//   - json.RawMessage / optionalRawMessage 等自由 JSON，显式标记为 object（不加约束）。
func schemaCustomizer(name string, t reflect.Type, tag reflect.StructTag, schema *openapi3.Schema) error {
	switch t {
	case reflect.TypeFor[uuid.UUID]():
		schema.Type = &openapi3.Types{"string"}
		schema.Format = "uuid"
	case reflect.TypeFor[json.RawMessage](), reflect.TypeFor[optionalRawMessage]():
		// 自由 JSON：不约束内部结构，标记为 object（允许任意字段）。
		schema.Type = &openapi3.Types{"object"}
		t := true
		schema.AdditionalProperties = openapi3.AdditionalProperties{Has: &t}
	case reflect.TypeFor[optionalInt]():
		// 三态可选整数：底层是 int，自定义 UnmarshalJSON 支持缺省。
		schema.Type = &openapi3.Types{"integer"}
	case reflect.TypeFor[value.WorkspaceRole]():
		applyStringEnum(schema, value.RoleMember, value.RoleAdmin, value.RoleOwner)
	case reflect.TypeFor[value.APIScope]():
		applyStringEnum(schema,
			value.ScopeKnowledgeBasesRead, value.ScopeKnowledgeBasesWrite, value.ScopeDocumentsRead,
			value.ScopeDocumentsWrite, value.ScopeSearchRead)
	case reflect.TypeFor[value.ChunkRole]():
		applyStringEnum(schema, value.ChunkRoleParent, value.ChunkRoleChild, value.ChunkRoleFlat)
	case reflect.TypeFor[value.ChunkingStrategy]():
		applyStringEnum(schema,
			value.ChunkingStrategyAuto, value.ChunkingStrategyHeading,
			value.ChunkingStrategyHeuristic, value.ChunkingStrategyRecursive)
	case reflect.TypeFor[value.ModelType]():
		applyStringEnum(schema, value.ModelTypeEmbedding, value.ModelTypeLLM, value.ModelTypeRerank)
	case reflect.TypeFor[value.ModelStatus]():
		applyStringEnum(schema, value.ModelStatusActive, value.ModelStatusDisabled)
	case reflect.TypeFor[value.ModelScope]():
		applyStringEnum(schema, value.ModelScopePlatform, value.ModelScopeWorkspace)
	case reflect.TypeFor[value.JobStatus]():
		applyStringEnum(schema,
			value.JobStatusPending, value.JobStatusQueued, value.JobStatusRunning,
			value.JobStatusCompleted, value.JobStatusSucceeded, value.JobStatusFailed,
			value.JobStatusCancelled)
	case reflect.TypeFor[value.DocumentStatus]():
		applyStringEnum(schema,
			value.DocumentStatusPending, value.DocumentStatusProcessing, value.DocumentStatusReady,
			value.DocumentStatusParsingSubmitted, value.DocumentStatusParsing, value.DocumentStatusParsed,
			value.DocumentStatusIndexing, value.DocumentStatusCompleted, value.DocumentStatusFailed,
			value.DocumentStatusDeleting, value.DocumentStatusDeleted)
	case reflect.TypeFor[value.DocumentKind]():
		applyStringEnum(schema, value.DocumentKindFile, value.DocumentKindFAQ, value.DocumentKindWeb)
	case reflect.TypeFor[value.DocumentRevisionStatus]():
		applyStringEnum(schema,
			value.DocumentRevisionPending, value.DocumentRevisionParsing,
			value.DocumentRevisionReady, value.DocumentRevisionFailed)
	case reflect.TypeFor[value.ChunkRevisionStatus]():
		applyStringEnum(schema,
			value.ChunkRevisionPending, value.ChunkRevisionIndexing,
			value.ChunkRevisionReady, value.ChunkRevisionFailed)
	case reflect.TypeFor[value.ChunkEditSource]():
		applyStringEnum(schema, value.ChunkEditSourceSystem, value.ChunkEditSourceUser)
	case reflect.TypeFor[value.FileTreeNodeType]():
		applyStringEnum(schema, value.FileTreeNodeRoot, value.FileTreeNodeFolder, value.FileTreeNodeFile)
	case reflect.TypeFor[value.IndexGenerationStatus]():
		applyStringEnum(schema,
			value.IndexGenerationBuilding, value.IndexGenerationReady,
			value.IndexGenerationStale, value.IndexGenerationFailed, value.IndexGenerationRetired)
	case reflect.TypeFor[value.ManualEditDisposition]():
		applyStringEnum(schema,
			value.ManualEditNotApplicable, value.ManualEditPending, value.ManualEditArchiveConfirmed)
	case reflect.TypeFor[value.APIKeyStatus]():
		applyStringEnum(schema,
			value.APIKeyStatusActive, value.APIKeyStatusExpiring,
			value.APIKeyStatusExpired, value.APIKeyStatusRevoked)
	case reflect.TypeFor[value.RerankFailureMode]():
		applyStringEnum(schema, value.RerankFailureFallback, value.RerankFailureFail)
	case reflect.TypeFor[value.RankingStage]():
		applyStringEnum(schema, value.RankingStageRRF, value.RankingStageRerank, value.RankingStageRRFFallback)
	}
	return nil
}

// applyStringEnum 把一组字符串常量写进 schema 的 enum，并确保 type=string。
// 入参用 any 以兼容具名字符串类型（value.WorkspaceRole 等），运行时断言为 string。
func applyStringEnum(schema *openapi3.Schema, vals ...any) {
	schema.Type = &openapi3.Types{"string"}
	enum := make([]any, len(vals))
	for i, v := range vals {
		enum[i] = v
	}
	schema.Enum = enum
}

// schemaRef 反射一个 Go 类型得到 *openapi3.SchemaRef，并补 required 数组。
// openapi3gen 不计算 required，这里按约定补：字段为非指针且 json tag 不含 omitempty。
//
// 反射结果有两类 ref 需要处理：
//  1. openapi3gen 对每个命名 struct 默认填入 Ref=t.Name()（如 "ModelProvider"），
//     SchemaRef.MarshalJSON 在 Ref 非空时只输出 {"$ref":name} 并丢弃 Value → 无根引用。
//     处理：清除这类"非 #/ 开头"的 Ref，让 schema 以 Value inline 输出。
//  2. 真实自引用类型（如 FileTreeNode.Children []*FileTreeNode）必须用 component ref 打破
//     循环，generator 产出 {Ref:"#/components/schemas/Name", Value:schema}。
//     处理：把这类标准 component ref 对应的 schema 收集到 b.schemas，最后挂到
//     doc.Components.Schemas，ref 本身保留。
func (b *specBuilder) schemaRef(t reflect.Type) (*openapi3.SchemaRef, error) {
	ref, err := b.gen.GenerateSchemaRef(t)
	if err != nil {
		return nil, fmt.Errorf("反射 %s 失败: %w", t.String(), err)
	}
	visited := map[*openapi3.Schema]struct{}{}
	b.normalizeRefs(ref, visited)
	// 仅对 struct 计算 required（slice/map/array 无 required 概念）。
	if ref.Value != nil && t.Kind() == reflect.Struct {
		fillRequired(ref.Value, t)
	}
	return ref, nil
}

// normalizeRefs 递归规整反射产生的 SchemaRef：
//   - 非标准 ref（如 "ModelProvider"）：清空 Ref，inline Value；
//   - 标准 component ref（#/components/schemas/Name）：收集 Value 到 b.schemas，保留 ref。
func (b *specBuilder) normalizeRefs(ref *openapi3.SchemaRef, visited map[*openapi3.Schema]struct{}) {
	if ref == nil {
		return
	}
	if strings.HasPrefix(ref.Ref, "#/components/schemas/") {
		// 真实 cycle 打破点：把 component 定义收集起来。
		name := strings.TrimPrefix(ref.Ref, "#/components/schemas/")
		if ref.Value != nil {
			if _, ok := b.schemas[name]; !ok {
				b.schemas[name] = &openapi3.SchemaRef{Value: ref.Value}
			}
		}
	} else if ref.Ref != "" {
		// 无根命名 ref：清除，inline。
		ref.Ref = ""
	}
	if ref.Value == nil {
		return
	}
	if _, ok := visited[ref.Value]; ok {
		return
	}
	visited[ref.Value] = struct{}{}
	s := ref.Value
	for _, p := range s.Properties {
		b.normalizeRefs(p, visited)
	}
	if s.Items != nil {
		b.normalizeRefs(s.Items, visited)
	}
	if s.AdditionalProperties.Schema != nil {
		b.normalizeRefs(s.AdditionalProperties.Schema, visited)
	}
	for _, one := range s.OneOf {
		b.normalizeRefs(one, visited)
	}
	for _, ao := range s.AllOf {
		b.normalizeRefs(ao, visited)
	}
}

// fillRequired 遍历 struct 的直接字段，把"非指针且无 omitempty"的字段名加入 required。
func fillRequired(schema *openapi3.Schema, t reflect.Type) {
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		jsonTag := f.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		name, opts := parseJSONTag(jsonTag)
		if name == "" {
			name = f.Name
		}
		if opts["omitempty"] {
			continue
		}
		// 指针、slice、map 默认可选（零值即缺省）；仅非指针的值类型判为必填。
		if f.Type.Kind() == reflect.Pointer || f.Type.Kind() == reflect.Slice ||
			f.Type.Kind() == reflect.Map || f.Type.Kind() == reflect.Interface {
			continue
		}
		required = append(required, name)
	}
	if len(required) > 0 {
		schema.Required = required
	}
}

// parseJSONTag 解析 `json:"name,omitempty"` 形式 tag，返回 name 与 option 集合。
func parseJSONTag(tag string) (string, map[string]bool) {
	parts := strings.Split(tag, ",")
	name := parts[0]
	opts := make(map[string]bool)
	for _, p := range parts[1:] {
		opts[p] = true
	}
	return name, opts
}

// buildSpec 组装完整 OpenAPI spec：创建构建器 → 注册全部路由 → 挂 cycle 打破所需的 component schemas。
func buildSpec() *openapi3.T {
	b := newSpecBuilder()
	b.registerRoutes()
	// normalizeRefs 期间收集的自引用类型 component schemas（如 FileTreeNode）挂到 doc，
	// 让 cycle 产生的 $ref 能被解析。
	if len(b.schemas) > 0 {
		if b.doc.Components == nil {
			b.doc.Components = &openapi3.Components{}
		}
		b.doc.Components.Schemas = b.schemas
	}
	return b.doc
}
