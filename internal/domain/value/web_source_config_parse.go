package value

import (
	"fmt"
	"strings"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/google/uuid"
)

// 本文件承载 web_source_config 的子对象解析器与原始值收窄辅助：
// 类型形状（枚举/结构体/键清单/缺省值/Parse 入口）在 web_source_config.go。

func parseWebSourceMode(raw map[string]any) (WebSourceMode, error) {
	rawMode, present := raw["mode"]
	if !present || rawMode == nil {
		return "", fmt.Errorf("%w: mode 必填（crawl|sitemap|urls）", domainerrors.ErrValidation)
	}
	s, ok := rawMode.(string)
	if !ok {
		return "", fmt.Errorf("%w: mode 必须是字符串", domainerrors.ErrValidation)
	}
	mode := WebSourceMode(s)
	if !mode.IsValid() {
		return "", fmt.Errorf("%w: 未知 web 源 mode %q（合法值：crawl|sitemap|urls）", domainerrors.ErrValidation, s)
	}
	return mode, nil
}

// parseWebSourceSitemapField 处理顶层 sitemap 键：仅在 mode=sitemap 下有效，
// 且该模式下必填（sitemap.url 是模式身份键，创建后不可变）。
func parseWebSourceSitemapField(raw map[string]any, mode WebSourceMode, cfg *WebSourceConfig) error {
	if _, present := raw["sitemap"]; !present {
		if mode == WebSourceModeSitemap {
			return fmt.Errorf("%w: mode=sitemap 时 sitemap.url 必填", domainerrors.ErrValidation)
		}
		return nil
	}
	if mode != WebSourceModeSitemap {
		return fmt.Errorf("%w: sitemap 键仅在 mode=sitemap 下有效", domainerrors.ErrValidation)
	}
	obj, present, err := webSourceObject(raw, "sitemap", "sitemap")
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("%w: mode=sitemap 时 sitemap.url 必填", domainerrors.ErrValidation)
	}
	if err := rejectUnknownKeys(obj, webSourceSitemapKeys, "（sitemap）"); err != nil {
		return err
	}
	rawURL, ok := obj["url"]
	if !ok || rawURL == nil {
		return fmt.Errorf("%w: mode=sitemap 时 sitemap.url 必填", domainerrors.ErrValidation)
	}
	s, ok := rawURL.(string)
	if !ok {
		return fmt.Errorf("%w: sitemap.url 必须是字符串 URL", domainerrors.ErrValidation)
	}
	normalized, err := NormalizeWebSourceURI(s)
	if err != nil {
		return err
	}
	cfg.Sitemap = &WebSourceSitemapConfig{URL: normalized}
	return nil
}

// parseWebSourceURLsField 处理顶层 urls 键：仅在 mode=urls 下有效（其它 mode 出现即报错，
// 显式拒绝静默吞配置），该模式下必须是非空数组，每项经 NormalizeWebSourceURI 规范化后存。
func parseWebSourceURLsField(raw map[string]any, mode WebSourceMode, cfg *WebSourceConfig) error {
	rawURLs, present := raw["urls"]
	if !present {
		if mode == WebSourceModeURLs {
			return fmt.Errorf("%w: mode=urls 时 urls 必须是非空数组", domainerrors.ErrValidation)
		}
		return nil
	}
	if mode != WebSourceModeURLs {
		return fmt.Errorf("%w: urls 键仅在 mode=urls 下有效", domainerrors.ErrValidation)
	}
	items, err := webSourceStringArray(rawURLs, "urls")
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("%w: mode=urls 时 urls 必须是非空数组", domainerrors.ErrValidation)
	}
	seen := make(map[string]struct{}, len(items))
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		n, err := NormalizeWebSourceURI(item)
		if err != nil {
			return err
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("%w: urls 规范化后重复：%s", domainerrors.ErrValidation, n)
		}
		seen[n] = struct{}{}
		normalized = append(normalized, n)
	}
	cfg.URLs = normalized
	return nil
}

