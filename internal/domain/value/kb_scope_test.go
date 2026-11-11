package value

import (
	"testing"

	"github.com/google/uuid"
)

func TestKBScopeNormalizeAndValidity(t *testing.T) {
	tests := []struct {
		name       string
		scope      KBScope
		wantNormal KBScope
		wantRestr  bool
		wantValid  bool
	}{
		{name: "空串归一为 all", scope: "", wantNormal: KBScopeAll, wantRestr: false, wantValid: false},
		{name: "all 保持", scope: KBScopeAll, wantNormal: KBScopeAll, wantRestr: false, wantValid: true},
		{name: "restricted 保持", scope: KBScopeRestricted, wantNormal: KBScopeRestricted, wantRestr: true, wantValid: true},
		{name: "未知值非法", scope: KBScope("deny"), wantNormal: KBScope("deny"), wantRestr: false, wantValid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.scope.Normalize(); got != tt.wantNormal {
				t.Fatalf("Normalize() = %q, want %q", got, tt.wantNormal)
			}
			if got := tt.scope.IsRestricted(); got != tt.wantRestr {
				t.Fatalf("IsRestricted() = %v, want %v", got, tt.wantRestr)
			}
			if got := tt.scope.IsValid(); got != tt.wantValid {
				t.Fatalf("IsValid() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

// TestAuthContextResourceAccessMatrix 是成员知识库授权的语义矩阵（spec D2/D3/D7）：
// API Key 恒按绑定集合；Session 中 admin/owner 与 all 成员全量，受限 member 按授权集合。
func TestAuthContextResourceAccessMatrix(t *testing.T) {
	workspaceID := uuid.New()
	granted := uuid.New()
	ungranted := uuid.New()
	grants := []uuid.UUID{granted}

	tests := []struct {
		name           string
		auth           AuthContext
		wantUnrestrict bool
	}{
		{
			name:           "API Key 按绑定集合",
			auth:           NewAPIKeyAuthContext(uuid.New(), workspaceID, []APIScope{ScopeSearchRead}, grants),
			wantUnrestrict: false,
		},
		{
			name:           "member + all 全量",
			auth:           AuthContext{PrincipalKind: PrincipalUser, UserID: uuid.New(), WorkspaceID: workspaceID, Role: RoleMember, KBScope: KBScopeAll},
			wantUnrestrict: true,
		},
		{
			name:           "member + 零值 scope 按 all 归一（D7）",
			auth:           AuthContext{PrincipalKind: PrincipalUser, UserID: uuid.New(), WorkspaceID: workspaceID, Role: RoleMember},
			wantUnrestrict: true,
		},
		{
			name:           "member + restricted 按授权集合",
			auth:           AuthContext{PrincipalKind: PrincipalUser, UserID: uuid.New(), WorkspaceID: workspaceID, Role: RoleMember, KBScope: KBScopeRestricted, KnowledgeBaseIDs: grants},
			wantUnrestrict: false,
		},
		{
			name:           "admin + restricted 隐含全量（D2）",
			auth:           AuthContext{PrincipalKind: PrincipalUser, UserID: uuid.New(), WorkspaceID: workspaceID, Role: RoleAdmin, KBScope: KBScopeRestricted, KnowledgeBaseIDs: grants},
			wantUnrestrict: true,
		},
		{
			name:           "owner + restricted 隐含全量（D2）",
			auth:           AuthContext{PrincipalKind: PrincipalUser, UserID: uuid.New(), WorkspaceID: workspaceID, Role: RoleOwner, KBScope: KBScopeRestricted, KnowledgeBaseIDs: grants},
			wantUnrestrict: true,
		},
		{
			name:           "Session 无 workspace 上下文（平台级路由）全量",
			auth:           AuthContext{PrincipalKind: PrincipalUser, UserID: uuid.New()},
			wantUnrestrict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			access := tt.auth.ResourceAccess()
			if access.Unrestricted != tt.wantUnrestrict {
				t.Fatalf("Unrestricted = %v, want %v", access.Unrestricted, tt.wantUnrestrict)
			}
			if tt.wantUnrestrict {
				if !access.AllowsKnowledgeBase(ungranted) {
					t.Fatalf("Unrestricted 边界应放行任意知识库")
				}
				return
			}
			if !access.AllowsKnowledgeBase(granted) {
				t.Fatalf("授权集合内的知识库应放行")
			}
			if access.AllowsKnowledgeBase(ungranted) {
				t.Fatalf("授权集合外的知识库应拒绝")
			}
		})
	}
}
