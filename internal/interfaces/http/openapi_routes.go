package http

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件是 OpenAPI 文档唯一的"手写"部分：声明每条对外 REST 路由的绑定关系
// （method/path/请求体/响应体/状态码/鉴权方式/tag）。字段级 schema 一律不写，
// 全部由 openapi_spec.go 的反射器从 struct 自动生成。
//
// 维护规则：
//   - 新增 REST 路由 → 在对应资源函数里加一行 op(...)；
//   - struct 加/删/改字段 → 什么都不用做（反射自动）。

// op 描述一条路由的文档元信息。只声明绑定，不含字段细节。
type op struct {
	method         string // HTTP 方法
	path           string // Gin 风格路径（:param），op 注册时转换为 {param}
	tag            string // OpenAPI tag，用于 UI 分组
	summary        string // 一句话描述
	reqBody        any    // 请求体类型示例值（reflect.TypeOf 用）；nil 表示无请求体
	reqMultipart   bool   // 是否 multipart/form-data（文件上传）
	respBody       any    // 响应体类型示例值；nil 表示无响应体（204）
	respType       string // 响应 Content-Type，默认 application/json；二进制用 application/octet-stream
	status         int    // 成功响应状态码
	sec            opSec  // 鉴权方式
	requiredScopes []value.APIScope
	params         []openapiParam
	description    string
}

type openapiParam struct {
	name, in, description string
	required              bool
	typeName              string
	format                string
	enum                  []string
	defaultValue          any
}

// registerRoutes 把全部 REST 路由注册进 spec。按资源组织，便于定位。
func (b *specBuilder) registerRoutes() {
	for _, o := range b.allOps() {
		b.add(o)
	}
}

// allOps 汇总全部对外 REST 路由。MCP、healthz、SPA 不纳入文档。
func (b *specBuilder) allOps() []op {
	var ops []op
	ops = append(ops, b.authOps()...)
	ops = append(ops, b.workspaceOps()...)
	ops = append(ops, b.userAndInvitationOps()...)
	ops = append(ops, b.knowledgeBaseOps()...)
	ops = append(ops, b.documentOps()...)
	ops = append(ops, b.documentAssetOps()...)
	ops = append(ops, b.faqOps()...)
	ops = append(ops, b.fileTreeOps()...)
	ops = append(ops, b.chunkOps()...)
	ops = append(ops, b.indexGenerationOps()...)
	ops = append(ops, b.searchOps()...)
	ops = append(ops, b.jobOps()...)
	ops = append(ops, b.modelProviderOps()...)
	ops = append(ops, b.modelOps()...)
	ops = append(ops, b.apiKeyOps()...)
	ops = append(ops, b.membershipOps()...)
	ops = append(ops, b.readinessAndSettingsOps()...)
	return ops
}

