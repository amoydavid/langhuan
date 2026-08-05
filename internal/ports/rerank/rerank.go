// Package rerank 定义琅嬛与外部 Rerank 服务之间的领域端口。
//
// Application 层根据需要在此声明 Client、Factory 与输入输出类型；
// 具体第三方协议由 internal/adapters/rerank 下的实现满足。
package rerank

import "context"

// Document 是一次重排调用的单个候选文本。ID 是 application 生成的短生命周期
// opaque 标识，绝不向 adapter 暴露 UUID、正文或数据库行；adapter 按位置调用上游，
// 返回前必须恢复并校验 DocumentID。
type Document struct {
	ID   string
	Text string
}

// RerankInput 是单次重排请求的输入。
type RerankInput struct {
	Query     string
	Documents []Document
	TopN      int
}

// RerankItem 是重排返回的单个结果，分数必须为有限浮点数。
type RerankItem struct {
	DocumentID string
	Score      float64
}

// RerankResult 是重排调用的结果集合。
type RerankResult struct {
	Items []RerankItem
}

// Client 是 application 持有的 Rerank 客户端端口。
type Client interface {
	Rerank(ctx context.Context, input RerankInput) (*RerankResult, error)
}
