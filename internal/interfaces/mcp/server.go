// Package mcp 实现琅嬛的 MCP Streamable HTTP Server，对外提供六个 typed tools。
// /mcp 只接受 Bearer API Key 鉴权（在 HTTP 层完成），鉴权后的 AuthContext 通过
// request.Context() 注入到 tool handler；工具层只做协议转换。
package mcp

import (
	"context"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
	"github.com/dajee/langhuan/internal/infrastructure/version"
)

// Dependencies 注入 MCP server 所需的全部 application service。
type Dependencies struct {
	KnowledgeBases MCPKnowledgeBaseService
	DocumentIngest MCPDocumentIngestService
	DocumentStatus *service.ProgrammaticDocumentStatusService
	DocumentDelete MCPDocumentDeleteService
	ChunkGet       MCPChunkGetService
	MultiSearch    *service.MultiKnowledgeSearchService
	InlineLimit    int64
	// EnableLocalhostProtection enables mcp-go's DNS rebinding protection.
	EnableLocalhostProtection bool
}

// Server 封装 mcp-go server 与 Streamable HTTP handler。
type Server struct {
	mcp     *mcpserver.MCPServer
	handler http.Handler
}

// NewServer 构造 langhuan MCP Server 并注册六个 typed tools。
// 工具注册或 output schema 生成失败时直接 panic（开发错误）。
func NewServer(deps Dependencies) *Server {
	buildVersion := version.Version()
	srv := mcpserver.NewMCPServer(
		"langhuan", buildVersion,
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithRecovery(),
		mcpserver.WithStrictInputSchemaDefault(),
		mcpserver.WithInputSchemaValidation(),
		mcpserver.WithOutputSchemaValidation(),
		mcpserver.WithToolFilter(scopeToolFilter),
	)
	registerTools(srv, deps)
	handler := mcpserver.NewStreamableHTTPServer(srv,
		mcpserver.WithStateLess(true),
		mcpserver.WithDisableStreaming(true),
		mcpserver.WithDisableLocalhostProtection(!deps.EnableLocalhostProtection),
	)
	return &Server{mcp: srv, handler: handler}
}

func (s *Server) Handler() http.Handler     { return s.handler }
func (s *Server) MCP() *mcpserver.MCPServer { return s.mcp }

// scopeToolFilter 按 AuthContext 的 scope 过滤 tools/list。无 AuthContext 时
// 返回全部（鉴权层已保证 /mcp 只有有效 key 到达此处）。
func scopeToolFilter(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	auth, ok := value.AuthContextFromContext(ctx)
	if !ok || !auth.IsAPIKey() {
		return tools
	}
	filtered := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		required, ok := toolScopeRequirement(tool.Name)
		if !ok || auth.HasScope(required) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