// add 把一条 op 注册进 spec：反射请求/响应 struct、转换路径参数语法、挂鉴权。
func (b *specBuilder) add(o op) {
	operation := &openapi3.Operation{
		Tags:      []string{o.tag},
		Summary:   o.summary,
		Responses: openapi3.NewResponses(),
	}
	if o.description != "" {
		operation.Description = o.description
	}
	if len(o.requiredScopes) > 0 {
		operation.Extensions = map[string]interface{}{"x-langhuan-required-scopes": o.requiredScopes}
	}
	params := append([]openapiParam(nil), o.params...)
	for _, segment := range strings.Split(o.path, "/") {
		if strings.HasPrefix(segment, ":") {
			name := strings.TrimPrefix(segment, ":")
			found := false
			for _, existing := range params {
				if existing.name == name && existing.in == "path" {
					found = true
					break
				}
			}
			if !found {
				format := "uuid"
				if name == "workspace_slug" {
					format = ""
				}
				params = append(params, openapiParam{name: name, in: "path", typeName: "string", format: format, required: true})
			}
		}
	}
	for _, p := range params {
		param := &openapi3.Parameter{Name: p.name, In: p.in, Description: p.description, Required: p.required, Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{p.typeName}, Format: p.format}}}
		if p.defaultValue != nil {
			param.Schema.Value.Default = p.defaultValue
		}
		if len(p.enum) > 0 {
			param.Schema.Value.Enum = make([]interface{}, len(p.enum))
			for i := range p.enum {
				param.Schema.Value.Enum[i] = p.enum[i]
			}
		}
		operation.Parameters = append(operation.Parameters, &openapi3.ParameterRef{Value: param})
	}
	// 请求体
	if o.reqBody != nil {
		mediaType := "application/json"
		if o.reqMultipart {
			mediaType = "multipart/form-data"
		}
		ref, err := b.schemaRef(reflect.TypeOf(o.reqBody))
		if err != nil {
			panic(err) // 启动期错误，struct 反射失败应立即暴露
		}
		operation.RequestBody = &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: true,
				Content: openapi3.Content{
					mediaType: &openapi3.MediaType{Schema: ref},
				},
			},
		}
	}
	// 成功响应
	if o.respBody != nil || o.status != 0 {
		status := o.status
		if status == 0 {
			status = http.StatusOK
		}
		resp := &openapi3.Response{}
		if o.respBody != nil {
			respType := o.respType
			if respType == "" {
				respType = "application/json"
			}
			ref, err := b.schemaRef(reflect.TypeOf(o.respBody))
			if err != nil {
				panic(err)
			}
			resp.Content = openapi3.Content{
				respType: &openapi3.MediaType{Schema: ref},
			}
		}
		operation.Responses.Set(strconv.Itoa(status), &openapi3.ResponseRef{Value: resp})
	}
	// 通用错误响应（除 public 路由外，鉴权类路由都可能返回 401/403/404/500）
	b.attachCommonErrors(operation, o.sec)
	// 鉴权
	operation.Security = b.securityFor(o.sec)

	path := ginPathToOpenAPI(o.path)
	pathItem := b.doc.Paths.Find(path)
	if pathItem == nil {
		pathItem = &openapi3.PathItem{}
		b.doc.Paths.Set(path, pathItem)
	}
	switch o.method {
	case http.MethodGet:
		pathItem.Get = operation
	case http.MethodPost:
		pathItem.Post = operation
	case http.MethodPut:
		pathItem.Put = operation
	case http.MethodPatch:
		pathItem.Patch = operation
	case http.MethodDelete:
		pathItem.Delete = operation
	}
}

// attachCommonErrors 为 operation 挂上与鉴权方式匹配的通用错误响应占位。
// 复用 errorBody（errors.go 里的 {"error":{"code","message"}}）。
func (b *specBuilder) attachCommonErrors(operation *openapi3.Operation, sec opSec) {
	errCodes := map[int]string{}
	errCodes[400] = "请求参数无效"
	if sec != secPublic {
		errCodes[401] = "未认证或会话过期"
		errCodes[403] = "无权限"
	}
	errCodes[404] = "资源不存在"
	errCodes[409] = "资源冲突"
	errCodes[500] = "服务器内部错误"
	// 仅当成功响应不是这些状态码时才挂，避免覆盖业务响应
	for code, desc := range errCodes {
		if operation.Responses.Status(code) != nil {
			continue
		}
		ref, err := b.schemaRef(reflect.TypeOf(errorBody{}))
		if err != nil {
			continue
		}
		descCopy := desc
		operation.Responses.Set(strconv.Itoa(code), &openapi3.ResponseRef{
			Value: &openapi3.Response{
				Description: &descCopy,
				Content: openapi3.Content{
					"application/json": &openapi3.MediaType{Schema: ref},
				},
			},
		})
	}
}

