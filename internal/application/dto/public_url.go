package dto

// PublicURLs 描述从全局 server.base_url 派生的对外可见地址。
//
// 所有 Web、REST、MCP 和邀请链接必须复用同一份派生结果，避免在多个
// adapter 里重新拼装 URL 造成漂移。
type PublicURLs struct {
	// BaseURL 是规范化（去尾斜杠）后的全局服务地址，例如
	// "https://langhuan.example.com" 或 "http://127.0.0.1:8080"。
	BaseURL string `json:"base_url"`
	// WebURL 是浏览器入口，固定为 BaseURL + "/"。
	WebURL string `json:"web_url"`
	// RESTBaseURL 是 REST API 前缀，固定为 BaseURL + "/api/v1"。
	RESTBaseURL string `json:"rest_base_url"`
	// MCPURL 是 MCP Streamable HTTP 入口，固定为 BaseURL + "/mcp"。
	MCPURL string `json:"mcp_url"`
}
