package value

// KnowledgeBaseSourceType 表示知识库的内容来源类型。
type KnowledgeBaseSourceType string

const (
	// SourceTypeUpload 表示本地文件上传型知识库。
	SourceTypeUpload KnowledgeBaseSourceType = "upload"
	// SourceTypeFeishuDrive 表示从飞书云文档（云空间目录）同步的知识库。
	SourceTypeFeishuDrive KnowledgeBaseSourceType = "feishu_drive"
	// SourceTypeFeishuWiki 表示从飞书知识库（wiki 空间）同步的知识库。
	SourceTypeFeishuWiki KnowledgeBaseSourceType = "feishu_wiki"
)

// IsValid 判断来源类型是否为已知值。
func (s KnowledgeBaseSourceType) IsValid() bool {
	return s == SourceTypeUpload || s == SourceTypeFeishuDrive || s == SourceTypeFeishuWiki
}

// IsFeishu 判断是否为飞书同步来源（drive 或 wiki）。
func (s KnowledgeBaseSourceType) IsFeishu() bool {
	return s == SourceTypeFeishuDrive || s == SourceTypeFeishuWiki
}
