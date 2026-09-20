package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件承载 web SourceConnector 的骨架：provider 标识、进程级参数（ConnectorConfig）、
// 「单次同步运行」会话（runSession：页缓存 + 抓取器 + robots 检查器 + 引擎分派）与会话
// 生命周期。ListTree 见 connector_tree.go，Fetch 见 connector_fetch.go。

// ProviderName 是 web provider 标识（kb_sources.kind='web'，与 feishu.ProviderName 同位）。
const ProviderName = "web"

// credentialsConfigKey 是凭证注入约定键（spec §5.4）：application 层把预解密后的
// 连接凭证放在 SourceConfig["__credentials"]，仅内存传递，不落库、不进日志。
// http 引擎不需要凭证；render/firecrawl/jina 引擎按下列子键消费。该键不属于
// config 合同——value.ParseWebSourceConfig 拒绝未知键，因此解析前必须剥离，且
// 不得进入会话指纹（避免凭证随 key 字符串滞留）。
const credentialsConfigKey = "__credentials"

// __credentials 的子键（application 层 webSourceConfigWithCredentials 写入）：
//   - provider：连接 provider（browser/firecrawl/jina），决定引擎与凭证键；
//   - api_key：firecrawl/jina 凭证；token：browser 的可选 CDP 凭证；
//   - endpoint_url：非敏感端点（browser CDP endpoint_url；firecrawl endpoint 覆盖）。
const (
	credentialsProviderKey = "provider"
	credentialsAPIKeyKey   = "api_key"
	credentialsTokenKey    = "token"
	credentialsEndpointKey = "endpoint_url"
)

// ConnectorConfig 是 web connector 的进程级参数（config.yaml web_source 小节，
// spec §4.2），由 DI 在装配时注入；源级覆盖值在每次调用内经 parseSourceConfig 合并。
type ConnectorConfig struct {
	// HTTPClient 必须注入 SSRF-safe client（httpclient.NewPublicWebClient 的产物，
	// spec §6.4），并已把 web_source.request_timeout_seconds 配置为 client.Timeout——
	// 单请求超时是传输层属性，在 client 上统一覆盖 http 引擎、robots.txt 与 sitemap
	// 文档三类请求，connector 不做第二套超时机制。
	HTTPClient *http.Client
	// UserAgent 是渲染 user_agent_template 后的最终抓取 UA（{{base_url}} 由 DI 渲染）；
	// 源级 crawl.user_agent 优先于它。
	UserAgent string
	// DefaultRequestDelay 对应 web_source.default_request_delay_ms；
	// 源级 crawl.request_delay_ms=0（未设置）时回退到它。
	DefaultRequestDelay time.Duration
	// MaxContentBytes 对应 web_source.max_content_bytes（单页响应体上限）；<=0 表示不限
	//（请求级 FetchOptions.MaxContentBytes 优先于它）。
	MaxContentBytes int64
	// RobotsCacheTTL 对应 web_source.robots_cache_ttl_seconds；<=0 用检查器内置默认。
	RobotsCacheTTL time.Duration
	// RenderMaxConcurrent 对应 web_source.render_max_concurrent：render 引擎的
	// 进程级并发上限（跨源共享一个信号量，Connector 持有，spec §4.2/§6.1）。
	// <=0 用引擎内置默认。
	RenderMaxConcurrent int
	// Limits 是 crawl.max_pages/max_depth 的部署者天花板，解析源配置时做超限校验；
	// 未注入（零值）会在首次 ListTree/Fetch 返回 validation error。
	Limits value.WebSourceLimits
}

