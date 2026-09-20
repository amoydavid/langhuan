package web

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件实现 web connector 的 ListTree（spec §5/§3.3/§3.4）：三种模式的发现分派、
// 目录投影（URL path 段 → folder/page ExternalNode）与 TreeSnapshot 组装。

// ListTree 列举 web 源同步根下的整棵页面树，返回「该源期望 URL 全集」的快照。
//
//   - root.Kind 必须为 SyncRootWeb；Token 为 seed_url（crawl）/ sitemap_url（sitemap）/
//     空串（urls），实际发现参数以解析后的 SourceConfig 为准（T8 由 config 推导 root）。
//   - Complete 语义（spec §3.4）：crawl 达到 max_pages 截断或失败越阈值 → false；
//     sitemap 子树缺失 → false（主文件失败直接返回 error）；urls 恒 true。
//   - MaxEditTime 取 sitemap lastmod 最大值；crawl/urls 无 lastmod 时为零值。
//   - 致命错误（种子/主文件抓取失败、配置非法、引擎未实现、ctx 中断）返回 error 且
//     不返回可应用快照——调用方绝不能把「空树」当完整快照参与删除检测。
func (c *Connector) ListTree(ctx context.Context, _ model.SourceConnection, root model.SyncRoot, options sourceport.ListOptions) (sourceport.TreeSnapshot, error) {
	if root.Kind != sourceport.SyncRootWeb {
		return sourceport.TreeSnapshot{}, fmt.Errorf("不支持的 sync root kind: %s", root.Kind)
	}
	// ListTree 是 run 的入口：替换同配置旧会话，本次发现与后续 Fetch 共用新缓存。
	session, err := c.sessionFor(options.SourceConfig, true)
	if err != nil {
		return sourceport.TreeSnapshot{}, err
	}
	result, err := session.discover(ctx)
	if err != nil {
		return sourceport.TreeSnapshot{}, err
	}
	nodes := projectDirectoryTree(result.Pages)
	return sourceport.TreeSnapshot{
		Nodes:       nodes,
		Complete:    result.Complete,
		Warnings:    result.Warnings,
		MaxEditTime: maxNodeEditTime(nodes),
	}, nil
}

// discover 按模式分派到发现层（crawl/sitemap/urls 实现见 discover_*.go）。
//
// 三种模式统一：
//   - robots 门（respect_robots=true 时）——礼貌性参数对三模式统一生效（spec §4.1）；
//   - 始终显式构造带 SeedHost 的 URLFilter——T6 评审移交：零值 Filter（SeedHost 空）
//     不做 host 限制，会让 sitemap/crawl 接受跨 host URL；
//
// 已知保守偏差（T6 评审移交，方向安全、维持现状）：crawl 截断判定会把 robots-Disallow
// 留在队列里的 URL 记为截断；skipped_non_html 不重置连续失败计数。两者都只会让
// Complete 偏向 false（宁可误伤不漏删）。
func (s *runSession) discover(ctx context.Context) (DiscoveryResult, error) {
	switch s.cfg.Mode {
	case value.WebSourceModeCrawl:
		filter, err := NewURLFilter(s.cfg.Crawl) // SeedHost 来自 crawl.seed_url
		if err != nil {
			return DiscoveryResult{}, err
		}
		return DiscoverCrawl(ctx, s.cfg.Crawl.SeedURL,
			CrawlScope{MaxDepth: s.cfg.Crawl.MaxDepth, MaxPages: s.cfg.Crawl.MaxPages},
			DiscoveryOptions{Filter: filter, Robots: s.robotsGate(), UserAgent: s.userAgent},
			pageFetcherFunc(s.fetchThroughCache))
	case value.WebSourceModeSitemap:
		filter, err := sitemapFilter(s.cfg)
		if err != nil {
			return DiscoveryResult{}, err
		}
		return DiscoverSitemap(ctx, s.client, s.cfg.Sitemap.URL,
			DiscoveryOptions{Filter: filter, Robots: s.robotsGate(), UserAgent: s.userAgent})
	case value.WebSourceModeURLs:
		// 列表即事实源（spec §3.4/§5.3）：不做 filter/robots，Complete 恒 true；
		// DiscoverURLs 的规范化复核失败记 invalid_url warning 而非丢弃整树。
		urls, warnings := DiscoverURLs(s.cfg.URLs)
		pages := make([]model.ExternalNode, 0, len(urls))
		for _, pageURL := range urls {
			pages = append(pages, pageNode(pageURL, time.Time{}))
		}
		return DiscoveryResult{Pages: pages, Complete: true, Warnings: warnings}, nil
	default:
		return DiscoveryResult{}, fmt.Errorf("未知 web 源 mode: %s", s.cfg.Mode)
	}
}

