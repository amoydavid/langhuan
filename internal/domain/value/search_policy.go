package value

// AIPolicyVersionV1 是文档级 AI 政策（ai_use_enabled）的当前版本（C02）。
// 检索响应携带该版本，供 Studio/SDK 记录"本次回答依据的召回政策"，
// 政策语义变化时递增（只增不改）。
const AIPolicyVersionV1 = "ai-use-v1"

// CurrentAIPolicyVersion 返回当前政策版本。
func CurrentAIPolicyVersion() string { return AIPolicyVersionV1 }
