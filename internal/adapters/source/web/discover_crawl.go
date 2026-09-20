package web

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// crawl 模式发现（spec §5.1）：种子 URL 起受限 BFS。
//
// 语义要点：
//   - frontier 为 FIFO 队列，按规范化 URL 去重；depth(seed)=0，仅接受
//     depth <= max_depth 的出链；单协程顺序推进（跨 host 串行，spec §14 取舍 8）；
//   - 页面请求走与 Fetch 相同的抓取管线（PageFetcher，生产为 HTTPFetcher、
//     可把 PageCache 夹在中间），出链经 ResolveLinks 做 resolve/去 fragment/
//     规范化/过滤（以重定向后的 FinalURL 为解析 base）；
//   - robots：每个 URL 抓取前检查；Disallow 不进 Nodes、不计数、记 warning；
//   - 单页抓取失败：该页不进 Nodes；连续失败达到阈值或失败率超阈值时置
//     Complete=false（可能漏删由此兜底）；内容类型跳过（skipped_non_html）是
//     「已知无文档」，不算失败；
//   - 达到 max_pages 截断时 Complete=false + WarningTruncated。
const (
	// crawlConsecutiveFailureThreshold 是连续抓取失败阈值（spec §5.1 具名常量，
	// 默认 3）：达到即视为站点级异常。边界口径取计划 T6 的「连续失败≥3」
	// ——第 3 次连续失败触发；方向保守（更早封住删除判定），宁可误伤不漏删。
	crawlConsecutiveFailureThreshold = 3
	// crawlFailureRatePercentThreshold 是抓取失败率阈值（百分比，严格大于才触发）。
	crawlFailureRatePercentThreshold = 20
	// crawlFailureRateScale 把失败率换算为整数百比运算（failures*scale/fetched）。
	crawlFailureRateScale = 100
)

// CrawlScope 限定 BFS 的规模边界（value.WebSourceCrawlConfig 的运行期子集，
// 由 T7 从解析后的源配置取值注入）。
type CrawlScope struct {
	// MaxDepth 是出链深度上限：depth(seed)=0，仅接受 depth <= MaxDepth 的出链。
	MaxDepth int
	// MaxPages 是进入 Nodes 的页面数上限（截断语义见 spec §3.4）。必须为正。
	MaxPages int
}

// PageFetcher 是发现阶段的单页抓取入口（spec §5.1：发现走与 Fetch 相同的抓取管线）。
// 方法签名与 *HTTPFetcher.Fetch 一致（可直接充当实现）；T7 把 PageCache 夹在
// 该实现的中间（命中缓存不产生真实请求），测试注入 fake。
type PageFetcher interface {
	Fetch(ctx context.Context, request FetchRequest) (fetchedPage, error)
}

var _ PageFetcher = (*HTTPFetcher)(nil)

// DiscoverCrawl 从 seedURL 起受限 BFS 发现页面节点（spec §5.1）。
//
// 返回 error 仅表示整个 run 无法成立：种子非法/抓取失败（无任何边界知识，
// 不能以「完整空树」参与 diff）、参数装配错误或 ctx 中断（经 robots/抓取透传）。
func DiscoverCrawl(ctx context.Context, seedURL string, scope CrawlScope, opts DiscoveryOptions, fetcher PageFetcher) (DiscoveryResult, error) {
	seed, ok := NormalizeURL(seedURL)
	if !ok {
		return DiscoveryResult{}, fmt.Errorf("crawl.seed_url 不是合法的绝对 HTTP(S) URL: %q", seedURL)
	}
	if scope.MaxDepth < 0 {
		return DiscoveryResult{}, fmt.Errorf("crawl.max_depth 不能为负: %d", scope.MaxDepth)
	}
	if scope.MaxPages <= 0 {
		return DiscoveryResult{}, fmt.Errorf("crawl.max_pages 必须为正: %d", scope.MaxPages)
	}
	if fetcher == nil {
		return DiscoveryResult{}, fmt.Errorf("crawl 发现必须注入页面抓取器")
	}

	result := DiscoveryResult{Complete: true}
	type frontierItem struct {
		url   string
		depth int
	}
	seen := map[string]struct{}{seed: {}}
	queue := []frontierItem{{url: seed, depth: 0}}
	var (
		// fetched 是实际发起的抓取次数（成功+失败，不含 robots 跳过）：
		// 失败率的分母与分子都只统计真实抓取。
		fetched     int
		failures    int
		consecutive int
	)

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if opts.Robots != nil {
			allowed, warning, err := opts.Robots.Allowed(ctx, item.url)
			if err != nil {
				return DiscoveryResult{}, fmt.Errorf("robots 校验中断: %w", err)
			}
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			if !allowed {
				if item.depth == 0 {
					// 种子被拒 = BFS 无法开始，快照没有任何边界知识；
					// 绝不能以「完整空树」参与 diff（会误删整源页面）。
					result.Complete = false
				}
				continue
			}
		}

		page, err := fetcher.Fetch(ctx, FetchRequest{URL: item.url})
		if err != nil {
			if item.depth == 0 {
				// 种子抓取失败 = 无任何边界知识，与 sitemap 主文件失败同理：
				// 作为 error 失败整个 run，而不是产出「完整空树」。
				return DiscoveryResult{}, fmt.Errorf("crawl 种子抓取失败: %w", err)
			}
			// 结果分类字面量即告警码（"gone"/"blocked"/"retryable"/…，与
			// WarningSkippedNonHTML = string(OutcomeSkippedNonHTML) 同一词汇表）。
			outcome := ClassifyError(err)
			result.Warnings = append(result.Warnings, string(outcome))
			if outcome == OutcomeSkippedNonHTML {
				continue // 内容类型跳过：已知无文档，不算抓取失败（spec §6.5）
			}
			fetched++
			failures++
			consecutive++
			if consecutive >= crawlConsecutiveFailureThreshold ||
				failures*crawlFailureRateScale > fetched*crawlFailureRatePercentThreshold {
				result.Complete = false
			}
			continue
		}

		fetched++
		consecutive = 0
		result.Pages = append(result.Pages, pageNode(item.url, time.Time{}))

		// 出链入队（受深度边界收口；规范化去重保证同 URL 全局只抓一次）。
		// 相对链接相对重定向后的最终地址成立，故以 FinalURL 为解析 base。
		if item.depth+1 <= scope.MaxDepth && len(page.Links) > 0 {
			base := crawlLinkBase(item.url, page.FinalURL)
			for _, link := range ResolveLinks(base, page.Links, opts.Filter) {
				if _, duplicated := seen[link]; duplicated {
					continue
				}
				seen[link] = struct{}{}
				queue = append(queue, frontierItem{url: link, depth: item.depth + 1})
			}
		}

		// 截断语义：达到上限且仍有未探明边界（队列非空）才算截断——
		// 恰好抓满不冤枉快照的完整性。
		if len(result.Pages) >= scope.MaxPages && len(queue) > 0 {
			result.Complete = false
			result.Warnings = append(result.Warnings, WarningTruncated)
			break
		}
	}
	return result, nil
}

// crawlLinkBase 返回出链解析的 base：重定向后的最终 URL 优先，
// 解析失败回退为请求 URL（规范化 URL 必可解析）。
func crawlLinkBase(requestURL, finalURL string) *url.URL {
	if finalURL != "" {
		if parsed, ok := resolveToURL(nil, finalURL); ok {
			return parsed
		}
	}
	parsed, _ := resolveToURL(nil, requestURL)
	return parsed
}
