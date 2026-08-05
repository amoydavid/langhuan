// Package requestmeta 在 context 中携带请求级元数据（request ID、transport、principal kind），
// 供 application 与 adapter 在结构化日志和错误链中使用。
package requestmeta

import "context"

// Meta 携带一次请求的稳定元数据。
type Meta struct {
	RequestID     string
	Transport     string
	PrincipalKind string
}

type metaKey struct{}

// With 把 Meta 写入 context。
func With(ctx context.Context, meta Meta) context.Context {
	if meta == (Meta{}) {
		return ctx
	}
	return context.WithValue(ctx, metaKey{}, meta)
}

// From 从 context 读取 Meta；不存在时返回零值。
func From(ctx context.Context) Meta {
	if ctx == nil {
		return Meta{}
	}
	value, _ := ctx.Value(metaKey{}).(Meta)
	return value
}