// Connector 实现 web 源的 SourceConnector（spec §5）：按 options.SourceConfig 解析
// 引擎与参数，组装本包构建块（HTTPFetcher/RobotsChecker/PageCache/三种发现层）。
//
// 无 connection 的 web 源调用时 conn 传零值 model.SourceConnection{}；http 引擎不消费
// connection 与凭证。本类型不记录任何日志，凭证（__credentials）只经内存透传给引擎
// 扩展点，不会被序列化进会话指纹或错误信息。
type Connector struct {
	config ConnectorConfig

	// renderLimiter 是 render 引擎的进程级并发信号量（RenderMaxConcurrent），
	// 跨源共享：各源会话构造的 RenderFetcher 都从这里取渲染槽。
	renderLimiter *renderLimiter

	// advancedEngineOverrides 把指定 provider 的高级引擎构建结果替换为注入的
	// engineFunc（仅测试装配用，生产恒 nil；与 HTTPFetcher 的 now/sleep 同属
	// 测试 seam——真实 render/firecrawl/jina 需要浏览器或外网服务）。
	advancedEngineOverrides map[value.SourceProvider]engineFunc

	// sessions 是「单次同步运行」的运行时会话，以 SourceConfig 的规范化指纹为键：
	//   - ListTree 是每次 run 的入口，替换同键旧会话（新 run 不复用上次的缓存，
	//     被替换会话持有的引擎资源如 CDP 连接随之关闭）；
	//   - 同一 run 内的 Fetch 命中同一会话，使 crawl 发现阶段抓过的页在 Fetch 阶段
	//     不二次抓取（spec §5.1 发现-抓取复用）；Fetch 前无 ListTree 时（防御路径）
	//     惰性建一次性会话。
	// 条目数以「去重后的源配置数」为上界，进程退出即清空；不做后台淘汰（不引入
	// 常驻 goroutine）。同源并发 run 由调度层排除（spec §9.1 同源单 active）。
	mu       sync.Mutex
	sessions map[string]*runSession
}

// NewConnector 构造 web SourceConnector；未注入 HTTP client 视为装配错误
// （无 SSRF 防护且无超时的 client 不允许默认兜底，与 NewHTTPFetcher 同一口径）。
func NewConnector(config ConnectorConfig) (*Connector, error) {
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("web connector 必须注入 SSRF-safe HTTP client")
	}
	return &Connector{
		config:        config,
		renderLimiter: newRenderLimiter(config.RenderMaxConcurrent),
		sessions:      make(map[string]*runSession),
	}, nil
}

// Provider 返回 "web"。
func (c *Connector) Provider() string { return ProviderName }

// runSession 是一次同步运行的共享运行时。同一 run 的发现与抓取必须复用同一组
// 构建块实例：页缓存（发现-抓取复用）、robots 缓存（per-origin TTL）与 HTTPFetcher
// 的 per-host 限速状态都依赖实例存续，按调用新建会使礼貌性与缓存全部失效。
type runSession struct {
	cfg       value.WebSourceConfig
	userAgent string
	client    *http.Client
	robots    *RobotsChecker // nil 表示 respect_robots=false，跳过检查
	cache     *PageCache
	// httpFetch 是 http 引擎入口：显式 http 引擎下它同时是会话引擎（engine）；
	// auto 引擎下作为起步引擎与升级判定的输入（fetch_auto.go）。
	httpFetch engineFunc
	// engine 是会话引擎入口；nil 表示 auto 引擎（fetchEngine 转入 autoFetch：
	// http 起步 + 升级判定 + last_engine 短路）。
	engine engineFunc
	// escalate 是 auto 引擎的升级通道（connection 指向的 render/firecrawl/jina）；
	// nil 表示升级无处可去（运行时防御路径，warning possible_js_shell）。
	escalate engineFunc
	// closer 释放引擎持有的长生命周期资源（render 的 CDP 连接）；会话被替换时
	// 由 connector 调用，重复调用安全。
	closer func()
}

// engineFunc 是引擎路由后的单页抓取入口（返回值合同见 fetch_contract.go）。
type engineFunc func(ctx context.Context, request FetchRequest) (fetchedPage, error)

