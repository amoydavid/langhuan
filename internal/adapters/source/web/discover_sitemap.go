package web

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sitemap 模式发现（spec §5.2）：解析 sitemap.xml（含 sitemapindex 递归、
// <urlset>/<sitemapindex>、<loc>/<lastmod>），产出页面节点列表。
//
// 语义要点：
//   - 主文件拉取/解析失败 → 返回 error（整个 run 失败）；子 sitemap 失败 →
//     该子树缺失 → Complete=false + warning（不中断其余子树）；
//   - sitemap 仅做发现，不取页面正文（条件请求与 hash 在 Fetch 阶段生效）；
//   - 页面条目依次过：规范化 → 去重 → URLFilter → robots → lastmod 解析；
//   - 子 sitemap 文档不做 URLFilter/robots 检查：它是发现用的导航元数据而非内容，
//     spec §5.2 的过滤与 robots 要求针对「sitemap 发现的 URL」（页面条目）；
//   - lastmod 解析支持 RFC 3339 与 W3C date 精度谱系，解析失败忽略该字段 + warning，
//     缺失为零值（无告警），时区按声明解析、无时区按 UTC。
const (
	// maxSitemapRecursionDepth 是 sitemapindex 递归深度上限（spec §5.2 具名常量 5）：
	// 主文件记深度 0，其 <sitemap> 子项依次 +1，深度超过上限的子树按缺失处理。
	maxSitemapRecursionDepth = 5
	// maxSitemapBytes 是单个 sitemap 文档的读取上限（sitemap 协议对未压缩文件的
	// 50MiB 上限），防止异常大文件占满内存。
	maxSitemapBytes = 50 << 20
)

// DiscoverSitemap 从 sitemapURL 起发现页面节点（spec §5.2）。
//
// client 必须注入 SSRF-safe HTTP client（生产为 httpclient.NewPublicWebClient 的产物）；
// sitemap 文档自身的拉取走该 client，页面正文不在本函数内抓取。
// UserAgent 经 opts 注入，缺省不发 UA 头。
func DiscoverSitemap(ctx context.Context, client *http.Client, sitemapURL string, opts DiscoveryOptions) (DiscoveryResult, error) {
	if client == nil {
		return DiscoveryResult{}, fmt.Errorf("sitemap 发现必须注入 HTTP client")
	}

	result := DiscoveryResult{Complete: true}
	pageTokens := make(map[string]struct{})
	sitemapTokens := make(map[string]struct{})

	// sitemap 文档队列（FIFO，天然按深度推进）：主文件失败是 error，
	// 子文件失败是 Complete=false + warning，故以 depth==0 区分主/子。
	type sitemapDoc struct {
		url   string
		depth int
	}
	queue := []sitemapDoc{{url: sitemapURL, depth: 0}}

	for len(queue) > 0 {
		doc := queue[0]
		queue = queue[1:]
		isMain := doc.depth == 0

		// 同一文档（含 index 互相引用成环）只展开一次；主文件首次入队不触发。
		if _, seen := sitemapTokens[doc.url]; seen {
			if !isMain {
				result.Warnings = append(result.Warnings, WarningDuplicateURL)
			}
			continue
		}
		sitemapTokens[doc.url] = struct{}{}

		if doc.depth > maxSitemapRecursionDepth {
			result.Complete = false
			result.Warnings = append(result.Warnings, WarningSitemapSubtreeFailed)
			continue
		}

		body, err := fetchSitemapDocument(ctx, client, opts.UserAgent, doc.url)
		if err != nil {
			if isMain {
				return DiscoveryResult{}, fmt.Errorf("拉取 sitemap 失败: %w", err)
			}
			result.Complete = false
			result.Warnings = append(result.Warnings, WarningSitemapSubtreeFailed)
			continue
		}

		isIndex, refs, urls, err := parseSitemapDocument(body)
		if err != nil {
			if isMain {
				return DiscoveryResult{}, fmt.Errorf("解析 sitemap 失败: %w", err)
			}
			result.Complete = false
			result.Warnings = append(result.Warnings, WarningSitemapSubtreeFailed)
			continue
		}

		// 文档自身 URL 已规范化、必可解析，作为相对 loc 的解析 base
		//（协议要求 loc 为绝对地址，这里对相对写法按所在文档解析，宽容不放宽身份口径）。
		base, _ := resolveToURL(nil, doc.url)
		if isIndex {
			for _, ref := range refs {
				loc := strings.TrimSpace(ref.Loc)
				// 空 loc 无法定位子文档，同样构成子树不可达。
				childURL, ok := ResolveURL(base, loc)
				if loc == "" || !ok {
					result.Complete = false
					result.Warnings = append(result.Warnings, WarningSitemapSubtreeFailed)
					continue
				}
				queue = append(queue, sitemapDoc{url: childURL, depth: doc.depth + 1})
			}
			continue
		}

		for _, entry := range urls {
			if err := result.appendSitemapEntry(ctx, base, entry, opts, pageTokens); err != nil {
				return DiscoveryResult{}, err
			}
		}
	}
	return result, nil
}

