package model

// SyncRoot 描述一个同步任务的起点：一个外部目录/知识库节点或一个网站源。
type SyncRoot struct {
	// Kind 取值见 ports/source 的 SyncRoot* 常量（drive_folder / wiki_node / web）。
	Kind string
	// Token 是外部侧的稳定起点标识：飞书为 folder_token / wiki node_token；
	// web 源为 seed_url（crawl）/ sitemap_url（sitemap），urls 模式为空串。
	Token string
}