// advancedEngineFor 依据注入凭证的 provider 构造高级引擎（render/firecrawl/jina，
// spec §6.1）。require=false（auto 升级通道）时凭证缺失或 provider 不可识别返回
// (nil, nil, nil)——升级无处可去走 possible_js_shell 防御路径；require=true
// （显式引擎）时同类情况返回 validation error：引擎是源配置的不可变字段，解析层
// 接受该值而运行期无法服务属于配置性永久失败——编排层据此判 failed 而非
// retryable，避免这些源陷入 Job 重试循环（与引擎未实现错误同口径）。
func (c *Connector) advancedEngineFor(engine value.WebSourceFetchEngine, credentials map[string]any, userAgent string, extract value.WebSourceExtractConfig, require bool) (engineFunc, func(), error) {
	provider := value.SourceProvider(credentialString(credentials, credentialsProviderKey))
	// 测试 seam：命中注入即绕过真实引擎构建。
	if override, ok := c.advancedEngineOverrides[provider]; ok {
		return override, nil, nil
	}
	// unavailable 表达「引擎所需的连接信息缺失/不可识别」：auto 通道静默降级，
	// 显式引擎判配置性永久失败。
	unavailable := func(reason string) (engineFunc, func(), error) {
		if require {
			return nil, nil, fmt.Errorf("%w: web 抓取引擎 %q 无法服务（%s；检查 fetch.connection_id 指向的连接及其 config/凭证）",
				domainerrors.ErrValidation, engine, reason)
		}
		return nil, nil, nil
	}

	switch provider {
	case value.SourceProviderBrowser:
		cdpURL := RenderCDPEndpoint(credentialString(credentials, credentialsEndpointKey), credentialString(credentials, credentialsTokenKey))
		if cdpURL == "" {
			return unavailable("缺少 CDP endpoint_url")
		}
		fetcher, err := NewRenderFetcher(RenderFetcherConfig{
			CDPURL:          cdpURL,
			UserAgent:       userAgent,
			Limiter:         c.renderLimiter,
			Timeout:         c.config.HTTPClient.Timeout,
			MaxContentBytes: c.config.MaxContentBytes,
		})
		if err != nil {
			return nil, nil, err
		}
		return fetcher.Fetch, fetcher.Close, nil
	case value.SourceProviderFirecrawl:
		apiKey := credentialString(credentials, credentialsAPIKeyKey)
		if apiKey == "" {
			return unavailable("缺少 api_key")
		}
		fetcher, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{
			APIKey:          apiKey,
			Endpoint:        credentialString(credentials, credentialsEndpointKey),
			OnlyMainContent: extract.Mode != value.WebSourceExtractModeFull,
			Timeout:         c.config.HTTPClient.Timeout,
			MaxContentBytes: c.config.MaxContentBytes,
		})
		if err != nil {
			return nil, nil, err
		}
		return fetcher.Fetch, nil, nil
	case value.SourceProviderJina:
		apiKey := credentialString(credentials, credentialsAPIKeyKey)
		if apiKey == "" {
			return unavailable("缺少 api_key")
		}
		fetcher, err := NewJinaFetcher(JinaFetcherConfig{
			APIKey:          apiKey,
			Timeout:         c.config.HTTPClient.Timeout,
			MaxContentBytes: c.config.MaxContentBytes,
		})
		if err != nil {
			return nil, nil, err
		}
		return fetcher.Fetch, nil, nil
	default:
		return unavailable(fmt.Sprintf("连接 provider %q 不提供 web 抓取引擎", provider))
	}
}

// engineFor 是非 auto 引擎的分派点：http 直接用 fetcher；render/firecrawl/jina
// 走凭证驱动的 advancedEngineFor（require=true）。auto 在 newRunSession 单独
// 装配（http 起步 + 升级通道，见 fetch_auto.go）。
func (c *Connector) engineFor(engine value.WebSourceFetchEngine, fetcher *HTTPFetcher, credentials map[string]any, userAgent string, extract value.WebSourceExtractConfig) (engineFunc, func(), error) {
	switch engine {
	case value.WebSourceFetchEngineHTTP:
		return fetcher.Fetch, nil, nil
	case value.WebSourceFetchEngineRender, value.WebSourceFetchEngineFirecrawl, value.WebSourceFetchEngineJina:
		return c.advancedEngineFor(engine, credentials, userAgent, extract, true)
	default:
		return nil, nil, fmt.Errorf("%w: 未知 web 抓取引擎 %q", domainerrors.ErrValidation, engine)
	}
}