// appendSitemapEntry 处理单个 <url> 条目：规范化 → 去重 → filter → robots → lastmod
// → 页面节点；ctx 中断（robots 校验错误）经 error 返回并放弃整个发现。
func (r *DiscoveryResult) appendSitemapEntry(ctx context.Context, base *url.URL, entry sitemapURLEntry, opts DiscoveryOptions, pageTokens map[string]struct{}) error {
	loc := strings.TrimSpace(entry.Loc)
	if loc == "" {
		// 空 loc 解析结果会退化为本文档自身，必须显式按脏数据丢弃。
		r.Warnings = append(r.Warnings, WarningInvalidURL)
		return nil
	}
	parsed, ok := resolveToURL(base, loc)
	if !ok {
		r.Warnings = append(r.Warnings, WarningInvalidURL)
		return nil
	}
	normalized := parsed.String()
	if _, duplicated := pageTokens[normalized]; duplicated {
		r.Warnings = append(r.Warnings, WarningDuplicateURL)
		return nil
	}
	pageTokens[normalized] = struct{}{}

	if !opts.Filter.AllowURL(parsed) {
		return nil
	}

	if opts.Robots != nil {
		allowed, warning, err := opts.Robots.Allowed(ctx, normalized)
		if err != nil {
			return fmt.Errorf("robots 校验中断: %w", err)
		}
		if warning != "" {
			r.Warnings = append(r.Warnings, warning)
		}
		if !allowed {
			return nil
		}
	}

	var editTime time.Time
	if raw := strings.TrimSpace(entry.Lastmod); raw != "" {
		parsedTime, ok := parseSitemapLastmod(raw)
		if !ok {
			r.Warnings = append(r.Warnings, WarningInvalidLastmod)
		} else {
			editTime = parsedTime
		}
	}

	r.Pages = append(r.Pages, pageNode(normalized, editTime))
	return nil
}

// sitemapDocument 是 sitemap XML 的探测形态：以根元素名区分 urlset/sitemapindex。
// encoding/xml 的字段匹配只看本地名，带默认命名空间的文档同样命中。
type sitemapDocument struct {
	XMLName  xml.Name
	Sitemaps []sitemapRef      `xml:"sitemap"`
	URLs     []sitemapURLEntry `xml:"url"`
}

// sitemapRef 是 sitemapindex 的 <sitemap> 子项。
type sitemapRef struct {
	Loc string `xml:"loc"`
}

// sitemapURLEntry 是 urlset 的 <url> 子项。
type sitemapURLEntry struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod"`
}

// parseSitemapDocument 解析并判定文档类型；根元素既非 urlset 也非 sitemapindex
// 视为解析残缺（spec §3.4：主文件 → error，子文件 → Complete=false）。
func parseSitemapDocument(body []byte) (isIndex bool, refs []sitemapRef, urls []sitemapURLEntry, err error) {
	var doc sitemapDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return false, nil, nil, fmt.Errorf("解析 sitemap XML 失败: %w", err)
	}
	switch doc.XMLName.Local {
	case "sitemapindex":
		return true, doc.Sitemaps, nil, nil
	case "urlset":
		return false, nil, doc.URLs, nil
	default:
		return false, nil, nil, fmt.Errorf("未知 sitemap 根元素 %q", doc.XMLName.Local)
	}
}

// sitemapLastmodLayouts 是 <lastmod> 的解析精度谱系（W3C date / RFC 3339），
// 按精度从高到低尝试；无时区声明按 UTC（time.Parse 缺省行为）。
var sitemapLastmodLayouts = []string{
	time.RFC3339, // 2006-01-02T15:04:05Z07:00
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04",
	"2006-01-02",
}

// parseSitemapLastmod 解析 <lastmod>；失败返回 ok=false（调用方忽略该字段 + warning）。
func parseSitemapLastmod(raw string) (time.Time, bool) {
	for _, layout := range sitemapLastmodLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// fetchSitemapDocument 拉取单个 sitemap 文档：仅 2xx 视为成功，
// 响应体受 maxSitemapBytes 上限约束。
func fetchSitemapDocument(ctx context.Context, client *http.Client, userAgent, docURL string) ([]byte, error) {
	parsed, ok := resolveToURL(nil, docURL)
	if !ok {
		return nil, fmt.Errorf("sitemap 地址不是合法的绝对 HTTP(S) URL: %q", docURL)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构造 sitemap 请求失败: %w", err)
	}
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	request.Header.Set("Accept", "application/xml,text/xml;q=0.9,*/*;q=0.5")

	response, err := client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("拉取 sitemap 被中断: %w", ctxErr)
		}
		return nil, fmt.Errorf("sitemap 请求失败: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseDrainBytes))
		_ = response.Body.Close()
	}()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("sitemap 响应状态 %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSitemapBytes+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("读取 sitemap 被中断: %w", ctxErr)
		}
		return nil, fmt.Errorf("读取 sitemap 失败: %w", err)
	}
	if int64(len(body)) > maxSitemapBytes {
		return nil, fmt.Errorf("sitemap 超过 %d 字节上限", maxSitemapBytes)
	}
	return body, nil
}
