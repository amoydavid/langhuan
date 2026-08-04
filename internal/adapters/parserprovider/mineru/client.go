package mineru

import (
	"net/http"
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
// 完整的 RequestUploadURL/Upload/Poll/Download 方法在 MinerU parser 适配器中实现。
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
