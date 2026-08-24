package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// hfClient 按「镜像优先、直连回退」下载 HuggingFace 数据集原始文件（spec §12）。
// 文件命中本地缓存（含 sha256 校验）时跳过下载。
type hfClient struct {
	Mirror   string // 如 https://hf-mirror.com
	Fallback string // 如 https://huggingface.co
	CacheDir string
	Client   *http.Client
}

func newHFClient(mirror, fallback, cacheDir string) *hfClient {
	if mirror == "" {
		mirror = "https://hf-mirror.com"
	}
	if fallback == "" {
		fallback = "https://huggingface.co"
	}
	return &hfClient{
		Mirror:   strings.TrimRight(mirror, "/"),
		Fallback: strings.TrimRight(fallback, "/"),
		CacheDir: cacheDir,
		Client:   &http.Client{Timeout: 30 * time.Minute},
	}
}

// downloadDatasetFile 下载 datasets/<repo>/resolve/main/<path> 到缓存目录，
// 返回本地路径与文件 sha256。镜像失败自动回退官方端点。
func (c *hfClient) downloadDatasetFile(repo, remotePath string) (string, string, error) {
	localPath := filepath.Join(c.CacheDir, repo, remotePath)
	if sum, ok := cachedSHA256(localPath); ok {
		return localPath, sum, nil
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", "", err
	}
	endpoints := []string{c.Mirror, c.Fallback}
	if strings.HasPrefix(c.Mirror, c.Fallback) || c.Mirror == c.Fallback {
		endpoints = []string{c.Mirror}
	}
	var lastErr error
	for _, endpoint := range endpoints {
		url := fmt.Sprintf("%s/datasets/%s/resolve/main/%s", endpoint, repo, remotePath)
		if err := c.downloadTo(url, localPath); err != nil {
			lastErr = err
			fmt.Printf("  下载失败（%s）：%v，尝试下一端点…\n", endpoint, err)
			continue
		}
		sum, err := fileSHA256(localPath)
		if err != nil {
			return "", "", err
		}
		return localPath, sum, nil
	}
	return "", "", fmt.Errorf("所有端点下载失败 %s/%s: %w", repo, remotePath, lastErr)
}

func (c *hfClient) downloadTo(url, dest string) error {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := c.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	tmp := dest + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dest)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// cachedSHA256 校验已有缓存文件可读即返回其 sha256。
func cachedSHA256(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return "", false
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return "", false
	}
	return sum, true
}
