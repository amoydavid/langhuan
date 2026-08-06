package model

// SyncRoot 描述一个同步任务的起点：一个外部目录/知识库节点。
type SyncRoot struct {
	// Kind 取值见 ports/source 的 SyncRoot* 常量（drive_folder / wiki_node）。
	Kind string
	// Token 是飞书侧的稳定标识（folder_token / wiki node_token）。
	Token string
}
