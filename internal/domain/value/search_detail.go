package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// SearchResultDetail 选择检索结果的响应档位（渐进披露）：
// full 返回父块正文 + 子块元数据；lean 返回最佳命中子块正文（evidence）
// + 父块钻取句柄。两档行粒度与排序语义一致，仅投影不同。
type SearchResultDetail string

const (
	SearchDetailFull SearchResultDetail = "full"
	SearchDetailLean SearchResultDetail = "lean"
)

// NormalizeSearchResultDetail 把空值归一为 full（协议面默认档位）。
func NormalizeSearchResultDetail(detail SearchResultDetail) SearchResultDetail {
	if detail == "" {
		return SearchDetailFull
	}
	return detail
}

// ValidateSearchResultDetail 校验档位取值合法。
func ValidateSearchResultDetail(detail SearchResultDetail) error {
	switch NormalizeSearchResultDetail(detail) {
	case SearchDetailFull, SearchDetailLean:
		return nil
	default:
		return fmt.Errorf("%w: detail 只支持 full/lean，当前 %q", domainerrors.ErrValidation, string(detail))
	}
}
