package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
)

// robots 相关告警码（写入 TreeSnapshot.Warnings / Job 结果，spec §6.3）。
const (
	// WarningRobotsDisallowed 表示目标 URL 被目标站 robots.txt 禁止抓取（跳过，不算 retryable）。
	WarningRobotsDisallowed = "robots_disallowed"
	// WarningRobotsUnavailable 表示 robots.txt 不可用（4xx/5xx/网络错误/解析失败），
	// 按「允许」处理并告警（保守可用性选择，与主流爬虫一致）。
	WarningRobotsUnavailable = "robots_unavailable"
)

// maxRobotsBytes 是 robots.txt 的读取上限：超出部分截断后解析，避免超大文件占用内存。
const maxRobotsBytes = 512 * 1024

// defaultRobotsCacheTTL 是 CacheTTL 未配置时的缓存时长。
// 生产装配应从 web_source.robots_cache_ttl_seconds 注入（spec §4.2）。
const defaultRobotsCacheTTL = time.Hour

// RobotsCheckerConfig 配置 robots.txt 检查器。
type RobotsCheckerConfig struct {
	// Client 必须注入 SSRF-safe client（生产为 httpclient.NewPublicWebClient 的产物）。
	Client *http.Client
	// UserAgent 是抓取 UA；robotstxt 按其与 user-agent 组名做前缀匹配
	// （"LanghuanBot/1.0 (+…)" 命中组名 "langhuanbot"）。空值表示只用 "*" 组。
	UserAgent string
	// CacheTTL 是每个 origin 的 robots.txt 缓存时长；<=0 用 defaultRobotsCacheTTL。
	CacheTTL time.Duration
}

// RobotsChecker 按 origin 拉取 robots.txt 并按抓取 UA 做语义匹配（spec §6.3）。
//
// 缓存以 origin（scheme://host[:port]）为键：命中 TTL 内直接复用；失败结果同样缓存，
// 避免在同一 run 内对不可用站点反复重试。并发安全（发现阶段可能跨 host 并发，
// 同一 origin 的并发调用可能各自拉取一次——首版同 host 串行，不做单飞合并）。
type RobotsChecker struct {
	client    *http.Client
	userAgent string
	ttl       time.Duration

	// now 可注入（测试用）；生产为 time.Now。
	now func() time.Time

	mu    sync.Mutex
	cache map[string]robotsEntry
}

type robotsEntry struct {
	data *robotstxt.RobotsData
	// warning 非空表示 robots.txt 不可用：本次结论为「允许」并携带该告警码。
	warning   string
	fetchedAt time.Time
}

// NewRobotsChecker 构造 robots.txt 检查器；未注入 client 视为装配错误。
func NewRobotsChecker(cfg RobotsCheckerConfig) (*RobotsChecker, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("robots 检查器必须注入 SSRF-safe HTTP client")
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultRobotsCacheTTL
	}
	return &RobotsChecker{
		client:    cfg.Client,
		userAgent: cfg.UserAgent,
		ttl:       ttl,
		now:       time.Now,
		cache:     make(map[string]robotsEntry),
	}, nil
}

// Allowed 报告 target（绝对 HTTP(S) URL）是否允许抓取，返回 (allowed, warning)：
//   - robots.txt 允许：true, ""；
//   - 命中 Disallow：false, WarningRobotsDisallowed；
//   - robots.txt 不可用：true, WarningRobotsUnavailable。
//
// 只有 context 取消/超时或 target 非法才返回 error：调用方（发现/抓取编排）据此
// 决定是否中断整个 run，而不是把「这一页能不能抓」混进 error 通道。
func (c *RobotsChecker) Allowed(ctx context.Context, target string) (bool, string, error) {
	parsed, ok := resolveToURL(nil, target)
	if !ok {
		return false, "", fmt.Errorf("robots 校验目标不是合法的绝对 HTTP(S) URL: %q", target)
	}
	entry, err := c.entryFor(ctx, originOf(parsed))
	if err != nil {
		return false, "", err
	}
	if entry.warning != "" {
		return true, entry.warning, nil
	}
	if entry.data == nil {
		return true, WarningRobotsUnavailable, nil
	}
	if entry.data.FindGroup(c.userAgent).Test(requestPath(parsed)) {
		return true, "", nil
	}
	return false, WarningRobotsDisallowed, nil
}

// entryFor 返回 origin 的 robots 缓存条目，必要时拉取并写入缓存。
func (c *RobotsChecker) entryFor(ctx context.Context, origin string) (robotsEntry, error) {
	c.mu.Lock()
	entry, cached := c.cache[origin]
	c.mu.Unlock()
	if cached && c.now().Sub(entry.fetchedAt) < c.ttl {
		return entry, nil
	}

	fetched, err := c.fetch(ctx, origin)
	if err != nil {
		return robotsEntry{}, err
	}
	fetched.fetchedAt = c.now()
	c.mu.Lock()
	c.cache[origin] = fetched
	c.mu.Unlock()
	return fetched, nil
}

// fetch 拉取并解析 origin 的 robots.txt。
//
// 与 robotstxt.FromStatusAndBytes 的默认语义（5xx → disallowAll）不同：spec §6.3
// 要求拉取失败按「允许」处理。因此这里只对 2xx 走解析，其余一律记为不可用告警。
func (c *RobotsChecker) fetch(ctx context.Context, origin string) (robotsEntry, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/robots.txt", nil)
	if err != nil {
		return robotsEntry{}, fmt.Errorf("构造 robots.txt 请求失败: %w", err)
	}
	if c.userAgent != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}
	request.Header.Set("Accept", "text/plain,*/*;q=0.8")

	response, err := c.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return robotsEntry{}, fmt.Errorf("拉取 robots.txt 被中断: %w", ctxErr)
		}
		return robotsEntry{warning: WarningRobotsUnavailable}, nil
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseDrainBytes))
		_ = response.Body.Close()
	}()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return robotsEntry{warning: WarningRobotsUnavailable}, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRobotsBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return robotsEntry{}, fmt.Errorf("读取 robots.txt 被中断: %w", ctxErr)
		}
		return robotsEntry{warning: WarningRobotsUnavailable}, nil
	}
	data, err := robotstxt.FromBytes(body)
	if err != nil || data == nil {
		return robotsEntry{warning: WarningRobotsUnavailable}, nil
	}
	return robotsEntry{data: data}, nil
}

func originOf(target *url.URL) string {
	return target.Scheme + "://" + target.Host
}

// requestPath 返回 robots 规则匹配用的 path+query（空 path 归一为 "/"）。
func requestPath(target *url.URL) string {
	return target.RequestURI()
}
