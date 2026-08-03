package value

// ModelScope 表示模型 Provider 的可见作用域。
type ModelScope string

const (
	// ModelScopePlatform 表示所有 Workspace 可见的平台共享 Provider。
	ModelScopePlatform ModelScope = "platform"
	// ModelScopeWorkspace 表示仅所属 Workspace 可见的 Provider。
	ModelScopeWorkspace ModelScope = "workspace"
)

// IsValid 判断作用域是否为已知值。
func (s ModelScope) IsValid() bool {
	return s == ModelScopePlatform || s == ModelScopeWorkspace
}
