package value

// APIScope 描述 Workspace API Key 的程序化权限。
//
// scope 是精确集合，不存在 write 自动包含 read 的隐式规则；服务端在
// 持久化前会排序去重，保证响应与审计稳定。
type APIScope string

const (
	// ScopeKnowledgeBasesRead 允许列出/查看 KnowledgeBase 及其统计与文件树。
	ScopeKnowledgeBasesRead APIScope = "knowledge_bases:read"
	// ScopeKnowledgeBasesWrite 允许创建 KnowledgeBase，并把新建项原子加入
	// 当前 key 的知识库范围。
	ScopeKnowledgeBasesWrite APIScope = "knowledge_bases:write"
	// ScopeDocumentsRead 允许读取 Document 状态、Job 状态和 Chunk。
	ScopeDocumentsRead APIScope = "documents:read"
	// ScopeDocumentsWrite 允许导入和软删除 Document。
	ScopeDocumentsWrite APIScope = "documents:write"
	// ScopeSearchRead 允许对指定 KnowledgeBase 执行混合检索。
	ScopeSearchRead APIScope = "search:read"
)

// AllAPIScopes 返回全部合法 scope，供校验与文档复用。顺序固定。
func AllAPIScopes() []APIScope {
	return []APIScope{
		ScopeKnowledgeBasesRead,
		ScopeKnowledgeBasesWrite,
		ScopeDocumentsRead,
		ScopeDocumentsWrite,
		ScopeSearchRead,
	}
}

// IsValidAPIScope 报告给定 scope 是否属于合法集合。
func IsValidAPIScope(scope APIScope) bool {
	for _, valid := range AllAPIScopes() {
		if scope == valid {
			return true
		}
	}
	return false
}