// securityFor 把 opSec 映射为 operation 的 security 要求。
func (b *specBuilder) securityFor(sec opSec) *openapi3.SecurityRequirements {
	switch sec {
	case secPublic:
		return nil
	case secSession:
		return openapi3.NewSecurityRequirements().With(openapi3.SecurityRequirement{secSessionCookie: []string{}})
	case secSessionAdmin, secSessionMember, secSessionAdminRole, secSessionOwner:
		return openapi3.NewSecurityRequirements().With(openapi3.SecurityRequirement{secSessionCookie: []string{}})
	case secBearerOrSession:
		// 任一凭证即可：两个独立 requirement（OR 语义）。
		reqs := openapi3.NewSecurityRequirements()
		reqs.With(openapi3.SecurityRequirement{secSessionCookie: []string{}})
		reqs.With(openapi3.SecurityRequirement{secBearer: []string{}})
		return reqs
	}
	return nil
}

// ginPathToOpenAPI 把 Gin 路径参数语法 :param 转换为 OpenAPI 的 {param}。
func ginPathToOpenAPI(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// =========================================================================
// 资源路由定义（每条路由一行 op）。路径与方法对齐 router.go，便于核对。
// =========================================================================

const wsBase = "/api/v1/workspaces/:workspace_slug"

func (b *specBuilder) authOps() []op {
	return []op{
		{method: http.MethodPost, path: "/api/v1/auth/login", tag: "认证", summary: "登录并写入 session cookie",
			reqBody: loginRequest{}, respBody: loginResponse{}, status: http.StatusOK, sec: secPublic},
		{method: http.MethodPost, path: "/api/v1/auth/register", tag: "认证", summary: "注册首位用户或接受邀请",
			reqBody: registerRequest{}, respBody: dto.AuthenticatedUser{}, status: http.StatusCreated, sec: secPublic},
		{method: http.MethodGet, path: "/api/v1/auth/bootstrap-status", tag: "认证", summary: "查询首位用户是否已初始化",
			respBody: bootstrapStatusResponse{}, status: http.StatusOK, sec: secPublic},
		{method: http.MethodGet, path: "/api/v1/invitations/:token", tag: "邀请", summary: "按 token 查询公开邀请信息",
			respBody: dto.PublicInvitation{}, status: http.StatusOK, sec: secPublic},
		{method: http.MethodPost, path: "/api/v1/auth/logout", tag: "认证", summary: "登出并清除 session cookie",
			respBody: nil, status: http.StatusNoContent, sec: secSession},
		{method: http.MethodGet, path: "/api/v1/auth/me", tag: "认证", summary: "查询当前用户与所属 workspace",
			respBody: meResponse{}, status: http.StatusOK, sec: secSession},
	}
}

func (b *specBuilder) workspaceOps() []op {
	return []op{
		{method: http.MethodPost, path: "/api/v1/workspaces", tag: "Workspace", summary: "创建 workspace（平台管理员）",
			reqBody: createWorkspaceRequest{}, respBody: dto.Workspace{}, status: http.StatusCreated, sec: secSessionAdmin},
		{method: http.MethodGet, path: wsBase, tag: "Workspace", summary: "查询当前 workspace",
			respBody: dto.Workspace{}, status: http.StatusOK, sec: secSessionMember},
	}
}

func (b *specBuilder) userAndInvitationOps() []op {
	return []op{
		{method: http.MethodPost, path: "/api/v1/admin/users/:user_id/password-reset", tag: "用户", summary: "平台管理员重置用户密码",
			reqBody: passwordResetRequest{}, respBody: nil, status: http.StatusNoContent, sec: secSessionAdmin},
		{method: http.MethodDelete, path: "/api/v1/invitations/:invitation_id", tag: "邀请", summary: "平台管理员吊销任意邀请",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdmin},
		{method: http.MethodGet, path: wsBase + "/invitations", tag: "邀请", summary: "列出 workspace 邀请",
			respBody: []*dto.InvitationListItem{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodPost, path: wsBase + "/invitations", tag: "邀请", summary: "创建 workspace 邀请",
			reqBody: createInvitationRequest{}, respBody: createInvitationResponse{}, status: http.StatusCreated, sec: secSessionAdminRole},
		{method: http.MethodDelete, path: wsBase + "/invitations/:invitation_id", tag: "邀请", summary: "吊销 workspace 邀请",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdminRole},
	}
}

func (b *specBuilder) knowledgeBaseOps() []op {
	return []op{
		{method: http.MethodPost, path: wsBase + "/knowledge-bases", tag: "知识库", summary: "创建知识库",
			reqBody: createKnowledgeBaseRequest{}, respBody: dto.KnowledgeBase{}, status: http.StatusCreated, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeKnowledgeBasesWrite}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases", tag: "知识库", summary: "列出知识库",
			respBody: []*dto.KnowledgeBase{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeKnowledgeBasesRead}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id", tag: "知识库", summary: "查询知识库",
			respBody: dto.KnowledgeBase{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeKnowledgeBasesRead}},
		{method: http.MethodPatch, path: wsBase + "/knowledge-bases/:id", tag: "知识库", summary: "更新知识库名称与描述",
			reqBody: updateKnowledgeBaseBasicsRequest{}, respBody: dto.KnowledgeBase{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeKnowledgeBasesWrite}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/summary", tag: "知识库", summary: "查询知识库汇总",
			respBody: dto.KnowledgeBaseSummary{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeKnowledgeBasesRead}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/jobs", tag: "知识库", summary: "查询知识库任务列表",
			respBody: dto.JobSummaryPage{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsRead}, params: []openapiParam{{name: "document_id", in: "query", typeName: "string", format: "uuid"}, {name: "status", in: "query", typeName: "string", enum: []string{"pending", "queued", "running", "completed", "succeeded", "failed", "cancelled"}}, {name: "cursor", in: "query", typeName: "string"}, {name: "limit", in: "query", typeName: "integer", defaultValue: 20}}},
	}
}

func (b *specBuilder) documentOps() []op {
	return []op{
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/documents", tag: "文档", summary: "上传并导入文档（multipart）",
			reqMultipart: true, reqBody: documentIngestForm{}, respBody: service.IngestDocumentResult{}, status: http.StatusCreated, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}},
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/documents/text", tag: "文档", summary: "导入 Markdown 文本",
			reqBody: ingestTextDocumentRequest{}, respBody: service.IngestDocumentResult{}, status: http.StatusCreated, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}, description: "Session 或 Bearer API Key 均可调用；content_type 必须为 markdown。"},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/documents", tag: "文档", summary: "列出知识库文档",
			respBody: []*dto.Document{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsRead}, params: []openapiParam{{name: "kind", in: "query", typeName: "string", enum: []string{"file", "faq", "web"}}}},
		{method: http.MethodGet, path: wsBase + "/documents/:document_id", tag: "文档", summary: "查询文档状态",
			respBody: dto.Document{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodDelete, path: wsBase + "/documents/:document_id", tag: "文档", summary: "删除文档",
			respBody: nil, status: http.StatusNoContent, sec: secSessionMember},
	}
}