// robotsGate 返回 robots 检查门；respect_robots=false 时返回 nil 接口
// （发现层以 opts.Robots != nil 判定跳过）。
func (s *runSession) robotsGate() RobotsGate {
	if s.robots == nil {
		return nil
	}
	return s.robots
}

// sitemapFilter 构造 sitemap 模式的过滤器：host 边界取 sitemap URL 自身
// （crawl.seed_url 在该模式不存在）；include/exclude/subdomain 与 crawl 共用同一
// 参数集（spec §4.1：crawl.* 是「发现+抓取」默认参数集，不做第二份配置）。
func sitemapFilter(cfg value.WebSourceConfig) (URLFilter, error) {
	if cfg.Sitemap == nil {
		// 解析器保证 sitemap 模式必有 sitemap.url，此处防御运行期 panic。
		return URLFilter{}, fmt.Errorf("mode=sitemap 时缺少 sitemap.url")
	}
	parsed, ok := resolveToURL(nil, cfg.Sitemap.URL)
	if !ok {
		return URLFilter{}, fmt.Errorf("sitemap.url 不是合法的绝对 HTTP(S) URL: %q", cfg.Sitemap.URL)
	}
	return URLFilter{
		SeedHost:        parsed.Hostname(),
		AllowSubdomains: cfg.Crawl.AllowSubdomains,
		IncludePaths:    cfg.Crawl.IncludePaths,
		ExcludePaths:    cfg.Crawl.ExcludePaths,
	}, nil
}

// pageFetcherFunc 让会话的缓存包装抓取函数满足发现层的 PageFetcher 接口。
type pageFetcherFunc func(ctx context.Context, request FetchRequest) (fetchedPage, error)

func (f pageFetcherFunc) Fetch(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	return f(ctx, request)
}

// fetchThroughCache 把页缓存夹在引擎抓取器中间（spec §5.1 发现-抓取复用）：
// 发现阶段抓过的页留在缓存，同一 run 的 Fetch 阶段直接复用，避免双倍流量。
// robots 检查不在这里做——发现层在发起抓取前已逐 URL 检查。
func (s *runSession) fetchThroughCache(ctx context.Context, request FetchRequest) (fetchedPage, error) {
	if page, ok := s.cache.Get(request.URL); ok {
		return page, nil
	}
	page, err := s.fetchEngine(ctx, request)
	if err == nil && !page.NotModified {
		s.cache.Put(request.URL, page)
	}
	return page, err
}

