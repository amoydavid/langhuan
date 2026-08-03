package value

// ModelStatus 表示 Provider 或 Model 的生命周期状态。
type ModelStatus string

const (
	// ModelStatusActive 表示记录可用于新知识库。
	ModelStatusActive ModelStatus = "active"
	// ModelStatusDisabled 表示记录保留但不可用于新知识库。
	ModelStatusDisabled ModelStatus = "disabled"
)

// IsValid 判断状态是否为已知值。
func (s ModelStatus) IsValid() bool {
	return s == ModelStatusActive || s == ModelStatusDisabled
}
