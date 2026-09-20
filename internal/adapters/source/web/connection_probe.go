package web

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"

	"github.com/dajee/langhuan/internal/adapters/httpclient"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 连接测试探测（v1.3.0 spec §9.4）：firecrawl/jina/browser 三 provider 的非持久化
// 连通性检查，供连接管理台在保存后验证「测试可用 = 运行时可用」——三个探测全部
// 复用运行时抓取路径（FirecrawlFetcher/JinaFetcher 与渲染引擎的建连代码），不引入
// 第二套协议实现。feishu 无测试（维持现状）。
//
// 失败信息面向管理台展示：只含端点形态与 HTTP 语义，绝不含凭证（CDP 端点错误
// 经 sanitizeCDPEndpoint 脱敏，provider 错误不回显 Authorization）。
// 测试结果由调用方（service 层）直接返回，不落库。

// connectionProbeTargetURL 是连接测试抓取的固定目标页：example.com 保留域，
// 稳定、轻量、无 robots 争议。
const connectionProbeTargetURL = "https://example.com"

// defaultProbeTimeout 是单次连接测试的超时上限（含 provider 响应与 CDP 握手）；
// 与 provider 通道的默认请求超时同量级。
const defaultProbeTimeout = 30 * time.Second

// ConnectionProber 实现 service.SourceConnectionProber（结构化类型，无需反向依赖
// application 层）。零值不可用，经 NewConnectionProber 构造。
type ConnectionProber struct {
	timeout time.Duration
	// validateTarget 可注入（测试绕过真实 DNS 预校验）；nil 用
	// httpclient.ValidateWebTarget（与运行时抓取同一语义）。
	validateTarget func(ctx context.Context, rawURL string) error
	// jinaEndpoint 仅测试注入（jina 连接无端点覆盖入口，生产恒用默认端点）。
	jinaEndpoint string
	// testCDP 可注入（测试不连真实 CDP）；nil 用 TestCDPEndpoint。
	testCDP func(ctx context.Context, endpointURL, token string, timeout time.Duration) error
}

// NewConnectionProber 构造连接测试探测；timeout<=0 用内置默认。
func NewConnectionProber(timeout time.Duration) *ConnectionProber {
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	return &ConnectionProber{timeout: timeout}
}

// Probe 按 provider 执行一次连接测试；失败以 error 表达（文案面向管理台，
// 不含凭证），成功返回 nil。endpointURL 是连接 config 的 endpoint_url
// （firecrawl 的 API base 覆盖 / browser 的 CDP 端点；jina 无覆盖入口）。
func (p *ConnectionProber) Probe(ctx context.Context, provider value.SourceProvider, secret, endpointURL string) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	switch provider {
	case value.SourceProviderFirecrawl:
		return p.probeFirecrawl(ctx, secret, endpointURL)
	case value.SourceProviderJina:
		return p.probeJina(ctx, secret)
	case value.SourceProviderBrowser:
		return p.probeBrowser(ctx, endpointURL, secret)
	default:
		return fmt.Errorf("%s 连接不支持连接测试", provider)
	}
}

// probeFirecrawl 复用 firecrawl 引擎抓取固定测试页：断言 2xx（Fetch 内经
// ClassifyStatus 等价映射）且返回非空 markdown。
func (p *ConnectionProber) probeFirecrawl(ctx context.Context, apiKey, endpointURL string) error {
	fetcher, err := NewFirecrawlFetcher(FirecrawlFetcherConfig{
		APIKey:         apiKey,
		Endpoint:       endpointURL,
		Timeout:        p.timeout,
		validateTarget: p.validateTargetOrDefault(),
	})
	if err != nil {
		return fmt.Errorf("firecrawl 连接测试失败: %w", err)
	}
	page, err := fetcher.Fetch(ctx, FetchRequest{URL: connectionProbeTargetURL})
	if err != nil {
		return fmt.Errorf("firecrawl 连接测试失败: %w", err)
	}
	if len(strings.TrimSpace(string(page.Markdown))) == 0 {
		return fmt.Errorf("firecrawl 连接测试失败: 测试页 %s 未返回 markdown", connectionProbeTargetURL)
	}
	return nil
}

// probeJina 复用 jina 引擎抓取固定测试页：断言 reader 通道 2xx（Fetch 内判定）。
func (p *ConnectionProber) probeJina(ctx context.Context, apiKey string) error {
	fetcher, err := NewJinaFetcher(JinaFetcherConfig{
		APIKey:         apiKey,
		Endpoint:       p.jinaEndpoint,
		Timeout:        p.timeout,
		validateTarget: p.validateTargetOrDefault(),
	})
	if err != nil {
		return fmt.Errorf("jina 连接测试失败: %w", err)
	}
	if _, err := fetcher.Fetch(ctx, FetchRequest{URL: connectionProbeTargetURL}); err != nil {
		return fmt.Errorf("jina 连接测试失败: %w", err)
	}
	return nil
}

// probeBrowser 对 CDP 端点做一次 Browser.getVersion 握手（连接成功即通过，
// 不打开任何页面）。endpoint_url 是连接可用性前提，缺失属防御路径。
func (p *ConnectionProber) probeBrowser(ctx context.Context, endpointURL, token string) error {
	endpointURL = strings.TrimSpace(endpointURL)
	if endpointURL == "" {
		return fmt.Errorf("browser 连接测试失败: 缺少 config.endpoint_url")
	}
	testCDP := p.testCDP
	if testCDP == nil {
		testCDP = TestCDPEndpoint
	}
	if err := testCDP(ctx, endpointURL, token, p.timeout); err != nil {
		return fmt.Errorf("browser 连接测试失败: %w", err)
	}
	return nil
}

func (p *ConnectionProber) validateTargetOrDefault() func(ctx context.Context, rawURL string) error {
	if p.validateTarget != nil {
		return p.validateTarget
	}
	return httpclient.ValidateWebTarget
}

// TestCDPEndpoint 对 CDP 端点做一次轻量握手（spec §9.4「browser 握手
// Browser.getVersion」）：完整复用渲染引擎的建连路径（bounded dial + ws 握手 +
// CDP 客户端就绪），再显式调用 Browser.getVersion 确认端点可响应，随后立即断开。
// 错误文案中的端点经 sanitizeCDPEndpoint 脱敏（token 以 query 附加，绝不入错误链）。
func TestCDPEndpoint(ctx context.Context, endpointURL, token string, timeout time.Duration) error {
	nav, err := newRodNavigator(ctx, RenderCDPEndpoint(endpointURL, token), timeout)
	if err != nil {
		return err
	}
	defer func() { _ = nav.Close() }()
	if _, err := (proto.BrowserGetVersion{}).Call(nav.browser); err != nil {
		return fmt.Errorf("CDP Browser.getVersion 握手失败 (%s): %w", sanitizeCDPEndpoint(endpointURL), err)
	}
	return nil
}
