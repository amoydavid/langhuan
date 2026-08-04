package mineru

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClientConfig 描述 MinerU HTTP client 的连接参数。
type ClientConfig struct {
	BaseURL      string
	Token        string
	ModelVersion string
	HTTPTimeout  time.Duration
}

// Client 实现 MinerU Cloud HTTP API 调用。
type Client struct {
	baseURL      string
	token        string
	modelVersion string
	httpClient   *http.Client
}

// NewClient 创建 MinerU HTTP client。
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &Client{
		baseURL:      cfg.BaseURL,
		token:        cfg.Token,
		modelVersion: cfg.ModelVersion,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

// UploadTicket 是 MinerU 返回的批量上传地址信息。
type UploadTicket struct {
	BatchID  string   `json:"batch_id"`
	URLs     []string `json:"urls"`
	ModelVersion string `json:"model_version"`
}

// TaskStatus 描述 MinerU 任务的状态码。
type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusSucceeded TaskStatus = "succeeded"
	TaskStatusFailed    TaskStatus = "failed"
)

// TaskResult 是轮询返回的任务结果摘要。
type TaskResult struct {
	Status       TaskStatus
	FullResultURL string // 成功时下载结果 zip/md 的 URL
	ErrorCode    string
	ErrorMessage string
}

// RequestUploadURL 向 MinerU 申请批量上传地址。
func (c *Client) RequestUploadURL(ctx context.Context, fileName string, sizeBytes int64) (*UploadTicket, error) {
	body, _ := json.Marshal(map[string]any{
		"enable_formula": true,
		"language":       "auto",
		"model":          c.modelVersion,
		"files": []map[string]any{
			{"name": fileName, "size": sizeBytes, "is_ocr": true},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v4/file-urls/batch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构建 MinerU 上传地址请求失败: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.sanitizeError(err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code int `json:"code"`
		Data struct {
			BatchID  string   `json:"batch_id"`
			URLs     []string `json:"file_urls"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("解析 MinerU 上传地址响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("MinerU 申请上传地址失败: code=%d msg=%s", apiResp.Code, apiResp.Msg)
	}
	return &UploadTicket{
		BatchID:      apiResp.Data.BatchID,
		URLs:         apiResp.Data.URLs,
		ModelVersion: c.modelVersion,
	}, nil
}

// Upload 将 PDF 内容上传到 MinerU 返回的签名 URL。
func (c *Client) Upload(ctx context.Context, uploadURL string, content io.Reader, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, content)
	if err != nil {
		return fmt.Errorf("构建 MinerU 上传请求失败: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.sanitizeError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("MinerU 上传失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Poll 查询 MinerU 任务状态。
func (c *Client) Poll(ctx context.Context, batchID string) (*TaskResult, error) {
	url := fmt.Sprintf("%s/api/v4/extract-results/batch/%s", c.baseURL, batchID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构建 MinerU 轮询请求失败: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.sanitizeError(err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code int `json:"code"`
		Data struct {
			Status        string `json:"state"`
			FullResultURL string `json:"full_zip_url"`
			ErrCode       string `json:"err_code"`
			ErrMsg        string `json:"err_msg"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("解析 MinerU 轮询响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("MinerU 轮询失败: code=%d msg=%s", apiResp.Code, apiResp.Msg)
	}

	result := &TaskResult{
		FullResultURL: apiResp.Data.FullResultURL,
		ErrorCode:     apiResp.Data.ErrCode,
		ErrorMessage:  apiResp.Data.ErrMsg,
	}
	switch strings.ToLower(apiResp.Data.Status) {
	case "running", "pending", "processing":
		result.Status = TaskStatusRunning
	case "success", "succeeded", "done", "completed":
		result.Status = TaskStatusSucceeded
	case "failed", "error":
		result.Status = TaskStatusFailed
	default:
		result.Status = TaskStatusRunning
	}
	return result, nil
}

// Download 下载 MinerU 结果。如果结果是 zip，提取其中的 Markdown 文件。
func (c *Client) Download(ctx context.Context, resultURL string) (markdown string, rawBytes []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resultURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("构建 MinerU 结果下载请求失败: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, c.sanitizeError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("MinerU 结果下载失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("读取 MinerU 结果失败: %w", err)
	}

	// 如果是 zip，提取 markdown
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "zip") || strings.HasSuffix(strings.ToLower(resultURL), ".zip") {
		md, err := extractMarkdownFromZip(data)
		if err != nil {
			return "", nil, fmt.Errorf("从 MinerU zip 提取 Markdown 失败: %w", err)
		}
		return md, data, nil
	}
	// 非 zip，直接当 markdown
	return string(data), data, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
}

// sanitizeError 确保 token 不泄漏到错误消息中。
func (c *Client) sanitizeError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, c.token) {
		msg = strings.ReplaceAll(msg, c.token, "***")
	}
	return fmt.Errorf("MinerU 请求失败: %s", msg)
}