// projectDirectoryTree 把发现层的页面节点投影为「目录 + 页面」树（spec §3.3）：
//   - 页面按 URL path 段逐级产生 folder 节点（ObjType=folder、HasDocument=false、
//     Token=规范化目录 URL，path 带结尾 "/"）；同一目录多个页面共享一个 folder；
//   - 页面 ParentToken 指向所属目录；a/b/c/page 产生 a/、a/b/、a/b/c/ 三级目录；
//   - 根目录（/）不产生 folder：根页面（含 /a/ 这类目录索引页）直接挂根，
//     ParentToken 为空串——对齐 feishu connector「根节点 ParentToken 为空」的语义；
//   - 与页面 Token 冲突的目录不产生 folder（见 pageTokens 注释），子页 ParentToken
//     解析落到目录索引页的文件节点上——目录索引页即该目录的代表文档；
//   - 节点顺序为确定性父先序（见排序处注释）：父节点无条件先于子节点，
//     编排层可按快照顺序逐个 upsert/建档，不依赖发现顺序。
func projectDirectoryTree(pages []model.ExternalNode) []model.ExternalNode {
	// 目录索引页（如 .../help/）与 folderToken 产出的目录 Token 完全相同——
	// 若两者都进快照，编排层会用同一 external_id 建 FileTreeNode，folder upsert 与
	// 页面建档撞唯一索引（评审缺陷 F1）。冲突目录跳过 folder 节点后，其子页的
	// ParentToken 仍指向该 Token，解析到目录索引页的文件节点（嵌套链上层的
	// folder 若同样冲突则一并跳过，不影响更深层 folder 的挂载语义）。
	//
	// 编排层义务（T8）：ParentToken 可能指向页面节点（目录索引页）——定位父子
	// 关系时必须把 folder 与 page 两类 Token 同时注册进父子映射，只注册 folder
	// 会让「父为索引页的 folder 下的子页」（如 /help/ 是索引页而 /help/api/ 不是
	// 时的 help/api/*）必然定位失败。
	pageTokens := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		pageTokens[page.Token] = struct{}{}
	}

	nodes := make([]model.ExternalNode, 0, len(pages)*2)
	emitted := make(map[string]struct{}, len(pages))
	for _, page := range pages {
		chain := folderChain(page.Token)
		for _, folder := range chain {
			if _, isPage := pageTokens[folder.Token]; isPage {
				continue
			}
			if _, seen := emitted[folder.Token]; seen {
				continue
			}
			emitted[folder.Token] = struct{}{}
			nodes = append(nodes, folder)
		}
		page.ParentToken = pageParentToken(chain)
		nodes = append(nodes, page)
	}

	// 确定性父先序（评审缺陷 F1 后续）：发现层的产出顺序任意（sitemap 条目序、
	// urls 列表序都可能子页在前、父索引页在后），若按发射顺序输出，父索引页排在
	// 子页之后会让按序消费快照的编排层在子页建档时定位不到父——首次同步确定性
	// partial。按（path 段数升序, Token 字典序）排序：严格祖先的 path 段数必然
	// 少于后代，「父先于子」由段数比较无条件保证，字典序仅打破无祖先关系的并列。
	sort.Slice(nodes, func(i, j int) bool {
		segmentI, segmentJ := pathSegmentCount(nodes[i].Token), pathSegmentCount(nodes[j].Token)
		if segmentI != segmentJ {
			return segmentI < segmentJ
		}
		return nodes[i].Token < nodes[j].Token
	})
	return nodes
}

// pathSegmentCount 统计节点 Token 的 URL path 段数（根为 0；目录 Token 的结尾 "/"
// 不计空段）。
func pathSegmentCount(token string) int {
	parsed, err := url.Parse(token)
	if err != nil {
		return 0 // Token 来自规范化 URL，必可解析；防御性兜底为最浅层
	}
	trimmed := strings.Trim(parsed.Path, "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

// folderChain 返回页面所属的目录祖先链（根 → 直接父目录）；页面位于根目录时为空。
// 页面 Token 来自规范化 URL，必可解析；解析失败按「无目录」兜底（不丢页面）。
func folderChain(pageToken string) []model.ExternalNode {
	parsed, err := url.Parse(pageToken)
	if err != nil {
		return nil
	}
	// 去结尾 "/" 得到「文件路径」：/help/ 视为根目录下的索引文档 help，
	// 其父目录是根；/help/getting-started 的父目录是 /help/。
	filePath := strings.TrimSuffix(parsed.Path, "/")
	slash := strings.LastIndexByte(filePath, '/')
	if slash <= 0 {
		return nil
	}
	dirPath := filePath[:slash]
	segments := strings.Split(strings.TrimPrefix(dirPath, "/"), "/")
	chain := make([]model.ExternalNode, 0, len(segments))
	parent := ""
	for i, segment := range segments {
		token := folderToken(parsed, "/"+strings.Join(segments[:i+1], "/"))
		chain = append(chain, model.ExternalNode{
			Token:       token,
			ParentToken: parent,
			Title:       segment,
			ObjType:     ObjTypeFolder,
			HasDocument: false,
		})
		parent = token
	}
	return chain
}

// pageParentToken 返回页面挂载点：所属目录 Token；根目录页面为空串。
func pageParentToken(chain []model.ExternalNode) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1].Token
}

// folderToken 构造规范化目录 URL：复用页面的 scheme/host（已规范化），path 补结尾
// "/"（与 NormalizeWebSourceURI 的空 path 补 "/" 行为协调）；目录不继承页面的 query。
func folderToken(page *url.URL, dirPath string) string {
	folder := *page
	folder.Path = dirPath + "/"
	folder.RawPath = ""
	folder.RawQuery = ""
	return folder.String()
}

// maxNodeEditTime 返回快照内非零 EditTime 的最大值（sitemap lastmod；全缺失为零值）。
func maxNodeEditTime(nodes []model.ExternalNode) time.Time {
	var max time.Time
	for i := range nodes {
		editTime := nodes[i].EditTime
		if editTime.IsZero() {
			continue
		}
		if max.IsZero() || editTime.After(max) {
			max = editTime
		}
	}
	return max
}
