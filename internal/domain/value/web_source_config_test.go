package value

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

// testWebSourceLimits 是 spec §4.2 的示例天花板（max_pages_ceiling=10000、max_depth_ceiling=10）。
var testWebSourceLimits = WebSourceLimits{MaxPagesCeiling: 10000, MaxDepthCeiling: 10}

const testWebSourceUUID = "123e4567-e89b-12d3-a456-426614174000"

func strPtr(s string) *string { return &s }

// —— 测试用 raw map 构造器（每次调用返回全新实例，便于按例变异）——

func minimalCrawlRaw() map[string]any {
	return map[string]any{
		"mode":  "crawl",
		"crawl": map[string]any{"seed_url": "https://example.com/docs/"},
	}
}

func minimalSitemapRaw() map[string]any {
	return map[string]any{
		"mode":    "sitemap",
		"sitemap": map[string]any{"url": "https://example.com/sitemap.xml"},
	}
}

func minimalURLsRaw() map[string]any {
	return map[string]any{
		"mode": "urls",
		"urls": []any{"https://example.com/a"},
	}
}

func setKey(raw map[string]any, key string, val any) map[string]any {
	raw[key] = val
	return raw
}

func delKey(raw map[string]any, key string) map[string]any {
	delete(raw, key)
	return raw
}

func setNested(raw map[string]any, obj, key string, val any) map[string]any {
	m, ok := raw[obj].(map[string]any)
	if !ok {
		m = map[string]any{}
		raw[obj] = m
	}
	m[key] = val
	return raw
}

func delNested(raw map[string]any, obj, key string) map[string]any {
	if m, ok := raw[obj].(map[string]any); ok {
		delete(m, key)
	}
	return raw
}

// minimalCrawlWant 是 minimalCrawlRaw 的期望解析结果：全部缺省值（spec §4.1）。
func minimalCrawlWant() WebSourceConfig {
	return WebSourceConfig{
		Mode: WebSourceModeCrawl,
		Crawl: WebSourceCrawlConfig{
			SeedURL:       "https://example.com/docs/",
			MaxDepth:      3,
			MaxPages:      500,
			IncludePaths:  []string{},
			ExcludePaths:  []string{},
			RespectRobots: true,
		},
		Fetch: WebSourceFetchConfig{
			Engine:       WebSourceFetchEngineHTTP,
			AutoEscalate: WebSourceAutoEscalateConfig{Enabled: true, MinTextChars: 200},
		},
		Extract: WebSourceExtractConfig{
			Mode:           WebSourceExtractModeReadability,
			FallbackToFull: true,
			Readability: WebSourceReadabilityConfig{
				CharThreshold:     500,
				NbTopCandidates:   5,
				ClassesToPreserve: []string{},
			},
		},
		OnDelete: SourceDeleteKeep,
	}
}

