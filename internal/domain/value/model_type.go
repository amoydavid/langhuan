package value

// ModelType 表示模型能力类型。
type ModelType string

const (
	// ModelTypeEmbedding 表示文本向量模型。
	ModelTypeEmbedding ModelType = "embedding"
	// ModelTypeLLM 为后续大语言模型配置保留。
	ModelTypeLLM ModelType = "llm"
	// ModelTypeRerank 为后续重排模型配置保留。
	ModelTypeRerank ModelType = "rerank"

	// DefaultEmbeddingDimension 是表单与测试数据的默认维度。
	DefaultEmbeddingDimension = 1024
)

// SupportedEmbeddingDimensions 与 000001 迁移中已有的 HNSW 部分索引保持一致。
var SupportedEmbeddingDimensions = [...]int{798, 1024, 2048, 3584}

// IsValid 判断模型类型是否为数据模型支持的已知值。
func (t ModelType) IsValid() bool {
	return t == ModelTypeEmbedding || t == ModelTypeLLM || t == ModelTypeRerank
}

// IsSupportedEmbeddingDimension 判断维度是否有对应的 HNSW 部分索引。
func IsSupportedEmbeddingDimension(dimension int) bool {
	for _, supported := range SupportedEmbeddingDimensions {
		if dimension == supported {
			return true
		}
	}
	return false
}
