package mineru

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	parserport "github.com/dajee/langhuan/internal/ports/parser"
)

// zipAsset 是 zip 内提取的图片文件，供 AssetResolver 归档。
type zipAsset struct {
	// RelativePath 是 zip 内的路径（如 images/xxx.jpg），
	// 与 Markdown 中的引用 `![](images/xxx.jpg)` 对应。
	RelativePath string
	// Name 是文件名（含扩展名）。
	Name string
	// MimeType 由扩展名推断。
	MimeType string
	// Data 是文件内容。
	Data []byte
}

// extractMarkdownFromZip 从 zip 数据中提取 Markdown 内容。
// 优先查找 full.md / result.md / *.md 文件。
// maxSizeBytes 限制单个文件解压后的大小（0 表示不限制），防止解压炸弹。
func extractMarkdownFromZip(data []byte, maxSizeBytes int64) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("打开 zip 失败: %w", err)
	}

	// 优先按名称查找
	priorityNames := []string{"full.md", "result.md", "main.md", "content.md"}
	for _, name := range priorityNames {
		if md, ok := readZipFile(reader, name, maxSizeBytes); ok {
			return md, nil
		}
	}

	// 回退到第一个 .md 文件
	for _, file := range reader.File {
		if strings.HasSuffix(strings.ToLower(file.Name), ".md") {
			md, ok := readZipFileEntry(file, maxSizeBytes)
			if ok {
				return md, nil
			}
		}
	}

	return "", fmt.Errorf("zip 中未找到 Markdown 文件")
}

// extractAssetsFromZip 提取 zip 内图片并映射为 parser 资产候选。
// 返回 nil 而不是 error：zip 中图片缺失或解压失败不应阻断解析主流程，
// 图片引用留待 AssetResolver 以 unsupported_ref warning 处理。
func extractAssetsFromZip(data []byte, maxSizeBytes int64) []parserport.AssetCandidate {
	assets, err := extractImagesFromZip(data, maxSizeBytes)
	if err != nil {
		return nil
	}
	if len(assets) == 0 {
		return nil
	}
	candidates := make([]parserport.AssetCandidate, 0, len(assets))
	for _, a := range assets {
		candidates = append(candidates, parserport.AssetCandidate{
			RelativePath: a.RelativePath,
			Name:         a.Name,
			MimeType:     a.MimeType,
			Data:         a.Data,
		})
	}
	return candidates
}

// extractImagesFromZip 提取 zip 内的图片文件（images/ 目录或图片扩展名条目）。
// maxSizeBytes 限制单个图片解压大小（0 表示不限制）。
// 返回按 zip 内路径索引的图片，供 AssetResolver 匹配 Markdown 相对路径引用。
func extractImagesFromZip(data []byte, maxSizeBytes int64) ([]zipAsset, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("打开 zip 失败: %w", err)
	}
	var result []zipAsset
	for _, file := range reader.File {
		name := file.Name
		if file.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		if !isImageExt(ext) {
			continue
		}
		content, ok := readZipFileEntry(file, maxSizeBytes)
		if !ok {
			continue
		}
		result = append(result, zipAsset{
			RelativePath: name,
			Name:         path.Base(name),
			MimeType:     mimeFromExt(ext),
			Data:         []byte(content),
		})
	}
	return result, nil
}

// isImageExt 判断扩展名是否为常见图片格式。
func isImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tiff", ".svg":
		return true
	default:
		return false
	}
}

// mimeFromExt 把图片扩展名映射为 MIME 类型。
func mimeFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".tiff":
		return "image/tiff"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

func readZipFile(reader *zip.Reader, name string, maxSizeBytes int64) (string, bool) {
	lower := strings.ToLower(name)
	for _, file := range reader.File {
		if strings.ToLower(file.Name) == lower {
			return readZipFileEntry(file, maxSizeBytes)
		}
	}
	return "", false
}

// readZipFileEntry 读取单个 zip 条目，限制解压大小防止 OOM。
func readZipFileEntry(file *zip.File, maxSizeBytes int64) (string, bool) {
	// M4 修复：检查声明大小，拒绝异常大的文件
	if maxSizeBytes > 0 && int64(file.UncompressedSize64) > maxSizeBytes {
		return "", false
	}
	rc, err := file.Open()
	if err != nil {
		return "", false
	}
	defer rc.Close()
	limit := int64(-1)
	if maxSizeBytes > 0 {
		limit = maxSizeBytes
	}
	data, err := io.ReadAll(io.LimitReader(rc, limit))
	if err != nil {
		return "", false
	}
	return string(data), true
}
