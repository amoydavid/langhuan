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
func extractMarkdownFromZip(data []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("打开 zip 失败: %w", err)
	}

	// 优先按名称查找
	priorityNames := []string{"full.md", "result.md", "main.md", "content.md"}
	for _, name := range priorityNames {
		if md, ok := readZipFile(reader, name); ok {
			return md, nil
		}
	}

	// 回退到第一个 .md 文件
	for _, file := range reader.File {
		if strings.HasSuffix(strings.ToLower(file.Name), ".md") {
			rc, err := file.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			return string(data), nil
		}
	}

	return "", fmt.Errorf("zip 中未找到 Markdown 文件")
}

func readZipFile(reader *zip.Reader, name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, file := range reader.File {
		if strings.ToLower(file.Name) == lower {
			rc, err := file.Open()
			if err != nil {
				return "", false
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return "", false
			}
			return string(data), true
		}
	}
	return "", false
}