// parseWebSourceCrawl 解析 crawl 子对象（各模式均可出现，礼貌性参数跨模式生效）。
// seed_url 仅在 mode=crawl 下允许出现且必填；显式 max_pages/max_depth 在任何 mode 下
// 都做天花板校验（配置卫生），缺省值只在 crawl 模式生效、故缺省×天花板的冲突
// 仅在 crawl 模式下报错。
func parseWebSourceCrawl(m map[string]any, mode WebSourceMode, limits WebSourceLimits, out *WebSourceCrawlConfig) error {
	if err := rejectUnknownKeys(m, webSourceCrawlKeys, "（crawl）"); err != nil {
		return err
	}
	if _, present := m["seed_url"]; present {
		if mode != WebSourceModeCrawl {
			return fmt.Errorf("%w: crawl.seed_url 仅在 mode=crawl 下有效", domainerrors.ErrValidation)
		}
		s, ok := m["seed_url"].(string)
		if !ok || s == "" {
			return fmt.Errorf("%w: crawl.seed_url 必须是非空字符串 URL", domainerrors.ErrValidation)
		}
		normalized, err := NormalizeWebSourceURI(s)
		if err != nil {
			return err
		}
		out.SeedURL = normalized
	} else if mode == WebSourceModeCrawl {
		return fmt.Errorf("%w: mode=crawl 时 crawl.seed_url 必填", domainerrors.ErrValidation)
	}

	if err := applyCeilingedInt(m, "max_depth", "crawl.max_depth", limits.MaxDepthCeiling,
		defaultWebSourceMaxDepth, mode == WebSourceModeCrawl, &out.MaxDepth); err != nil {
		return err
	}
	if err := applyCeilingedInt(m, "max_pages", "crawl.max_pages", limits.MaxPagesCeiling,
		defaultWebSourceMaxPages, mode == WebSourceModeCrawl, &out.MaxPages); err != nil {
		return err
	}

	if v, present := m["include_paths"]; present {
		paths, err := webSourceStringArray(v, "crawl.include_paths")
		if err != nil {
			return err
		}
		if out.IncludePaths, err = webSourceTrimmedNonEmpty(paths, "crawl.include_paths"); err != nil {
			return err
		}
	}
	if v, present := m["exclude_paths"]; present {
		paths, err := webSourceStringArray(v, "crawl.exclude_paths")
		if err != nil {
			return err
		}
		if out.ExcludePaths, err = webSourceTrimmedNonEmpty(paths, "crawl.exclude_paths"); err != nil {
			return err
		}
	}
	if err := webSourceBool(m, "allow_subdomains", "crawl.allow_subdomains", &out.AllowSubdomains); err != nil {
		return err
	}
	if err := webSourceBool(m, "respect_robots", "crawl.respect_robots", &out.RespectRobots); err != nil {
		return err
	}
	if v, present := m["request_delay_ms"]; present && v != nil {
		n, err := webSourceInt(v, "crawl.request_delay_ms")
		if err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("%w: crawl.request_delay_ms 不能为负", domainerrors.ErrValidation)
		}
		out.RequestDelayMs = n
	}
	if v, present := m["user_agent"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%w: crawl.user_agent 必须是字符串", domainerrors.ErrValidation)
		}
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			return fmt.Errorf("%w: crawl.user_agent 不能是空白字符串", domainerrors.ErrValidation)
		}
		out.UserAgent = &trimmed
	}
	return nil
}

// applyCeilingedInt 处理带天花板的正整数字段：显式值必须为正且 ≤ 天花板；
// 缺省时若该值在当前模式下生效（effective=true）且缺省值超天花板，同样报错——
// 部署者把天花板压到缺省值之下时，要求调用方显式给出 ≤ 天花板的值。
func applyCeilingedInt(m map[string]any, rawKey, errKey string, ceiling, defaultValue int, effective bool, out *int) error {
	v, present := m[rawKey]
	if !present || v == nil {
		if effective && defaultValue > ceiling {
			return fmt.Errorf("%w: %s 缺省值 %d 超过进程天花板 %d，需显式配置不超天花板的值",
				domainerrors.ErrValidation, errKey, defaultValue, ceiling)
		}
		return nil
	}
	n, err := webSourceInt(v, errKey)
	if err != nil {
		return err
	}
	if n <= 0 {
		return fmt.Errorf("%w: %s 必须是正整数", domainerrors.ErrValidation, errKey)
	}
	if n > ceiling {
		return fmt.Errorf("%w: %s %d 超过进程天花板 %d（调高 config.yaml web_source 天花板或降低配置）",
			domainerrors.ErrValidation, errKey, n, ceiling)
	}
	*out = n
	return nil
}