// TestParseWebSourceConfig_Valid 覆盖合法完整配置、每类缺省值、规范化与天花板边界。
//
// 注意：spec §4.1 的 JSON 是"全键形状示例"（三种 mode 的键并集），三种模式互斥键
// 并存时按校验规则拒绝，因此这里的"完整配置"按 mode 取各自合法子集。
func TestParseWebSourceConfig_Valid(t *testing.T) {
	tests := []struct {
		name   string
		raw    map[string]any
		limits *WebSourceLimits
		want   WebSourceConfig
	}{
		{
			name: "crawl 完整配置（全显式值+URL 规范化）",
			raw: map[string]any{
				"mode": "crawl",
				"crawl": map[string]any{
					"seed_url":         "HTTPS://Docs.Example.COM:443/guide?utm_source=nav",
					"max_depth":        3,
					"max_pages":        500,
					"include_paths":    []any{"docs/*"},
					"exclude_paths":    []any{"admin/*", "private/*"},
					"allow_subdomains": false,
					"respect_robots":   true,
					"request_delay_ms": 200,
					"user_agent":       "MyBot/1.0",
				},
				"fetch": map[string]any{
					"engine":        "http",
					"connection_id": nil,
					"auto_escalate": map[string]any{"enabled": false, "min_text_chars": 300},
				},
				"extract": map[string]any{
					"mode":             "full",
					"target_selector":  "article.main",
					"fallback_to_full": false,
					"readability": map[string]any{
						"char_threshold":      800,
						"nb_top_candidates":   3,
						"classes_to_preserve": []any{"highlight"},
						"keep_classes":        true,
					},
				},
				"sync":      map[string]any{"cron": "0 */6 * * *"},
				"on_delete": "remove",
			},
			want: WebSourceConfig{
				Mode: WebSourceModeCrawl,
				Crawl: WebSourceCrawlConfig{
					SeedURL:         "https://docs.example.com/guide",
					MaxDepth:        3,
					MaxPages:        500,
					IncludePaths:    []string{"docs/*"},
					ExcludePaths:    []string{"admin/*", "private/*"},
					AllowSubdomains: false,
					RespectRobots:   true,
					RequestDelayMs:  200,
					UserAgent:       strPtr("MyBot/1.0"),
				},
				Fetch: WebSourceFetchConfig{
					Engine:       WebSourceFetchEngineHTTP,
					AutoEscalate: WebSourceAutoEscalateConfig{Enabled: false, MinTextChars: 300},
				},
				Extract: WebSourceExtractConfig{
					Mode:           WebSourceExtractModeFull,
					TargetSelector: "article.main",
					FallbackToFull: false,
					Readability: WebSourceReadabilityConfig{
						CharThreshold:     800,
						NbTopCandidates:   3,
						ClassesToPreserve: []string{"highlight"},
						KeepClasses:       true,
					},
				},
				Sync:     WebSourceSyncConfig{Cron: "0 */6 * * *"},
				OnDelete: SourceDeleteRemove,
			},
		},
		{
			name: "crawl 最小配置：全部缺省值",
			raw:  minimalCrawlRaw(),
			want: minimalCrawlWant(),
		},
		{
			name: "crawl 显式天花板边界值（等于天花板放行）",
			raw:  setNested(setNested(minimalCrawlRaw(), "crawl", "max_pages", 10000), "crawl", "max_depth", 10),
			want: func() WebSourceConfig {
				w := minimalCrawlWant()
				w.Crawl.MaxPages = 10000
				w.Crawl.MaxDepth = 10
				return w
			}(),
		},
		{
			name: "sitemap 最小配置 + crawl 礼貌性参数跨模式生效",
			raw: map[string]any{
				"mode":    "sitemap",
				"sitemap": map[string]any{"url": "https://example.com/sitemap.xml"},
				"crawl":   map[string]any{"request_delay_ms": 500, "respect_robots": false},
			},
			want: WebSourceConfig{
				Mode: WebSourceModeSitemap,
				Crawl: WebSourceCrawlConfig{
					MaxDepth:       3,
					MaxPages:       500,
					IncludePaths:   []string{},
					ExcludePaths:   []string{},
					RespectRobots:  false,
					RequestDelayMs: 500,
				},
				Sitemap: &WebSourceSitemapConfig{URL: "https://example.com/sitemap.xml"},
				Fetch: WebSourceFetchConfig{
					Engine:       WebSourceFetchEngineHTTP,
					AutoEscalate: WebSourceAutoEscalateConfig{Enabled: true, MinTextChars: 200},
				},
				Extract: WebSourceExtractConfig{
					Mode:           WebSourceExtractModeReadability,
					FallbackToFull: true,
					Readability: WebSourceReadabilityConfig{
						CharThreshold:     500,
						NbTopCandidates:   5,
						ClassesToPreserve: []string{},
					},
				},
				OnDelete: SourceDeleteKeep,
			},
		},
		{
			name: "可选对象显式 null 视为未设置（缺省值生效）",
			raw: map[string]any{
				"mode":      "sitemap",
				"sitemap":   map[string]any{"url": "https://example.com/sitemap.xml"},
				"crawl":     nil,
				"fetch":     nil,
				"extract":   nil,
				"sync":      nil,
				"on_delete": nil,
			},
			want: func() WebSourceConfig {
				w := minimalCrawlWant()
				w.Mode = WebSourceModeSitemap
				w.Crawl.SeedURL = ""
				w.Sitemap = &WebSourceSitemapConfig{URL: "https://example.com/sitemap.xml"}
				return w
			}(),
		},
		{
			name: "urls 模式：每项规范化（小写 host/去默认端口/剥跟踪参数/去 fragment）",
			raw: map[string]any{
				"mode": "urls",
				"urls": []any{
					"HTTPS://Example.com:443/a?utm_source=x",
					"https://example.com/b#frag",
				},
			},
			want: func() WebSourceConfig {
				w := minimalCrawlWant()
				w.Mode = WebSourceModeURLs
				w.Crawl.SeedURL = ""
				w.URLs = []string{"https://example.com/a", "https://example.com/b"}
				return w
			}(),
		},
		{
			name: "engine=auto + connection_id：UUID 归一为小写规范形",
			raw: setKey(minimalCrawlRaw(), "fetch", map[string]any{
				"engine":        "auto",
				"connection_id": "123E4567-E89B-12D3-A456-426614174000",
			}),
			want: func() WebSourceConfig {
				w := minimalCrawlWant()
				w.Fetch.Engine = WebSourceFetchEngineAuto
				w.Fetch.ConnectionID = testWebSourceUUID
				return w
			}(),
		},
		{
			name: "engine=http + connection_id 空串：视为未设置",
			raw: setKey(minimalCrawlRaw(), "fetch", map[string]any{
				"engine":        "http",
				"connection_id": "",
			}),
			want: minimalCrawlWant(),
		},
		{
			name: "request_delay_ms 显式 0：合法（0=运行时用进程级默认）",
			raw:  setNested(minimalCrawlRaw(), "crawl", "request_delay_ms", 0),
			want: minimalCrawlWant(),
		},
		{
			name: "sync.cron 空串：视为无定时",
			raw:  setKey(minimalCrawlRaw(), "sync", map[string]any{"cron": ""}),
			want: minimalCrawlWant(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := testWebSourceLimits
			if tt.limits != nil {
				limits = *tt.limits
			}
			got, err := ParseWebSourceConfig(tt.raw, limits)
			if err != nil {
				t.Fatalf("ParseWebSourceConfig 报错: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseWebSourceConfig 结果不符:\n got  %+v\n want %+v", got, tt.want)
			}
		})
	}
}

