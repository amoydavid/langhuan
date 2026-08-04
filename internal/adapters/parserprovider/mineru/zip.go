package mineru

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

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
