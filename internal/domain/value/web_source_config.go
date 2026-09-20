package value

import (
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// —— 枚举 ——

// WebSourceMode 是 web 内容源的发现模式（spec §4.1），创建后不可变。
type WebSourceMode string

const (
	// WebSourceModeCrawl 从 seed_url 起受限 BFS 爬取。
	WebSourceModeCrawl WebSourceMode = "crawl"
	// WebSourceModeSitemap 从 sitemap.xml（含 sitemapindex 递归）发现页面。
	WebSourceModeSitemap WebSourceMode = "sitemap"
	// WebSourceModeURLs 固定 URL 列表，经 API 增删。
	WebSourceModeURLs WebSourceMode = "urls"
)

// IsValid 报告模式是否属于已知合法集合。
func (m WebSourceMode) IsValid() bool {
	return m == WebSourceModeCrawl || m == WebSourceModeSitemap || m == WebSourceModeURLs
}

// WebSourceFetchEngine 是抓取引擎（spec §6.1）。http 无需 connection；
// 其余引擎（含 auto 的升级目标）经 fetch.connection_id 引用连接，创建后不可变。
type WebSourceFetchEngine string

const (
	// WebSourceFetchEngineAuto 以 http 起步、命中升级条件后用 connection 指向的引擎重抓。
	WebSourceFetchEngineAuto WebSourceFetchEngine = "auto"
	// WebSourceFetchEngineHTTP 直接 HTTP 抓取（默认引擎），支持条件请求，无需连接。
	WebSourceFetchEngineHTTP WebSourceFetchEngine = "http"
	// WebSourceFetchEngineRender 经 browser 连接（CDP）渲染后取 outerHTML。
	WebSourceFetchEngineRender WebSourceFetchEngine = "render"
	// WebSourceFetchEngineFirecrawl 经 firecrawl 连接抓取，markdown 直出。
	WebSourceFetchEngineFirecrawl WebSourceFetchEngine = "firecrawl"
	// WebSourceFetchEngineJina 经 jina reader 抓取，markdown 直出。
	WebSourceFetchEngineJina WebSourceFetchEngine = "jina"
)

// IsValid 报告引擎是否属于已知合法集合。
func (e WebSourceFetchEngine) IsValid() bool {
	switch e {
	case WebSourceFetchEngineAuto, WebSourceFetchEngineHTTP, WebSourceFetchEngineRender,
		WebSourceFetchEngineFirecrawl, WebSourceFetchEngineJina:
		return true
	}
	return false
}

// WebSourceExtractMode 是正文抽取模式（spec §7）。
type WebSourceExtractMode string

const (
	// WebSourceExtractModeReadability 用 go-readability 抽取正文（默认）。
	WebSourceExtractModeReadability WebSourceExtractMode = "readability"
	// WebSourceExtractModeFull 整页转 markdown。
	WebSourceExtractModeFull WebSourceExtractMode = "full"
)

// IsValid 报告抽取模式是否属于已知合法集合。
func (m WebSourceExtractMode) IsValid() bool {
	return m == WebSourceExtractModeReadability || m == WebSourceExtractModeFull
}

// —— typed 结构 ——

// WebSourceConfig 是 kb_sources.config（jsonb）的 typed 形态（spec §4.1）。
// 运行态键（cursor 等）不在 config 内，见 WebSourceConfigRuntimeKeys。
type WebSourceConfig struct {
	Mode     WebSourceMode
	Crawl    WebSourceCrawlConfig
	Sitemap  *WebSourceSitemapConfig // 仅 mode=sitemap
	URLs     []string                // 仅 mode=urls（已规范化）
	Fetch    WebSourceFetchConfig
	Extract  WebSourceExtractConfig
	Sync     WebSourceSyncConfig
	OnDelete SourceDeletePolicy
}

// WebSourceCrawlConfig 是「发现 + 抓取」默认参数集：键名沿用 crawl，但礼貌性参数
// （RespectRobots/RequestDelayMs/UserAgent）对三种模式统一生效（spec §4.1），
// 因此本对象在 sitemap/urls 模式下也可出现；SeedURL 是 crawl 模式专属的身份键。
type WebSourceCrawlConfig struct {
	// SeedURL 已规范化，仅 mode=crawl 下允许出现。
	SeedURL string
	// MaxDepth/MaxPages 受进程级天花板钳制（WebSourceLimits），超限报错而非截断。
	MaxDepth int
	MaxPages int
	// IncludePaths/ExcludePaths 为 path glob（* / **），exclude 优先。
	IncludePaths    []string
	ExcludePaths    []string
	AllowSubdomains bool
	RespectRobots   bool
	// RequestDelayMs 为 0 表示未设置：运行时使用进程级 default_request_delay_ms。
	RequestDelayMs int
	// UserAgent 为 nil 表示未设置：运行时使用进程级 user_agent_template。
	UserAgent *string
}

// WebSourceSitemapConfig 是 sitemap 模式的发现入口，URL 创建后不可变。
type WebSourceSitemapConfig struct {
	URL string
}

// WebSourceFetchConfig 是抓取引擎选择与升级参数。
type WebSourceFetchConfig struct {
	Engine WebSourceFetchEngine
	// ConnectionID 是 connection 的 UUID 规范形（小写连字符）；http 引擎必须为空。
	ConnectionID string
	AutoEscalate WebSourceAutoEscalateConfig
}

// WebSourceAutoEscalateConfig 是 auto 引擎的渐进升级条件（spec §6.2）。
type WebSourceAutoEscalateConfig struct {
	Enabled      bool
	MinTextChars int
}

// WebSourceExtractConfig 是正文抽取配置（spec §7）。
type WebSourceExtractConfig struct {
	Mode WebSourceExtractMode
	// TargetSelector 是可选 CSS 选择器，空串表示未设置。
	TargetSelector string
	FallbackToFull bool
	Readability    WebSourceReadabilityConfig
}

// WebSourceReadabilityConfig 是 go-readability 参数集，全可选有默认。
type WebSourceReadabilityConfig struct {
	CharThreshold     int
	NbTopCandidates   int
	ClassesToPreserve []string
	KeepClasses       bool
}

// WebSourceSyncConfig 是同步调度配置；Cron 为空串表示无定时（仅手动触发）。
type WebSourceSyncConfig struct {
	Cron string
}

// WebSourceLimits 是 web 源配置解析的进程级天花板（config.yaml web_source 小节），
// 由 application 层注入：domain 层不读取 config.yaml（分层约束）。
type WebSourceLimits struct {
	// MaxPagesCeiling 是 crawl.max_pages 的部署者天花板。
	MaxPagesCeiling int
	// MaxDepthCeiling 是 crawl.max_depth 的部署者天花板。
	MaxDepthCeiling int
}

func (l WebSourceLimits) validate() error {
	if l.MaxPagesCeiling <= 0 || l.MaxDepthCeiling <= 0 {
		return fmt.Errorf("%w: web 源天花板未注入（max_pages_ceiling/max_depth_ceiling 必须为正）", domainerrors.ErrValidation)
	}
	return nil
}

// —— 键清单 ——

// WebSourceConfigRuntimeKeys 是运行态键：存储在 kb_sources 列上而非 config jsonb，
// parser 遇到即报错，防止 API 误写（spec §4.1）。
var WebSourceConfigRuntimeKeys = []string{
	"sync_cursor", "next_sync_at", "sync_requested_force", "sync_last_result",
}

// WebSourceConfigImmutableKeys 是创建后不可变的配置键（点分路径，spec §9.2 PATCH 拒绝项）。
// 变更这些键 = 换源身份，应新建源替换旧源。
var WebSourceConfigImmutableKeys = []string{
	"mode",
	"crawl.seed_url",
	"sitemap.url",
	"fetch.engine",
	"fetch.connection_id",
}

var (
	webSourceTopLevelKeys     = []string{"mode", "crawl", "sitemap", "urls", "fetch", "extract", "sync", "on_delete"}
	webSourceCrawlKeys        = []string{"seed_url", "max_depth", "max_pages", "include_paths", "exclude_paths", "allow_subdomains", "respect_robots", "request_delay_ms", "user_agent"}
	webSourceSitemapKeys      = []string{"url"}
	webSourceFetchKeys        = []string{"engine", "connection_id", "auto_escalate"}
	webSourceAutoEscalateKeys = []string{"enabled", "min_text_chars"}
	webSourceExtractKeys      = []string{"mode", "target_selector", "fallback_to_full", "readability"}
	webSourceReadabilityKeys  = []string{"char_threshold", "nb_top_candidates", "classes_to_preserve", "keep_classes"}
	webSourceSyncKeys         = []string{"cron"}
)

// 缺省值（spec §4.1 示例值）。RequestDelayMs/UserAgent 无源级缺省：
// 留空（0/nil）表示运行时回退进程级 default_request_delay_ms / user_agent_template。
const (
	defaultWebSourceMaxDepth                  = 3
	defaultWebSourceMaxPages                  = 500
	defaultWebSourceAutoEscalateMinTextChars  = 200
	defaultWebSourceReadabilityCharThreshold  = 500
	defaultWebSourceReadabilityNbTopCandidate = 5
)

func defaultWebSourceCrawlConfig() WebSourceCrawlConfig {
	return WebSourceCrawlConfig{
		MaxDepth:      defaultWebSourceMaxDepth,
		MaxPages:      defaultWebSourceMaxPages,
		IncludePaths:  []string{},
		ExcludePaths:  []string{},
		RespectRobots: true,
	}
}

func defaultWebSourceFetchConfig() WebSourceFetchConfig {
	// engine 缺省 http（spec §6.1 引擎表）：其余引擎（含 auto）都要求 connection，
	// 默认 http 才能让不带连接的最小配置成立。
	return WebSourceFetchConfig{
		Engine: WebSourceFetchEngineHTTP,
		AutoEscalate: WebSourceAutoEscalateConfig{
			Enabled:      true,
			MinTextChars: defaultWebSourceAutoEscalateMinTextChars,
		},
	}
}

func defaultWebSourceExtractConfig() WebSourceExtractConfig {
	return WebSourceExtractConfig{
		Mode:           WebSourceExtractModeReadability,
		FallbackToFull: true,
		Readability: WebSourceReadabilityConfig{
			CharThreshold:     defaultWebSourceReadabilityCharThreshold,
			NbTopCandidates:   defaultWebSourceReadabilityNbTopCandidate,
			ClassesToPreserve: []string{},
		},
	}
}

// —— 解析入口 ——

// ParseWebSourceConfig 把 kb_sources.config 的 jsonb map 解析为 typed 配置：
// 校验枚举与跨字段约束、以 NormalizeWebSourceURI 规范化全部 URL、填充缺省值，
// 并对 max_pages/max_depth 做天花板校验（超限返回 validation error 而非静默截断）。
//
// 未知键（顶层与各子对象）一律拒绝，显式防拼写错误静默失效；
// 运行态键（WebSourceConfigRuntimeKeys）出现即报错。
// limits 由 application 层从 config.yaml 注入，未注入（零值）视为调用错误。
func ParseWebSourceConfig(raw map[string]any, limits WebSourceLimits) (WebSourceConfig, error) {
	if err := limits.validate(); err != nil {
		return WebSourceConfig{}, err
	}
	for _, key := range WebSourceConfigRuntimeKeys {
		if _, ok := raw[key]; ok {
			return WebSourceConfig{}, fmt.Errorf("%w: 运行态键 %q 由系统维护，不允许写入 config", domainerrors.ErrValidation, key)
		}
	}
	if err := rejectUnknownKeys(raw, webSourceTopLevelKeys, ""); err != nil {
		return WebSourceConfig{}, err
	}

	cfg := WebSourceConfig{
		Crawl:    defaultWebSourceCrawlConfig(),
		Fetch:    defaultWebSourceFetchConfig(),
		Extract:  defaultWebSourceExtractConfig(),
		OnDelete: SourceDeleteKeep,
	}

	mode, err := parseWebSourceMode(raw)
	if err != nil {
		return WebSourceConfig{}, err
	}
	cfg.Mode = mode

	if crawlObj, present, err := webSourceObject(raw, "crawl", "crawl"); err != nil {
		return WebSourceConfig{}, err
	} else if present {
		if err := parseWebSourceCrawl(crawlObj, mode, limits, &cfg.Crawl); err != nil {
			return WebSourceConfig{}, err
		}
	} else if mode == WebSourceModeCrawl {
		return WebSourceConfig{}, fmt.Errorf("%w: mode=crawl 时 crawl 子对象必填", domainerrors.ErrValidation)
	}

	if err := parseWebSourceSitemapField(raw, mode, &cfg); err != nil {
		return WebSourceConfig{}, err
	}
	if err := parseWebSourceURLsField(raw, mode, &cfg); err != nil {
		return WebSourceConfig{}, err
	}

	if fetchObj, present, err := webSourceObject(raw, "fetch", "fetch"); err != nil {
		return WebSourceConfig{}, err
	} else if present {
		if err := parseWebSourceFetch(fetchObj, &cfg.Fetch); err != nil {
			return WebSourceConfig{}, err
		}
	}
	if extractObj, present, err := webSourceObject(raw, "extract", "extract"); err != nil {
		return WebSourceConfig{}, err
	} else if present {
		if err := parseWebSourceExtract(extractObj, &cfg.Extract); err != nil {
			return WebSourceConfig{}, err
		}
	}
	if syncObj, present, err := webSourceObject(raw, "sync", "sync"); err != nil {
		return WebSourceConfig{}, err
	} else if present {
		if err := parseWebSourceSync(syncObj, &cfg.Sync); err != nil {
			return WebSourceConfig{}, err
		}
	}
	if rawOnDelete, present := raw["on_delete"]; present && rawOnDelete != nil {
		s, ok := rawOnDelete.(string)
		if !ok {
			return WebSourceConfig{}, fmt.Errorf("%w: on_delete 必须是字符串", domainerrors.ErrValidation)
		}
		policy, err := ParseSourceDeletePolicy(s)
		if err != nil {
			return WebSourceConfig{}, err
		}
		cfg.OnDelete = policy
	}
	return cfg, nil
}

// RejectImmutableWebSourceConfigKeys 校验 PATCH 的 partial config map：只要出现
// 任一不可变键（WebSourceConfigImmutableKeys，无论值是否与旧值相同）即返回
// ErrValidation——PATCH 语义里重发不可变键视为客户端错误，显式拒绝优于静默忽略。
// partial 与旧 config 的合并、合并后的全量校验（ParseWebSourceConfig）由
// application 层完成。
func RejectImmutableWebSourceConfigKeys(partial map[string]any) error {
	if len(partial) == 0 {
		return nil
	}
	if _, ok := partial["mode"]; ok {
		return fmt.Errorf("%w: 键 mode 创建后不可变", domainerrors.ErrValidation)
	}
	if crawl, ok := partial["crawl"].(map[string]any); ok {
		if _, ok := crawl["seed_url"]; ok {
			return fmt.Errorf("%w: 键 crawl.seed_url 创建后不可变", domainerrors.ErrValidation)
		}
	}
	if sitemap, ok := partial["sitemap"].(map[string]any); ok {
		if _, ok := sitemap["url"]; ok {
			return fmt.Errorf("%w: 键 sitemap.url 创建后不可变", domainerrors.ErrValidation)
		}
	}
	if fetch, ok := partial["fetch"].(map[string]any); ok {
		for _, key := range []string{"engine", "connection_id"} {
			if _, ok := fetch[key]; ok {
				return fmt.Errorf("%w: 键 fetch.%s 创建后不可变", domainerrors.ErrValidation, key)
			}
		}
	}
	return nil
}
