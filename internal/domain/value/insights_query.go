package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// 洞察聚合的参数边界（spec §7）：窗口与条目上限越界按 validation error 拒绝，
// 不做静默钳制——调用方（前端）应只提供合法值。
const (
	// InsightsWindowMaxDays 是聚合窗口最大天数。
	InsightsWindowMaxDays = 31
	// InsightsListMaxLimit 是 overview/queries/documents 面板的条目上限。
	InsightsListMaxLimit = 50
	// InsightsActivityMaxLimit 是内容动态 feed 的条目上限。
	InsightsActivityMaxLimit = 100
)

// ValidateInsightsWindowDays 校验聚合窗口天数（1..31）。
func ValidateInsightsWindowDays(days int) error {
	if days < 1 || days > InsightsWindowMaxDays {
		return fmt.Errorf("%w: 窗口天数必须在 1..%d 之间", domainerrors.ErrValidation, InsightsWindowMaxDays)
	}
	return nil
}

// ValidateInsightsListLimit 校验 overview/queries/documents 的 limit（1..50）。
func ValidateInsightsListLimit(limit int) error {
	if limit < 1 || limit > InsightsListMaxLimit {
		return fmt.Errorf("%w: limit 必须在 1..%d 之间", domainerrors.ErrValidation, InsightsListMaxLimit)
	}
	return nil
}

// ValidateInsightsActivityLimit 校验 activity feed 的 limit（1..100）。
func ValidateInsightsActivityLimit(limit int) error {
	if limit < 1 || limit > InsightsActivityMaxLimit {
		return fmt.Errorf("%w: activity limit 必须在 1..%d 之间", domainerrors.ErrValidation, InsightsActivityMaxLimit)
	}
	return nil
}
