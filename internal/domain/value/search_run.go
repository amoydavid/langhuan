package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// RetrievalStatus 是一次检索运行的最终协议状态，不等同于 HTTP 状态码。
// running 仅用于数据库执行期间的内部状态，不会作为已完成的 API 响应返回。
type RetrievalStatus string

const (
	// RetrievalStatusRunning 表示检索正在执行中（内部状态，不对外返回）。
	RetrievalStatusRunning RetrievalStatus = "running"
	// RetrievalStatusAvailable 表示检索成功并返回至少一条证据。
	RetrievalStatusAvailable RetrievalStatus = "available"
	// RetrievalStatusEmpty 表示检索成功，但没有证据命中。
	RetrievalStatusEmpty RetrievalStatus = "empty"
	// RetrievalStatusDegraded 表示返回证据，但发生了允许降级的情况（如 Rerank 回退）。
	RetrievalStatusDegraded RetrievalStatus = "degraded"
	// RetrievalStatusFailed 表示未能完成检索；failure_class 必填。
	RetrievalStatusFailed RetrievalStatus = "failed"
)

// Validate 判断检索状态是否为已知值。
func (s RetrievalStatus) Validate() error {
	switch s {
	case RetrievalStatusRunning, RetrievalStatusAvailable, RetrievalStatusEmpty,
		RetrievalStatusDegraded, RetrievalStatusFailed:
		return nil
	default:
		return fmt.Errorf("%w: 未知检索状态 %q", domainerrors.ErrValidation, s)
	}
}

// IsTerminal 判断检索状态是否为终态（running 不是终态）。
func (s RetrievalStatus) IsTerminal() bool {
	switch s {
	case RetrievalStatusAvailable, RetrievalStatusEmpty, RetrievalStatusDegraded, RetrievalStatusFailed:
		return true
	default:
		return false
	}
}

// CitationStatus 表示一条引用是否可用于验证。
type CitationStatus string

const (
	// CitationStatusValid 表示引用来自当前可加载的证据。
	CitationStatusValid CitationStatus = "valid"
	// CitationStatusUnavailable 表示引用对应的资源已不可用（预留，本期不产生）。
	CitationStatusUnavailable CitationStatus = "unavailable"
)

// Validate 判断引用状态是否为已知值。
func (s CitationStatus) Validate() error {
	switch s {
	case CitationStatusValid, CitationStatusUnavailable:
		return nil
	default:
		return fmt.Errorf("%w: 未知引用状态 %q", domainerrors.ErrValidation, s)
	}
}

// SearchScope 表示检索请求的有效范围语义。
type SearchScope string

const (
	// SearchScopeSelected 表示检索请求显式指定了知识库集合。
	SearchScopeSelected SearchScope = "selected"
	// SearchScopeAPIKeyBoundAll 表示 MCP adapter 将空 knowledge_base_ids 展开为当前 API Key 绑定集合。
	SearchScopeAPIKeyBoundAll SearchScope = "api_key_bound_all"
)

// Validate 判断检索范围是否为已知值。
func (s SearchScope) Validate() error {
	switch s {
	case SearchScopeSelected, SearchScopeAPIKeyBoundAll:
		return nil
	default:
		return fmt.Errorf("%w: 未知检索范围 %q", domainerrors.ErrValidation, s)
	}
}

// NormalizeSearchScope 把零值规范化为 selected。
func NormalizeSearchScope(scope SearchScope) SearchScope {
	if scope == "" {
		return SearchScopeSelected
	}
	return scope
}
