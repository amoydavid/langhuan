package value

import (
	"fmt"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// SourceDeletePolicy 描述来源清理任务对本地文档的删除策略。
//
//   - SourceDeleteKeep：保留本地文档（默认、安全策略）。
//   - SourceDeleteRemove：软删本地文档。
//
// 历史上 SourceConfig 可能缺失/空/非法值，配置加载场景统一退化为 keep；
// 显式 API（创建知识库）必须提供合法值，缺失即视为校验错误。
type SourceDeletePolicy string

const (
	// SourceDeleteKeep 保留本地文档，是缺失/非法配置的回退值。
	SourceDeleteKeep SourceDeletePolicy = "keep"
	// SourceDeleteRemove 软删本地文档。
	SourceDeleteRemove SourceDeletePolicy = "remove"
)

// ParseSourceDeletePolicy 是严格解析器，供 API/创建知识库使用。
// 仅接受 keep / remove（大小写不敏感、去首尾空白）；空值或非法值返回 ErrValidation。
func ParseSourceDeletePolicy(raw string) (SourceDeletePolicy, error) {
	normalized := normalizeSourceDeletePolicy(raw)
	switch SourceDeletePolicy(normalized) {
	case SourceDeleteKeep, SourceDeleteRemove:
		return SourceDeletePolicy(normalized), nil
	}
	return SourceDeleteKeep, fmt.Errorf("%w: 未知的 source delete policy %q", domainerrors.ErrValidation, raw)
}

// SourceDeletePolicyFromConfig 是宽容解析器，供配置加载使用。
// nil、空串、空白、类型不符或非法值统一退化为 keep；合法值返回归一化后的策略。
// 它不返回错误：历史配置缺失字段不应阻断服务启动。
func SourceDeletePolicyFromConfig(raw any) SourceDeletePolicy {
	s, ok := raw.(string)
	if !ok {
		return SourceDeleteKeep
	}
	normalized := normalizeSourceDeletePolicy(s)
	switch SourceDeletePolicy(normalized) {
	case SourceDeleteRemove:
		return SourceDeleteRemove
	default:
		// 包括 keep 与任何未知值，统一退化为 keep。
		return SourceDeleteKeep
	}
}

// IsValid 报告该策略是否属于已知合法集合。
func (p SourceDeletePolicy) IsValid() bool {
	return p == SourceDeleteKeep || p == SourceDeleteRemove
}

// String 返回策略的归一化字符串形式。
func (p SourceDeletePolicy) String() string {
	return string(p)
}

// normalizeSourceDeletePolicy 去空白并转小写，便于大小写不敏感比较。
func normalizeSourceDeletePolicy(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