func (b *specBuilder) documentAssetOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/documents/:document_id/assets", tag: "文档资产", summary: "列出文档资产",
			respBody: []dto.DocumentAsset{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodGet, path: wsBase + "/documents/:document_id/assets/:asset_id", tag: "文档资产", summary: "下载文档资产（二进制流）",
			respBody: binaryBody{}, respType: "application/octet-stream", status: http.StatusOK, sec: secSessionMember},
	}
}

func (b *specBuilder) faqOps() []op {
	return []op{
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/documents/faq", tag: "FAQ", summary: "创建 FAQ 文档",
			reqBody: createFAQRequest{}, respBody: dto.FAQDocument{}, status: http.StatusCreated, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/documents/:document_id/faq", tag: "FAQ", summary: "查询 FAQ 文档",
			respBody: dto.FAQDocument{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsRead}},
		{method: http.MethodPut, path: wsBase + "/knowledge-bases/:id/documents/:document_id/faq", tag: "FAQ", summary: "更新 FAQ 文档",
			reqBody: updateFAQRequest{}, respBody: dto.FAQDocument{}, status: http.StatusAccepted, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}},
	}
}

func (b *specBuilder) fileTreeOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/file-tree", tag: "文件树", summary: "查询文件树",
			respBody: dto.FileTree{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsRead}},
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/file-tree/folders", tag: "文件树", summary: "创建文件夹",
			reqBody: createFileTreeFolderRequest{}, respBody: dto.FileTreeNode{}, status: http.StatusCreated, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}},
		{method: http.MethodPatch, path: wsBase + "/knowledge-bases/:id/file-tree/nodes/:node_id", tag: "文件树", summary: "更新文件树节点",
			reqBody: updateFileTreeNodeRequest{}, respBody: nil, status: http.StatusNoContent, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}},
		{method: http.MethodDelete, path: wsBase + "/knowledge-bases/:id/file-tree/nodes/:node_id", tag: "文件树", summary: "删除文件树节点",
			respBody: nil, status: http.StatusNoContent, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsWrite}},
	}
}

