package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/dajee/langhuan/internal/application/dto"
)

// PublicURLBuilder 把全局唯一的 server.base_url 派生成 Web、REST、MCP 和
// 邀请链接等对外地址。它是配置阶段唯一允许构造公开 URL 的位置，其它代码
// 只能消费 builder 的结果，禁止再从请求 Host、origin 或前端常量推断。
type PublicURLBuilder struct {
	baseURL string
	parsed  *url.URL
}

// NewPublicURLBuilder 校验并规范化 base_url，返回可复用的 builder。
//
// 规则：
//   - 必须是绝对的 http/https URL。
//   - 不得包含 userinfo、query 或 fragment。
//   - 去掉末尾斜杠，支持部署前缀（例如 https://example.com/langhuan）。
//
// 空值视为非法：v0.6.0 起 base_url 是必填配置。
func NewPublicURLBuilder(baseURL string) (*PublicURLBuilder, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return nil, errors.New("server.base_url 不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("server.base_url 无效: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("server.base_url 必须是绝对 http/https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("server.base_url 不得包含用户信息、query 或 fragment")
	}
	normalized := strings.TrimRight(raw, "/")
	return &PublicURLBuilder{baseURL: normalized, parsed: parsed}, nil
}

// URLs 返回 Web、REST、MCP 三个派生地址。WebURL 固定带末尾斜杠。
func (b *PublicURLBuilder) URLs() dto.PublicURLs {
	return dto.PublicURLs{
		BaseURL:     b.baseURL,
		WebURL:      b.baseURL + "/",
		RESTBaseURL: b.baseURL + "/api/v1",
		MCPURL:      b.baseURL + "/mcp",
	}
}

// Resolve 把以 "/" 开头的绝对应用路径拼接到 base_url 之后，用于邀请链接
// 等需要完整 URL 的场景。path 形如 "/invitations/accept?token=x"。
func (b *PublicURLBuilder) Resolve(path string) string {
	return b.baseURL + path
}

// BaseURL 返回规范化后的 base_url，便于需要原始值的调用方使用。
func (b *PublicURLBuilder) BaseURL() string {
	return b.baseURL
}

// ValidateProduction 要求生产部署使用 HTTPS。本地开发允许 http。
func (b *PublicURLBuilder) ValidateProduction() error {
	if b.parsed.Scheme != "https" {
		return errors.New("生产环境 server.base_url 必须使用 HTTPS")
	}
	return nil
}