// newRunSession 解析源配置并装配本次运行的构建块。参数合并优先级：源级显式值 >
// 进程级 ConnectorConfig > 构建块内置默认。引擎装配（spec §6.1）：
//   - http：fetcher.Fetch；
//   - auto：engine 留空（fetchEngine 转 autoFetch），升级通道按凭证 provider 构建，
//     缺失时为 nil（运行时防御路径）；
//   - render/firecrawl/jina：凭证驱动构建，缺失/不可识别判配置性永久失败。
func (c *Connector) newRunSession(raw map[string]any) (*runSession, error) {
	cfg, err := parseSourceConfig(raw, c.config.Limits)
	if err != nil {
		return nil, err
	}

	userAgent := c.config.UserAgent
	if cfg.Crawl.UserAgent != nil {
		userAgent = *cfg.Crawl.UserAgent
	}
	requestDelay := c.config.DefaultRequestDelay
	if cfg.Crawl.RequestDelayMs > 0 {
		requestDelay = time.Duration(cfg.Crawl.RequestDelayMs) * time.Millisecond
	}

	fetcher, err := NewHTTPFetcher(HTTPFetcherConfig{
		Client:          c.config.HTTPClient,
		UserAgent:       userAgent,
		RequestDelay:    requestDelay,
		MaxContentBytes: c.config.MaxContentBytes,
	})
	if err != nil {
		return nil, err
	}
	injected := injectedCredentials(raw)

	session := &runSession{
		cfg:       cfg,
		userAgent: userAgent,
		client:    c.config.HTTPClient,
		cache:     NewPageCache(cfg.Crawl.MaxPages),
		httpFetch: fetcher.Fetch,
	}
	switch cfg.Fetch.Engine {
	case value.WebSourceFetchEngineAuto:
		escalate, closer, err := c.advancedEngineFor(cfg.Fetch.Engine, injected, userAgent, cfg.Extract, false)
		if err != nil {
			return nil, err
		}
		session.escalate = escalate
		session.closer = closer
	default:
		engine, closer, err := c.engineFor(cfg.Fetch.Engine, fetcher, injected, userAgent, cfg.Extract)
		if err != nil {
			return nil, err
		}
		session.engine = engine
		session.closer = closer
	}
	if cfg.Crawl.RespectRobots {
		checker, err := NewRobotsChecker(RobotsCheckerConfig{
			Client:    c.config.HTTPClient,
			UserAgent: userAgent,
			CacheTTL:  c.config.RobotsCacheTTL,
		})
		if err != nil {
			return nil, err
		}
		session.robots = checker
	}
	return session, nil
}

// close 释放会话内引擎持有的长生命周期资源（render 的 CDP 连接）；重复调用安全。
func (s *runSession) close() {
	if s.closer != nil {
		s.closer()
	}
}

// injectedCredentials 返回注入的凭证 map（无注入键或形状不符时 nil——调用方按
// 「无凭证」消费）。
func injectedCredentials(raw map[string]any) map[string]any {
	injected, _ := raw[credentialsConfigKey].(map[string]any)
	return injected
}

// credentialString 读取凭证子键的字符串值（缺失/类型不符按空串；去首尾空白）。
func credentialString(credentials map[string]any, key string) string {
	value, _ := credentials[key].(string)
	return strings.TrimSpace(value)
}

// parseSourceConfig 把 options.SourceConfig（jsonb 原样 map）解析为 typed 配置：
// 先剥离 __credentials 注入键（非 config 合同键，未知键校验会拒绝它），再走
// value.ParseWebSourceConfig 做枚举/天花板/规范化校验。
func parseSourceConfig(raw map[string]any, limits value.WebSourceLimits) (value.WebSourceConfig, error) {
	return value.ParseWebSourceConfig(stripCredentials(raw), limits)
}

// stripCredentials 返回剥离 __credentials 后的配置 map；无注入键时原样返回
// （不拷贝、不改写入参——调用方持有的 map 可能同时是 KB source 行的解码产物）。
func stripCredentials(raw map[string]any) map[string]any {
	if _, injected := raw[credentialsConfigKey]; !injected {
		return raw
	}
	cleaned := make(map[string]any, len(raw)-1)
	for key, val := range raw {
		if key == credentialsConfigKey {
			continue
		}
		cleaned[key] = val
	}
	return cleaned
}

// sessionKey 计算会话指纹：剥离凭证后的 config 做 canonical JSON（encoding/json
// 对 map 按 key 排序，同一源的两次 jsonb 解码必然同形）。序列化失败时回退
// fmt.Sprintf——SourceConfig 只含 jsonb 可表达类型，实际不可达。
func sessionKey(raw map[string]any) string {
	cleaned := stripCredentials(raw)
	if encoded, err := json.Marshal(cleaned); err == nil {
		return string(encoded)
	}
	return fmt.Sprintf("%v", cleaned)
}

// sessionFor 返回 SourceConfig 对应的运行会话。newRun=true（ListTree，run 入口）
// 时无条件新建并替换同键会话（被替换会话的引擎资源随之关闭）；newRun=false
// （Fetch）时优先复用，缺失时惰性新建。
func (c *Connector) sessionFor(raw map[string]any, newRun bool) (*runSession, error) {
	key := sessionKey(raw)
	if !newRun {
		c.mu.Lock()
		session := c.sessions[key]
		c.mu.Unlock()
		if session != nil {
			return session, nil
		}
	}
	session, err := c.newRunSession(raw)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	previous := c.sessions[key]
	c.sessions[key] = session
	c.mu.Unlock()
	if previous != nil {
		previous.close()
	}
	return session, nil
}

// compile-time interface conformance.
var _ sourceport.SourceConnector = (*Connector)(nil)