// parseWebSourceFetch 解析抓取配置。组合约束：engine=http 时 connection_id 必须为空；
// engine != http（含 auto——升级目标经连接取凭证）时 connection_id 必填且为合法 UUID。
func parseWebSourceFetch(m map[string]any, out *WebSourceFetchConfig) error {
	if err := rejectUnknownKeys(m, webSourceFetchKeys, "（fetch）"); err != nil {
		return err
	}
	if v, present := m["engine"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%w: fetch.engine 必须是字符串", domainerrors.ErrValidation)
		}
		engine := WebSourceFetchEngine(s)
		if !engine.IsValid() {
			return fmt.Errorf("%w: 未知 fetch.engine %q（合法值：auto|http|render|firecrawl|jina）", domainerrors.ErrValidation, s)
		}
		out.Engine = engine
	}
	if v, present := m["connection_id"]; present && v != nil && v != "" {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%w: fetch.connection_id 必须是字符串", domainerrors.ErrValidation)
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return fmt.Errorf("%w: fetch.connection_id 必须是合法 UUID", domainerrors.ErrValidation)
		}
		out.ConnectionID = id.String()
	}
	if out.Engine == WebSourceFetchEngineHTTP && out.ConnectionID != "" {
		return fmt.Errorf("%w: fetch.engine=http 不允许配置 connection_id", domainerrors.ErrValidation)
	}
	if out.Engine != WebSourceFetchEngineHTTP && out.ConnectionID == "" {
		return fmt.Errorf("%w: fetch.engine=%s 需要 fetch.connection_id（http 外的引擎均经连接取凭证）",
			domainerrors.ErrValidation, out.Engine)
	}
	if obj, present, err := webSourceObject(m, "auto_escalate", "fetch.auto_escalate"); err != nil {
		return err
	} else if present {
		if err := rejectUnknownKeys(obj, webSourceAutoEscalateKeys, "（fetch.auto_escalate）"); err != nil {
			return err
		}
		if err := webSourceBool(obj, "enabled", "fetch.auto_escalate.enabled", &out.AutoEscalate.Enabled); err != nil {
			return err
		}
		if v, present := obj["min_text_chars"]; present && v != nil {
			n, err := webSourceInt(v, "fetch.auto_escalate.min_text_chars")
			if err != nil {
				return err
			}
			if n <= 0 {
				return fmt.Errorf("%w: fetch.auto_escalate.min_text_chars 必须是正整数", domainerrors.ErrValidation)
			}
			out.AutoEscalate.MinTextChars = n
		}
	}
	return nil
}

func parseWebSourceExtract(m map[string]any, out *WebSourceExtractConfig) error {
	if err := rejectUnknownKeys(m, webSourceExtractKeys, "（extract）"); err != nil {
		return err
	}
	if v, present := m["mode"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%w: extract.mode 必须是字符串", domainerrors.ErrValidation)
		}
		mode := WebSourceExtractMode(s)
		if !mode.IsValid() {
			return fmt.Errorf("%w: 未知 extract.mode %q（合法值：readability|full）", domainerrors.ErrValidation, s)
		}
		out.Mode = mode
	}
	if v, present := m["target_selector"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%w: extract.target_selector 必须是字符串", domainerrors.ErrValidation)
		}
		out.TargetSelector = s
	}
	if err := webSourceBool(m, "fallback_to_full", "extract.fallback_to_full", &out.FallbackToFull); err != nil {
		return err
	}
	if obj, present, err := webSourceObject(m, "readability", "extract.readability"); err != nil {
		return err
	} else if present {
		return parseWebSourceReadability(obj, &out.Readability)
	}
	return nil
}

func parseWebSourceReadability(m map[string]any, out *WebSourceReadabilityConfig) error {
	if err := rejectUnknownKeys(m, webSourceReadabilityKeys, "（extract.readability）"); err != nil {
		return err
	}
	if v, present := m["char_threshold"]; present && v != nil {
		n, err := webSourceInt(v, "extract.readability.char_threshold")
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("%w: extract.readability.char_threshold 必须是正整数", domainerrors.ErrValidation)
		}
		out.CharThreshold = n
	}
	if v, present := m["nb_top_candidates"]; present && v != nil {
		n, err := webSourceInt(v, "extract.readability.nb_top_candidates")
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("%w: extract.readability.nb_top_candidates 必须是正整数", domainerrors.ErrValidation)
		}
		out.NbTopCandidates = n
	}
	if v, present := m["classes_to_preserve"]; present {
		items, err := webSourceStringArray(v, "extract.readability.classes_to_preserve")
		if err != nil {
			return err
		}
		if out.ClassesToPreserve, err = webSourceTrimmedNonEmpty(items, "extract.readability.classes_to_preserve"); err != nil {
			return err
		}
	}
	if err := webSourceBool(m, "keep_classes", "extract.readability.keep_classes", &out.KeepClasses); err != nil {
		return err
	}
	return nil
}

// parseWebSourceSync 只做 5 段非空的结构校验，不做完整 cron 语法校验：
// domain 层不引入第三方库（分层铁律），robfig/cron 解析先例在 application 层
// （source_sync_scheduler.go，飞书 cron 同源）；无效表达式在调度侧退化为无定时并告警。
func parseWebSourceSync(m map[string]any, out *WebSourceSyncConfig) error {
	if err := rejectUnknownKeys(m, webSourceSyncKeys, "（sync）"); err != nil {
		return err
	}
	v, present := m["cron"]
	if !present || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("%w: sync.cron 必须是字符串", domainerrors.ErrValidation)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if fields := strings.Fields(s); len(fields) != 5 {
		return fmt.Errorf("%w: sync.cron %q 必须是 5 段标准 cron 表达式（minute hour dom month dow）",
			domainerrors.ErrValidation, s)
	}
	out.Cron = s
	return nil
}
