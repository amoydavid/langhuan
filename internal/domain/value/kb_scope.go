package value

// KBScope 是成员的知识库可见范围（all | restricted）。
// all 为默认：可访问 workspace 内全部知识库（含未来新建）；
// restricted 只能访问 workspace_member_knowledge_bases 中显式授权的集合。
// 该约束仅作用于 member 角色：admin/owner 隐含全量（见 AuthContext.ResourceAccess）。
type KBScope string

const (
	// KBScopeAll 表示不限制成员的知识库可见范围（默认与存量行为）。
	KBScopeAll KBScope = "all"
	// KBScopeRestricted 表示成员只能访问显式授权的知识库集合。
	KBScopeRestricted KBScope = "restricted"
)

// Normalize 把零值/空串归一为 KBScopeAll（失败安全方向：未迁移路径回退现状）。
func (s KBScope) Normalize() KBScope {
	if s == "" {
		return KBScopeAll
	}
	return s
}

// IsRestricted 报告该范围是否为显式授权模式。零值按 all 处理。
func (s KBScope) IsRestricted() bool {
	return s.Normalize() == KBScopeRestricted
}

// IsValid 判断是否为已知的 all/restricted 之一（空串视为无效，需显式取值）。
func (s KBScope) IsValid() bool {
	return s == KBScopeAll || s == KBScopeRestricted
}