// TestParseWebSourceConfig_Errors 覆盖枚举非法、模式互斥键、天花板超限报错（非静默截断）、
// engine/connection 组合约束、运行态键拒绝、未知键拒绝与各类类型错误。
// wantErrContains 断言错误消息包含期望的字段点分路径或分支关键词，
// 防止用例因错误的原因通过（如夹具拼写错误把预期的天花板失败变成"未知键"失败）。
func TestParseWebSourceConfig_Errors(t *testing.T) {
	tests := []struct {
		name            string
		raw             map[string]any
		limits          *WebSourceLimits
		wantErrContains string
	}{
		// —— mode 枚举 ——
		{name: "mode 缺失", raw: delKey(minimalCrawlRaw(), "mode"), wantErrContains: "mode 必填"},
		{name: "mode 为 null", raw: setKey(minimalCrawlRaw(), "mode", nil), wantErrContains: "mode 必填"},
		{name: "mode 非字符串", raw: setKey(minimalCrawlRaw(), "mode", 42), wantErrContains: "mode 必须是字符串"},
		{name: "mode 非法枚举", raw: setKey(minimalCrawlRaw(), "mode", "list"), wantErrContains: `mode "list"`},
		{
			name: "spec §4.1 全键形状（三种 mode 互斥键并存）拒绝",
			raw: setKey(setKey(minimalCrawlRaw(), "sitemap",
				map[string]any{"url": "https://example.com/sitemap.xml"}),
				"urls", []any{"https://example.com/a"}),
			wantErrContains: "sitemap 键仅在 mode=sitemap 下有效",
		},

		// —— mode=crawl ——
		{name: "mode=crawl 缺 crawl 子对象", raw: delKey(minimalCrawlRaw(), "crawl"), wantErrContains: "crawl 子对象必填"},
		{name: "crawl 为 null", raw: setKey(minimalCrawlRaw(), "crawl", nil), wantErrContains: "crawl 子对象必填"},
		{name: "crawl 非对象", raw: setKey(minimalCrawlRaw(), "crawl", "https://example.com/"), wantErrContains: `"crawl" 必须是对象`},
		{name: "crawl.seed_url 缺失", raw: delNested(minimalCrawlRaw(), "crawl", "seed_url"), wantErrContains: "crawl.seed_url 必填"},
		{name: "crawl.seed_url 为 null", raw: setNested(minimalCrawlRaw(), "crawl", "seed_url", nil), wantErrContains: "crawl.seed_url 必须是非空字符串"},
		{name: "crawl.seed_url 非 http(s)", raw: setNested(minimalCrawlRaw(), "crawl", "seed_url", "ftp://example.com/"), wantErrContains: "绝对 HTTP(S) URL"},
		{name: "crawl.seed_url 相对路径", raw: setNested(minimalCrawlRaw(), "crawl", "seed_url", "/docs"), wantErrContains: "绝对 HTTP(S) URL"},
		{
			name:            "crawl 模式出现 sitemap 键",
			raw:             setKey(minimalCrawlRaw(), "sitemap", map[string]any{"url": "https://example.com/sitemap.xml"}),
			wantErrContains: "sitemap 键仅在 mode=sitemap 下有效",
		},
		{name: "crawl 模式出现 urls 键", raw: setKey(minimalCrawlRaw(), "urls", []any{"https://example.com/a"}), wantErrContains: "urls 键仅在 mode=urls 下有效"},

		// —— mode=sitemap ——
		{name: "mode=sitemap 缺 sitemap", raw: delKey(minimalSitemapRaw(), "sitemap"), wantErrContains: "sitemap.url 必填"},
		{name: "sitemap 为 null", raw: setKey(minimalSitemapRaw(), "sitemap", nil), wantErrContains: "sitemap.url 必填"},
		{name: "sitemap.url 缺失", raw: setKey(minimalSitemapRaw(), "sitemap", map[string]any{}), wantErrContains: "sitemap.url 必填"},
		{name: "sitemap.url 非法 URL", raw: setNested(minimalSitemapRaw(), "sitemap", "url", "not-a-url"), wantErrContains: "绝对 HTTP(S) URL"},
		{
			name:            "sitemap 模式出现 crawl.seed_url",
			raw:             setKey(minimalSitemapRaw(), "crawl", map[string]any{"seed_url": "https://example.com/"}),
			wantErrContains: "crawl.seed_url 仅在 mode=crawl 下有效",
		},
		{name: "sitemap 模式出现 urls 键", raw: setKey(minimalSitemapRaw(), "urls", []any{"https://example.com/a"}), wantErrContains: "urls 键仅在 mode=urls 下有效"},

		// —— mode=urls ——
		{name: "mode=urls 缺 urls", raw: delKey(minimalURLsRaw(), "urls"), wantErrContains: "urls 必须是非空数组"},
		{name: "urls 空数组", raw: setKey(minimalURLsRaw(), "urls", []any{}), wantErrContains: "urls 必须是非空数组"},
		{name: "urls 非数组", raw: setKey(minimalURLsRaw(), "urls", "https://example.com/a"), wantErrContains: "urls 必须是字符串数组"},
		{name: "urls 项非字符串", raw: setKey(minimalURLsRaw(), "urls", []any{123}), wantErrContains: "urls 第 0 项必须是字符串"},
		{name: "urls 项非法 URL", raw: setKey(minimalURLsRaw(), "urls", []any{"ftp://example.com/a"}), wantErrContains: "绝对 HTTP(S) URL"},
		{
			name: "urls 规范化后重复",
			raw: setKey(minimalURLsRaw(), "urls", []any{
				"https://example.com/a",
				"HTTPS://EXAMPLE.com:443/a?utm_source=x",
			}),
			wantErrContains: "urls 规范化后重复",
		},

		// —— 天花板（超限报错而非静默截断）——
		{name: "max_pages 超天花板", raw: setNested(minimalCrawlRaw(), "crawl", "max_pages", 10001), wantErrContains: "超过进程天花板"},
		{name: "max_depth 超天花板", raw: setNested(minimalCrawlRaw(), "crawl", "max_depth", 11), wantErrContains: "超过进程天花板"},
		{
			name:            "缺省 max_pages(500) 超低天花板(100)",
			raw:             minimalCrawlRaw(),
			limits:          &WebSourceLimits{MaxPagesCeiling: 100, MaxDepthCeiling: 10},
			wantErrContains: "超过进程天花板",
		},
		{
			name:            "缺省 max_depth(3) 超低天花板(2)",
			raw:             minimalCrawlRaw(),
			limits:          &WebSourceLimits{MaxPagesCeiling: 10000, MaxDepthCeiling: 2},
			wantErrContains: "超过进程天花板",
		},
		{
			name:            "天花板零值（application 未注入）",
			raw:             minimalCrawlRaw(),
			limits:          &WebSourceLimits{},
			wantErrContains: "天花板未注入",
		},

		// —— 数值/类型 ——
		{name: "max_pages 为 0", raw: setNested(minimalCrawlRaw(), "crawl", "max_pages", 0), wantErrContains: "crawl.max_pages 必须是正整数"},
		{name: "max_pages 为负", raw: setNested(minimalCrawlRaw(), "crawl", "max_pages", -5), wantErrContains: "crawl.max_pages 必须是正整数"},
		{name: "max_pages 非整数", raw: setNested(minimalCrawlRaw(), "crawl", "max_pages", 3.5), wantErrContains: "crawl.max_pages 必须是整数"},
		{name: "max_pages 字符串", raw: setNested(minimalCrawlRaw(), "crawl", "max_pages", "500"), wantErrContains: "crawl.max_pages 必须是整数"},
		{name: "request_delay_ms 为负", raw: setNested(minimalCrawlRaw(), "crawl", "request_delay_ms", -1), wantErrContains: "crawl.request_delay_ms 不能为负"},
		{
			name:            "respect_robots 非布尔",
			raw:             setNested(minimalCrawlRaw(), "crawl", "respect_robots", "true"),
			wantErrContains: "crawl.respect_robots 必须是布尔值",
		},
		{
			name:            "include_paths 含空串",
			raw:             setNested(minimalCrawlRaw(), "crawl", "include_paths", []any{"docs/*", ""}),
			wantErrContains: "crawl.include_paths 第 1 项不能为空",
		},
		{
			name:            "include_paths 含非字符串项",
			raw:             setNested(minimalCrawlRaw(), "crawl", "include_paths", []any{1}),
			wantErrContains: "crawl.include_paths 第 0 项必须是字符串",
		},
		{name: "user_agent 空串", raw: setNested(minimalCrawlRaw(), "crawl", "user_agent", ""), wantErrContains: "crawl.user_agent 不能是空白字符串"},

		// —— fetch ——
		{
			name:            "engine 非法枚举",
			raw:             setKey(minimalCrawlRaw(), "fetch", map[string]any{"engine": "puppeteer"}),
			wantErrContains: `fetch.engine "puppeteer"`,
		},
		{
			name:            "engine=render 无 connection_id",
			raw:             setKey(minimalCrawlRaw(), "fetch", map[string]any{"engine": "render"}),
			wantErrContains: "fetch.engine=render 需要 fetch.connection_id",
		},
		{
			name:            "engine=auto 无 connection_id",
			raw:             setKey(minimalCrawlRaw(), "fetch", map[string]any{"engine": "auto"}),
			wantErrContains: "fetch.engine=auto 需要 fetch.connection_id",
		},
		{
			name:            "connection_id 非 UUID",
			raw:             setKey(minimalCrawlRaw(), "fetch", map[string]any{"engine": "render", "connection_id": "not-uuid"}),
			wantErrContains: "fetch.connection_id 必须是合法 UUID",
		},
		{
			name:            "engine=http 带 connection_id",
			raw:             setKey(minimalCrawlRaw(), "fetch", map[string]any{"engine": "http", "connection_id": testWebSourceUUID}),
			wantErrContains: "fetch.engine=http 不允许配置 connection_id",
		},
		{
			name: "auto_escalate.min_text_chars 为 0",
			raw: setKey(minimalCrawlRaw(), "fetch", map[string]any{
				"engine": "auto", "connection_id": testWebSourceUUID,
				"auto_escalate": map[string]any{"min_text_chars": 0},
			}),
			wantErrContains: "min_text_chars 必须是正整数",
		},
		{
			name: "auto_escalate.min_text_chars 非整数",
			raw: setKey(minimalCrawlRaw(), "fetch", map[string]any{
				"engine": "auto", "connection_id": testWebSourceUUID,
				"auto_escalate": map[string]any{"min_text_chars": 1.5},
			}),
			wantErrContains: "min_text_chars 必须是整数",
		},
		{
			name:            "fetch.auto_escalate 非对象：错误路径带父前缀",
			raw:             setKey(minimalCrawlRaw(), "fetch", map[string]any{"auto_escalate": 5}),
			wantErrContains: `"fetch.auto_escalate" 必须是对象`,
		},

		// —— extract ——
		{
			name:            "extract.mode 非法枚举",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"mode": "dom"}),
			wantErrContains: `extract.mode "dom"`,
		},
		{
			name:            "readability.char_threshold 为 0",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"readability": map[string]any{"char_threshold": 0}}),
			wantErrContains: "char_threshold 必须是正整数",
		},
		{
			name:            "readability.nb_top_candidates 为负",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"readability": map[string]any{"nb_top_candidates": -1}}),
			wantErrContains: "nb_top_candidates 必须是正整数",
		},
		{
			name:            "readability.classes_to_preserve 含空串",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"readability": map[string]any{"classes_to_preserve": []any{""}}}),
			wantErrContains: "classes_to_preserve 第 0 项不能为空",
		},
		{
			name:            "extract.readability 非对象：错误路径带父前缀",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"readability": "x"}),
			wantErrContains: `"extract.readability" 必须是对象`,
		},

		// —— sync / on_delete ——
		{
			name:            "cron 四段",
			raw:             setKey(minimalCrawlRaw(), "sync", map[string]any{"cron": "0 */6 * *"}),
			wantErrContains: "必须是 5 段标准 cron 表达式",
		},
		{
			name:            "cron 六段",
			raw:             setKey(minimalCrawlRaw(), "sync", map[string]any{"cron": "0 */6 * * * *"}),
			wantErrContains: "必须是 5 段标准 cron 表达式",
		},
		{
			name:            "cron 非字符串",
			raw:             setKey(minimalCrawlRaw(), "sync", map[string]any{"cron": 5}),
			wantErrContains: "sync.cron 必须是字符串",
		},
		{
			name:            "on_delete 非法枚举",
			raw:             setKey(minimalCrawlRaw(), "on_delete", "delete"),
			wantErrContains: `source delete policy "delete"`,
		},
		{
			name:            "on_delete 非字符串",
			raw:             setKey(minimalCrawlRaw(), "on_delete", 1),
			wantErrContains: "on_delete 必须是字符串",
		},

		// —— 运行态键（防 API 误写）——
		{name: "运行态键 sync_cursor", raw: setKey(minimalCrawlRaw(), "sync_cursor", "x"), wantErrContains: `运行态键 "sync_cursor"`},
		{
			name:            "运行态键 next_sync_at",
			raw:             setKey(minimalCrawlRaw(), "next_sync_at", "2026-09-16T00:00:00Z"),
			wantErrContains: `运行态键 "next_sync_at"`,
		},
		{
			name:            "运行态键 sync_requested_force",
			raw:             setKey(minimalCrawlRaw(), "sync_requested_force", true),
			wantErrContains: `运行态键 "sync_requested_force"`,
		},
		{
			name:            "运行态键 sync_last_result",
			raw:             setKey(minimalCrawlRaw(), "sync_last_result", "ok"),
			wantErrContains: `运行态键 "sync_last_result"`,
		},

		// —— 未知键拒绝（显式防拼写错误静默失效）——
		{name: "顶层未知键", raw: setKey(minimalCrawlRaw(), "priority", 1), wantErrContains: `未知键 "priority"`},
		{name: "crawl 未知键", raw: setNested(minimalCrawlRaw(), "crawl", "max_redirects", 3), wantErrContains: `未知键 "max_redirects"`},
		{name: "sitemap 未知键", raw: setNested(minimalSitemapRaw(), "sitemap", "changefreq", "daily"), wantErrContains: `未知键 "changefreq"`},
		{name: "fetch 未知键", raw: setKey(minimalCrawlRaw(), "fetch", map[string]any{"timeout": 30}), wantErrContains: `未知键 "timeout"`},
		{
			name: "auto_escalate 未知键",
			raw: setKey(minimalCrawlRaw(), "fetch", map[string]any{
				"engine": "auto", "connection_id": testWebSourceUUID,
				"auto_escalate": map[string]any{"threshold": 1},
			}),
			wantErrContains: `未知键 "threshold"`,
		},
		{
			name:            "extract 未知键",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"selector": "x"}),
			wantErrContains: `未知键 "selector"`,
		},
		{
			name:            "readability 未知键",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"readability": map[string]any{"min_chars": 1}}),
			wantErrContains: `未知键 "min_chars"`,
		},
		{
			// link_density_modifier 已从合同移除（所选 readability 实现无该字段，
			// 保留会形成「配了不生效」陷阱）：出现即按未知键拒绝。
			name:            "readability 已移除的 link_density_modifier",
			raw:             setKey(minimalCrawlRaw(), "extract", map[string]any{"readability": map[string]any{"link_density_modifier": 0.1}}),
			wantErrContains: `未知键 "link_density_modifier"`,
		},
		{
			name:            "sync 未知键",
			raw:             setKey(minimalCrawlRaw(), "sync", map[string]any{"interval": "6h"}),
			wantErrContains: `未知键 "interval"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := testWebSourceLimits
			if tt.limits != nil {
				limits = *tt.limits
			}
			got, err := ParseWebSourceConfig(tt.raw, limits)
			if err == nil {
				t.Fatalf("ParseWebSourceConfig 意外成功, got %+v", got)
			}
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("ParseWebSourceConfig 错误 = %v, 期望包含 %v", err, domainerrors.ErrValidation)
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("ParseWebSourceConfig 错误 = %q, 期望包含 %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}

// TestRejectImmutableWebSourceConfigKeys 验证 PATCH partial 中不可变键的识别：
// 只要出现即拒绝（无论值是否与旧值相同）；合并与全量校验属 application 层（T13）。
// 错误用例同样断言错误消息包含期望的键路径。
func TestRejectImmutableWebSourceConfigKeys(t *testing.T) {
	tests := []struct {
		name            string
		partial         map[string]any
		wantErrContains string // 空 = 期望成功
	}{
		{name: "空 partial", partial: map[string]any{}},
		{name: "nil partial", partial: nil},
		{name: "可变键 on_delete", partial: map[string]any{"on_delete": "remove"}},
		{name: "可变键 sync.cron", partial: map[string]any{"sync": map[string]any{"cron": "0 * * * *"}}},
		{name: "可变键 crawl.max_pages", partial: map[string]any{"crawl": map[string]any{"max_pages": 100}}},
		{
			name:    "可变键 fetch.auto_escalate",
			partial: map[string]any{"fetch": map[string]any{"auto_escalate": map[string]any{"enabled": false}}},
		},
		{
			name:            "顶层 mode",
			partial:         map[string]any{"mode": "crawl"},
			wantErrContains: "mode 创建后不可变",
		},
		{
			name:            "crawl.seed_url（即使值未变）",
			partial:         map[string]any{"crawl": map[string]any{"seed_url": "https://example.com/"}},
			wantErrContains: "crawl.seed_url 创建后不可变",
		},
		{
			name:            "sitemap.url",
			partial:         map[string]any{"sitemap": map[string]any{"url": "https://example.com/sitemap.xml"}},
			wantErrContains: "sitemap.url 创建后不可变",
		},
		{
			name:            "fetch.engine",
			partial:         map[string]any{"fetch": map[string]any{"engine": "http"}},
			wantErrContains: "fetch.engine 创建后不可变",
		},
		{
			name:            "fetch.connection_id",
			partial:         map[string]any{"fetch": map[string]any{"connection_id": testWebSourceUUID}},
			wantErrContains: "fetch.connection_id 创建后不可变",
		},
		{
			name: "crawl 混合可变与不可变键",
			partial: map[string]any{
				"crawl": map[string]any{"max_pages": 100, "seed_url": "https://example.com/"},
			},
			wantErrContains: "crawl.seed_url 创建后不可变",
		},
		{
			// crawl 非 map 时无从判定内部键，放行给全量解析兜底（类型错误在 Parse 报）
			name:    "crawl 非 map 交给全量解析兜底",
			partial: map[string]any{"crawl": "oops"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RejectImmutableWebSourceConfigKeys(tt.partial)
			if tt.wantErrContains == "" {
				if err != nil {
					t.Fatalf("RejectImmutableWebSourceConfigKeys(%v) 报错: %v", tt.partial, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("RejectImmutableWebSourceConfigKeys(%v) 意外成功", tt.partial)
			}
			if !errors.Is(err, domainerrors.ErrValidation) {
				t.Fatalf("错误 = %v, 期望包含 %v", err, domainerrors.ErrValidation)
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("错误 = %q, 期望包含 %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}

// TestWebSourceConfigImmutableKeys 钉死不可变键清单：T13（REST PATCH）会直接消费该导出契约。
func TestWebSourceConfigImmutableKeys(t *testing.T) {
	want := []string{"mode", "crawl.seed_url", "sitemap.url", "fetch.engine", "fetch.connection_id"}
	if !slices.Equal(WebSourceConfigImmutableKeys, want) {
		t.Fatalf("WebSourceConfigImmutableKeys = %v, 期望 %v", WebSourceConfigImmutableKeys, want)
	}
}
