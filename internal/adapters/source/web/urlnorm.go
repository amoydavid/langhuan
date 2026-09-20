// Package web 实现网站内容源的抓取与抽取构建块（spec §6–§7）：
// URL 规范化与出链过滤、robots.txt 遵守、本次运行的页缓存、http 引擎单页抓取、
// 正文抽取与 JS 空壳判定。发现层（crawl BFS/sitemap，discover_*.go）与 SourceConnector
// 总装（connector*.go：三模式 ListTree、目录投影、Fetch 缓存与抽取组装）在此包内
// 组合这些构建块；高级引擎（render/firecrawl/jina）经 engineFor 扩展点后续接入。
//
// 适配器不持有数据库与全局状态：所有外部依赖（HTTP client、UA、超时、延迟、上限）
// 由调用方经构造函数注入；HTTP client 必须是 internal/adapters/httpclient 的
// SSRF-safe client（spec §6.4），本包不自行放宽 URL/host 校验。
package web

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/dajee/langhuan/internal/domain/value"
)

// URLFilter 是出链过滤规则（spec §5.1）：
//   - IncludePaths 为空表示不限制；ExcludePaths 命中优先于 IncludePaths；
//   - path glob 以 "/" 分段匹配：`*` 匹配段内任意字符（不跨段），`**` 匹配零到多段；
//   - AllowSubdomains=false 时只接受与 SeedHost 完全相同的 hostname，
//     为 true 时额外接受 `*.SeedHost` 形态的子域（不匹配 `example.com.evil.net` 这类后缀伪装）；
//   - SeedHost 为空表示不做 host 限制（sitemap/urls 模式复用同一过滤器）；
//   - 只按 path 过滤：query 与 fragment 不参与匹配（fragment 已在规范化时剥离），
//     端口不参与 host 比较（同一 hostname 的不同端口视为同一 host）。
type URLFilter struct {
	SeedHost        string
	AllowSubdomains bool
	IncludePaths    []string
	ExcludePaths    []string
}

// NewURLFilter 用 crawl 配置构造出链过滤器：种子 URL 提供 host 边界。
// 种子 URL 必须是合法的绝对 HTTP(S) URL（写入边界已规范化，这里防御性复核）。
func NewURLFilter(crawl value.WebSourceCrawlConfig) (URLFilter, error) {
	seed, ok := resolveToURL(nil, crawl.SeedURL)
	if !ok {
		return URLFilter{}, fmt.Errorf("crawl.seed_url 不是合法的绝对 HTTP(S) URL: %q", crawl.SeedURL)
	}
	return URLFilter{
		SeedHost:        seed.Hostname(),
		AllowSubdomains: crawl.AllowSubdomains,
		IncludePaths:    crawl.IncludePaths,
		ExcludePaths:    crawl.ExcludePaths,
	}, nil
}

// Allow 报告原始 URL 是否允许抓取（相对、非 http(s)、带凭证一律拒绝）。
func (f URLFilter) Allow(rawURL string) bool {
	parsed, ok := resolveToURL(nil, rawURL)
	if !ok {
		return false
	}
	return f.AllowURL(parsed)
}

// AllowURL 报告已解析的绝对 URL 是否允许抓取。
func (f URLFilter) AllowURL(target *url.URL) bool {
	if target == nil || target.Hostname() == "" || target.User != nil {
		return false
	}
	switch strings.ToLower(target.Scheme) {
	case "http", "https":
	default:
		return false
	}
	if !f.hostAllowed(target.Hostname()) {
		return false
	}
	return f.pathAllowed(targetPath(target))
}

func (f URLFilter) hostAllowed(hostname string) bool {
	if f.SeedHost == "" {
		return true
	}
	host := strings.ToLower(hostname)
	seed := strings.ToLower(f.SeedHost)
	if host == seed {
		return true
	}
	return f.AllowSubdomains && strings.HasSuffix(host, "."+seed)
}

// pathAllowed 判定 path 是否在范围内：exclude 优先命中即拒；include 为空表示不限制。
func (f URLFilter) pathAllowed(target string) bool {
	for _, pattern := range f.ExcludePaths {
		if matchPathGlob(pattern, target) {
			return false
		}
	}
	if len(f.IncludePaths) == 0 {
		return true
	}
	for _, pattern := range f.IncludePaths {
		if matchPathGlob(pattern, target) {
			return true
		}
	}
	return false
}

func targetPath(target *url.URL) string {
	if target.Path == "" {
		return "/"
	}
	return target.Path
}

// matchPathGlob 按 path 段匹配 glob。非法 pattern（path.Match 报错）按不匹配处理：
// 过滤器宁可少收页面，也不因规则写错而静默放行整站。
func matchPathGlob(pattern, target string) bool {
	if pattern == "" {
		return false
	}
	if pattern == target {
		return true
	}
	return matchPathSegments(strings.Split(pattern, "/"), strings.Split(target, "/"))
}

func matchPathSegments(patterns, segments []string) bool {
	if len(patterns) == 0 {
		return len(segments) == 0
	}
	if patterns[0] == "**" {
		if matchPathSegments(patterns[1:], segments) {
			return true
		}
		return len(segments) > 0 && matchPathSegments(patterns, segments[1:])
	}
	if len(segments) == 0 {
		return false
	}
	matched, err := path.Match(patterns[0], segments[0])
	if err != nil || !matched {
		return false
	}
	return matchPathSegments(patterns[1:], segments[1:])
}

// NormalizeURL 规范化绝对 URL，口径与页面身份一致（value.NormalizeWebSourceURI：
// 小写 scheme/host、去默认端口与 fragment、剥跟踪参数）；相对或非法 URL 返回 ok=false。
func NormalizeURL(raw string) (string, bool) {
	return ResolveURL(nil, raw)
}

// ResolveURL 以 base 解析可能是相对的引用并规范化；base 为 nil 时只接受绝对 URL。
func ResolveURL(base *url.URL, raw string) (string, bool) {
	parsed, ok := resolveToURL(base, raw)
	if !ok {
		return "", false
	}
	return parsed.String(), true
}

// resolveToURL 解析 + 规范化，返回规范化后的 URL（供过滤时直接取 host/path 使用）。
func resolveToURL(base *url.URL, raw string) (*url.URL, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" && base == nil {
		return nil, false
	}
	reference, err := url.Parse(trimmed)
	if err != nil {
		return nil, false
	}
	resolved := reference
	if base != nil {
		resolved = base.ResolveReference(reference)
	}
	if !resolved.IsAbs() {
		return nil, false
	}
	normalized, err := value.NormalizeWebSourceURI(resolved.String())
	if err != nil {
		return nil, false
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, false
	}
	return parsed, true
}

// ResolveLinks 把页面上的原始出链集合解析为可抓取的规范化 URL 列表（spec §5.1）：
// resolve 相对引用 → 去 fragment → 规范化 → scheme/host/path 过滤 → 去重（保留首次出现顺序）。
//
// 空引用与纯 fragment（"#section"）指向当前文档自身，crawl 不需要它们，直接跳过。
func ResolveLinks(base *url.URL, rawLinks []string, filter URLFilter) []string {
	links := make([]string, 0, len(rawLinks))
	seen := make(map[string]struct{}, len(rawLinks))
	for _, raw := range rawLinks {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parsed, ok := resolveToURL(base, trimmed)
		if !ok || !filter.AllowURL(parsed) {
			continue
		}
		normalized := parsed.String()
		if _, duplicated := seen[normalized]; duplicated {
			continue
		}
		seen[normalized] = struct{}{}
		links = append(links, normalized)
	}
	return links
}
