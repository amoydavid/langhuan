package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 繁→简单字转换表（OpenCC TSCharacters.txt，Apache-2.0）。
// 用于 miracl-simplified 变体：模拟「入库侧繁简归一化」的上限收益。
// 只做单字级转换（无词组消歧，如「乾隆」会被转成「干隆」）——
// 对检索测量是二阶噪声，报告需注明。
const (
	openCCTSCharactersURL  = "https://raw.githubusercontent.com/BYVoid/OpenCC/master/data/dictionary/TSCharacters.txt"
	openCCTSCharactersName = "TSCharacters.txt"
)

// loadT2STable 下载（或复用缓存）OpenCC 单字转换表，返回 rune -> 替换串。
func loadT2STable(cacheDir string) (map[rune]string, string, error) {
	localPath := filepath.Join(cacheDir, "opencc", openCCTSCharactersName)
	body, err := readCachedOrDownload(localPath, openCCTSCharactersURL)
	if err != nil {
		return nil, "", err
	}
	sum, err := fileSHA256(localPath)
	if err != nil {
		return nil, "", err
	}
	table := make(map[rune]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		runes := []rune(fields[0])
		if len(runes) != 1 {
			continue
		}
		table[runes[0]] = fields[1]
	}
	if len(table) < 1000 {
		return nil, "", fmt.Errorf("繁简转换表异常：仅 %d 条映射", len(table))
	}
	return table, sum, nil
}

// convertToSimplified 按单字表转换；未收录字符原样保留。
func convertToSimplified(text string, table map[rune]string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range text {
		if replacement, ok := table[r]; ok {
			builder.WriteString(replacement)
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// readCachedOrDownload 命中可读缓存即读本地，否则 GET 落盘。
func readCachedOrDownload(localPath, url string) ([]byte, error) {
	if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
		return os.ReadFile(localPath)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("下载 %s 失败: %w", filepath.Base(url), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s 失败: HTTP %d", filepath.Base(url), response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024))
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(localPath, body, 0o644); err != nil {
		return nil, err
	}
	return body, nil
}
