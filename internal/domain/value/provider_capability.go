package value

import (
	"regexp"
	"strings"
)

// ProviderCapability 描述一个 Provider 支持的能力类型，用于 Provider options。
// 能力由运行时 descriptor 注册，不在领域层维护供应商或能力白名单。
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

var providerCapabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// NormalizeProviderCapability 规范化并校验一个 descriptor 能力标识。
func NormalizeProviderCapability(capability ProviderCapability) (ProviderCapability, bool) {
	normalized := ProviderCapability(strings.ToLower(strings.TrimSpace(string(capability))))
	return normalized, providerCapabilityPattern.MatchString(string(normalized))
}
