package web

// WarningInvalidURL 表示配置中的 URL 项非法（非绝对 HTTP(S)、含凭证等），已被丢弃。
const WarningInvalidURL = "invalid_url"

// DiscoverURLs 返回 urls 模式待抓取的页面列表（spec §5.3）。
//
// 列表来自 config.urls[]：合法性与规范化在 API 写入边界完成（value.NormalizeWebSourceURI），
// ListTree 直接透传，不做额外解释。这里仍做一次幂等规范化复核与去重（保留写入顺序）：
//   - 复核是幂等的（已规范化的 URL 输出与输入相同），不改变正常列表；
//   - 漏网的脏数据（相对 URL、非 http(s) scheme、带凭证 URL）被丢弃并记
//     warning invalid_url，而不是变成抓取目标。
func DiscoverURLs(rawURLs []string) ([]string, []string) {
	urls := make([]string, 0, len(rawURLs))
	var warnings []string
	seen := make(map[string]struct{}, len(rawURLs))
	for _, raw := range rawURLs {
		normalized, ok := NormalizeURL(raw)
		if !ok {
			warnings = append(warnings, WarningInvalidURL)
			continue
		}
		if _, duplicated := seen[normalized]; duplicated {
			continue
		}
		seen[normalized] = struct{}{}
		urls = append(urls, normalized)
	}
	return urls, warnings
}
