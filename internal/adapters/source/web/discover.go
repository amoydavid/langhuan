package web

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
)

// 本文件承载发现层（spec §5）的共享合同：节点对象类型词汇、发现告警码、
// robots 检查门、共用参数与产出形状。三种模式的实现分布：
// discover_crawl.go（受限 BFS）、discover_sitemap.go（sitemap.xml）、
// discover_urls.go（显式 URL 列表）；目录投影（folder 节点与 ParentToken）与
// TreeSnapshot 组装由 T7 连接器完成。

// web 源节点的 ObjType 取值（spec §3.3）。
const (
	// ObjTypePage 是页面节点：HasDocument=true，可同步为 web Document。
	ObjTypePage = "page"
	// ObjTypeFolder 是目录节点（由 URL path 段投影，T7 构造）：HasDocument=false。
	ObjTypeFolder = "folder"
)

// 发现层告警码（写入 TreeSnapshot.Warnings / Job 结果）。与 feishu walker 及
// discover_urls.go 口径一致：每个 occurrence 逐条追加，不去重。
const (
	// WarningDuplicateURL 表示同一规范化 URL 重复出现，仅保留首次（spec §3.4）。
	WarningDuplicateURL = "duplicate_url"
	// WarningInvalidLastmod 表示 sitemap <lastmod> 无法按 RFC 3339 / W3C date 解析，已忽略该字段。
	WarningInvalidLastmod = "invalid_lastmod"
	// WarningSitemapSubtreeFailed 表示某个子 sitemap 拉取/解析失败、子项非法或超出
	// 递归深度上限，该子树缺失（spec §5.2：Complete=false + warning）。
	WarningSitemapSubtreeFailed = "sitemap_subtree_failed"
	// WarningTruncated 表示 crawl 达到 max_pages 截断（spec §3.4：truncated_at=max_pages）。
	WarningTruncated = "truncated_at=max_pages"
)

// RobotsGate 抽象 robots.txt 检查（spec §6.3）。*RobotsChecker 满足该接口；
// 测试可注入 fake。nil（DiscoveryOptions.Robots）表示 respect_robots=false，跳过检查。
type RobotsGate interface {
	Allowed(ctx context.Context, target string) (allowed bool, warning string, err error)
}

var _ RobotsGate = (*RobotsChecker)(nil)

// DiscoveryOptions 是 crawl/sitemap 发现共用的过滤与合规参数。
type DiscoveryOptions struct {
	// Filter 是候选 URL 的过滤规则（include/exclude/subdomain/scheme）。
	// crawl 模式由 NewURLFilter(crawl 配置) 构造；sitemap/urls 模式 SeedHost
	// 为空表示不限 host（见 URLFilter 注释）。被 filter 排除是配置性排除，
	// 静默跳过、不告警（与 crawl 出链处理一致）。
	Filter URLFilter
	// Robots 非 nil 时，每个候选 URL 在进入 Nodes 前做 robots 检查；
	// Disallow 的 URL 不进 Nodes 并记 warning（spec §5.1/§5.2）。
	Robots RobotsGate
	// UserAgent 用于 sitemap 文档自身的拉取；页面抓取的 UA 由 PageFetcher 自持。
	UserAgent string
}

// DiscoveryResult 是发现层的产出（spec §3.4）：页面节点 + 完整性标记 + 告警。
// T7 连接器据此外推目录投影（URL path 段 → folder 节点、ParentToken 指向所属目录）
// 并组装 sourceport.TreeSnapshot（MaxEditTime 取 Pages 中最大 EditTime）。
type DiscoveryResult struct {
	// Pages 是发现的页面节点：ObjType=ObjTypePage、HasDocument=true、
	// Token=规范化 URL、Title=URL 末段（Fetch 后由既有标题更新路径覆盖）、
	// EditTime=sitemap lastmod（缺失/解析失败为零值）。ParentToken 留空。
	Pages []model.ExternalNode
	// Complete 表示本次发现是否构成「该源期望 URL 全集」的完整边界：
	// false 时调用方（diff）不得做删除判定。截断、子树缺失、抓取失败越阈值时为 false。
	Complete bool
	// Warnings 是非致命告警码（每 occurrence 一条，顺序为发现顺序）。
	Warnings []string
}

// pageNode 构造页面节点（ParentToken 留空，目录投影属 T7）。
func pageNode(token string, editTime time.Time) model.ExternalNode {
	return model.ExternalNode{
		Token:       token,
		Title:       urlLastSegmentTitle(token),
		ObjType:     ObjTypePage,
		HasDocument: true,
		EditTime:    editTime,
	}
}

// urlLastSegmentTitle 取 URL path 的最后一个非空段作为节点标题（spec §3.3：
// ListTree 阶段未必有正文标题，先用 URL 末段）；根路径回退为 hostname。
func urlLastSegmentTitle(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	segments := strings.Split(parsed.Path, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if segment := strings.TrimSpace(segments[i]); segment != "" {
			return segment
		}
	}
	return parsed.Hostname()
}