func (b *specBuilder) chunkOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/documents/:document_id/chunks", tag: "分块", summary: "查询文档分块列表",
			respBody: dto.DocumentChunkPage{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsRead}, params: []openapiParam{{name: "enabled", in: "query", typeName: "boolean"}, {name: "cursor", in: "query", typeName: "string"}, {name: "limit", in: "query", typeName: "integer", defaultValue: 50}}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/chunks/:chunk_id", tag: "分块", summary: "查询单个分块",
			respBody: dto.Chunk{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeDocumentsRead}},
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/chunks/:chunk_id/revisions", tag: "分块", summary: "查询分块修订历史",
			respBody: []*dto.ChunkRevision{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/chunks/:chunk_id/revisions", tag: "分块", summary: "创建分块修订",
			reqBody: createChunkRevisionRequest{}, respBody: dto.ChunkRevision{}, status: http.StatusAccepted, sec: secSessionAdminRole},
	}
}

func (b *specBuilder) indexGenerationOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/knowledge-bases/:id/index-generations", tag: "索引", summary: "查询索引生成历史",
			respBody: []*dto.IndexGeneration{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/index-generations", tag: "索引", summary: "创建索引生成",
			reqBody: createIndexGenerationRequest{}, respBody: dto.IndexGeneration{}, status: http.StatusAccepted, sec: secSessionAdminRole},
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/index-generations/:generation_id/activate", tag: "索引", summary: "激活索引生成",
			reqBody: activateIndexGenerationRequest{}, respBody: dto.IndexGeneration{}, status: http.StatusOK, sec: secSessionAdminRole},
	}
}

func (b *specBuilder) searchOps() []op {
	return []op{
		{method: http.MethodPost, path: wsBase + "/knowledge-bases/:id/search", tag: "检索", summary: "单知识库检索",
			reqBody: searchRequest{}, respBody: []*dto.SearchResult{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeSearchRead}},
		{method: http.MethodPost, path: wsBase + "/search", tag: "检索", summary: "多知识库检索",
			reqBody: multiSearchRequest{}, respBody: multiSearchResponse{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeSearchRead}},
	}
}

func (b *specBuilder) jobOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/jobs/:id", tag: "任务", summary: "查询任务状态",
			respBody: dto.Job{}, status: http.StatusOK, sec: secSessionMember},
	}
}

