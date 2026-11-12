package http

import (
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
)

// CapabilityFeature 是公开能力发现的稳定特性名。消费方（Studio/SDK）据此
// 协商：缺失必需特性时明确降级或阻止对应操作，不得假装兼容（合同 C01）。
// 新增特性必须保持字符串向后兼容：只追加，不重命名、不复用。
const (
	// FeatureSearchMultiKB 多知识库检索（POST /workspaces/{slug}/search）。
	FeatureSearchMultiKB = "search.multi_kb"
	// FeatureSearchDocumentIDsFilter 检索请求支持 document_ids 范围收窄
	//（召回前过滤，C02）。
	FeatureSearchDocumentIDsFilter = "search.document_ids_filter"
	// FeatureDocumentAIUseEnabled 文档级 ai_use_enabled 政策与 PATCH 端点（C02）。
	FeatureDocumentAIUseEnabled = "document.ai_use_enabled"
	// FeatureSourceAnchorPDFV2 PDF 物理页锚点（source_anchor.schema_version=2，
	// pdf.page/page_start/page_end，C03）。
	FeatureSourceAnchorPDFV2 = "source_anchor.pdf.v2"
	// FeatureRevisionAssetBearer revision 原件/资产支持 Bearer API Key 读取，
	// 含 Range/ETag（C05）。
	FeatureRevisionAssetBearer = "revision.asset.bearer"
	// FeatureDocumentRetry 失败文档/任务幂等重试。
	FeatureDocumentRetry = "document.retry"
)

// CapabilityFeatures 返回当前构建启用的全部稳定特性名。
// 顺序稳定（便于契约测试与 diff），只增不改。
func CapabilityFeatures() []string {
	return []string{
		FeatureSearchMultiKB,
		FeatureSearchDocumentIDsFilter,
		FeatureDocumentAIUseEnabled,
		FeatureSourceAnchorPDFV2,
		FeatureRevisionAssetBearer,
		FeatureDocumentRetry,
	}
}

// capabilities 是能力发现端点（C01）：免鉴权（与 healthz 同级，无敏感数据），
// 消费方启动时协商 required features。
func capabilities(c *gin.Context) {
	c.JSON(stdhttp.StatusOK, gin.H{
		"service":  "langhuan",
		"features": CapabilityFeatures(),
	})
}
