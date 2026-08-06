package model

import "time"

// ExternalNode 表示外部内容源（飞书）目录树中的一个节点。
type ExternalNode struct {
	// Token 是该节点的稳定标识（docx 的 document_id、wiki 的 node_token、folder 的 token）。
	Token string
	// ParentToken 是父节点标识，根节点为空。
	ParentToken string
	// Title 是节点标题（用于命名 FileTreeNode）。
	Title string
	// ObjType 是飞书侧对象类型："docx" / "folder" / "sheet" / "bitable" / ...
	ObjType string
	// HasDocument 表示该节点是否对应一个可同步的文档（docx 为 true；folder 为 false）。
	HasDocument bool
	// EditTime 是飞书侧最近编辑时间，用于增量同步判断；缺省为 EditTimeZero。
	EditTime time.Time
}

// FetchedDocument 是拉取单个外部文档后的结果。
type FetchedDocument struct {
	// Markdown 是 docx 转换后的 markdown 字节。
	Markdown []byte
	// Title 是文档标题。
	Title string
	// EditTime 是文档最近编辑时间。
	EditTime time.Time
	// ObjType 是文档类型（"docx" 等）。
	ObjType string
}