func (b *specBuilder) modelProviderOps() []op {
	platform := "/api/v1/admin/model-providers"
	return []op{
		// workspace scope
		{method: http.MethodGet, path: wsBase + "/model-providers", tag: "模型供应商", summary: "列出 workspace 可见供应商",
			respBody: []*dto.ModelProvider{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodGet, path: wsBase + "/model-providers/options", tag: "模型供应商", summary: "供应商可选项",
			respBody: providerOptionsResponse{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodGet, path: wsBase + "/model-providers/:provider_id/model-catalog", tag: "模型供应商", summary: "查询供应商模型目录",
			respBody: dto.ModelCatalogResponse{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodGet, path: wsBase + "/model-providers/:provider_id", tag: "模型供应商", summary: "查询供应商详情",
			respBody: dto.ModelProvider{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodPost, path: wsBase + "/model-providers", tag: "模型供应商", summary: "创建 workspace 供应商",
			reqBody: createModelProviderRequest{}, respBody: dto.ModelProvider{}, status: http.StatusCreated, sec: secSessionAdminRole},
		{method: http.MethodPatch, path: wsBase + "/model-providers/:provider_id", tag: "模型供应商", summary: "更新 workspace 供应商",
			reqBody: updateModelProviderRequest{}, respBody: dto.ModelProvider{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodDelete, path: wsBase + "/model-providers/:provider_id", tag: "模型供应商", summary: "删除 workspace 供应商",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdminRole},
		// platform scope
		{method: http.MethodGet, path: platform, tag: "模型供应商", summary: "列出平台供应商（平台管理员）",
			respBody: []*dto.ModelProvider{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodGet, path: platform + "/options", tag: "模型供应商", summary: "供应商可选项（平台管理员）",
			respBody: providerOptionsResponse{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodPost, path: platform, tag: "模型供应商", summary: "创建平台供应商",
			reqBody: createModelProviderRequest{}, respBody: dto.ModelProvider{}, status: http.StatusCreated, sec: secSessionAdmin},
		{method: http.MethodGet, path: platform + "/:provider_id/model-catalog", tag: "模型供应商", summary: "查询供应商模型目录（平台管理员）",
			respBody: dto.ModelCatalogResponse{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodGet, path: platform + "/:provider_id", tag: "模型供应商", summary: "查询供应商详情（平台管理员）",
			respBody: dto.ModelProvider{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodPatch, path: platform + "/:provider_id", tag: "模型供应商", summary: "更新平台供应商",
			reqBody: updateModelProviderRequest{}, respBody: dto.ModelProvider{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodDelete, path: platform + "/:provider_id", tag: "模型供应商", summary: "删除平台供应商",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdmin},
	}
}

func (b *specBuilder) modelOps() []op {
	return []op{
		// workspace scope
		{method: http.MethodGet, path: wsBase + "/model-providers/:provider_id/models", tag: "模型", summary: "列出供应商模型",
			respBody: []*dto.Model{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodGet, path: wsBase + "/models", tag: "模型", summary: "列出可选模型",
			respBody: []*dto.Model{}, status: http.StatusOK, sec: secBearerOrSession, requiredScopes: []value.APIScope{value.ScopeKnowledgeBasesWrite}, description: "Bearer 请求必须精确使用 type=embedding、status=active、scope=platform；Session 兼容既有 selectable/management 合同。", params: []openapiParam{{name: "type", in: "query", typeName: "string", enum: []string{"embedding", "rerank", "all"}}, {name: "status", in: "query", typeName: "string", enum: []string{"active", "disabled", "all"}}, {name: "scope", in: "query", typeName: "string", enum: []string{"platform", "workspace", "all"}}}},
		{method: http.MethodGet, path: wsBase + "/models/:model_id", tag: "模型", summary: "查询模型详情",
			respBody: dto.Model{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodPost, path: wsBase + "/model-providers/:provider_id/models", tag: "模型", summary: "创建 workspace 模型",
			reqBody: createModelRequest{}, respBody: dto.Model{}, status: http.StatusCreated, sec: secSessionAdminRole},
		{method: http.MethodPatch, path: wsBase + "/models/:model_id", tag: "模型", summary: "更新 workspace 模型",
			reqBody: updateModelRequest{}, respBody: dto.Model{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodDelete, path: wsBase + "/models/:model_id", tag: "模型", summary: "删除 workspace 模型",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdminRole},
		{method: http.MethodPost, path: wsBase + "/models/:model_id/test", tag: "模型", summary: "测试 workspace 模型连通性",
			respBody: dto.ConnectionTestResult{}, status: http.StatusOK, sec: secSessionAdminRole},
		// platform scope
		{method: http.MethodPost, path: "/api/v1/admin/model-providers/:provider_id/models", tag: "模型", summary: "创建平台模型",
			reqBody: createModelRequest{}, respBody: dto.Model{}, status: http.StatusCreated, sec: secSessionAdmin},
		{method: http.MethodGet, path: "/api/v1/admin/model-providers/:provider_id/models", tag: "模型", summary: "列出供应商模型（平台管理员）",
			respBody: []*dto.Model{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodGet, path: "/api/v1/admin/models", tag: "模型", summary: "列出全部平台模型",
			respBody: []*dto.Model{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodGet, path: "/api/v1/admin/models/:model_id", tag: "模型", summary: "查询平台模型详情",
			respBody: dto.Model{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodPatch, path: "/api/v1/admin/models/:model_id", tag: "模型", summary: "更新平台模型",
			reqBody: updateModelRequest{}, respBody: dto.Model{}, status: http.StatusOK, sec: secSessionAdmin},
		{method: http.MethodDelete, path: "/api/v1/admin/models/:model_id", tag: "模型", summary: "删除平台模型",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdmin},
		{method: http.MethodPost, path: "/api/v1/admin/models/:model_id/test", tag: "模型", summary: "测试平台模型连通性",
			respBody: dto.ConnectionTestResult{}, status: http.StatusOK, sec: secSessionAdmin},
	}
}

func (b *specBuilder) apiKeyOps() []op {
	base := wsBase + "/api-keys"
	return []op{
		{method: http.MethodGet, path: base, tag: "API Key", summary: "列出 API Key",
			respBody: dto.WorkspaceAPIKeyListEnvelope{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodPost, path: base, tag: "API Key", summary: "创建 API Key（返回一次性明文）",
			reqBody: createAPIKeyRequest{}, respBody: dto.WorkspaceAPIKeySecretEnvelope{}, status: http.StatusCreated, sec: secSessionAdminRole},
		{method: http.MethodGet, path: base + "/:api_key_id", tag: "API Key", summary: "查询 API Key 详情",
			respBody: dto.WorkspaceAPIKeyDetailEnvelope{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodPatch, path: base + "/:api_key_id", tag: "API Key", summary: "更新 API Key",
			reqBody: updateAPIKeyRequest{}, respBody: dto.WorkspaceAPIKeyDetailEnvelope{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodPost, path: base + "/:api_key_id/reveal", tag: "API Key", summary: "重新获取 API Key 明文（no-store）",
			respBody: dto.WorkspaceAPIKeySecretEnvelope{}, status: http.StatusOK, sec: secSessionAdminRole},
		{method: http.MethodDelete, path: base + "/:api_key_id", tag: "API Key", summary: "吊销 API Key",
			respBody: nil, status: http.StatusNoContent, sec: secSessionAdminRole},
	}
}

func (b *specBuilder) membershipOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/members", tag: "成员", summary: "列出 workspace 成员",
			respBody: []*dto.Membership{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodPatch, path: wsBase + "/members/:user_id", tag: "成员", summary: "调整成员角色（owner）",
			reqBody: changeMemberRoleRequest{}, respBody: dto.Membership{}, status: http.StatusOK, sec: secSessionOwner},
		{method: http.MethodDelete, path: wsBase + "/members/:user_id", tag: "成员", summary: "移除成员（owner）",
			respBody: nil, status: http.StatusNoContent, sec: secSessionOwner},
	}
}

func (b *specBuilder) readinessAndSettingsOps() []op {
	return []op{
		{method: http.MethodGet, path: wsBase + "/readiness", tag: "Workspace", summary: "查询 workspace 就绪状态",
			respBody: dto.WorkspaceReadiness{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodGet, path: wsBase + "/search-settings", tag: "Workspace", summary: "查询检索设置",
			respBody: dto.WorkspaceSearchSettings{}, status: http.StatusOK, sec: secSessionMember},
		{method: http.MethodPut, path: wsBase + "/search-settings", tag: "Workspace", summary: "更新检索设置",
			reqBody: workspaceSearchSettingsRequest{}, respBody: dto.WorkspaceSearchSettings{}, status: http.StatusOK, sec: secSessionAdminRole},
	}
}
