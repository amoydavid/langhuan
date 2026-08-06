package http

import (
	stdhttp "net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gin-gonic/gin"
)

// 本文件含两类文档专用定义：
//  1. Scalar 文档 UI 的 HTML（scalarHTML）；
//  2. 少量文档专用占位 struct，用于表达 handler 里用 gin.H/二进制流返回、
//     没有具名 struct 的响应。这些 struct 只服务文档生成，不参与运行时。

// serveOpenAPISpec 返回一个 gin handler，把启动时生成的 spec 序列化为 JSON 输出。
func serveOpenAPISpec(spec *openapi3.T) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(stdhttp.StatusOK, spec)
	}
}

// serveDocsUI 返回 Scalar 文档 UI 的 HTML 页面。
func serveDocsUI() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(stdhttp.StatusOK, "text/html; charset=utf-8", []byte(scalarHTML))
	}
}

// scalarHTML 返回 Scalar API Reference 的 HTML 页面，指向 /openapi.json。
// Scalar 从 CDN 加载，无需本地静态资源。
const scalarHTML = `<!doctype html>
<html>
  <head>
    <title>琅嬛 API 文档</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <div id="openapi-docs"></div>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
    <script>
      Scalar.createApiReference('#openapi-docs', {
        spec: { url: '/openapi.json' },
      });
    </script>
  </body>
</html>
`

// --- 文档专用占位 struct ---
//
// 这些 struct 不被任何 handler 运行时使用，只为了让 openapi 反射器能生成
// 对应 schema。字段与 handler 实际输出对齐（见各 handler 的 c.JSON 调用）。

// loginResponse 对应 auth_handler.go login 的 c.JSON(gin.H{"user_id": ...})。
type loginResponse struct {
	UserID string `json:"user_id"`
}

// bootstrapStatusResponse 对应 auth_handler.go bootstrapStatus 的
// c.JSON(gin.H{"initialized": ...})。
type bootstrapStatusResponse struct {
	Initialized bool `json:"initialized"`
}

// documentIngestForm 描述 multipart/form-data 上传的表单字段。
// file 字段是二进制文件，其余是文本字段。仅供文档生成。
type documentIngestForm struct {
	File         string `json:"file"`
	Title        string `json:"title,omitempty"`
	SourceType   string `json:"source_type,omitempty"`
	ParentNodeID string `json:"parent_node_id,omitempty"`
	NodeName     string `json:"node_name,omitempty"`
}

// binaryBody 表示二进制响应体（无 JSON schema），仅用于标记响应 Content-Type。
type binaryBody struct{}
