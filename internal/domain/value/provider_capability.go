package value

// ProviderCapability 描述一个 Provider 支持的能力类型，用于 Provider options。
// 它与 ModelType 在 embedding/rerank 上重叠，但额外包含 parser 这种非模型能力。
type ProviderCapability string

const (
	// CapabilityEmbedding 表示 Provider 支持文本向量模型。
	CapabilityEmbedding ProviderCapability = "embedding"
	// CapabilityRerank 表示 Provider 支持重排模型。
	CapabilityRerank ProviderCapability = "rerank"
	// CapabilityParser 表示 Provider 支持文档解析（如 MinerU）。
	CapabilityParser ProviderCapability = "parser"
)

// CapabilityFromModelType 把 ModelType 映射为对应的能力标签。
func CapabilityFromModelType(modelType ModelType) ProviderCapability {
	switch modelType {
	case ModelTypeEmbedding:
		return CapabilityEmbedding
	case ModelTypeRerank:
		return CapabilityRerank
	default:
		return ProviderCapability(modelType)
	}
}
