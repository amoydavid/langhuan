package value

import (
	"context"

	"github.com/google/uuid"
)

// PrincipalKind 描述请求主体的类型。
type PrincipalKind string

const (
	// PrincipalUser 表示浏览器 Session 用户主体。
	PrincipalUser PrincipalKind = "user"
	// PrincipalAPIKey 表示程序化 Bearer API Key 主体。
	PrincipalAPIKey PrincipalKind = "workspace_api_key"
)

// AuthContext 表示一次请求的认证上下文。
//
// Session 主体的 Role 来自 membership，Scopes/KnowledgeBaseIDs 为空，且不受
// API Key 知识库范围限制；API Key 主体的 UserID 为空，Workspace/Scopes/
// KnowledgeBaseIDs 来自 key，绑定集合必须非空，且不继承创建者身份或实时角色。
//
// WorkspaceID 与 Role 仅在 workspace-scoped 路由上填充；
// 平台级路由（如登录、注册）这两字段为零值。
type AuthContext struct {
	PrincipalKind    PrincipalKind
	PrincipalID      uuid.UUID
	UserID           uuid.UUID
	IsPlatformAdmin  bool
	WorkspaceID      uuid.UUID
	Role             WorkspaceRole
	Scopes           []APIScope
	KnowledgeBaseIDs []uuid.UUID
}

// IsAPIKey 报告主体是否为程序化 API Key。
func (a AuthContext) IsAPIKey() bool { return a.PrincipalKind == PrincipalAPIKey }

// IsUser 报告主体是否为浏览器 Session 用户。
func (a AuthContext) IsUser() bool {
	return a.PrincipalKind == PrincipalUser || (a.PrincipalKind == "" && a.UserID != uuid.Nil)
}

// HasScope 报告 API Key 主体是否拥有指定 scope。Session 主体不受 scope 限制。
func (a AuthContext) HasScope(scope APIScope) bool {
	if !a.IsAPIKey() {
		return true
	}
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ResourceAccess 是协议中立的资源访问边界。Session 主体为 Unrestricted；
// API Key 主体只能访问 AllowedKnowledgeBaseIDs 集合内的资源。
type ResourceAccess struct {
	WorkspaceID             uuid.UUID
	Unrestricted            bool
	AllowedKnowledgeBaseIDs []uuid.UUID
}

// AllowsKnowledgeBase 报告该访问边界是否允许操作指定知识库。
func (a ResourceAccess) AllowsKnowledgeBase(id uuid.UUID) bool {
	if a.Unrestricted {
		return true
	}
	for _, allowed := range a.AllowedKnowledgeBaseIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

// ResourceAccess 把当前主体转成协议中立的资源访问边界。
// Session 主体为 Unrestricted；API Key 主体只能访问绑定的知识库集合。
func (a AuthContext) ResourceAccess() ResourceAccess {
	if !a.IsAPIKey() {
		return ResourceAccess{WorkspaceID: a.WorkspaceID, Unrestricted: true}
	}
	return ResourceAccess{WorkspaceID: a.WorkspaceID, AllowedKnowledgeBaseIDs: a.KnowledgeBaseIDs}
}

// NewAPIKeyAuthContext 构造 API Key 主体的认证上下文。绑定集合必须非空。
func NewAPIKeyAuthContext(apiKeyID, workspaceID uuid.UUID, scopes []APIScope, knowledgeBaseIDs []uuid.UUID) AuthContext {
	return AuthContext{
		PrincipalKind:    PrincipalAPIKey,
		PrincipalID:      apiKeyID,
		WorkspaceID:      workspaceID,
		Scopes:           scopes,
		KnowledgeBaseIDs: knowledgeBaseIDs,
	}
}

// authContextContextKey 是 context.Context 注入 AuthContext 的键。
type authContextContextKey struct{}

// ContextWithAuthContext 把 AuthContext 注入 context.Context，供 MCP 等非 gin
// 入口使用。
func ContextWithAuthContext(ctx context.Context, auth AuthContext) context.Context {
	return context.WithValue(ctx, authContextContextKey{}, auth)
}

// AuthContextFromContext 从 context.Context 读取 AuthContext。不存在时返回
// 零值与 false。
func AuthContextFromContext(ctx context.Context) (AuthContext, bool) {
	auth, ok := ctx.Value(authContextContextKey{}).(AuthContext)
	return auth, ok
}
