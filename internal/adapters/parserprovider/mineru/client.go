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

// UploadTicket 是 MinerU /file-urls/batch 返回的批量上传地址信息。
type UploadTicket struct {
	BatchID      string   // batch_id
	URLs         []string // file_urls：预签名上传地址列表
	ModelVersion string   // 使用的模型版本
}

// TaskStatus 描述 MinerU 任务的状态码（内部归一化后的值）。
type TaskStatus string

const (
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusSucceeded TaskStatus = "succeeded"
	TaskStatusFailed    TaskStatus = "failed"
)

// TaskResult 是轮询返回的任务结果摘要。
type TaskResult struct {
	Status        TaskStatus
	FullResultURL string // 成功时的 full_zip_url
	ErrorCode     string
	ErrorMessage  string
	// 进度信息（running/converting 时有值）
	ExtractedPages int
	TotalPages     int
}

// batchUploadRequest 是 /api/v4/file-urls/batch 的请求体。
// 文档规定 files 数组中每个对象只接受 name（必填）和 is_ocr/data_id/page_ranges（可选），
// 不接受 size。顶层字段是 model_version（不是 model）。
type batchUploadRequest struct {
	Files []batchUploadFile `json:"files"`
	// 顶层可选参数（与单文件/URL提交接口共享）
	ModelVersion  string `json:"model_version,omitempty"`
	EnableFormula *bool  `json:"enable_formula,omitempty"`
	EnableTable   *bool  `json:"enable_table,omitempty"`
	Language      string `json:"language,omitempty"`
}

type batchUploadFile struct {
	Name string `json:"name"`
}

// RequestUploadURL 向 MinerU 申请批量上传地址。
// 文档：POST /api/v4/file-urls/batch
func (c *Client) RequestUploadURL(ctx context.Context, fileName string, _ int64) (*UploadTicket, error) {
	reqBody := batchUploadRequest{
		Files:        []batchUploadFile{{Name: fileName}},
		ModelVersion: c.modelVersion,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("构建 MinerU 上传地址请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v4/file-urls/batch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构建 MinerU 上传地址请求失败: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.sanitizeError(err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BatchID  string   `json:"batch_id"`
			FileURLs []string `json:"file_urls"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("解析 MinerU 上传地址响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("MinerU 申请上传地址失败: code=%d msg=%s", apiResp.Code, apiResp.Msg)
	}
	return &UploadTicket{
		BatchID:      apiResp.Data.BatchID,
		URLs:         apiResp.Data.FileURLs,
		ModelVersion: c.modelVersion,
	}, nil
}

// Upload 将 PDF 内容 PUT 到 MinerU 返回的预签名 URL。
// 文档明确指出上传到预签名 URL 时不需要 Content-Type header。
func (c *Client) Upload(ctx context.Context, uploadURL string, content io.Reader, _ string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, content)
	if err != nil {
		return fmt.Errorf("构建 MinerU 上传请求失败: %w", err)
	}
	// 不设置 Content-Type —— 文档说预签名 URL 上传不需要
	// 不设置 Authorization —— 预签名 URL 自带鉴权
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

// Poll 查询 MinerU 批量任务状态。
// 文档：GET /api/v4/extract-results/batch/{batch_id}
func (c *Client) Poll(ctx context.Context, batchID string) (*TaskResult, error) {
	url := fmt.Sprintf("%s/api/v4/extract-results/batch/%s", c.baseURL, batchID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构建 MinerU 轮询请求失败: %w", err)
	}
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.sanitizeError(err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			State       string `json:"state"`
			FullZipURL  string `json:"full_zip_url"`
			ErrMsg      string `json:"err_msg"`
			ExtractProgress *struct {
				ExtractedPages int    `json:"extracted_pages"`
				TotalPages     int    `json:"total_pages"`
			} `json:"extract_progress"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("解析 MinerU 轮询响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("MinerU 轮询失败: code=%d msg=%s", apiResp.Code, apiResp.Msg)
	}

	result := &TaskResult{
		FullResultURL: apiResp.Data.FullZipURL,
		ErrorMessage:  apiResp.Data.ErrMsg,
	}
	// 失败时用 state 作为 ErrorCode（MinerU 没有独立 err_code 字段）
	if strings.ToLower(apiResp.Data.State) == "failed" {
		result.ErrorCode = "mineru_parse_failed"
	}
	if apiResp.Data.ExtractProgress != nil {
		result.ExtractedPages = apiResp.Data.ExtractProgress.ExtractedPages
		result.TotalPages = apiResp.Data.ExtractProgress.TotalPages
	}

	// 文档定义的实际状态值：waiting-file, pending, running, converting, done, failed
	switch strings.ToLower(apiResp.Data.State) {
	case "done":
		result.Status = TaskStatusSucceeded
	case "failed":
		result.Status = TaskStatusFailed
	case "running", "pending", "converting", "waiting-file":
		result.Status = TaskStatusRunning
	default:
		// 未知状态视为 running（保守策略：继续轮询而非失败）
		result.Status = TaskStatusRunning
	}
	return result, nil
}

// Download 下载 MinerU 结果。如果结果是 zip，提取其中的 Markdown 文件。
// 文档：Precise API 返回 full_zip_url，zip 内默认包含 Markdown 和 JSON。
func (c *Client) Download(ctx context.Context, resultURL string) (markdown string, rawBytes []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resultURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("构建 MinerU 结果下载请求失败: %w", err)
	}
	// 下载结果不需要 Authorization（CDN URL 自带鉴权）
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

	// 如果是 zip，提取 markdown（限制解压大小 100MB 防止 OOM）
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "zip") || strings.HasSuffix(strings.ToLower(resultURL), ".zip") {
		md, err := extractMarkdownFromZip(data, 100*1024*1024)
		if err != nil {
			return "", nil, fmt.Errorf("从 MinerU zip 提取 Markdown 失败: %w", err)
		}
		return md, data, nil
	}
	// 非 zip，直接当 markdown
	return string(data), data, nil
}

// setAuthHeaders 设置 MinerU Precise API 需要的 Authorization header。
// 仅用于与 mineru.net 交互的请求（申请上传地址、轮询），不用于预签名 URL 上传/下载。
func (c *Client) setAuthHeaders(req *http.Request) {
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
