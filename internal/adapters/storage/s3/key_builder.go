// Package s3 提供 S3-compatible 对象存储适配器，同时实现 RawDocumentStore 和 AssetStore。
// 支持 RustFS / MinIO / 阿里云 OSS / 腾讯云 COS 等 path-style 或 virtual-host 风格的 endpoint。
package s3

import (
	"path"
	"strings"

	"github.com/google/uuid"
)

// mimeToExt 把常见图片 MIME 映射为扩展名，不在 allowlist 内的 MIME 回退到 .bin。
func mimeToExt(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "text/markdown", "text/x-markdown":
		return ".md"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}

// sanitizeFileName 从用户提供的文件名中只保留安全字符作为扩展名来源。
// 丢弃所有目录前缀，防止 path traversal。
func sanitizeFileName(fileName string) string {
	base := path.Base(fileName)
	// 如果 path.Base 返回的仍然是绝对路径（如 Windows 盘符），取最后的分量
	base = path.Base(strings.TrimPrefix(base, "/"))
	dot := strings.LastIndexByte(base, '.')
	if dot < 0 || dot == len(base)-1 {
		return ""
	}
	ext := strings.ToLower(base[dot:])
	// 只允许字母数字的扩展名
	var b strings.Builder
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RawDocumentKey 生成原始上传文件的 S3 key。
// 格式: raw-documents/{workspace_id}/{kb_id}/{doc_id}/{revision_id}/original.{ext}
func RawDocumentKey(workspaceID, kbID, docID, revisionID uuid.UUID, fileName string) string {
	ext := sanitizeFileName(fileName)
	if ext == "" {
		ext = ".bin"
	}
	return strings.Join([]string{
		"raw-documents",
		workspaceID.String(),
		kbID.String(),
		docID.String(),
		revisionID.String(),
		"original" + ext,
	}, "/")
}

// RawMarkdownKey 生成解析产出的原始 Markdown 的 S3 key。
// 格式: parser-results/{workspace_id}/{doc_id}/{revision_id}/{job_id}/raw.md
func RawMarkdownKey(workspaceID, docID, revisionID, jobID uuid.UUID) string {
	return strings.Join([]string{
		"parser-results",
		workspaceID.String(),
		docID.String(),
		revisionID.String(),
		jobID.String(),
		"raw.md",
	}, "/")
}

// AssetKey 生成图片资产的 S3 key。
// 格式: assets/{workspace_id}/{doc_id}/{revision_id}/{asset_id}.{ext}
// 扩展名从 mimeType 推导；若 mimeType 为空则从 fileName 推导。
func AssetKey(workspaceID, docID, revisionID, assetID uuid.UUID, mimeType, fileName string) string {
	ext := mimeToExt(mimeType)
	if ext == ".bin" && fileName != "" {
		if guessed := sanitizeFileName(fileName); guessed != "" {
			ext = guessed
		}
	}
	return strings.Join([]string{
		"assets",
		workspaceID.String(),
		docID.String(),
		revisionID.String(),
		assetID.String() + ext,
	}, "/")
}
